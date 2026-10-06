package main

import (
	"embed"
	"flag"
	"fmt"
	"go/types"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"

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
	// ① CHECK_ONLY：只检测指定镜像（逗号分隔，不含 tag）。
	//    上游每轮检查【全部】镜像，每个约 2 次 registry 请求（取 token + 取 manifest）。
	//    本机 22 个镜像 → 每轮约 44 次请求；Docker Hub 匿名限流是 100 次 manifest/6h/IP
	//    → 单纯把周期调短会撞限流，反而查不出更新。设白名单后只查关心的那个，多快都安全。
	// ② CHECK_CRON：检测周期。上游硬编码 "30 * * * *"（每小时 :30），
	//    镜像推上去最多等 1 小时才被发现；且没有任何「立即检查」HTTP 端点
	//    （GET /api/containers 返回的只是内存缓存 ctx.HubImageInfo.Data）。
	// 两个变量都不设时，行为与上游完全一致（零变化）。
	checkOnly := strings.TrimSpace(os.Getenv("CHECK_ONLY"))
	var onlyNames []string
	if checkOnly != "" {
		for _, n := range strings.Split(checkOnly, ",") {
			if n = strings.TrimSpace(n); n != "" {
				onlyNames = append(onlyNames, n)
			}
		}
		logx.Infof("镜像更新检测白名单 CHECK_ONLY=%q（仅检测这些镜像）", checkOnly)
	}
	// checkAll 统一两处调用（启动时 + 定时）的过滤逻辑，避免只改一处造成行为不一致。
	checkAll := func() {
		l, err := utiles.GetImagesList(ctx)
		if err != nil {
			logx.Errorf("panic获取镜像列表出错: %v", err)
			panic(err)
		}
		if len(onlyNames) == 0 {
			ctx.HubImageInfo.CheckUpdate(l)
			return
		}
		filtered := make([]MyType.Image, 0, len(onlyNames))
		for _, img := range l {
			for _, n := range onlyNames {
				if img.ImageName == n {
					filtered = append(filtered, img)
					break
				}
			}
		}
		logx.Infof("白名单命中 %d / %d 个镜像", len(filtered), len(l))
		ctx.HubImageInfo.CheckUpdate(filtered)
	}
	go checkAll()
	corndanmu := cron.New(cron.WithParser(cron.NewParser(
		cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow,
	)))
	checkCron := os.Getenv("CHECK_CRON")
	if checkCron == "" {
		checkCron = "30 * * * *"
	}
	logx.Infof("镜像更新检测周期: %q（可用环境变量 CHECK_CRON 覆盖）", checkCron)
	_, err = corndanmu.AddFunc(checkCron, checkAll)
	if err != nil {
		logx.Errorf("panic添加定时任务出错（CHECK_CRON=%q，请检查 cron 表达式格式）: %v", checkCron, err)
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
