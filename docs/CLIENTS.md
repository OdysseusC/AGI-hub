# 独立助手 Client

每个 AI 助手有自己的 Client 进程，通过本机 Hub 的 REST / SSE 协议领取任务、报告投递。Go Hub 保存消息和状态；Client 只翻译该助手实际支持的原生入口。替换一个 Client 不需要重新编译 Hub，也不需要让其他助手退出。

这是一种进程级插件接口。没有使用 Go 动态库，也没有把 Python 嵌入 Hub。Python 是当前 Client 实现的一种可选运行时，后续 Client 可以使用其他语言，只要遵循同一协议。

## 目录和技术栈

```text
clients/
  shared/             # 标准库 HTTP、SSE、回执与通知格式
  codex/              # 已有 Codex 桌面 thread 的 queue CLI
  workbuddy/          # 官方 localassistant OpenAPI
  grok-routine/       # 用户配置的官方 Routine webhook
  tests/              # 本地模拟传输测试，不调用真实助手
```

每个助手目录有 `manifest.json`、`main.py`、`adapter.py` 和 `client.example.json`。`manifest` 声明协议版本、启动入口、会话类型和允许的发送模式；它不是原生能力的验证报告。Client 必须先在用户的具体电脑、助手版本和目标会话中完成唤醒和回传验收，才能把对应 Hub endpoint 的 `idle_wake_verified` 设为 `true`。

### 单仓库先行，以后拆分

第一版在同一仓库维护 Go Hub、各助手 Client 和 shared runtime。每个 Client 版本为 `0.1.0`，使用协议 `1`，manifest 固定依赖 shared `0.1.0`；`clients/shared/manifest.json` 单独记录运行库版本。Shared 是库，不能作为一个助手 Client 启动。

Client 具有独立的进程和维护边界，但目前一个助手目录不能脱离 shared 单独运行。单独复制或发布 Client 时，必须一起提供其指定版本的 `clients/shared`，并保持两者在同一父目录下；建议固定仓库提交以得到完整匹配的源码。版本约束是当前维护约定，不是自动下载或依赖解析功能。后续需要分别发版时，可以把 shared 打包成固定版本运行库，再拆分 Client 仓库；协议无需因此改变。

Go Hub 本身不需要 Python。启动这里的 Client 需要 Python 3.11 或更新版本，不需要安装第三方 Python 包。当前没有自动安装 Python、后台服务托管或自动 OAuth 续期；这些是安装体验的后续工作，不应影响 Hub 的协议。

## 接入范围

| Client | 原生入口 | 发送模式 | 本实现的实际限制 |
| --- | --- | --- | --- |
| Codex | `codex queue --thread ID --message TEXT` | `queue` | 必须使用支持该命令的本机 Codex CLI，并绑定已有桌面 thread；普通 `codex exec/resume` 独立运行不等同于这个入口 |
| WorkBuddy | 官方 `/openapi/v2/localassistant/message` | `queue` | 目标是官方持续本地助理；API 没有普通 Chat 的 `session_id` 定位参数。需要用户本人正式 OAuth 授权；尚未在本项目持有真实 token 完成接口验收 |
| Grok Routine | 用户提供的官方 Routine webhook | `queue` | URL、认证方式和请求结构来自用户的实际 Routine 配置；示例字段不是对官方请求格式的推测 |

三个 Client 都没有实现已验证的 `steer` 或 `interrupt`。不支持的模式在执行原生操作前拒绝，不会悄悄降级为队列发送。会话是否空闲、资源是否可用、是否允许唤醒，由 Hub 根据已核实的状态决定；Client 不自行伪造 `idle` 或 `busy`。

WorkBuddy 文档来源：[官方 OpenAPI](https://open.workbuddy.cn/docs/openapi) 和 [本地助理功能说明](https://www.codebuddy.cn/docs/workbuddy/From-Beginner-to-Expert-Guide/Function-Description/Assistant)。该 Client 使用 `Authorization: Bearer access_token`，发送 `{"content": "完整通知", "msg_type": "text"}`，需要 `user.localassistant.invokable`。不读取桌面应用的 Cookie、缓存 token 或 IPC ticket，也不把应用内部鉴权当作公开接口授权。`oauth_scopes` 是用户配置的授权范围记录，平台仍负责实际鉴权。

## 配置和启动

先让 Hub 注册该 endpoint，在对应 binding 中设置 `"driver": {"type": "external"}`，由独立 Client 消费。不要同时启动两个 Client 领取同一 endpoint。原生身份参数放在 Client 的私有配置，Hub 的管理密钥不应放进去。

每次连接先通过 `GET /v1/endpoint?endpoint=ID` 读取该 endpoint 的已注册配置，只能读取自己。Client 核对协议 `1`、endpoint ID、启用状态和原生目的地后，才能建立 SSE 和领取消息：

| Client | Hub 的 `endpoint.driver` | Hub 的 `endpoint.session_id` |
| --- | --- | --- |
| Codex | `codex` | 必须与私有配置 `native.thread_id` 完全一致 |
| WorkBuddy | `workbuddy` | 必须为 `localassistant`，表示持续专用本地助理 |
| Grok Routine | `grok-routine` | 必须与私有配置 `native.routine_id` 完全一致；这是用户为该 Routine 设置的标签 |

这里的 `endpoint.driver` 是助手类型，和 binding 的外部传输类型 `driver.type="external"` 不同。协议不兼容、禁用 endpoint、会话或类型不匹配时停止消费，消息正文不能指定或覆盖目的地。会话状态 `unknown` 仍保留为未知，不会被 Client 改写为 idle；是否允许投递由 Hub 决定。每次断线重连都会重新验证绑定。

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

在仓库根目录启动所需的 Client：

```sh
python3 clients/codex/main.py --config /path/to/private/codex/client.json
python3 clients/workbuddy/main.py --config /path/to/private/workbuddy/client.json
python3 clients/grok-routine/main.py --config /path/to/private/grok/client.json
```

Windows 可以使用 `py -3` 替换 `python3`，并提供该电脑上的绝对配置路径。Hub 的实际端口应与私有配置一致；示例端口不是自动发现结果。把个人配置文件限制为本机用户可读，避免提交真实 token、thread ID、聊天内容和成果文件。

WorkBuddy 需要在 `native` 中配置用户通过官方流程明确授予的短期 `access_token` 和 `oauth_scopes`。缺少 token 或 invokable 范围时，Client 在领取消息前返回 `unconfigured`。这里没有应用密钥、refresh token 或自动刷新流程；稳定长期接入仍需完成官方应用授权及续期设计。Token 失效后的已尝试投递保留为不确定，不自动发送第二次。

Grok 的 `native.routine_id` 是用户为该官方 webhook 目标设置的稳定标签，必须与 Hub 的 `endpoint.session_id` 一致。`native.body` 必须包含完整消息。`"{{envelope}}"` 替换为 JSON 对象，`{{envelope_json}}` 替换为完整 JSON 字符串，`{{notification}}` 替换为带报告指引的完整通知。还支持 `{{content}}`、`{{task_id}}`、`{{message_id}}`，但它们不能代替完整消息。替换仅进行一轮，任务内容中的占位符不会被再次解释。对象占位符必须独占整个 JSON 字符串值。

Codex 的执行入口在启动 Client 时解析并保存为绝对路径，投递时不会重新从 PATH 查找。Windows 必须配置实际的 `codex.exe`；Client 明确拒绝 `.cmd` / `.bat` 包装器，以避免消息参数经过 shell 解释。Hub 的 `endpoint.session_id` 与 `native.thread_id` 必须指向同一个已核实原生 thread。

## 消费、回执和结果返回

1. Client 先核对 Hub 的已注册原生绑定，再建立 SSE 订阅，之后读取持久化 inbox，避免连接和领取之间遗漏新任务。
2. Hub 原子地把可领取消息从 `pending` 改为 `dispatching`。Client 只处理属于自己 endpoint 的领取结果。
3. 对原生入口尝试一次。执行前确定配置错误为 `failed`；CLI 已启动但失败/超时、网络尝试后没有确认、非 2xx 响应或重定向都为 `uncertain`。Codex CLI 成功、Grok webhook HTTP 2xx 记录传输已投递，不证明助手接手或任务成功。WorkBuddy 还必须返回业务 JSON：`code` 严格为整数 `0`，`data.message_id` 为合法非空字符串；空响应、非 JSON、业务错误或无法识别的响应均保留 `uncertain`，不重发。
4. Client 发送 `/v1/delivery`。接手、执行和业务完成由原生助手主动调用 `/v1/report` 汇报；没有后台历史消息轮询。
5. 任务成功后，Hub 发出 `kind=result` 返回原入口。通知明确要求展示成果，不能把结果再执行成新任务。

SSE 保活是连接健康机制，不会触发业务查询。连接断开时按 1、2、4 秒逐渐退避，最多 30 秒，再订阅并扫描 inbox。只会取到新的 `pending`；`dispatching` 或 `uncertain` 不自动重新领取。Client 不会因网络重连重发原生消息。

如果原生操作完成后 delivery 回执没有确认，Client 停止并提示核对 Hub。保留已有任务，不认为操作没发生。重新启动 Client 也不会把已有 claim 当成待发送消息；按 Hub 的恢复流程核对真实原生记录和回执后处理。

可以在原生助手安装报告技能，或者使用配置好的 Hub CLI。技能只教原生助手读取完整交接、遵守授权和主动回报；它不代替后台 Client 监听消息，也不会赋予企业微信或其他应用的新权限。需要报告的助手必须能执行本机报告工具；无法执行时应报告接入未完成。

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

Client 使用 Python 标准库的 HTTP、子进程和文件 API；同一源码可运行在 Windows、macOS、Linux。原生助手的 CLI、接口或安装方式仍须在每个平台单独核实。Go 核心可构建为原生程序，Python 只由需要这些 Client 的用户选择安装。

当前 Hub 接口仅在本机 loopback 使用。另一台电脑的助手需要在那台电脑上运行自己的 Client，并采用后续经过授权的跨设备传输；本机 SSE 不自动成为远程消息服务。文件引用带有 `device_id`、`workspace_id` 和工作区内的相对路径，不会自动复制文件，也不能把本机路径直接交给远程助手使用。

## 验证

```sh
python3 -m unittest discover -s clients/tests -v
```

测试使用本地假 HTTP 和模拟子进程，检查订阅/领取顺序、Host/Origin、拒绝重定向、传输不确定状态、一次原生尝试、失败回执后停止、结果通知语义、OAuth 配置边界与模板的一次替换。它不会发送真实助手消息、读取联系人或证明任何原生平台已接入。

真实验收仍需核实空闲唤醒、接手报告、执行报告、需要用户介入、成果核对、返回原入口及不确定投递的恢复。接入 WorkBuddy 时，可用已授权的只读联系人查询完成第一条闭环；发通知给联系人需要具体人类指令和原生应用要求的本人确认。
