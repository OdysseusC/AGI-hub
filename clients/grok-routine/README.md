# Grok Routine Client

[简体中文](README.md) | [English](README.en.md)

版本 `0.2.0`，Hub 协议 `1`，实现寻址邮箱协议 `1`，依赖同级 `shared` runtime `0.2.0` 和 Python 3.11+。使用 Python 标准库；单独复制本目录时必须一起复制匹配的 shared，保持相邻目录结构。Client 可以独立更新，但必须实际替换旧 `0.1.0` 程序；只修改 manifest 或 Hub 的协议字段不能升级旧消费者。

本 Client 把完整 Hub 消息发送到用户配置的官方 Routine webhook。仅支持 `queue`；没有已验证的 `steer` 或 `interrupt`。Webhook 2xx（包括 202）只确认 HTTP 传输，不生成业务回复或证明原生接手、执行成功。邮箱回复必须由 Grok 在获准的原生工具中显式发送。当前版本仅完成离线协议测试；真实 Routine 的字段、接收目标和双向闭环仍待本人配置与实测，其他电脑或旧 Task/Report 的验证不能替代本次邮箱验收。

## 配置与运行

先注册 `grok` endpoint，将对应 binding 设置为 `"driver": {"type": "external"}`。导出私有配置：

```sh
agi-hub client-config --data /absolute/path/to/private/hub --endpoint grok --file /absolute/path/to/private/grok/client.json
```

命令只导出 endpoint 凭据和空 `native`，不覆盖已有文件。按 [client.example.json](client.example.json) 设置真实 Routine 的 `native.url`、`native.headers`、`native.body` 和用户定义的 `native.routine_id` 标签。该标签本身不是已验证的原生会话 ID。须确认 webhook 实际指向本人选定的 Routine，按其官方实际结构填写请求字段；示例占位字段不能直接作为已验证格式。

请求模板须包含 `{{notification}}`、`{{envelope_json}}` 或独占 JSON 字符串值的 `{{envelope}}`，确保发送完整交接。也可使用 `{{content}}`、`{{message_id}}`；`{{task_id}}` 只属于旧 Task/Report，邮箱模板包含它会在请求前失败。替换只有一轮，消息正文内的占位符保持原样，不作为配置重新展开。消息不能改变固定的 URL、headers 或 Routine 绑定。默认本机 Hub 为 `http://127.0.0.1:7331`，以导出配置为准。

先完成 [安装文档中的 Grok 工作区配置](../../INSTALL.md#grok-工作区安装) 和 [协作技能说明](../../docs/SKILLS.md)。目标 Routine 必须能够读取已安装的协作技能、在本人授权的本机工作区保存消息文件并调用固定身份的 producer helper；安装技能本身不提供这些原生能力。仅有 webhook 或云端 Routine，不能证明它能访问本机 helper、文件或 loopback Hub。缺少该能力时保持待配置，不把凭据放进提示词或开放管理员接口来补齐。

Grok 工作区安装入口为 `agi-hub-setup --host grok`，须明确提供该 endpoint 的 `--client-config`、本人选择的 `--workspace` 及其中的 `--skills-dir`，同时指定现有 Hub 的 `--data`；完整命令见安装文档。安装器提供固定 scoped helper 和工作区内的 `agi-hub-client/SKILL.md`，不猜测 Grok 的原生技能目录，也不替本人启用会话。将实际工作区技能路径交给目标 Routine 读取；不能仅凭安装输出声称云端已加载或原生工具可用。

保存并启用绑定后再启动 Client：`endpoint.driver` 为 `grok-routine`，binding 的 `driver.type` 为 `external`，`endpoint.session_id` 与 `native.routine_id` 完全一致，`modes` 为 `["queue"]`。每次连接都先核对这些字段及启用状态。邮箱明确设置 `mailbox_protocol_version:1`，为发送者配置获准的 `allowed_peers`；`visible_peers` 只控制目录可见性。Hub 仅向已核实的可接收会话投递：没有原生观察依据时，不因进程启动或 HTTP 2xx 自动设置 idle、`idle_wake_verified` 或忙时验证标记。

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

## 维护与测试

Grok Routine 的实现、配置、manifest 和专项测试都放在本目录。单独验证适配器：

```sh
python3 clients/grok-routine/test_adapter.py -v
```

公共协议及跨 Client 兼容测试在 `clients/tests`，公共夹具在 `clients/tests/support.py`。仓库根目录运行 `python3 -m unittest discover -s clients/tests -v` 会汇总所有助手专项测试；只使用本地模拟，不触发真实 webhook。
