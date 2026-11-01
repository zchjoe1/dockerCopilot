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
	dctypes "github.com/onlyLTY/dockerCopilot/internal/types"
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

	// 2026-11-01：把「只查关心的镜像 / 自定义检查节奏」两个开关收进仓库。
	// 来源：NAS 上那台 dc 是**本地改过再构建**的（compose 里写着 CHECK_ONLY / CHECK_CRON），
	// 代码从没进仓库 ⇒ 一换成 registry 里的镜像这两个变量就失效，dc 会每 30 分钟检查全部镜像
	// （实测那台有 20+ 个镜像，Docker Hub 匿名额度会被打满，私有仓还会 401）。
	// 所以按原行为在这里实现：
	//   CHECK_ONLY=zchjoe/procurement-center,zchjoe/nc-bridge   ← 逗号分隔；为空 = 全查
	//   CHECK_CRON=*/5 * * * *                                   ← 5 段标准 cron；默认 30 * * * *
	// 匹配规则用**子串包含**：写 zchjoe/nc-bridge 也能命中 zchjoe/nc-bridge:latest。
	checkOnly := parseCheckOnly(os.Getenv("CHECK_ONLY"))
	checkCron := strings.TrimSpace(os.Getenv("CHECK_CRON"))
	if checkCron == "" {
		checkCron = "30 * * * *"
	}
	if len(checkOnly) > 0 {
		logx.Infof("[checkupdate] 只检查 %d 个镜像：%s", len(checkOnly), strings.Join(checkOnly, ", "))
	}
	logx.Infof("[checkupdate] 检查节奏：%s", checkCron)

	list, err := utiles.GetImagesList(ctx)
	if err != nil {
		logx.Errorf("panic获取镜像列表出错: %v", err)
		panic(err)
	}
	go ctx.HubImageInfo.CheckUpdate(filterImages(list, checkOnly))
	corndanmu := cron.New(cron.WithParser(cron.NewParser(
		cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow,
	)))
	_, err = corndanmu.AddFunc(checkCron, func() {
		list, err := utiles.GetImagesList(ctx)
		if err != nil {
			logx.Errorf("panic获取镜像列表出错: %v", err)
			panic(err)
		}
		ctx.HubImageInfo.CheckUpdate(filterImages(list, checkOnly))
	})
	if err != nil {
		logx.Errorf("panic添加定时任务出错: %v", err)
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

// parseCheckOnly 解析 CHECK_ONLY（逗号分隔，去空白、去重、忽略空项）。
// 为空表示"不限制"，即保持上游默认行为（检查全部镜像）。
func parseCheckOnly(raw string) []string {
	out := make([]string, 0, 4)
	seen := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		p := strings.TrimSpace(part)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// filterImages 按 CHECK_ONLY 过滤镜像列表（子串匹配 ImageName，写 zchjoe/nc-bridge 即可命中 tag 形式）。
// checkOnly 为空时原样返回。
func filterImages(list []dctypes.Image, checkOnly []string) []dctypes.Image {
	if len(checkOnly) == 0 {
		return list
	}
	out := make([]dctypes.Image, 0, len(list))
	for _, img := range list {
		for _, want := range checkOnly {
			if strings.Contains(img.ImageName, want) {
				out = append(out, img)
				break
			}
		}
	}
	return out
}
