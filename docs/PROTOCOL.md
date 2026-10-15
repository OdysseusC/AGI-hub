# 消息与回执协议

V0 使用同一套 JSON 协议登记会话、领取消息和汇报结果。通知只是有消息可取的提示，完整内容以持久化记录为准。所有示例 ID 都是假数据。

## 任务交接

```json
{
  "id": "task-example-001",
  "origin": "codex",
  "recipient": "grok",
  "goal": "根据已授权资料整理一份摘要文件",
  "context": "只使用本次提供的资料，保留来源。",
  "inputs": [{"device_id": "this-device", "workspace_id": "work", "path": "input/brief.txt"}],
  "mode": "queue",
  "resources": ["workspace:work"],
  "require_artifact": true,
  "authorized_actions": ["read provided input", "write summary in work workspace"],
  "max_handoffs": 1,
  "deadline": "2030-01-01T12:00:00Z"
}
```

`origin` 省略时取当前 leader。任务创建后固定原入口、协调者和 leader 版本；切换 leader 不迁移旧任务。`id` 是调用方持久保存的幂等键，重复发送必须保留相同内容。文件引用使用所属设备、已配置工作区和 `/` 分隔的相对路径，提交前检查文件存在及可选 SHA256；输入文件仍是引用，不会自动冻结版本或跨电脑复制。

`authorized_actions` 是交接说明，不是新的用户批准或原生助手权限沙盒。V0 仅允许管理入口创建任务，普通 endpoint 凭据只能领取自己的消息和发送自己的回执；同一系统账号内具备完整文件权限的原生助手不属于额外隔离的安全域。

`max_handoffs` 在 V0 固定为 1，一次交接后仅返回结果。没有自动递归转派。`deadline` 限制尚未投递的消息；不会自行停止已经执行的原生任务，也不表示已落实 Token 或费用预算。

## 消息关联与会话标识

Hub 把提交转换为完整 Envelope，原生入口接收并保留以下字段：

| 字段 | 含义 |
| --- | --- |
| `message_id` | 这一条消息的独立标识；原交接与返回结果分别拥有不同值 |
| `task_id` | 所属任务，跨交接、回执与结果保持一致 |
| `correlation_id` | 对话链的关联标识；V0 等于原任务 ID |
| `reply_to` | 正在回答的消息 ID；原任务消息省略，结果指向原任务的 `message_id` |
| `sender` / `recipient` | 本次消息的 endpoint；结果由执行端返回任务保存的原入口 |
| `kind` | `task`、`control` 或 `result`，决定是否执行任务、请求原生控制或只展示结果 |
| `content` | 完整 Submission 或终态 Task 的 JSON 文本，包含必要上下文与成果引用 |

例如原交接的 `message_id=task:task-example-001`、`task_id=correlation_id=task-example-001`；结果的 `message_id=result:task-example-001`，`reply_to=task:task-example-001`，仍保留相同任务和关联 ID。结果收件人由 Hub 从已保存任务确定；接收助手不自行切换入口或创建新任务来回复。

`Endpoint.session_id` 是固定的原生目的会话，`Report.run_id` 是该任务的一次执行。多个任务可以进入同一会话，但不能把固定 `session_id` 用作多个任务的 `run_id`。优先使用宿主实际公开的 run/turn ID；没有公开时，可使用通知提供的任务专属 `hubrun-...`，明确它是 Hub 执行标识而非原生 ID。同一任务的所有执行回执须保持该标识一致；控制任务仍要求实际匹配目标运行的 `target_run_id`，Hub 标识本身不证明原生控制能力。

## 消息发送模式

| 模式 | 目标关系 | 接收约束 |
| --- | --- | --- |
| `queue` | 新独立任务 | 已核实会话空闲、没有未完成或不确定的交接、资源可用后投递 |
| `steer` | 为现有运行补充方向 | 必须填写 `target_run_id`，原运行保持进行，原生 Client 必须支持引导 |
| `interrupt` | 请求停止现有运行 | 必须填写 `target_run_id`；收到实际 `interrupted` 回执才释放旧运行资源 |

不支持的模式明确拒绝，无自动替换。停止成功不代表已经启动替代任务；恢复暂停也不是当前协议的能力。模式可按每条消息指定，客户端能力配置约束允许的模式，当前默认方式为 `queue`。

Endpoint 的 `result_queue_while_busy_verified` 默认为 `false`，与 `idle_wake_verified` 分别核实。它仅表示该原生入口已验证能在 `busy` 或 `needs_user` 状态接收 `kind=result` 的返回通知。开启后，Hub 可投递这类结果，但仍阻止未决的 `dispatching/uncertain` 传输，不启动忙时普通任务、不释放业务资源、不改变当前运行；`unknown` 状态仍等待核对。这是结果投递能力声明，不等于已实现引导、打断、即时展示或自动处理权限提示。能力验证绑定具体传输：2026-10-15 本机独立 Node 经 Desktop MCP 的忙时即时引导已实测，App Server proxy 则因缺少 daemon 控制 socket 尚未接通；不能用一个通道的成功或失败代替另一个，也不能仅凭新增字段声称闭环通过；本机完整 MQ 忙时返回另有后续真实验收记录。

## 执行回执

```json
{
  "event_id": "event-example-accepted",
  "task_id": "task-example-001",
  "endpoint": "grok",
  "state": "accepted",
  "run_id": "native-run-example"
}
```

状态依次为 `waiting`、`accepted`、`running`，可以进入 `needs_user` 再恢复 `running`，最终为 `succeeded`、`failed` 或 `interrupted`。每个回执使用独立、稳定的 `event_id`；相同事件重复提交去重，同 ID 不同内容拒绝。接手后的 `run_id` 必须一致。

```json
{
  "event_id": "event-example-completed",
  "task_id": "task-example-001",
  "endpoint": "grok",
  "state": "succeeded",
  "run_id": "native-run-example",
  "summary": "摘要已生成并核对来源。",
  "artifacts": [{"device_id": "this-device", "workspace_id": "work", "path": "output/summary.md"}]
}
```

成功需要摘要；任务要求文件时还需至少一个可验证文件。边车检查文件归属、路径边界、软链接、存在性及可选校验值，并记录实际 SHA256。它核对可访问的成果证据，成果质量仍通过任务验收判断。

## 投递与返回

投递状态独立记录：`pending → dispatching → delivered / uncertain / failed`。Client 领取时先持久化 `dispatching`。原生传输成功仅确认 `delivered`；原生执行回执才确认接手和业务状态。只有 Client 重启时，已有 claim 保持 `dispatching`；Hub 重启后遗留 `dispatching` 转为 `uncertain`。两者都保留资源占用，不自动重发。

接收端已用稳定运行标识主动报告执行后，迟到的传输超时或退出失败不能把已确认投递降级；匹配任务接收方和原生执行证据时按幂等空操作处理。仅传输确认、无运行标识的失败和结果投递冲突仍拒绝，不能吞掉所有错误回执。

正式结果生成一条 `kind=result` 消息，返回任务的原入口。它不是新任务，不重新执行，不产生新交接。结果投递状态与业务成功状态独立；原入口暂不可用时保留成果和待返回消息。

原生权限可能允许会话收件，却阻止读取技能元数据、执行报告或生成文件。本轮首个 WorkBuddy 独立 CLI 任务确认原生明确拒绝且未执行后，修正窄范围报告与文件权限、真实绝对工作目录并恢复；第二个任务则由 SSE 自动领取，默认恢复同一既有 CLI 会话，首次执行即完成。两次均有原生 `accepted/running/succeeded` 和 SHA-256 核验一致的真实文件，不能仅由传输回执推导这些状态。报告通道可用时由原生会话回报 `needs_user`；否则保留已有投递记录与原生提示供用户核对，不伪造业务状态。

两个任务的 `run_id` 不同，两个结果分别使用独立 `message_id`、正确的 `reply_to` 与同任务关联 ID；重复同一 Submission 或已保存 Report 去重，不追加消息或再次执行。原生终态后资源释放已核对。

2026-10-14 的两个结果经 Codex 宿主内官方工具实际引导回原聊天。2026-10-15，独立 Node 进程通过官方 Desktop MCP 完成 initialize/tools/list，发现 57 个工具，并真实调用 `send_message_to_thread`：`isError=false`、thread ID 匹配，且消息作为 `codex_delegation` 即时进入忙碌原聊天，无人工点击、未用 native queue。随后常驻 Node Client 已在先订阅、后提交的新任务中，自动完成领取、固定独立 WorkBuddy CLI 会话的默认恢复、真实执行回报、文件与 SHA-256 核对，以及独立 MCP 向忙碌原聊天返回关联结果；无手工转发或人工点击，资源已释放。本机两轮 MQ 忙时返回已通过。随后相同 Desktop MCP 的一次独立空闲诊断，先确认原聊天 idle、旧轮次 completed，再仅发送一次，实际启动不同的新轮次且官方桌面 read_thread 仍可调用；这确认该传输的本机空闲启动，不是第三条 MQ 业务任务，也不验证 idle 下完整 MQ 回路。App Server proxy 缺少控制 socket 和 Python 父链签名校验失败是分别观测到的问题，不能推出 Desktop MCP 不可用。

同日原 WorkBuddy 会话的两次默认认证续聊已通过。新联系人搜索先被本地 Bash/Read 拒绝；修正精确命令规则后，已用官方随包 wecom-cli 在原会话实际执行，原始工具结果返回远端 `850003 / authorization expired`，未取得联系人数据。当前需要该机器人创建者恢复通讯录授权；不能继续停留在“只卡本地权限”的旧结论，也不能从调用完成推导数据获取成功或完整桌面工具继承。

本轮两次 MQ 忙时回传均完成；不同 hubrun、四条 task/result 消息 delivered、当前成果与 Core SHA-256 一致及资源释放均已核对，第二轮成果更正了上述因果顺序。验收成果成功交付不代表联系人获取成功，也不改变消息去重、来源授权或成果核对契约。[按日期验证记录](NATIVE-VALIDATION.md)

## 本机 API

所有接口默认只监听数字形式的 loopback 地址。管理凭据保存在私有配置中；endpoint 凭据由管理密钥和 endpoint ID 派生，不能据此修改其他客户端或 leader。

| 接口 | 操作 | 权限 |
| --- | --- | --- |
| `GET /v1/state` | 查看本机记录 | 管理 |
| `POST /v1/tasks` | 提交完整任务 JSON | 管理 |
| `POST /v1/leader` | 设置后续任务 leader | 管理 |
| `POST /v1/session` | 核实并更新会话状态 | 管理 |
| `GET /v1/endpoint?endpoint=ID` | 查询自身协议版本、会话绑定及状态 | 对应 endpoint 或管理 |
| `POST /v1/inbox?endpoint=ID` | 原子领取下一条可处理消息 | 对应 endpoint 或管理 |
| `POST /v1/delivery?endpoint=ID` | 汇报投递证据 | 对应 endpoint 或管理 |
| `POST /v1/report?endpoint=ID` | 汇报原生执行及结果 | 对应 endpoint 或管理 |
| `GET /v1/events?endpoint=ID` | SSE 变更提示 | 对应 endpoint 或管理 |

SSE 首次连接发送一次变更提示，随后按事件推送；完整状态和消息通过授权接口读取。连接保活不轮询业务任务。独立 Client 应先订阅、再领取，重连后只领取仍为 `pending` 且满足条件的消息；已经领取但投递不确定的消息不会被重新领取。Hub 没有内置助手执行器，也不把 SSE 断开当作原生操作失败的证据。

Client 在连接与领取前先读取自身 `/v1/endpoint`。返回 `protocol_version: 1`、该 `endpoint` 和 `session`，不返回其他会话、任务或凭据。Client 校验产品类型、支持模式及原生会话 ID 与私有配置一致；不兼容协议或目标错配时拒绝消费，不能以不同会话执行后再冒认原绑定。

Client 和 Hub 约定协议版本 `1`。当前 Client 的 manifest 描述协议、会话类型与发送模式，Hub 不自动加载它或因此扩大 endpoint 的能力。每个 endpoint 使用一个 Client 进程；不同 Client 可以用不同语言实现，必须遵守领取后不盲重发、回执去重和授权边界。原生配置与凭据属于 Client 的私有文件，不能加入 Envelope 或 Hub 配置。详见 [独立 Client](CLIENTS.md)。
