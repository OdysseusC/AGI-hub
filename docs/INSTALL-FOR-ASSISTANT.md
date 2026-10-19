# 交给 AI 助手的安装与使用指南

这份文档可以交给能够操作本机终端与文件的 AI 助手协助安装。当前仍需手动核对会话与原生接入；“交给任意通用 AI，一步安装并把当前助手自动注册成 leader”是后续目标，尚未实现。

## 安装助手的执行约定

先检查实际 OS、CPU 架构、Go 和宿主助手，不假设另一台电脑的工程、配置或能力已经存在。只读取本次安装需要的公开说明和用户明确提供的配置，不扫描真实聊天历史，也不从其他应用提取登录凭据。

创建边车私有配置，不覆盖已有 token。接入方式、会话 ID 与工作区由用户指定或用已授权的原生机制核对；没有证据时保持绑定禁用。当前入口只有在接入与结果返回已验证后才可以设为 leader。权限提示交给用户，不能由助手自动批准。

## 1. 构建程序

从用户提供的本项目源码目录开始。构建 Hub 需要 Go 1.22+，零第三方 Go 依赖。Python Client 使用 Python 3.11+ 标准库，Codex Desktop MCP 使用 Node 22+ 标准库，Claude Code mod 使用宿主 API；按选择的 Client 检查对应运行时，不安装第三方包。

macOS / Linux：

```sh
go build -o agi-hub ./cmd/agi-hub
./agi-hub init
./agi-hub doctor
```

Windows PowerShell：

```powershell
go build -o agi-hub.exe ./cmd/agi-hub
.\agi-hub.exe init
.\agi-hub.exe doctor
```

当前没有在本指南提供 Release 下载地址或一键安装脚本。可以把构建好的单文件程序放在用户选定的位置；让接手助手能找到同一个已安装程序，或在其接入说明中记录真实绝对路径。

所有命令都支持 `--data <本机私有目录>`。若使用自定义目录，服务、提交与回报必须采用同一目录。未指定时使用系统用户配置目录下的 `agi-hub`。`init` 已存在时拒绝覆盖；已有安装应保留其配置和历史。

## 2. 编辑私有配置

在 `init` 打印的 `config.json` 中按需修改，保留已生成的 `admin_token`，不要将整个文件粘贴到公共聊天或提交仓库。

| 字段 | 设置方式 |
| --- | --- |
| `device_id` | 当前设备的稳定 ID，所有本机 endpoint 和文件引用保持一致 |
| `listen` | 数字 loopback 地址，默认 `127.0.0.1:7331` |
| `workspaces` | 工作区 ID 到现有本机绝对目录的映射 |
| `bindings` | 每个助手及其具体原生会话，`driver.type` 固定为 `external` |
| `leader` | 初始为空；实际验证后通过命令选择 |

例如工作区映射可以是 `"daily-work": "/absolute/local/workspace"`，Windows 可使用 `"daily-work": "C:\\Users\\you\\work"`。这些是示例，须替换为本机现有目录。文件引用采用 `inputs/source.txt` 这样的正斜杠相对路径。

绑定核对 `endpoint.id / device_id / session_id / driver / modes`。产品连接参数分别保存在仓库外的 `client.json`；Hub 配置不再存可执行文件、Webhook URL 或原生凭据。Client 在领取前核对 Hub 返回的自身绑定，Codex 的 thread 必须与 session_id 一致。WorkBuddy 官方本地助理使用专用标识 `localassistant`，不表示可定位普通 Chat；CLI 分支要求绑定已验证的既有会话 ID、工作目录及配置目录，独立 CLI 与桌面会话须分别标注。[独立 Client](CLIENTS.md) 提供配置格式。

`init` 创建 Codex、WorkBuddy、Grok、Claude 四个禁用绑定。初始化的 `enabled=false` 和 `idle_wake_verified=false` 不能未经实测改为 true。旧开发版把原生参数写在 Hub `driver` 内的配置会被拒绝；先保留原文件及记录，再将产品参数迁移到对应私有 Client 配置。

原生验收建议先选一个专属会话，做用户明确授权、可逆的任务，确认实际接手、原生工具保留和结果回报。对云端 Routine，要先解决授权回到本机的报告通道；本地 loopback 地址不能直接供云端使用。完成这些验证后才启用绑定。当前修改配置需要重启 `serve`。

### 安装会话技能和生成 Client 配置

先将 Hub 可执行文件放在稳定的安装位置，再用这个已安装程序执行：

```sh
agi-hub install-skill --host codex --endpoint codex
agi-hub install-skill --host workbuddy --endpoint workbuddy
agi-hub install-skill --host claude --endpoint claude
agi-hub client-config --endpoint codex --file /absolute/private/codex/client.json
```

Windows 使用 `agi-hub.exe` 及该电脑的绝对路径。上述 endpoint 必须对应本机配置。`client-config` 只写该 endpoint 的 scoped token 和空 `native` 到新私有文件，拒绝 Git 仓库内路径、符号链接父目录或覆盖已有文件；再按该 Client 的示例填写原生参数。

技能安装记录程序路径、数据目录和 endpoint，不包含凭据。默认支持 Codex、当前 WorkBuddy 桌面版本、Claude Code 的用户技能目录；在原生应用核对是否发现并启用，其他宿主用明确验证过的 `--skills-dir`。[技能安装支持范围](SKILLS.md)

安装成功会登记受管位置，并同步该 Hub 已登记的技能；后续 `serve` 启动也同步。原有安装须在原路径重装一次加入登记。使用专属回报 helper 时保留 `--binary <原 helper>`，加 `--hub-binary <本机 Hub CLI>` 分开查询与回报；不修改原生工具授权。`skills-status` 核对磁盘，`sync-skills --endpoint ID` 限定更新范围。用户修改的文件或已卸载目录不会被覆盖或恢复；加载状态仍为 unknown，按宿主机制核对。

其他助手可见范围在 binding 设置 `"visible_peers": ["获准成员ID"]`，默认空，变化后重启 `serve`。技能按需查询目录，新增成员不发送模型通知。当前目录不授予端到端发起任务权限，多跳协作仍在后续方案中。

Codex、WorkBuddy、Grok 使用独立 Client 进程，例如：

```sh
python3 clients/codex/main.py --config /absolute/private/codex/client.json
```

Hub 不自动启动 Client；同一 endpoint 只运行一个消费者。Claude Code 不另起消费者进程，由原会话内插件消费，安装方式见下文。每个 Client 单独配置与更新，既有 native 执行不随 Client 停止而取消。WorkBuddy OAuth 缺失时在领取前停止，不能用桌面登录态代替授权。

### Claude Code：安装到打开的原会话

当前实现以 Claude Code **2.1.299** 开发与校验。先检查目标宿主实际版本；桌面内置 CLI 与 PATH 中的 CLI 可能不同，不能只看桌面应用版本。只支持加载 mods 的本地 Code 会话；Chat、Cowork、WSL 不支持。

1. 保留用户指定的原 Code 会话，取得其真实 UUID；不要新开独立 `--resume` 进程代替桌面验收。配置 Hub `claude` binding 的 `driver="claude-code"` 与 `session_id`，先保留 `idle_wake_verified=false`。
2. 导出仓库外的私有配置，再按 [配置示例](../clients/claude-code/client.example.json) 补 `native`，同一 UUID 同时用于 Hub 与插件配置。令牌只在该私有文件，不能放进插件 userConfig、提交或聊天。
3. 用本机已核实的 `claude` CLI 从本地 marketplace 安装：

   ```sh
   claude plugin install agi-hub-endpoint --marketplace /absolute/path/to/AGI-hub/clients/claude-code --scope user --config client_config=/absolute/private/claude/client.json
   ```

   Windows 使用该电脑上的绝对路径；如果 CLI 不在 PATH，用当前桌面宿主的实际二进制路径代替 `claude`。插件 userConfig 仅记录私有配置文件路径。
4. 使用受管安装器安装 `--host claude --endpoint claude` 技能；在原生权限配置里仅允许 report 工具和本次任务必要的文件操作。保留本人权限提示，不能用插件消息代为批准。
5. 在目标 Code 会话运行 `/reload-plugins`，然后 `/agi-hub-id`、`/agi-hub-status`；UUID 应保持原值。无关会话应显示休眠。同 endpoint 的另一进程由 Hub 原子租约拒绝消费。
6. 在确认空闲的目标会话运行 `/agi-hub-diagnose`，等待一次插件来源的新回合。只有实际通过 N2 才启用 `idle_wake_verified`，再执行带成果文件的 N3 完整闭环，分别记录接手、执行、原生 turn ID、文件 SHA-256、结果返回与资源释放。

目标会话需要保持加载插件。关闭应用后的启动、忙时普通任务引导和打断不在本 Client 范围。重载或会话切换会重新核对绑定与租约；未决消息仅对账，不重新提交。自动化校验通过也不能代替上述原会话验收。[插件完整说明](../clients/claude-code/README.md)

## 3. 启动与核对会话

```sh
./agi-hub serve --open
```

保持服务运行，在另一终端检查记录：

```sh
./agi-hub status
./agi-hub dashboard
```

`doctor` 不发消息，也不能证明原生空闲唤醒可用。每次启动后所有会话都是 `unknown`；实际核对绑定的原生会话后再声明状态：

```sh
./agi-hub session --endpoint codex --state idle
./agi-hub session --endpoint grok --state idle
./agi-hub leader --endpoint codex
```

以上 endpoint 名称须存在于自己的配置。`busy / needs_user` 声明还需 `--target-run <真实运行 ID>`；不要把尚未核对的会话设为 idle。leader 必须启用、支持 queue，并验证过空闲唤醒或忙时接收成果。忙时成果能力不能替代普通任务的空闲唤醒验收。当前边车不自动识别当前交谈助手。

## 4. 提交任务

简单任务可使用命令行参数：

```sh
./agi-hub submit --to grok --goal "整理明确提供的资料并返回摘要" --context "必要上下文，避免复制全部聊天历史"
```

CLI 默认采用当前 leader 为 origin、queue 模式、一次交接和提交后 30 分钟的投递截止时间。输出里的任务 ID 用于跟踪后续回报。

需要文件引用和明确授权范围时，保存完整 Submission JSON，再使用 `submit --file task.json`。以下为语法示例，设备、工作区、文件、endpoint 和截止时间都必须按实际情况填写：

```json
{
  "id": "daily-summary-001",
  "origin": "codex",
  "recipient": "grok",
  "goal": "读取指定输入，生成 daily-work 工作区内的 summary.md 并返回成果引用",
  "context": "只处理本次指定资料",
  "mode": "queue",
  "inputs": [
    {
      "device_id": "this-device",
      "workspace_id": "daily-work",
      "path": "inputs/source.txt"
    }
  ],
  "resources": ["workspace:daily-work"],
  "require_artifact": true,
  "authorized_actions": ["读取指定输入文件", "在 daily-work 内创建 summary.md"],
  "max_handoffs": 1,
  "deadline": "<未来的 RFC3339 时间，例如带 +08:00 时区的时间>"
}
```

输入必须已经存在于同机 allowlist 工作区；也可提供 `sha256` 要求匹配。边车传引用，不自动复制或冻结输入。需要重试同一提交时保存并复用完全相同的 JSON（包括 deadline），不要换 ID 规避未解决的投递状态。

`steer / interrupt` 只有经过实际验证的 endpoint 模式才可使用，还必须指定匹配的活跃 `--target-run`。普通任务控制尚未接入；Codex 可选 app-server 只对成果通知使用原生引导，详见各 Client 的实际支持范围。[协议边界](ARCHITECTURE.md)

## 5. 接手助手回报

接手助手在实际接手后发回 accepted，执行期间可回报 running，需要用户时回报 needs_user；最终按真实结果回报 succeeded、failed 或 interrupted。

下面是 Report 的字段示例，不是可以冒充实际执行的收据：

```json
{
  "event_id": "daily-summary-001-accepted",
  "task_id": "daily-summary-001",
  "endpoint": "grok",
  "state": "accepted",
  "run_id": "native-run-example-001",
  "summary": "已实际接手任务",
  "artifacts": []
}
```

每条新回报使用新的稳定 `event_id`；重发同一条回报时保持 ID 和全部内容不变。保持同一任务的真实 `run_id`，回报的 endpoint 必须是任务接收端。

宿主有 `mcp__agi-hub-endpoint__report` 等原生回报工具时优先使用；Claude 插件自动固定 endpoint 与首次报告的真实 `claude-turn:<turn-id>`，同一任务后续保持一致。其他已配置环境可用以下两种 CLI 回报方式：

```sh
agi-hub report --endpoint grok
agi-hub report --data <configured-local-data-directory> --endpoint grok --file <local-report-json-path>
```

第一种读取 stdin 的完整 Report JSON；第二种读取本机文件。尖括号表示必须替换的参数，不是可直接执行的命令。Windows 使用已安装的 `agi-hub.exe`。如果当前目录没有程序或 PATH 未配置，使用安装时记录的实际路径。

CLI 从私有配置使用对应 endpoint 的 scope token，不打印凭据。接手助手不能另选 endpoint 冒充他人，也不能通过回报批准原生权限提示。

成功回报应将 state 改为 succeeded，使用新的事件 ID，保留 run_id，填写真实摘要与文件引用，例如：

```json
{
  "event_id": "daily-summary-001-completed",
  "task_id": "daily-summary-001",
  "endpoint": "grok",
  "state": "succeeded",
  "run_id": "native-run-example-001",
  "summary": "已完成摘要，成果为 summary.md；请填写真实结果说明",
  "artifacts": [
    {
      "device_id": "this-device",
      "workspace_id": "daily-work",
      "path": "summary.md"
    }
  ]
}
```

任务要求成果时，文件必须实际存在、可读取并通过工作区与校验检查。`sha256` 可由接手端提供；未提供时边车计算。摘要质量仍由用户验收，文件存在性不证明内容质量。终态触发一次结果通知送回原入口；原入口看到 result 后展示成果，不重新执行任务或对原任务再回报 accepted。

## 6. 核对故障与日常验收

| 现象 | 下一步 |
| --- | --- |
| pending 且会话 unknown | 在原生应用核对真实状态，再声明；不要盲改为 idle |
| delivered 但无 accepted | 检查指定会话是否实际接手与回报通道；工具接收不代表执行 |
| uncertain | 保留关联 ID，检查原生会话或正式回执，不换 ID 盲重发 |
| Client 停止，消息一直 dispatching | 核对原生接收后，用 `delivery --endpoint NAME --message-id ID --outcome uncertain --detail "实际核对说明"` 记录；不得改回 pending 或清空记录 |
| needs_user | 用户处理原生提示或判断，再由接手端回报后续状态 |
| 成功文件校验失败 | 核对设备、工作区、相对路径、文件实际存在及 SHA |
| 日志达到上限或校验失败 | 保留私有数据与历史，停止追加；不要清空日志冒充恢复 |

完成一个真实日常任务后，记录使用了哪个入口、交接次数、手动切换/复制/传文件/催问次数，以及成果质量。原生闭环验收通过后再扩大助手数量。

返回 [README](../README.md) · 查看 [架构](ARCHITECTURE.md) · [适配器](ADAPTERS.md)
