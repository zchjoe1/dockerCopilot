# AGENTS.md — dc-ts

TypeScript 重写版 Docker 管理面板。**不是 Go 版的替代仓库**：Go 代码与 `front/` 原样保留，本目录独立构建、独立镜像。

## 结构

```
ts/
├─ server/        Node 22 + Hono + dockerode
│  ├─ app.ts      路由（createApp，可注入 mock，便于测试）
│  ├─ index.ts    监听 + 采样循环 + 更新检测调度
│  ├─ docker.ts   容器列表 / stats / inspect / 启停 / 改名
│  ├─ update.ts   拉取→重建→切换→回滚
│  ├─ updates.ts  digest 检测（10 分钟缓存）
│  ├─ registry.ts 引用解析 + 401 取 token + 远端 digest
│  ├─ images.ts   镜像列表 / 删除 / 打标签
│  ├─ backups.ts  容器配置导出 JSON / 恢复 / 删除
│  ├─ compose.ts  扫描 /vol1/1000/docker、读写、docker compose up -d
│  ├─ settings.ts 读写 data/settings.json（校验 + 原子落盘）
│  ├─ icons.ts    图标上传（≤2MB，png/jpg/webp/gif/svg）
│  └─ mock.ts     MOCK=1 的全量假数据（与真实数据同形）
├─ ui/            Vite + React 18，无 UI 组件库
│  ├─ src/App.tsx         hash 路由 + 侧边轨
│  ├─ src/pages/          容器 / 镜像 / 备份 / Compose / 设置
│  └─ src/styles.css      全部样式与设计变量
├─ tests/         node --test（API 与更新流程）
├─ scripts/ui-check.mjs   CDP 多视口验收
└─ Dockerfile
```

## 命令（全部在 `ts/` 下执行）

```bash
npm install
MOCK=1 npm run dev          # 3080 起 mock 服务（不需要 docker）
npm run build               # 构建前端(dist) + 服务端(dist-server)
npm test                    # node --test，36+ 项
npm run ui:check            # 自己起 mock + 1440/1024/390 三档截图与断言，需要 CDP 浏览器
docker build -t dc-ts:dev .
```

## 约定

- **不加 UI 组件库**。样式只写在 `ui/src/styles.css`，颜色/字号用 `:root` 变量；新颜色先加变量。
- **接口错误统一** `{ "error": "..." }` + 合适的状态码；成功返回 `{ ok: true, ... }`。
- **每个新接口必须有 `tests/` 里的用例**，并且能在 `MOCK=1` 下跑（不依赖真实 docker）。
- **改 UI 必跑 `npm run ui:check`**：它会在三档视口断言「关键元素存在 / 无横向溢出 / 手机端点格子能开抽屉」。
- 需要真实 docker 的行为（stats、pull、重建、compose）在 MOCK 下只走分支；不要为了过测试伪造成功。
- 数据目录由 `DATA_DIR` 决定（默认 `./data`）：`settings.json`、`backups/`、`icons/` 都在这里。

## 已知边界

- 这个容器里没有 `go`、没有 `docker`、没有 `/var/run/docker.sock`：本地只能构建前端与跑 mock/单测，**真实联调必须在 NAS**（起 `dc-ts` 容器挂 socket）。
- Compose 的「一键生效」调用宿主 `docker compose` CLI；镜像里需带 docker CLI（Dockerfile 未内置，见 README 的后续项）。
- 更新检测对私有仓库需要 registry 凭据，当前未接 docker authconfig，私有镜像会显示「digest 不可得」。
