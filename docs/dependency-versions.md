# 依赖与镜像版本

| 项目 | 固定版本/镜像 | 说明 |
| --- | --- | --- |
| Temporal Server | `temporalio/auto-setup:1.29.7` | Docker Hub 官方可用 tag；`1.32.2` 的 auto-setup tag 不存在 |
| Temporal UI | `temporalio/ui:2.56.4` | 与本机 Temporal CLI 报告的 UI `2.56.4` 对齐 |
| PostgreSQL | `postgres:16.10-bookworm` | 数据目录 `/var/lib/postgresql/data` |
| Node 构建镜像 | `node:24.16.0-bookworm-slim` | WebUI 构建 |
| Go 构建镜像 | `golang:1.26.0-bookworm` | 与 `go.mod` 的 Go `1.26.0` 对齐 |
| 运行时镜像 | `gcr.io/distroless/static-debian12:nonroot` | 非 root 最小运行镜像 |

## 初始化方式

- `temporalio/auto-setup:1.29.7` 负责 Temporal PostgreSQL schema 初始化。
- `temporal-db-bootstrap` 在 PostgreSQL 健康后幂等创建 `temporal` 与 `temporal_visibility` 数据库。
- 应用镜像执行 `platform bootstrap`，幂等设置 namespace `default` 的 90 天保留期和 `PlatformPendingApproval` Search Attribute。
- `app-api` 和 `app-worker` 在 bootstrap 成功完成后启动。

## 验证状态

- `docker compose --env-file .env.example -f deploy/compose.yaml config` 已通过。
- 本次未拉取镜像或实际启动 Compose；由使用者在本机执行 `make up` 验证。
