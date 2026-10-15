# 本机原生接入记录

记录日期：2026-10-14 至 2026-10-15，环境为 macOS arm64。10 月 14 日记录的版本为 WorkBuddy 5.8.2、官方 CodeBuddy CLI 2.161.0。这里只保留方法、结果与支持范围，不保存凭据、个人联系人、真实会话 ID 或私有绝对路径。Windows 与 Linux 的本仓库原生接入另行验收，构建通过不能替代实测。

## 2026-10-14：WorkBuddy 默认恢复与历史

Client 用配置指定的既有会话 ID、工作目录、配置目录执行 `--resume`；完整 Envelope 经 stdin 普通文件输入，stdout 使用私有普通临时文件接收。只增加非交互输出格式，不传 `--tools`、MCP、模型、权限覆盖或禁止持久化参数。

正式 CLI 登录前，完整官方 CLI 与桌面随包 CLI 恢复原桌面会话均返回 `Authentication required`。用户随后在仓库外的正式 CLI 配置目录完成本人登录。该目录下的独立既有会话默认恢复成功，正确记住前轮讨论，证明这条独立 CLI 路径的续聊能力。

当天原 WorkBuddy 配置目录的默认恢复仍返回登录错误，包括登录站点环境对照。一次默认持久化、使用最终 argv 和 stdin 的失败执行追加了原历史；核对确认旧内容前缀完整保留。失败、空 stdout 或退出码 0 都不能作为安全重发依据。这是 10 月 14 日的观察，不能覆盖下文 10 月 15 日原路径恢复成功的新证据。

早期关闭工具或设置 `Bash,Read` 的诊断改变了调用条件；历史复述不证明默认工具环境或企微接入。配置目录也不是账户隔离边界，官方实现可能使用共享认证存储。项目未提取桌面 token、IPC ticket 或强行改写登录态。[官方配置目录](https://www.codebuddy.cn/docs/cli/installation)、[官方无头模式](https://www.workbuddy.cn/docs/cli/headless)

## 2026-10-14：独立 CLI 的 MQ 执行与回报

真实 MQ 消息经 Python Client 送入正式登录的独立 CLI 既有会话，原上下文与已安装 `agi-hub-client` 技能可用。首次调用因非交互 Read/Write/Bash 权限被拒，没有回执和成果文件；Hub 保留任务未完成，没有把聊天说明或 CLI 成功状态当作业务完成。

在核对前次零文件写入、零回执后，窄范围配置仅覆盖技能读取、私有验收工作区读写和 endpoint 专属报告 helper，并明确工作区绝对目录，再恢复同一任务。未使用 `-y`、`bypassPermissions` 或整体放行 Bash。原生会话随后主动发送 accepted/running/succeeded，生成文件并通过 Hub SHA-256 核对。报告用原生文件工具保存 JSON，再直接调用 helper 的 `report --file`，不包进管道或另一解释器。[官方权限规则](https://www.workbuddy.cn/docs/cli/iam)

第二个任务由常驻 SSE Client 自动领取，默认恢复同一既有 CLI 会话，首次执行即完成三种原生回执和新的成果文件，无手工恢复。两个任务使用不同执行标识；结果各有独立消息 ID、正确的 `reply_to` 与 `correlation_id`。原样重投 Submission 和已保存成功回执后，任务数、结果消息数不增加，终态资源已释放。

报告 helper 只持有该 endpoint 的 scope token，只调用 `/v1/report`；不读 Hub 管理配置、不启动助手、不切换 endpoint。报告文件限定在声明工作区，拒绝越界、符号链接和其他 subcommand。该验收证明独立 CLI 的 MQ 执行、回报和文件核对，不证明原 WorkBuddy 桌面企微工具已执行。

## 2026-10-14：Codex 返回通道的分段结果

`codex queue` 曾成功保存诊断消息，但用户手动点击“引导”后才进入当前轮次，只证明排队。此后两个真实 MQ 成果由正在运行的 Codex 入口调用宿主内官方 `send_message_to_thread` 工具，以 send-now 实际引导回同一原聊天，无需人工点击，未用 queue 充当即时返回证据；各自的真实投递回执已保存。

独立 App Server Client 采用官方 `app-server proxy`，只读取线程元信息与轮次 ID/status，结果通过带 `expectedTurnId` 的 `turn/steer` 投递；默认不启用空闲启动，不自动恢复未加载线程，也不回退到 queue 或另起 exec/resume。当天实际 proxy 在连接控制 socket 时退出；当前宿主以 stdio 管理自己的 app-server，未开放该 daemon 控制 socket。[官方协议](https://learn.chatgpt.com/docs/app-server)

这项失败只描述 App Server proxy 通道，不能推出官方 Desktop MCP 不可用；Desktop MCP 的独立进程新证据见下文。

## 2026-10-15：原 WorkBuddy 会话与原配置恢复

使用原 WorkBuddy 会话 ID 与原 `.workbuddy` 配置，通过官方 `codebuddy` 默认 `--resume`，两次只读认证续聊成功。未传工具、权限或 MCP 覆盖参数。这项新证据确认原路径可以正常认证续聊，不能再笼统写成“原配置仍无法登录”。

新的联系人搜索最初在本地非交互 Bash/Read 权限阶段被拒，没有执行到远端。随后按公开命令解析器修正了精确 allow 规则的 JSON/shell 外层引号归一化；离线反例确认追加命令、改变关键词和重定向仍被拒绝，没有整体放行 Bash。

再次使用原会话默认 `codebuddy --resume`，已实际执行本轮新联系人搜索。入口为官方随包 wecom-cli `1.3.4-workbuddy-local.281` 的绝对路径，未采用默认 PATH 中另一个 `1.1.0` 版本。已核对原生工具结果：调用状态为 `completed`，原始远端输出明确包含 `errcode=850003`、`authorization expired`；这不是只根据助手最后一段解释作出的归因。未取得联系人数据。

当前阻点已从本地命令规则进入远端授权阶段：该 CLI 路径使用的机器人通讯录授权过期，需要该机器人创建者在企业微信工作台的智能机器人恢复通讯录授权。原生 CLI 登录和本轮新搜索实际执行已通过，联系人数据获取仍未通过；不能继续只写“本地权限拒绝”，也不能声称完整桌面工具注入继承已验证。

官方随包 1.3 不支持 `auth` 子命令，不把 1.1 的 `auth status` 当作这条路径的登录证据。本次未读取或复制桌面私有 env/auth/ticket，没有禁用 sandbox，没有盲重试远端搜索；临时五条精确 allow 已将 settings 恢复到原字节。未发送联系人消息。

## 2026-10-15：独立 Node 经官方 Desktop MCP 即时引导

独立 Node 客户端在自然宿主环境下，由官方 Node runtime 执行，再 spawn 官方 Node runtime 和 Desktop MCP server，完成 MCP initialize 与 tools/list，发现 57 个工具。随后真实调用官方 `send_message_to_thread` 发送到原聊天，得到 `isError=false` 与匹配 thread ID 的 ACK；消息也已作为 `codex_delegation` 立即进入忙碌原聊天，无人工点击“引导”，未使用 native queue。

这次来自独立进程的真实 MCP 调用，不是由当前模型内部工具代发充当外部 Client。发现 57 个工具不表示 Hub 获准调用所有工具；本条发送证据只对应指定原会话的发送工具。

同机 Python 父进程链曾因 macOS 签名校验失败；官方 Node 的自然宿主启动链通过。结论限定于实际测试的启动链，不能概括成 Python 不支持 MCP。App Server proxy 所需 daemon socket 未开放是另一问题，不是这次 Desktop MCP 的阻点。没有新建 daemon 或自行接入未公开应用 IPC 来冒充原聊天。

本节记录独立 Desktop MCP 工具层的发送与忙时引导证据。随后常驻 Node MQ Client 0.2.0 已完成下节的真实 MQ 闭环；空闲启动仍需独立测试，不能由忙时成功推导。

## 2026-10-15：完整 MQ 忙时返回闭环

本轮两次真实 MQ 忙时返回均通过。消费者已订阅后再提交新任务，每轮依次观测到：

1. Hub 持久化任务，经 SSE 通知由 WorkBuddy Client 自动领取。
2. Client 用默认 `--resume` 恢复固定的独立 CLI 协调会话，不覆盖工具、权限或 MCP。
3. 该原生会话主动提交 accepted/running/succeeded，生成实际成果文件。
4. Core 核对 device、workspace、相对路径与 SHA-256；终态后生成全新的 result 消息，`reply_to` 指向原交接，`task_id` 与 `correlation_id` 保持关联。
5. 独立 Node 消费者自动领取 result，通过官方 Desktop MCP 返回原 Codex 聊天；消息作为 `codex_delegation` 直接进入当时忙碌的原轮次。
6. 无当前入口或其他操作者手工转发，无 native queue，无人工点“引导”；文件校验和终态资源释放均已核对。

两轮使用不同的任务专属 hubrun 标识，对应四条 task/result 消息均为 delivered；两份成果的当前内容与 Core 记录的 SHA-256 均匹配，资源锁已清空。第二轮交付的成果修正了回传顺序：先由执行助手提交完成 Report，再由 Hub 核验并生成 result，随后由 Codex Client 消费并通过 Desktop MCP 返回原聊天；修订后的成果也已自动返回。

因此本机两轮完整 MQ 忙时返回已通过。这两条工作流的执行端是固定独立 CLI 协调会话；原 WorkBuddy 会话的认证与新联系人搜索是另行验证的路径，不能把验收报告成功交付写成联系人数据获取成功。Codex 空闲自动启动、完整桌面工具注入继承及其他平台仍按各自范围记录。


## 后续验收

- 单独验证 Codex 空闲自动启动，并继续核对断线、超时和投递不确定时不重发；本机 MQ 忙时返回已通过。
- 由该机器人创建者在企微工作台的智能机器人恢复通讯录授权后，再按用户授权验证联系人数据获取；保留本地拒绝与后续远端 850003 的先后记录，不盲目重试。
- 分别记录 Codex 空闲启动与忙时引导、权限提示、关联回执、成果校验和返回原入口的证据。
- 在 Windows、Linux 和其他宿主版本上单独验收官方启动链，不把本机成功推广为全平台支持。

WorkBuddy OpenAPI 的 `localassistant/message` 仍是独立候选，需第三方应用与本人 OAuth，当前未配置这条真实授权路径。CLI 正式登录不自动授予 OpenAPI 权限；它也不是普通桌面任意聊天的定位接口。[官方 OpenAPI](https://open.workbuddy.cn/docs/openapi)

返回 [Client 说明](CLIENTS.md) · [接入范围](ADAPTERS.md)
