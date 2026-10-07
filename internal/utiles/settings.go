package utiles

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
)

// 【本地新增 2026-10-07】设置存储。
//
// 存在 /data/settings.json —— /data 是挂载卷（宿主 /vol1/1000/docker/dockercopilot/config），
// 所以容器重建、镜像升级后设置都还在，不需要改 compose.yaml。
//
// 空字符串一律表示「未设置」：此时回退到环境变量，再回退到内置默认值。
// 这样既保留了原有的 env 配置方式，又能在网页上改。
const SettingsPath = "/data/settings.json"

type Settings struct {
	// CheckOnly 只检测这些镜像（逗号分隔，不含 tag）；空 = 全部镜像。
	// 上游每轮检查全部镜像，每个约 2 次 registry 请求，22 个镜像就是 44 次/轮；
	// Docker Hub 匿名限流 100 次 manifest/6h/IP，所以周期调短必须配合白名单。
	CheckOnly string `json:"checkOnly"`
	// CheckCron 检测周期的 cron 表达式；空 = 用环境变量 CHECK_CRON 或默认 "30 * * * *"。
	CheckCron string `json:"checkCron"`
}

var (
	settingsMu sync.RWMutex
	settings   = Settings{}
)

// LoadSettings 启动时从磁盘读一次。文件不存在/损坏都当作空设置，不阻断启动。
func LoadSettings() Settings {
	var s Settings
	if b, err := os.ReadFile(SettingsPath); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	settingsMu.Lock()
	settings = s
	settingsMu.Unlock()
	return s
}

// GetSettings 返回当前设置的副本。
func GetSettings() Settings {
	settingsMu.RLock()
	defer settingsMu.RUnlock()
	return settings
}

// SaveSettings 先写临时文件再 rename，避免写到一半断电留下坏文件。
func SaveSettings(s Settings) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := SettingsPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, SettingsPath); err != nil {
		return err
	}
	settingsMu.Lock()
	settings = s
	settingsMu.Unlock()
	return nil
}

// SplitList 把逗号分隔的镜像名切成切片，去空白与空项。
func SplitList(s string) []string {
	var out []string
	for _, n := range strings.Split(s, ",") {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return out
}
