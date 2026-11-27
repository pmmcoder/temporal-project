# 飞书交互卡片与长连接可行性研究

| 项目 | 内容 |
| --- | --- |
| 研究日期 | 2026-11-20 |
| 结论 | `OPEN-001` 可以关闭：企业自建应用机器人可通过长连接接收新版交互卡片按钮回调 |
| 适用范围 | 企业自建应用、应用机器人、飞书卡片 JSON 2.0、官方 Go SDK v3 |
| 来源约束 | 仅使用 `open.feishu.cn` 官方文档和 `github.com/larksuite/oapi-sdk-go` 官方仓库 |
| SDK 审计基线 | `v3_main` commit `a3550335752a9af4662314e5838138562f04aa70`，提交日期 2026-11-16 |

> **OPEN-001 关闭结论**
>
> “无公网入站端口，通过应用机器人的出站长连接接收卡片按钮回调，再用 OpenAPI 发送和更新卡片”的方案成立。实现时必须使用新版卡片回传交互 `card.action.trigger`，不能使用旧版 `card.action.trigger_v1`；备注输入应放入表单容器，与“通过”或“拒绝”提交按钮在同一次回调中提交。

## 1. 结论摘要

| 问题 | 状态 | 结论 | 官方依据 |
| --- | --- | --- | --- |
| 企业自建应用机器人能否通过长连接接收按钮回调 | 已确认 | 可以。新版 `card.action.trigger` 支持“使用长连接接收回调”，且该模式仅支持企业自建应用 | [官方处理回调](https://open.feishu.cn/document/server-side-sdk/golang-sdk-guide/handle-callback)、[卡片回传交互](https://open.feishu.cn/document/feishu-cards/card-callback-communication) |
| 旧版按钮回调能否走长连接 | 限制 | 不能。旧版 `card.action.trigger_v1` 只能发送到开发者服务器 | [官方处理回调](https://open.feishu.cn/document/server-side-sdk/golang-sdk-guide/handle-callback) |
| 长连接网络和连接要求 | 限制 | 服务器只需能访问公网，无需公网 IP、域名或入站白名单；每个应用最多 50 个连接，消息在多个连接间竞争消费 | [官方处理回调](https://open.feishu.cn/document/server-side-sdk/golang-sdk-guide/handle-callback)、[官方处理事件](https://open.feishu.cn/document/server-side-sdk/golang-sdk-guide/handle-events) |
| 回调响应时限 | 限制 | 收到消息后须在 3 秒内处理完成；事件超时会触发重推，卡片回调超时会在客户端报错 | [官方处理事件](https://open.feishu.cn/document/server-side-sdk/golang-sdk-guide/handle-events)、[官方处理卡片回调](https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/handle-card-callbacks) |
| 按钮自定义回传数据 | 已确认 | `action.value` 承载按钮 `behaviors[].value`；JSON 2.0 和 Go SDK 仅支持 object，推荐使用字符串键值对象 | [配置卡片交互](https://open.feishu.cn/document/feishu-cards/configuring-card-interactions)、[SDK callback model](https://github.com/larksuite/oapi-sdk-go/blob/a3550335752a9af4662314e5838138562f04aa70/event/dispatcher/callback/model.go#L30-L40) |
| 发送和更新卡片 | 已确认 | 发送用 `POST /open-apis/im/v1/messages`，更新用 `PATCH /open-apis/im/v1/messages/:message_id`；保存发送响应中的 `message_id` 供后续更新 | [发送消息](https://open.feishu.cn/document/server-docs/im-v1/message/create)、[更新已发送的消息卡片](https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/im-v1/message/patch) |
| 备注输入 | 已确认 | 官方输入框组件明确服务于原因、评价、备注等文本收集；与按钮组合时必须放入表单容器 | [输入框组件](https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/card-json-v2-components/interactive-components/input)、[表单容器](https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/card-json-v2-components/containers/form-container) |
| Go SDK 接口 | 已确认 | 长连接用 `ws.NewClient` + `Start`，回调用 `EventDispatcher.OnP2CardActionTrigger`，消息 API 用 `service/im/v1` 的 `Message.Create` 和 `Message.Patch` | [官方 Go SDK 处理回调](https://open.feishu.cn/document/server-side-sdk/golang-sdk-guide/handle-callback)、[SDK IM resource](https://github.com/larksuite/oapi-sdk-go/blob/a3550335752a9af4662314e5838138562f04aa70/service/im/v1/resource.go#L1185-L1376) |

## 2. 企业自建应用机器人与长连接按钮回调

### 2.1 已确认能力

【已确认】企业自建应用机器人可以接收交互卡片按钮回调：

- 官方卡片回调类型为 `card.action.trigger`。
- 官方 Go SDK 的《处理回调》文档明确提供“使用长连接接收回调”。
- 长连接是飞书 SDK 通过 WebSocket 与开放平台建立的全双工通道。
- 应用只需具备公网出站能力，不需要公网 IP、域名、内网穿透、防火墙入站规则或 Webhook 验签服务。

来源：

- [卡片回传交互](https://open.feishu.cn/document/feishu-cards/card-callback-communication)
- [处理回调](https://open.feishu.cn/document/server-side-sdk/golang-sdk-guide/handle-callback)
- [处理卡片回调](https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/handle-card-callbacks)

### 2.2 配置位置

【已确认】卡片按钮在飞书开发者后台属于 **回调订阅**，不是普通业务事件订阅：

1. 在“开发配置 > 事件与回调 > 回调配置”中选择“使用长连接接收回调”。
2. 订阅新版“卡片回传交互”回调 `card.action.trigger`。
3. 先启动 SDK 长连接，再在开发者后台保存长连接订阅方式。官方文档说明，必须先建立连接才能保存该配置。
4. 发送卡片的必须是配置了机器人能力的企业自建应用，不能是只用于发送静态通知的群自定义机器人。

【已确认】订阅 `card.action.trigger` 本身没有额外权限要求，官方文档标注为“暂无”。回调默认提供 `operator.open_id`，审批人白名单应使用 Open ID；若业务需要 `user_id`，才需要额外申请 `contact:user.employee_id:readonly`。

依据：

- [卡片回传交互：权限与字段权限](https://open.feishu.cn/document/feishu-cards/card-callback-communication)
- [处理卡片回调](https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/handle-card-callbacks)
- [使用指定应用发送飞书卡片](https://open.feishu.cn/document/feishu-cards/quick-start/send-feishu-cards-with-app-bots)
- [配置卡片交互](https://open.feishu.cn/document/feishu-cards/configuring-card-interactions)

### 2.3 明确限制

【限制】

- 长连接模式仅支持企业自建应用，不支持商店应用使用该接收方式。
- 旧版 `card.action.trigger_v1` 不支持长连接。
- 如果同时订阅新版和旧版回调，飞书会分别发送两次请求；官方建议只保留新版。
- 请求回调只适用于通过应用发送的卡片。群自定义机器人发送的卡片不能使用请求回调交互。
- 卡片回调绑定在应用维度，不支持为单张卡片单独配置回调地址。

来源：

- [处理回调](https://open.feishu.cn/document/server-side-sdk/golang-sdk-guide/handle-callback)
- [处理卡片回调](https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/handle-card-callbacks)
- [配置卡片交互](https://open.feishu.cn/document/feishu-cards/configuring-card-interactions)

### 2.4 文档表述差异

【已确认】卡片回调结构页的“推送方式”栏仍写着 Webhook，但同一官方站点的《处理回调》和《处理卡片回调》已明确提供“使用长连接接收回调”。实现时应以回调订阅配置页和长连接处理文档为准，并确保开发者后台实际选择的是“使用长连接接收回调”。

来源：

- [卡片回传交互](https://open.feishu.cn/document/feishu-cards/card-callback-communication)
- [处理回调](https://open.feishu.cn/document/server-side-sdk/golang-sdk-guide/handle-callback)

## 3. 长连接要求、重连与响应时限

### 3.1 平台侧要求

| 项目 | 要求或行为 | 来源 |
| --- | --- | --- |
| 应用类型 | 仅企业自建应用 | [处理回调](https://open.feishu.cn/document/server-side-sdk/golang-sdk-guide/handle-callback) |
| 网络 | 运行环境能够访问公网；无需开放公网入站端口 | [处理事件](https://open.feishu.cn/document/server-side-sdk/golang-sdk-guide/handle-events) |
| 连接建立 | 先启动 SDK 客户端并连接，随后才能在后台保存长连接订阅 | [处理回调](https://open.feishu.cn/document/server-side-sdk/golang-sdk-guide/handle-callback) |
| 连接数 | 每个应用最多 50 个连接；每初始化一个 SDK client 即算一个连接 | [处理回调](https://open.feishu.cn/document/server-side-sdk/golang-sdk-guide/handle-callback) |
| 消息分发 | 集群竞争消费，不是广播；多个 client 中只有一个会收到每条消息 | [处理回调](https://open.feishu.cn/document/server-side-sdk/golang-sdk-guide/handle-callback) |
| 数据处理时限 | 收到消息后 3 秒内处理完成 | [处理事件](https://open.feishu.cn/document/server-side-sdk/golang-sdk-guide/handle-events)、[处理卡片回调](https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/handle-card-callbacks) |

### 3.2 3 秒时限的实际含义

【限制】长连接不是 HTTP Webhook，应用代码不需要自行返回 HTTP 200；SDK 会把处理结果编码为长连接响应。官方 Go SDK 当前实现中：

- handler 成功返回时，SDK 回送 `code: 200`。
- handler 返回 error 时，SDK 回送 `code: 500`。
- `nil, nil` 是合法的“已处理、无需返回 Toast 或新卡片”响应。

依据：

- [卡片回传交互：响应回调结构](https://open.feishu.cn/document/feishu-cards/card-callback-communication)
- [SDK client_message.go](https://github.com/larksuite/oapi-sdk-go/blob/a3550335752a9af4662314e5838138562f04aa70/ws/client_message.go#L94-L136)

【限制】3 秒是完整处理预算，不应在回调 handler 中等待 Workflow 最终结果、执行较慢的外部请求或串行调用多个 OpenAPI：

- 事件回调超时会触发飞书重推。
- 卡片回调超时或立即更新失败时，用户客户端会显示错误。
- 需要异步处理时，应尽快完成校验和必要的轻量转交，在 3 秒内返回 Toast 或空响应，之后再用 `message_id` 更新卡片。

来源：

- [处理事件](https://open.feishu.cn/document/server-side-sdk/golang-sdk-guide/handle-events)
- [处理卡片回调](https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/handle-card-callbacks)

### 3.3 SDK 自动重连

【已确认】官方 Go SDK 内置自动重连，不应在业务层再实现一套 WebSocket 重连：

- `ws.NewClient` 默认 `autoReconnect=true`。
- 默认 `reconnectCount=-1`，表示无限次重试。
- 首次重连带 0 至 30 秒随机抖动，后续默认每 2 分钟重试。
- 默认每 2 分钟 Ping，单次 WebSocket 写入超时 10 秒。
- 飞书服务端可通过连接接口或 Pong 下发 `ClientConfig`，覆盖重连次数、间隔、抖动和 Ping 间隔；因此上述默认值是可观察的 SDK 行为，不是飞书平台的永久协议承诺。

依据：

- [SDK ws/client.go](https://github.com/larksuite/oapi-sdk-go/blob/a3550335752a9af4662314e5838138562f04aa70/ws/client.go#L214-L271)
- [SDK ws/client_lifecycle.go](https://github.com/larksuite/oapi-sdk-go/blob/a3550335752a9af4662314e5838138562f04aa70/ws/client_lifecycle.go#L187-L250)
- [SDK ws/client.go：服务端配置覆盖](https://github.com/larksuite/oapi-sdk-go/blob/a3550335752a9af4662314e5838138562f04aa70/ws/client.go#L303-L313)

【已确认】当前 SDK 提供连接生命周期回调：

- `WithOnReady`
- `WithOnReconnecting`
- `WithOnReconnected`
- `WithOnError`
- `WithOnDisconnected`

这些回调可用于更新 `/api/v1/healthz?deep=1` 和 WebUI 的连接状态。断线只表示通道暂不可用，不应让整个 API 进程被 Docker 健康检查判死并反复重启。

依据：

- [SDK ws/client.go](https://github.com/larksuite/oapi-sdk-go/blob/a3550335752a9af4662314e5838138562f04aa70/ws/client.go#L154-L212)

### 3.4 尚未确认

【未确认】官方一手资料没有公开以下完整策略：

- 事件超时重推的最大次数、间隔和退避算法。
- SDK 连接完全断开期间，飞书侧事件或回调的缓冲时长和最终丢弃条件。
- 多客户端时，重推是否一定切换至另一个连接。
- 卡片回调与普通事件是否采用完全相同的重推策略。

因此实现必须以 `header.event_id` 做幂等去重，不能让一次重复回调产生两次审批决定。

## 4. 卡片按钮回调可携带的数据

### 4.1 核心回调字段

| 回调字段 | 内容 | 用途 |
| --- | --- | --- |
| `header.event_id` | 回调唯一标识 | 推荐作为幂等键，抵御超时重推 |
| `event.operator.open_id` | 操作用户在本应用中的 Open ID | 审批人白名单校验的首选身份 |
| `event.operator.user_id` | 用户 ID | 仅开启 `contact:user.employee_id:readonly` 后返回，不是默认可用字段 |
| `event.action.value` | 被操作组件的 `behaviors[].value` | 承载 `workflowId`、`runId`、`approvalId`、`decision` 等路由数据 |
| `event.action.tag` | 组件标签，例如 `button`、`input` | 判断交互组件类型 |
| `event.action.name` | 组件 `name` | 表单内按钮等组件的业务标识 |
| `event.action.form_value` | 表单内全部字段，键为组件 `name` | 读取备注输入 |
| `event.action.input_value` | 非表单输入框提交的文本 | 独立输入框场景；审批按钮场景不应用它承载备注 |
| `event.context.open_message_id` | 卡片消息 ID | 定位卡片 |
| `event.context.open_chat_id` | 卡片所在会话 ID | 记录来源会话 |
| `event.token` | 延时更新卡片凭证，有效期 30 分钟，最多更新 2 次 | 用户交互后的短期延时更新 |

来源：

- [卡片回传交互](https://open.feishu.cn/document/feishu-cards/card-callback-communication)
- [输入框组件：回调示例](https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/card-json-v2-components/interactive-components/input)
- [表单容器：回调结构](https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/card-json-v2-components/containers/form-container)

### 4.2 `callback value` 的类型限制

【已确认】当前 JSON 2.0 的组件回调数据配置为：

```json
{
  "behaviors": [
    {
      "type": "callback",
      "value": {
        "workflow_id": "order-123",
        "run_id": "run-456",
        "approval_id": "approval-789",
        "decision": "approve"
      }
    }
  ]
}
```

【限制】

- JSON 2.0 的 `behaviors[].value` 官方只声明支持 object。
- 开放平台 Go SDK 只能解码 object，对应 `map[string]interface{}`。
- 回调结构总表虽把 `action.value` 写为 object 或 string，但这是兼容旧版结构；不应在 JSON 2.0 + Go SDK 方案中依赖字符串类型。
- object 内可以保存 JSON 值，但业务代码仍需自行校验必填键、字符串类型和允许的决策值。

来源：

- [配置卡片交互](https://open.feishu.cn/document/feishu-cards/configuring-card-interactions)
- [按钮组件：JSON 2.0](https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/card-json-v2-components/interactive-components/button)
- [SDK callback model](https://github.com/larksuite/oapi-sdk-go/blob/a3550335752a9af4662314e5838138562f04aa70/event/dispatcher/callback/model.go#L15-L47)

### 4.3 数据容量

【限制】自定义回调值会写入卡片 JSON，因此整个卡片消息仍受 30 KB 上限约束。官方没有另行公开 `behaviors[].value` 的独立字段大小限制。

【未确认】除整张卡片 30 KB 外，单个 `value` 字段是否还有未公开的服务端限制，无法从官方文档确认。不要把大文本、文件或业务详情放入按钮回传值，只放定位和决策所需的小型字段。

来源：

- [发送消息：content 限制](https://open.feishu.cn/document/server-docs/im-v1/message/create)
- [配置卡片交互](https://open.feishu.cn/document/feishu-cards/configuring-card-interactions)

## 5. 卡片发送与更新 API

### 5.1 发送卡片

| 项目 | 值 |
| --- | --- |
| API | `POST https://open.feishu.cn/open-apis/im/v1/messages` |
| 接收者查询参数 | `receive_id_type=chat_id` 或 `open_id` 等 |
| 请求体 | `receive_id`、`msg_type=interactive`、序列化后的卡片 JSON 字符串 `content` |
| 响应 | `data.message_id`，例如 `om_...` |
| 最小应用权限 | `im:message:send_as_bot` |
| 其他可用发送权限 | `im:message` 或历史版本 `im:message:send` |

来源：

- [发送消息](https://open.feishu.cn/document/server-docs/im-v1/message/create)
- [使用指定应用发送飞书卡片](https://open.feishu.cn/document/feishu-cards/quick-start/send-feishu-cards-with-app-bots)

【限制】

- 应用必须开启机器人能力并发布生效。
- 向用户发送时，用户必须位于应用可用范围内。
- 向群发送时，机器人必须在群内且拥有发言权限。
- 同一用户发送限频 5 QPS；同一群组内机器人共享 5 QPS；接口整体限频 1000 次/分钟、50 次/秒。
- 卡片消息请求体最多 30 KB。
- `content` 必须是 JSON 序列化后的字符串，不是直接嵌套的对象。
- `uuid` 最大 50 字符；相同 `uuid` 的请求在 1 小时内至多成功发送一条。建议由 Workflow、Run 和 Approval 组成确定性幂等键，避免 Activity 重试重复发卡。

来源：

- [发送消息](https://open.feishu.cn/document/server-docs/im-v1/message/create)

### 5.2 更新卡片

| 项目 | 值 |
| --- | --- |
| API | `PATCH https://open.feishu.cn/open-apis/im/v1/messages/:message_id` |
| 请求体 | `content`，为新的完整卡片 JSON 序列化字符串 |
| 响应 | `code=0`、空 `data` |
| 最小应用权限 | `im:message:send_as_bot` |
| 其他可用更新权限 | `im:message` 或 `im:message:update` |

来源：

- [更新已发送的消息卡片](https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/im-v1/message/patch)
- [更新卡片](https://open.feishu.cn/document/feishu-cards/update-feishu-card)

【限制】

- 仅支持更新未撤回的 `interactive` 卡片，不支持普通文本消息的卡片编辑。
- 调用更新接口的 access token 身份必须与发送卡片时一致。
- 仅支持更新发送后 14 天内的消息。
- 更新前后卡片的 `config.update_multi` 都必须显式为 `true`。
- 不支持批量发送的消息和仅特定人可见的临时卡片。
- 单条消息更新频控为 5 QPS。
- 更新后的卡片 JSON 仍不得超过 30 KB。
- 本接口是整张卡片替换。若只需要局部修改组件，可另行评估 CardKit 组件级接口，但审批终态替换使用 `Message.Patch` 已足够。

来源：

- [更新已发送的消息卡片](https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/im-v1/message/patch)
- [更新卡片](https://open.feishu.cn/document/feishu-cards/update-feishu-card)

### 5.3 `message_id` 的获取和使用

【已确认】推荐流程：

1. 发卡成功后，从响应 `data.message_id` 取得并持久化消息 ID。
2. 卡片任意按钮回调还会提供 `event.context.open_message_id`；可用于关联和校验，但审批状态仍应以发送响应中保存的 ID 为准。
3. 终态卡片通过 `PATCH /im/v1/messages/{message_id}` 更新。
4. `message_id` 必须放在 URL 路径参数中，不是请求体字段。

来源：

- [发送消息](https://open.feishu.cn/document/server-docs/im-v1/message/create)
- [更新已发送的消息卡片](https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/im-v1/message/patch)

### 5.4 三种卡片更新路径的区别

| 场景 | 官方方式 | 限制 |
| --- | --- | --- |
| 收到卡片回调后 3 秒内同步更新 | 在回调响应中返回新卡片 | 必须在 3 秒内 |
| 用户交互后短时间延时更新 | 使用回调 `event.token` 调用延时更新卡片接口 | 必须等回调响应成功后再调用；token 30 分钟有效，最多 2 次 |
| 系统主动更新已发送卡片 | 使用发送响应中的 `message_id` 调用 `Message.Patch` | 发送后 14 天内；`update_multi=true`；同发送身份 |

来源：

- [处理卡片回调](https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/handle-card-callbacks)
- [更新已发送的消息卡片](https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/im-v1/message/patch)

【实施判断】本项目的审批结果由 Workflow 异步推进，使用 `message_id` + `Message.Patch` 比依赖回调 token 更直接。回调 handler 应快速返回，不等待 Workflow 到达终态。

## 6. 备注输入的官方支持方式与时序

### 6.1 支持方式

【已确认】官方输入框组件明确用于收集“原因、评价、备注”等主观文本。审批备注推荐配置：

- 使用卡片 JSON 2.0。
- 在根节点 `body.elements` 下放一个表单容器 `form`。
- 表单内放一个 `input`，设置唯一 `name`，例如 `comment`。
- 设置 `input_type: "multiline_text"` 以支持多行备注。
- 可选备注设置 `required: false`；若要强制填写则设为 `true`。
- `max_length` 取 1 至 1000，默认 1000。
- “通过”和“拒绝”两个按钮都放在同一表单内。
- 每个按钮设置唯一的 `name`、`form_action_type: "submit"`，以及各自的 `callback` behavior value。

输入框官方说明：

- 输入框仅支持飞书 V6.8 及以上客户端。
- 表单容器的输入数据是异步提交的：用户编辑时不会逐字回调，只有点击绑定提交事件的按钮后，才把所有表单项一次性回调。
- 表单内交互组件的 `name` 必填且全局唯一。
- 非必填输入框为空时，表单仍会提交其余数据。

来源：

- [输入框组件](https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/card-json-v2-components/interactive-components/input)
- [表单容器](https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/card-json-v2-components/containers/form-container)
- [按钮组件](https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/card-json-v2-components/interactive-components/button)

### 6.2 推荐的回调映射

点击“通过”时：

```text
event.action.value.decision                         = "approve"
event.action.form_value.comment                     = 用户输入的备注
event.operator.open_id                              = 审批人
event.context.open_message_id                       = 卡片消息 ID
```

点击“拒绝”时只改变按钮回传的 `decision`，备注仍在同一个 `form_value` 中。这样决定和备注由同一次用户提交产生，不会出现“先提交备注、再点击决定”或反向顺序导致的两次异步回调。

字段结构依据：

- [表单容器：回调结构](https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/card-json-v2-components/containers/form-container)
- [输入框组件：嵌套规则](https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/card-json-v2-components/interactive-components/input)

### 6.3 时序

1. 用户点击输入框并输入备注。此时数据只保存在客户端表单内，不触发网络回调。
2. 用户点击“通过”或“拒绝”按钮。
3. 客户端校验表单必填项，然后提交完整表单。
4. 应用收到一次 `card.action.trigger`：
   - `action.value` 来自被点击按钮的 behavior。
   - `action.form_value` 包含输入框 `name` 到备注文本的映射。
   - `action.name` 标识被点击的表单按钮。
5. 应用必须在 3 秒内返回处理结果：
   - 返回 Toast 或空响应。
   - 或直接返回更新后的卡片。
6. 如果需要 Workflow 后续处理，应先完成快速校验和 Signal 转交，不在 handler 内等待 Workflow 完成；终态通过已保存的 `message_id` 更新。

来源：

- [处理卡片回调](https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/handle-card-callbacks)
- [表单容器](https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/card-json-v2-components/containers/form-container)

### 6.4 不应采用的组合

【限制】不要把输入框和审批按钮作为彼此独立的交互组件放在表单外。官方文档说明：

- 普通交互组件相互独立，操作任一组件都会立即形成一次交互。
- 非表单输入框点击提交图标时通过 `action.input_value` 单独回调。
- 只有输入框位于表单容器中，并点击表单的提交按钮时，输入文本才会作为 `form_value` 与按钮决定一起提交。

来源：

- [配置卡片交互](https://open.feishu.cn/document/feishu-cards/configuring-card-interactions)
- [输入框组件](https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/card-json-v2-components/interactive-components/input)

### 6.5 有效期限制

【限制】卡片 JSON 2.0 的请求回调交互和可更新时间统一为 14 天。JSON 1.0 的交互期是 30 天、更新期是 14 天。因此本项目应统一使用 JSON 2.0，并把审批有效期设计在 14 天以内。

来源：

- [配置卡片交互：使用限制](https://open.feishu.cn/document/feishu-cards/configuring-card-interactions)

## 7. 官方 Go SDK 的包名与核心接口

### 7.1 包名

| 用途 | import path | 常用别名/核心类型 |
| --- | --- | --- |
| API Client | `github.com/larksuite/oapi-sdk-go/v3` | `lark.NewClient` |
| 长连接 | `github.com/larksuite/oapi-sdk-go/v3/ws` | `larkws.NewClient`、`larkws.Client` |
| 回调/事件分发 | `github.com/larksuite/oapi-sdk-go/v3/event/dispatcher` | `dispatcher.NewEventDispatcher`、`EventDispatcher` |
| 卡片回调模型 | `github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback` | `CardActionTriggerEvent`、`CardActionTriggerResponse` |
| 消息 API | `github.com/larksuite/oapi-sdk-go/v3/service/im/v1` | `larkim.NewCreateMessageReqBuilder`、`NewPatchMessageReqBuilder` |

来源：

- [官方 Go SDK 处理回调](https://open.feishu.cn/document/server-side-sdk/golang-sdk-guide/handle-callback)
- [官方 Go SDK 调用服务端 API](https://open.feishu.cn/document/server-side-sdk/golang-sdk-guide/calling-server-side-apis)
- [官方 SDK callback model](https://github.com/larksuite/oapi-sdk-go/blob/a3550335752a9af4662314e5838138562f04aa70/event/dispatcher/callback/model.go#L1-L64)

### 7.2 核心接口

| 接口 | 作用 |
| --- | --- |
| `lark.NewClient(appID, appSecret, options...)` | 创建 OpenAPI client，之后通过 `client.Im.Message` 调用消息 API |
| `larkws.NewClient(appID, appSecret, options...)` | 创建长连接 client |
| `larkws.WithEventHandler(dispatcher)` | 为长连接注册回调/事件分发器 |
| `client.Start(ctx)` | 建立并维持连接；该方法阻塞直到 context 结束或连接终止 |
| `client.Close()` / `client.CloseAndWait(ctx)` | 请求关闭或等待连接与回调 worker 退出 |
| `dispatcher.NewEventDispatcher("", "")` | 初始化长连接模式分发器；两个凭据参数必须为空，鉴权由长连接握手完成 |
| `dispatcher.OnP2CardActionTrigger(handler)` | 监听 `card.action.trigger` |
| `callback.CardActionTriggerEvent` | 回调入口，暴露 operator、action、context 和 token |
| `callback.CardActionTriggerResponse` | 可返回 `Toast` 和/或 `Card` |
| `client.Im.Message.Create(ctx, req)` | 调用发送消息 API |
| `client.Im.Message.Patch(ctx, req)` | 调用更新已发送卡片 API |

依据：

- [官方 Go SDK 处理回调](https://open.feishu.cn/document/server-side-sdk/golang-sdk-guide/handle-callback)
- [SDK ws/client.go](https://github.com/larksuite/oapi-sdk-go/blob/a3550335752a9af4662314e5838138562f04aa70/ws/client.go#L50-L182)
- [SDK ws/client_lifecycle.go](https://github.com/larksuite/oapi-sdk-go/blob/a3550335752a9af4662314e5838138562f04aa70/ws/client_lifecycle.go#L33-L91)
- [SDK callback_dispatch.go](https://github.com/larksuite/oapi-sdk-go/blob/a3550335752a9af4662314e5838138562f04aa70/event/dispatcher/callback_dispatch.go#L9-L16)

### 7.3 核心调用示意

以下代码仅展示包名和接口关系，省略错误处理、业务校验和生命周期管理：

```go
apiClient := lark.NewClient(appID, appSecret)

eventHandler := dispatcher.NewEventDispatcher("", "").
	OnP2CardActionTrigger(func(
		ctx context.Context,
		event *callback.CardActionTriggerEvent,
	) (*callback.CardActionTriggerResponse, error) {
		openID := event.Event.Operator.OpenID
		value := event.Event.Action.Value
		comment := event.Event.Action.FormValue["comment"]

		// 校验 openID、value 和 note，然后尽快完成 Signal 转交。
		_ = openID
		_ = value
		_ = comment

		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{
				Type:    "success",
				Content: "审批已提交",
			},
		}, nil
	})

wsClient := larkws.NewClient(
	appID,
	appSecret,
	larkws.WithEventHandler(eventHandler),
)
go func() {
	_ = wsClient.Start(ctx)
}()

resp, err := apiClient.Im.Message.Create(
	ctx,
	larkim.NewCreateMessageReqBuilder().
		ReceiveIdType("chat_id").
		Body(
			larkim.NewCreateMessageReqBodyBuilder().
				ReceiveId(chatID).
				MsgType(larkim.MsgTypeInteractive).
				Content(string(cardJSON)).
				Uuid(idempotencyKey).
				Build(),
		).
		Build(),
)
messageID := *resp.Data.MessageId

_, err = apiClient.Im.Message.Patch(
	ctx,
	larkim.NewPatchMessageReqBuilder().
		MessageId(messageID).
		Body(
			larkim.NewPatchMessageReqBodyBuilder().
				Content(string(updatedCardJSON)).
				Build(),
		).
		Build(),
)
```

源码依据：

- [IM Create 方法](https://github.com/larksuite/oapi-sdk-go/blob/a3550335752a9af4662314e5838138562f04aa70/service/im/v1/resource.go#L1185-L1209)
- [IM Patch 方法](https://github.com/larksuite/oapi-sdk-go/blob/a3550335752a9af4662314e5838138562f04aa70/service/im/v1/resource.go#L1353-L1376)
- [CreateMessage builder/model](https://github.com/larksuite/oapi-sdk-go/blob/a3550335752a9af4662314e5838138562f04aa70/service/im/v1/model.go#L13636-L13837)
- [PatchMessage builder/model](https://github.com/larksuite/oapi-sdk-go/blob/a3550335752a9af4662314e5838138562f04aa70/service/im/v1/model.go#L14434-L14540)
- [interactive 消息常量](https://github.com/larksuite/oapi-sdk-go/blob/a3550335752a9af4662314e5838138562f04aa70/service/im/v1/ext_model.go#L646-L660)

### 7.4 SDK 使用注意

【限制】

- API Client 与长连接 Client 是两个 client：`lark.NewClient` 负责 OpenAPI，`larkws.NewClient` 负责 WebSocket。
- 长连接模式下 `dispatcher.NewEventDispatcher` 的两个参数必须传空字符串，不能把 Verification Token 或 Encrypt Key 传进去。
- HTTP Webhook 模式在开启加密时，需要传 Verification Token 和 Encrypt Key 做验签、解密。
- `Start` 会阻塞，若 API 服务还要监听 HTTP，必须在独立 goroutine 中运行。
- `Start` 的 context 应绑定进程生命周期；收到 SIGTERM 后调用 `CloseAndWait` 做优雅退出。
- “每个初始化 client 算一个连接”，不能把长连接 client 放到每个 HTTP 请求内重复创建。

依据：

- [处理回调](https://open.feishu.cn/document/server-side-sdk/golang-sdk-guide/handle-callback)
- [SDK ws/client_lifecycle.go](https://github.com/larksuite/oapi-sdk-go/blob/a3550335752a9af4662314e5838138562f04aa70/ws/client_lifecycle.go#L33-L91)

## 8. 已确认、限制与未确认汇总

### 8.1 已确认

- 企业自建应用机器人可通过长连接接收新版交互卡片按钮回调。
- 官方 Go SDK 原生封装 WebSocket、鉴权、自动重连和回调分发。
- 发送卡片返回 `message_id`，可在 14 天内用同一应用身份调用 `Message.Patch` 更新共享卡片。
- 卡片 JSON 2.0 的输入框 + 表单容器 + 提交按钮可以把备注和按钮决策放在同一次回调中提交。
- 最小发送和更新权限可统一申请 `im:message:send_as_bot`。
- 长连接场景无需公开公网入站端口。

### 8.2 关键限制

- 长连接仅支持企业自建应用。
- 只支持新版 `card.action.trigger`，不支持旧版 `card.action.trigger_v1`。
- 每个应用最多 50 个连接，多客户端随机竞争消费。
- 回调处理必须控制在 3 秒内。
- JSON 2.0 卡片交互和更新有效期均为发送后 14 天。
- 卡片 JSON 和更新内容均不得超过 30 KB。
- `Message.Patch` 要求更新前后 `config.update_multi=true`，且调用身份与发送身份一致。
- 备注输入依赖飞书 V6.8 及以上客户端。

### 8.3 未确认

- 事件超时重推的具体次数、间隔和退避策略。
- 长连接完全断开期间飞书侧消息的缓冲时长和最终丢弃条件。
- 单个 `behaviors[].value` 除整卡 30 KB 外是否还有未公开大小限制。
- 本次仅完成官方文档和 SDK 源码核实，未在真实企业租户中完成端到端真机验证。

## 9. 对 OPEN-001 的最终判断

【结论】`OPEN-001` 的能力问题已经解决，可以关闭。

实现定案应为：

1. 配置企业自建应用机器人，并使用新版卡片回传交互 `card.action.trigger`。
2. 在 API 进程中用官方 Go SDK 建立一条长连接，不用公网 Webhook。
3. 卡片使用 JSON 2.0，并显式设置 `update_multi=true`。
4. 备注输入框和两个审批按钮放在同一表单容器中；按钮决定放 `action.value`，备注从 `action.form_value` 读取。
5. 回调在 3 秒内返回，只做身份校验、数据校验、幂等判断和轻量 Signal 转交。
6. 发卡后保存 `message_id`；Workflow 终态使用 `Message.Patch` 更新卡片。
7. 后续真机联调仅验证租户配置、客户端版本和实际分发行为，不再把“长连接能否接收卡片按钮回调”视为未决能力。

## 10. 官方来源清单

- [卡片回传交互](https://open.feishu.cn/document/feishu-cards/card-callback-communication)
- [配置卡片交互](https://open.feishu.cn/document/feishu-cards/configuring-card-interactions)
- [处理卡片回调](https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/handle-card-callbacks)
- [处理回调（Go SDK）](https://open.feishu.cn/document/server-side-sdk/golang-sdk-guide/handle-callback)
- [处理事件（Go SDK）](https://open.feishu.cn/document/server-side-sdk/golang-sdk-guide/handle-events)
- [调用服务端 API（Go SDK）](https://open.feishu.cn/document/server-side-sdk/golang-sdk-guide/calling-server-side-apis)
- [发送消息](https://open.feishu.cn/document/server-docs/im-v1/message/create)
- [更新已发送的消息卡片](https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/im-v1/message/patch)
- [发送卡片](https://open.feishu.cn/document/feishu-cards/send-feishu-card)
- [更新卡片](https://open.feishu.cn/document/feishu-cards/update-feishu-card)
- [使用指定应用发送飞书卡片](https://open.feishu.cn/document/feishu-cards/quick-start/send-feishu-cards-with-app-bots)
- [输入框组件（JSON 2.0）](https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/card-json-v2-components/interactive-components/input)
- [表单容器（JSON 2.0）](https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/card-json-v2-components/containers/form-container)
- [按钮组件（JSON 2.0）](https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/card-json-v2-components/interactive-components/button)
- [官方 Go SDK 仓库](https://github.com/larksuite/oapi-sdk-go)
- [本次审计的 SDK commit](https://github.com/larksuite/oapi-sdk-go/commit/a3550335752a9af4662314e5838138562f04aa70)
