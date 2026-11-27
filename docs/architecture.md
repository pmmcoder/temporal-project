# 个人工作流平台架构设计

| 项目 | 内容 |
| --- | --- |
| 文档版本 | 1.0 |
| 文档状态 | V1 架构已确认；OPEN-001、OPEN-002 均已关闭 |
| 更新日期 | 2026-11-19 |
| 需求依据 | [需求文档](requirements.md) |

## 1. 目标

本文档将需求文档落实为可实现、可部署、可测试的架构，说明：

- 部署拓扑、进程角色与网络规则。
- 业务代码如何接入平台（Worker 运行框架）。
- 控制台 API 与 WebUI 的模块划分、数据流和接口。
- 飞书人工确认组件的 Workflow 内部机制与回调路径。
- 可观测性、测试策略，以及从当前示例代码迁移的步骤。

职责边界（控制台与 Temporal UI）以需求文档 8.9.1 为准，本文档不重复。

## 2. 架构决策

| 编号 | 决策 | 理由 |
| --- | --- | --- |
| ADR-001 | Temporal 使用独立 Server + PostgreSQL（持久化与 Visibility 共用一个 PostgreSQL 实例、两个库） | 升级路径明确，Visibility 支持自定义 Search Attribute 与条件查询 |
| ADR-002 | 业务代码与平台代码同仓库、同镜像；`serve` 与 `worker` 两个进程角色 | 目录在编译期确定，API 与 Worker 读取同一份定义；Worker 频繁重启不影响控制台和飞书回调 |
| ADR-003 | Workflow / Activity 使用显式注册名，Workflow Type 等于目录 ID | 重命名 Go 函数不破坏历史重放；目录与运行记录天然对应 |
| ADR-004 | 输入 Schema 由 Go 类型反射生成 | 避免手写 Schema 与结构体漂移 |
| ADR-005 | 运行级幂等使用确定性 Workflow ID + `REJECT_DUPLICATE` + Memo 中的输入摘要 | 无需应用数据库即可实现“相同键同输入返回已有、不同输入冲突、永不重复创建” |
| ADR-006 | 审批状态存在 Workflow 内部，通过 Memo 与 Search Attribute 对外可见；决定通过 Signal 送达 | Worker 离线时控制台仍能列出待办，决定仍能持久化 |
| ADR-007 | 飞书交互只用出站连接：长连接接收回调，OpenAPI 发送与更新卡片 | 满足“不开放公网入站端口” |
| ADR-008 | V1 不建立应用数据库 | Temporal 已覆盖全部状态，避免双写一致性问题 |
| ADR-009 | 前端 React + TypeScript + Vite，构建产物 `go:embed` 进二进制 | 运行镜像不需要 Node.js |

## 3. 系统上下文

```text
                    ┌──────────────── 本机浏览器 ────────────────┐
                    │                                           │
                    │ http://127.0.0.1:8080        http://127.0.0.1:8233
                    ▼                                           ▼
┌─────────────────────────────────┐                ┌──────────────────┐
│ app-api  (platform serve)       │                │ temporal-ui      │
│ WebUI + REST API + 飞书长连接    │                └────────┬─────────┘
└───────┬────────────────▲────────┘                         │
        │ gRPC           │ 出站 WebSocket                   │ gRPC
        │                │ （卡片回调）                      │
        ▼                │                                  ▼
┌──────────────────┐     │      ┌──────────────┐    ┌──────────────────┐
│ temporal :7233   │◄────┼──────│ 飞书开放平台  │    │ （同左 temporal） │
└───────┬──────────┘     │      └──────▲───────┘    └──────────────────┘
        │ Task Queue     └─────────────┤
        ▼                              │ 出站 HTTPS（发送/更新卡片、发消息）
┌──────────────────────────────┐       │
│ app-worker (platform worker) │───────┘
│ 业务 Workflow/Activity       │
│ 平台 Activity (platform.*)   │
└───────┬──────────────────────┘
        │
┌───────▼──────────┐
│ postgresql       │  （仅 temporal 访问）
└──────────────────┘
```

浏览器直接访问 Temporal UI；控制台只生成跳转链接，不代理 Temporal UI。

## 4. 部署拓扑

### 4.1 进程角色

应用构建为单一可执行文件 `platform`：

| 子命令 | 职责 | 容器 |
| --- | --- | --- |
| `platform serve` | 提供 WebUI 静态资源与 REST API；持有 Temporal Client；维持飞书长连接并把卡片回调转为 Signal；健康检查 | `app-api` |
| `platform worker` | 注册业务与平台 Workflow/Activity；轮询 Task Queue；内部健康端点 | `app-worker` |
| `platform bootstrap` | 创建或更新 namespace 保留期，注册平台 Search Attribute | `temporal-bootstrap` |

两个角色都调用同一个 `app.Register` 构建注册表：`worker` 用它注册到 Temporal Worker，`serve` 只读取其中的元数据（ID、说明、Schema、Signal 声明）。

隔离性：

- `app-api` 停止：执行中的 Workflow 不受影响；飞书回调暂时无法接收，卡片点击会在飞书侧提示失败，用户可稍后重试或改用控制台。
- `app-worker` 停止：控制台查询、待办列表、审批决定都可用；任务与 Signal 在 Temporal 中排队，Worker 恢复后继续。

### 4.2 Docker Compose 服务

| 服务 | 镜像 | 宿主机端口 | 依赖 | 说明 |
| --- | --- | --- | --- | --- |
| `postgresql` | `postgres:16.10-bookworm` | 不暴露 | — | 具名 Volume `pgdata`（见 4.2.1） |
| `temporal-db-bootstrap` | `postgres:16.10-bookworm` | 不暴露 | `postgresql` healthy | 幂等创建 `temporal`、`temporal_visibility` 数据库 |
| `temporal` | `temporalio/auto-setup:1.29.7` | 不暴露 `7233` | `postgresql` healthy | 自动初始化 PostgreSQL schema，并使用 PostgreSQL 持久化与 Visibility |
| `temporal-bootstrap` | 本项目镜像，`bootstrap` | 不暴露 | `temporal` 可用 | 一次性任务：创建或更新 namespace `default` 保留期为 90 天；注册 Search Attribute；全部幂等 |
| `temporal-ui` | `temporalio/ui:2.56.4` | `127.0.0.1:8233 → 8080` | `temporal` | 底层调试入口 |
| `app-api` | 本项目镜像，`serve` | `127.0.0.1:8080 → 8080` | `temporal-bootstrap` 成功完成 | — |
| `app-worker` | 本项目镜像，`worker` | 不暴露（内部 `8081`） | `temporal-bootstrap` 成功完成 | `stop_grace_period: 40s` |

版本选择见 [dependency-versions.md](dependency-versions.md)。所有常驻服务配置 `restart: unless-stopped`。

#### 4.2.1 持久化分层

持久化分两层，任何一层缺失都会导致数据丢失：

| 层 | 负责方 | 作用 |
| --- | --- | --- |
| 逻辑层 | Temporal → PostgreSQL | Workflow 状态、历史、Schedule、审批状态写入数据库，而非 Temporal 进程内存 |
| 物理层 | PostgreSQL → 具名 Volume `pgdata` | 数据库文件存放在独立于容器生命周期的存储上 |

- **只有 `postgresql` 有状态**。`temporal`、`temporal-ui`、`app-api`、`app-worker` 和 `temporal-bootstrap` 均不挂载数据卷，可随时重建。
- **为什么需要 Volume**：PostgreSQL 运行在容器中，未挂载具名 Volume 时数据文件位于容器可写层或匿名卷，`docker compose down` 后再 `up`、升级镜像或修改配置都会创建新容器并得到空库；`temporal` 会随之重新初始化，历史全部丢失。
- **选择具名 Volume 而非 bind mount**：macOS Docker Desktop 上具名 Volume 位于 Linux 虚拟机内，避免宿主机文件共享带来的性能与文件权限问题；备份通过 `make backup`（`pg_dump`）导出到宿主机目录，而不是直接拷贝数据文件。
- **挂载路径**：`postgres:16.10-bookworm` 固定挂载到 `/var/lib/postgresql/data`；挂载路径错误时数据会落在匿名卷中，等同未持久化。
- **验证**：`make smoke` 与验收第 11 条通过“写入数据 → `docker compose down` → `make up` → 数据仍在”验证物理层持久化。
- **数据删除**：只有 `docker compose down -v` 或手动删除 `pgdata` 会清空数据；README 中明确标注该命令的后果。
- **大版本升级**：PostgreSQL 大版本之间数据文件格式不兼容，不能直接更换镜像版本复用同一 Volume；须 `make backup` → 新建 Volume → 导入备份。

平台注册的 Search Attribute：

| 名称 | 类型 | 用途 |
| --- | --- | --- |
| `PlatformPendingApproval` | Bool | 标记运行中存在等待处理的审批，用于待办页查询 |

### 4.3 网络规则

- 容器之间使用服务名访问，例如 `temporal:7233`；容器内禁止使用 `127.0.0.1` 访问其他容器。
- 宿主机只能访问 `127.0.0.1:8080`（控制台）和 `127.0.0.1:8233`（Temporal UI）。
- `7233` 与 `5432` 默认不映射到宿主机。
- 开发模式使用 `compose.dev.yaml` 覆盖文件，仅额外映射 `127.0.0.1:7233:7233`，供宿主机运行的 Worker 连接（`make dev-worker`）。该文件默认不加载。
- 所有出站流量仅为飞书开放平台 HTTPS 与长连接。

### 4.4 运维命令

| 命令 | 作用 |
| --- | --- |
| `make up` | `docker compose up -d`，首次启动自动完成初始化 |
| `make down` | 停止全部服务，保留 Volume |
| `make deploy` | `docker compose up -d --build app-api app-worker`，业务代码变更后使用；两个容器同时重建，目录与注册保持一致 |
| `make dev-worker` | 加载 `compose.dev.yaml` 并在宿主机运行 Worker，用于调试 |
| `make logs` | 查看应用日志 |
| `make backup` | 使用 `pg_dump` 导出 `temporal` 与 `temporal_visibility` 到 `./backups/<时间戳>/` |
| `make test` / `make lint` / `make smoke` | 见第 15 节 |

恢复步骤写入 README：停止全部服务 → 删除 `pgdata` Volume → 仅启动 `postgresql` → 导入备份 → `make up`。升级 Temporal 或 PostgreSQL 版本前必须先 `make backup`。

## 5. 代码结构与依赖方向

### 5.1 目录

```text
cmd/platform/main.go          子命令 serve | worker
platform/
  config/                     环境变量解析与校验
  obs/                        slog 日志、请求 ID、脱敏
  sdk/                        业务代码唯一需要引用的包：注册、默认策略、错误、子流程 ID
    approval/                 人工确认组件（Workflow 侧）
    notify/                   通知函数（Workflow 侧）
    testkit/                  测试脚手架
  catalog/                    注册表的只读视图、Schema 生成与校验
  runtime/                    Worker 生命周期、平台 Activity 注册、健康端点
  feishu/                     飞书 OpenAPI 客户端、长连接、卡片模板
  runs/                       启动、查询、详情、取消、Signal、幂等、状态映射
  schedules/                  Temporal Schedule 业务封装
  approvals/                  待办查询与审批决定（serve 侧）
  webapi/                     HTTP 路由、请求校验、错误映射、安全中间件
  webui/                      go:embed 前端产物（dist/）
app/
  register.go                 func Register(r *sdk.Registry)
  greeting/                   示例：并行子流程
  approvaldemo/               示例：人工确认
web/                          React + TypeScript + Vite 源码
deploy/                       compose.yaml、compose.dev.yaml、初始化脚本
```

### 5.2 依赖规则

```text
cmd/platform ─► runtime, webapi, config, app
webapi ───────► runs, schedules, approvals, catalog, obs
runs ─────────► catalog, Temporal Client
schedules ────► catalog, Temporal Client
approvals ────► sdk/approval（共享类型）, feishu, Temporal Client
runtime ──────► sdk, catalog, feishu, Temporal Client/Worker
app ──────────► sdk, sdk/approval, sdk/notify（仅此三者）
```

禁止：

- `webapi` 导入 `go.temporal.io/sdk/client`。
- `app` 导入 `platform/` 下除 `sdk` 子树之外的任何包。
- `sdk/*` 在 Workflow 代码路径中进行任何 I/O。
- 业务代码使用 `platform.` 前缀的注册名（该前缀保留给平台）。

## 6. Worker 运行框架

### 6.1 注册入口

```go
// app/register.go
func Register(r *sdk.Registry) {
	greeting.Register(r)
	approvaldemo.Register(r)
}

// app/greeting/register.go
func Register(r *sdk.Registry) {
	sdk.Workflow(r, sdk.WorkflowDef[GreetIn, GreetOut]{
		ID:          "greeting",
		DisplayName: "问候",
		Description: "为每个名字并行启动子流程并汇总问候语",
		Fn:          GreetingWorkflow,
		Example:     GreetIn{Names: []string{"Alice", "Bob"}},
		Signals:     nil, // 可选：[]sdk.SignalDef{sdk.Signal[PauseIn]("pause")}
	})
	sdk.Workflow(r, sdk.WorkflowDef[GreetOneIn, string]{
		ID: "greeting.one", DisplayName: "单个问候", Fn: GreetOneWorkflow,
		Example: GreetOneIn{Name: "Alice"}, Internal: true, // 仅作子流程，不在目录中展示
	})
	sdk.Activity(r, sdk.ActivityDef{Name: ActGreet, Fn: Greet}) // 不传 Options 使用默认策略
}
```

约定：

- Workflow 函数签名固定为 `func(workflow.Context, In) (Out, error)`，`In` 必须是 struct。
- 注册名即 Workflow Type / Activity Type。Activity 名称以业务包名为前缀（如 `greeting.Greet`），定义为包内常量。
- 不兼容变更使用新 ID（如 `greeting.v2`）或 `workflow.GetVersion`。
- `Internal: true` 的定义只注册到 Worker，不在目录展示、不可从控制台直接启动。

### 6.2 启动校验

`sdk.Registry.Validate()` 在 `serve` 和 `worker` 启动时都执行，任一失败即退出：

- Workflow ID、Activity 名称重复；使用 `platform.` 前缀。
- Workflow 函数签名不符合约定。
- 由 `In` 生成的 JSON Schema 非法，或 `Example` 不通过该 Schema 校验。
- Signal 名称在同一 Workflow 内重复，或使用 `platform.` 前缀。

Schema 生成与校验库在实现时选定（候选：`invopop/jsonschema` 生成、`santhosh-tekuri/jsonschema` 校验），字段说明与约束通过 struct tag 表达。

### 6.3 Activity 默认策略与调用

```go
var DefaultActivityOptions = workflow.ActivityOptions{
	StartToCloseTimeout: 30 * time.Second,
	RetryPolicy: &temporal.RetryPolicy{
		InitialInterval:    1 * time.Second,
		BackoffCoefficient: 2,
		MaximumInterval:    1 * time.Minute,
		MaximumAttempts:    5,
		NonRetryableErrorTypes: []string{sdk.ErrTypeInvalidInput},
	},
	// 不设置 ScheduleToStartTimeout：Worker 离线期间任务保持排队。
	// 不设置 ScheduleToCloseTimeout：总时长由 MaximumAttempts 与退避间隔约束。
}
```

- 业务在 Workflow 中调用 `sdk.ExecuteActivity(ctx, ActGreet, in)`，框架按注册名查找该 Activity 注册时的选项（未提供则用默认值），返回 `workflow.Future`。
- 长时间运行的 Activity 在注册时覆盖 `StartToCloseTimeout` 并设置 `HeartbeatTimeout`，在实现中调用 `activity.RecordHeartbeat`，以便及时感知取消。
- `sdk.InvalidInput(err)` 返回类型为 `sdk.ErrTypeInvalidInput` 的 `ApplicationError`，不重试。
- 所有 `time.Duration` 必须带单位，禁止裸整数。

### 6.4 子 Workflow

- `sdk.ChildOptions(ctx, bizKey)` 返回 `WorkflowID = {父 Workflow ID}/{bizKey}`、Task Queue 继承父流程的子流程选项。
- 父 Workflow ID 在平台内唯一（见 7.2），因此子流程 ID 确定且不与其他父流程冲突；父流程 continue-as-new 后若复用同一 `bizKey`，前一个同名子流程已结束时允许重新创建。
- 业务代码负责保证同一父流程内 `bizKey` 唯一。

### 6.5 确定性约束

Workflow 代码禁止：系统时间（使用 `workflow.Now`）、随机数与 UUID（使用 `workflow.SideEffect`）、原生 goroutine / channel / select（使用 `workflow.Go` / `workflow.Channel` / `workflow.Selector`）、依赖 map 遍历顺序、全局可变状态、任何 I/O。`make lint` 运行 `workflowcheck` 静态检查。

### 6.6 Worker 生命周期（runtime）

- 连接 Temporal：失败时指数退避重试，不立即退出，以适应启动顺序抖动。
- 注册：业务定义 + 平台 Workflow 与 Activity（`platform.feishu.SendCard`、`platform.feishu.UpdateCard`、`platform.feishu.SendMessage`）。
- `worker.Options{WorkerStopTimeout: 30 * time.Second}`；SIGTERM 时停止轮询并等待进行中任务；Compose `stop_grace_period` 为 40 秒。
- 健康端点 `:8081/healthz`（仅容器内）：Worker 已启动且最近 30 秒内 `client.CheckHealth` 成功时返回 200。

## 7. 控制台：运行管理

### 7.1 启动数据流

```text
WebUI（表单实例生成 idempotencyKey）
  -> POST /api/v1/workflows/{id}/runs  {input, idempotencyKey}
  -> webapi：Host/Origin/Content-Type 校验，JSON 解析
  -> runs.Start
       1. catalog 查找定义（不存在或 Internal -> WORKFLOW_NOT_FOUND）
       2. 按 Schema 校验 input（失败 -> INVALID_INPUT + 字段错误）
       3. 规范化 input（键排序的紧凑 JSON），计算 SHA-256 = inputHash
       4. workflowID = {definitionId}:{idempotencyKey}（无键时 key 由 API 生成 UUID）
       5. StartWorkflow(ID=workflowID, TaskQueue, WorkflowIDReusePolicy=REJECT_DUPLICATE,
                        WorkflowExecutionErrorWhenAlreadyStarted=true, Memo{inputHash})
       6a. 成功 -> 201 {workflowId, runId, temporalUiUrl}
       6b. serviceerror.WorkflowExecutionAlreadyStarted
             -> Describe(workflowID) 读取 Memo.inputHash
             -> 相同：200 返回已有运行；不同：409 RUN_ALREADY_EXISTS
  -> Worker 从 Task Queue 拉取并执行
```

- API 在 Temporal 接受启动后立即返回，不等待完成。
- `idempotencyKey` 长度上限 64，字符集 `[A-Za-z0-9_-]`，否则 `INVALID_INPUT`。
- WebUI 在启动成功后为该表单生成新的 key；网络错误重试沿用原 key。
- 幂等有效期等于保留期 90 天；保留期之后同一 key 可再次创建运行。该限制在界面帮助文本中说明。

### 7.2 Workflow ID 规则

| 场景 | 规则 |
| --- | --- |
| 控制台 / API 启动 | `{definitionId}:{idempotencyKey}` |
| 定时任务启动 | Temporal 以 Schedule 的 Workflow ID 前缀 `{scheduleId}` 加时间戳生成 |
| 子 Workflow | `{父 Workflow ID}/{bizKey}`（见 6.4） |

### 7.3 运行列表

- 通过 `ListWorkflowExecutions` 查询 Visibility，查询条件由 `runs` 内部构造：
  - 按 Workflow：`WorkflowType = '{definitionId}'`
  - 按状态：`ExecutionStatus = '{status}'`
  - 按时间：`StartTime BETWEEN '{from}' AND '{to}'`
- 排序 `StartTime DESC`，每页 50 条，使用 `pageToken` 翻页。
- WorkflowType 不在当前目录中的运行显示原始 Type 并标记“未登记”。

### 7.4 状态映射

| Temporal `WorkflowExecutionStatus` | 业务状态 | 编码 |
| --- | --- | --- |
| Running | 运行中 | `running` |
| Completed | 成功 | `succeeded` |
| Failed | 失败 | `failed` |
| Canceled | 已取消 | `canceled` |
| Terminated | 已终止 | `terminated` |
| TimedOut | 超时 | `timed_out` |
| ContinuedAsNew | 已续接 | `continued` |

运行中且 `PlatformPendingApproval = true` 时，列表额外显示“待审批”标记。

### 7.5 运行详情

| 内容 | 来源 |
| --- | --- |
| 状态、时间、Memo | `DescribeWorkflowExecution` |
| 进行中 Activity 的类型、尝试次数、最近错误 | `DescribeWorkflowExecution.PendingActivities` |
| 输入 | History 第一个事件 `WorkflowExecutionStarted` |
| 输出或失败原因 | History 最后一个关闭事件 |
| 时间线摘要 | 服务端遍历 History，只提取 Activity 调度/完成/失败、子流程、Timer、Signal、审批相关事件，按时间合并为条目 |
| 待审批 | Memo `platformApprovals` |
| Temporal UI 链接 | `{TEMPORAL_UI_URL}/namespaces/{ns}/workflows/{workflowId}/{runId}` |

时间线摘要最多处理 2000 个事件，超出时标记“已截断”并引导跳转 Temporal UI。

### 7.6 取消、Signal 与再次运行

- 取消：`Describe` 确认状态为 Running 后调用 `CancelWorkflow`；不存在返回 `RUN_NOT_FOUND`，已结束返回 `INVALID_STATE`。取消是请求，最终状态取决于 Workflow 对取消的处理。
- Signal：只接受定义中声明的名称（否则 `SIGNAL_NOT_DEFINED`），载荷按 Signal Schema 校验后调用 `SignalWorkflow`；状态检查同取消。`platform.` 前缀的 Signal 不可通过该接口发送。
- 再次运行：前端读取运行详情中的输入，带入启动表单并生成新的 `idempotencyKey`，不提供专门接口。

## 8. 控制台：定时任务

`schedules` 封装 Temporal Schedule：

| 字段 | 映射 |
| --- | --- |
| 名称 | Schedule Memo `displayName` |
| Workflow | Action：`StartWorkflow`，Workflow Type = `definitionId`，Task Queue = 应用队列 |
| 输入 | Action 参数 |
| Cron 表达式、时区 | `ScheduleSpec.CronExpressions`、`ScheduleSpec.TimeZoneName` |
| 启用状态 | `State.Paused` |
| 重叠策略 | `Policy.Overlap`，默认 `SKIP` |
| 补跑窗口 | `Policy.CatchupWindow`，默认 1 小时 |
| 最近一次 / 下一次执行 | `Describe` 返回的 `RecentActions`、`NextActionTimes` |

- Schedule ID 为 `{definitionId}-{8 位随机串}`，由 API 生成。
- 保存时校验 Cron 与时区（解析失败 → `INVALID_INPUT`）及输入 Schema。
- 列表展示时，用当前目录 Schema 重新校验每个任务保存的输入，不通过则标记 `needsReview`；定义已不存在的任务同样标记。不自动修改任务。
- Temporal 不可达时返回 `TEMPORAL_UNAVAILABLE`；V1 不另存 Schedule 数据。

## 9. 飞书人工确认组件

### 9.1 业务侧接口

```go
res, err := approval.Request(ctx, approval.Req{
	Key:          "publish",                 // 同一运行内唯一
	Title:        "发布确认",
	Summary:      "**待发布**：...",          // Markdown
	Fields:       []approval.Field{{Label: "文章", Value: "..."}},
	AllowComment: true,
	Timeout:      24 * time.Hour,            // 必填，> 0
	OnTimeout:    approval.Reject,           // 或 approval.Fail
})
// res.Decision: approval.Approved | approval.Rejected | approval.TimedOut
// res.Comment, res.Operator, res.Channel ("feishu" | "web"), res.DecidedAt
```

`OnTimeout = Fail` 时超时返回错误；`Reject` 时返回 `Decision = TimedOut` 且不报错，由业务决定后续。

### 9.2 Workflow 内部流程

`approval.Request` 完全在 Workflow 内执行，满足确定性：

1. **登记**：审批 ID = `Key`；同一运行内重复的 `Key` 返回错误。将该审批写入 Memo `platformApprovals`（列表：ID、标题、摘要、字段、发起时间、截止时间），并 `UpsertTypedSearchAttributes(PlatformPendingApproval = true)`。
2. **发卡**：若 Worker 配置了飞书，执行平台 Activity `platform.feishu.SendCard`，返回 `messageId`；发卡失败按默认策略重试，最终失败只记录警告，审批继续通过控制台进行。未配置飞书则跳过。
3. **等待**：`workflow.Selector` 同时等待：
   - Signal `platform.approval`：载荷 `{approvalId, decision, comment, operator, channel, decidedAt}`；`approvalId` 不匹配当前等待中的任何审批则忽略；同一审批的后续 Signal 忽略。
   - `workflow.NewTimer(Timeout)`：触发即按 `OnTimeout` 处理。
   - ctx 取消：按“已取消”处理。
4. **收尾**：从 Memo 移除该审批，若无剩余待审则将 Search Attribute 置为 false；若存在 `messageId`，执行 `platform.feishu.UpdateCard` 将卡片更新为终态（已通过 / 已拒绝 / 已超时 / 已取消，附操作人和时间）。取消场景使用 `workflow.NewDisconnectedContext` 执行收尾。更新卡片失败只记录警告。

`approval` 包在一次运行内首次调用时获取 `platform.approval` 信号通道（`workflow.GetSignalChannel`），并启动一个 `workflow.Go` 分发协程；同一运行内的多个并发审批共享该通道，按 `approvalId` 分发。在审批登记之前到达的 Signal 会被 Temporal 缓冲，不会丢失。

### 9.3 回调路径（serve 侧）

```text
用户点击飞书卡片按钮
  -> 飞书开放平台经长连接推送卡片回调到 app-api
  -> approvals.HandleFeishuAction
       1. 解析回调值 {workflowId, runId, approvalId, decision} 与表单备注
       2. 操作人 open_id 不在 FEISHU_APPROVER_OPEN_IDS -> toast“无审批权限”
       3. Describe(workflowId, runId)：运行已结束 -> toast“流程已结束”；
          Memo 中无该审批 -> toast“已处理”
       4. SignalWorkflow(workflowId, runId, "platform.approval", {..., channel: "feishu"})
       5. 回调响应：toast“已提交” + 卡片置为“处理中”（3 秒内）

控制台待办页
  -> GET  /api/v1/approvals            Visibility: PlatformPendingApproval = true AND ExecutionStatus = 'Running'
                                       逐条读取 Memo.platformApprovals 展示
  -> POST /api/v1/approvals/{workflowId}/{runId}/{approvalId}/decision
       与上面第 3～4 步相同，channel = "web"，operator = "local"
```

- 使用 Signal 而非 Update：Worker 离线时 Signal 仍会持久化到历史，Worker 恢复后 Workflow 继续；待办列表来自 Visibility 与 Memo，同样不依赖 Worker 在线。
- 第 3 步的预检查只用于给用户即时反馈；并发场景下的最终去重由 Workflow 内部保证（9.2 第 3 步）。
- 长连接使用飞书官方 Go SDK，断线自动重连；连接状态在 `/api/v1/healthz?deep=1` 和 WebUI 顶部提示中展示，不影响 Docker 健康检查结果。

### 9.4 卡片

- 卡片模板在 `platform/feishu` 中统一定义：标题、Markdown 摘要、字段列表、可选备注输入框、“通过”“拒绝”按钮，按钮回传值包含 `workflowId`、`runId`、`approvalId`、`decision`。
- 卡片底部附“在控制台查看”文字与运行 ID（本机链接在手机上不可达，仅作提示）。
- 终态卡片移除按钮，显示决定、操作人、时间和备注。
- 卡片能否经长连接回调、回调响应格式与时限、卡片更新接口的约束，按 OPEN-001 在实现前对照飞书开放平台文档核实。

### 9.5 通知

```go
err := notify.Send(ctx, notify.Msg{Markdown: "**任务完成**", ChatID: ""}) // ChatID 为空使用 FEISHU_DEFAULT_CHAT_ID
```

`notify.Send` 执行平台 Activity `platform.feishu.SendMessage`，使用默认 Activity 策略。未配置飞书时返回 `notify.ErrNotConfigured`，业务可选择忽略。

### 9.6 飞书应用前置配置

README 提供步骤：创建企业自建应用 → 启用机器人 → 事件与回调订阅方式选择长连接 → 开通发送消息、更新消息所需权限 → 发布版本 → 将机器人加入目标会话 → 在 `.env` 填写 App ID、App Secret、审批人 open_id 列表、默认会话 ID。

## 10. HTTP API

### 10.1 路由

| 方法 | 路径 | 用途 | 优先级 |
| --- | --- | --- | --- |
| `GET` | `/api/v1/healthz` | 健康检查；`?deep=1` 附带 Worker 轮询与飞书连接状态 | P0 |
| `GET` | `/api/v1/workflows` | 目录（不含 Internal） | P0 |
| `GET` | `/api/v1/workflows/{id}` | 定义详情：Schema、示例、Signal 声明 | P0 |
| `POST` | `/api/v1/workflows/{id}/runs` | 启动 | P0 |
| `GET` | `/api/v1/runs` | 运行列表：`workflow`、`status`、`from`、`to`、`pageToken` | P0 |
| `GET` | `/api/v1/runs/{workflowId}/{runId}` | 运行详情 | P0 |
| `POST` | `/api/v1/runs/{workflowId}/{runId}/cancel` | 取消 | P0 |
| `POST` | `/api/v1/runs/{workflowId}/{runId}/signals/{name}` | 发送已声明 Signal | P1 |
| `GET` | `/api/v1/approvals` | 待办列表 | P0 |
| `POST` | `/api/v1/approvals/{workflowId}/{runId}/{approvalId}/decision` | 审批决定 `{decision, comment}` | P0 |
| `GET` | `/api/v1/schedules` | 定时任务列表 | P1 |
| `POST` | `/api/v1/schedules` | 创建 | P1 |
| `GET` | `/api/v1/schedules/{id}` | 详情 | P1 |
| `PATCH` | `/api/v1/schedules/{id}` | 编辑名称、输入、Cron、时区、策略 | P1 |
| `POST` | `/api/v1/schedules/{id}/pause` | 暂停 | P1 |
| `POST` | `/api/v1/schedules/{id}/resume` | 恢复 | P1 |
| `DELETE` | `/api/v1/schedules/{id}` | 删除 | P1 |

`{workflowId}` 中的 `:`、`/` 由前端做 URL 编码，服务端解码后使用。

### 10.2 错误结构

```json
{
  "error": {
    "code": "INVALID_INPUT",
    "message": "workflow input is invalid",
    "requestId": "01J...",
    "details": { "fields": [{ "path": "/names/0", "message": "must be string" }] }
  }
}
```

| 错误码 | HTTP 状态 | 场景 |
| --- | --- | --- |
| `INVALID_INPUT` | 400 | JSON、Schema、Cron、时区、幂等键格式校验失败 |
| `FORBIDDEN_ORIGIN` | 403 | Host 不在白名单或写请求 Origin 不同源 |
| `WORKFLOW_NOT_FOUND` | 404 | 定义不存在或为 Internal |
| `RUN_NOT_FOUND` | 404 | 运行不存在或已超出保留期 |
| `APPROVAL_NOT_FOUND` | 404 | 审批不存在 |
| `SCHEDULE_NOT_FOUND` | 404 | 定时任务不存在 |
| `RUN_ALREADY_EXISTS` | 409 | 幂等键相同但输入不同 |
| `INVALID_STATE` | 409 | 对已结束运行取消、发送 Signal 或审批；审批已处理 |
| `SIGNAL_NOT_DEFINED` | 422 | Signal 未在定义中声明 |
| `UNSUPPORTED_MEDIA_TYPE` | 415 | 写请求不是 `application/json` |
| `TEMPORAL_UNAVAILABLE` | 503 | Temporal 连接或 RPC 失败 |
| `INTERNAL_ERROR` | 500 | 未分类错误 |

所有响应带 `X-Request-ID` 头。

### 10.3 本地安全中间件

- **Host 白名单**：默认 `127.0.0.1:8080`、`localhost:8080`，可通过 `HTTP_ALLOWED_HOSTS` 扩展；不匹配返回 `FORBIDDEN_ORIGIN`，防御 DNS rebinding。
- **写请求**：`POST`/`PATCH`/`DELETE` 必须带同源 `Origin`（或缺省时 `Sec-Fetch-Site: same-origin`），且 `Content-Type: application/json`。
- 不返回任何 CORS 头。
- 认证扩展点：中间件链预留 `Authenticator` 接口，V1 使用放行实现。

## 11. WebUI

### 11.1 页面

| 页面 | 能力 |
| --- | --- |
| 目录 | 列表、搜索、说明 |
| 启动 | 由 Schema 生成表单，支持切换为 JSON 编辑；示例填充；最近输入（localStorage）；字段级错误 |
| 运行列表 | 筛选、分页、状态与待审批标记、未登记标记 |
| 运行详情 | 输入、输出、失败原因、时间线摘要、进行中 Activity 重试、取消、Signal、再次运行、Temporal UI 链接 |
| 待办 | 待审批列表，通过 / 拒绝 / 备注 |
| 定时任务 | 列表（上次 / 下次执行、需复核标记）、创建、编辑、暂停、恢复、删除 |

### 11.2 前端约定

- 统一 API Client 处理错误结构与请求 ID；服务端状态用查询缓存管理，运行中详情和待办页轮询刷新。
- 默认进入运行列表；全局顶栏显示 Temporal 连接与飞书长连接状态。
- 所有深层调试入口明确标注“在 Temporal UI 中查看”。
- 开发时 Vite Proxy（`web/vite.config.ts`）将 `/api` 转发到本地 Go 服务；生产产物输出到 `platform/webui/dist` 由 `go:embed` 嵌入。

## 12. 配置

| 环境变量 | 默认值（二进制） | Compose 中取值 | 说明 |
| --- | --- | --- | --- |
| `TEMPORAL_ADDRESS` | `127.0.0.1:7233` | `temporal:7233` | 宿主机直跑依赖 `compose.dev.yaml` 映射端口 |
| `TEMPORAL_NAMESPACE` | `default` | `default` | — |
| `TEMPORAL_TASK_QUEUE` | `personal-workflows` | 同左 | — |
| `TEMPORAL_UI_URL` | `http://127.0.0.1:8233` | 同左 | 浏览器可访问地址，用于生成链接 |
| `HTTP_ADDR` | `127.0.0.1:8080` | `:8080` | 容器内监听全部接口，但仅映射到宿主机回环地址 |
| `HTTP_ALLOWED_HOSTS` | `127.0.0.1:8080,localhost:8080` | 同左 | Host 白名单 |
| `WORKER_HEALTH_ADDR` | `127.0.0.1:8081` | `:8081` | Worker 健康端点，不映射到宿主机 |
| `FEISHU_APP_ID` / `FEISHU_APP_SECRET` | 空 | 来自 `.env` | 为空时飞书通道关闭 |
| `FEISHU_APPROVER_OPEN_IDS` | 空 | 来自 `.env` | 逗号分隔；飞书启用时必填 |
| `FEISHU_DEFAULT_CHAT_ID` | 空 | 来自 `.env` | 审批卡片与通知默认会话；飞书启用时必填 |
| `LOG_LEVEL` | `info` | 同左 | — |

`config` 启动时校验：飞书 App ID 与 Secret 必须同时为空或同时存在；启用飞书时审批人与默认会话必填。

## 13. 镜像构建

```text
阶段 1：node 镜像构建 web/，产物输出到 platform/webui/dist
阶段 2：golang 镜像，CGO_ENABLED=0 编译 cmd/platform
阶段 3：distroless static（nonroot）运行镜像，仅含二进制与 CA 证书
```

- 使用 `docker buildx` 构建 `linux/arm64` 与 `linux/amd64`。
- 以非 root 用户运行；`ENTRYPOINT ["/platform"]`，Compose 通过 `command` 传 `serve` / `worker`。
- 两个子命令均处理 SIGTERM：`serve` 关闭 HTTP 服务（超时 10 秒）与长连接；`worker` 见 6.6。

## 14. 可观测性

日志使用 `log/slog` JSON 输出，字段：

```text
service=api|worker  request_id  workflow_id  run_id  workflow_type
activity_type  approval_id  error_code  duration_ms
```

- Workflow 内使用 `workflow.GetLogger`（重放时不重复输出）；Activity 内使用 `activity.GetLogger`；二者通过 SDK Logger 适配到 slog。
- 默认不输出 Payload；Token、App Secret、Authorization、Cookie 统一由 `obs` 脱敏。
- 健康检查：

| 对象 | 方式 | 用于 Docker healthcheck |
| --- | --- | --- |
| `app-api` | `GET /api/v1/healthz`：进程存活且 Temporal `CheckHealth` 成功 | 是 |
| Worker 轮询 | `healthz?deep=1` 中通过 `DescribeTaskQueue` 检查最近 1 分钟内是否有 Poller | 否 |
| 飞书长连接 | `healthz?deep=1` 与 WebUI 顶栏 | 否 |
| `app-worker` | 容器内 `:8081/healthz`（见 6.6） | 是 |

## 15. 测试策略

| 层级 | 范围 | 工具 |
| --- | --- | --- |
| SDK / Catalog 单元测试 | 重复注册、保留前缀、签名检查、Schema 生成、示例校验、默认 Activity 选项 | `go test` |
| 审批组件测试 | 通过、拒绝、超时（Reject / Fail）、重复 Signal、ID 不匹配、并发两个审批、运行取消、未配置飞书、发卡失败 | `testsuite.WorkflowTestSuite` |
| 业务 Workflow 测试 | `testkit.New(t)` 预注册全部定义；`testkit.ApproveAll` / `RejectAll` / `TimeoutAll` mock 审批 | `testkit` |
| 重放测试 | `testkit.Replay(t, "testdata/histories")` 回放导出历史 | `worker.WorkflowReplayer` |
| runs / schedules 测试 | 幂等（同输入、不同输入、已结束）、状态映射、错误转换、Schedule 字段映射与需复核判定 | Temporal 测试服务器（`testsuite.StartDevServer`） |
| approvals 测试 | 白名单、预检查分支、Signal 载荷 | 飞书回调使用桩数据 |
| API 测试 | 路由、状态码、错误结构、Host / Origin / Content-Type 校验、请求 ID | `httptest` |
| WebUI 测试 | 表单生成与校验、分页、待办操作 | Vitest |
| 确定性检查 | 全部 Workflow 包 | `workflowcheck`（`make lint`） |
| Compose 冒烟 | 见下 | `make smoke` |

`make smoke` 步骤：

1. `make up` 并等待全部服务健康，检查 namespace 保留期为 90 天。
2. 查询目录，确认包含 `greeting` 与 `approvaldemo`。
3. 启动 `greeting`，等待成功并检查输出。
4. 以相同幂等键与输入重复提交，确认返回同一 Run ID；以不同输入提交，确认 409。
5. 启动 `approvaldemo`，通过待办 API 发出“通过”决定，确认运行成功。
6. 检查运行详情中的 Temporal UI 链接可访问。
7. 确认宿主机无法连接 `7233`、`5432`。
8. 执行 `docker compose down`（不带 `-v`）后再 `make up`，确认第 3、5 步的运行仍可在运行列表中查询到。

飞书真机联调（需求验收第 7、8 条）为手工验收项。

## 16. 从当前示例迁移

当前仓库为学习示例：根目录 `workeflow.go`（`SayHelloWorkflow`）、`childworkflow.go`（`ChildSayHelloWorkflow`）、`activity.go`（`Greet`），以及 `worker/main.go`、`start/main.go`。迁移步骤：

1. 将 Go 模块路径由 `myTemporal` 改为正式名称（实现时确定），建立第 5.1 节目录。
2. 实现 `platform/config`、`obs`、`sdk`、`catalog`、`runtime` 与 `cmd/platform worker`。
3. 将示例改写为 `app/greeting`：`greeting`（原 `SayHelloWorkflow`，输入改为 struct）、`greeting.one`（原子流程，`Internal`）、`greeting.Greet`（原 Activity）；子流程使用 `sdk.ChildOptions` 生成 ID；Activity 使用默认策略。
4. 编写 `deploy/` 下的 Compose、初始化任务与 Makefile，跑通 `make up`。
5. 实现 `runs`、`webapi`、`cmd/platform serve` 与 WebUI 的目录、启动、运行列表与详情。
6. 实现审批组件、`approvals`、飞书客户端与长连接，新增 `app/approvaldemo` 示例；实现 `notify`。
7. 实现 `schedules` 与定时任务页。
8. 补齐测试与 `make smoke`，导出示例历史到 `testdata/histories`。

迁移完成后删除：根目录三个示例文件、`worker/`、`start/`，以及其中的硬编码 `127.0.0.1:7233`、`my-task-queue`、固定 Workflow ID `greeting-workflow`。

## 17. 未决事项

见需求文档第 13 节。与架构直接相关的是：

- OPEN-001：飞书长连接卡片回调能力，决定 9.3、9.4 的实现细节；若不支持，审批的飞书通道降级为“卡片仅提醒，审批在控制台完成”，其余设计不变。
- OPEN-002 已关闭，版本记录见 [dependency-versions.md](dependency-versions.md)。
