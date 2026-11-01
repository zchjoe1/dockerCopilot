package module

import (
	"crypto/tls"
	"errors"
	"fmt"
	ref "github.com/distribution/reference"
	"github.com/onlyLTY/dockerCopilot/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
	"io"
	"net"
	"net/http"
	url2 "net/url"
	"strings"
	"time"
)

// ImageCheckList 检查更新处理后的镜像列表
//
// 2026-11-01：加 Status/Err。原来只有 NeedUpdate 一个 bool，而"检查失败"（例如私有仓
// 匿名取 manifest 被 401）是**直接 return、什么都不写** ⇒ 前端既不说"有更新"也不说"已最新"，
// 用户看到的就是"这个镜像没有通知"。现在三类结果都落一条记录：
//
//	Status="update" 有更新可拉
//	Status="ok"     已是最新
//	Status="error"  检查失败（Err 写原因，前端/日志都能看出是缺凭据还是网络问题）
type ImageCheckList struct {
	NeedUpdate bool
	Status     string
	Err        string
}
type ImageUpdateData struct {
	Data map[string]ImageCheckList
}

const ContentDigestHeader = "Docker-Content-Digest"

func NewImageCheck() *ImageUpdateData {
	return &ImageUpdateData{
		Data: map[string]ImageCheckList{},
	}
}
func (i *ImageUpdateData) CheckUpdate(imageList []types.Image) {
	for _, image := range imageList {
		if strings.Contains(image.ImageName, "0nlylty/dockercopilot") {
			continue
		}
		i.checkSingleImage(image)
	}
}

func (i *ImageUpdateData) checkSingleImage(image types.Image) {
	// 2026-11-01：带上私有仓凭据。原来传 "" ⇒ 私有仓（如 zchjoe/nc-bridge）匿名 HEAD 必 401。
	auth, authSource := AuthForWithSource(image.ImageName)
	token, err := GetToken(image, auth)
	if err != nil {
		logx.Error("获取token失败或者无需获取token，继续尝试检查" + err.Error())
	}
	digestURL, err := BuildManifestURL(image)
	if err != nil {
		logx.Error("获取digestURL失败" + err.Error())
		i.markCheck(image.ID, "error", "构建 manifest 地址失败："+err.Error())
		return
	}
	remoteDigest, err := GetDigest(digestURL, token)
	if err != nil {
		logx.Error("获取digest失败" + err.Error())
		msg := err.Error()
		// 401 + 没凭据 = 私有仓没配认证。这是最可能的原因，直接把"该怎么修"写进错误里，
		// 免得下次又要在日志里翻半天才想到挂 config.json。
		if auth == "" && strings.Contains(msg, "401") {
			msg += "（私有仓需要 docker 凭据：把宿主机 ~/.docker/config.json 只读挂进容器，或设置 DC_REGISTRY_AUTH）"
		} else if auth != "" {
			msg += "（已用凭据：来源 " + authSource + "）"
		}
		i.markCheck(image.ID, "error", msg)
		return
	}
	if len(image.RepoDigests) == 0 {
		logx.Error("未在本地获取到repoDigest" + image.ImageName + ":" + image.ImageTag)
		i.markCheck(image.ID, "error", "本地没有 repoDigest（镜像可能未从 registry 拉取）")
		return
	}
	needUpdate := false
	for _, localRepoDigests := range image.RepoDigests {
		localDigest := strings.Split(localRepoDigests, "@")[1]
		if remoteDigest != localDigest {
			if remoteDigest == "" || localDigest == "" {
				logx.Error("Digest为空" + image.ImageName + ":" + image.ImageTag)
				continue
			}
			logx.Info(image.ImageName + ":" + image.ImageTag + " need update")
			logx.Infof("localDigest: %s, remoteDigest: %s", localDigest, remoteDigest)
			needUpdate = true
		} else {
			logx.Info(image.ImageName + ":" + image.ImageTag + " not need update")
			needUpdate = false
		}
	}
	status := "ok"
	if needUpdate {
		status = "update"
	}
	i.markCheck(image.ID, status, "")
}

// markCheck 统一落一条检查结果（成功与失败都写，避免"沉默的镜像"）。
func (i *ImageUpdateData) markCheck(imageID, status, errMsg string) {
	i.Data[imageID] = ImageCheckList{
		NeedUpdate: status == "update",
		Status:     status,
		Err:        errMsg,
	}
}

func BuildManifestURL(image types.Image) (string, error) {
	normalizedRef, err := ref.ParseDockerRef(image.ImageName + ":" + image.ImageTag)
	if err != nil {
		return "", err
	}
	normalizedTaggedRef, isTagged := normalizedRef.(ref.NamedTagged)
	if !isTagged {
		return "", errors.New("镜像无tag" + normalizedRef.String())
	}

	host, ErrGetRegistryAddress := GetRegistryAddress(normalizedTaggedRef.Name())
	img, tag := ref.Path(normalizedTaggedRef), normalizedTaggedRef.Tag()

	if ErrGetRegistryAddress != nil {
		return "", ErrGetRegistryAddress
	}

	url := url2.URL{
		Scheme: "https",
		Host:   host,
		Path:   fmt.Sprintf("/v2/%s/manifests/%s", img, tag),
	}
	return url.String(), nil
}

func GetDigest(url string, token string) (string, error) {
	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}

	req, _ := http.NewRequest("HEAD", url, nil)

	if token != "" {
		req.Header.Add("Authorization", token)
	}
	req.Header.Add("Accept", "application/vnd.docker.distribution.manifest.v2+json")
	req.Header.Add("Accept", "application/vnd.docker.distribution.manifest.list.v2+json")
	req.Header.Add("Accept", "application/vnd.docker.distribution.manifest.v1+json")
	req.Header.Add("Accept", "application/vnd.oci.image.index.v1+json")

	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			logx.Error("GetDigest关闭body失败" + err.Error())
		}
	}(res.Body)

	if res.StatusCode != 200 {
		wwwAuthHeader := res.Header.Get("www-authenticate")
		if wwwAuthHeader == "" {
			wwwAuthHeader = "not present"
		}
		return "", fmt.Errorf("registry responded to head request with %q, auth: %q", res.Status, wwwAuthHeader)
	}
	return res.Header.Get(ContentDigestHeader), nil
}
