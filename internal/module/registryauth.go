package module

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	ref "github.com/distribution/reference"
)

// registryauth.go —— 私有仓凭据解析（2026-11-01）
//
// 为什么需要：checkupdate 原先用匿名身份取 manifest（GetToken(image, "")），
// Docker Hub **私有**仓对匿名 HEAD 直接 401 ⇒ GetDigest 报错 ⇒ checkSingleImage 提前 return
// ⇒ 该镜像连 i.Data[id] 都不会写 ⇒ 前端既不说"有更新"也不说"已最新"，看起来像"没通知"。
// 实测（zchjoe/nc-bridge 私有）：dc 容器只挂了 docker.sock，容器内没有任何 docker 凭据，
// 所以必须（a）让代码去读凭据，（b）部署时把 ~/.docker/config.json 只读挂进来。
//
// ⚠️ 我们**不去调用 dockerd 代拉 manifest**：DistributionInspect 也需要显式传
// X-Registry-Auth，daemon 不会读客户端的 config.json。所以凭据只能由本进程自己读。

// dockerConfigAuth 是 config.json 里单条 registry 的凭据形态。
// 两种写法都要支持：新版只有 auth（base64(user:pass)）；老版可能拆成 username/password。
type dockerConfigAuth struct {
	Auth     string `json:"auth"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type dockerConfigFile struct {
	Auths map[string]dockerConfigAuth `json:"auths"`
}

// dockerConfigCandidates 按优先级列出可能存在的 docker config 路径。
// 容器里通常只有 /root/.docker/config.json（挂载进来）或 /data/docker-config.json（自定义）。
func dockerConfigCandidates() []string {
	out := make([]string, 0, 4)
	if dir := strings.TrimSpace(os.Getenv("DOCKER_CONFIG")); dir != "" {
		out = append(out, filepath.Join(dir, "config.json"))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		out = append(out, filepath.Join(home, ".docker", "config.json"))
	}
	out = append(out, "/root/.docker/config.json", "/data/docker-config.json")
	return out
}

// normalizeRegistryKey 把 config.json 里的 auths key 和镜像里的 registry 域名归一到同一形态。
//
// 归一化规则（实测各工具写法不一）：
//
//	https://index.docker.io/v1/  → docker.io
//	registry-1.docker.io         → docker.io
//	docker.io / docker.io/v1     → docker.io
//	ghcr.io/                     → ghcr.io
func normalizeRegistryKey(key string) string {
	k := strings.ToLower(strings.TrimSpace(key))
	k = strings.TrimPrefix(k, "https://")
	k = strings.TrimPrefix(k, "http://")
	k = strings.TrimSuffix(k, "/")
	if i := strings.Index(k, "/v1"); i >= 0 {
		k = k[:i]
	}
	switch k {
	case "index.docker.io", "registry-1.docker.io", "docker.io", "registry.hub.docker.com":
		return "docker.io"
	}
	return k
}

// registryOf 从镜像名解析 registry 域名（统一归一化）。
func registryOf(imageName string) string {
	normalized, err := ref.ParseNormalizedNamed(imageName)
	if err != nil {
		// 解析不了就退回"有没有点号"的粗判，保证常见场景（显式域名）还能命中
		if i := strings.Index(imageName, "/"); i > 0 && strings.Contains(imageName[:i], ".") {
			return normalizeRegistryKey(imageName[:i])
		}
		return "docker.io"
	}
	return normalizeRegistryKey(ref.Domain(normalized))
}

// AuthFor 返回该镜像对应 registry 的 base64(user:pass)；取不到返回空串（=匿名）。
func AuthFor(imageName string) string {
	auth, _ := AuthForWithSource(imageName)
	return auth
}

// AuthForWithSource 同 AuthFor，额外返回凭据来源（配置文件路径或 "env"），供日志与 /status 展示。
func AuthForWithSource(imageName string) (string, string) {
	// 兜底：显式环境变量（一次性验证 / 无法挂文件时用）
	if v := strings.TrimSpace(os.Getenv("DC_REGISTRY_AUTH")); v != "" {
		return v, "env:DC_REGISTRY_AUTH"
	}
	reg := registryOf(imageName)
	for _, path := range dockerConfigCandidates() {
		cfg, err := readDockerConfig(path)
		if err != nil {
			continue
		}
		if auth, ok := lookupAuth(cfg, reg); ok {
			return auth, path
		}
	}
	return "", ""
}

func readDockerConfig(path string) (*dockerConfigFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg dockerConfigFile
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// lookupAuth 在 auths 里找该 registry 的凭据；找不到 ok=false。
func lookupAuth(cfg *dockerConfigFile, registry string) (string, bool) {
	if cfg == nil {
		return "", false
	}
	for key, entry := range cfg.Auths {
		if normalizeRegistryKey(key) != registry {
			continue
		}
		if entry.Auth != "" {
			return entry.Auth, true
		}
		if entry.Username != "" || entry.Password != "" {
			return base64.StdEncoding.EncodeToString([]byte(entry.Username + ":" + entry.Password)), true
		}
	}
	return "", false
}
