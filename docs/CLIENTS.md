# 独立助手 Client

每个 AI 助手有自己的 Client 进程，通过本机 Hub 的 REST / SSE 协议领取任务、报告投递。Go Hub 保存消息和状态；Client 只翻译该助手实际支持的原生入口。替换一个 Client 不需要重新编译 Hub，也不需要让其他助手退出。

这是一种进程级插件接口。没有使用 Go 动态库，也没有把 Python 嵌入 Hub。Client 可以使用不同语言，只要遵循同一协议；当前已有 Python 实现，Codex Desktop MCP 的独立 Node 发送已实测，常驻 MQ Client 已提供并通过本地模拟。

## 目录和技术栈

```text
clients/
  shared/             # 标准库 HTTP、SSE、回执与通知格式
  codex/              # 官方 Desktop MCP / queue 兼容 / 结果专用 app-server
  workbuddy/          # 官方 CLI 会话恢复 / localassistant OpenAPI
  grok-routine/       # 用户配置的官方 Routine webhook
  tests/              # 本地模拟传输测试，不调用真实助手
```

现有 Python Client 在各助手目录维护 `manifest.json`、`main.py`、`adapter.py` 和 `client.example.json`；其他语言实现维护自己的入口。`manifest` 声明协议版本、启动入口、会话类型和允许的发送模式，不是原生能力的验证报告。Client 必须在用户的具体电脑、助手版本和目标会话中完成空闲唤醒验收，才能把对应 Hub endpoint 的 `idle_wake_verified` 设为 `true`；忙时结果收件单独验证。

### 单仓库先行，以后拆分

第一版在同一仓库维护 Go Hub、各助手 Client 和 shared runtime。WorkBuddy 与 Codex Client 为 `0.2.0`，Grok Routine Client 为 `0.1.0`，都使用协议 `1`。Python 入口使用 shared `0.1.0`，Codex 在 `alternate_entrypoints.python` 中单独声明；`clients/shared/manifest.json` 记录该运行库版本，它不能作为助手 Client 启动。Codex 的 Node Desktop MCP Client 使用 Node 22+ 标准库，无需 npm 或 Python shared；原有 Python queue/app-server/Desktop MCP 路径保留各自边界。

Client 具有独立进程和维护边界。Python 实现单独复制或发布时，必须一起提供其指定版本的 `clients/shared`，并保持同一父目录结构；Node Desktop MCP Client 无此 Python 依赖。建议固定仓库提交以得到匹配的源码与 manifest。版本约束是当前维护约定，不是自动下载或依赖解析功能。后续需要分别发版时，可以把 shared 打包成固定版本运行库，再拆分 Client 仓库；协议无需因此改变。

Go Hub 本身不需要 Python 或 Node.js。Python Client 需要 Python 3.11+，没有第三方 Python 包依赖；Codex Desktop MCP Client 使用 Node 22+，本机 macOS 验证还要求已核实的官方已签名 Node 与自然宿主启动链。当前没有自动安装运行时、后台服务托管或自动 OAuth 续期；这些是安装体验的后续工作，不改变 Hub 协议。

## 接入范围

| Client | 原生入口 | 发送模式 | 本实现的实际限制 |
| --- | --- | --- | --- |
| Codex Desktop MCP | 官方 Desktop MCP 的 `send_message_to_thread` | `queue`，仅 `kind=result` | 2026-10-15 独立 Node 调用已实际引导到忙碌原聊天；完整 MQ 忙时返回已通过，空闲启动仍待验收 |
| Codex queue | `codex queue --thread ID --message TEXT` | `queue` | 只证明排队；忙时需要用户引导的队列不能记为自动引导 |
| Codex app-server | 官方 `app-server proxy` → `turn/steer` | 结果专用 | 仅 `kind=result`；默认不启用空闲启动；当前 stdio 宿主未开放独立 proxy |
| WorkBuddy CLI | 官方 CodeBuddy CLI `--resume SESSION_ID` | `queue` | 独立 CLI 的 MQ 执行、Report 和成果核验已通过；原 WorkBuddy 会话与配置的默认认证续聊也已通过，新企微搜索已实际执行，远端返回 `850003 / authorization expired`，未取得联系人数据 |
| WorkBuddy OpenAPI | 官方 `/openapi/v2/localassistant/message` | `queue` | 目标是官方持续本地助理；API 没有普通 Chat 的 `session_id` 定位参数。需要用户本人正式 OAuth 授权；尚未在本项目持有真实 token 完成接口验收 |
| Grok Routine | 用户提供的官方 Routine webhook | `queue` | URL、认证方式和请求结构来自用户的实际 Routine 配置；示例字段不是对官方请求格式的推测 |

独立 Client 尚无普通任务 `steer` 或 `interrupt` 控制模式的原生验收通过记录。现有 Client 的绑定只接受 `modes=["queue"]`；成果的原生引导与普通任务控制分开声明。不支持的模式在领取消息前拒绝，不会悄悄降级为队列发送。会话是否空闲、资源是否可用、是否允许唤醒，由 Hub 根据已核实的状态决定；Client 不自行伪造 `idle` 或 `busy`。

WorkBuddy Client 明确选择 `native.transport="cli"` 或 `"openapi"`，不自动降级。2026-10-14 独立正式登录 CLI 会话通过续聊和 MQ 任务，原桌面配置路径当时仍报需要登录；2026-10-15 使用原 WorkBuddy 会话 ID 与原配置，两次默认只读认证续聊成功，未传工具、权限或 MCP 覆盖参数。新联系人查询最初被本地非交互 Bash/Read 拒绝。随后精确规则修正 JSON/shell 外层引号归一化，用官方随包 wecom-cli `1.3.4-workbuddy-local.281` 的绝对路径在原会话实际执行；原始工具结果显示调用完成，但远端返回 `850003 / authorization expired`，未取得联系人数据。当前阻点已进入该机器人通讯录授权阶段，需创建者在企微工作台的智能机器人恢复授权；不能继续只写本地拒绝，也不能将其说成 CodeBuddy 登录失败或完整桌面工具继承已通过。[按日期记录](NATIVE-VALIDATION.md)

本轮 macOS arm64 的真实 MQ 验收已有原生执行与文件证据。首个任务在原生明确拒绝且确认未执行后，修正了技能读取、真实验收工作区读写与专属报告 helper 的窄范围配置，并按绝对工作目录恢复；原生会话主动发出 `accepted/running/succeeded`，文件 SHA-256 核对一致。第二个任务由 SSE 自动领取，默认 `resume` 同一已登录的独立 CLI 会话，不传工具或权限覆盖参数，首次执行即产生三种原生回执和新版本成果文件，SHA-256 同样一致。

两个任务使用不同的任务执行 ID；结果分别有新的 `message_id`、正确的 `reply_to` 和 `correlation_id`。重复提交同一 Submission 与已保存的原生 Report 均去重，没有新消息或重复执行，终态资源已释放。`delivered` 仍不等于业务成功；此次业务成功依据是原生回执和真实文件核验。

2026-10-14 的两个结果由运行中的 Codex 入口调用官方桌面工具，以 `send-now` 引导回原聊天。2026-10-15，独立 Node 客户端在自然宿主环境中通过官方 Desktop MCP 初始化并发现 57 个工具，真实调用 `send_message_to_thread`，得到 `isError=false` 和匹配 thread ID 的 ACK；消息也已作为 `codex_delegation` 立即进入忙碌原聊天，不需人工点击，未使用 native queue。

同机 Python 父进程链曾遇 macOS 签名校验失败，官方 Node 启动链则通过；该结果只限定这些实际启动链。App Server proxy 是另一条通道，其所需 daemon 控制 socket 未开放，不代表 Desktop MCP 不可用。随后常驻 Node MQ Client 0.2.0 在已订阅后完成新的真实任务链路：MQ 自动交接到固定独立 WorkBuddy CLI 会话，原生 accepted/running/succeeded 与成果文件核验后，新的关联 result 被 Node Client 自动领取并经官方 Desktop MCP 立即送入忙碌原聊天。没有当前入口或其他操作者手工转发、native queue 或人工点击，终态资源释放已核对。两轮真实 MQ 忙时返回均通过；第二轮已交付修正因果顺序的成果。两份当前文件与 Core SHA-256 匹配，使用不同 hubrun 执行标识，对应四条 task/result 消息均 delivered，资源已释放。空闲启动仍需单独验证。[分段验收证据](NATIVE-VALIDATION.md)

CLI 依据为 [官方无头模式](https://www.workbuddy.cn/docs/cli/headless)。OpenAPI 依据为 [官方 OpenAPI](https://open.workbuddy.cn/docs/openapi) 和 [本地助理功能说明](https://www.codebuddy.cn/docs/workbuddy/From-Beginner-to-Expert-Guide/Function-Description/Assistant)。两种传输都不读取桌面 Cookie、缓存 token 或 IPC ticket，也不把应用内部鉴权当作公开接口授权。

## 配置和启动

先让 Hub 注册该 endpoint，在对应 binding 中设置 `"driver": {"type": "external"}`，由独立 Client 消费。不要同时启动两个 Client 领取同一 endpoint。原生身份参数放在 Client 的私有配置，Hub 的管理密钥不应放进去。

每次连接先通过 `GET /v1/endpoint?endpoint=ID` 读取该 endpoint 的已注册配置，只能读取自己。Client 核对协议 `1`、endpoint ID、启用状态和原生目的地后，才能建立 SSE 和领取消息：

| Client | Hub 的 `endpoint.driver` | Hub 的 `endpoint.session_id` |
| --- | --- | --- |
| Codex | `codex` | 必须与私有配置 `native.thread_id` 完全一致 |
| WorkBuddy CLI | `workbuddy` | 必须与私有配置 `native.session_id` 完全一致 |
| WorkBuddy OpenAPI | `workbuddy` | 必须为 `localassistant`，表示持续专用本地助理 |
| Grok Routine | `grok-routine` | 必须与私有配置 `native.routine_id` 完全一致；这是用户为该 Routine 设置的标签 |

这里的 `endpoint.driver` 是助手类型，和 binding 的外部传输类型 `driver.type="external"` 不同。协议不兼容、禁用 endpoint、会话或类型不匹配时停止消费，消息正文不能指定或覆盖目的地。会话状态 `unknown` 仍保留为未知，不会被 Client 改写为 idle；是否允许投递由 Hub 决定。每次断线重连都会重新验证绑定。

`endpoint.session_id` 是长期复用的目的会话；`Report.run_id` 是本次任务的执行标识，两者不能混用。接收会话优先回报实际原生 run/turn ID；宿主没有公开时，可以使用通知提供的任务专属 `hubrun-...`，并明确它属于 Hub。同一任务始终保留该标识，不跨任务复用固定会话 ID。

默认 `result_queue_while_busy_verified=false`。只有实际核实某个原生入口能在忙时接收结果通知后，用户才能对该 endpoint 开启它；此时 Hub 可在 `busy` 或 `needs_user` 状态投递 `kind=result`，仍会等待未决的传输尝试。它不允许忙时启动普通 `kind=task`，不释放当前任务资源，也不证明已实现 `steer`、打断或自动批准原生权限。`unknown` 状态不会因此变成可投递。

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

`base_url` 必须使用数字形式的本机 loopback 地址，不接受局域网地址、DNS 域名、嵌入凭据或额外路径。`endpoint_token` 只访问该 endpoint 的收件箱、事件和回执，不能切换 leader 或创建管理任务。请求明确设置 Hub 要求的 Host、Origin 和 Authorization；不使用环境代理，也不跟随 HTTP 重定向。

在仓库根目录启动所需的 Python Client；Codex Node Desktop MCP 的启动方式见 [Codex Client](../clients/codex/README.md)，macOS 应使用该宿主已验证的官方签名 Node 绝对路径，不把 PATH 中任意 Node 当作相同启动链：

```sh
python3 clients/codex/main.py --config /path/to/private/codex/client.json
python3 clients/workbuddy/main.py --config /path/to/private/workbuddy/client.json
python3 clients/grok-routine/main.py --config /path/to/private/grok/client.json
```

Windows 可以使用 `py -3` 替换 `python3`，并提供该电脑上的绝对配置路径。Hub 的实际端口应与私有配置一致；示例端口不是自动发现结果。把个人配置文件限制为本机用户可读，避免提交真实 token、thread ID、聊天内容和成果文件。

### WorkBuddy CLI 配置

[WorkBuddy 示例](../clients/workbuddy/client.example.json) 使用 `native.transport="cli"`。先核实用户选择的 CLI 配置目录和既有会话是否可用；需要登录时走官方本人登录流程，再填写配置。Client 只恢复已有 ID，不自动创建会话。`command` 是一个绝对可执行文件路径，不是命令字符串或 argv 数组。`cwd`、`config_dir` 均须为本机已存在目录的绝对路径；`session_id` 与 Hub 绑定完全一致。`timeout_seconds` 默认 `120`，接受 `1` 至 `600` 秒。

Client 将完整 Envelope 与报告指引写入本机临时普通文件，作为 stdin 传给 CLI；stdout 也使用临时普通文件接收，完整解析后删除。不把任务正文放进命令行或 shell。调用参数固定为：

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

### WorkBuddy OpenAPI 配置

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

### Grok Routine 配置

Grok 的 `native.routine_id` 是用户为该官方 webhook 目标设置的稳定标签，必须与 Hub 的 `endpoint.session_id` 一致。`native.body` 必须包含完整消息。`"{{envelope}}"` 替换为 JSON 对象，`{{envelope_json}}` 替换为完整 JSON 字符串，`{{notification}}` 替换为带报告指引的完整通知。还支持 `{{content}}`、`{{task_id}}`、`{{message_id}}`，但它们不能代替完整消息。替换仅进行一轮，任务内容中的占位符不会被再次解释。对象占位符必须独占整个 JSON 字符串值。

### Codex 程序配置

Desktop MCP、queue 与 App Server proxy 是不同通道，配置与验收分别进行。2026-10-15 独立 Node 启动链已真实调用官方 Desktop MCP 并向忙碌原聊天即时引导消息；常驻 MQ Client 0.2.0 已提供，使用 Node 22+，启动说明见 Codex 目录，不以内部模型工具调用充当独立 Client 的成功证据。仅使用固定目标会话与允许的发送工具，发现全部工具不等于获得调用它们的授权。

Python 父进程链在本机 macOS 签名校验失败，官方 Node 链通过；不要因 App Server proxy 缺少 daemon socket 推断 Desktop MCP 也不可用。不新建另一套 daemon，不复制登录信息或访问未公开 IPC 来替代目标聊天。

以下为原有 queue/app-server Python 实现的程序配置边界：

Codex 的执行入口在启动 Client 时解析并保存为绝对路径，投递时不会重新从 PATH 查找。Windows 必须配置实际的 `codex.exe`；Client 明确拒绝 `.cmd` / `.bat` 包装器，以避免消息参数经过 shell 解释。Hub 的 `endpoint.session_id` 与 `native.thread_id` 必须指向同一个已核实原生 thread。

## 消费、回执和结果返回

1. Client 先核对 Hub 的已注册原生绑定，再建立 SSE 订阅，之后读取持久化 inbox，避免连接和领取之间遗漏新任务。
2. Hub 原子地把可领取消息从 `pending` 改为 `dispatching`。Client 只处理属于自己 endpoint 的领取结果。
3. 对原生入口尝试一次。执行前确定配置错误为 `failed`；CLI 已启动但失败/超时、网络尝试后没有确认、非 2xx 响应或重定向都为 `uncertain`。Codex CLI 成功、Grok webhook HTTP 2xx 记录传输已投递。WorkBuddy CLI 还核对完整 JSON 的末尾 `type=result`、匹配 `session_id` 和非错误结果；登录错误、残缺或不匹配输出保留 `uncertain`。WorkBuddy OpenAPI 则须核对 `code` 严格为整数 `0`、`data.message_id` 合法非空。任一传输的已投递均不证明业务完成，`UNKNOWN`、模型拒绝、退出码 `0` 或结果 `success` 不能代替原生 Report。
4. Client 发送 `/v1/delivery`。接手、执行和业务完成由原生助手主动调用 `/v1/report` 汇报；没有后台历史消息轮询。
5. 任务终态后，Hub 发出 `kind=result` 返回保存的原入口。结果使用独立 `message_id`，`reply_to` 指向原交接消息，`task_id` 与 `correlation_id` 保持关联；当前 V0 的 `correlation_id` 等于原任务 ID。接收方保留这些字段，只展示成果或失败原因，不能把结果再执行成新任务。助手不自行改收件人或建立下一轮任务。

SSE 保活是连接健康机制，不会触发业务查询。连接断开时按 1、2、4 秒逐渐退避，最多 30 秒，再订阅并扫描 inbox。只会取到新的 `pending`；`dispatching` 或 `uncertain` 不自动重新领取。Client 不会因网络重连重发原生消息。

如果原生操作完成后 delivery 回执没有确认，Client 停止并提示核对 Hub。保留已有任务，不认为操作没发生。重新启动 Client 也不会把已有 claim 当成待发送消息；按 Hub 的恢复流程核对真实原生记录和回执后处理。

可以在原生助手安装报告技能，或者使用配置好的 Hub CLI。技能只教原生助手读取完整交接、遵守授权和主动回报；它不代替后台 Client 监听消息，也不会赋予企业微信或其他应用的新权限。需要报告的助手必须能执行本机报告工具；无法执行时应报告接入未完成。

非交互 CLI 还须在宿主自身配置中核实读取该技能安装元数据、执行对应报告程序，以及读写本次任务文件的权限。安装技能不会自动批准这些操作。需要用户处理时，报告通道可用则回报 `needs_user`；连报告都被拦时保留原生提示与 Hub 已有投递记录供用户核对，不伪造接手、进度或完成回执。权限配置只覆盖用户选择的报告入口和工作区，不把交接通知当成更广授权。

除了 Go CLI，也有一个只发送报告的 Python 辅助入口：

```sh
python3 clients/shared/report.py --config /path/to/private/workbuddy/client.json --file /path/to/report.json
```

`--file -` 从 stdin 读取完整 Report JSON。该命令只读取 Client 的 endpoint 凭据调用本机 Hub，不触发 WorkBuddy 或其他助手。报告必须包含匹配的 `endpoint`；采用稳定 `event_id` 方便核对和幂等提交。

## 单独更新和拔插

当前实现支持独立进程的单独启动、停止和替换，Hub 协议不变；还没有一个替用户管理全部 Client 进程的 supervisor，也没有自动下载、验证或执行陌生插件。

安全更新时先暂停向该 endpoint 提交任务，让已领取消息拿到明确投递回执。任务已经交给原生助手后，停止 Client 不会停止原生助手本身；原生执行仍通过报告工具回传。停止旧 Client 后替换该助手目录，再启动新进程，其他 Client 和 Hub 可以继续运行。

如果更新时存在 `dispatching`、`uncertain` 或未保存回执，应先核对原生执行记录，再恢复消费。不要通过清空记录、改成 idle 或提交新 task ID 绕过不确定状态。中途强制停止 Client 只能进入保守恢复流程，不能被称为无损热更新。

新 Client 需要实现 `validate_binding(endpoint)` 和 `deliver(envelope) -> (outcome, detail)`，声明真实 `session_kinds` 和发送模式，再复用 shared runtime 或独立实现协议。绑定验证必须核对原生类型和会话目的地，不能让任务正文改写配置。Hub 和 Client 的协议版本是 `1`，不兼容升级明确拒绝。Python 文件替换不改变原生工具登录态；独立 CLI/SDK 会话也必须在 manifest 及支持说明中明确标注。

## 多平台与多电脑

Python Client 使用标准库 HTTP、子进程和文件 API；Codex Desktop MCP 的新路径使用 Node runtime。Go 核心可构建为原生程序，各 Client 的运行时由需要它们的用户选择安装。Windows、macOS、Linux 的原生 CLI、官方 MCP 启动链、签名与环境检查分别验收；某一语言或某一宿主启动成功不能直接推广到其他环境。

当前 Hub 接口仅在本机 loopback 使用。另一台电脑的助手需要在那台电脑上运行自己的 Client，并采用后续经过授权的跨设备传输；本机 SSE 不自动成为远程消息服务。文件引用带有 `device_id`、`workspace_id` 和工作区内的相对路径，不会自动复制文件，也不能把本机路径直接交给远程助手使用。

## 验证

```sh
python3 -m unittest discover -s clients/tests -v
```

测试使用本地假 HTTP 和模拟子进程，检查订阅/领取顺序、Host/Origin、拒绝重定向、传输不确定状态、一次原生尝试、失败回执后停止、结果通知语义、WorkBuddy CLI 绑定与输入输出、OAuth 配置边界及模板的一次替换。它不会发送真实助手消息、读取联系人或证明任何原生平台已接入。

真实验收仍需核实空闲唤醒、接手报告、执行报告、需要用户介入、成果核对、返回原入口及不确定投递的恢复。接入 WorkBuddy 时，可用已授权的只读联系人查询完成第一条闭环；发通知给联系人需要具体人类指令和原生应用要求的本人确认。
