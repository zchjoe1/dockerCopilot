package utiles

import (
	"context"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/onlyLTY/dockerCopilot/internal/svc"
	"github.com/zeromicro/go-zero/core/logx"
)

// CleanupReplacedImage —— 更新容器后删掉**被替换的那个旧镜像**（2026-11-06）
//
// 【为什么需要】
// docker pull 拉到新镜像后，旧镜像只是被摘掉 tag 变成 `<none>:<none>`（dangling），
// 一直占着磁盘。`UpdateContainer` 原来只删旧容器，从来不碰旧镜像 ——
// 每更新一次就多留一份，NAS 上几百 MB~几 GB 地攒。
//
// 【为什么不用 docker image prune】
// “删所有 dangling”会顺手删掉**别人**的旧镜像（比如你手动留着的回滚版本）。
// 这里只删**本次更新前那个容器用的那个 image ID**，范围最小。
//
// 【三道闸门】任何一道不满足就跳过，且**只记日志、不返回错误**
// （清理失败不该让一次成功的更新显示成失败）：
//  1. 旧 image ID 为空或不是 sha256: 开头（信息不可信，宁可不删）；
//  2. 新旧 image ID 相同（本次其实没换镜像，删了就是自伤）；
//  3. 还有容器在引用这个 image ID（含已停止的容器 —— 那往往是回滚目标）。
//
// 返回 (是否删除, 说明)。说明会写进任务进度与日志，便于事后追。
func CleanupReplacedImage(ctx *svc.ServiceContext, oldImageID string, newImageRef string) (bool, string) {
	oldImageID = strings.TrimSpace(oldImageID)

	// 取新镜像的真实 ID（newImageRef 是 name:tag，不是 ID）
	newImageID := ""
	if insp, _, err := ctx.DockerClient.ImageInspectWithRaw(context.Background(), newImageRef); err == nil {
		newImageID = insp.ID
	}

	// 还有谁在用这个镜像（All:true = 含已停止的容器）
	var users []string
	list, err := ctx.DockerClient.ContainerList(context.Background(), container.ListOptions{All: true})
	if err != nil {
		return false, "查询容器列表失败，跳过清理：" + err.Error()
	}
	for _, c := range list {
		if c.ImageID == oldImageID {
			users = append(users, containerNameOf(c))
		}
	}

	// 三道闸门集中判断（纯函数，可单测）
	if ok, reason := cleanupDecision(oldImageID, newImageID, users); !ok {
		return false, reason
	}

	// ⛔ Force=false：即使前面判断有漏，Docker 自己也会拒绝删除仍被引用的镜像，
	//    这是最后一道保险 —— 宁可删不掉，也不要误删。
	//    PruneChildren=true：同时清掉只属于这个镜像的中间层。
	_, err = ctx.DockerClient.ImageRemove(context.Background(), oldImageID, image.RemoveOptions{
		Force:         false,
		PruneChildren: true,
	})
	if err != nil {
		logx.Errorf("[cleanup] 删除旧镜像 %s 失败：%v", shortID(oldImageID), err)
		return false, "删除旧镜像失败：" + err.Error()
	}
	logx.Infof("[cleanup] 已删除被替换的旧镜像 %s", shortID(oldImageID))
	return true, "已删除旧镜像 " + shortID(oldImageID)
}

// cleanupDecision 是"到底删不删"的唯一判断点 —— 提成纯函数，便于单测。
// 返回 (proceed, reason)：proceed=false 时 reason 说明为什么跳过。
func cleanupDecision(oldImageID, newImageID string, users []string) (bool, string) {
	if !strings.HasPrefix(oldImageID, "sha256:") || len(oldImageID) <= len("sha256:") {
		return false, "旧镜像 ID 不可用，跳过清理"
	}
	if newImageID != "" && newImageID == oldImageID {
		return false, "新旧镜像相同，无需清理"
	}
	if len(users) > 0 {
		return false, "旧镜像仍被容器 " + strings.Join(users, "、") + " 使用，跳过清理"
	}
	return true, ""
}

func containerNameOf(c types.Container) string {
	if len(c.Names) > 0 {
		return strings.TrimPrefix(c.Names[0], "/")
	}
	return c.ID[:12]
}

// shortID 把 sha256:abcdef... 压成 12 位，日志里读得下去。
func shortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
