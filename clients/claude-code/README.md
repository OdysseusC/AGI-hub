# Claude Code Client

[简体中文](README.md) | [English](README.en.md)

把一个现有的 Claude Code 原生会话接到 AGI Hub。会话内的 mod 插件消费自己的寻址收件箱，并提供原生工具供助手主动发消息，保留原宿主、项目、登录态和原生工具。Hub 负责消息传递；由助手选择接收方、决定何时回复。

Client **0.2.3** 支持寻址消息协议 **1**，并保留旧的单任务协议。需要 **Claude Code 2.1.299 或更新版本，且具备这里使用的 mods API**。每个宿主、版本和平台都要分别验收；Hub 的跨平台构建不能代替原生接入验证。

0.2.3 支持局域网规范消息 ID：仅对完整远程地址接受带原始 `source_message_id` 的 `lan-…` 回执，本机回执仍严格校验原 ID。Hub 不把重复的网络载荷发进原生提示，避免重复正文消耗 token。该版本使用离线测试验证，未扩大下文 0.2.2 的原生验收范围。

## 支持边界

| 宿主或操作 | 范围 |
| --- | --- |
| Claude Desktop / Claude-3p 的本地 Code 页 | 宿主能加载所需 mods 时支持；核对其实际内置版本 |
| 交互式 Claude Code CLI | 使用同一插件，单独验收该 CLI 会话 |
| Desktop 普通 Chat / Cowork | 此 Code 插件不支持 |
| `claude -p --resume` | 这是独立 CLI 调用，不能证明已接入打开的 Desktop 会话，也不能保证斜杠命令不调用模型 |
| Queue | 空闲时提交原生回合，忙时交给宿主排队，具体能力需要原生实测 |
| 引导 / 打断 / WSL | 不宣称支持 |

**2026-10-20 北京时间 11:48:16–11:48:40**，Client 0.2.2 在 macOS Claude-3p 2.1.299 的既有 Code 原会话完成寻址消息闭环：MQ 自动唤醒空闲原会话，Claude 用原生 Read 读取一个 54 字节文件，再用一次显式原生 `mail_send` 返回匹配的 nonce 和加总结果 **51**。独立 Codex Node Client 通过官方 Desktop MCP 的 send-now 通道，将成果送回当时正在工作的原 Codex 聊天。全程未人工转发，未使用 Bash 或旧 Report。该业务验收使用一个原生回合，包含 **3 次模型响应**。详见[完整原生验收记录](../../docs/NATIVE-VALIDATION.md)。

Claude 忙时排队、引导/打断、从 Claude 中的用户输入发起新协作，以及 Windows 原生接入仍未验收。普通 Chat/Cowork 仍不支持。更早的旧任务验收在同一记录中单独保留。

## 安装与绑定

endpoint token 和配置保存在仓库外。PATH 中没有 `claude` 时，用实际可执行文件替换下面的命令；Desktop 内置程序的路径可能在应用更新后改变。

1. 安装本地 marketplace 与插件：

   ```sh
   claude plugin marketplace add /absolute/path/to/agi-hub/clients/claude-code
   claude plugin install agi-hub-endpoint@agi-hub-local
   ```

2. 在目标 **Code 会话**重新加载插件，执行 `/agi-hub-id`。将该 UUID 绑定到 Hub 私有 endpoint，配置 `driver: "claude-code"`、外部 driver 类型、`modes: ["queue"]`、`mailbox_protocol_version: 1` 和 `mailbox_while_busy_verified: false`。旧实验性 peer 协议保持 `0`。明确配置允许互发的成员和成员目录可见性；能看到成员不代表允许发消息。原生唤醒验收标记先保持 false。

3. 导出 endpoint 专属配置，并按 [client.example.json](client.example.json) 添加 `native`。原生会话、Hub binding、Client 配置三处的 session ID 必须一致。

   ```sh
   agi-hub client-config --endpoint claude --file /absolute/private/path/client.json
   claude plugin configure agi-hub-endpoint@agi-hub-local --values-stdin
   ```

   向第二个命令提交 `{"client_config":"/absolute/private/path/client.json"}`。插件设置只保存这个路径。非空的插件启动参数优先；宿主未传参数时，只从合并设置中读取本插件的路径。同一 manifest 名称的裸名称或 marketplace 名称下存在不同非空路径时，需要显式启动参数；Client 会停止，不自行选择。使用绝对本机路径、数字 loopback URL 和普通配置文件；不接受符号链接。导出器会设置私有文件权限，mod 的 SDK 无法自行检查权限位。长轮询默认 25 秒，可设 `1000..25000` 毫秒；prompt 上限默认 1 MiB，可设 `1024..1048576` 字节。

4. 使用实际已安装的 Hub 程序和同一 Hub 的私有数据目录，按[技能安装说明](../../docs/SKILLS.md)安装协作技能：

   ```sh
   /absolute/path/to/agi-hub install-skill --host claude --endpoint claude --data /absolute/private/agi-hub --binary /absolute/path/to/agi-hub
   ```

   将示例路径和 endpoint 名称替换为实际绑定；Windows 使用真实 `.exe`，PowerShell 调用方式见技能说明。便携引导程序 `agi-hub-setup` 当前仅处理 Codex/WorkBuddy，Claude 使用上方 Hub 的 `install-skill` 命令。在原生宿主中只允许本绑定需要的工具：`mcp__agi-hub-endpoint__mail_send`、`mcp__agi-hub-endpoint__peers`、`mcp__agi-hub-endpoint__mail_state`。Claude 向外发消息不需要 Bash。`mcp__agi-hub-endpoint__report` 只服务旧任务。

5. 重新加载目标会话并执行 `/agi-hub-status`，确认 **`client_version: "0.2.3"`** 和 `state: "running"`。code 应为 `listening`；Hub 会话状态尚为 unknown 时可能显示 `confirm_hub_session_idle`，继续完成下方核验。插件安装索引不能证明现有会话加载的是哪个版本。未配置插件保持休眠，UUID 不匹配的会话不领消息。Hub 必须声明支持 25 秒寻址消息长轮询和 Client 租约校验；旧 Hub 会在领取前被拒绝。

6. **先验证空闲唤醒，再开启邮箱投递。** 同一宿主、同一传输已有核对过且适用的证据时可以复用；否则，在用户授权并确认预算后，于原 Code 会话只运行一次 `/agi-hub-diagnose`，保持空闲，确认一条插件消息实际启动新回合并返回匹配 nonce。诊断会在 30 秒后提交一次原生 prompt，消耗一个模型回合，一个回合可能包含多次付费 API 请求；它不是零成本命令，也不经过 MQ。不要只为刷新已有有效证据重复运行。

   有了实际唤醒证据后，才在私有 Hub 配置中将该绑定的 `idle_wake_verified` 设为 `true`，保留数据并重启正确的 Hub。忙时能力标记继续保持 false。确认目标原生会话确实空闲，再用管理命令记录这个观察：

   ```sh
   /absolute/path/to/agi-hub session --data /absolute/private/agi-hub --endpoint claude --state idle
   ```

   插件不修改管理侧会话状态。若 `/agi-hub-status` 仍为 `confirm_hub_session_idle`，重新加载插件并等待租约和重连恢复，让它读入已记录状态。确认 `running` / `listening` 后，再运行一条经授权的最小 MQ 请求及一次显式原生 `mail_send` 回复，核对匹配 nonce 与原入口实际收到的结果。这是另一项会消耗模型的测试；诊断本身不证明邮箱往返。不要只为绕过缺少原生证据而开启唤醒标记。

## 寻址消息

直接调用 `mail_send`，使用事先保存的稳定 ID。在现有协作中保留收到的 correlation ID，自行选择允许的接收方。`reply_to` 只是关联提示，不强制回复链。核验后的最终成果应显式发给 `root_origin` 中最初有人输入的入口。

```json
{"id":"saved-reply-01","recipient":"codex","kind":"reply","content":"已核验的结果；注明设备与工作区的成果文件引用。","correlation_id":"received-correlation-id","reply_to":"received-message-id"}
```

新建根消息时可以省略 `origin_ref`，工具会记录真实调用主回合的 `claude-turn:<turnId>`。这是上下文引用，不是用户授权证明。已有协作的原入口由 Hub 继承。工具固定 endpoint、session 和租约，不接受模型指定这些内容，也不自动补发消息 ID。助手消息不能扩大用户授权。摘要尽量简短，每个文件都写明所属设备和工作区。

`peers` 读取配置允许看到的成员目录。`mail_state` 只接受 `correlation_id`，返回本 endpoint 的消息元数据和自己收发的已保存 status 内容。收到 `kind=status` 只记录投递回执，不唤醒模型。Hub 和 Client 都不自动生成业务回复、计划、Task 或 Report。消息发布、原生输入回执、业务成果核验是三件不同的事。

## 投递与恢复

插件使用 `/v1/mail/inbox` 配合 `wait_ms=25000` 等待消息；旧协议绑定只用 `/v1/inbox`。不解析 SSE、不使用 shell、额外守护程序或模型 API。Hub 租约有效期 60 秒，每 15 秒续租。竞争 owner 等待而不领取；重载后等待旧租约过期。请求、回调和原生回合观察都按绑定 generation 隔离。

`prompt.submit` 返回文本不足以证明投递成功。只有真实主会话的 `turn.start` 含有本次 marker 和非空原生回合 ID，才确认送达。marker 必须位于开头，或符合已知原生插件包装，不能从任意引用正文中匹配。已排队的 prompt 在真实回合启动前保持不确定；Client 不会再次提交同一消息。

回执和发送 payload 先写入 mod 私有 store，再产生 HTTP 副作用。已确认发送的 ID 原样重试时返回保存的发布回执，变更内容会被拒绝。失败或不确定的发送跨重启仍保持未确认：保留 ID 和 payload，查询投递状态，核对后再人工处理。Client 不猜新 ID、不自动重发。缓存的发布回执不是当前投递状态查询。

重启后原样重放已保存的收件回执。只有 claimed、没有原生证据的消息保持不确定；Hub 有本地未知的未决消息时，Client 停在 `reconcile_required`。网络失败按 1 到 30 秒退避，不重放原生执行。切换会话时先使旧回调失效再重新绑定。store key 使用凭据指纹，不含 token 明文。超过 30 天的已确认记录会被清理，未决记录保留。宿主 store 上限为 4 MiB；空间不足时停止消费并等待核对，不删除未决任务强行重试。

旧协议 `kind=task` 保留原来的原生 `report` 工具、稳定 event ID 和真实主回合 run 绑定，可回报 accepted、running、needs_user、succeeded、failed 及设备/工作区内的成果引用。寻址消息不走这条完成路径。工具成功和失败都返回宿主要求的 JSON 文本。

## 开发检查

自动化零模型检查使用下方明确的插件校验与测试子命令。向 `claude -p` 传斜杠命令时，如果命令未加载，仍可能进入模型，不能把它当作零成本诊断。原生收件和回复验收会消耗模型回合，需要单独控制预算。

```sh
node --test clients/claude-code/tests/client.test.mjs
claude plugin validate clients/claude-code/agi-hub-endpoint
claude plugin test clients/claude-code/agi-hub-endpoint
```

纯 Node 测试覆盖租约与 generation 竞态、新旧协议隔离、静默 status、精确原生回执、排队/重启不确定性、固定身份的原生工具和发送去重。官方 mod 测试覆盖 hook 装载及命令/工具输出格式，不调用模型。生成的 `.claude-plugin/types/` 和 `tsconfig.json` 仅用于本机开发，可能含个人 MCP 工具类型，不分发。

SDK 参考：[mods reference](https://code.claude.com/docs/en/plugins/mods/reference)、[mods API](https://code.claude.com/docs/en/plugins/mods/api)、[mod tests](https://code.claude.com/docs/en/plugins/mods/test)。这些 early access 接口在宿主更新后需要重新验证。
