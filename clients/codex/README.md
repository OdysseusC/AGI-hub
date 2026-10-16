# Codex Client

Node Client 版本 `0.3.0`，Hub 协议 `1`。默认入口是 Node.js 22+ 标准库实现的 `desktop_mcp_client.mjs`，不需要 npm 安装或第三方库。它使用官方 `codex-app-tools` MCP server 向已绑定的原桌面聊天返回成果，显式启用后也可接收普通任务，保留原会话的工具、模型和权限。

每个 Hub endpoint 只启动一个消费者。默认只接受 `kind=result / mode=queue`：展示已完成任务的结果，不重新执行任务，不发送业务接手与进度回执。Node 私有配置显式设置 `native.receive_tasks=true` 后，允许接收 `kind=task / mode=queue`；仍拒绝 `control`、`steer` 和 `interrupt`。传输不确定时不会重发或回退到 `codex queue`。

## 官方 Desktop MCP

`native.transport="desktop-mcp"` 调用官方 server 暴露的固定工具 `send_message_to_thread`。客户端先完成 MCP 初始化、核对工具参数，再订阅 Hub SSE，随后领取消息。`threadId` 固定来自私有配置，调用者身份只来自宿主已提供的 `CODEX_THREAD_ID`；Envelope 的 recipient 必须匹配固定 endpoint，不能替换目标、调用者、工具、启动参数或权限。工具参数仍只有 `threadId` 和 `prompt`。

普通任务通知保留完整 Envelope 和既有授权边界，并要求原聊天使用安装时固定、限定 endpoint 的报告 helper 主动提交完整 Report。安装时须提供该 helper 的真实程序路径及允许的报告工作区，不能从 Envelope 读取或覆盖这些配置。任务使用与 `clients/shared/notification.py` 相同的 `hubrun-` 标识：对 endpoint、换行和 task_id 的 UTF-8 字节做 SHA-256，取前 32 位小写十六进制；这只是 Hub 执行标识，不能冒充原生 run ID 或复用固定 session_id。实际接手、执行和完成分别由原聊天报告；Client 只保存传输回执，不自动报告 accepted/running/succeeded，也不读取聊天历史代收结果。

`receive_tasks` 不改变 Hub 的调度门槛。普通任务仍受会话状态、资源与空闲唤醒验证约束，忙时不会因开启此项而投递任务；默认状态仍为 `unknown`，Client 不自行标记 idle。普通任务能力已通过本地模拟测试，2026-10-16 Windows 本机另已完成 Codex 单入口的原生任务自检；其他会话与跨助手链路须分别验收，不能沿用历史结果返回证据。[Windows 自检范围](../../docs/NATIVE-VALIDATION.md#windows-codex-selfcheck-20261016)

官方工具没有 `mode` 参数。本机公开实现采用 `send-now` follow-up，忙时进入原生引导路径，空闲时开始新轮次。本机 macOS 已通过独立官方 Node 进程启动 server、列举真实工具并单次调用，返回 `isError=false` 和匹配的原聊天 ID；结果已即时进入原聊天，无需用户点击队列。

完整忙时闭环也已实际通过：先启动 Node 消费者并确认 SSE 订阅，再提交新任务；WorkBuddy 默认恢复原 CLI 会话并主动报告接手、执行和成功，Hub 核对成果文件及 SHA-256 后生成独立结果。Node 自动领取结果，经官方 Desktop MCP 即时引导回运行中的原 Codex 聊天；任务与两段投递均为成功终态，结果关联正确、资源释放。整个返回阶段没有依赖原聊天里的模型代为调用工具。两轮完整 MQ 回路均只验证本机忙时结果返回。随后同一官方 Desktop MCP 通道已另行通过一次空闲启动诊断，详见下节；idle 下完整 MQ 回路、普通任务控制及其他平台仍未验收。

### 单次空闲原聊天启动诊断

2026-10-15，上一轮结束后，独立官方 Node 程序只延时一次，并在唤醒前仅调用一次 `read_thread`，确认 `thread.status.type=idle`、前一轮次为 `completed`。随后只调用一次官方 Desktop MCP `send_message_to_thread`，取得 `isError=false` 与匹配目标聊天的 ACK。

原聊天实际收到消息并开始新轮次，新 turn ID 与前一个 completed turn 不同；官方桌面 `read_thread` 工具仍可调用，未换成独立 CLI/SDK 会话。没有重复唤醒、手工转发或新建 MQ 业务任务。

该单次诊断证明相同官方 MCP 传输在本机能启动空闲原聊天；不能把它写成第三次 MQ 业务闭环或 idle 下完整 MQ 回路通过。两轮完整 MQ 验收仍是 busy 返回。[分段证据](../../docs/NATIVE-VALIDATION.md)

Desktop MCP 和 App Server proxy 是两个独立入口。前者由官方 server 使用宿主提供的桥接环境；后者连接 `app-server-control.sock`。当前宿主未开放 proxy 控制 socket，不代表 Desktop MCP 也不可用。

当前 macOS 正式版宿主校验连接进程及父链的签名。实测可用启动链为官方 Node 客户端直接启动官方 Node server；需要从当前已提供官方环境的 Codex 宿主正常执行。Python 父链在这台电脑导致工具发现连接关闭，单独安装一个系统 Node 或脱离宿主后台启动也不能据此保证可用。不要复制桥接票据、持久化 pipe 值、猜测私有 socket 或更改身份来连接。

### 2026-10-16：Windows Codex 单入口自检

Node `0.3.0` 显式启用 `native.receive_tasks=true`，以 `origin=recipient=codex` 提交自检。真实 Hub SSE → 常驻 Node Client → 官方 Desktop MCP → 原聊天链路完成了任务投递；原聊天主动保存并提交 accepted/running/succeeded 三个稳定事件，实读输入文件中的随机校验值，并实际调用官方桌面 `list_artifacts`。核对目标原聊天匹配、Hub 成果 SHA-256 一致、task 与 result 均 delivered、资源锁释放后，再次核对原聊天最终回复，确认对应 result 已实际消费。

独立空闲唤醒探针已另行通过；本轮未设置 `result_queue_while_busy_verified`，不据此新增 Windows 忙时结果能力声明。这是 Codex 自检，既不是 Grok 发起，也不证明 Grok 跨助手闭环。Grok 配置衔接截至本机 12:13 仍未完成，其唤醒标记保持 false。五份旧传输源码随后已按用户要求删除，历史消息、回执与协议保留；Grok 迁移验收仍未完成。六平台包已重建并核对本版 Node 源码与 manifest，未包含私有 `client.json`；打包不替代各平台的原生验收。[完整分段记录](../../docs/NATIVE-VALIDATION.md#windows-codex-selfcheck-20261016)

## 配置与启动

先在 Hub 注册 `codex` endpoint，令 `endpoint.driver="codex"`、`endpoint.session_id` 等于已核实的原生 thread ID，对应 binding 使用 `"driver":{"type":"external"}`。导出仅属于此 endpoint 的私有配置：

```sh
agi-hub client-config --endpoint codex --file /absolute/private/codex/client.json
```

导出以私有权限创建文件，不打印凭据或覆盖已有配置。按 [client.example.json](client.example.json) 填写 `native`：

| 字段 | 要求 |
| --- | --- |
| `transport` | `desktop-mcp` |
| `command` | 用户已核实的官方 Node 或官方 `launch_codex_app_tools_mcp` 的绝对可执行路径；Windows 必须为实际 `.exe` |
| `args` | 只包含一个官方 `server.mjs` 的绝对文件路径；不接受 Shell、身份参数或消息文本 |
| `cwd` | 该官方插件目录的绝对路径，必须是 `server.mjs` 所在目录 |
| `thread_id` | 已有原生桌面聊天 ID，与 Hub 绑定一致 |
| `timeout_seconds` | 单次 MCP 请求超时，默认 `30`，范围 `1` 至 `300` 秒 |
| `receive_tasks` | 可选，严格布尔值，默认 `false`；仅 Node 入口支持，设为 `true` 后接受普通 queue 任务及结果 |

`command`、`args`、`cwd` 必须来自实际安装的官方插件清单与运行环境，不猜目录。客户端只继承正常操作系统变量及官方 Node/桥接变量白名单，不从配置读取桌面凭据。宿主缺少 `CODEX_THREAD_ID` 或 `CODEX_APP_TOOLS_PIPE_PATH` 时，在领取消息前停止；这些变量的值不要复制到 `client.json`、文档或日志。

在已具备官方宿主环境的入口中，直接启动已核实的官方 Node。POSIX 示例中的 `exec` 让 Node 替代启动 shell，保持实际验证过的父链：

```sh
exec /absolute/path/to/official/node /absolute/path/to/agi-hub/clients/codex/desktop_mcp_client.mjs --config /absolute/private/codex/client.json
```

Windows 使用实际 `node.exe` 与本机绝对路径；不要把 `.cmd` 或 `.bat` 包装器当作进程入口。Node 启动官方 MCP helper 时设置 `windowsHide=true`，不弹出终端窗口。Windows、Linux 和不同 macOS 宿主的桥接与唤醒须重新验收。Hub 默认地址为 `http://127.0.0.1:7331`，仅接受数字 loopback 地址，实际地址以导出的配置为准。

首次启动与每次 Hub 重连都会重新核对 endpoint 绑定和 MCP 工具。先订阅 SSE 再领取，keepalive 不触发轮询。断线仅重建连接并扫描 Hub 仍可投递的 pending 消息；已经 `dispatching / uncertain` 的消息不会因此重新发送。客户端不自行宣称会话 idle，不自动处理权限提示。

只有工具返回 `isError=false`，且唯一文本内容可解析为匹配原 thread ID 的 JSON ACK，才记录 `delivered`。工具调用前的初始化或发现失败不会领取消息；调用开始后的超时、断线、工具错误或 ACK 不符记为 `uncertain`。回执保存失败时停止，保留 Hub 领取记录供核对。ACK 只证明原生接收，不能替代业务成功或空闲唤醒验收。

## 保留的 Python 入口

`main.py` 仍使用 Python 3.11+ 标准库，依赖同级 `shared` runtime `0.1.0`。Python Desktop MCP 和 App Server 仍只接收结果，旧 queue 传输保留原有队列行为；所有 Python 入口均不支持 `native.receive_tasks`，Node 的任务开关不能用于 Python 配置。复制或固定 Python Client 版本时须一起保留相邻的 `clients/shared`。Node Desktop MCP 入口不导入 shared；二者采用同一个 Hub 协议。

Python 明确选择以下一种传输，不能同时为同一 endpoint 启动 Node 与 Python 消费者：

| transport | 范围 |
| --- | --- |
| `desktop-mcp` | 相同官方 MCP，仅接收结果且不支持 `receive_tasks` 配置；当前 macOS Python 父链未通过宿主校验，供其他实际允许的宿主单独验收 |
| `app-server` | 官方 `codex app-server proxy` 连接已运行 daemon，仅结果返回；本机宿主未开放默认控制 socket |
| `queue` | 兼容 `codex queue --thread ID --message TEXT`；只证明队列接收，忙时需手点引导的路径不能记为自动返回 |

App Server 使用 `thread/read` 排除正文、`thread/turns/list` 只取元信息，忙时通过 `expectedTurnId` 保护的 `turn/steer` 返回结果；不处理普通任务或 interrupt。其 `native` 示例：

```json
{"transport":"app-server","thread_id":"<verified native thread ID>","executable":"codex","timeout_seconds":30,"result_mode":"steer","idle_start_verified":false}
```

App Server 自己的空闲 `turn/start` 仍未实测，不能由 Desktop MCP 的单次空闲启动替它通过；仅在此 App Server 分支单独核实原线程工具保留及空闲启动后显式开启；不自动 resume 未加载线程。旧 queue 配置为：

```json
{"transport":"queue","thread_id":"<verified native thread ID>","executable":"codex","timeout_seconds":30}
```

Python 启动命令：

```sh
python3 clients/codex/main.py --config /absolute/private/codex/client.json
```

## 维护与测试

实现、配置、manifest 和专项测试都在本目录。Node 测试仅使用内存 stdio 和本地 HTTP，不启动官方 server 或发送原生消息：

```sh
node --test clients/codex/test_desktop_mcp_client.mjs
python3 clients/codex/test_adapter.py -v
```

公共 Python 协议测试位于 `clients/tests`，仓库根目录运行 `python3 -m unittest discover -s clients/tests -v`。更新和不确定投递的处理见 [Client 文档](../../docs/CLIENTS.md)，分段原生证据见 [验收记录](../../docs/NATIVE-VALIDATION.md)。
