# dc-ts — Docker Copilot 的 TypeScript 重写

不用 Go 的版本：**Node 22 + Hono + dockerode**，前端 **Vite + React**，一套进程同时提供 `/api` 与静态页面。

```
ts/
├─ server/
│  ├─ index.ts    # Hono 路由 + 静态服务 + 采样循环
│  ├─ docker.ts   # dockerode：列表 / stats / inspect / 启停
│  └─ mock.ts     # MOCK=1 时的假数据（用于前端开发与截图）
├─ ui/
│  ├─ index.html
│  └─ src/{main.tsx, App.tsx, styles.css}
├─ Dockerfile
└─ vite.config.ts
```

## 跑起来

```bash
cd ts
npm install

# 1) 假数据模式（不需要 docker，前端联调用）
MOCK=1 npm run dev            # http://127.0.0.1:3080

# 2) 真模式（需要 /var/run/docker.sock）
npm run build
node dist-server/index.mjs    # 默认 3080
```

前端单独开发：`npm run dev:ui`（5173，`/api` 代理到 3080）。

## 已经有的

| 接口 | 说明 |
|---|---|
| `GET /api/health` | 模式与采样周期 |
| `GET /api/containers` | 容器列表（含 CPU% / 内存，按内存倒序） |
| `GET /api/containers/:id` | inspect 摘要（镜像 ID、重启策略、挂载） |
| `POST /api/containers/:id/start\|stop\|restart` | 启停与重启 |
| `GET /api/updates` | 占位，见下 |

- 采样：每 10 秒对运行中容器做一次非流式 `stats`，4 路并发，失败容器拉黑不再重试。
- 界面：控制墙（4×N 仪表格）+ 右侧 inspector + 底部采样带；选中跟着数据刷新。

## 还没移植的（重要）

1. **镜像更新检测与「拉取并重建」**：Go 版里的 registry digest 比对（私有仓库认证、加速站回退、强制 IPv4、更新后清旧镜像）没有搬过来。`POST /api/containers/:id/update` 现在返回 501，`hasUpdate` 在真实模式下恒为 false。
2. **Compose 项目读写**、**备份/恢复**、**图标上传**、**设置页（检测白名单/周期）**。
3. 前端只做了容器页；镜像 / 备份 / Compose / 设置 仍是空链接。

## 部署

```bash
docker build -t dc-ts:dev .
docker run -d --name dc-ts --network host \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e PORT=3081 dc-ts:dev
```

和现有 `dockercopilot` 并行跑（不同端口），验证后再决定是否替换。

## 为什么不是改写现有前端

现有 `front/` 是编译产物，仓库没有源码；公开的 `onlyLTY/dockerCopilotWeb` 停在 2025-06，缺 Compose / 设置 / 统计三块页面。所以这里按后端 API 契约重写。
