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
	// CheckOnly **优先检测名单**（逗号分隔，不含 tag）；空 = 全部镜像都走快档。
	//
	// 【2026-10-07 语义调整】原先语义是「**只**检测这些」，其余镜像永远不查 ——
	// 结果是那 20 个第三方镜像有更新也不知道。现在改为：
	//   名单里的 → 走**快档** CheckCron；不在名单里的 → 走**慢档** CheckCronOthers。
	//
	// ⚠️ 顺带更正一个长期误传：本检测**不消耗** Docker Hub 的 pull 额度。
	//    checkSingleImage 读远端 digest 用的是 `HEAD /v2/.../manifests/...`，而 Docker 官方
	//    文档明写「**HEAD requests are not counted**」；取 token 走 auth.docker.io，也不是
	//    manifest URL。官方口径：一个 pull = 最多两次 manifest 的 **GET**；
	//    匿名 100 次/6h/IP、认证 200 次/6h/账号。
	//    ⇒ 白名单存在的理由不是限流，而是「重要镜像勤查、其余慢查」的信号取舍。
	CheckOnly string `json:"checkOnly"`
	// CheckCron 快档周期（优先名单）；空 = 用环境变量 CHECK_CRON 或默认 "30 * * * *"。
	CheckCron string `json:"checkCron"`
	// CheckCronOthers 慢档周期（不在优先名单里的其余镜像）；空 = 用环境变量
	// CHECK_CRON_OTHERS 或默认 "30 4 * * *"（每天 4:30）。显式填 off/none/- = 关闭慢档。
	CheckCronOthers string `json:"checkCronOthers"`
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
