# Grok Routine Client

[简体中文](README.md) | [English](README.en.md)

版本 `0.2.1`，Hub 协议 `1`，实现寻址邮箱协议 `1`，依赖同级 `shared` runtime `0.2.1` 和 Python 3.11+。使用 Python 标准库；单独复制本目录时必须一起复制匹配的 shared，保持相邻目录结构。Client 可以独立更新，但必须实际替换旧程序；只修改 manifest 或 Hub 的协议字段不能升级消费者。本版的邮箱队列登记还需要配套 Hub 的 `native-queue` 命令，旧 Hub 不会被自动绕过。

本 Client 把完整 Hub 消息发送到用户配置的官方 Routine webhook。仅支持 `queue`；没有已验证的 `steer` 或 `interrupt`。Webhook 2xx（包括 202）只确认 HTTP 传输，不生成业务回复或证明原生接手、执行成功。邮箱回复必须由 Grok 在获准的原生工具中显式发送。软件已通过离线协议及本地 Hub 进程的登记、重启回归；具体 Routine 的字段、接收目标和双向闭环仍须本人实测，其他电脑或旧 Task/Report 的验证不能替代本次邮箱验收。

## 配置与运行

先注册 `grok` endpoint，将对应 binding 设置为 `"driver": {"type": "external"}`。导出私有配置：

```sh
agi-hub client-config --data /absolute/path/to/private/hub --endpoint grok --file /absolute/path/to/private/grok/client.json
```

命令只导出 endpoint 凭据和空 `native`，不覆盖已有文件。按 [client.example.json](client.example.json) 设置真实 Routine 的 `native.url`、`native.headers`、`native.body` 和用户定义的 `native.routine_id` 标签。该标签本身不是已验证的原生会话 ID。须确认 webhook 实际指向本人选定的 Routine。

[Grok Bot Routines 官方文档](https://cursor.com/help/grok-bot/routines)的 Webhook 说明允许发送 JSON body，Bot 会同时收到该 body 和 Routine instruction；该页没有规定 `notification` 等固定 JSON 字段名。模板字段由用户与所选 Routine 的指令共同约定，无需等待一个未被文档规定的“官方 notification 字段”。例如：

```json
{"body": {"agi_hub_notification": "{{notification}}"}}
```

`agi_hub_notification` 是本示例的约定，不是平台固定 schema。须让既有 Routine 的指令明确读取此字段内的完整交接，核对本 endpoint 和 Routine 绑定，再按协作技能处理；先用安全输入做原生探针，确认实际接收、上下文读取和自身固定 helper 的 Hub 回信。示例和 HTTP 2xx 都不证明这些步骤已经通过，也不创建新 webhook 或增加原生权限。

仅接收 `{id}`、再从固定目录读取旧 endpoint 的 Task 文件的 Routine，使用的是另一套自定义契约；不能因同一 webhook 或旧流程曾通过，就宣称兼容邮箱 V1 或新 endpoint。适配前备份原指令与配置，保留旧分支和业务行为，再为新 endpoint 明确增加 inline JSON 处理分支并单独验收。不要把邮箱消息误送进旧 Task 文件分支，也不要为了绕过验收修改 Hub 队列资格。

请求模板须包含 `{{notification}}`、`{{envelope_json}}` 或独占 JSON 字符串值的 `{{envelope}}`，确保发送完整交接。也可使用 `{{content}}`、`{{message_id}}`；`{{task_id}}` 只属于旧 Task/Report，邮箱模板包含它会在请求前失败。替换只有一轮，消息正文内的占位符保持原样，不作为配置重新展开。消息不能改变固定的 URL、headers 或 Routine 绑定。默认本机 Hub 为 `http://127.0.0.1:7331`，以导出配置为准。

先完成 [安装文档中的 Grok 工作区配置](../../INSTALL.md#grok-工作区安装) 和 [协作技能说明](../../docs/SKILLS.md)。目标 Routine 必须能够读取已安装的协作技能、在本人授权的本机工作区保存消息文件并调用固定身份的 producer helper；安装技能本身不提供这些原生能力。仅有 webhook 或云端 Routine，不能证明它能访问本机 helper、文件或 loopback Hub。缺少该能力时保持待配置，不把凭据放进提示词或开放管理员接口来补齐。

Grok 工作区安装入口为 `agi-hub-setup --host grok`，须明确提供该 endpoint 的 `--client-config`、本人选择的 `--workspace` 及其中的 `--skills-dir`，同时指定现有 Hub 的 `--data`；完整命令见安装文档。安装器提供固定 scoped helper 和工作区内的 `agi-hub-client/SKILL.md`，不猜测 Grok 的原生技能目录，也不替本人启用会话。将实际工作区技能路径交给目标 Routine 读取；不能仅凭安装输出声称云端已加载或原生工具可用。

保存并启用绑定后再启动 Client：`endpoint.driver` 为 `grok-routine`，binding 的 `driver.type` 为 `external`，`endpoint.session_id` 与 `native.routine_id` 完全一致，`modes` 为 `["queue"]`。每次连接及真实 Hub change 事件都会复核这些字段及启用状态。邮箱明确设置 `mailbox_protocol_version:1`，为发送者配置获准的 `allowed_peers`；`visible_peers` 只控制目录可见性。

## 登记独立原生队列

Routine 每次 webhook 触发独立的云端原生运行；固定 `routine_id` 标签不是一个可据此推断空闲的长期聊天。邮箱 V1 要求本机 endpoint 明确登记 `mailbox_native_queue_verified`，默认关闭。Client 不会因进程已启动、订阅成功或 HTTP 2xx 自动授予该标记，也不写 idle、`idle_wake_verified` 或忙时验证标记。

首次登记前，保留两项真实且可核对的原生证据：所选 Routine 收到本次探针的完整上下文并读取协作技能；它用自己的固定身份 helper，将本次 nonce 或核对结果显式经 Hub 回信。使用用户授权的安全安装探针，不能手工转发未消费业务来冒充已通过正常邮箱。仅有模板测试或 2xx 不满足这些要求。

本机 Hub 管理者核对证据后，用配套 Hub 的正式入口登记；替换下列占位值，路径分别加引号，Windows 使用相应 `.exe`：

```text
agi-hub native-queue --data <private-hub-directory> --endpoint <chosen-endpoint> --native-session <same-native.routine_id> --verified=true --evidence-ref <saved-native-probe-reference>
```

登记只接受本机 Hub 管理员身份，并精确匹配当前 native session；endpoint 凭据不能自行登记。私有配置持久记录当前 device、driver、native session 和简短 `evidence_ref`，后者只是证据引用，不读取路径或复制聊天。不要只手改布尔值。换绑使验证失效；同一绑定的开关或 Hub 重启保留登记。更换实际 Routine 或请求契约时也须重新核验。

未登记时，Client 输出 `needs_native_queue_verification` 和登记步骤，保持只读订阅，不报 ready、不领取邮件。登记产生的真实 Hub 事件会让它重新核对，随后自动消费仍有效且尚未领取的原 pending 消息；不要求重新发送或假造 idle。登记后的独立原生队列可在 `session=unknown` 或 idle 时按本地 MQ 规则领取，unknown 不会因此改成 idle。已知 busy 仍需另行核验原有忙时能力，needs_user 仍阻挡普通消息。该标记不证明原生串行、并发控制或批准权限。已过期、`dispatching/uncertain` 的记录不会被重置、换 ID 或重放。

撤销使用同一命令和当前绑定，显式设置 `--verified=false`，并给出撤销记录的 `--evidence-ref`。撤销事件使 Client 回到等待验证；禁用、ACL、租约、改绑校验及不确定投递保护继续生效。旧 Task/Report 路径不因此获得新队列资格。

在仓库根目录运行：

```sh
python3 clients/grok-routine/main.py --config /absolute/path/to/private/grok/client.json
```

Windows 使用 `py -3` 和本机绝对路径。个人 webhook 和认证配置放在仓库外；同一 endpoint 只启动一个消费者。完整说明见 [Client 文档](../../docs/CLIENTS.md)。

## 邮箱投递与主动回复

协议由 Hub binding 明确选择：`mailbox_protocol_version:1` 只消费 `/v1/mail/inbox` 并写 `/v1/mail/delivery`；`0` 保留旧 Task/Report inbox，不混用或自动降级。V1 的 `request/reply/result` 携带完整 Envelope，`status` 由 shared runtime 保存投递回执，不发 webhook、不唤醒模型；adapter 也拒绝直接投递 status。

需要回复时，Grok 根据当前上下文选择获准收件人，在固定 helper 的工作区保存新消息 JSON，再调用安装说明中的固定 helper 执行 `mail-send --file <已保存的消息文件>`。例如：

```json
{
  "id": "saved-new-reply-id",
  "recipient": "chosen-authorized-endpoint",
  "kind": "reply",
  "content": "brief verified result",
  "correlation_id": "received-correlation-id",
  "reply_to": "received-message-id"
}
```

新回复使用新稳定 ID，并继承收到的关联；`reply_to` 只建立引用，不强制回给上一发送者。最终成果按协作技能核对 `root_origin`。固定 helper 仅使用该 endpoint 的 scoped 凭据和授权工作区，不提供管理员、会话控制或原生执行能力。邮箱不用旧 Task Report，也不会在 webhook 返回或模型回合结束时自动回信。

每次领取只发起一次 webhook 请求。超时、非 2xx 或投递回执不确定时保留原消息及 ID，通过 Hub 状态与原生证据核对；不换 ID、盲目重发 webhook、重置领取状态或自动生成接手/成功回报。既有 `dispatching/uncertain` 记录不会由 Client 自动重新消费。

正常助手协作使用 AGI Hub 邮箱。CUA/GUI 仅用于最初恢复、安装引导和真实 GUI 按钮验收；Hub 恢复后，不用向 Grok 窗口键入消息来代替正常投递或主动回信。

独立维护入口默认关闭，必须由用户明确配置，且所选 Routine 已有单独验证的维护分支。普通邮箱分支不等于维护入口；本说明不宣称任何具体电脑支持该维护通道或完整的 OS 重启后无人值守恢复。

## 维护与测试

Grok Routine 的实现、配置、manifest 和专项测试都放在本目录。单独验证适配器：

```sh
python3 clients/grok-routine/test_adapter.py -v
```

公共协议及跨 Client 兼容测试在 `clients/tests`，公共夹具在 `clients/tests/support.py`。仓库根目录运行 `python3 -m unittest discover -s clients/tests -v` 会汇总所有助手专项测试；只使用本地模拟，不触发真实 webhook。

独立的 Hub 离线维护说明见 [受限恢复入口](RECOVERY.md)；它默认关闭，须先核实原 Routine 的专用维护分支。
