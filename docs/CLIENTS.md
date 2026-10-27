[简体中文](CLIENTS.md) | [English](CLIENTS.en.md)

# 独立助手 Client

每个 AI 助手有自己的 Client，通过本机 Hub 领取明确发给自己的消息并报告投递。当前定址邮箱由 Codex Desktop MCP 与 WorkBuddy CLI 实现；Claude Code mod 0.2.2 也通过原生工具实现邮箱 1，2026-10-20 已验证 macOS 原 Code 会话空闲执行并回到忙碌 Codex 原聊天；Grok Routine Client 0.2.0 实现 queue 邮箱 1，真实官方 Routine 收件、执行与显式回信闭环仍为 **pending（待验收）**。Go Hub 不规划任务、不生成回复；Client 翻译该助手真实支持的原生入口。替换一个 Client 不需要重新编译 Hub，也不需要让其他助手退出。

协议把 Client 与 Hub 分开维护，Client 可以是独立进程，也可以由原生助手加载。没有使用 Go 动态库，也没有把 Python 嵌入 Hub。Client 可以使用不同语言，只要遵循同一协议；当前已有 Python 实现，Codex Desktop MCP 的独立 Node 发送已实测，常驻 MQ Client 已提供并通过本地模拟。

## 目录和技术栈

```text
clients/
  shared/             # 标准库 HTTP、SSE、回执与通知格式
  codex/              # 定址邮箱 Desktop MCP / 旧 queue 与 app-server 兼容
  workbuddy/          # 官方 CLI 会话恢复 / localassistant OpenAPI
  grok-routine/       # 用户配置的官方 Routine webhook
  claude-code/        # 原会话内 mod、原生 report 工具与本地 marketplace
  tests/              # 本地模拟传输测试，不调用真实助手
```

现有 Python Client 在各助手目录维护 `manifest.json`、`main.py`、`adapter.py` 和 `client.example.json`；其他语言实现维护自己的入口。`manifest` 声明协议版本、启动入口、会话类型和允许的发送模式，不是原生能力的验证报告。Client 必须在用户的具体电脑、助手版本和目标会话中完成空闲唤醒验收，才能把对应 Hub endpoint 的 `idle_wake_verified` 设为 `true`；忙时结果收件单独验证。

### 单仓库先行，以后拆分

第一版在同一仓库维护 Go Hub、各助手 Client 和 shared runtime。WorkBuddy 与 Codex Client 为 `0.3.0`，Grok Routine 为 `0.2.0`、Claude Code 为 `0.2.2`，接口协议都为 `1`，邮箱能力单独声明。Python 入口使用 shared `0.2.0`，Codex 在 `alternate_entrypoints.python` 中单独声明；`clients/shared/manifest.json` 记录该运行库版本，它不能作为助手 Client 启动。Codex 的 Node Desktop MCP Client 使用 Node 22+ 标准库，无需 npm 或 Python shared；原有 Python queue/app-server/Desktop MCP 路径保留各自边界。

Client 具有独立维护边界。Python 实现单独复制或发布时，必须一起提供其指定版本的 `clients/shared`，并保持同一父目录结构；Node Desktop MCP Client 无此 Python 依赖；Claude Code mod 只依赖宿主 mods API，直接分发完整插件目录与 marketplace。建议固定仓库提交以得到匹配的源码与 manifest。版本约束是当前维护约定，不是自动下载或依赖解析功能。后续需要分别发版时，可以把 shared 打包成固定版本运行库，再拆分 Client 仓库；协议无需因此改变。

Go Hub 本身不需要 Python 或 Node.js。Python Client 需要 Python 3.11+，没有第三方 Python 包依赖；Codex Desktop MCP Client 使用 Node 22+，本机 macOS 验证还要求已核实的官方已签名 Node 与自然宿主启动链。Claude Code 插件当前以 2.1.299 开发与校验，无额外 Node/Python 运行时。当前没有自动安装运行时、后台服务托管或自动 OAuth 续期；这些是安装体验的后续工作，不改变 Hub 协议。

## 当前定址邮箱

Hub binding 显式设置 `mailbox_protocol_version:1`，原生 Client 才领取 `/v1/mail/inbox`；否则沿用旧版 inbox。用户还需为发送者配置 `allowed_peers`；`visible_peers` 只控制技能目录可见性。默认关闭、不因安装技能或发现 MCP 工具而自动接入。

核实双方原会话与唤醒通道后，在各自完整 binding 中填写：

```json
{
  "endpoint": {"mailbox_protocol_version": 1},
  "visible_peers": ["workbuddy"],
  "allowed_peers": ["workbuddy"]
}
```

这是 Codex binding 的字段片段，不是完整配置；WorkBuddy 对应列表填 `codex`。仓库 `config.example.json` 始终关闭邮箱、清空互通名单并禁用未绑定助手，安装时由用户明确选择。只有核实该入口支持忙时引导，才另设 `mailbox_while_busy_verified:true`。


| Client / 传输 | 邮箱协议实现 | 原生行为与边界 | 原生闭环验收 |
| --- | --- | --- | --- |
| Codex Desktop MCP | `1` | `send_message_to_thread` 向固定既有聊天直接引导，支持 `request/reply/result`；不回退 native queue；`status` 本地保存、不唤醒模型 | 按日期、会话与传输入口查阅[实测记录](NATIVE-VALIDATION.md) |
| WorkBuddy CLI | `1` | 官方 CodeBuddy CLI `--resume` 指定既有会话；完整 Envelope 经 stdin，真实进程结束并核对结果会话后才 ACK | 按日期、会话与传输入口查阅[实测记录](NATIVE-VALIDATION.md) |
| Codex queue / app-server | `0` | 保留旧兼容通道，不以排队、独立 SDK 或另一会话冒充原聊天的邮箱唤醒 | 旧协议记录不覆盖 V1 邮箱 |
| WorkBuddy OpenAPI | `0` | 保留官方本地助理候选入口；需本人 OAuth，不能定位普通 Chat 的原 `session_id` | 独立候选入口，单独验收 |
| Claude Code mod 0.2.2 | `1` | 会话内 queue、固定身份原生 `peers/mail_send/mail_state`、租约保护事件长轮询，不经 Bash 或自动回复，不支持忙时 steer | 2026-10-20 已通过 macOS 原 Code 空闲执行与忙碌 Codex 回传 |
| Grok Routine 0.2.0 | `1` | 仅 queue，投递 `request/reply/result`，`status` 本地确认；旧 Task/Report 仍可兼容，不支持忙时引导、steer 或 interrupt | **pending**：真实官方 Routine 收件、原生执行与固定身份显式回信闭环待验收；Webhook 2xx 只确认传输 |

`mode:"queue"` 是当前邮箱 Envelope 的格式，不决定原生发送方式。`mailbox_while_busy_verified` 仅在实际核实该传输能忙时引导后开启，与旧 `result_queue_while_busy_verified` 分开。邮箱不要求父任务、等待状态、执行 Report 或 settled；原生会话状态与在途传输记录决定容量。新消息不会自动生成任务或回复，也不会沿固定父子树返回。

助手自己发送带相同 `correlation_id` 的邮件，明确指定接收者；`root_origin` 保留最初输入的会话供技能选择最终回传位置，`reply_to` 只是关联。Client 不做分工或替模型回信。当前平台验证结论须结合[按日期记录](NATIVE-VALIDATION.md)，旧协议原生测试不自动覆盖新邮箱的完整闭环。

## 旧版原生接入与验收记录

以下保留 Task/Report 兼容模式的实际过程。它的运行标识、资源和自动 result 返回不适用于新邮箱。

WorkBuddy Client 明确选择 `native.transport="cli"` 或 `"openapi"`，不自动降级。2026-10-14 原配置的登录错误、10 月 15 日认证续聊成功、查询的本地权限拒绝及远端 `850003 / authorization expired` 均保留为历史。用户随后重新授权，本次直接绑定原 endpoint、会话、工作目录和 `.workbuddy`，原生单次联系人 search 已返回候选，候选字段与成果一致，并完成主动回报和 MQ 自动忙时返回。首轮已投递但模型受旧诊断限制未接手，核对零工具、文件与 Report 后，由入口核对并显式续接一次后才执行；保留同任务、锁与已投递状态，没有 requeue 或伪造回执。Client 加入固定的当前交接 positional 指令，完整 Envelope 仍经 stdin，未覆盖工具、权限、MCP 或模型。这项验收限定指定联系人候选，不代表全通讯录唯一或完整桌面工具继承。[按日期记录](NATIVE-VALIDATION.md)

本轮 macOS arm64 的真实 MQ 验收已有原生执行与文件证据。首个任务在原生明确拒绝且确认未执行后，修正了技能读取、真实验收工作区读写与专属报告 helper 的窄范围配置，并按绝对工作目录恢复；原生会话主动发出 `accepted/running/succeeded`，文件 SHA-256 核对一致。第二个任务由 SSE 自动领取，默认 `resume` 同一已登录的独立 CLI 会话，不传工具或权限覆盖参数，首次执行即产生三种原生回执和新版本成果文件，SHA-256 同样一致。

两个任务使用不同的任务执行 ID；结果分别有新的 `message_id`、正确的 `reply_to` 和 `correlation_id`。重复提交同一 Submission 与已保存的原生 Report 均去重，没有新消息或重复执行，终态资源已释放。`delivered` 仍不等于业务成功；此次业务成功依据是原生回执和真实文件核验。

2026-10-14 的两个结果由运行中的 Codex 入口调用官方桌面工具，以 `send-now` 引导回原聊天。2026-10-15，独立 Node 客户端在自然宿主环境中通过官方 Desktop MCP 初始化并发现 57 个工具，真实调用 `send_message_to_thread`，得到 `isError=false` 和匹配 thread ID 的 ACK；消息也已作为 `codex_delegation` 立即进入忙碌原聊天，不需人工点击，未使用 native queue。

同机 Python 父进程链曾遇 macOS 签名校验失败，官方 Node 启动链则通过；该结果只限定这些实际启动链。App Server proxy 是另一条通道，其所需 daemon 控制 socket 未开放，不代表 Desktop MCP 不可用。随后常驻 Node MQ Client 0.2.0 在已订阅后完成新的真实任务链路：MQ 自动交接到固定独立 WorkBuddy CLI 会话，原生 accepted/running/succeeded 与成果文件核验后，新的关联 result 被 Node Client 自动领取并经官方 Desktop MCP 立即送入忙碌原聊天。没有当前入口或其他操作者手工转发、native queue 或人工点击，终态资源释放已核对。两轮真实 MQ 忙时返回均通过；第二轮已交付修正因果顺序的成果。两份当前文件与 Core SHA-256 匹配，使用不同 hubrun 执行标识，对应四条 task/result 消息均 delivered，资源已释放。随后相同 Desktop MCP 通道的单次空闲诊断也已通过：独立官方 Node 程序仅延时一次、检查一次原聊天 idle 与旧轮次 completed，再发送一次；原聊天实际启动了不同的新轮次，官方桌面 read_thread 仍可调用。该诊断本身没有新增 MQ 业务任务，不能据此宣称 idle 下完整 MQ 回路通过。原 WorkBuddy 会话随后完成的单联系人业务另有入口核对后显式续接一次记录，自动回传也发生在原入口忙碌时。[分段验收证据](NATIVE-VALIDATION.md)

CLI 依据为 [官方无头模式](https://www.workbuddy.cn/docs/cli/headless)。OpenAPI 依据为 [官方 OpenAPI](https://open.workbuddy.cn/docs/openapi) 和 [本地助理功能说明](https://www.codebuddy.cn/docs/workbuddy/From-Beginner-to-Expert-Guide/Function-Description/Assistant)。两种传输都不读取桌面 Cookie、缓存 token 或 IPC ticket，也不把应用内部鉴权当作公开接口授权。

## 配置和启动

先让 Hub 注册该 endpoint，在对应 binding 中设置 `"driver": {"type": "external"}`，由独立 Client 消费。不要同时启动两个 Client 领取同一 endpoint。原生身份参数放在 Client 的私有配置，Hub 的管理密钥不应放进去。

每次连接先通过 `GET /v1/endpoint?endpoint=ID` 核对接口协议 `1`、endpoint ID、启用状态、原生目的地及该传输是否支持邮箱。选定邮箱能力后，Codex/WorkBuddy 先订阅 SSE 再原子领取邮箱；Claude 的邮箱和旧版模式均使用事件长轮询。类型和绑定必须符合下表：

| Client | Hub 的 `endpoint.driver` | Hub 的 `endpoint.session_id` |
| --- | --- | --- |
| Codex | `codex` | 必须与私有配置 `native.thread_id` 完全一致 |
| WorkBuddy CLI | `workbuddy` | 必须与私有配置 `native.session_id` 完全一致 |
| WorkBuddy OpenAPI | `workbuddy` | 必须为 `localassistant`，表示持续专用本地助理 |
| Grok Routine | `grok-routine` | 必须与私有配置 `native.routine_id` 完全一致；这是用户为该 Routine 设置的标签 |
| Claude Code mod | `claude-code` | 必须与私有配置 `native.session_id` 及当前 `$.session.id()` 完全一致 |

这里的 `endpoint.driver` 是助手类型，和 binding 的外部传输类型 `driver.type="external"` 不同。协议不兼容、禁用 endpoint、会话或类型不匹配时停止消费，消息正文不能指定或覆盖目的地。会话状态 `unknown` 仍保留为未知，不会被 Client 改写为 idle；是否允许投递由 Hub 决定。每次断线重连都会重新验证绑定。

兼容 Task/Report 时，`endpoint.session_id` 是长期复用的目的会话；`Report.run_id` 是本次任务的执行标识，两者不能混用。新邮箱不使用执行 Report 或 run ID。接收会话优先回报实际原生 run/turn ID；宿主没有公开时，可以使用通知提供的任务专属 `hubrun-...`，并明确它属于 Hub。同一任务始终保留该标识，不跨任务复用固定会话 ID。

旧版兼容配置默认 `result_queue_while_busy_verified=false`。只有实际核实某个原生入口能在忙时接收结果通知后，用户才能对该 endpoint 开启它；此时 Hub 可在 `busy` 或 `needs_user` 状态投递 `kind=result`，仍会等待未决的传输尝试。它不允许忙时启动普通 `kind=task`，不释放当前任务资源，也不证明已实现 `steer`、打断或自动批准原生权限。`unknown` 状态不会因此变成可投递。

验证结论属于实际使用的传输入口。本轮 Codex 宿主内官方工具的结果引导成功，不证明独立 Python proxy 具备同样能力；换用外部传输后须重新核实。

先通过 Hub 管理 CLI 导出对应 endpoint 的私有配置：

```sh
agi-hub client-config --endpoint workbuddy --file /absolute/path/to/private/workbuddy/client.json
```

`--file` 必须是本机绝对路径；命令以 `0600`、`O_EXCL` 创建文件，不覆盖已有配置，不在终端打印凭据。它只导出该 endpoint 的 scope 配置和 `native: {}`，不导出 Hub 管理密钥或任何助手登录信息。Windows 的个人文件权限仍须按该平台的用户 ACL 管理。然后按该助手的 `client.example.json` 填写 `native` 字段。默认端口为 `7331`，实际 `base_url` 由本机 Hub 配置决定。

也可以根据对应目录的 `client.example.json` 手工建立仓库外的私有 `client.json`。顶层只接受四个字段：

```json
{
  "base_url": "http://127.0.0.1:7331",
  "endpoint": "workbuddy",
  "endpoint_token": "<由本机 Hub 管理入口签发的该 endpoint 凭据>",
  "native": {}
}
```

`base_url` 必须使用数字形式的本机 loopback 地址，不接受局域网地址、DNS 域名、嵌入凭据或额外路径。`endpoint_token` 访问自身邮箱、事件和回执，并仅向 `allowed_peers` 允许的接收者发送消息；不能切换 leader、改配置或创建管理任务。请求明确设置 Hub 要求的 Host、Origin 和 Authorization。独立 Python/Node Client 不使用环境代理，也不跟随 HTTP 重定向；Claude Code mod 使用宿主的 HTTP API，该 API 会跟随重定向，异源跳转会移除 Authorization 等非保留请求头，因此仍必须使用本机数字 loopback 的固定 Hub 地址。

在仓库根目录启动所需的 Python Client；Codex Node Desktop MCP 的启动方式见 [Codex Client](../clients/codex/README.md)，macOS 应使用该宿主已验证的官方签名 Node 绝对路径，不把 PATH 中任意 Node 当作相同启动链：

```sh
python3 clients/codex/main.py --config /path/to/private/codex/client.json
python3 clients/workbuddy/main.py --config /path/to/private/workbuddy/client.json
python3 clients/grok-routine/main.py --config /path/to/private/grok/client.json
```

Windows 可以使用 `py -3` 替换 `python3`，并提供该电脑上的绝对配置路径。Hub 的实际端口应与私有配置一致；示例端口不是自动发现结果。把个人配置文件限制为本机用户可读，避免提交真实 token、thread ID、聊天内容和成果文件。

### Claude Code mod 配置

Claude Code 不启动外部消费者进程。先用 `agi-hub client-config --endpoint claude --file <absolute-private-path>` 导出该 endpoint 的私有配置，再按 [Claude Code 示例](../clients/claude-code/client.example.json) 填写 `native.transport="mod"`、目标 `session_id`、`long_poll_ms` 和 `max_prompt_bytes`。插件选项只保存私有配置文件的绝对路径，不包含 token。非空启动选项优先；宿主已保存设置却传入空选项时，Client 通过 `$.plugin.name` 与 `$.settings.read()` 只读取本插件的路径，发现冲突路径则停止，不自行选择。

通过官方 `claude plugin install` 从本地 marketplace 安装 `agi-hub-endpoint@agi-hub-local`，再运行 `claude plugin configure agi-hub-endpoint@agi-hub-local --values-stdin`，从 stdin 提交 `{"client_config":"/absolute/private/path/client.json"}`，或使用宿主插件界面配置；已打开的目标 Code 会话运行 `/reload-plugins`，然后 `/agi-hub-id`、`/agi-hub-status` 核对原 UUID 和监听状态。可用 CLI 不在 PATH 时，使用用户当前桌面宿主提供的实际 Claude Code 二进制路径，不硬编码其他电脑的版本目录。[完整步骤](../clients/claude-code/README.md)

插件先取得 Hub 的 60 秒原子租约，续租并在提交 prompt 前确认归属。`/clear`、`/resume`、`/branch` 后重新检查当前会话；不匹配时休眠，不继续消费原 endpoint。持久投递记录按 Hub、endpoint、session 隔离；`outstanding` 仅显示本 endpoint 的未决消息供对账，不能当作重新投递队列。

邮箱模式领取前要求 `/v1/endpoint` 明确声明 `mailbox_long_poll_max_ms:25000` 和 `mailbox_client_lease:true`，避免旧协议 1 Hub 静默忽略等待和租约。使用 `/v1/mail/inbox?endpoint=claude&wait_ms=25000` 等待事件；修改邮箱的接口受租约保护。原生 `peers`、`mail_send`、`mail_state` 固定身份与租约，返回 JSON 文本。只有模型明确发送才产生业务回复，不由回合结束或 Report 自动生成。仅支持 queue，保持 `mailbox_while_busy_verified=false`。2026-10-20 已通过 macOS 原 Code 空闲邮箱执行与显式回传忙碌 Codex 原聊天；其他原生范围仍待验收。

旧版兼容中，`report` 工具只接受当前绑定接收过的 `kind=task`，自动固定 endpoint 和第一次报告时的真实 `claude-turn:<turn-id>`。同一任务后续回报保持该 run_id，相同 event_id 要求相同完整 Report；`kind=result` 只展示，不回报或执行。原生工具权限和用户授权保持有效，插件交接不能批准权限提示。

实际收件测试或原生诊断核实空闲唤醒后才能开启 `idle_wake_verified`；不要求额外付费运行一次 N2。忙时结果能力需另行验收。历史 N2/N3 与 2026-10-20 的 macOS Claude-3p 2.1.299 原会话邮箱闭环分别记录；后者为一个原生业务回合，但有三条计费模型响应 usage 记录，不是一次模型请求或零消耗。这些结果不替其他宿主开启验证标记，见[本机记录](NATIVE-VALIDATION.md#2026-10-20纯邮箱-claude-原-code-会话与-codex-回传)。

### WorkBuddy CLI 配置

[WorkBuddy 示例](../clients/workbuddy/client.example.json) 使用 `native.transport="cli"`。先核实用户选择的 CLI 配置目录和既有会话是否可用；需要登录时走官方本人登录流程，再填写配置。Client 只恢复已有 ID，不自动创建会话。`command` 是一个绝对可执行文件路径，不是命令字符串或 argv 数组。`cwd`、`config_dir` 均须为本机已存在目录的绝对路径；`session_id` 与 Hub 绑定完全一致。`timeout_seconds` 默认 `120`，接受 `1` 至 `600` 秒。

Client 将完整 Envelope 与对应邮箱或兼容协议说明写入本机临时普通文件，作为 stdin 传给 CLI；stdout 也使用临时普通文件接收，完整解析后删除。不把任务正文放进命令行或 shell。调用参数固定为：

```text
<command> [<entry_script>] --resume <session_id> --print --output-format json --input-format text
```

实现不传工具开关、权限设置、MCP、模型或 profile 覆盖参数，也不传 `--no-session-persistence`；使用配置目录与会话的默认工作环境，续聊默认持久化。Client 设置 `CODEBUDDY_CONFIG_DIR`，但配置目录隔离不等于凭据存储隔离，官方 CLI 可能使用产品自己的共享认证存储。不能把一个 `config_dir` 当成账户安全边界。

失败不代表历史没有变化。2026-10-14 原桌面会话的一次默认持久化恢复尝试虽然返回需要登录，仍追加了原历史；当时核对确认原有前缀完整保留。2026-10-15 同一原配置与会话的默认认证续聊已通过，不能继续把旧登录错误写作当前结论。遇到这类情况保留不确定投递记录、核对历史与原生结果，不能据失败或空回复盲目重发。

可选 `environment` 只接受以下字符串值，不接受任意环境变量：

| 键 | 允许值 |
| --- | --- |
| `CODEBUDDY_FORCE_LITE_WB_BUNDLE` | `"0"`、`"1"` |
| `DISABLE_AUTOUPDATER` | `"0"`、`"1"` |
| `CODEBUDDY_INTERNET_ENVIRONMENT` | `"internal"`、`"external"`、`"ioa"`、`"cloudhosted"`、`"selfhosted"` |

示例使用 `internal` 站点，应改为本人实际完成官方登录的站点；这个值选择 CLI 服务环境，不代表新增或继承桌面工具。Bundle 标志须匹配实际安装入口。不要将 token、桌面 IPC 参数或权限覆盖放入 `environment`。

Windows 必须把 `command` 配成实际 `.exe`，拒绝 `.cmd` / `.bat`。若官方安装入口是 JavaScript，可以使用实际 `node.exe` 加可选 `entry_script`，例如 `command="C:/Program Files/nodejs/node.exe"`、`entry_script="C:/absolute/path/to/official/codebuddy.js"`。脚本须为本机现存普通文件，扩展名为 `.js`、`.mjs`、`.cjs`，或文件名为 `codebuddy`；仍以独立 argv 调用，不经过 shell。该方式需要用户已有 Node.js 和官方 CLI 安装，不会自动安装运行时。

### WorkBuddy OpenAPI 配置（邮箱关闭）

保留 `native.transport="openapi"` 作为独立选择；旧配置没有 `transport` 字段时仍按 OpenAPI 解释，不会自动尝试 CLI。用以下 `native` 对象替换 CLI 配置中的整个 `native`，不要混用两种传输的字段：

```json
{
  "transport": "openapi",
  "access_token": "<short-lived access_token from the user's official OAuth grant>",
  "oauth_scopes": ["user.localassistant.invokable"],
  "timeout_seconds": 30
}
```

OpenAPI 使用 `Authorization: Bearer access_token`，发送 `{"content":"完整通知","msg_type":"text"}`，需要 `user.localassistant.invokable`。`oauth_scopes` 是用户配置的授权范围记录，平台仍负责实际鉴权。缺少 token 或 invokable 范围时，Client 在领取消息前返回 `unconfigured`。这里没有应用密钥、refresh token 或自动刷新流程；稳定长期接入仍需完成官方应用授权及续期设计。Token 失效后的已尝试投递保留为不确定，不自动发送第二次。CLI 本人登录不配置第三方 OpenAPI 授权。

### Grok Routine 配置（0.2.0，queue 邮箱 V1）

旧 Client 0.1 不能消费 V1 邮箱。必须实际替换为完整的 Grok Routine Client 0.2.0，并保留匹配的 `clients/shared`；仅修改 manifest 或 Hub 的 `mailbox_protocol_version` 不能升级旧程序。0.2.0 根据 binding 二选一消费 V1 邮箱或旧 inbox，不混用；正常协作使用 V1，旧 Task/Report 仅保留兼容。

1. 按 [Grok Client README](../clients/grok-routine/README.md) 准备并核实本人的官方 Routine webhook 与真实请求结构。URL、请求头和凭据只保存在本机私有文件，不贴聊天、不提交仓库。
2. 用上文的 `client-config` 命令导出此 endpoint 专属的私有配置，参照 [示例](../clients/grok-routine/client.example.json) 填写 `native.url`、`native.body`、`native.headers` 和 `native.routine_id`。`routine_id` 是用户为该官方 webhook 目标设置的稳定标签，必须与 Hub 的 `endpoint.session_id` 完全一致。
3. 使用 `endpoint.driver="grok-routine"`、binding `driver.type="external"`、`mailbox_protocol_version:1` 与 queue 模式；由用户明确配置 `allowed_peers` 和 `visible_peers`。先保存并启用 binding，再运行独立 Client；禁用的 endpoint 会被 Client 拒绝。启用或启动进程不代表原生已连接，当前不启用空闲唤醒或忙时验证标记。
4. 按 README 中的安装与技能文档准备**固定身份 producer helper 和协作技能**。目标 Routine 必须能读取技能并调用该本机 helper，才能以自己的 endpoint 主动回信；不猜测 Grok 的原生技能目录。助手明确选择收件人、沿用 `correlation_id` 并按需设置 `reply_to`，Hub 和 Client 不代写业务回复。真实官方 Routine 的 V1 收件、执行与回信闭环仍为 **pending**；Webhook 2xx 不证明原生执行、读取技能或主动回信成功。

`native.body` 必须包含完整消息。`"{{envelope}}"` 替换为 JSON 对象，`{{envelope_json}}` 替换为完整 JSON 字符串，`{{notification}}` 替换为完整通知，V1 使用邮箱回信指引，旧 Task/Report 使用报告指引。`{{content}}`、`{{message_id}}` 等局部占位符不能代替完整消息；`{{task_id}}` 仅适用于旧任务。替换仅进行一轮，消息内容中的占位符不会被再次解释；对象占位符必须独占整个 JSON 字符串值。仅支持 queue，不支持忙时引导、steer 或 interrupt；本机真实验收前保持 `idle_wake_verified` 及各忙时验证标记为 `false`。

### Codex 程序配置

Desktop MCP、queue 与 App Server proxy 是不同通道，配置与验收分别进行。2026-10-15 独立 Node 启动链已真实调用官方 Desktop MCP 并向忙碌原聊天即时引导消息；旧版常驻 MQ Client 0.2.0 曾用于该验收；当前 0.3.0 增加定址邮箱，使用 Node 22+，启动说明见 Codex 目录，不以内部模型工具调用充当独立 Client 的成功证据。仅使用固定目标会话与允许的发送工具，发现全部工具不等于获得调用它们的授权。

Python 父进程链在本机 macOS 签名校验失败，官方 Node 链通过；不要因 App Server proxy 缺少 daemon socket 推断 Desktop MCP 也不可用。不新建另一套 daemon，不复制登录信息或访问未公开 IPC 来替代目标聊天。

以下为原有 queue/app-server Python 实现的程序配置边界：

Codex 的执行入口在启动 Client 时解析并保存为绝对路径，投递时不会重新从 PATH 查找。Windows 必须配置实际的 `codex.exe`；Client 明确拒绝 `.cmd` / `.bat` 包装器，以避免消息参数经过 shell 解释。Hub 的 `endpoint.session_id` 与 `native.thread_id` 必须指向同一个已核实原生 thread。

## 邮箱消费、投递回执与助手回复

1. Client 核对绑定与邮箱能力，再读持久化邮箱；Codex/WorkBuddy 先订阅 SSE，Claude 使用事件长轮询。Hub 原子将 `pending` 改为 `dispatching`；同 endpoint 已有 `dispatching/uncertain` 时不再领取另一条消息。
2. `status` 仅保存本地记录并 ACK，不进入模型。`request/reply/result` 经原生传输尝试一次。WorkBuddy CLI 使用既有会话；Codex Desktop MCP 使用固定既有聊天，直接引导、不改用 native queue；Claude 提交到绑定的原 Code 会话。
3. WorkBuddy CLI 实际退出且完整结果的 `session_id` 匹配、非错误，Codex MCP 确认输入，或 Claude 在原生主会话 `turn.start` 观察到精确消息 marker 后，Client 才发送 `/v1/mail/delivery`。ACK 仅是传输证据，不证明业务成功。模型通过另一条邮箱消息表达进度、需本人处理或成果，不能提交原消息的 delivery。
4. 收件助手如需回复，自己调用固定身份的 `mail-send` helper 或 Claude 原生 `mail_send` 工具，使用新 ID、原 `correlation_id`、必要 `reply_to`，明确选择用户允许的接收者。可以直接给最初入口，不要求回上一个发送者；结束模型回合不会自动回复。

首次发布默认最多 16 条、30 分钟；状态也计数。`mail-state` 只列调用方发出/收到的投递记录；可读其自身可见 `status` 的正文，其他 `request/reply/result` 仍仅显示投递元数据，不复制任务上下文。

SSE 保活不查询业务任务；重连退避后只取新的 `pending`。网络尝试后缺少确认、CLI 已启动但超时、结果残缺或会话不匹配等，保留 `uncertain`，不重新发送。只有 Client 重启时，已有 claim 保持原状态；Hub 重启将 `dispatching` 转为 `uncertain`。回执未确认则停止并核对，不能清记录、换 ID 或伪造原生空闲绕过在途占用。

文件引用必须标明设备、工作区和相对路径；邮箱不自动复制文件或验证业务成果，最终入口按需求核验。原生权限提示仍交给用户，安装技能或消息不能批准工具调用。新模式不需要 Report helper；若使用固定 endpoint 的消息 helper，它只调用本机邮箱与目录接口，不触发其他隐藏工具。

### 旧版 Task/Report 兼容

只有旧 `kind=task/control`、没有 `mailbox:true` 的信封继续由原生助手报告 `accepted/running/needs_user/succeeded/failed`；旧协议终态生成返回原入口的 `kind=result`。旧结果只展示。Claude 原生 `report` 工具和 `clients/shared/report.py --config ... --file ...` 仅用于此兼容路径，不能向新邮箱邮件提交执行回执。历史验收见上文和[完整记录](NATIVE-VALIDATION.md)。

## 单独更新和拔插

当前实现支持独立进程的单独启动、停止和替换，Hub 协议不变；还没有一个替用户管理全部 Client 进程的 supervisor，也没有自动下载、验证或执行陌生插件。

Claude Code 插件通过官方插件更新或重新安装后，在目标会话 `/reload-plugins`；它会停止旧监听、重取租约、核对持久记录，不把热重载当作重新发送任务。已领取消息必须先明确投递状态，不能靠重载清除不确定记录。

安全更新时先暂停向该 endpoint 提交任务，让已领取消息拿到明确投递回执。任务已经交给原生助手后，停止 Client 不会停止原生助手本身；新邮箱中的原生助手仍需自行发消息回传，旧任务沿用 Report。停止旧 Client 后替换该助手目录，再启动新进程，其他 Client 和 Hub 可以继续运行。

如果更新时存在 `dispatching`、`uncertain` 或未保存回执，应先核对原生执行记录，再恢复消费。不要通过清空记录、改成 idle 或提交新消息 ID 绕过不确定状态。中途强制停止 Client 只能进入保守恢复流程，不能被称为无损热更新。

新 Client 需要实现 `validate_binding(endpoint)` 和 `deliver(envelope) -> (outcome, detail)`，声明真实 `session_kinds`、邮箱能力及发送模式，再复用 shared runtime 或独立实现协议。邮箱能力 `1` 必须先完成原生绑定与唤醒验证；旧传输不能仅增加标记就消费新邮件。绑定验证必须核对原生类型和会话目的地，不能让任务正文改写配置。Hub 和 Client 的协议版本是 `1`，不兼容升级明确拒绝。Python 文件替换不改变原生工具登录态；独立 CLI/SDK 会话也必须在 manifest 及支持说明中明确标注。

## 多平台与多电脑

Python Client 使用标准库 HTTP、子进程和文件 API；Codex Desktop MCP 的新路径使用 Node runtime，Claude Code 使用宿主内的 mods API。Go 核心可构建为原生程序，各 Client 的运行时由需要它们的用户选择安装。Windows、macOS、Linux 的原生 CLI、官方 MCP 启动链、签名与环境检查分别验收；某一语言或某一宿主启动成功不能直接推广到其他环境。

当前 Hub 接口仅在本机 loopback 使用。另一台电脑的助手需要在那台电脑上运行自己的 Client，并采用后续经过授权的跨设备传输；本机 SSE 不自动成为远程消息服务。文件引用带有 `device_id`、`workspace_id` 和工作区内的相对路径，不会自动复制文件，也不能把本机路径直接交给远程助手使用。

## 验证

Claude 离线检查使用 Node 状态机测试及官方 `plugin validate` / `plugin test`。`claude -p` 的斜杠命令不保证跳过模型；本次原本只查配置的探测实际触发了模型，不再将它作为零成本预检。见[事件与实测用量](NATIVE-VALIDATION.md#2026-10-20纯邮箱-claude-原-code-会话与-codex-回传)。

```sh
python3 -m unittest discover -s clients/tests -v
```

测试使用本地假 HTTP 和模拟子进程，检查订阅/领取顺序、Host/Origin、拒绝重定向、传输不确定状态、一次原生尝试、失败回执后停止、结果通知语义、WorkBuddy CLI 绑定与输入输出、OAuth 配置边界及模板的一次替换。它不会发送真实助手消息、读取联系人或证明任何原生平台已接入。

新邮箱真实验收先用低成本、本地文件任务，核实原会话唤醒、助手明确选择接收者、关联回复、成果核对、最终回到最初入口及不确定投递恢复。旧版接手/执行 Report 的证据保留，不冒充新邮箱业务完成。访问业务系统或向联系人发送通知仍需具体人类指令和原生应用要求的本人确认。
