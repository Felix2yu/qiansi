# 牵丝 (Qiansi)

[![测试与构建](https://github.com/Felix2yu/qiansi/actions/workflows/build.yml/badge.svg)](https://github.com/Felix2yu/qiansi/actions/workflows/build.yml)
[![codecov](https://codecov.io/gh/Felix2yu/qiansi/branch/main/graph/badge.svg)](https://codecov.io/gh/Felix2yu/qiansi)

自托管的人情往来与关系管理应用。记录身边的人、发生过的事、欠着的人情——单个 Go 二进制 + 内嵌 SQLite，开箱即用。

## 功能

- **人物档案**：联系人（昵称/电话/微信/生日/位置/备注/分级/分类/标签），支持自定义字段
- **vCard 导入导出**：兼容 macOS 联系人.app（vCard 2.1/3.0/4.0，QP 编码中文、GBK、`itemN`/`X-ABLabel` 分组标签、`X-SOCIALPROFILE` 微信）；导出携带 `X-ABUID`，导入回联系人.app 可合并更新而非重复新建
- **事件记录**：自定义事件类型，记录与他人相关的往来事件
- **人情账本**：借还记录（精确到分）、还款流水
- **备忘与提醒**：备忘（可设截止日）、提醒（后台调度）、纪念日（支持农历）
- **关系图谱**：人物关系网络可视化、亲密度分析、词云
- **统计看板**：今日待办、月度趋势、分级分布、智能建议

## 快速开始

镜像已发布至 GitHub Container Registry（amd64 / arm64 双架构）：

```yaml
services:
  qiansi:
    image: ghcr.io/felix2yu/qiansi:latest
    container_name: qiansi
    restart: unless-stopped
    ports:
      - "127.0.0.1:8080:8080"
    volumes:
      - ./data:/app/data
    environment:
      QIANSI_ADDR: ":8080"
      QIANSI_DATA_DIR: "/app/data"
```

```bash
docker compose up -d
```

浏览器访问 `http://127.0.0.1:8080`。端口绑定了 `127.0.0.1`，如需局域网访问请自行调整为 `8080:8080` 并自行加反代/鉴权。

### 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| `QIANSI_ADDR` | `:8080` | HTTP 监听地址 |
| `QIANSI_DATA_DIR` | `data` | 数据目录（数据库、上传文件；镜像内已固定为 `/app/data`） |
| `QIANSI_WEB_DIR` | — | 前端静态文件目录（镜像内已固定为 `/app/web/dist`，一般无需设置） |
| `QIANSI_TOKEN` | — | 访问令牌。设置后 `/api/` 与 `/uploads/` 需要 `Authorization: Bearer <token>`；不设则放行 |
| `QIANSI_CORS_ORIGINS` | — | 允许跨域的来源，逗号分隔。默认不返回 CORS 头（仅同源可用） |

> 默认配置面向「本机单用户」：不鉴权、不跨域。若要暴露到局域网或公网，请设置 `QIANSI_TOKEN` 并自行加反向代理与 HTTPS。

## 数据与备份

所有状态都在挂载的数据目录里（容器内 `/app/data`，由 `QIANSI_DATA_DIR` 指定），备份即备份该目录：

```
/app/data/          # 容器内数据目录；绑定挂载时对应宿主的 ./data
├── qiansi.db        # SQLite 数据库（WAL 模式，含 -wal/-shm 边车文件）
├── uploads/         # 上传的图片附件（头像等）
└── backups/         # 归档快照（每日 04:00 自动一份，最多保留 7 份）
```

- 目录在启动时自动创建，无需手动初始化
- 设置页「导出数据库」会生成一份一致性快照（等同于在线热备份），「从备份恢复」会先把当前数据归档再覆盖
- 冷备份：`docker compose stop` 后直接拷贝 `qiansi.db`（连同 `-wal`/`-shm`，如有）

### 命令行导出：`qiansi backup`

热拷贝正在写入的 `qiansi.db` 不是原子操作，可能拷出损坏的副本或漏掉 `-wal` 里已提交的数据。
因此导出由应用自身完成：`backup` 子命令用 `VACUUM INTO` 生成事务级一致快照写到 stdout，
不跑迁移、不建目录、不改源库（库文件不存在时直接报错，绝不交出一份空备份）。
外部备份工具（docker-db-auto-backup）正是经 `docker exec` 采集这段 stdout。

```bash
# db 形态（默认）：只有数据库
docker exec qiansi /app/qiansi backup > qiansi-$(date +%F).db

# zip 形态：数据库 + uploads 图片
docker exec qiansi /app/qiansi backup --format zip > qiansi-$(date +%F).zip
```

> 镜像里的二进制在 `/app/qiansi`，而 `/app` 不在 `PATH` 上，所以 `docker exec` 要写绝对路径。

| 形态 | 内容 | 是否完整备份 | 恢复 |
|---|---|---|---|
| `db`（默认） | 仅 `qiansi.db` | **不完整**：`uploads/` 里的头像等图片附件不在库内，需另行备份 | 把文件放回 `/app/data` 卷（修好属主）重启容器 |
| `zip` | `qiansi.db` + `uploads/<原目录结构>` | 完整：库与库外文件一次带走 | 解包后 `.db` 放回 `/app/data`、`uploads/` 放回 `/app/data/uploads`，重启容器 |

- 默认是 `db`，因为备份工具的 SQLite 产物默认按 `.db` 与 SQLite 魔数校验；
  用 `zip` 必须同时声明 `BACKUP_SQLITE_EXT: "zip"`，否则校验失败、该轮备份标记为失败（不静默存坏备份）。
- 应用没有导入 zip 的入口（`/api/v1/import/json` 只吃 JSON、`/api/v1/backup/restore` 只吃单个 `.db`），
  所以 zip 走上面那条「解包放回」的路子。
- 只想要一份完整档案又不想动环境变量：直接定期跑 `--format zip` 重定向到数据目录之外即可。

日志一律走 stderr，stdout 只有产物字节，因此上面的重定向不需要额外处理。

## 本地开发

依赖：Go 1.27+、Node 26+（构建前端）。

```bash
# 前端
cd web && pnpm install && pnpm exec vite build && cd ..

# 启动（自动建库、跑 migration，从 web/dist 提供 SPA）
go run ./cmd/server
```

- 后端接口统一挂在 `/api/v1/` 下，健康检查：`GET /api/v1/health`
- 测试：`go test ./...`
- 覆盖率：`./scripts/coverage.sh`（产出 `coverage.out` 与 `coverage.xml`，低于 80% 时脚本非零退出，与 codecov 的项目状态检查同一条线）
- 仓库根目录自带的 `docker-compose.yml` 使用 `build: .` 从源码构建镜像；想直接用预构建镜像请替换为上面的 `image:` 写法

## 技术栈

Go（chi 路由，CGO 关闭）· SQLite（WAL）· Svelte + Vite + Tailwind CSS · 运行镜像基于 distroless/static

## License

[MIT](LICENSE)
