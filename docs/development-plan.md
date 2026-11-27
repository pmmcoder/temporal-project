# 个人工作流平台开发计划

| 项目 | 内容 |
| --- | --- |
| 文档版本 | 0.1 |
| 更新日期 | 2026-11-20 |
| 需求依据 | [需求文档](requirements.md) |
| 架构依据 | [架构设计](architecture.md) |

## 1. 实施原则

- 按可运行的纵向切片交付，每一阶段都包含实现、自动化测试和运行验证。
- 先完成不依赖外部飞书能力和镜像版本的平台核心，再推进部署与审批通道。
- 测试只面向公开 Interface：`config`、`sdk.Registry`、`catalog`、`runs` 和 HTTP API。
- Workflow 代码与平台基础设施分模块，业务代码只能依赖 `platform/sdk`。
- 不在页面、API 和 Worker 中重复定义 Workflow Type、Task Queue 或 Signal 名称。

## 2. 阶段计划

### M0 基础骨架

目标：建立可编译、可测试、可启动的平台核心。

- [x] 将 Go 模块迁移为正式模块路径。
- [x] 建立 `cmd/platform`、`platform/`、`app/` 目录；`web/` 在 M1 建立。
- [x] 实现环境配置加载和启动校验。
- [x] 实现结构化日志、HTTP 请求 ID 和敏感字段脱敏。
- [x] 实现 SDK 显式注册、签名校验、Schema 生成和重复注册检测。
- [x] 实现 Catalog 只读视图。
- [x] 将现有问候示例迁移到 `app/greeting`。
- [x] 实现 `platform worker` 注册和优雅退出。
- [x] 实现 `platform serve` 的健康检查、目录 API 和启动 API。
- [x] 为上述公开 Interface 编写单元测试。

完成标准：`go test ./...` 通过，`platform worker` 可注册示例 Workflow，`platform serve` 可返回目录和健康状态。

### M1 控制面

目标：打通目录、输入校验、启动、运行查询、取消与基础 WebUI。

- [x] 实现 Runs 服务：启动、幂等、运行查询、状态映射、详情、取消和 Memo 输入摘要。
- [x] 实现 `/api/v1/workflows`、启动、运行列表、运行详情和取消 API。
- [x] 实现统一错误结构和本地 Host/Origin/Content-Type 安全中间件。
- [x] 创建 React、TypeScript、Vite WebUI。
- [x] 实现目录、JSON 启动表单、运行列表和运行详情页面。
- [x] 使用 `go:embed` 打包前端产物。
- [x] 增加 API、Runs 和前端关键交互测试。

完成标准：通过控制台启动示例 Workflow，能查询状态、结果并取消运行。

### M2 基础设施与部署

前置：`OPEN-002` 已关闭，版本见 [dependency-versions.md](dependency-versions.md)。

- [x] 编写 Dockerfile 和 Makefile。
- [x] 编写 PostgreSQL、Temporal、Temporal UI、API、Worker Compose。
- [x] 实现 namespace 90 天保留期和 Search Attribute 初始化。
- [x] 实现 Volume 持久化、健康检查、重启策略和备份命令。
- [x] 增加 `make up`、`make deploy`、`make backup` 和 `make smoke`。

完成标准：全新环境一条命令启动，重启后运行数据保留。

### M3 人工确认

目标：先完成不依赖飞书通道的审批核心与控制台待办。

- [x] 实现 `sdk/approval` 的确定性 Workflow 内审批流程。
- [x] 实现审批 Memo、Search Attribute、Signal 去重和超时。
- [x] 实现控制台待办查询和审批决定 API。
- [x] 实现审批示例 Workflow 和测试脚手架审批 mock。
- [x] 关闭 `OPEN-001` 后实现飞书发卡、更新卡片、长连接回调和白名单；真实企业租户通过路径已验证。
- [x] 实现通知函数和飞书配置校验；真实飞书通知发送待联调。

完成标准：控制台和飞书均可完成审批，重复决定只生效一次。

### M4 调度与加固

- [x] 实现 Temporal Schedule 业务封装、创建/列表/暂停/恢复/删除 API。
- [x] 实现定时任务页面、编辑、补跑窗口和需复核标记。
- [x] 增加运行分页、筛选、时间线摘要和 Activity 重试摘要。
- [x] 增加 Workflow 确定性检查、历史重放和 Compose 冒烟测试。
- [ ] 完成备份恢复、异常场景和发布验收；备份、临时库恢复和保留 Volume 重启已完成，飞书真机发布验收待执行。

完成标准：需求文档第 12 节全部验收项通过。

## 3. 当前进度

### 2026-11-20 实施记录

- `go test ./...` 通过。
- 已使用本机 Temporal dev server 验证目录查询、幂等启动、并行子 Workflow 和 Activity 执行。
- 示例 `greeting` 的最终结果为 `["Hello Alice","Hello Bob"]`。
- M1 控制面已打通：运行列表、运行详情、最终输出、取消接口、Temporal UI 深链接和安全中间件均已实现。
- `npm test`、`npm run build` 通过；Playwright 使用本机 Chrome 完成桌面、移动端、目录、运行列表和运行详情冒烟验证。
- M3 控制台审批已打通：`approvaldemo` 发起审批、待办 API 查询、WebUI 待办页通过/拒绝和 Workflow 恢复执行均已完成真实验证。
- M3 测试脚手架已加入 `app/testkit`，可预注册全部 Workflow 并自动批准/拒绝审批。
- Workflow 侧通知 Interface `notify.Send` 和平台 Activity `platform.feishu.SendMessage` 已加入；未配置飞书时返回不可重试的明确错误。
- M4 Schedule 主链路已真实验证：创建 `0 9 * * *` 任务、列表回显名称/Cron/时区/下次执行时间，以及暂停和恢复。
- M3 飞书通道代码已完成：卡片 JSON 2.0、表单备注、发卡、PATCH 终态更新、长连接 `card.action.trigger`、审批人白名单和 3 秒内 Signal 转交。
- `OPEN-001` 已根据官方文档和 SDK 源码关闭；真实企业租户的通过审批、卡片更新、操作人和渠道记录已完成验证。
- Compose 冒烟已覆盖目录、幂等启动、运行结果、控制台审批、Schedule 创建/暂停/恢复/删除。
- 已声明 Signal 的 Schema、API、WebUI 发送入口、运行时间线和 Activity 重试摘要已完成并纳入全量冒烟。
- Schedule 编辑和 `needsReview` 已完成，真实编辑后 Cron、名称、输入和下次执行时间均正确更新。
- 官方 `workflowcheck v0.6.0` 已加入 `make lint`，并修复审批 Memo 的 map 遍历确定性问题。
- 已导出真实 greeting 历史，`testkit.Replay` 使用原始 Workflow ID/Run ID 完成回放。
- 运行列表已支持 Workflow、状态和时间范围筛选。
- PostgreSQL 备份已生成，并在临时数据库完成恢复验证；`docker compose down` 后保留 Volume 重启，运行历史、Schedule 和审批数据均保留。
- 飞书审批卡片改版已按 PRD 完成，并通过真实飞书群、长连接回调和手机端操作验证；操作人与渠道正确记录为 Open ID 和 `feishu`。

## 4. 当前阻塞项

| 编号 | 影响阶段 | 处理方式 |
| --- | --- | --- |
| `OPEN-001` | M3 飞书通道 | 已关闭，研究结论见 [feishu-research.md](feishu-research.md) |
| `OPEN-002` | M2 部署 | 已关闭，版本见 [dependency-versions.md](dependency-versions.md) |
| `OPEN-003` | V1 发布 | 不阻塞开发 |

## 5. 首批纵向切片

1. 配置：合法配置加载、飞书凭据成对校验、默认值。
2. 注册表：显式名称、重复检测、签名检测、Schema 和示例输入。
3. 目录 API：返回与注册表一致的 Workflow 元数据。
4. Worker：注册示例 Workflow 和 Activity，并处理 SIGTERM。
5. WebUI：显示目录并通过 API 启动示例 Workflow。
