[English](PROTOCOL.md) | [简体中文](PROTOCOL.zh-CN.md)

# 消息与回执协议

当前协作使用定址邮箱：助手通过技能选择接收者，Client 唤醒该接收者的原生会话，Hub 只保存消息、投递证据、去重及基础限额。下面的 ID 均为示例。旧版 Task/Report 保留兼容，见本文后半部分；它不是新邮箱消息的处理流程。

## 定址邮箱

```json
{
  "id": "mail-example-001",
  "recipient": "workbuddy",
  "kind": "request",
  "content": "请核对本次提供的资料，只返回必要结论；文件属于 this-device 的 work 工作区。",
  "origin_ref": "human-request-example",
  "max_messages": 16,
  "deadline": "2030-01-01T12:00:00Z"
}
```

调用 `POST /v1/mail/send?endpoint=codex`，或保存上述 JSON 后运行：

```sh
agi-hub mail-send --data /absolute/private/agi-hub --endpoint codex --file /absolute/work/request.json
agi-hub mail-state --data /absolute/private/agi-hub --endpoint codex --correlation-id mail-example-001
```

| 字段 | 约定 |
| --- | --- |
| `id` | 调用方保存的消息幂等键；重试同 ID 必须保持相同内容 |
| `recipient` | 调用方明确选定的单个 endpoint；另发一条独立 ID 的消息即可联系另一成员 |
| `kind` | `request`、`reply`、`status`、`result`；类别描述内容，不产生 Hub 工作流 |
| `content` | 非空文本，最多 64 KiB；可放精简 JSON、任务摘要与明确设备/工作区的文件引用 |
| `correlation_id` | 后续消息沿用原值；首次省略时取该消息 ID，新关联也只能使用首条消息 ID |
| `origin_ref` | 首条消息必填的人类输入引用，由调用方提供；不是新的授权证明 |
| `reply_to` | 可选的相关消息 ID；须属于同一关联且对发送者可见，不决定接收者 |
| `max_messages` / `deadline` | 新关联默认 16 条、30 分钟，最多 64 条；后续继承，不能提高或延长 |

Hub 从登记的发送 endpoint 保存 `root_origin`：`endpoint`、`device_id`、`session_id` 和 `request_ref`。它帮助技能识别最初入口，**不会强制回复路径**。例如 A 发给 B，B 可以请求 D，D 可以明确回 B，也可以在用户允许互通的范围内直接发给 A。最终结果回到最初会话是技能与助手的协作约定，Hub 不代替助手生成消息或汇总成果。若选中的接收者就是原入口，而该 endpoint 已改绑其他设备或会话，Hub 拒绝误投，保留原上下文。

后续发送者必须已是该关联的参与者；双方都须启用邮箱协议 `1`，发送者的 `allowed_peers` 必须包含接收者。`visible_peers` 只控制目录可见性，不授予发送权。成员可调整分工，消息不能替代用户批准、共享其他成员权限或扩大原授权范围。

返回的 Envelope 带 `mailbox:true`、独立 `message_id`、`correlation_id`、`root_origin`、明确 `sender/recipient`，且 `task_id` 为空。`mode:"queue"` 是当前 Hub 信封格式，不要求原生入口使用排队：Codex Desktop MCP 仍直接引导，WorkBuddy 使用原会话 `--resume`。邮箱不创建 Task，不要求父任务、等待状态、运行 ID、执行 Report 或 settled 回执。

## 投递证据与限额

`pending → dispatching → delivered / uncertain / failed` 只描述传输。Hub 原子领取后保留同 endpoint 的在途占用；`dispatching/uncertain` 未核对前不再领取另一条消息，也不会自动重发。Hub 重启将未完成的 `dispatching` 保留为 `uncertain`。

Client 独占原生传输结果的回执：WorkBuddy CLI 完整退出并核对结果会话后、或 Codex Desktop MCP 确认原生输入后，才调用 `/v1/mail/delivery`。模型发出的 `status/result` 是另一条消息，不能把原消息的投递改成已完成。`delivered` 不代表模型接手、业务成功或成果质量通过；这些内容由助手明确发送并由原入口核验。

`request/reply/result` 仅在已核实空闲可唤醒，或已核实忙时可引导的原生入口领取；不根据业务阶段推断原生空闲。`status` 由 Client 记入本地记录并回执，不注入会话、不启动模型。所有发布都计入同一关联的共享消息总量，包括状态；达到上限或截止时间拒绝新发布。截止与取消不停止已发生的原生操作，也不承诺限制宿主内部 Token 或积分。助手应精简消息、避免确认循环，并在限额前返回必要结果。

`mail-cancel --endpoint <原入口> --correlation-id <ID>` 关闭该关联，只撤回尚未投递的消息。已投递、正在投递或不确定的消息保留真实状态，不伪造业务中断。相同 ID 的重试仍只返回已有记录。

## 当前邮箱 API

全部只监听本机数字 loopback，并校验 Host、Origin 和 endpoint 凭据；不能把本机地址或文件路径直接交给另一台电脑使用。

| 接口 | 操作 |
| --- | --- |
| `POST /v1/mail/send?endpoint=ID` | 向用户允许的明确接收者发布消息 |
| `POST /v1/mail/inbox?endpoint=ID[&wait_ms=N]` | 原子领取，可按事件等待最长 25 秒 |
| `POST /v1/mail/delivery?endpoint=ID` | Client 回报 `message_id/outcome/detail`，仅限自身接收的消息 |
| `GET /v1/mail/state?endpoint=ID&correlation_id=ID` | 查看关联上下文、额度及自己发送/收到的投递记录；仅可见 status 返回正文，其他邮件只列元数据 |
| `POST /v1/mail/cancel?endpoint=ID` | 原入口关闭关联，正文为 `{"correlation_id":"ID"}` |

状态查询仅对自己发出或收到的 `status` 返回正文；其他参与者的邮件与请求/回复/成果正文均不在状态查询中复制。

这些接口使用对应 endpoint 的凭据。`GET /v1/endpoint`、目录、规则和 SSE 事件接口继续可用；事件只通知有变更，完整消息通过邮箱领取。邮箱必须显式设置 `mailbox_protocol_version:1`；Codex Desktop MCP、WorkBuddy CLI 和 Claude Code mod 0.2.2 已实现；Claude 已于 2026-10-20 通过 macOS 原 Code 空闲邮箱执行与忙碌 Codex 回传，Grok 保持 `0`；其他原生范围分别验收。接口协议仍为 `1`，不要与磁盘日志版本混淆。首次成功发布邮箱消息将日志升级为 `3`，新版读取 `1/2/3`，旧程序应拒绝新版日志。未发布原型的 v2 记录仅保留诊断，不恢复其父子任务流程。

邮箱 `wait_ms` 沿用下文旧接口的 `1..25000` 校验、订阅后复查、取消和 HTTP 200 `null` 超时语义。Claude 邮箱模式先要求 `/v1/endpoint` 声明 `capabilities: {"mailbox_long_poll_max_ms":25000,"mailbox_client_lease":true}`；send/inbox/delivery/cancel 修改操作须携带有效 `X-AGI-Hub-Lease`，旧 Hub 缺少能力声明时在领取前拒绝。原生 `mail_send/peers/mail_state` 固定身份并返回 JSON 文本；普通 CLI 不能绕过租约，其他 driver 保持原行为。

## 旧版 Task/Report 兼容协议

下面记录原单次任务接口、执行回执及历史原生验收。新邮箱消息不使用该任务状态机，也不会由 Report 自动生成回复。

### 任务交接

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

### 消息关联与会话标识

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

### 消息发送模式

| 模式 | 目标关系 | 接收约束 |
| --- | --- | --- |
| `queue` | 新独立任务 | 已核实会话空闲、没有未完成或不确定的交接、资源可用后投递 |
| `steer` | 为现有运行补充方向 | 必须填写 `target_run_id`，原运行保持进行，原生 Client 必须支持引导 |
| `interrupt` | 请求停止现有运行 | 必须填写 `target_run_id`；收到实际 `interrupted` 回执才释放旧运行资源 |

不支持的模式明确拒绝，无自动替换。停止成功不代表已经启动替代任务；恢复暂停也不是当前协议的能力。模式可按每条消息指定，客户端能力配置约束允许的模式，当前默认方式为 `queue`。

Endpoint 的 `result_queue_while_busy_verified` 默认为 `false`，与 `idle_wake_verified` 分别核实。它仅表示该原生入口已验证能在 `busy` 或 `needs_user` 状态接收 `kind=result` 的返回通知。开启后，Hub 可投递这类结果，但仍阻止未决的 `dispatching/uncertain` 传输，不启动忙时普通任务、不释放业务资源、不改变当前运行；`unknown` 状态仍等待核对。这是结果投递能力声明，不等于已实现引导、打断、即时展示或自动处理权限提示。能力验证绑定具体传输：2026-10-15 本机独立 Node 经 Desktop MCP 的忙时即时引导已实测，App Server proxy 则因缺少 daemon 控制 socket 尚未接通；不能用一个通道的成功或失败代替另一个，也不能仅凭新增字段声称闭环通过；本机完整 MQ 忙时返回另有后续真实验收记录。

### 执行回执

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

### 投递与返回

投递状态独立记录：`pending → dispatching → delivered / uncertain / failed`。Client 领取时先持久化 `dispatching`。原生传输成功仅确认 `delivered`；原生执行回执才确认接手和业务状态。只有 Client 重启时，已有 claim 保持 `dispatching`；Hub 重启后遗留 `dispatching` 转为 `uncertain`。两者都保留资源占用，不自动重发。

接收端已用稳定运行标识主动报告执行后，迟到的传输超时或退出失败不能把已确认投递降级；匹配任务接收方和原生执行证据时按幂等空操作处理。仅传输确认、无运行标识的失败和结果投递冲突仍拒绝，不能吞掉所有错误回执。

正式结果生成一条 `kind=result` 消息，返回任务的原入口。它不是新任务，不重新执行，不产生新交接。结果投递状态与业务成功状态独立；原入口暂不可用时保留成果和待返回消息。

原生权限可能允许会话收件，却阻止读取技能元数据、执行报告或生成文件。本轮首个 WorkBuddy 独立 CLI 任务确认原生明确拒绝且未执行后，修正窄范围报告与文件权限、真实绝对工作目录并恢复；第二个任务则由 SSE 自动领取，默认恢复同一既有 CLI 会话，首次执行即完成。两次均有原生 `accepted/running/succeeded` 和 SHA-256 核验一致的真实文件，不能仅由传输回执推导这些状态。报告通道可用时由原生会话回报 `needs_user`；否则保留已有投递记录与原生提示供用户核对，不伪造业务状态。

两个任务的 `run_id` 不同，两个结果分别使用独立 `message_id`、正确的 `reply_to` 与同任务关联 ID；重复同一 Submission 或已保存 Report 去重，不追加消息或再次执行。原生终态后资源释放已核对。

2026-10-14 的两个结果经 Codex 宿主内官方工具实际引导回原聊天。2026-10-15，独立 Node 进程通过官方 Desktop MCP 完成 initialize/tools/list，发现 57 个工具，并真实调用 `send_message_to_thread`：`isError=false`、thread ID 匹配，且消息作为 `codex_delegation` 即时进入忙碌原聊天，无人工点击、未用 native queue。随后常驻 Node Client 已在先订阅、后提交的新任务中，自动完成领取、固定独立 WorkBuddy CLI 会话的默认恢复、真实执行回报、文件与 SHA-256 核对，以及独立 MCP 向忙碌原聊天返回关联结果；无手工转发或人工点击，资源已释放。本机两轮 MQ 忙时返回已通过。随后相同 Desktop MCP 的一次独立空闲诊断，先确认原聊天 idle、旧轮次 completed，再仅发送一次，实际启动不同的新轮次且官方桌面 read_thread 仍可调用；这确认该传输的本机空闲启动；该诊断本身没有新增 MQ 业务任务，也不验证 idle 下完整 MQ 回路。App Server proxy 缺少控制 socket 和 Python 父链签名校验失败是分别观测到的问题，不能推出 Desktop MCP 不可用。

同日原 WorkBuddy 会话先后经历认证续聊成功、本地 Bash/Read 拒绝及远端 `850003 / authorization expired`。用户重新授权后，本次原 endpoint、会话与配置的单联系人新查询已取得候选，姓名、部门、职务与成果一致。首轮 `delivered` 后模型受旧诊断限制未接手，已核对零工具、文件和 Report；入口核对后显式续接一次只在原 `delivered` detail 记录 operator continuation，保留同任务与锁，没有 requeue、新建 ID 或伪造 Report。随后才有真实 `accepted/running/succeeded`、Core 成果核验和新关联 result 自动回到忙碌原入口。固定当前交接指令不改变完整 stdin Envelope 或来源授权；这不代表首轮无人干预、全通讯录唯一匹配或完整桌面工具继承。

此前两轮独立 CLI 的 MQ 忙时回传，不同 hubrun、四条 task/result 消息 delivered、当前成果与 Core SHA-256 一致及资源释放均已核对，第二轮成果更正了上述因果顺序。后续原会话单联系人业务另有原始工具结果、文件核验、自动回传与资源释放证据；它不改变消息去重、来源授权或成果核对契约。idle 下完整 MQ 回路及其他平台仍未验收。[按日期验证记录](NATIVE-VALIDATION.zh-CN.md)

### 本机 API

所有接口默认只监听数字形式的 loopback 地址。管理凭据保存在私有配置中；endpoint 凭据由管理密钥和 endpoint ID 派生，不能据此修改其他客户端或 leader。

| 接口 | 操作 | 权限 |
| --- | --- | --- |
| `GET /v1/state` | 查看本机记录 | 管理 |
| `POST /v1/tasks` | 提交完整任务 JSON | 管理 |
| `POST /v1/leader` | 设置后续任务 leader | 管理 |
| `POST /v1/session` | 核实并更新会话状态 | 管理 |
| `GET /v1/endpoint?endpoint=ID` | 查询自身协议版本、会话绑定及状态 | 对应 endpoint 或管理 |
| `GET /v1/catalog?endpoint=ID` | 查询显式可见且已启用的成员、目录摘要及规则版本 | 对应已启用 endpoint 或管理 |
| `GET /v1/skills?endpoint=ID[&content=1&known_version=HASH]` | 查询规则摘要；仅按需返回正文，相同摘要不重复正文 | 对应已启用 endpoint 或管理 |
| `GET /v1/skill-release` | 本机安装器核对运行中 Hub 的规则修订与摘要 | 管理，只读 |
| `POST /v1/inbox?endpoint=ID[&wait_ms=N]` | 原子领取；可等待消息变更，最长 25 秒 | 对应 endpoint 或管理 |
| `POST /v1/client-lease?endpoint=ID` | 原子获取、续期或释放该 endpoint 的消费者租约 | 对应 endpoint 或管理 |
| `POST /v1/delivery?endpoint=ID` | 汇报投递证据 | 对应 endpoint 或管理 |
| `POST /v1/report?endpoint=ID` | 汇报原生执行及结果 | 对应 endpoint 或管理 |
| `GET /v1/events?endpoint=ID` | SSE 变更提示 | 对应 endpoint 或管理 |

SSE 首次连接发送一次变更提示，随后按事件推送；完整状态和消息通过授权接口读取。连接保活不轮询业务任务。独立 Client 应先订阅、再领取，重连后只领取仍为 `pending` 且满足条件的消息；已经领取但投递不确定的消息不会被重新领取。Hub 没有内置助手执行器，也不把 SSE 断开当作原生操作失败的证据。

目录是按需读取的短数据，不是完整状态：只列对应 binding 的 `visible_peers` 中当前已启用成员，默认空；不含原生会话地址、私有路径、凭据与历史。`purpose` 由本机用户配置，不是可执行指令或能力排名；验证字段仍是配置声明。目录可见性不赋予提交任务权限，不改变当前单次交接行为。更新规则文件、读取目录或版本不会创建消息、改变会话状态或启动模型。[技能维护与宿主加载](SKILLS.zh-CN.md)

Client 在连接与领取前先读取自身 `/v1/endpoint`。返回 `protocol_version: 1`、该 `endpoint`、`session` 和 `outstanding`；后者只包含该 endpoint 的 `dispatching/uncertain` 消息，供重启核对。它不是可领取列表，不返回其他 endpoint 或管理凭据。Client 校验产品类型、支持模式及原生会话 ID 与私有配置一致；不兼容协议或目标错配时拒绝消费，不能以不同会话执行后再冒认原绑定。

Client 和 Hub 约定协议版本 `1`。当前 Client 的 manifest 描述协议、会话类型与发送模式，Hub 不自动加载它或因此扩大 endpoint 的能力。每个 endpoint 只有一个活跃消费者，可由独立进程或原生会话内插件承载；不同 Client 可以用不同语言实现，必须遵守领取后不盲重发、回执去重和授权边界。原生配置与凭据属于 Client 的私有文件，不能加入 Envelope 或 Hub 配置。详见 [独立 Client](CLIENTS.zh-CN.md)。

#### 事件长轮询

省略 `wait_ms` 时保持原来的即时领取行为；`wait_ms=N` 仅接受一个十进制整数 `1..25000`，空值、重复参数、非整数或越界返回 HTTP 400。已有可处理消息立即返回完整 Envelope，否则订阅 broker 变更、复查后等待；消息到达立即领取，无消息等待至上限后返回 HTTP 200、JSON `null`。客户端断开会取消等待。

长轮询不会重新领取 `dispatching/uncertain`，不会改变资源或会话准入规则。两个等待请求最多一个领取同一条消息。Claude Code mod 无法读取 SSE 流，使用长轮询作为等价事件唤醒；它不查询原生业务历史。其他 Client 可以继续使用 SSE，不需要改动。

#### 原子消费者租约

Claude Code endpoint（`driver="claude-code"`）用 Hub 内的原子租约防止同一原会话在桌面与终端被两边同时消费，替代先写后读的非原子文件锁。请求示例只展示结构，不包含真实租约：

```json
{"owner_id":"consumer-instance-example"}
```

首次成功返回 `lease_id`、`expires_at` 和 `ttl_ms:60000`。同 owner 的获取幂等；续期提交相同 `owner_id` 和 `lease_id`，释放再加 `"release":true`。租约已被其他 owner 占用、旧代租约过期或 lease_id 不匹配时返回 HTTP 409。owner 是每次客户端加载的实例标识，不能只用固定 session_id。租约只存在于当前 Hub 进程，重启后必须重新取得。

Claude Code 的 inbox、delivery、report 请求必须带 `X-AGI-Hub-Lease: <lease_id>`，Hub 在同一临界区核对有效租约再执行变更；无租约或已失效返回 409。此约束也适用于管理凭据调用，不能用管理权限绕过活跃消费者。其他 driver 保持原有行为。租约不替代 endpoint token，不提供同一 OS 账号内的权限隔离。

客户端每次原生提交前重新确认租约。领取响应丢失、租约失效或本地缺少投递记录时，保留 Hub 的 outstanding 和任务锁供核对；不能重新领取或提交同一 prompt。已保存结果先写持久记录，再发送完全相同的 `outcome + detail`；重启只重发该投递回执。未知原生结果记录 `uncertain`，不把网络错误当作未执行证据。
