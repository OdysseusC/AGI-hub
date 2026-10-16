# 适配器与支持范围

本页区分代码已实现的投递方式、官方候选能力，以及当前设备仍需完成的原生验证。用相同模型的 API 或另起 CLI/SDK 会话，不能算作接入用户正在使用的桌面助手。

## 当前独立 Client

| 目录 | 原生入口 | 支持模式 | 当前验证 |
| --- | --- | --- | --- |
| `clients/codex` | 官方 Desktop MCP / 已有线程 queue / app-server 结果引导 | `queue` / 结果专用 | 2026-10-15 独立 Node 经 Desktop MCP 即时引导已实测；常驻 Node Client 已通过完整 MQ 忙时返回；App Server proxy 缺少控制 socket，三条通道分别验收 |
| `clients/workbuddy` | 官方 CLI 会话恢复或本地助理 OpenAPI | `queue` | 独立 CLI 的 MQ 已通过；用户重新授权、入口核对并显式续接一次后，原会话的单联系人新查询、主动回报和自动忙时回传已通过；完整桌面工具继承及 OpenAPI 未验收 |
| `clients/grok-routine` | 用户配置的官方 Routine webhook | `queue` | 本地模拟 HTTP、模板、断线及重定向；实际 Routine 待配置验证 |
| `clients/claude-code` | Claude Code 会话内 mod 的 `prompt.submit` 与原生 report 工具 | `queue` | macOS Claude-3p 2.1.299 原 Code 会话已验证 N2；N3 经修复后一次回报续接完成并自动返回；忙时排队、其他平台待验收 |

Hub 没有内置助手驱动。现有 Python Client 使用 Python 3.11+ 标准库；Codex Desktop MCP 的独立 Node 发送已实测，常驻 MQ Client 已提供并通过本地模拟。原生 Adapter、manifest 和配置示例放在各自目录，公共部分只处理本机协议。Claude Code 使用宿主 mods API 运行会话内 Client；不同实现使用同一本机 REST 协议，唤醒可选 SSE 或事件长轮询。[独立 Client 的配置和启动](CLIENTS.md)

Client 不读取其他助手的登录凭据或历史。原生参数只从用户配置的私有文件获得；程序输出和 HTTP 响应正文不作为公开投递诊断展示。当前同仓库、每个助手一个目录，后续按维护与发布需要拆成独立仓库。

## Codex Desktop MCP

2026-10-15，独立 Node 客户端在自然宿主环境中，经官方 Node runtime 启动官方 Desktop MCP server，完成 initialize 和 tools/list，发现 57 个工具。真实调用 `send_message_to_thread` 后收到 `isError=false` 与匹配 thread ID 的 ACK，消息也已作为 `codex_delegation` 即时进入忙碌原聊天，无人工点“引导”、未使用 native queue。

同机 Python 父进程链因 macOS 签名校验失败；官方 Node 启动链成功。该限制与语言的一般能力不同，必须保留实际宿主环境和官方启动链的验证边界，不复制凭据或拼装未公开 IPC。发现全部工具也不代表 Hub 获准调用全部工具；目标 thread 和允许的发送工具由用户固定。

这条证据来自独立进程真实调用，不是让模型内部工具代发。随后常驻 Node MQ Client 0.2.0 在先订阅、后提交的新任务中完成 MQ → 固定独立 WorkBuddy CLI 会话 → 原生回报与文件核验 → 关联 result → 独立 Desktop MCP 的真实闭环。结果直接进入忙碌原聊天，没有手工转发、native queue 或人工点击，资源已释放。本机两轮 MQ 忙时返回已通过；随后同一官方 MCP 通道的一次独立诊断也启动了空闲原聊天，旧轮次 completed、新轮次不同且官方 read_thread 仍可用。空闲诊断没有新增 MQ 任务，idle 下完整 MQ 回路仍未验收。[本机记录](NATIVE-VALIDATION.md)

## Codex 队列

Client 使用独立 argv 调用，不经过 shell：

```text
codex queue --thread <native session ID> --message <complete notification>
```

配置示例只创建禁用绑定，实际 ID 与验证值由用户补齐：

```json
{
  "endpoint": {
    "id": "codex",
    "device_id": "this-device",
    "session_id": "",
    "enabled": false,
    "idle_wake_verified": false,
    "result_queue_while_busy_verified": false,
    "modes": ["queue"],
    "driver": "codex"
  },
  "driver": {
    "type": "external"
  }
}
```

`thread_id / executable / timeout_seconds` 配置在独立 Client 的私有 `native` 对象，Hub 只保留 endpoint 的绑定和能力声明。通知包含完整信封、已有授权边界和回报方法；结果通知只用于展示结果，不能对原任务重新启动工作或递归回报。

官方 `codex queue` 支持已有会话的 UUID 或精确名称；名称重名时应使用 UUID，不支持图片附件。当前 Client 只实现本机默认连接，不暴露 `--remote` 参数。[官方 queue 文档](https://learn.chatgpt.com/docs/cli/reference)

官方 App Server 有 `thread/resume`、`turn/start`、`turn/steer`、`turn/interrupt` 与执行事件，且 steer 需匹配活跃 turn。本项目已实现结果专用 App Server 分支并通过协议模拟；当前宿主未开放独立 proxy，普通任务控制和外部桌面引导未验收通过。独立启动 App Server 并不证明复用了桌面既有会话或其原生工具。[官方 App Server](https://learn.chatgpt.com/docs/app-server)

2026-10-14 的两个 WorkBuddy 结果经 Codex 宿主内官方工具 send-now 实际引导回原聊天。随后独立 Desktop MCP 已有上文的新证据；它与 App Server proxy 不是同一入口。proxy 所需 daemon 控制 socket 未开放，不代表 Desktop MCP 不可用；Desktop MCP 成功也不能反过来证明 proxy 已接通。常驻 Node Client 已通过完整 MQ 忙时返回，同一 Desktop MCP 的单次空闲启动诊断也已通过；这不验证 App Server proxy 的空闲 turn/start。普通任务控制和其他平台仍分别验收。

## Webhook / Grok Routine

Webhook URL 和请求体必须来自用户已核实的实际平台配置，Client 不会猜官方路由、payload 或鉴权。它们保存在该 Client 的私有 `native.url / native.headers / native.body` 中。云端 Routine 还需要一个真实、授权且能够回到本机的报告通道；请求成功不能证明闭环已经完成。当前没有跨设备 relay 或公网入口部署。

`body` 必须是有效 JSON，下面是模板替换规则，不能将语法演示误用成真实 Routine 请求体：

| JSON 字符串值中的占位符 | 替换结果 |
| --- | --- |
| `{{content}}` | Envelope 的 content，原样作为文本 |
| `{{envelope_json}}` | 完整 Envelope 的 JSON 文本，作为字符串转义 |
| `{{task_id}}` | 关联任务 ID |
| `{{message_id}}` | 消息 ID |
| `{{notification}}` | 完整 Envelope 与接手、报告指引组成的通知 |
| 整个字符串值恰为 `{{envelope}}` | 替换为完整 JSON 对象 |

替换在结构化 JSON 值上进行，最后序列化；不会直接拼接未经转义的内容，也不会再次解释任务文本中的占位符。`{{envelope}}` 不能嵌入带前后缀的字符串。请求模板必须传递完整信封或完整通知，单独的摘要不能代替关联 ID、授权与文件引用。

只允许 HTTPS，或指向 localhost/loopback 的 HTTP。不跟随重定向，防止把用户配置的 headers 带到其他地址。2xx 表示 `delivered`；请求发起后的非 2xx、断线或超时保守记为 `uncertain`。配置明显错误记为 `failed`。

用户此前在另一台 Windows 电脑验证过 Grok Routine 与 Codex 的事件交接。那是特定环境的经验；本项目不依赖其源工程，也不假设当前电脑存在相同配置。

## 扩展自己的 Client

增加一个助手目录，实现本机协议消费和对应的原生 Adapter；无需向 Go Hub 添加产品分支。当前 Python Client 用 `validate_binding(binding)` 核对真实目标，再由 `deliver(envelope) -> (outcome, detail)` 执行一次投递，可复用公共运行时。Hub 不执行来自配置的任意命令，也不自动加载陌生模块。

要声明 `steer` 或 `interrupt`，必须先确认原生入口定位的是绑定会话和目标运行，支持该语义，并能回传对应 `run_id` 与实际执行回执。内核要求 `target_run_id` 匹配活跃的边车管理运行。把控制文本当普通 queue 消息发送，不构成这些能力。已提供的 Client 尚无这些模式的原生验收通过记录，候选代码或本地模拟测试不代替实际验证。

`result_queue_while_busy_verified` 另行声明忙时接收 `kind=result` 的投递能力，默认关闭；它不是 `steer` 或 `interrupt`。普通任务、未知状态、未决投递和资源协调仍遵守 [消息协议](PROTOCOL.md) 的约束。

进程未成功启动为 `failed`；成功启动后非零退出、超时或取消为 `uncertain`；退出成功仅表示传输完成。原生任务完成仍要 Report 与成果验证。

## Claude Code 会话内插件

`clients/claude-code` 提供 `agi-hub-endpoint` mod，直接运行在用户绑定的 Claude Code 原会话内。它使用官方 `$.session.id()` 核对目的会话，以 `$.prompt.submit({text})` 提交插件来源的完整交接，并注册 `mcp__agi-hub-endpoint__report` 回报工具；不另起 `--resume` 进程，不伪装用户消息，也不通过 Bash 回报。[配置与安装](../clients/claude-code/README.md)

插件在所有加载它的 Code 会话中注册诊断命令，只有与私有配置及 Hub 绑定一致的会话才消费消息。Hub 的原子客户端租约代替非原子锁文件，同一 endpoint 的另一个进程不能同时领取或回报；会话切换时停止旧监听，重新核对新会话。持久化记录按 Hub 地址、endpoint、会话隔离；重启只对账保存的投递回执，不重新提交已领取的 prompt。

mods 的 HTTP API 不能读取流式 SSE，因此使用最长 25 秒的 inbox 长轮询，消息提交事件会立即唤醒等待请求。无消息时返回 `null` 后重建等待，不查询业务历史。原生投递确认、执行回报、成果核验仍分别记录；HTTP 或 `prompt.submit` 的不确定结果不重发。

当前以 Claude Code **2.1.299** 开发与校验。2026-10-16，macOS Claude-3p 的原 Code 会话已通过 N2 单次空闲启动；N3 自动接手并生成成果，修复回报工具输出格式后显式续接一次回报，Core 核验两份成果并自动返回忙碌的 Codex 原聊天。没有重派 MQ 任务，也没有在最终版本重复一条全新无人值守任务；忙时排队仍未实测。[实际过程与边界](NATIVE-VALIDATION.md#2026-10-16claude-3p-原-code-会话)

macOS、Windows、Linux 共享无第三方依赖的插件源码，但宿主版本及各平台原生能力分别验证。目标 Code 会话必须保持打开并加载插件；插件不负责启动已退出的桌面应用。

普通 Claude Chat、Cowork、WSL Code 会话不在支持范围。Desktop Code 与终端交互 Code 分开记录验证结果。跨会话 socket、Channels 和独立 CLI 恢复不作为本 Client 的备用投递，避免静默更换执行环境。[官方 mods API](https://code.claude.com/docs/en/plugins/mods/api)、[官方插件安装](https://code.claude.com/docs/en/discover-plugins)

## WorkBuddy：CLI 与官方本地助理

Client `0.2.1` 显式选择 `native.transport="cli"` 或 `"openapi"`，两种入口不自动切换。旧配置省略 `transport` 时保持 OpenAPI 行为。CLI 为已有普通会话的恢复候选；其工作环境、桌面身份和工具保留须单独验收，不能将独立 CLI 执行直接称为接通 WorkBuddy 桌面。[配置与运行](../clients/workbuddy/README.md)

### CLI 会话恢复候选

CLI 使用官方本人登录与用户配置的绝对程序路径、工作目录、配置目录和会话 ID，传 `--resume ID --print --output-format json --input-format text`，并附 Client 固定的简短当前交接 positional 指令。完整通知经 stdin 普通文件传入，任务正文不进入命令行，完整 stdout 经普通文件解析；可用实际 `node.exe` 与官方 JavaScript 入口替代 Windows `.cmd` 包装器，不经 shell。不传工具、权限、MCP、模型或 profile 覆盖参数，续聊默认持久化。固定指令不扩大用户授权。官方模式见 [无头执行](https://www.workbuddy.cn/docs/cli/headless)。

本机正式登录的私有 CLI 配置目录中，首个 MQ 任务确认原生明确拒绝且未执行后，修正了技能读取、真实工作区文件访问和专属报告 helper 的窄范围配置及绝对工作目录，再恢复原任务。第二个任务由 SSE 自动领取，默认 `resume` 同一既有独立 CLI，在未传工具或权限覆盖参数的情况下首次执行即完成。两次均有原生 `accepted/running/succeeded` 和 SHA-256 一致的成果文件；任务执行 ID 不同、结果关联正确，重复提交与回执去重，终态资源释放已核对。

2026-10-14 原 WorkBuddy 配置根与原会话的默认恢复曾返回 `Authentication required`。10 月 15 日原配置与原会话的两次默认只读认证续聊成功；新联系人查询先受本地 Bash/Read 限制，修正精确规则后，官方随包 wecom-cli `1.3.4-workbuddy-local.281` 实际执行并返回远端 `850003 / authorization expired`。这些是历史阶段。用户随后重新授权，本次原 endpoint、会话、工作目录和 `.workbuddy` 的新联系人查询已成功返回候选，字段与成果一致，并完成原生 `accepted/running/succeeded`、Core 文件核验及独立 Node Desktop MCP 自动忙时回传。首轮已投递但未接手，核对零工具、文件和 Report 后，仅由入口核对并显式续接一次，保留同任务与资源锁，未 requeue 或伪造回执；原历史前缀完整。不能将它写成首轮即自动执行成功。

默认持久化的失败尝试可能追加会话历史；10 月 14 日已核对原历史前缀完整保留，不能将失败解释为没有任何原会话变化。因此不盲目重发，保留实际结果核对。本次窄范围技能、报告和文件配置已通过独立 CLI 执行验收，不代表其他目录或命令已获授权。本人登录可能使用产品共享认证存储，配置目录隔离不构成凭据隔离；不读取或复制受保护 token。[本机验证记录](NATIVE-VALIDATION.md)

### 官方持续本地助理 OpenAPI

官方 Open API 提供本地助理状态、消息投递与历史查询：

| 能力 | 官方接口 | Scope |
| --- | --- | --- |
| 只读在线状态 | `GET /openapi/v2/localassistant` | `user.localassistant.readable` |
| 触发本地助理 | `POST /openapi/v2/localassistant/message` | `user.localassistant.invokable` |
| 读取回复 | `GET /openapi/v2/localassistant/message` | `user.localassistant.readable` |

域名为 `https://www.workbuddy.cn`。投递返回 `message_id`，文档未提供普通桌面任务的 `session_id`、设备选择参数或本地助理完成事件；历史增量接口用于轮询回复。云端 `/tasks` 与 ACP 的 SSE 属于云端会话，不能当作本地助理能力。当前 Client 已实现官方消息投递，未持有真实授权完成本机接口验收；不轮询历史替代接收端主动 Report。[官方 Open API](https://open.workbuddy.cn/docs/openapi)

Client 使用 `Authorization: Bearer access_token`，请求体为 `{"content":"完整通知","msg_type":"text"}`，要求正式 OAuth 的 `user.localassistant.invokable` 范围。缺少配置时不领取任务；不读取桌面 Cookie、缓存 token 或内部 IPC ticket。开发配置只接受用户明确授予的短期 access token，不包含应用密钥、refresh token 或自动续期。

HTTP 成功后还要核对官方 JSON 中严格整数 `code=0` 和合法非空 `data.message_id`；否则保留为投递不确定，不自动重发。合法投递回执也不证明业务执行或完成。

官方助理使用固定专属目录、一个持续会话，区别于普通任务的多会话与可选工作目录。第三方 API 需要应用审核和用户 OAuth 授权；桌面已经登录企业微信不会自动授予该第三方应用。纯本地开源安装的长期授权与续期方式仍需确认。[助理指南](https://www.workbuddy.cn/docs/workbuddy/From-Beginner-to-Expert-Guide/Function-Description/Assistant)、[第三方接入](https://open.workbuddy.cn/docs/third-party-app)

原生助手还需安装 [报告技能](SKILLS.md) 并验证实际接手和回报。技能解决消息解释和报告，Client 解决外部消费和唤醒，两者不能相互替代。Computer Use 可以做人工监督的原生入口验证，不能因此宣称官方 API 已授权或普通桌面任务具备后台唤醒能力。

本机 WorkBuddy 独立 CLI 的 MQ 执行与成果校验、原会话的认证续聊，以及重新授权后原会话的单联系人业务与自动忙时回传，分别有实际证据。最后一项需要由入口核对后显式续接一次，只验证返回候选匹配，不代表全通讯录唯一、全部企微业务或完整桌面工具注入；临时四条查询许可已移除，四条窄范围报告安装许可保留。Codex 的常驻 MQ 忙时返回及相同 Desktop MCP 的单次空闲原聊天启动均已实测；空闲诊断本身不新增业务任务，idle 下完整 MQ 回路与 App Server proxy 仍未验收。CLI 与 OpenAPI 保持显式选择，CLI 登录不授予第三方 OpenAPI 权限。[本机验证记录与 CLI 依据](NATIVE-VALIDATION.md)

## 原生接入验收

每个适配器分别验证：绑定的真实会话、空闲接手、忙时行为、权限提示、关联回执、成果文件、返回原入口以及失败核对。需明确列出验证的 OS、宿主版本和会话类型。遇到只能新起独立 CLI/SDK 会话的入口，要标明该差别。

返回 [README](../README.md) · 查看 [架构](ARCHITECTURE.md) · [安装指南](INSTALL-FOR-ASSISTANT.md)
