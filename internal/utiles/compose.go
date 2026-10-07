package utiles

// 【本地新增 2026-10-07】飞牛 compose 项目的读取 / 编辑 / 一键生效。
//
// 背景与两个关键决定：
//
//  1. 挂载必须是【容器内同路径】：compose 文件里的相对路径（如 ./config:/data）
//     是相对 compose 文件所在目录解析的，而 Docker 把数据卷源当成【宿主机路径】。
//     所以必须 `-v /vol1/1000/docker:/vol1/1000/docker`（两边一模一样），
//     相对路径才会解析成正确的宿主机路径。挂到 /composehost 之类的别名必错。
//
//  2. 镜像里没有 docker CLI，也不想为此重建镜像（飞牛 pull 是坏的，新镜像进不来）。
//     所以把 docker-compose 静态二进制放在挂载目录里直接 exec：
//     /vol1/1000/docker/.dockercopilot/docker-compose
//     compose v2 的独立二进制可以不经 docker CLI 直接跑，自己通过 DOCKER_HOST 连 daemon。

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	// ComposeRoot 与宿主机同路径挂载，不能改。
	ComposeRoot = "/vol1/1000/docker"
	// ComposeBin 放在挂载目录里，这样不用动镜像。
	ComposeBin = ComposeRoot + "/.dockercopilot/docker-compose"
	// composeWait 单次生效命令的超时。拉镜像可能很慢，给宽一点。
	composeWait = 5 * time.Minute
	// composeOutputLimit 返回给前端的日志上限，避免把响应撑爆。
	composeOutputLimit = 64 * 1024
)

// composeNameRe 项目名只允许单层目录名，杜绝 ../ 穿越和绝对路径。
var composeNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ComposeProject 一个飞牛 compose 项目（一个目录 + 一个 compose 文件）。
type ComposeProject struct {
	Name       string `json:"name"`
	Dir        string `json:"dir"`
	ConfigFile string `json:"configFile"`
	Size       int64  `json:"size"`
	ModTime    string `json:"modTime"`
}

// ValidComposeName 校验项目名。
func ValidComposeName(name string) bool {
	return composeNameRe.MatchString(name) && !strings.Contains(name, "..")
}

// composeFilePath 找到项目目录下的 compose 文件。
// 飞牛自己的命名不统一：多数是 compose.yaml / docker-compose.yml，
// deepseek-harness 用的是 compose.host.yaml，所以按前缀匹配而不是写死文件名。
func composeFilePath(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var candidates []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		lower := strings.ToLower(n)
		if !strings.HasSuffix(lower, ".yaml") && !strings.HasSuffix(lower, ".yml") {
			continue
		}
		if strings.HasPrefix(lower, "compose") || strings.HasPrefix(lower, "docker-compose") {
			candidates = append(candidates, n)
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("目录 %s 下没有找到 compose 文件", dir)
	}
	// 同名优先级：compose.yaml > docker-compose.yml > 其它，保证结果稳定。
	sort.Slice(candidates, func(i, j int) bool {
		rank := func(s string) int {
			switch strings.ToLower(s) {
			case "compose.yaml":
				return 0
			case "compose.yml":
				return 1
			case "docker-compose.yaml":
				return 2
			case "docker-compose.yml":
				return 3
			}
			return 4
		}
		if rank(candidates[i]) != rank(candidates[j]) {
			return rank(candidates[i]) < rank(candidates[j])
		}
		return candidates[i] < candidates[j]
	})
	return filepath.Join(dir, candidates[0]), nil
}

// ListComposeProjects 扫 ComposeRoot 下的一层子目录，返回所有 compose 项目。
func ListComposeProjects() ([]ComposeProject, error) {
	entries, err := os.ReadDir(ComposeRoot)
	if err != nil {
		return nil, fmt.Errorf("读取 %s 失败（容器里挂载了吗？）: %w", ComposeRoot, err)
	}
	projects := make([]ComposeProject, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") { // .dockercopilot 这类隐藏目录不算项目
			continue
		}
		dir := filepath.Join(ComposeRoot, name)
		cfg, err := composeFilePath(dir)
		if err != nil {
			continue // 没有 compose 文件的目录直接跳过
		}
		p := ComposeProject{Name: name, Dir: dir, ConfigFile: cfg}
		if st, err := os.Stat(cfg); err == nil {
			p.Size = st.Size()
			p.ModTime = st.ModTime().Format("2006-01-02 15:04:05")
		}
		projects = append(projects, p)
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].Name < projects[j].Name })
	return projects, nil
}

// ReadCompose 读取某个项目的 compose 文件内容。
func ReadCompose(name string) (ComposeProject, string, error) {
	if !ValidComposeName(name) {
		return ComposeProject{}, "", fmt.Errorf("非法的项目名: %q", name)
	}
	dir := filepath.Join(ComposeRoot, name)
	cfg, err := composeFilePath(dir)
	if err != nil {
		return ComposeProject{}, "", err
	}
	b, err := os.ReadFile(cfg)
	if err != nil {
		return ComposeProject{}, "", fmt.Errorf("读取 %s 失败: %w", cfg, err)
	}
	p := ComposeProject{Name: name, Dir: dir, ConfigFile: cfg}
	if st, err := os.Stat(cfg); err == nil {
		p.Size = st.Size()
		p.ModTime = st.ModTime().Format("2006-01-02 15:04:05")
	}
	return p, string(b), nil
}

// SaveCompose 写回 compose 文件。先备份再原子替换，避免写坏。
// 返回备份文件路径（没有旧文件时为空）。
func SaveCompose(name, content string) (string, error) {
	if !ValidComposeName(name) {
		return "", fmt.Errorf("非法的项目名: %q", name)
	}
	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("拒绝写入空内容")
	}
	dir := filepath.Join(ComposeRoot, name)
	cfg, err := composeFilePath(dir)
	if err != nil {
		return "", err
	}

	backup := ""
	if old, err := os.ReadFile(cfg); err == nil {
		backup = fmt.Sprintf("%s.bak-%s", cfg, time.Now().Format("20060102-150405"))
		if err := os.WriteFile(backup, old, 0644); err != nil {
			return "", fmt.Errorf("写备份失败: %w", err)
		}
	}

	// 先写临时文件再 rename：中途出错不会留下半个文件。
	tmp := cfg + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("写临时文件失败: %w", err)
	}
	if err := os.Rename(tmp, cfg); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("替换文件失败: %w", err)
	}
	return backup, nil
}

// ApplyCompose 执行 docker-compose 命令，返回合并后的输出。
// action: up（默认生效）/ down / restart / config（只做语法校验）。
func ApplyCompose(name, action string) (string, error) {
	if !ValidComposeName(name) {
		return "", fmt.Errorf("非法的项目名: %q", name)
	}
	if _, err := os.Stat(ComposeBin); err != nil {
		return "", fmt.Errorf("找不到 compose 二进制 %s（需要先上传到挂载目录）: %w", ComposeBin, err)
	}
	// 通过 fnOS 文件接口上传的文件默认没有执行位，这里补一下。
	// 容器以 root 跑、挂载是 rw，所以能改；改不动就直接报出来，别等到 exec 才报 permission denied。
	if err := os.Chmod(ComposeBin, 0o755); err != nil {
		return "", fmt.Errorf("给 %s 加执行权限失败: %w", ComposeBin, err)
	}
	dir := filepath.Join(ComposeRoot, name)
	cfg, err := composeFilePath(dir)
	if err != nil {
		return "", err
	}

	var args []string
	switch action {
	case "", "up":
		// --remove-orphans：compose 里删掉的服务，其容器也一并清理。
		args = []string{"-p", name, "-f", cfg, "up", "-d", "--remove-orphans"}
	case "down":
		args = []string{"-p", name, "-f", cfg, "down"}
	case "restart":
		args = []string{"-p", name, "-f", cfg, "restart"}
	case "config":
		// 只校验 + 展开，不动容器，适合保存前自检。
		args = []string{"-p", name, "-f", cfg, "config"}
	default:
		return "", fmt.Errorf("不支持的动作: %q", action)
	}

	ctx, cancel := context.WithTimeout(context.Background(), composeWait)
	defer cancel()

	cmd := exec.CommandContext(ctx, ComposeBin, args...)
	cmd.Dir = dir // 相对路径以项目目录为基准，和飞牛自己的行为一致
	// DOCKER_HOST 继承容器环境（unix:///var/run/docker.sock）
	cmd.Env = append(os.Environ(), "COMPOSE_PROGRESS=plain")
	out, runErr := cmd.CombinedOutput()

	text := string(out)
	if len(text) > composeOutputLimit {
		text = text[:composeOutputLimit] + "\n...(输出过长已截断)"
	}
	if ctx.Err() == context.DeadlineExceeded {
		return text, fmt.Errorf("命令超时（%s）", composeWait)
	}
	if runErr != nil {
		return text, fmt.Errorf("docker-compose %s 失败: %w", action, runErr)
	}
	return text, nil
}

// ComposeAvailable 判断 compose 二进制是否就位，供前端提前提示。
func ComposeAvailable() bool {
	if _, err := os.Stat(ComposeBin); err != nil {
		return false
	}
	if _, err := os.Stat(ComposeRoot); err != nil {
		return false
	}
	return true
}
