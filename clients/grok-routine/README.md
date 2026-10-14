# Grok Routine Client

版本 `0.1.0`，Hub 协议 `1`，依赖同级 `shared` runtime `0.1.0` 和 Python 3.11+。使用 Python 标准库；单独复制本目录时必须一起复制匹配的 shared，保持相邻目录结构。

本 Client 把完整 Hub 消息发送到用户配置的官方 Routine webhook。仅支持 `queue`；没有已验证的 `steer` 或 `interrupt`。Webhook 2xx 只确认传输，Grok Bot 须主动回报接手和成果。用户在另一台 Windows 电脑上的既有验证，不等同于本 Client 在当前环境完成验证；本实现的原生闭环仍待实测。

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
