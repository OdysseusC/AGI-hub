# Codex Client

版本 `0.1.0`，Hub 协议 `1`，依赖同级 `shared` runtime `0.1.0` 和 Python 3.11+。使用 Python 标准库；单独复制本目录时必须一起复制匹配的 shared，保持 `clients/codex` 与 `clients/shared` 的相邻结构。

本 Client 提供两种明确选择的传输。默认 `native.transport="queue"` 保留 `codex queue --thread ID --message TEXT`；CLI 成功只确认传输，是否仍是目标桌面会话并保留桌面工具，需要在具体宿主中验证。Client 不使用独立 `exec/resume` 来替代桌面会话。

`native.transport="app-server"` 是结果专用入口：通过官方 `codex app-server proxy` 连接已在运行的 daemon，完成握手后只读线程元信息和 turn ID/status，再将 `kind=result` 引导到原线程当前运行的 turn。内部使用 `turn/steer` 的 `expectedTurnId` 防止引导到已变化的运行。该分支不处理普通任务或 interrupt，也绝不回退到 queue。

当前本机已通过宿主内官方 `send_message_to_thread` 工具，把两条 MQ 正式结果直接引导到原入口的运行中聊天；消息立即入站，没有让用户点击队列。公共工具实现使用 `send-now` follow-up，忙时进入 steer 分支、空闲时进入 start 分支。本次实测验证了忙时结果引导，不代表空闲唤醒也已验收。

这次调用来自原生 AGI 入口中可用的官方桌面工具。独立 Python proxy 的支持边界仍不同：本机桌面宿主以 stdio 管理自己的 app-server，没有向外开放默认 `app-server-control.sock`，实际 proxy 诊断连接该默认路径失败。因此本项目尚未在这个宿主验证独立 Client 自动返回；不能把宿主内工具验收写成外部 proxy 已接通。不得新建另一个 daemon、复制登录信息或连接未公开的桌面 IPC 来冒充当前聊天。

## 配置与运行

先在 Hub 中注册 `codex` endpoint，并将对应 binding 设置为 `"driver": {"type": "external"}`。导出私有配置：

```sh
agi-hub client-config --endpoint codex --file /absolute/path/to/private/codex/client.json
```

该命令只导出 endpoint 凭据，`native` 为空，不覆盖已有文件。按 [client.example.json](client.example.json) 填写 transport、`native.thread_id`、`native.executable` 和可选超时。Hub 的 `endpoint.driver` 必须为 `codex`，`endpoint.session_id` 必须与 `native.thread_id` 一致；Client 每次连接先验证这一绑定。必须确认已选择传输在目标原生宿主可用，并指定实际已有 thread ID。默认本机 Hub 为 `http://127.0.0.1:7331`，以导出的真实配置为准。

执行入口启动时解析为绝对路径，投递不重新查找 PATH。Windows 应填写实际 `codex.exe` 路径；`.cmd` / `.bat` 包装器会被拒绝，消息参数不会通过 shell 解释。

app-server 模式的 `native.result_mode` 为 `steer`。Hub Envelope 的 `mode=queue` 表示持久化结果返回，仍须 `kind=result`，不会转成一个新的控制任务。忙时只向当时已核实的 active turn 引导；初始化失败、turn 变化、超时或 ACK 不匹配，都保留 `uncertain`，不会重发。原生输出不进入日志，`thread/read` 排除历史，turn 列表使用 `itemsView=notLoaded`。

`native.idle_start_verified` 默认 `false`。仅在同一已加载原线程通过工具保留和空闲启动实测后，才可显式开启 `turn/start` 用于显示返回结果；未加载线程不自动 resume。RPC ACK 只证明原生接收，不能代替业务回执或证明用户已经看到结果。

若明确选择旧 queue 兼容传输，使用它自己的最小 native 配置；不要保留 app-server 专用字段：

```json
{
  "transport": "queue",
  "thread_id": "<existing verified native thread ID>",
  "executable": "codex",
  "timeout_seconds": 30
}
```

当前示例仍明确选择 app-server + steer；其投递不确定时不会自动采用上述 queue 配置。

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
