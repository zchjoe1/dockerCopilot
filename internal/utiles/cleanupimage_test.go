package utiles

import "testing"

// 2026-11-06：清理旧镜像的三道闸门。
// 这些用例的价值在于"别把不该删的删了" —— 删错一次就是丢回滚版本，
// 比不清理严重得多，所以每一道闸门都要有独立用例。
func TestCleanupDecision(t *testing.T) {
	const oldID = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	const newID = "sha256:2222222222222222222222222222222222222222222222222222222222222222"

	cases := []struct {
		name     string
		oldID    string
		newID    string
		users    []string
		wantOK   bool
		wantText string
	}{
		{"空 ID —— 信息不可信，宁可不删", "", newID, nil, false, "旧镜像 ID 不可用"},
		{"不是 sha256 开头 —— 可能传进来的是 name:tag", "zchjoe/nc-bridge:latest", newID, nil, false, "旧镜像 ID 不可用"},
		{"只有 sha256: 前缀、没有内容", "sha256:", newID, nil, false, "旧镜像 ID 不可用"},
		{"新旧相同 —— 本次没换镜像，删了就是自伤", oldID, oldID, nil, false, "新旧镜像相同"},
		{"仍被别的容器用着（含已停止）", oldID, newID, []string{"nc-bridge", "old-web"}, false, "仍被容器 nc-bridge、old-web 使用"},
		{"新镜像 ID 拿不到，但没有别的容器引用 —— 允许删", oldID, "", nil, true, ""},
		{"正常情况：不同镜像、无人引用 → 删", oldID, newID, nil, true, ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ok, reason := cleanupDecision(c.oldID, c.newID, c.users)
			if ok != c.wantOK {
				t.Fatalf("proceed = %v，期望 %v（reason=%q）", ok, c.wantOK, reason)
			}
			if c.wantText != "" && !contains(reason, c.wantText) {
				t.Fatalf("reason = %q，应包含 %q", reason, c.wantText)
			}
		})
	}
}

// 设置项默认值：老配置文件没有这个键时必须按"开"处理，
// 否则 bool 零值会把默认开变成默认关，用户会以为功能坏了。
func TestCleanOldImageOn(t *testing.T) {
	var unset Settings
	if !unset.CleanOldImageOn() {
		t.Fatal("未设置时应当默认开启清理")
	}
	off := false
	if (Settings{CleanOldImage: &off}).CleanOldImageOn() {
		t.Fatal("显式关闭时不应清理")
	}
	on := true
	if !(Settings{CleanOldImage: &on}).CleanOldImageOn() {
		t.Fatal("显式开启时应清理")
	}
}

func TestShortID(t *testing.T) {
	if got := shortID("sha256:abcdef0123456789"); got != "abcdef012345" {
		t.Fatalf("shortID = %q", got)
	}
	if got := shortID("sha256:abc"); got != "abc" {
		t.Fatalf("短 ID 不应被截断，得到 %q", got)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
