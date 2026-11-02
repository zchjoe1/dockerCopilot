package module

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/docker/docker/client"
)

// daemondigest.go —— 用 Docker daemon 自己的通道取远端 digest（2026-11-01 夜）
//
// 为什么必须这样：实测这台 NAS **到 Docker Hub 直连不通**
//   · registry-1.docker.io → 解析到 199.96.63.53，TCP 直接超时（DNS 被污染）
//   · IPv6 AAAA → 黑洞
//   · 公共加速站（docker.1ms.run）→ 即使带 Basic 凭据也 401（它不代理私有仓）
// 但 `docker pull zchjoe/nc-bridge:latest` 是成功的 —— 因为 **dockerd 配了可用的
// registry-mirrors 且持有凭据**，这条通道能出去。
//
// ⇒ 检查更新不该自己裸连 registry，应当让 daemon 去取 manifest：
//   GET /distribution/{name}/json（client.DistributionInspect）= daemon 用自己的
//   镜像源 + 凭据解析，返回 Descriptor.Digest。这样镜像源、私有仓、代理全部沿用
//   daemon 的既有配置，dc 不再需要自己维护加速站列表。

// daemonAuth 把 config.json 的 base64(user:pass) 转成 daemon 要的 X-Registry-Auth
// （JSON {username,password,serveraddress} 的 base64）。取不到凭据返回 ""（=匿名）。
func daemonAuth(imageName string) string {
	auth, _ := AuthForWithSource(imageName)
	if auth == "" {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(auth)
	if err != nil {
		return ""
	}
	user, pass := "", ""
	for i := 0; i < len(raw); i++ {
		if raw[i] == ':' {
			user, pass = string(raw[:i]), string(raw[i+1:])
			break
		}
	}
	payload, err := json.Marshal(map[string]string{
		"username":      user,
		"password":      pass,
		"serveraddress": "https://index.docker.io/v1/",
	})
	if err != nil {
		return ""
	}
	return base64.URLEncoding.EncodeToString(payload)
}

// DigestViaDaemon 让 daemon 去取镜像 manifest 的 digest（走 daemon 的镜像源/凭据）。
func DigestViaDaemon(cli *client.Client, imageName, imageTag string) (string, error) {
	if cli == nil {
		return "", errNoDaemon
	}
	c, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	insp, err := cli.DistributionInspect(c, imageName+":"+imageTag, daemonAuth(imageName))
	if err != nil {
		return "", err
	}
	return insp.Descriptor.Digest.String(), nil
}

type daemonUnavailable struct{}

func (daemonUnavailable) Error() string { return "docker daemon 不可用" }

var errNoDaemon = daemonUnavailable{}
