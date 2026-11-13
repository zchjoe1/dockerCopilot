# dc-ts — Docker Copilot 的 TypeScript 重写

不用 Go 的版本：**Node 22 + Hono + dockerode** 服务端 + **Vite + React** 前端，单进程同时提供 `/api` 与页面。

```
容器控制墙 · 右侧 inspector · 手机端底部抽屉
镜像页（搜索/删除/打标签） · 备份页（导出/恢复/删除）
Compose 页（扫描/编辑/保存/一键生效） · 设置页（优先名单、快慢 cron、清旧镜像）
```

## 快速开始

```bash
cd ts
npm install

# 假数据（不需要 docker，前端联调/演示）
MOCK=1 npm run dev            # http://127.0.0.1:3080

# 真模式（需要 /var/run/docker.sock）
npm run build
node dist-server/index.mjs    # 默认 3080
```

## 校验

```bash
npm test                      # 39 项 node --test（API / 更新流程 / 备份 / compose / 图标）
npm run ui:check              # 1440·1024·390 三档截图 + 断言（需要 CDP 浏览器）
```

`ui:check` 会断言：五个页面在三个视口都存在关键元素、没有横向溢出、手机端点格子能弹出详情抽屉、桌面端 inspector 常驻；截图落在 `ts/shots/`。

## 已实现（与旧 Go 版对齐的部分）

| 能力 | 接口 |
|---|---|
| 容器列表（CPU/内存 10s 采样） | `GET /api/containers` |
| 容器详情 | `GET /api/containers/:id` |
| 启停 / 重启 | `POST /api/containers/:id/{start\|stop\|restart}` |
| 重命名 | `POST /api/containers/:id/rename` |
| 更新：digest 检测 → 拉取 → 原配置重建 → 回滚 | `POST /api/containers/:id/update` + `GET /api/jobs/:id` |
| 更新检测 | `GET /api/updates`（10 分钟缓存） |
| 镜像：列表 / 删除 / 打标签 | `GET /api/images`、`DELETE /api/images/:id`、`POST /api/images/tag` |
| 备份：导出 / 列表 / 恢复 / 删除 | `/api/backups*` |
| Compose：扫描 / 读 / 存 / 生效 | `/api/compose*` |
| 设置：读 / 写（cron 校验、原子落盘） | `/api/settings` |
| 图标：上传 / 读取 / 删除 | `/api/icons*` |

## 与旧版的差异（有意为之）

- **认证**：旧版已取消 JWT，这里同样没有登录。
- **备份格式**：旧版导出 compose；这里导出容器 inspect 的 JSON（能完整还原网络/挂载/环境变量，恢复为一个新容器）。
- **更新流程**：旧版有加速站回退与「强制 IPv4」；这里默认直连 registry，失败即判定为「digest 不可得」，不做加速站轮询。私有仓库尚未接 docker authconfig。
- **采样**：旧版在 Go 里并发拉 stats；这里 Node 侧 4 路并发、失败容器拉黑。

## 部署

```bash
docker build -t dc-ts:dev .
docker run -d --name dc-ts --network host --restart unless-stopped \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v /vol1/1000/docker/deepseek-harness/data/dc-ts:/app/data \
  -e PORT=3081 -e COMPOSE_ROOT=/vol1/1000/docker dc-ts:dev
```

与现有 `dockercopilot` 并行（不同端口），验证后再替换。CI：`.github/workflows/dc-ts-image.yml` 构建并推 `ghcr.io/<owner>/dc-ts:latest`。

## 与旧版接口对照（20 个旧接口 → 新实现）

| 旧接口 | 新接口 | 说明 |
|---|---|---|
| `GET /`（obtain） | `GET /api/icons` | 图标列表 |
| `POST /`（upload） | `POST /api/icons` | 图标上传 |
| `POST /auth` | — | 旧版已取消 JWT，不再需要 |
| `GET /containers` | `GET /api/containers` | 含 CPU/内存采样 |
| `POST /container/:id/start\|stop\|restart` | `POST /api/containers/:id/*` | |
| `POST /container/:id/rename` | `POST /api/containers/:id/rename` | |
| `POST /container/:id/update` | `POST /api/containers/:id/update` | 返回 jobId，可轮询进度 |
| `GET /container/backup` | `POST /api/backups/:id` | 备份容器配置 |
| `GET /container/backup2compose` | `GET /api/compose/export?container=` | 导出 compose 片段 |
| `GET /container/listBackups` | `GET /api/backups` | |
| `POST /container/backups/restore` | `POST /api/backups/:file/restore` | |
| `DELETE /container/backups` | `DELETE /api/backups/:file` | |
| `GET /images` | `GET /api/images` | |
| `DELETE /image/:id` | `DELETE /api/images/:id` | |
| `GET /progress/:taskid` | `GET /api/jobs/:id` | 更新任务进度 |
| `GET /version` | `GET /api/version` | |
| `PUT /program` | `PUT /api/program` | 容器部署下返回 501（改由更新镜像完成） |
| 本地新增 `/api/settings` | `GET/PUT /api/settings` | |
| 本地新增 `/api/compose*` | `GET/PUT/POST /api/compose*` | |
| 本地新增 `/api/image/tag` | `POST /api/images/tag` | |

## 已知缺口

- Compose「一键生效」依赖 **docker CLI**，当前镜像没内置，需在 Dockerfile 里加 `docker-cli` 或改用 compose 库。
- 私有镜像的 digest 检测需要 registry 凭据（未接）。
- 旧版的「程序自更新 / 版本信息」「镜像图标自动匹配」未做。
