package utiles

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	dockerMsgType "github.com/docker/docker/pkg/jsonmessage"
	"github.com/onlyLTY/dockerCopilot/internal/module"
	"github.com/onlyLTY/dockerCopilot/internal/svc"
	"github.com/zeromicro/go-zero/core/logx"
	"io"
	"time"
)

func UpdateContainer(serviceContext *svc.ServiceContext, id string, name string, imageNameAndTag string, delOldContainer bool, taskID string) error {
	ctx := context.Background()
	serviceContext.UpdateProgress(taskID, svc.TaskProgress{
		TaskID:     taskID,
		Percentage: 0,
		Name:       name,
		Message:    "正在连接Docker",
		DetailMsg:  "正在连接Docker",
		IsDone:     false,
	})
	var oldTaskProgress, result = serviceContext.GetProgress(taskID)
	if !result {
		oldTaskProgress = svc.TaskProgress{
			Percentage: 0,
			Name:       "",
			Message:    "",
			DetailMsg:  "",
			IsDone:     false,
		}
	}
	timeout := 10
	signal := "SIGINT"

	serviceContext.UpdateProgress(taskID, oldTaskProgress)
	oldTaskProgress.Message = "正在拉取新镜像"
	oldTaskProgress.Percentage = 10
	oldTaskProgress.DetailMsg = "正在拉取新镜像"
	serviceContext.UpdateProgress(taskID, oldTaskProgress)
	serviceContext.DockerClient.NegotiateAPIVersion(ctx)
	/* ===== 2026-11-10：拉取也要带凭据 =====
	   现象：私有仓（zchjoe/nc-bridge）在 dc 里能看到「有更新」，点更新却失败：
	       pull access denied for zchjoe/nc-bridge, may require 'docker login'
	   原因：**检测**那步早就改过（ea46863：checkSingleImage 带 docker 凭据取 manifest），
	         但**拉取**这步一直是空的 image.PullOptions{} —— 靠宿主机 Docker 守护进程的登录态。
	         dc 容器里有凭据、宿主机没有 ⇒ 能查出更新、拉不动。
	   改法：把同一份凭据以 X-Registry-Auth 传给 daemon；取不到凭据时行为与原来完全一致。 */
	pullOpts := image.PullOptions{}
	if a := registryAuthForPull(imageNameAndTag); a != "" {
		pullOpts.RegistryAuth = a
	}
	reader, err := serviceContext.DockerClient.ImagePull(ctx, imageNameAndTag, pullOpts)
	if err != nil {
		oldTaskProgress.Message = "拉取镜像失败"
		oldTaskProgress.DetailMsg = err.Error()
		oldTaskProgress.IsDone = true
		serviceContext.UpdateProgress(taskID, oldTaskProgress)
		logx.Errorf("Failed to pull image: %s", err)
		return err
	}
	err = decodePullResp(reader, serviceContext, taskID)
	if err != nil {
		oldTaskProgress.Message = "拉取镜像失败"
		oldTaskProgress.DetailMsg = err.Error()
		oldTaskProgress.IsDone = true
		serviceContext.UpdateProgress(taskID, oldTaskProgress)
		logx.Errorf("Failed to pull image: %s", err)
		return err
	}
	oldTaskProgress, result = serviceContext.GetProgress(taskID)
	if !result {
		oldTaskProgress = svc.TaskProgress{
			Percentage: 0,
			Name:       "",
			Message:    "",
			DetailMsg:  "",
			IsDone:     false,
		}
	}
	oldTaskProgress.Message = "拉取镜像成功"
	oldTaskProgress.DetailMsg = "拉取镜像成功"

	oldTaskProgress.Percentage = 30
	oldTaskProgress.Message = "正在停止容器"
	oldTaskProgress.DetailMsg = "正在停止容器"
	serviceContext.UpdateProgress(taskID, oldTaskProgress)
	stopOptions := container.StopOptions{
		Signal:  signal,
		Timeout: &timeout,
	}
	err = serviceContext.DockerClient.ContainerStop(context.Background(), id, stopOptions)
	if err != nil {
		oldTaskProgress.Message = "停止容器失败"
		oldTaskProgress.DetailMsg = err.Error()
		oldTaskProgress.IsDone = true
		serviceContext.UpdateProgress(taskID, oldTaskProgress)
		return err
	}
	oldTaskProgress.Message = "容器停止成功"
	oldTaskProgress.DetailMsg = "容器停止成功"

	oldTaskProgress.Percentage = 40
	serviceContext.UpdateProgress(taskID, oldTaskProgress)
	oldTaskProgress.Message = "正在重命名旧容器"
	oldTaskProgress.DetailMsg = "正在重命名旧容器"
	serviceContext.UpdateProgress(taskID, oldTaskProgress)
	currentDate := time.Now().Format("2006-01-02-15-04-05")
	err = serviceContext.DockerClient.ContainerRename(context.Background(), id, name+"-"+currentDate)
	if err != nil {
		oldTaskProgress.Message = "重命名旧容器失败"
		oldTaskProgress.DetailMsg = err.Error()
		oldTaskProgress.IsDone = true
		serviceContext.UpdateProgress(taskID, oldTaskProgress)
		return err
	}
	oldTaskProgress.Message = "重命名旧容器成功"
	oldTaskProgress.DetailMsg = "重命名旧容器成功"
	oldTaskProgress.Percentage = 60
	serviceContext.UpdateProgress(taskID, oldTaskProgress)
	oldTaskProgress.Message = "正在创建新容器"
	oldTaskProgress.DetailMsg = "正在创建新容器"
	serviceContext.UpdateProgress(taskID, oldTaskProgress)
	inspectedContainer, err := serviceContext.DockerClient.ContainerInspect(ctx, id)
	if err != nil {
		oldTaskProgress.Message = "获取容器信息失败"
		oldTaskProgress.DetailMsg = err.Error()
		oldTaskProgress.IsDone = true
		serviceContext.UpdateProgress(taskID, oldTaskProgress)
		logx.Error("获取容器信息失败" + err.Error())
		return err
	}
	// 2026-11-06：**先**记下旧镜像 ID —— 下面几行会把 inspectedContainer.Image 改成
	// name:tag（重建容器要用），那之后就再也拿不到"更新前跑的是哪个镜像"了。
	// 这就是更新完成后要清理的那个镜像。
	oldImageID := inspectedContainer.Image

	inspectedContainer.Config.Hostname = ""
	inspectedContainer.Config.Image = imageNameAndTag
	inspectedContainer.Image = imageNameAndTag
	config := inspectedContainer.Config
	hostConfig := inspectedContainer.HostConfig
	networkingConfig := &network.NetworkingConfig{
		EndpointsConfig: inspectedContainer.NetworkSettings.Networks,
	}
	containerName := name
	_, err = serviceContext.DockerClient.ContainerCreate(ctx, config, hostConfig, networkingConfig, nil, containerName)
	if err != nil {
		oldTaskProgress.Message = "创建新容器失败"
		oldTaskProgress.DetailMsg = err.Error()
		oldTaskProgress.IsDone = true
		serviceContext.UpdateProgress(taskID, oldTaskProgress)
		return err
	}
	oldTaskProgress.Message = "创建新容器成功"
	oldTaskProgress.DetailMsg = "创建新容器成功"
	oldTaskProgress.Percentage = 80
	serviceContext.UpdateProgress(taskID, oldTaskProgress)
	oldTaskProgress.Message = "正在启动新容器以及删除旧容器(如果不保留旧容器)"
	oldTaskProgress.DetailMsg = "正在启动新容器以及删除旧容器(如果不保留旧容器)"
	serviceContext.UpdateProgress(taskID, oldTaskProgress)
	err = serviceContext.DockerClient.ContainerStart(context.Background(), containerName, container.StartOptions{
		CheckpointID:  "",
		CheckpointDir: "",
	})
	if err != nil {
		oldTaskProgress.Message = "启动新容器失败"
		oldTaskProgress.DetailMsg = err.Error()
		oldTaskProgress.IsDone = true
		serviceContext.UpdateProgress(taskID, oldTaskProgress)
		return err
	}
	if delOldContainer {
		err = serviceContext.DockerClient.ContainerRemove(context.Background(), id, container.RemoveOptions{})
		if err != nil {
			oldTaskProgress.Message = "删除旧容器失败"
			oldTaskProgress.DetailMsg = err.Error()
			oldTaskProgress.IsDone = true
			serviceContext.UpdateProgress(taskID, oldTaskProgress)
			return err
		}
	}
	// 2026-11-06：清理被替换掉的旧镜像。默认开（设置页可关）。
	// ⚠️ 只删旧镜像、不 prune 全局 dangling —— 免得误删别的回滚版本；
	//    清理失败**不影响**本次更新的成败（函数内部只记日志并回一句话）。
	if GetSettings().CleanOldImageOn() {
		if ok, msg := CleanupReplacedImage(serviceContext, oldImageID, imageNameAndTag); ok {
			logx.Infof("容器 %s 更新完成，%s", name, msg)
		} else {
			logx.Infof("容器 %s 更新完成，旧镜像未清理：%s", name, msg)
		}
	}

	oldTaskProgress.Message = "更新成功"
	oldTaskProgress.DetailMsg = "更新成功"
	oldTaskProgress.Percentage = 100
	oldTaskProgress.IsDone = true
	serviceContext.UpdateProgress(taskID, oldTaskProgress)
	return nil
}

func decodePullResp(reader io.Reader, ctx *svc.ServiceContext, taskID string) (err error) {
	decoder := json.NewDecoder(reader)
	var oldTaskProgress, result = ctx.GetProgress(taskID)
	if !result {
		oldTaskProgress = svc.TaskProgress{
			Percentage: 0,
			Name:       "",
			Message:    "",
			DetailMsg:  "",
			IsDone:     false,
		}
	}
	for {
		var msg dockerMsgType.JSONMessage
		if err = decoder.Decode(&msg); err != nil {
			if err == io.EOF {
				return nil
			}
			oldTaskProgress.Message = "拉取镜像失败"
			oldTaskProgress.DetailMsg = err.Error()
			oldTaskProgress.Percentage = 25
			oldTaskProgress.IsDone = true
			ctx.UpdateProgress(taskID, oldTaskProgress)
			logx.Errorf("Failed to decode pull image response: %s", err)
			return fmt.Errorf("拉取镜像失败: %w", err)
		}
		// Print the progress or error information from the response
		if msg.Error != nil {
			oldTaskProgress.Message = "拉取镜像失败"
			oldTaskProgress.DetailMsg = msg.Error.Error()
			oldTaskProgress.Percentage = 25
			oldTaskProgress.IsDone = true
			ctx.UpdateProgress(taskID, oldTaskProgress)
			logx.Errorf("Error: %s", msg.Error)
			return fmt.Errorf("拉取镜像失败: %w", msg.Error)
		} else {
			var formattedMsg string
			if msg.Progress != nil {
				formattedMsg = fmt.Sprintf("进度%s: %s", msg.Status, msg.Progress.String())
			} else {
				formattedMsg = fmt.Sprintf("进度%s", msg.Status)
			}
			oldTaskProgress.DetailMsg = formattedMsg
			oldTaskProgress.Percentage = 25
			ctx.UpdateProgress(taskID, oldTaskProgress)
			logx.Infof("拉取镜像进度\t %s: %s\n", msg.Status, msg.Progress)
		}
	}
}

/* registryAuthForPull 把 dc 自己读到的 docker 凭据转成 Docker API 要的
   X-Registry-Auth 形态：**base64url(JSON{username,password,serveraddress})**。

   ⚠️ module.AuthForWithSource 返回的是 base64(user:pass)（检测路径拿它拼 Basic 头），
      两种编码不通用：直接塞给 ImagePull 会被 daemon 当成"没有凭据"。
   ⚠️ 取不到 / 解不开就返回空串，让调用方走匿名拉取（与改动前行为一致，不引入新失败面）。 */
func registryAuthForPull(imageNameAndTag string) string {
	auth, _ := module.AuthForWithSource(imageNameAndTag)
	if auth == "" {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(auth)
	if err != nil {
		return ""
	}
	user, pass, ok := strings.Cut(string(raw), ":")
	if !ok || user == "" {
		return ""
	}
	cfg, err := json.Marshal(map[string]string{
		"username":      user,
		"password":      pass,
		"serveraddress": "https://index.docker.io/v1/",
	})
	if err != nil {
		return ""
	}
	return base64.URLEncoding.EncodeToString(cfg)
}
