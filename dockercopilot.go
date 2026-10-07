package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"go/types"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/onlyLTY/dockerCopilot/internal/config"
	"github.com/onlyLTY/dockerCopilot/internal/handler"
	"github.com/onlyLTY/dockerCopilot/internal/svc"
	MyType "github.com/onlyLTY/dockerCopilot/internal/types"
	"github.com/onlyLTY/dockerCopilot/internal/utiles"
	"github.com/robfig/cron/v3"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/rest/httpx"
	"github.com/zeromicro/x/errors"
	xhttp "github.com/zeromicro/x/http"
)

//go:embed front/*
var embeddedFront embed.FS

var configFile = flag.String("f", "etc/dockerCopilot.yaml", "the config file")

type UnauthorizedResponse struct {
	Code int                    `json:"code"`
	Msg  string                 `json:"msg"`
	Data map[string]interface{} `json:"data"`
}

func main() {
	logDir := "./logs"
	ErrSetupLog := SetupLog(logDir)
	if ErrSetupLog != nil {
		logx.Errorf("failed to setup log: %v", ErrSetupLog)
		os.Exit(1)
	}
	logx.SetLevel(logx.InfoLevel)

	flag.Parse()
	var c config.Config
	err := conf.Load(*configFile, &c, conf.UseEnv())
	if err != nil {
		logx.Errorf("无法加载配置文件出错: %v", err)
		logx.Errorf("请确认secretKey设置正确，要求非纯数字且大于八位")
		os.Exit(1)
	}
	server := rest.MustNewServer(c.RestConf, rest.WithCors("*"), rest.WithUnauthorizedCallback(
		func(w http.ResponseWriter, r *http.Request, err error) {
			response := UnauthorizedResponse{
				Code: http.StatusUnauthorized, // 401
				Msg:  "未授权",
				Data: map[string]interface{}{},
			}
			httpx.WriteJson(w, http.StatusUnauthorized, response)
		}))
	defer server.Stop()
	ctx := svc.NewServiceContext(c)

	// Ensure data directory and config exist (Auto-init)
	dataDir := "/data/config/image"
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		logx.Errorf("Failed to create data directory: %v", err)
	}

	imageLogosPath := "/data/config/imageLogos.js"
	if _, err := os.Stat(imageLogosPath); os.IsNotExist(err) {
		defaultConfig := []byte(`// 自定义镜像logo配置
export const customImageLogos = {
};
`)
		if err := os.WriteFile(imageLogosPath, defaultConfig, 0644); err != nil {
			logx.Errorf("Failed to create default imageLogos.js: %v", err)
		}
	}

	// 启动时先探一次镜像列表：连不上 Docker 就直接退出，避免服务起在半残状态。
	// （实际的检测与白名单过滤在下面的 checkAll 里，它会再取一次。）
	if _, err := utiles.GetImagesList(ctx); err != nil {
		logx.Errorf("panic获取镜像列表出错: %v", err)
		panic(err)
	}
	// ── 本地修改（2026-10-07）────────────────────────────────────────────
	// 设置来源优先级：/data/settings.json（网页「设置」页） > 环境变量 > 内置默认。
	// 白名单每轮现读，改完下一轮即生效；周期变更由 PUT /api/settings 动态重挂定时任务，
	// 两者都不需要重启容器。
	//
	// ① 白名单：上游每轮检查【全部】镜像，每个约 2 次 registry 请求（取 token + 取 manifest）。
	//    22 个镜像 → 每轮约 44 次；Docker Hub 匿名限流 100 次 manifest/6h/IP
	//    → 单纯调短周期会撞限流，反而查不出更新。
	// ② 周期：上游硬编码 "30 * * * *"（每小时 :30），镜像推上去最多等 1 小时才被发现；
	//    且没有任何「立即检查」HTTP 端点（GET /api/containers 只返回内存缓存）。
	utiles.LoadSettings()
	appCtx = ctx

	go checkAll()

	// 【本地新增 2026-10-07】容器资源占用（CPU/内存）后台采样，
	// 供 /api/containers 把数字直接带进卡片。详见 utiles/containerstats.go 顶部说明。
	utiles.StartStatsSampler(ctx)

	corndanmu := cron.New(cron.WithParser(cron.NewParser(
		cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow,
	)))
	cronInst = corndanmu
	if err := applyCron(); err != nil {
		logx.Errorf("panic挂载定时任务失败: %v", err)
		panic(err)
	}
	corndanmu.Start()
	defer corndanmu.Stop()
	httpx.SetErrorHandler(func(err error) (int, any) {
		switch e := err.(type) {
		case *errors.CodeMsg:
			return http.StatusOK, xhttp.BaseResponse[types.Nil]{
				Code: e.Code,
				Msg:  e.Msg,
			}
		default:
			return http.StatusOK, xhttp.BaseResponse[types.Nil]{
				Code: 50000,
				Msg:  err.Error(),
			}
		}
	})
	handler.RegisterHandlers(server, ctx)
	RegisterHandlers(server)
	fmt.Printf("Starting server at %s:%d...\n", c.Host, c.Port)
	logx.Info("程序版本" + config.Version)
	server.Start()
}

// ── 本地新增（2026-10-07）：设置驱动的检测 ──────────────────────────
var (
	appCtx *svc.ServiceContext

	cronMu    sync.Mutex
	cronInst  *cron.Cron
	cronEntry cron.EntryID
)

// effectiveCron 按「设置 > 环境变量 > 默认」解析出周期表达式。
func effectiveCron() string {
	if v := strings.TrimSpace(utiles.GetSettings().CheckCron); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("CHECK_CRON")); v != "" {
		return v
	}
	return "30 * * * *"
}

// effectiveOnly 按「设置 > 环境变量」解析出白名单原始串。
func effectiveOnly() string {
	if v := strings.TrimSpace(utiles.GetSettings().CheckOnly); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv("CHECK_ONLY"))
}

// applyCron 用当前生效的周期重挂定时任务（先摘旧的再挂新的）。
// 启动时与 PUT /api/settings 之后都会调用，所以改周期不需要重启容器。
func applyCron() error {
	expr := effectiveCron()
	cronMu.Lock()
	defer cronMu.Unlock()
	if cronInst == nil {
		return nil
	}
	if cronEntry != 0 {
		cronInst.Remove(cronEntry)
		cronEntry = 0
	}
	id, err := cronInst.AddFunc(expr, checkAll)
	if err != nil {
		return fmt.Errorf("cron 表达式 %q 无效（需要 5 段：分 时 日 月 周）: %w", expr, err)
	}
	cronEntry = id
	logx.Infof("镜像更新检测周期: %q（可在「设置」页修改，无需重启）", expr)
	return nil
}

// checkAll 取一次镜像列表，按白名单过滤后交给检测器。启动时与每轮定时都会调用。
func checkAll() {
	if appCtx == nil {
		return
	}
	l, err := utiles.GetImagesList(appCtx)
	if err != nil {
		logx.Errorf("panic获取镜像列表出错: %v", err)
		panic(err)
	}
	only := utiles.SplitList(effectiveOnly())
	if len(only) == 0 {
		appCtx.HubImageInfo.CheckUpdate(l)
		return
	}
	filtered := make([]MyType.Image, 0, len(only))
	for _, img := range l {
		for _, n := range only {
			if img.ImageName == n {
				filtered = append(filtered, img)
				break
			}
		}
	}
	logx.Infof("白名单命中 %d / %d 个镜像", len(filtered), len(l))
	appCtx.HubImageInfo.CheckUpdate(filtered)
}

// settingsView 是 GET /api/settings 的返回：既给出已保存的值，
// 也给出「实际生效值」，这样界面能显示 env 兜底的结果。
type settingsView struct {
	CheckOnly     string `json:"checkOnly"`
	CheckCron     string `json:"checkCron"`
	EffectiveOnly string `json:"effectiveOnly"`
	EffectiveCron string `json:"effectiveCron"`
	SettingsFile  string `json:"settingsFile"`
}

func settingsGetHandler(w http.ResponseWriter, r *http.Request) {
	s := utiles.GetSettings()
	// 用 types.Resp 包一层：本项目的接口都是 {code,msg,data} 结构，
	// 前端（以及其它调用方）按 j.data 取值。httpx.OkJson 是裸返回，不能用。
	httpx.WriteJson(w, http.StatusOK, MyType.Resp{
		Code: 200,
		Msg:  "success",
		Data: settingsView{
			CheckOnly:     s.CheckOnly,
			CheckCron:     s.CheckCron,
			EffectiveOnly: effectiveOnly(),
			EffectiveCron: effectiveCron(),
			SettingsFile:  utiles.SettingsPath,
		},
	})
}

func settingsPutHandler(w http.ResponseWriter, r *http.Request) {
	var body utiles.Settings
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteJson(w, http.StatusOK, xhttp.BaseResponse[types.Nil]{Code: 40000, Msg: "请求体不是合法 JSON: " + err.Error()})
		return
	}
	body.CheckOnly = strings.TrimSpace(body.CheckOnly)
	body.CheckCron = strings.TrimSpace(body.CheckCron)

	// 先校验 cron 再落盘，避免存进去一个永远挂不上的表达式。
	if body.CheckCron != "" {
		if _, err := cron.ParseStandard(body.CheckCron); err != nil {
			httpx.WriteJson(w, http.StatusOK, xhttp.BaseResponse[types.Nil]{
				Code: 40001,
				Msg:  fmt.Sprintf("cron 表达式 %q 无效（需要 5 段：分 时 日 月 周，例如 */5 * * * *）: %v", body.CheckCron, err),
			})
			return
		}
	}
	if err := utiles.SaveSettings(body); err != nil {
		httpx.WriteJson(w, http.StatusOK, xhttp.BaseResponse[types.Nil]{Code: 50000, Msg: "写入设置失败: " + err.Error()})
		return
	}
	if err := applyCron(); err != nil {
		httpx.WriteJson(w, http.StatusOK, xhttp.BaseResponse[types.Nil]{Code: 40001, Msg: err.Error()})
		return
	}
	logx.Infof("设置已更新: 白名单=%q 周期=%q", body.CheckOnly, effectiveCron())
	go checkAll() // 改完立刻按新白名单跑一轮，不用等下一个周期
	httpx.WriteJson(w, http.StatusOK, MyType.Resp{
		Code: 200,
		Msg:  "success",
		Data: settingsView{
			CheckOnly:     body.CheckOnly,
			CheckCron:     body.CheckCron,
			EffectiveOnly: effectiveOnly(),
			EffectiveCron: effectiveCron(),
			SettingsFile:  utiles.SettingsPath,
		},
	})
}

// ── 本地新增（2026-10-07）：飞牛 compose 项目的读取 / 编辑 / 一键生效 ──────
//
// 项目名走查询参数或 JSON body，不用路径参数 —— 少依赖一层 go-zero 的
// 路径参数提取方式。名字合法性由 utiles 侧白名单兜底（禁止 ../ 穿越）。
type composeBody struct {
	Name    string `json:"name"`
	Content string `json:"content"`
	Action  string `json:"action"`
}

func composeListHandler(w http.ResponseWriter, r *http.Request) {
	projects, err := utiles.ListComposeProjects()
	if err != nil {
		httpx.WriteJson(w, http.StatusOK, MyType.Resp{Code: 50000, Msg: err.Error()})
		return
	}
	httpx.WriteJson(w, http.StatusOK, MyType.Resp{
		Code: 200,
		Msg:  "success",
		Data: map[string]interface{}{
			"root":      utiles.ComposeRoot,
			"bin":       utiles.ComposeBin,
			"available": utiles.ComposeAvailable(),
			"projects":  projects,
		},
	})
}

func composeGetHandler(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		httpx.WriteJson(w, http.StatusOK, MyType.Resp{Code: 40000, Msg: "缺少 name 参数"})
		return
	}
	p, content, err := utiles.ReadCompose(name)
	if err != nil {
		httpx.WriteJson(w, http.StatusOK, MyType.Resp{Code: 40001, Msg: err.Error()})
		return
	}
	httpx.WriteJson(w, http.StatusOK, MyType.Resp{
		Code: 200,
		Msg:  "success",
		Data: map[string]interface{}{"project": p, "content": content},
	})
}

func composePutHandler(w http.ResponseWriter, r *http.Request) {
	var body composeBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteJson(w, http.StatusOK, MyType.Resp{Code: 40000, Msg: "请求体不是合法 JSON: " + err.Error()})
		return
	}
	backup, err := utiles.SaveCompose(strings.TrimSpace(body.Name), body.Content)
	if err != nil {
		httpx.WriteJson(w, http.StatusOK, MyType.Resp{Code: 50000, Msg: err.Error()})
		return
	}
	logx.Infof("compose 已保存: %s（备份 %s）", body.Name, backup)
	httpx.WriteJson(w, http.StatusOK, MyType.Resp{
		Code: 200,
		Msg:  "success",
		Data: map[string]interface{}{"backup": backup},
	})
}

func composeApplyHandler(w http.ResponseWriter, r *http.Request) {
	var body composeBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteJson(w, http.StatusOK, MyType.Resp{Code: 40000, Msg: "请求体不是合法 JSON: " + err.Error()})
		return
	}
	name := strings.TrimSpace(body.Name)
	// 即使失败也把命令输出带回前端：出错时那段输出才是最有用的信息。
	out, err := utiles.ApplyCompose(name, strings.TrimSpace(body.Action))
	code, msg := 200, "success"
	if err != nil {
		code, msg = 50000, err.Error()
	}
	logx.Infof("compose 生效: %s action=%q err=%v", name, body.Action, err)
	httpx.WriteJson(w, http.StatusOK, MyType.Resp{
		Code: code,
		Msg:  msg,
		Data: map[string]interface{}{"output": out, "ok": err == nil},
	})
}

// POST /api/image/tag  {source, target}
//
// 给本地已有镜像重打一个标签。加它是因为一个具体处境：
// CI 推的标签是 zchjoe/dockercopilot:latest，而飞牛上现存的镜像是 :local，
// 飞牛的镜像拉取又是坏的（errno 52428822），compose 见到本地没有 :latest 就会去拉、然后失败。
// 有了这个接口就能就地 :local → :latest，不用拉任何东西。
func imageTagHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source string `json:"source"`
		Target string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteJson(w, http.StatusOK, MyType.Resp{Code: 40000, Msg: "请求体不是合法 JSON: " + err.Error()})
		return
	}
	body.Source = strings.TrimSpace(body.Source)
	body.Target = strings.TrimSpace(body.Target)
	if body.Source == "" || body.Target == "" {
		httpx.WriteJson(w, http.StatusOK, MyType.Resp{Code: 40000, Msg: "需要 source 和 target，例如 {\"source\":\"zchjoe/dockercopilot:local\",\"target\":\"zchjoe/dockercopilot:latest\"}"})
		return
	}
	if appCtx == nil || appCtx.DockerClient == nil {
		httpx.WriteJson(w, http.StatusOK, MyType.Resp{Code: 50000, Msg: "docker client 未就绪"})
		return
	}
	if err := appCtx.DockerClient.ImageTag(context.Background(), body.Source, body.Target); err != nil {
		httpx.WriteJson(w, http.StatusOK, MyType.Resp{Code: 50000, Msg: "打标签失败: " + err.Error()})
		return
	}
	logx.Infof("镜像已打标签: %s -> %s", body.Source, body.Target)
	httpx.WriteJson(w, http.StatusOK, MyType.Resp{
		Code: 200,
		Msg:  "success",
		Data: map[string]string{"source": body.Source, "target": body.Target},
	})
}

func RegisterHandlers(engine *rest.Server) {
	frontFS, err := fs.Sub(embeddedFront, "front")
	if err != nil {
		log.Fatal(err)
	}

	frontFileServer := http.StripPrefix("/manager", http.FileServer(http.FS(frontFS)))

	assetsHandler := http.FileServer(http.FS(frontFS))

	// Serve custom icons
	iconFileServer := http.StripPrefix("/src/config/image/", http.FileServer(http.Dir("/data/config/image")))
	engine.AddRoutes(
		[]rest.Route{
			{
				Method: http.MethodGet,
				Path:   "/src/config/image/:file",
				Handler: func(w http.ResponseWriter, r *http.Request) {
					iconFileServer.ServeHTTP(w, r)
				},
			},
		},
	)

	// 【本地新增 2026-10-07】设置接口：网页「设置」页读写白名单与检测周期。
	engine.AddRoutes(
		[]rest.Route{
			{
				Method:  http.MethodGet,
				Path:    "/api/settings",
				Handler: settingsGetHandler,
			},
			{
				Method:  http.MethodPut,
				Path:    "/api/settings",
				Handler: settingsPutHandler,
			},
		},
	)

	// 【本地新增 2026-10-07】飞牛 compose：列表 / 读取 / 保存 / 一键生效。
	engine.AddRoutes(
		[]rest.Route{
			{
				Method:  http.MethodGet,
				Path:    "/api/compose",
				Handler: composeListHandler,
			},
			{
				Method:  http.MethodGet,
				Path:    "/api/compose/file",
				Handler: composeGetHandler,
			},
			{
				Method:  http.MethodPut,
				Path:    "/api/compose/file",
				Handler: composePutHandler,
			},
			{
				Method:  http.MethodPost,
				Path:    "/api/compose/apply",
				Handler: composeApplyHandler,
			},
			{
				Method:  http.MethodPost,
				Path:    "/api/image/tag",
				Handler: imageTagHandler,
			},
		},
	)

	engine.AddRoutes(
		[]rest.Route{
			{
				Method: http.MethodGet,
				Path:   "/manager",
				Handler: func(w http.ResponseWriter, r *http.Request) {
					frontFileServer.ServeHTTP(w, r)
				},
			},
			{
				Method: http.MethodGet,
				Path:   "/manager/:path",
				Handler: func(w http.ResponseWriter, r *http.Request) {
					frontFileServer.ServeHTTP(w, r)
				},
			},
			{
				Method: http.MethodGet,
				Path:   "/manager/assets/:path",
				Handler: func(w http.ResponseWriter, r *http.Request) {
					frontFileServer.ServeHTTP(w, r)
				},
			},
			{
				Method: http.MethodGet,
				Path:   "/assets/:path",
				Handler: func(w http.ResponseWriter, r *http.Request) {
					assetsHandler.ServeHTTP(w, r)
				},
			},
		},
	)
}

// 检查并创建日志目录
func ensureLogDirectory(logDir string) error {
	if _, err := os.Stat(logDir); os.IsNotExist(err) {
		return os.MkdirAll(logDir, 0755) // 创建目录并设置权限
	}
	return nil
}

// SetupLog 初始化日志设置
func SetupLog(logDir string) error {
	// 检查日志目录是否存在
	if err := ensureLogDirectory(logDir); err != nil {
		return fmt.Errorf("failed to create log directory: %v", err)
	}

	logConf := logx.LogConf{
		Path:     logDir,
		Level:    "info",
		KeepDays: 7,
		Compress: true,
		Mode:     "file",
	}
	logx.MustSetup(logConf)
	logx.AddWriter(logx.NewWriter(os.Stdout))
	return nil
}
