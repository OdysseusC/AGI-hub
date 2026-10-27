[简体中文](NATIVE-VALIDATION.md) | [English](NATIVE-VALIDATION.en.md)

# 原生接入验收记录

记录日期：2026-10-14 至 2026-10-23。早期环境为 macOS arm64；10 月 23 日新增 Windows 11 与 macOS arm64 的 Codex 原生跨机邮箱闭环。10 月 14 日记录的版本为 WorkBuddy 5.8.2、官方 CodeBuddy CLI 2.161.0；10 月 16 日新增 Claude-3p Desktop 本地 Code 会话，内置 Claude Code 2.1.299。10 月 19 日 WorkBuddy 为 5.8.3，新增无模型技能维护核查。这里只保留方法、结果与支持范围，不保存凭据、个人联系人、真实会话或 Hub ID、IP、私有绝对路径。其他平台、宿主和助手组合须分别验收，构建通过不能替代实测。

## 2026-10-19：受管技能同步，无模型调用

本机 Codex、Claude Code、WorkBuddy 三处原有受管技能均通过原清单校验后显式更新和登记，使用同一私有 Hub；规则修订 `1`，正文 SHA256 为 `5cb0cf94da3b55863c0b219f924cde722c7d9ebace27c762ce49f90f4ce096f5`。更新前备份留在仓库外，未改变原生工具权限、插件代码或账号授权。

WorkBuddy 原 `binary_path` 继续指向专属报告 helper，仅新增可选 `hub_binary_path` 用于查询和维护，未用通用 CLI 替换报告入口。三处 `skills-status` 均为 `current`、`host_load_state=unknown`；再次同步三份文件修改时间完全不变。配置仅为三个已启用接入点设置互相可见目录，不启用其他接入点、不授予新增任务发送权。

实际 Hub 启动后，三个 endpoint 查询目录都只得到另外两个成员，规则摘要一致；相同 `known_version` 的规则请求不返回正文。启动后的原生会话状态仍为 `unknown`，没有虚设 idle、提交业务任务或发送更新唤醒。全部维护操作的原生模型调用数为 **0**。磁盘和目录通过不代表三个旧会话的模型已读入新正文。

宿主核查：Codex `0.163.0-alpha.10.2` 的独立 app-server 实际 `skills/list(forceReload:true)` 发现该受管技能，未创建 thread/turn；Claude Code `2.1.299` 的本机程序包含 `/reload-skills`，官方说明支持文件监测，但旧正文仍保留在历史；WorkBuddy `5.8.3` 程序包含技能目录文件事件监听，CodeBuddy CLI `2.161.0` 新进程会扫描对应配置目录。未为了刷新执行 Claude 模型续接、`/reload-plugins --force` 或 WorkBuddy 扩展重载。Grok Bot 的云端技能维护未实测，不加入本地文件同步。[宿主机制与官方资料](SKILLS.md#各宿主如何生效)

软件验证：Go race 测试、vet，Windows/macOS/Linux 的 amd64/arm64 六目标构建通过。真实 Hub 的模拟协议 smoke 保持单次交接、文件校验和一次返回；技能 smoke 核对三处登记、幂等时间戳、修改冲突保留、卸载不重装、禁用后目录版本变化及零消息产生。降级、同修订不同正文、Hub 与 CLI 来源不一致由测试拒绝。技能前置信息和代码围栏用标准库检查；上游技能校验脚本因本机缺少 PyYAML 未运行成功，未为此添加依赖。

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

这一阶段的阻点从本地命令规则进入远端授权：该 CLI 路径使用的机器人通讯录授权过期，当时未取得联系人数据。原生 CLI 登录、搜索实际执行与数据获取分别记录。用户随后完成重新授权，原会话的新查询与 MQ 回传成功，见后文；`850003` 保留为历史错误，不再作为当前阻点，成功范围也不扩张为完整桌面工具注入。

官方随包 1.3 不支持 `auth` 子命令，不把 1.1 的 `auth status` 当作这条路径的登录证据。本次未读取或复制桌面私有 env/auth/ticket，没有禁用 sandbox，没有盲重试远端搜索；临时五条精确 allow 已将 settings 恢复到原字节。未发送联系人消息。

## 2026-10-15：独立 Node 经官方 Desktop MCP 即时引导

独立 Node 客户端在自然宿主环境下，由官方 Node runtime 执行，再 spawn 官方 Node runtime 和 Desktop MCP server，完成 MCP initialize 与 tools/list，发现 57 个工具。随后真实调用官方 `send_message_to_thread` 发送到原聊天，得到 `isError=false` 与匹配 thread ID 的 ACK；消息也已作为 `codex_delegation` 立即进入忙碌原聊天，无人工点击“引导”，未使用 native queue。

这次来自独立进程的真实 MCP 调用，不是由当前模型内部工具代发充当外部 Client。发现 57 个工具不表示 Hub 获准调用所有工具；本条发送证据只对应指定原会话的发送工具。

同机 Python 父进程链曾因 macOS 签名校验失败；官方 Node 的自然宿主启动链通过。结论限定于实际测试的启动链，不能概括成 Python 不支持 MCP。App Server proxy 所需 daemon socket 未开放是另一问题，不是这次 Desktop MCP 的阻点。没有新建 daemon 或自行接入未公开应用 IPC 来冒充原聊天。

本节记录独立 Desktop MCP 工具层的发送与忙时引导证据。随后常驻 Node MQ Client 0.2.0 完成两轮真实 MQ 忙时返回；同一传输的空闲原聊天启动也已单独实测，分别记录如下，不能将一种测试范围推导成另一种。

## 2026-10-15：完整 MQ 忙时返回闭环

本轮两次真实 MQ 忙时返回均通过。消费者已订阅后再提交新任务，每轮依次观测到：

1. Hub 持久化任务，经 SSE 通知由 WorkBuddy Client 自动领取。
2. Client 用默认 `--resume` 恢复固定的独立 CLI 协调会话，不覆盖工具、权限或 MCP。
3. 该原生会话主动提交 accepted/running/succeeded，生成实际成果文件。
4. Core 核对 device、workspace、相对路径与 SHA-256；终态后生成全新的 result 消息，`reply_to` 指向原交接，`task_id` 与 `correlation_id` 保持关联。
5. 独立 Node 消费者自动领取 result，通过官方 Desktop MCP 返回原 Codex 聊天；消息作为 `codex_delegation` 直接进入当时忙碌的原轮次。
6. 无当前入口或其他操作者手工转发，无 native queue，无人工点“引导”；文件校验和终态资源释放均已核对。

两轮使用不同的任务专属 hubrun 标识，对应四条 task/result 消息均为 delivered；两份成果的当前内容与 Core 记录的 SHA-256 均匹配，资源锁已清空。第二轮交付的成果修正了回传顺序：先由执行助手提交完成 Report，再由 Hub 核验并生成 result，随后由 Codex Client 消费并通过 Desktop MCP 返回原聊天；修订后的成果也已自动返回。

因此本机两轮完整 MQ 忙时返回已通过。这两条工作流的执行端是固定独立 CLI 协调会话，不能把这两次验收报告交付写成原 WorkBuddy 会话的联系人获取证据。原会话后续的真实联系人查询与入口核对后的一次显式续接另见下文。相同 Codex Desktop MCP 的单次空闲启动也单独记录；当时 idle 下完整 MQ 回路、WorkBuddy 完整桌面工具注入继承及其他平台尚未验收。


## 2026-10-15：同一 Desktop MCP 的单次空闲启动

上一轮结束后，独立官方 Node 程序仅延时一次。发送前仅调用一次 `read_thread`，实际确认原聊天的 `thread.status.type=idle`，前一个 turn 已为 `completed`；没有循环轮询状态。

随后仅调用一次官方 Desktop MCP `send_message_to_thread`，返回 `isError=false` 与匹配目标聊天的 ACK。原聊天已实际收到消息并开始新轮次，新 turn ID 与前一个 completed turn 不同；官方桌面 `read_thread` 仍可调用，原聊天没有换成独立 CLI/SDK 会话。

因此本机同一官方 MCP 传输的空闲原聊天启动已单独实测通过，证据包括发送前的真实空闲状态、单次发送、新轮次实际启动和已核实桌面工具仍可用，不只是一条工具 ACK。

这次没有重复唤醒、手工转发或新建任务。它是一次独立诊断，本身没有新增 MQ 业务任务，也不是 idle 下完整 MQ 回路。此前两轮独立 CLI 闭环验证的是 busy 返回；当时 App Server proxy 的空闲 turn/start 和其他平台尚未实测。

## 2026-10-15：原 WorkBuddy 会话的单联系人查询与 MQ 回传

用户完成远端重新授权后，本次业务绑定原 `workbuddy` endpoint、原会话、原工作目录和原 `.workbuddy` 配置，未使用此前的独立 CLI 测试会话。受管技能安装绑定固定 endpoint 的报告 helper，只能向 `/v1/report` 提交；成果使用独立工作区。四条报告安装许可限定技能读取、成果目录读写和指定 helper，不放行无关目录或命令。

首条真实 MQ 已投递，但模型受旧诊断限制未接手；已核对零工具调用、零成果文件、零 Report。该 stdin 本来就是新的 user message，不能把未接手归因为 CLI 重放历史。Client 随后增加固定的简短 positional 指令，明确处理当前交接；完整 Envelope 仍从 stdin 传入，没有工具、权限、MCP 或模型覆盖参数，指令也不能扩大用户授权。

这次需要由入口核对后显式续接一次。保留同一任务与资源锁，只在 Core 已有 `delivered` 记录的 detail 中登记一次 operator continuation；未 requeue、未新建任务或消息 ID、未伪造 Report。该续接由当前 Codex 入口在用户已有任务授权下执行，无需用户再次点击确认，也不是 Broker 自动重试；不能将它描述为首轮即自动执行成功。

随后依次核实：

1. 原会话真实提交 `accepted/running/succeeded`，三份 Report 的 `event_id` 已逐一与 Core 已保存事件核对，保持同一任务关联和本次执行标识。
2. 仅执行一次新版官方随包 wecom-cli 的联系人 search；原始 `function_call_result` 成功返回 `users`。候选的姓名、部门、职务与成果文件一致，表明返回候选匹配，不声称全通讯录唯一。
3. Core 核对成果的 device、workspace、相对文件路径与 SHA-256，生成新的 result 消息；`reply_to` 指向原交接，`task_id` 与 `correlation_id` 保持关联。
4. 独立 Node Client 经官方 Desktop MCP 自动把 result 引导回忙碌的原 Codex 聊天，无手工转发或人工点引导；资源锁已释放、消息均为 delivered，原 WorkBuddy 会话已回到 idle，原历史前缀完整。

本次未读取或复制桌面私有 env/auth/ticket。临时四条单查询许可已移除，只保留四条窄范围报告安装许可；报告 helper 使用仓库外独立 runtime，不依赖开发 checkout。此前本地拒绝、远端 `850003 / authorization expired` 与重新授权后的成功属于先后阶段；当前单联系人数据获取已通过，不再把旧授权错误列为当前阻点。这项证据覆盖原会话、指定查询、主动回报、成果核验和自动忙时返回，不代表全部企微业务、全部 GUI 工具注入或 WorkBuddy OpenAPI 已验收。

## 2026-10-16：Claude-3p 原 Code 会话

执行宿主为正在打开的 Claude-3p Desktop 本地 Code 页，原生入口标识为 `claude-desktop-3p`、Claude Code 版本为 `2.1.299`。Client 为会话内 `agi-hub-endpoint` mod，最终版本 `0.1.3`；没有另起 CLI/SDK 会话承担被测任务。绑定原会话 UUID，原历史字节前缀校验保持完整。下列时间均为北京时间（UTC+8）。

### N2：单次空闲唤醒

在原会话运行一次 `/agi-hub-diagnose` 后保持空闲。17:16:35，原 transcript 出现唯一一条插件来源、带随机 nonce 的消息；17:16:45，Claude 自动启动回合并只回复同一 nonce，没有工具调用或重复消息。实际空闲唤醒成立，不只是一条 `prompt.submit` ACK。

当时 Client 为 `0.1.0`，它未识别宿主自动附加的插件来源包装，本地诊断仍显示 `submitted_unconfirmed`。`0.1.1` 改为严格匹配已实测的完整包装与第一行 marker；自动化测试覆盖正反例，随后 N3 实际投递也确认了同一包装和真实主线程 turn ID。为节约原长会话的模型消耗，没有在最终版本再跑一次独立 N2。`idle_wake_verified` 只在实际唤醒后开启；Claude 忙时结果标记仍为 false。

### N3：完整回路经修复后续接完成

17:27:56 只提交一条 MQ 任务，由已监听的插件自动领取并送入同一个空闲 Code 会话。合成输入包含六条状态记录，要求用原生工具读取、独立判定、生成 JSON 与 Markdown 两份成果并回读，然后经原生 `report` 工具返回。没有 Bash 回报、模型 API 调用替代或外部 CLI 恢复。

首条 accepted 已被 Hub 保存，但工具处理器错误地将对象直接放进原生 `result` 字段，宿主拒绝该输出。模型重投同一 accepted 事件时 Hub 幂等处理，没有新增任务；随后仍读取输入、生成两份成果并实际回读，但没有完成 running/succeeded 回报。此时业务文件完成不等于 Hub 任务成功。

`0.1.3` 将成功、失败及未绑定分支统一返回 JSON 文本，并新增通过宿主真实 `$.tool.call` 的回归检查。升级保留插件私有投递和任务记录，热重载只等待旧租约自然过期，没有卸载清空记录或重新 submit prompt。入口独立核验六条判定和计数后，按用户明确授权只发送一次同任务的回报续接。该续接仅调用 running 和 succeeded 两个原生工具，不再读写文件、不创建新任务、不重派消息。

17:44:14 原生 running 回报成功，17:45:06 succeeded 回报成功；三条唯一 Report 的完整内容指纹均与 Core 保存事件匹配，始终使用首次真实主线程 turn ID 对应的 `claude-turn:*`。这不表示三条报告都发生在同一个物理回合。两份文件的当前 SHA-256 与 Core 核验值一致，续接前后字节未变；六种状态各一条，三条需要人工关注。

Core 随后生成唯一 `kind=result`，`reply_to` 指向原交接，`task_id` 与 `correlation_id` 正确继承。17:45:06 独立 Node MQ Client 经官方 Desktop MCP 以 send-now 自动返回忙碌的 Codex 原聊天，消息实际进入当前轮次，无入口手工代发、无 native queue 或人工点击引导。Envelope 的 `mode=queue` 是 Hub 业务模式，不代表这次原生返回使用了 Codex queue。任务和结果均为 delivered，Claude endpoint 回到 idle，资源锁清空。Core 接受执行报告后会将投递 detail 更新为执行回执确认文本，不能要求最终仍保留最初的传输 detail。

因此本次闭环已实际完成，N3 记录为“修复后一次回报续接完成”。首轮无人值守没有通过，也没有在最终版本重复一条全新任务来声明这一点；保留此限制以避免额外消耗长会话 token。

### 自动化与支持边界

Go 全仓 `go test -race ./...` 与 `go vet ./...` 通过。最终 Client 的 Node 状态机测试 30 项、宿主 `plugin test` 5 项及 `plugin validate` 通过；新增原生工具调用检查验证 JSON 文本 ABI，不仅检查工具注册。既有 Python Client 54 项与 Codex Node 9 项回归通过。这些测试不请求 Claude 模型。私有验收记录另保存原 UUID、执行时间、报告指纹、文件哈希、原历史前缀及测试输出；它们不进入开源仓库或便携包。

本期额外修正了文件锁写后回读的竞争问题，使用 Hub 原子租约；增加绑定变更隔离、迟到请求保护、热重载租约等待、token 指纹隔离、丢失领取响应后的未决对账，以及仅靠 `prompt.submit` resolve 不能确认投递的判断。上述恢复与安全分支有自动化覆盖；不把模拟测试写成 N1、N4 至 N9 的全部原生实测。未绑定 CLI 的零模型命令冒烟也不作为终端 CLI 完整接入证据。

Claude 忙时排队、真实权限提示 needs_user、退出桌面后的恢复、Claude 作入口的反向任务及 Windows/Linux 原生接入仍待单独验收。Claude 空闲执行与 Codex 忙时返回已在本节记录；Codex 自身空闲时的完整 MQ 业务回路仍未验收。没有计量服务商实际扣除积分，也不将用户截图的累计消耗归于本次测试。

## 2026-10-19：纯邮箱 Codex ↔ WorkBuddy 原会话

本轮保持 Hub 仅作消息管道。Codex 与 WorkBuddy 各自启用 mailbox 协议 1；沿用原 Codex Desktop 聊天和原 WorkBuddy / CodeBuddy CLI 会话，没有创建新的协调聊天。Claude 模型调用为零；未查询企微资料或发送外部消息。

16:38 开始的第一封 request 已由常驻 Client 恢复 WorkBuddy 原会话。模型沿用缓存的旧说明，写出的协助请求缺少必填稳定 `id`，本地收发程序拒绝，未进入 Hub。泛化报错导致模型多次修改错误字段，不能记为无人干预成功。随后修正程序的字段错误提示，并在原生通知中加入简短完整 JSON 示例；确认旧 CLI 已退出且无未决投递后更新客户端，由原入口在同一关联中显式发送一封格式说明，没有重新派发原消息。

修正后，WorkBuddy 主动发 request 给 Codex；独立 Node Client 经官方 Desktop MCP 以 send-now 自动引导当前忙碌的原聊天，无需点击 queue 或手工转发。Codex 实际读取合成输入，计算 `[3,5,8]` 总和为 16，连同 nonce 显式回信。WorkBuddy Client 等首轮 CLI 退出并保存真实投递回执后，才再次恢复同一会话接收 reply；WorkBuddy 独立重读、核对 nonce/总和，写出 result.json，并主动发送新的 result 给 Codex 原入口。结果真实进入当前聊天，未由 Hub 自动生成。

入口实际回读成果：nonce、total、checked_by 与 correlation_id 均一致，并计算文件 SHA-256。WorkBuddy 明确报告自身窄权限下无法执行 shasum，未冒称已核验哈希；文件哈希由 Codex 侧完成，不为验收扩大命令权限。本轮五封消息（含一次格式说明）共用同一关联与原入口上下文；邮箱不创建 Task/Report、不采用父子等待或强制返回链。真实 UUID、时间点和文件哈希保存在本机私有诊断中，不进入仓库。

结论：**经一次格式修正后的原生双向协作与成果回传已通过**。不宣称首轮零干预、三名真实助手并行、Claude 新邮箱接入或其他平台原生能力已验收。三方向自由路由、共享限额、重复投递、静默状态与不确定投递保留另由实际 Hub 加模拟原生适配器的零模型集成测试覆盖。

## 2026-10-20：纯邮箱 Claude 原 Code 会话与 Codex 回传

### 原会话与配置修复

实际宿主为 macOS arm64 上已打开的 Claude-3p Desktop 本地 Code 会话，内置 Claude Code **2.1.299**，会话内插件为 **0.2.2**。原会话 UUID 未变，执行后旧 transcript 字节前缀的哈希保持一致；没有用独立 CLI/SDK 会话替换。宿主原有 bypass 模式保持不变，本次未新增原生权限，也未复制认证数据。

Desktop 宿主已加载新版插件，但已保存插件设置时仍传入空的启动 `client_config`。修复后优先使用非空启动选项；否则通过 SDK 的 `$.plugin.name` 字符串属性与 `$.settings.read()`，只从合并设置中读取本插件的路径。未限定 marketplace 的清单名会同时检查裸名称及本插件的 marketplace 限定设置，冲突路径直接停止，不自行选择；不选取其他插件的配置或凭据。此修复先通过离线检查，再提交真实邮箱请求。

### 真实邮箱闭环

下列时间均为北京时间（UTC+8）。**11:48:16.902**，持有租约的 Client 通过 `$.prompt.submit` 自动将一封 MQ request 送入空闲的原 Code 会话；真实主会话 `turn.start` 观察到精确 marker，从而确认输入 delivered，不只依据 submit promise。

原生工具调用恰好为一次 **`Read`**（读取 **54 字节合成输入文件**）和一次 **`mcp__agi-hub-endpoint__mail_send`**（显式发 result 给最初 Codex 入口）。文件中的 nonce 与总和 **51** 均和结果一致。Claude 未使用 Bash、旧 report 或继续委托其他助手，最终回复“已回传”，并于 **11:48:40.870** 回到 idle。结果来自助手明确发送，不是 Hub 自动生成的业务回复。

独立 Codex Node Client 随后经官方 Desktop MCP **send-now** 自动将结果送入忙碌的 Codex 原聊天，并核实该聊天实际收到。无需人工转发、选择 native queue 或点击引导。此关联在 **四条消息上限内使用两封 delivered 邮件**：一封 request、一封 result；既有 Task 数保持 **7 → 7**，消息数为 **18 → 20**。没有新增 Task/Report 流程或强制回传树。

结论：**Claude 原会话空闲邮箱执行与显式回传忙碌 Codex 原聊天已通过**。这是配置修复后的一个原生业务回合，不表示安装和预检全过程无需介入。

### 模型用量与预检事件

这个原生业务回合产生了 **三次真实模型 API 响应，各有独立 usage ID**。首次写入 **272,896 个输入 token** 的缓存，后两次分别记录 **272,896** 和 **273,092** 个 token 缓存命中；三次响应的**输出合计 363 token**。一个原生回合不等于一次模型请求，本次也不是零消耗。输入文件很短，没有消除原会话已有的长上下文；邮箱消息限额约束消息数，不约束宿主模型请求或服务商积分，本节未计量实际扣除积分。

真实业务验收之前，另一个原本只执行斜杠命令的 CLI 配置探测意外调用了模型。初始私有审计中**至少两条 usage 记录**可见 **63,840 个缓存创建输入 token**及 **2,218 个输出 token**。因存在嵌套尝试，总请求数及总消耗仍未知，不能把这些数字当作完整账单。探测已停止，私有审计证据保留，之后未再使用 CLI `-p` 探测。**向 `claude -p` 传入斜杠命令不保证跳过模型。** 离线检查使用 Node 测试及官方 `plugin validate` / `plugin test`，真实模型验收单独明确执行和计数。

Desktop 界面还出现过服务商健康检查的 `ERR_CONNECTION_CLOSED` 横幅；实际 Code 业务回合、原生工具和回传仍成功，因此不将该横幅记录为原生接入被阻断。

### 已验证范围

本次仅覆盖该 macOS arm64 Claude-3p Code 版本、既有 Claude 空闲会话，以及返回忙碌 Codex 原聊天。Claude 忙时排队、steer、由人在 Claude 发起的反向业务、Windows 原生接入及其他宿主仍未验收。已有真实空闲唤醒证据，无需额外付费跑一次 N2 或重复业务任务。上文 10 月 16 日旧版 N2/N3 与 10 月 19 日 WorkBuddy 记录保留当时结论，不改写为本次新邮箱通过的证据。

真实会话 ID、路径、nonce、usage ID 和 transcript 哈希留在私有记录中，不进入仓库或安装包。

## 2026-10-23：Windows Codex 空闲接手与 macOS 原入口回传

运行代码为 `1387677`，Hub 为 **0.4.0**。macOS arm64 与 Windows 11 使用互相配对的隔离 lab Hub。用户在 Windows 既有 Codex 原聊天中启动独立 Node Client，经官方 Desktop MCP 预检并订阅 SSE。启动配置误带旧定制字段 `native.receive_tasks`；Windows 助手备份后仅删除该键，严格未知字段校验未放宽，离线回归同时覆盖拒绝旧字段与单键迁移成功。

确认 Windows bootstrap 回合已完成、原聊天没有 active turn 后，发送唯一一封包含 nonce 和 `17+29+43` 的 MQ request。独立 Client 经官方 **send-now** 自动启动原聊天的新原生回合：UTC **01:54:46.300** 开始，**02:02:23.740** 完成。Windows 模型使用自身原生 exec 工具保存完整结果 JSON，并调用 lab 固定身份 helper 的 `mail-send`，明确发送一封 result，退出码为 0；磁盘 JSON 的 nonce、结果 **89** 与 canonical 关联均与 Hub 记录一致。

macOS 独立 Client 随后自动将该 `kind=result` 引导进入用户最初输入的、当时忙碌的原 Codex 聊天，入口已实际收到原生输入，官方 `read_thread` 确认仍为同一活跃轮次。`correlation_id`、`reply_to` 和 `root_origin` 保持关联，**两封消息预算使用 2/2**，没有手工转发或点击 native 队列。Grok 仅协助取证和原生观察后的行政状态登记，未代发 request/result；确认 Windows 没有新 active turn 后恢复 lab 的 idle 状态。本次 Claude 调用数为 **0**，Windows 原生产 Hub、Client 和旧队列未修改。

**已通过的范围是这一小型合成任务的跨机邮箱链路：Windows 原聊天空闲接手、原生工具执行、显式发结果，以及自动回到忙碌的 macOS 原入口。** Windows 忙时收件没有验收，其 busy 验证标记仍为 false；状态来自实际观察后的登记，长期自动状态跟踪及全生命周期无人值守恢复未验收。其他助手的跨机业务不随此次通过。源码允许 caller 与 target 不同，但本轮从目标原聊天启动，不能作为替代启动路径的实测证据。

真实会话与 Hub ID、IP、nonce、私有路径及原始投递和回合证据保存在仓库外，不进入公开文档或安装包。

## 2026-10-27：Hub 重启后的真实状态恢复

UTC 07:12，在本机原 Codex 桌面聊天仍运行时，用独立 loopback Hub 和空邮箱检验 Node Client `0.3.1`。初次连接从 `unknown` 恢复为原生 `read_thread` 确认的 `busy` 与真实执行中 turn ID；只重启该测试 Hub 后，状态再次重置为 `unknown`，Client 自动重连并恢复同一真实执行状态。每次连接的初始领取、SSE 初始事件和首次状态广播合计 3 次元数据读取，之后稳定；两次共 6 次，未形成自激循环。

测试未尝试原生发送，未提交业务消息，也未改变唤醒验证标记；额外模型调用和 Claude 调用均为 0。既有主 Hub、业务 Client 与 C 的原配置保持不变，测试进程已清理。证据仅证明**原生 Client 已运行时，Hub 重启后的状态重建**，不证明系统重启、宿主自动启动、连续忙转闲通知或 C 的 Windows 界面验收通过。原生轮次、私有配置和原始记录保存在仓库外。

## 后续验收

- 保留此前两轮独立 CLI 的 MQ 忙时返回、原 WorkBuddy 会话由入口核对并显式续接一次后的单联系人业务、单次空闲启动诊断，以及 10 月 23 日跨机邮箱闭环的分别记录。macOS 原入口空闲时接收完整业务结果仍须另行验收；继续核对断线、超时和投递不确定时不重发。
- 其他企微业务与桌面工具分别按用户授权验收，不从单联系人候选匹配推导全部能力或全通讯录唯一。保留本地拒绝、远端 850003 和重新授权后成功的历史，不盲目重试。
- App Server proxy 的连接与空闲 turn/start 单独验收，不能用 Desktop MCP 已通过的空闲启动替代；权限提示、关联回执和成果核对仍遵循各自通道的记录。
- 在本次已验证范围以外的 Windows/macOS 宿主及 Linux 上单独验收官方启动链，不把指定两台电脑的成功推广为全平台支持。

WorkBuddy OpenAPI 的 `localassistant/message` 仍是独立候选，需第三方应用与本人 OAuth，当前未配置这条真实授权路径。CLI 正式登录不自动授予 OpenAPI 权限；它也不是普通桌面任意聊天的定位接口。[官方 OpenAPI](https://open.workbuddy.cn/docs/openapi)

返回 [Client 说明](CLIENTS.md) · [接入范围](ADAPTERS.md)
