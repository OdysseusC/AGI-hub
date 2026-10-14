# 本机原生接入记录

验证日期：2026-10-14。实际环境：macOS arm64、WorkBuddy 5.8.2、官方 CodeBuddy CLI 2.161.0。这里只记录可复现方法与支持范围，不保存凭据、个人联系人或真实会话 ID。Windows 与 Linux 的原生接入另行验收，构建通过不能替代实测。

## WorkBuddy 默认恢复与历史

当前客户端按完整 Envelope 消费消息，用配置指定的既有会话 ID、工作目录、配置目录执行 `--resume`；完整内容通过 stdin 输入，stdout 使用私有普通临时文件接收。命令只增加非交互输出格式，不传 `--tools`、MCP、模型、权限覆盖或禁止持久化参数。

正式 CLI 登录前，完整官方 CLI 与桌面随包 CLI 恢复原桌面会话都返回 `Authentication required`。用户随后在仓库外的正式 CLI 配置目录完成本人登录。该配置目录下的独立既有会话默认恢复成功，正确记住上一轮“Go 编写核心、Python 编写客户端”的讨论；这证明该独立 CLI 的续聊能力。

原 WorkBuddy 配置目录的默认恢复仍返回同样登录错误，包括按正式登录站点选择环境后的对照。一次使用客户端最终 argv、默认持久化与 stdin 的失败执行追加了原历史；已核对旧内容前缀完整保留。认证错误、空 stdout 与退出码 0 都不能作为安全重发依据。

早期诊断显式关闭工具或设置 `Bash,Read`，只能说明那些人为改变的调用路径，不能据此否定默认 `--resume` 的设计，也不能证明桌面工具继承。关闭工具时两种 CLI 都曾复述原桌面历史，但测试未持久化，不等于桌面原生运行与企业微信工具接入成功。

官方配置目录并不等同账户凭据隔离，公开实现还有共享认证存储。本项目未提取桌面 token、IPC ticket、改写受保护登录态或在原桌面目录强行重新登录。[官方配置目录](https://www.codebuddy.cn/docs/cli/installation)、[官方无头模式](https://www.workbuddy.cn/docs/cli/headless)

## MQ 与回报技能

真实 MQ 消息已由 Python Client 领取并送入正式登录的独立 CLI 既有会话，原上下文与已安装 `agi-hub-client` 技能可用。首次原生调用拒绝非交互 Read/Write/Bash，返回聊天说明，但没有回执或成果文件；Hub 正确保留任务未完成，没有把 CLI 成功结果当作业务成功。

安装仅限定原生 profile 的读取技能目录、读写私有验收工作区和直接调用 endpoint 专属报告命令。没有传 `-y` 或 `bypassPermissions`，没有整体放行 Bash，也没有通过消息替用户批准。在核对前次零文件写入、零回执发送后，明确告知工作区绝对目录并恢复同一任务。原会话随后发送真实 accepted/running/succeeded，生成文件并通过 Hub SHA-256 核对。避免用 workspace ID 猜路径；CLI Client 的通知也携带本机执行目录。回报应使用原生文件工具保存 JSON，再直接调用报告 helper 的 `report --file`，不包进管道或另一个解释器。[官方权限规则](https://www.workbuddy.cn/docs/cli/iam)

报告 helper 只持有此 endpoint 的 scope token，通过 `/v1/report` 回传；不读 Hub 管理配置、不启动原生助手、不支持任意消息或切换 endpoint。只接受声明工作区内的普通 JSON 文件，拒绝越界、符号链接和其他 subcommand。Hub 为终态生成独立结果消息，核对 `reply_to / correlation_id` 与成果设备、工作区、相对路径及 SHA-256。

第二次任务由已启动的 SSE Client 自动领取，默认恢复同一既有 CLI 会话，一次完成三个原生回执与新的成果文件，没有手工恢复。两次任务的执行标识不同，终态结果各有新的消息 ID、正确的原消息指向和关联 ID。原样重投第二次 Submission 与已保存的原生成功回执后，任务数、结果消息数不增加，资源已经释放。

当前这条验收是独立官方 CLI，不能标为 WorkBuddy 桌面企微接入。普通 WorkBuddy 界面此前对用户指定单个联系人的只读查询成功，仍只是原生基线；本轮没有重新搜索通讯录或发送联系人消息。

## Codex 原入口返回

此前 `codex queue` 成功保存诊断消息，但用户手动点击“引导”后才进入当前轮次。这只证明排队，不能记为忙时自动引导或空闲自动处理。

新增可选 app-server Client：通过官方 `codex app-server proxy` 连接运行中的服务；`thread/read` 不加载正文，`thread/turns/list` 只取轮次 ID 与状态，忙时使用带 `expectedTurnId` 的 `turn/steer`。结果投递失败不会退回 queue，也不另起 `exec/resume` 会话。空闲 `turn/start` 需单独明确验证，未加载线程不会自动恢复。[官方协议](https://learn.chatgpt.com/docs/app-server)

本机默认 proxy 在连接控制 socket 时退出，尚未向当前轮次发送诊断。公开宿主实现与 socket 元数据确认，当前宿主采用 stdio 而未开放该控制 socket；未另起 daemon 冒充原聊天，也未将应用 IPC socket 用作 proxy。

随后从 MQ 原子领取两次真实成果，通过当前 Codex 会话内的官方 `send_message_to_thread` 工具返回同一原聊天。本机公开工具实现走 send-now，活跃轮次走原生引导；两条结果均实际进入本轮，不需用户点击“引导”，未调用 `codex queue`，随后保存各自的真实投递回执。

这里验收的是宿主内工具返回；结果领取和返回由当前 Codex 入口执行，独立 Python 返回 Client 未接通，不能写成完全无人值守的边车闭环。空闲唤醒和独立 proxy 仍需单独验收。

## 官方本地助理与后续验收

WorkBuddy OpenAPI 的 `localassistant/message` 是独立候选 Client，需要第三方应用和本人 OAuth。本机没有创建该应用，因此未配置；CLI 正式登录不会自动授权 OpenAPI。接口没有普通桌面会话定位参数，不能把它当作原桌面任意聊天消息入口。[官方 OpenAPI](https://open.workbuddy.cn/docs/openapi)

后续验收分别检查：指定原生会话空闲启动、忙时实际引导或打断、原生权限处理、主动关联回执、真实成果校验、回到原入口以及失败不重复执行。每项都注明宿主、系统、版本和会话类型。

返回 [Client 说明](CLIENTS.md) · [接入范围](ADAPTERS.md)
