# Claude Code Client

把一个指定的 Claude Code 原生会话接到 AGI Hub。Client 是运行在该会话内的 mod 插件，保留宿主会话、登录态、项目与原生工具。它通过本机 Hub 领取完整 Envelope，以插件来源提交交接消息，并提供专用 `report` 工具主动回传执行状态。

当前实现版本 `0.1.3`，最低运行版本为 Claude Code `2.1.299`。仅支持 `queue`：空闲时由宿主启动回合，忙时等待宿主结束当前回合。原生空闲唤醒和忙时排队必须在目标宿主实测后才能标为已验证。

## 支持范围

| 会话 | 范围 |
| --- | --- |
| Claude Desktop / Claude-3p 的本地 Code 页 | 宿主内置 Claude Code 支持并加载 mods 时可接入；按实际版本和安装策略分别验收 |
| 交互式 Claude Code CLI | 同一插件协议；须单独验证该 CLI 会话 |
| Desktop 普通 Chat / Cowork | 不支持，不会因为安装了 Claude Code 插件而接入 |
| `claude -p --resume` 独立会话 | 可用于零模型命令冒烟；不视为接入正在使用的 Desktop 会话 |
| WSL / 组织禁止个人 mod 的宿主 | 不宣称支持；先核对宿主插件策略 |
| `steer` / `interrupt` | 本 Client 不支持 |

Claude-3p 与官方 Claude.app 的内置版本不能互相代替。本机曾验证 Claude-3p `2.1.299` 能加载 mod，而另一个官方 Claude.app 内置版本较旧；其他安装须重新检查。构建包跨平台不代表原生宿主已逐个平台验收。当前实际验证记录以 [NATIVE-VALIDATION.md](../../docs/NATIVE-VALIDATION.md) 为准。

2026-10-16 的 macOS 原 Code 会话已完成一次 N2 空闲唤醒。N3 的自动领取、原生处理、两份成果及回读已发生；首次回报遇到工具输出格式错误，`0.1.3` 修复后只续接一次回报，Hub 核验并自动返回原 Codex 入口。没有再次运行修复后的全新无人值守任务，也未进行 Claude 忙时排队验收；具体过程和测试输出范围见本机记录。

## 结构

```text
clients/claude-code/
  manifest.json
  client.example.json
  .claude-plugin/marketplace.json
  agi-hub-endpoint/
    .claude-plugin/plugin.json
    hooks/hooks.json
    hooks/register.js
    hooks/lib/config.js
    hooks/lib/protocol.js
    hooks/lib/client.js
    tests/commands.test.ts
  tests/client.test.mjs
```

插件本体是零依赖 JavaScript，不使用 Node 内置模块、Python、额外后台进程、未公开 socket 或模型 API。`register.js` 将公开 mods API 包装为端口；纯函数和状态机不依赖 Claude。官方宿主生成的 `.claude-plugin/types/` 和 `tsconfig.json` 仅用于本机开发，不提交、不分发；其中可能包含本人的 MCP 工具类型。

## 安装与绑定

先准备支持 mods 的 Claude Code 可执行文件。PATH 中没有 `claude` 时，可以用 Desktop 自带的程序，但更新后其绝对路径可能变化。以下示例中的 `claude` 应替换为实际可执行文件路径。

1. 安装本地 marketplace 和插件：

   ```sh
   claude plugin marketplace add /absolute/path/to/agi-hub/clients/claude-code
   claude plugin install agi-hub-endpoint@agi-hub-local
   ```

2. 在目标 **Code 会话**重新加载插件，运行 `/agi-hub-id`，记录原生会话 UUID。未配置的插件只显示 `dormant (unconfigured)`；其他未绑定会话不会领消息。

3. 在 Hub 的私有配置启用 `claude` endpoint，填写该 UUID，`driver` 为 `claude-code`，`driver.type` 为 `external`，`modes` 只保留 `["queue"]`。初始 `idle_wake_verified` 和 `result_queue_while_busy_verified` 均为 `false`。修改 Hub 配置后重新加载 Hub；不要让第二个 Client 同时消费此 endpoint。

4. 导出 endpoint 专属配置到仓库外，并按 [client.example.json](client.example.json) 增加 `native`：

   ```sh
   agi-hub client-config --endpoint claude --file /absolute/private/path/client.json
   ```

   `native.session_id` 必须与目标会话和 Hub binding 一致。`long_poll_ms` 默认 `25000`，允许 `1000..25000`；`max_prompt_bytes` 默认 `1048576`，允许 `1024..1048576`。`base_url` 仅接受数字 loopback，包括 `127.x.x.x` 和 `[::1]`。禁止相对路径、网络路径及配置符号链接。私有目录建议 `0700`、配置文件 `0600`；Hub 导出器会设置文件权限。mods 的 `fs.stat` 没有权限位，插件不会声称重新验证了 `0600`。

5. 配置插件选项。`client_config` 只保存配置文件的绝对路径，token 不进入插件 settings。可使用官方配置命令：

   ```sh
   claude plugin configure agi-hub-endpoint@agi-hub-local --values-stdin
   ```

   在标准输入提交 `{"client_config":"/absolute/private/path/client.json"}` 后结束输入。也可使用宿主的插件配置界面。开发时 `--plugin-dir` 的插件选项键为 `agi-hub-endpoint@inline`；marketplace 安装使用 `agi-hub-endpoint@agi-hub-local`。

6. 安装 Hub 协作技能，并在 Claude 的工具权限中只允许 `mcp__agi-hub-endpoint__report`。工具会固定 endpoint 和任务 run_id，不需要允许 Bash：

   ```sh
   agi-hub install-skill --host claude --endpoint claude --data /absolute/private/agi-hub --binary /absolute/path/to/agi-hub
   ```

7. 重新加载目标会话的插件，检查 `/agi-hub-status`。应为 `running`，`code` 为 `listening`。Hub 状态仍为 `unknown` 时，先人工确认原会话空闲，再使用 Hub 的 `session --endpoint claude --state idle`；插件不调用管理接口修改会话状态。

8. 执行 `/agi-hub-diagnose`，让会话保持空闲。30 秒后只提交一次带 nonce 的诊断，核对原会话自动启动的真实回合和回复。该诊断会运行一次模型；它不经过 MQ，也不能代替完整业务闭环验收。通过后才将 `idle_wake_verified` 设置为 `true`。

Claude 作为入口向 WorkBuddy/Grok 发任务，还需由本人另外配置允许的 `agi-hub submit` 操作，并设置 leader 或显式 origin。这不会自动授予其他助手任何新权限。现有 Codex Client 只消费 `kind=result`，不把 Claude → Codex 普通执行任务列为支持范围。

## 领取、互斥与完成

Client 通过 `POST /v1/inbox?endpoint=claude&wait_ms=25000` 等待事件唤醒。这个请求最多挂起 25 秒，返回 `null` 后再等待；不读取聊天历史、不流式解析 SSE、不启动历史轮询。

互斥由 Hub 的原子 `client-lease` 实现，不用文件“写后回读”假锁。租约 60 秒，每 15 秒续租；领取响应到达后，在真正提交 prompt 前再次确认绑定和续租。所有领取、投递回执、执行回报均携带当前租约。竞争者拿不到租约时进入 `locked`，只退避检查绑定并重新申请租约，不领取消息；Client 不抢占另一个仍有效的 owner。插件热重载没有释放旧租约时，新 Client 等待它自然过期后恢复；另一进程持续续租时则继续等待。重绑会生成独立 owner，迟到请求不能释放或污染新绑定。

`prompt.submit` 正常返回并不直接算已投递。Client 给消息加一个随机 marker，只有绑定会话的主线程 `turn.start` 看到同一个 marker，并带真实非空 `turnId`，才记录 `delivered`。识别格式为 marker 位于开头，或 2.1.299 已实测的完整原生插件来源包装；不在任意正文中搜索 marker。首次原生诊断发现宿主会加此包装，`0.1.1` 据此修正匹配，保留插件来源和原生授权边界。其他插件返回一段文本却没有放行原生输入时，记录 `uncertain`。忙时已排队但尚未开始的 prompt 同样先保留 `uncertain`，等待对应的真实回合启动后对账为 `delivered`。任何路径都不再次 submit 同一 message。

收到 `kind=task` 后，Claude 主动调用 `mcp__agi-hub-endpoint__report`：

```json
{"event_id":"stable-accepted-event","task_id":"the-received-task-id","state":"accepted","summary":"已接手"}
```

继续发送 `running`、需要本人操作时的 `needs_user`，以及核验成果后的 `succeeded` / 已确认失败后的 `failed`。成果引用保留 `device_id`、`workspace_id`、相对 `path` 和可选 SHA-256，由 Hub 核验文件。HTTP 成功、消息已投递、最终聊天文字和会话空闲都不代表业务完成。

工具只接受本绑定实际收到的 task，不接收 result 或其他会话任务，不接收模型指定的 endpoint/run_id。首次回报固定为 `claude-turn:<真实主线程 turnId>`，随后保持一致；同一个原生 turn 不会首次绑定到两个 task。子 agent 的回合和工具调用不能冒充主会话。相同 `event_id` 只能原样重发。工具的原生返回值使用 JSON 文本，遵循宿主 MCP 工具的输出格式；不会把任意对象直接放入 `result`。真实首次 Report 暴露过“Hub 已保存但宿主拒绝对象输出”的差异，`0.1.3` 已统一修正成功和失败分支，旧 event 仍按原内容幂等。

## 故障与恢复

- 回执先写插件私有 store，再发 Hub；完整 Report 也先保存。store 按 Hub origin、endpoint、session UUID、endpoint token 的 SHA-256 指纹隔离，token 明文不进入 key、状态命令或工具返回。
- 已保存的 `delivered`、`failed`、`uncertain` 回执在重启后原样对账。只有 `claimed` 记录则记 `uncertain`；不重新提交 prompt。
- inbox 响应丢失时，不盲目再次领取。重连后先读取 Hub 的 `outstanding`；发现没有本地记录的 `dispatching/uncertain` 消息时停在 `reconcile_required`，让本人在原会话核对。
- Hub 不可用或安全请求出现瞬时错误时，以 1、2、4…30 秒退避重绑。Hub 重启导致旧租约失效时可重新申请；申请时仍有其他有效 owner 则进入 `locked` 退避等待，不抢占、不消费。断线恢复不重放原生任务。
- `/clear`、`/resume`、`/branch` 先使旧 callback 失效，公开 `classic.SessionStart` 事件再核对新会话。组织策略可能禁止个人插件监听该事件；这种环境在切换后保持停用，需要重新加载插件并重新验收。
- 过期或不确定租约不会继续提交原生操作。所有 callback 使用 generation 隔离，旧进程状态、旧 HTTP 失败及旧存储操作不能接管新会话。
- store 有宿主的 4 MiB 总上限。当前绑定在启动时清理超过 30 天的已确认终态记录，保留未决项。空间不足会停止接收并保留 Hub 的未决记录，需本人处理，不能通过删除未决项来强行重试。Hub 自身的追踪记录仍保留。

`/agi-hub-status` 仅显示运行状态、固定错误码、最近 message ID、当前原生 turn ID 和诊断 nonce，不显示 token、配置内容、原始网络错误或本人的聊天历史。

## 开发验证

```sh
node --test clients/claude-code/tests/client.test.mjs
claude plugin validate clients/claude-code/agi-hub-endpoint
claude plugin test clients/claude-code/agi-hub-endpoint
```

Node 测试覆盖配置、租约、并发/跨代竞态、旧请求迟到、消息去重、伪 ACK、排队后的真实 turn、投递三种结果、重启对账、稳定 Report、子 agent 隔离、Hub 重启和权限边界。官方 mod 测试验证真实 hook 装载与命令/工具入口，均不调用模型、不访问网络或私人数据。

本机编译依据是宿主实际生成的 `// Written by Claude Code 2.1.299` 类型；其中 `TurnStartInput` 确有 `text`、`turnId`、`agentId`，`ToolSpec` 支持 `isDeferred`。该公共 SDK 类型文件 SHA-256 为 `cccdf705f7b6bd9b5e81a5212650258c014d2455907221ac0c4c2fd0942c9638`；不分发本人宿主生成的工具类型。

官方接口：[mods reference](https://code.claude.com/docs/en/plugins/mods/reference)、[mods API](https://code.claude.com/docs/en/plugins/mods/api)、[mod tests](https://code.claude.com/docs/en/plugins/mods/test)。API 属于 early access，更新宿主后应重新 validate，并分别验收原生空闲唤醒、忙时排队、完整 MQ 返回和故障对账。
