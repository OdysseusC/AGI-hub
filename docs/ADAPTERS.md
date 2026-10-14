# 适配器与支持范围

本页区分代码已实现的投递方式、官方候选能力，以及当前设备仍需完成的原生验证。用相同模型的 API 或另起 CLI/SDK 会话，不能算作接入用户正在使用的桌面助手。

## 当前驱动

| 驱动 | 输入 | 支持模式 | 当前验证 |
| --- | --- | --- | --- |
| `codex-queue` | 指定线程与完整 Envelope 通知文本 | `queue` | 本地模拟参数与进程行为；真实原生会话闭环待验收 |
| `webhook` | 用户配置的 URL、headers 和真实平台 JSON 模板 | `queue` | 本地模拟 HTTP、模板、断线及重定向；实际 Routine 待配置验证 |
| `command` | 指定可执行程序与参数，stdin 完整 Envelope JSON | endpoint 明确声明且预先验证的模式 | 本地模拟进程接口；实际原生程序由用户验证 |

这些驱动不读取其他助手的登录凭据或历史。Headers 只从用户的私有配置获得；程序输出和 HTTP 响应正文不作为公开投递诊断展示。

## Codex 队列

驱动使用独立 argv 调用，不经过 shell：

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
    "modes": ["queue"],
    "driver": "codex-queue"
  },
  "driver": {
    "type": "codex-queue",
    "timeout_seconds": 30
  }
}
```

`executable` 未指定时使用 PATH 中的 `codex`，也可设为本机真实可执行文件路径。通知包含完整信封、已有授权边界和回报方法；结果通知只用于展示结果，不能对原任务重新启动工作或递归回报。

官方 `codex queue` 支持已有会话的 UUID 或精确名称；名称重名时应使用 UUID，不支持图片附件。当前驱动只实现本机默认连接，不暴露 `--remote` 参数。[官方 queue 文档](https://learn.chatgpt.com/docs/cli/reference)

官方 App Server 有 `thread/resume`、`turn/start`、`turn/steer`、`turn/interrupt` 与执行事件，且 steer 需匹配活跃 turn。此项目尚未实现 App Server driver；独立启动 App Server 并不证明复用了桌面既有会话或其原生工具。[官方 App Server](https://learn.chatgpt.com/docs/app-server)

## Webhook / Grok Routine

Webhook URL 和请求体必须来自用户已核实的实际平台配置，边车不会猜官方路由、payload 或鉴权。云端 Routine 还需要一个真实、授权且能够回到本机的报告通道；请求成功不能证明闭环已经完成。当前没有跨设备 relay 或公网入口部署。

`body` 必须是有效 JSON，下面是模板替换规则，不能将语法演示误用成真实 Routine 请求体：

| JSON 字符串值中的占位符 | 替换结果 |
| --- | --- |
| `{{content}}` | Envelope 的 content，原样作为文本 |
| `{{envelope_json}}` | 完整 Envelope 的 JSON 文本，作为字符串转义 |
| `{{task_id}}` | 关联任务 ID |
| `{{message_id}}` | 消息 ID |
| 整个字符串值恰为 `{{envelope}}` | 替换为完整 JSON 对象 |

替换在结构化 JSON 值上进行，最后序列化；不会直接拼接未经转义的内容，也不会再次解释任务文本中的占位符。`{{envelope}}` 不能嵌入带前后缀的字符串。

只允许 HTTPS，或指向 localhost/loopback 的 HTTP。不跟随重定向，防止把用户配置的 headers 带到其他地址。2xx 表示 `delivered`；请求发起后的非 2xx、断线或超时保守记为 `uncertain`。配置明显错误记为 `failed`。

用户此前在另一台 Windows 电脑验证过 Grok Routine 与 Codex 的事件交接。那是特定环境的经验；本项目不依赖其源工程，也不假设当前电脑存在相同配置。

## 自定义原生命令

`command` 使用用户配置的 `executable` 与 `args`，以 stdin 传完整 Envelope JSON。它是给已验证原生接口的扩展口，不自带助手、模型或新会话运行器。

要声明 `steer` 或 `interrupt`，必须先确认实际程序定位的是绑定会话和目标运行，支持该语义，并能回传对应 `run_id` 与实际执行回执。内核要求 `target_run_id` 匹配活跃的边车管理运行。把控制文本当普通 queue 消息发送，不构成这些能力。

进程未成功启动为 `failed`；成功启动后非零退出、超时或取消为 `uncertain`；退出成功仅表示传输完成。原生任务完成仍要 Report 与成果验证。

## Claude 候选能力

Claude Code 官方跨会话消息可触达本机运行中的其他 Code 会话，使用 macOS/Linux Unix socket 或 Windows named pipe；忙时在工具调用间读取，空闲时开始新回合。macOS/Linux/WSL 2 至少需要 `2.1.224`，原生 Windows 至少需要 `2.1.234`。消息仅为文本，仍受接收方 inbound controls 与自己的权限约束。[跨会话消息](https://code.claude.com/docs/en/cross-session-messaging)

Desktop Code 另有自己的会话管理面，只看到 Desktop 运行的本地、SSH、WSL Code 会话，忙时等当前工作完成后读取。不能把这套面与普通 Chat、Cowork、terminal CLI 或 cloud 会话混为一谈。[Desktop Code](https://code.claude.com/docs/en/desktop#work-across-sessions)

Code 会话向 hooks/Bash 导出每会话收件地址和 token，可作为注册候选；Windows 连接要求认证。当前还需验证完整投递载荷、Desktop 实际收件地址、空闲接手及结果回报。[收件环境变量](https://code.claude.com/docs/en/env-vars)

插件支持不等于外部唤醒：普通 Chat 不运行 hooks、subagents 或本地 MCP；Cowork 与 Code 另有运行条件。Channels 是明确的外部事件机制，但处于研究预览、需要会话打开和按会话启用，还受插件白名单约束。只安装 MCP 不会获得推送能力。本项目尚未实现 Claude driver。[插件范围](https://support.claude.com/en/articles/13837440-use-plugins-in-claude)、[Channels](https://code.claude.com/docs/en/channels)

## WorkBuddy 候选能力

官方 Open API 提供本地助理状态、消息投递与历史查询：

| 能力 | 官方接口 | Scope |
| --- | --- | --- |
| 只读在线状态 | `GET /openapi/v2/localassistant` | `user.localassistant.readable` |
| 触发本地助理 | `POST /openapi/v2/localassistant/message` | `user.localassistant.invokable` |
| 读取回复 | `GET /openapi/v2/localassistant/message` | `user.localassistant.readable` |

域名为 `https://www.workbuddy.cn`。投递返回 `message_id`，文档未提供普通桌面任务的 `session_id`、设备选择参数或本地助理完成事件；历史增量接口用于轮询回复。云端 `/tasks` 与 ACP 的 SSE 属于云端会话，不能当作本地助理能力。本项目尚未实现此 driver，也不实现云端轮询替代本地接入。[官方 Open API](https://open.workbuddy.cn/docs/openapi)

官方助理使用固定专属目录、一个持续会话，区别于普通任务的多会话与可选工作目录。第三方 API 需要应用审核和用户 OAuth 授权，纯本地开源安装的授权部署方式仍需确认。[助理指南](https://www.workbuddy.cn/docs/workbuddy/From-Beginner-to-Expert-Guide/Function-Description/Assistant)、[第三方接入](https://open.workbuddy.cn/docs/third-party-app)

## 原生接入验收

每个适配器分别验证：绑定的真实会话、空闲接手、忙时行为、权限提示、关联回执、成果文件、返回原入口以及失败核对。需明确列出验证的 OS、宿主版本和会话类型。遇到只能新起独立 CLI/SDK 会话的入口，要标明该差别。

返回 [README](../README.md) · 查看 [架构](ARCHITECTURE.md) · [安装指南](INSTALL-FOR-ASSISTANT.md)
