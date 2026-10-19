# 原生助手技能安装

每个接入会话需要知道怎样处理任务信封、按真实执行状态回报，以及怎样展示返回结果。AGI Hub 提供同一份 `agi-hub-client` 技能，由本机 CLI 安装到用户选定助手的技能目录。技能是协议说明；消费通知和唤醒现有会话仍由各宿主适配器负责。

## 安装

先按 [安装指南](INSTALL-FOR-ASSISTANT.md) 构建、初始化边车，并配置用户明确要接入的 endpoint。使用实际安装的程序和私有数据目录：

macOS / Linux：

```sh
/absolute/path/agi-hub install-skill --host codex --endpoint codex --data /absolute/path/private
/absolute/path/agi-hub install-skill --host workbuddy --endpoint workbuddy --data /absolute/path/private
/absolute/path/agi-hub install-skill --host claude --endpoint claude --data /absolute/path/private
```

Windows PowerShell：

```powershell
& 'C:\Users\you\bin\agi-hub.exe' install-skill --host codex --endpoint codex --data 'C:\Users\you\private\agi-hub'
& 'C:\Users\you\bin\agi-hub.exe' install-skill --host workbuddy --endpoint workbuddy --data 'C:\Users\you\private\agi-hub'
& 'C:\Users\you\bin\agi-hub.exe' install-skill --host claude --endpoint claude --data 'C:\Users\you\private\agi-hub'
```

以上是路径示例，须替换为本机真实绝对路径与已配置的 endpoint。安装流程适合交给当前 AI 助手直接执行，不需要下载脚本、额外包管理器或修改宿主权限设置。CLI 的 `--binary` 可明确记录接手助手以后使用的真实程序绝对路径。

安装器只写协议说明和安装元数据，不修改宿主工具权限。非交互 CLI 的实际回报还需要宿主允许读取该安装元数据、执行对应报告程序以及访问本次任务工作区；技能被发现不代表这些操作已经获准。用户选择官方 CLI 自身的窄范围配置，交接消息和技能不能代替用户批准，也不应授予无关目录或命令权限。

| 宿主 | 默认技能目录 | 支持边界 |
| --- | --- | --- |
| Codex | 当前用户 `~/.codex/skills` | 安装协议技能；既有会话是否加载需在实际会话中核对 |
| WorkBuddy | 当前用户 `~/.workbuddy/skills` | 根据当前 5.8.2 本机扫描实现选择；须通过技能界面和真实任务核对加载，不作为全版本保证 |
| Claude Code | 当前用户 `~/.claude/skills` | 为 Code 会话安装接手说明；唤醒由另外安装的 `agi-hub-endpoint` mod 完成，普通 Chat/Cowork/WSL 不支持 |
| 其他宿主 | 无默认值 | 使用官方说明或本机已核实的技能目录，显式指定 `--skills-dir`；安装相同说明不代表支持唤醒 |

自定义目录示例：

```text
<installed-binary> install-skill --host <host> --endpoint <configured-endpoint> --data <absolute-data-dir> --skills-dir <verified-absolute-skills-dir>
```

尖括号是待替换参数。只安装到用户选定宿主，不批量扫描应用配置、提取登录态、注册其他助手或切换 leader。不同宿主的技能格式和加载条件需要分别核实；如果需要 UI 导入，使用宿主原生技能界面完成并保留用户审批流程。

使用 WorkBuddy 的独立 CLI 配置目录时，通过 `--skills-dir` 选择该 CLI 环境实际扫描、已核实能加载的技能目录；桌面默认目录不会自动成为每个 CLI profile 的技能目录。先核对会话加载证据，再配置技能读取、指定报告 helper 与本次工作区文件权限。

Claude Code mod 提供 `mcp__agi-hub-endpoint__report` 时，技能优先调用该原生工具，endpoint 与真实任务 turn ID 由插件固定；不需要 Bash 报告命令。Go CLI `report --file` 是其他支持环境的后备方式，不能在 Claude 插件租约之外绕过其绑定、回报来源和幂等核对。插件、技能与权限分别安装：技能存在不代表已加载插件，也不能证明空闲唤醒。当前 Claude Code 以 2.1.299 校验，实际 N2/N3 结果见 [本机记录](NATIVE-VALIDATION.md)。

## 安装内容与更新

安装目录固定为技能根目录下的 `agi-hub-client/`：

```text
agi-hub-client/
  SKILL.md
  installation.json
  .agi-hub-manifest.json
```

`installation.json` 包含 `host`、`endpoint_id`、`binary_path`、`data_dir`，可选 `hub_binary_path`。`binary_path` 始终用于回报；若它是固定 endpoint 的专属报告程序，使用 `--hub-binary <实际 Hub CLI>` 单独登记查询与同步程序，不能覆盖报告程序或修改宿主权限。未设置时两个用途共用 `binary_path`。元数据不包含凭据、登录态或聊天记录；CLI 内部完成对应 endpoint 鉴权。

所有权清单记录安装器标识与两个受管文件的 SHA256。相同内容再次安装保持文件不变；升级只替换清单仍匹配的受管文件，保留其他本地文件。没有清单、内容被手工编辑、目标为软链接或目录路径包含软链接时拒绝覆盖，用户可以先检查并备份冲突目录，再明确选择安装位置。

文件先写入同目录临时文件、同步后重命名；安装器不会在失败时删除原文件来强行覆盖。Windows 上宿主占用文件可能使更新失败，应关闭占用后重试。多个本安装器通过本机锁协调；崩溃留下的锁需要先检查，不能盲删正在使用的锁。升级中途异常也可能留下清单与文件不匹配，此时停止更新并保留文件供核对。这不是对同一 OS 账号下其他进程的安全隔离。

### 自动同步与版本

`install-skill` 成功后将该位置登记到私有数据目录的 `skill-installations.json`，并同步本 Hub 已登记的受管位置；`serve` 启动时也同步。旧安装需要按原路径、endpoint 和报告程序显式重装一次才能加入登记，不扫描其他应用。删除技能目录视为卸载，同步只报告 `missing`，不会偷偷装回来。

```sh
agi-hub skills-status
agi-hub sync-skills
agi-hub sync-skills --endpoint workbuddy
agi-hub peers --endpoint codex
agi-hub skill-rules --endpoint codex --content --known-version 'sha256:previous-digest'
```

均支持 `--data <私有数据目录>`。`skills-status` 只读；全量同步供本机安装维护，技能请求维护时只用自身 `--endpoint`。同步返回每处 `current / updated / conflict / missing / error / incompatible`、磁盘版本、期望版本及 `host_load_state=unknown`；冲突会使同步命令失败，但不会阻止 Hub 恢复消息记录。修改、未托管目录、符号链接、目录路由不一致和占用失败均保留供核对。

规则版本使用完整 SHA256，独立于 manifest 格式 `1`。维护者修改内嵌技能时必须增加 `install.SkillRevision`；私有登记保存认可的修订与摘要，拒绝旧修订降级和相同修订换正文。同步前还核对运行中 Hub 与 CLI 的规则是否一致；只有连接明确被拒绝才允许离线维护，超时或鉴权错误不能当作离线。尚未实现本机文件同步的旧版本程序仍可能显式覆盖技能，不要保留旧安装脚本并行维护同一位置。

目录 API `GET /v1/catalog?endpoint=ID` 只返回该 endpoint 配置的 `visible_peers` 中已启用的成员，默认空；没有凭据、原生会话地址、工作区路径或聊天历史。可在 binding 中设置 `"visible_peers": ["workbuddy", "claude"]`，用 `purpose` 填短用途；可见性不授予任务提交权。配置仍需重启 `serve` 才生效。目录内容有独立摘要版本，包含规则版本，业务按需查询即可获得最新目录，不把名单写进每份技能，也不发送模型唤醒通知。

`GET /v1/skills?endpoint=ID` 默认只返回版本；加 `content=1` 才取正文，`known_version` 相同时省略正文。旧回报专用安装不能查询时继续处理原协议内已投递任务，标明同步查询不可用，不用管理提交绕过能力限制。多跳发送与关联回复仍属于[后续对等协作方案](TEAM-ORCHESTRATION.md)，本次维护接口不扩大消息协议。

### 各宿主如何生效

核查于 2026-10-19；安装文件、宿主发现、模型已经读取是三个不同事实。

| 宿主 | 更新和加载方式 | 唤醒或发消息的作用 |
| --- | --- | --- |
| Codex | 官方支持自动检测；本机 `0.163.0-alpha.10.2` 零模型 `skills/list(forceReload:true)` 实际发现受管技能；未发现时按官方说明重启 | 可以请求助手执行获准的安装器，但无需为写文件启动模型；目录发现不证明原聊天已读新正文。[官方技能说明](https://learn.chatgpt.com/docs/build-skills) |
| Claude Code / 本机 Claude-3p Code `2.1.299` | 监测本地 `SKILL.md`；必要时 `/reload-skills`。旧正文仍留在历史，下次协作按需读最新版本 | 原生命令与注入普通消息不同，不能假设投递 `/reload-skills` 文本就执行命令；不要为了同步续接大上下文。[技能生命周期](https://code.claude.com/docs/en/skills#skill-content-lifecycle) |
| WorkBuddy Desktop `5.8.3` / CodeBuddy CLI `2.161.0` | 本机桌面包监听技能目录变化；新 `--resume` 进程重新扫描其实际配置目录。长驻 CLI 的 `/reload-plugins` 会同时重载 Skills、Hooks、MCP/LSP，需单独选择 | 写受管文件无需模型；不会抹掉原会话历史中的旧说明。`CODEBUDDY_CONFIG_DIR` 决定实际目录，不混用桌面和独立 profile。[技能](https://www.codebuddy.cn/docs/cli/skills) · [刷新命令](https://www.codebuddy.cn/docs/cli/slash-commands) |
| Grok Bot | 当前确认的是云端 Private skills 与 Routine，没有核实通用技能 CRUD API 或本机技能目录，不加入文件同步 | 官方支持通过聊天保存技能，但要消耗模型并核对保存结果；Routine webhook 只启动任务。该维护通道待本机验收。[官方说明](https://docs.x.ai/grok-bot/skills-routines-and-automations) |

Claude 的 Client 插件代码更新另走原生 plugin update 和 `/reload-plugins`，普通技能或目录变化不重装插件。不要自动 `--force`，它可能让下一次请求失去提示词缓存。[插件更新](https://code.claude.com/docs/en/discover-plugins#keep-plugins-updated)

因此，消息唤醒可以作为用户已授权维护的入口，不是统一的技能更新机制。默认让本机安装器直接更新受管文件；需要原生刷新或云端保存时按适配器能力处理。普通协作消息不能新增安装位置、批准权限或扩大互通范围。[同步设计](TEAM-ORCHESTRATION.md#技能随-hub-同步更新)

## 收件与回报标识

技能核对 Envelope 的 `recipient` 与安装元数据的 `endpoint_id` 相同，并保留 `message_id`、`task_id`、`correlation_id`、`reply_to`、`kind`。最终结果由 Hub 分配独立的 `message_id`，令 `reply_to` 指向原交接，继承任务及关联 ID 后返回保存的原入口。当前 V0 的 `correlation_id` 等于原任务 ID；技能不自行选择新收件人、创建下一轮任务或把 `kind=result` 再执行一次。

固定 `session_id` 标识长期会话，`Report.run_id` 标识本次任务执行。优先使用实际原生 run/turn ID；宿主未公开时使用通知提供的任务专属 `hubrun-...` 并明确它是 Hub 标识。各回执使用独立稳定的 `event_id`，同一任务保持同一 `run_id`；不要跨任务复用固定会话 ID。

`result_queue_while_busy_verified` 是 endpoint 的可选结果投递能力，默认关闭，只在实际核实原生忙时收件后配置。它不赋予技能引导或打断能力，不会解除权限提示，也不能证明 Codex App Server 或其他入口已通过原生测试。

## 原生验收

安装命令成功后在所选宿主核对技能是否出现，并在指定会话验证它实际读取 `installation.json`。使用一个用户明确授权的实际任务，记录以下证据：

1. 原生入口在已核实空闲的状态收到 Envelope 并开始处理；配置和技能安装本身不是唤醒证据。
2. `accepted/running` 来自接手会话，保持同一 `task_id` 和任务专属执行标识；固定的 `session_id` 不充当多次运行 ID，宿主未公开原生运行 ID 时明确该限制。
3. 需要用户处理时保留原生提示并回报 `needs_user`；安装器不自动批准权限或代替用户发送消息。
4. 完成后回报真实摘要与所需成果文件；重复消息不重新执行，返回的 `kind=result` 只展示结果。
5. 原入口收到结果，任务记录能区分投递、接手和业务完成。

当前队列与驱动能力边界见 [适配器](ADAPTERS.md)，回执字段与文件归属见 [协议](PROTOCOL.md)。

本轮 WorkBuddy 独立 CLI 已验证技能实际加载与主动回报。首个验收任务确认原生明确拒绝且未执行后，修正技能读取、实际工作区读写和专属报告 helper 的窄范围配置，再按真实绝对目录恢复；第二个任务由 SSE 自动领取，在同一既有会话中首次执行即完成。Client 未传工具或权限覆盖参数，两次均由原生会话主动提交 `accepted/running/succeeded`，并生成 SHA-256 核验一致的成果文件。

两个任务使用不同执行标识，返回消息关联正确；重复 Submission 与已保存原生 Report 均去重，终态资源释放已核对。2026-10-14 的结果经 Codex 宿主内官方桌面工具引导回原聊天。2026-10-15，独立 Node 进程在自然宿主环境下真实调用官方 Desktop MCP，取得匹配目标的成功 ACK，消息也已即时引导到忙碌原聊天；随后常驻 Node Client 已自动完成一条新任务的 MQ → 固定独立 WorkBuddy CLI 会话 → 原生回报与文件核验 → 独立 MCP 返回忙碌原聊天的真实闭环，无手工转发或人工点击；同一 Desktop MCP 通道随后也通过一次独立空闲诊断：确认 idle 和旧轮次 completed 后单次发送，原聊天实际进入不同的新轮次，官方桌面 read_thread 仍可调用。此前两轮独立 CLI 的完整 MQ 验收是 busy 返回；本次诊断本身没有新增 MQ 任务，不能证明 idle 下完整 MQ 回路。这与未开放控制 socket 的 App Server proxy 是不同通道。

同日原 WorkBuddy 会话与原配置的默认认证续聊两次成功。查询先被本地 Bash/Read 拒绝，精确规则修正后曾实际执行并返回远端 `850003 / authorization expired`；当时的临时五条规则已恢复原字节。这些历史阶段保留，用户重新授权后的新查询已成功取得候选。

本次业务使用原 endpoint、会话、工作目录及 `.workbuddy`，受管技能绑定固定 endpoint-scoped 报告 helper，只向 `/v1/report` 提交，并使用独立成果工作区。四条报告安装许可窄到技能、成果目录与指定 helper；临时四条单查询许可在完成后移除，仅留下报告安装许可。首轮已投递但受旧诊断限制未接手，核对零工具、文件及 Report 后，由入口核对并显式续接一次后才执行；原 stdin 是新消息，Client 的固定 positional 指令只明确本轮交接，不扩大用户授权。原任务、资源锁与已投递状态均保留，没有 requeue 或伪造回执。

原生随后真实提交 `accepted/running/succeeded`；一次官方随包联系人 search 的原始结果返回候选，姓名、部门、职务与成果一致，Core 核对文件后生成关联 result，经独立 Node Desktop MCP 自动回传忙碌原入口，资源释放、原历史前缀完整。未禁用 sandbox 或复制桌面私有 env/auth/ticket。该证据不代表首轮无需显式续接、全通讯录唯一、全部企微业务或完整桌面工具注入。发现或安装技能不等于批准本地工具操作，报告受阻时仍保留真实提示，不伪造回执。[本机验证记录](NATIVE-VALIDATION.md)
