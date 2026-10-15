# 原生助手技能安装

每个接入会话需要知道怎样处理任务信封、按真实执行状态回报，以及怎样展示返回结果。AGI Hub 提供同一份 `agi-hub-client` 技能，由本机 CLI 安装到用户选定助手的技能目录。技能是协议说明；消费通知和唤醒现有会话仍由各宿主适配器负责。

## 安装

先按 [安装指南](INSTALL-FOR-ASSISTANT.md) 构建、初始化边车，并配置用户明确要接入的 endpoint。使用实际安装的程序和私有数据目录：

macOS / Linux：

```sh
/absolute/path/agi-hub install-skill --host codex --endpoint codex --data /absolute/path/private
/absolute/path/agi-hub install-skill --host workbuddy --endpoint workbuddy --data /absolute/path/private
```

Windows PowerShell：

```powershell
& 'C:\Users\you\bin\agi-hub.exe' install-skill --host codex --endpoint codex --data 'C:\Users\you\private\agi-hub'
& 'C:\Users\you\bin\agi-hub.exe' install-skill --host workbuddy --endpoint workbuddy --data 'C:\Users\you\private\agi-hub'
```

以上是路径示例，须替换为本机真实绝对路径与已配置的 endpoint。安装流程适合交给当前 AI 助手直接执行，不需要下载脚本、额外包管理器或修改宿主权限设置。CLI 的 `--binary` 可明确记录接手助手以后使用的真实程序绝对路径。

安装器只写协议说明和安装元数据，不修改宿主工具权限。非交互 CLI 的实际回报还需要宿主允许读取该安装元数据、执行对应报告程序以及访问本次任务工作区；技能被发现不代表这些操作已经获准。用户选择官方 CLI 自身的窄范围配置，交接消息和技能不能代替用户批准，也不应授予无关目录或命令权限。

| 宿主 | 默认技能目录 | 支持边界 |
| --- | --- | --- |
| Codex | 当前用户 `~/.codex/skills` | 安装协议技能；既有会话是否加载需在实际会话中核对 |
| WorkBuddy | 当前用户 `~/.workbuddy/skills` | 根据当前 5.8.2 本机扫描实现选择；须通过技能界面和真实任务核对加载，不作为全版本保证 |
| 其他宿主 | 无默认值 | 使用官方说明或本机已核实的技能目录，显式指定 `--skills-dir`；安装相同说明不代表支持唤醒 |

自定义目录示例：

```text
<installed-binary> install-skill --host <host> --endpoint <configured-endpoint> --data <absolute-data-dir> --skills-dir <verified-absolute-skills-dir>
```

尖括号是待替换参数。只安装到用户选定宿主，不批量扫描应用配置、提取登录态、注册其他助手或切换 leader。不同宿主的技能格式和加载条件需要分别核实；如果需要 UI 导入，使用宿主原生技能界面完成并保留用户审批流程。

使用 WorkBuddy 的独立 CLI 配置目录时，通过 `--skills-dir` 选择该 CLI 环境实际扫描、已核实能加载的技能目录；桌面默认目录不会自动成为每个 CLI profile 的技能目录。先核对会话加载证据，再配置技能读取、指定报告 helper 与本次工作区文件权限。

## 安装内容与更新

安装目录固定为技能根目录下的 `agi-hub-client/`：

```text
agi-hub-client/
  SKILL.md
  installation.json
  .agi-hub-manifest.json
```

`installation.json` 只包含 `host`、`endpoint_id`、`binary_path`、`data_dir` 四个字段，供技能找到同一台电脑的回报程序与对应 endpoint。它不复制管理凭据、原生登录态或聊天记录。CLI 在内部加载私有边车配置并使用对应 endpoint 凭据；技能不需要读取 token。

所有权清单记录安装器标识与两个受管文件的 SHA256。相同内容再次安装保持文件不变；升级只替换清单仍匹配的受管文件，保留其他本地文件。没有清单、内容被手工编辑、目标为软链接或目录路径包含软链接时拒绝覆盖，用户可以先检查并备份冲突目录，再明确选择安装位置。

文件先写入同目录临时文件、同步后重命名；安装器不会在失败时删除原文件来强行覆盖。Windows 上宿主占用文件可能使更新失败，应关闭占用后重试。多个本安装器通过本机锁协调；崩溃留下的锁需要先检查，不能盲删正在使用的锁。升级中途异常也可能留下清单与文件不匹配，此时停止更新并保留文件供核对。这不是对同一 OS 账号下其他进程的安全隔离。

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

两个任务使用不同执行标识，返回消息关联正确；重复 Submission 与已保存原生 Report 均去重，终态资源释放已核对。2026-10-14 的结果经 Codex 宿主内官方桌面工具引导回原聊天。2026-10-15，独立 Node 进程在自然宿主环境下真实调用官方 Desktop MCP，取得匹配目标的成功 ACK，消息也已即时引导到忙碌原聊天；随后常驻 Node Client 已自动完成一条新任务的 MQ → 固定独立 WorkBuddy CLI 会话 → 原生回报与文件核验 → 独立 MCP 返回忙碌原聊天的真实闭环，无手工转发或人工点击；空闲唤醒仍待单独验收。这与未开放控制 socket 的 App Server proxy 是不同通道。

同日原 WorkBuddy 会话与原配置的默认认证续聊两次成功。新企微查询最初被本地 Bash/Read 拒绝；随后精确 allow 规则按公开解析器修正 JSON/shell 外层引号归一化，离线反例中的追加命令、改关键词和重定向仍拒绝。原会话已用官方随包 wecom-cli 实际执行搜索，原始工具结果显示调用完成但返回远端 `850003 / authorization expired`，未取得联系人数据；需该机器人创建者恢复通讯录授权。

本次没有禁用 sandbox、复制桌面私有 env/auth/ticket 或盲重试，临时五条 allow 规则已将 settings 精确恢复到原字节。技能安装、CLI 认证、命令执行与数据获取分别验收；当前结果不证明完整桌面工具注入继承。两轮 MQ 忙时回传的成果和资源释放已核对，第二轮更正了因果顺序。发现或安装技能不等于批准本地工具操作，报告受阻时仍保留真实提示，不伪造回执。[本机验证记录](NATIVE-VALIDATION.md)
