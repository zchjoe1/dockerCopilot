package container

import (
	"context"
	"github.com/onlyLTY/dockerCopilot/internal/utiles"
	"time"

	"github.com/onlyLTY/dockerCopilot/internal/svc"
	"github.com/onlyLTY/dockerCopilot/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ContainersListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

type Info struct {
	Id          string `json:"id"`
	Status      string `json:"status"`
	Name        string `json:"name"`
	UsingImage  string `json:"usingImage"`
	CreateImage string `json:"createImage"`
	CreateTime  string `json:"createTime"`
	RunningTime string `json:"runningTime"`
	HaveUpdate  bool   `json:"haveUpdate"`

	// 【本地新增 2026-10-07】资源占用，取自后台采样缓存（utiles/containerstats.go）。
	// 用指针：没有数据时序列化成 null，前端据此不显示这一段，
	// 这样「容器已停止 / 还没采到」和「CPU 真的是 0%」在界面上不会混淆。
	CPUPercent *float64 `json:"cpuPercent"`
	MemUsed    *uint64  `json:"memUsed"`
	MemLimit   *uint64  `json:"memLimit"`
	MemPercent *float64 `json:"memPercent"`
}

func NewContainersListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ContainersListLogic {
	return &ContainersListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ContainersListLogic) ContainersList() (resp *types.Resp, err error) {
	// 获取所有容器（包括停止的容器）
	resp = &types.Resp{}
	list, err := utiles.GetContainerList(l.svcCtx)
	if err != nil {
		resp.Code = 500
		resp.Msg = err.Error()
		resp.Data = map[string]interface{}{}
		return resp, err
	}
	resp.Msg = "success"
	var containerInfoList []Info
	list = utiles.CheckImageUpdate(l.svcCtx, list)
	for _, v := range list {
		var containerInfo Info
		containerInfo.Id = v.ID
		containerInfo.Status = v.State
		if len(v.Names) > 0 {
			ContainerName := v.Names[0][1:]
			containerInfo.Name = ContainerName
		} else {
			containerInfo.Name = "get container name error"
			l.Error("get container name error" + v.ID)
		}
		if v.Image != "" {
			containerInfo.UsingImage = v.Image
		} else {
			containerInfo.UsingImage = v.ImageID
			l.Error("image dont have name" + v.ID)
		}
		containerInspect, err := utiles.GetContainerInspect(l.svcCtx, v.ID)
		if err != nil {
			containerInfo.CreateImage = ""
			l.Error("get image name error" + v.ID)
		}
		containerInfo.CreateImage = containerInspect.Config.Image
		t := time.Unix(v.Created, 0)
		containerInfo.CreateTime = t.Format("2006-01-02 15:04:05")
		containerInfo.RunningTime = v.Status
		containerInfo.HaveUpdate = v.Update
		// 【本地新增 2026-10-07】带上资源占用。读的是后台采样缓存，不发起 Docker 调用，
		// 所以列表接口的耗时不受影响。采样器只采运行中的容器，停止的自然取不到。
		if v.State == "running" {
			if st, ok := utiles.GetContainerStats(v.ID); ok {
				cpu, mem, limit, memPct := st.CPUPercent, st.MemUsed, st.MemLimit, st.MemPercent
				containerInfo.CPUPercent = &cpu
				containerInfo.MemUsed = &mem
				containerInfo.MemLimit = &limit
				containerInfo.MemPercent = &memPct
			}
		}
		containerInfoList = append(containerInfoList, containerInfo)
	}
	resp.Data = containerInfoList
	return resp, nil
}
