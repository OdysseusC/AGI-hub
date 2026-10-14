# Codex Client

版本 `0.1.0`，Hub 协议 `1`，依赖同级 `shared` runtime `0.1.0` 和 Python 3.11+。使用 Python 标准库；单独复制本目录时必须一起复制匹配的 shared，保持 `clients/codex` 与 `clients/shared` 的相邻结构。

本 Client 向已配置的 Codex 桌面 thread 调用 `codex queue --thread ID --message TEXT`。仅支持 `queue`；不使用 `exec/resume` 建立替代会话，也没有已验证的 `steer` 或 `interrupt`。CLI 成功只确认传输，接手和业务结果由原生助手主动发送回执。当前环境的空闲唤醒和完整回传验收尚未完成；manifest 不能替代实测。

## 配置与运行

先在 Hub 中注册 `codex` endpoint，并将对应 binding 设置为 `"driver": {"type": "external"}`。导出私有配置：

```sh
agi-hub client-config --endpoint codex --file /absolute/path/to/private/codex/client.json
```

该命令只导出 endpoint 凭据，`native` 为空，不覆盖已有文件。按 [client.example.json](client.example.json) 填写 `native.thread_id`、`native.executable` 和可选超时。Hub 的 `endpoint.driver` 必须为 `codex`，`endpoint.session_id` 必须与 `native.thread_id` 一致；Client 每次连接先验证这一绑定。必须在本机先确认 CLI 支持 queue 命令，并指定实际已有 thread ID。默认本机 Hub 为 `http://127.0.0.1:7331`，以导出的真实配置为准。

执行入口启动时解析为绝对路径，投递不重新查找 PATH。Windows 应填写实际 `codex.exe` 路径；`.cmd` / `.bat` 包装器会被拒绝，消息参数不会通过 shell 解释。

在仓库根目录运行：

```sh
python3 clients/codex/main.py --config /absolute/path/to/private/codex/client.json
```

Windows 使用 `py -3` 和本机绝对路径。不要为同一 endpoint 同时启动两个消费者。配置含个人会话及凭据，应放在仓库外。更新、事件消费和不确定投递的处理见 [Client 文档](../../docs/CLIENTS.md)。

## 维护与测试

Codex 的实现、配置、manifest 和专项测试都放在本目录。单独验证适配器：

```sh
python3 clients/codex/test_adapter.py -v
```

公共协议及跨 Client 兼容测试在 `clients/tests`，公共夹具在 `clients/tests/support.py`。仓库根目录运行 `python3 -m unittest discover -s clients/tests -v` 会汇总所有助手专项测试；只使用本地模拟，不触发真实助手。
