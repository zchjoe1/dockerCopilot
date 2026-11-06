package utiles

import (
	"os"
	"path/filepath"
	"testing"
)

// 守两条：
//  1. 默认路径必须落在 dc 自己的项目目录里（2026-11-04 从顶层 .dockercopilot/ 迁过来，
//     与已有的 dockercopilot/ 项目目录并存时顶层显得杂乱）；
//  2. DC_COMPOSE_BIN 能覆盖它 —— 上次挪路径被迫重建镜像，就是因为路径是编译期死值。
func TestComposeBinPath(t *testing.T) {
	t.Setenv("DC_COMPOSE_BIN", "")
	want := filepath.Join(ComposeRoot, "dockercopilot", ".bin", "docker-compose")
	if got := ComposeBinPath(); got != want {
		t.Fatalf("默认路径 = %q，期望 %q", got, want)
	}

	t.Setenv("DC_COMPOSE_BIN", "/tmp/whatever/docker-compose")
	if got := ComposeBinPath(); got != "/tmp/whatever/docker-compose" {
		t.Fatalf("环境变量覆盖失效：%q", got)
	}

	// 空白字符不应该被当成"用户设了值"
	t.Setenv("DC_COMPOSE_BIN", "   ")
	if got := ComposeBinPath(); got != want {
		t.Fatalf("空白值应当回落到默认，实际 %q", got)
	}

	if _, err := os.Stat(want); err == nil {
		t.Logf("提示：本机正好存在 %s", want)
	}
}
