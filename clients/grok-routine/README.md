# Grok Routine Client

[简体中文](README.md) | [English](README.en.md)

版本 `0.2.0`，Hub 协议 `1`，依赖同级 `shared` runtime `0.2.0` 和 Python 3.11+。使用 Python 标准库；单独复制本目录时必须一起复制匹配的 shared，保持相邻目录结构。当前不支持寻址消息协议，不能配置 `mailbox_protocol_version=1`。

本 Client 通过完整正文 Routine 模板或显式本机 file-webhook 处理旧协议 queue 任务；steer 和 interrupt 未验收。Webhook 接受只确认传输，Grok 须主动回报执行与成果。固定 Windows Codex/Grok 安装于 2026-10-16 独立通过旧协议验收，见后文；其他安装与新邮箱不在该范围内。

## 配置与运行

先注册 `grok` endpoint，将对应 binding 设置为 `"driver": {"type": "external"}`。导出私有配置：

```sh
agi-hub client-config --endpoint grok --file /absolute/path/to/private/grok/client.json
```

命令只导出 endpoint 凭据和空 `native`，不覆盖已有文件。按 [client.example.json](client.example.json) 设置真实 Routine 的 `native.url`、`native.headers`、`native.body` 和用户定义的 `native.routine_id` 标签。Hub 的 `endpoint.driver` 必须为 `grok-routine`，`endpoint.session_id` 必须与该标签一致；每次连接先验证这一绑定。示例中的请求字段只是占位，必须按该 Routine 的官方实际结构填写，不能直接当作已验证格式。

请求模板须包含 `{{notification}}`、`{{envelope_json}}` 或独占 JSON 字符串值的 `{{envelope}}`，确保发送完整交接。替换只有一轮，消息正文中的占位符保持原样。默认本机 Hub 为 `http://127.0.0.1:7331`，以导出配置为准。

在仓库根目录运行：

```sh
python3 clients/grok-routine/main.py --config /absolute/path/to/private/grok/client.json
```

Windows 使用 `py -3` 和本机绝对路径。个人 webhook 和认证配置放在仓库外；同一 endpoint 只启动一个消费者。完整说明见 [Client 文档](../../docs/CLIENTS.md)。

## 维护与测试

Grok Routine 的实现、配置、manifest 和专项测试都放在本目录。单独验证适配器：

```sh
python3 clients/grok-routine/test_adapter.py -v
```

公共协议及跨 Client 兼容测试在 `clients/tests`，公共夹具在 `clients/tests/support.py`。仓库根目录运行 `python3 -m unittest discover -s clients/tests -v` 会汇总所有助手专项测试；只使用本地模拟，不触发真实 webhook。

## 旧协议本机 file-webhook 传输

Client `0.2.0` 显式设置 `native.transport="file-webhook"` 保留 Windows 本机接入；省略 transport 则保持完整正文模板模式。两种模式均不支持新邮箱，`mailbox_protocol_version` 保持 `0`，不转换旧桥消息或回执。

在仓库外为这一 Hub、endpoint、Routine 创建独立、已存在的绝对 inbox、接收状态目录及成果工作区；inbox 不得经过符号链接或重解析路径。固定原 Bot 和启用的 webhook-only Routine，实核本机文件、已有工具及报告命令能力。云端 webhook 不会自行获得本机访问能力。绑定 driver grok-routine、external、queue，session_id 与稳定 routine_id 标签一致；标签本身不能定位平台会话。

私有 native 使用 transport、routine_id、inbox_dir、实际 url、headers、timeout_seconds，不配置 body。真实 URL、认证和 token 只存私有配置。Windows 使用实际 Python 可执行文件。匹配 shared `0.2.0`、endpoint 固定报告 launcher 和已填好的 [Routine 接收模板](GROK_INSTRUCTIONS.md) 一起安装；模板不等于已自动加载的技能。收件目录、工作区映射、报告程序和配置由安装固定，消息不能替换。

Client 先发布完整 `agi-hub-grok-file-v1` 包，再且仅发送 `{"id":"hub-…"}`。包包含 id、endpoint、routine_id、原完整 envelope、notification；文件名为 id.json，id 是 hub- 加 message_id 的完整 UTF-8 SHA-256 小写十六进制。原 task/result ID 和关联字段保持在 Envelope 内。持久目录 claim、同目录临时文件与重命名兼容 exFAT；接收端忽略临时文件，另存 processing 和稳定 Report，防止重复执行业务。

文件模式仅 HTTP 200 为 delivered，401/403 为 failed，其余响应、重定向、超时或不确定传输为 uncertain。无效 Envelope/编码/模式在通知前为 failed；文件或 claim 缺失冲突、发布或回执保存失败为 uncertain。不跟随重定向、不读取响应正文、不自动重发；保留文件、claim、回执和 Report 先对账。超时限制连接后的发送及响应头等待，标准库 DNS 不能跨平台中断，但时限耗尽后不再发 POST。模板模式保持原有 2xx 传输确认语义，不自动切换。

新安装先保留唤醒能力 false；用户授权的独立探针须核实发送前空闲、固定会话自动唤醒和真实文件消费。通过后仅设置对应唤醒标记；重启并重新核实真实状态后才能登记 idle。另用真实 Hub 任务核对 accepted/running/终态、成果 SHA-256、关联 result 与原入口实际消费。HTTP 200、旧桥成功或本地模拟均不能替代。当前 Windows 固定安装已于 2026-10-16 独立通过双向旧协议验收，其他安装及忙时能力另行核实。[分段证据](../../docs/NATIVE-VALIDATION.md#windows-grok-roundtrip-20261016)

构建 Hub 后运行 `python3 tools/smoke_grok_client.py --hub /absolute/path/to/agi-hub`，Windows 使用实际 exe；可加 `--temporary-parent <已存在且位于仓库外的目录>` 验证 exFAT。检查使用真实本地 Hub、文件 Client、固定报告 launcher 和模拟 Routine，核对完整发布后 ID-only 通知、实读文件、Report、SHA-256、一次关联结果及重建 Adapter 不重发；不调用真实 Grok 或改变能力标记。
