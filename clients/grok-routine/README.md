# Grok Routine Client

版本 `0.2.0`，Hub 协议 `1`，依赖同级 `shared` runtime `0.1.0` 和 Python 3.11+。使用 Python 标准库；单独复制本目录时必须一起复制匹配的 shared，保持相邻目录结构。

本 Client 从 Hub 的 SSE 事件领取消息，通知用户配置的官方 Grok Routine。Grok 在原生会话中使用已有工具执行，再主动报告接手、进度和成果。Client 不启动另一个 AI 执行器，不读取聊天历史收集结果。两种传输都只支持 `queue`，没有普通任务的 `steer` 或 `interrupt`。

| `native.transport` | 完整消息的位置 | Webhook 请求 |
| --- | --- | --- |
| `file-webhook` | 用户指定的本机共享 inbox 中的完整 JSON 文件 | 固定仅 `{"id":"hub-…"}` |
| 省略 | 按 `native.body` 模板放入请求体，兼容已有配置 | 用户已核实的官方 Routine 请求格式 |

本机文件模式要求该 Routine 的实际 Grok 会话已有本机文件和报告命令能力；云端 webhook 本身不会获得这些能力。旧 Codex↔Grok 文件桥的实际验证不能直接算作本 Client 的 Hub 闭环验收。[验证范围](../../docs/NATIVE-VALIDATION.md)

## 配置本机文件模式

1. 在仓库外选定并创建独立的 inbox、接收记录目录、任务工作区和私有配置目录。inbox 必须是已经存在的本机绝对目录，不能是符号链接或重解析路径。每一组 Hub、endpoint 与 Routine 使用自己的目录；不要借用旧文件桥的收件箱、消息格式或回执目录。
2. 在用户选定的 Grok Bot 中配置一个用于 Hub 的 Routine，仅保留 webhook 触发并保持启用。固定目标会话，核实该会话能访问本机目录和报告程序。已有旧桥 Routine 保持原样；不要把新包交给旧桥处理器。
3. 为 Hub 注册 `endpoint.driver="grok-routine"`、`modes=["queue"]` 和 `binding.driver={"type":"external"}`。`endpoint.session_id` 与 Client 的 `native.routine_id` 使用同一个稳定标签；标签用于核对配置，不会自动创建或定位平台会话。
4. 导出该 endpoint 的私有配置，然后填写原生参数：

```sh
agi-hub client-config --endpoint grok --file /absolute/path/to/private/grok/client.json
```

命令只导出 endpoint 凭据和空 `native`，不覆盖已有文件。下面是 `native` 的结构；路径、标签和 URL 都须替换为当前安装的实际值。Webhook URL 与 headers 只保存在私有配置，不能写入任务文件、Routine 提示或公开日志。

```json
{
  "transport": "file-webhook",
  "routine_id": "hub-grok-routine",
  "inbox_dir": "/absolute/private/grok/inbox",
  "url": "<official Routine webhook URL from the user's setup>",
  "headers": {},
  "timeout_seconds": 30
}
```

此模式不配置 `body`。客户端会先发布完整文件，再发送固定的 id 通知；不会把任务正文或命令加进 webhook。Windows 路径可用 `C:/Users/you/private/agi-hub/grok/inbox`，其中 `you` 和目录均为示例。

5. 按 [GROK_INSTRUCTIONS.md](GROK_INSTRUCTIONS.md) 填好固定接收提示、无凭据的工作区映射和 endpoint 专属报告 launcher。这个文件是配置模板，不是已安装的宿主技能；不要假设 Grok 有自动发现的技能目录。
6. 启动 Hub，再启动一个 Grok Client：

```sh
python3 clients/grok-routine/main.py --config /absolute/path/to/private/grok/client.json
```

Windows 使用 `py -3` 或实际 Python 可执行文件绝对路径。每次连接先核对 Hub 的协议、endpoint 和 Routine 标签，随后先订阅 SSE、再领取消息。只运行一个 endpoint 消费者；没有定时收件轮询。

首次授权验收时可启用已配置的 endpoint，保持 `idle_wake_verified=false`。启动后实际确认原 Grok 会话空闲，再执行 `agi-hub session --endpoint grok --state idle`；已核实 idle 的首次投递不要求预先声明唤醒已通过。原生接入能力须分段验证：Routine 实际读文件和回报、Hub 核验成果，以及关联 result 实际回到原 Codex 会话。配置和本地测试不能把空闲唤醒或忙时结果能力变成已验证；每次 Hub 启动后的 `unknown` 会话都要重新核实。[安装与验收](../../docs/INSTALL-FOR-ASSISTANT.md)

## 文件、去重和投递证据

包的 `protocol` 为 `agi-hub-grok-file-v1`，字段为 `id`、`endpoint`、`routine_id`、完整 `envelope` 和带报告说明的 `notification`。文件名为 `<id>.json`；`id` 是 `hub-` 加原始 `envelope.message_id` 的完整 UTF-8 SHA-256 小写十六进制值。Hub 的 `task:…`、`result:…` ID 保留在 Envelope 内，不直接当作文件名。

Client 先建立持久目录 claim，再通过同目录临时文件和重命名发布完整 JSON，最后尝试一次 webhook。目录 claim 兼容 exFAT，不依赖硬链接；接收方忽略临时文件。保留包与投递记录，不覆盖冲突文件、不自动清理 claim。发现已有 claim、已有包或不确定状态时核对保存证据，不重新发送通知。这个去重只限制 Client 投递；Grok 仍须持久保存自己的接手记录和 Report，避免重复执行。

| 文件模式的通知结果 | Hub delivery | 后续 |
| --- | --- | --- |
| HTTP 200 | `delivered` | 仅说明传输接受，等待原生 Report |
| HTTP 401 / 403 | `failed` | 明确拒绝，保留文件和诊断 |
| 其他响应，包括其他 2xx、重定向、429、5xx，或连接/响应不确定 | `uncertain` | 不重试，核对原生消费与回报 |
| 无效 Envelope、包无法编码或不支持的模式 | `failed` | 未开始通知，核对消息或配置 |
| 文件/claim 缺失、冲突，或发布、保存投递记录失败 | `uncertain` | 保留全部证据，先核对再处理 |

不跟随重定向，不自动重投 webhook；Hub 也不会重新领取 `dispatching` 或 `uncertain` 消息。HTTP 200、文件存在和普通聊天答复都不代表业务完成。只有原生会话提交真实 Report、Hub 核验所需成果后，才生成关联 result 返回保存的原入口。

`timeout_seconds` 限制连接后的发送与响应头等待，且不读取响应正文；系统 DNS 查询无法由 Python 标准库可移植地中断，可能延迟方法返回，但耗尽时限后不会再发出 POST。

## 兼容模板模式

旧配置省略 `native.transport`，继续使用 `routine_id`、`url`、`headers`、`body` 和 `timeout_seconds`。请求体字段必须来自用户已核实的 Routine 配置，不把示例字段视为官方接口约定。

模板须包含 `{{notification}}`、`{{envelope_json}}` 或独占 JSON 字符串值的 `{{envelope}}`，以传递完整交接。还支持 `{{content}}`、`{{task_id}}`、`{{message_id}}`；它们不能单独代替完整信封。替换只有一轮，正文中的占位符保持原样。该兼容模式保留原有 HTTP 语义：2xx 仅记录传输 delivered，其他已尝试但未确认的请求为 uncertain。两种模式不自动切换。

## 维护与测试

```sh
python3 clients/grok-routine/test_adapter.py -v
python3 -m unittest discover -s clients/tests -v
python3 tools/smoke_grok_client.py --hub /absolute/path/to/agi-hub
```

最后一条使用本机已构建 Hub、真实文件包与固定 endpoint 报告 launcher，本地模拟 Routine 接收 id 并提交 Report，核对成果 SHA-256、一次关联 result 和 Client 重建后不重发。可加 `--temporary-parent <已有且位于 Git 仓库外的目录>`，把临时数据放在待核实的文件系统；本次 Windows exFAT 的该模拟检查已通过。[记录](../../docs/NATIVE-VALIDATION.md)

测试不向真实 Routine 发消息。实际安装、原生文件权限、主动回报、空闲唤醒及返回原入口仍需分别验收。Client 更新时保留私有配置、包、claim、接收记录与 Report；未决交接先核对，不清空记录绕过不确定状态。[Client 维护](../../docs/CLIENTS.md)
