# WorkBuddy Client

版本 `0.1.0`，Hub 协议 `1`，依赖同级 `shared` runtime `0.1.0` 和 Python 3.11+。使用 Python 标准库；单独复制本目录时必须一起复制匹配的 shared，保持相邻目录结构。

本 Client 使用官方 `/openapi/v2/localassistant/message`，目标是持续本地助理。接口没有用于定位普通聊天的 `session_id` 参数。仅支持 `queue`，不支持已验证的 `steer` 或 `interrupt`。只有 HTTP 2xx 且业务响应 `code` 为整数 `0`、`data.message_id` 为合法非空字符串，才记录已投递；其他响应保留不确定状态，不重发。原生助手仍须主动报告接手和成果。本项目目前没有持有真实官方 OAuth token 完成原生唤醒、执行和回传验收。

## 配置与运行

先注册 `workbuddy` endpoint，将对应 binding 设置为 `"driver": {"type": "external"}`。其中 `endpoint.driver` 为 `workbuddy`，`endpoint.session_id` 必须设为 `localassistant`，表示官方持续专用助理；不能填写一个普通聊天 ID。Client 每次连接先核对该绑定，再订阅和领取。导出私有配置：

```sh
agi-hub client-config --endpoint workbuddy --file /absolute/path/to/private/workbuddy/client.json
```

命令只导出 endpoint 凭据和空 `native`，不覆盖已有文件。按 [client.example.json](client.example.json) 填写用户本人通过官方 OAuth 流程授予的短期 `native.access_token`，并记录 `native.oauth_scopes` 中的 `user.localassistant.invokable`。未配置时会在领取消息前停止。

不读取桌面登录 Cookie、缓存 token 或 IPC ticket；不支持自动 token 续期。官方应用授权和长期续期接入仍待验证。默认 Hub 为本机 `http://127.0.0.1:7331`，以导出配置为准。

在仓库根目录运行：

```sh
python3 clients/workbuddy/main.py --config /absolute/path/to/private/workbuddy/client.json
```

Windows 使用 `py -3` 和该电脑的绝对路径。个人配置放在仓库外；不要并行启动同一 endpoint 的两个 Client。支持范围、官方来源和回执恢复见 [Client 文档](../../docs/CLIENTS.md)。

## 维护与测试

WorkBuddy 的实现、配置、manifest 和专项测试都放在本目录。单独验证适配器：

```sh
python3 clients/workbuddy/test_adapter.py -v
```

公共协议及跨 Client 兼容测试在 `clients/tests`，公共夹具在 `clients/tests/support.py`。仓库根目录运行 `python3 -m unittest discover -s clients/tests -v` 会汇总所有助手专项测试；只使用本地模拟，不调用官方真实接口。
