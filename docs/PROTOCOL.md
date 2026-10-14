# 消息与回执协议

V0 使用同一套 JSON 协议登记会话、领取消息和汇报结果。通知只是有消息可取的提示，完整内容以持久化记录为准。所有示例 ID 都是假数据。

## 任务交接

```json
{
  "id": "task-example-001",
  "origin": "codex",
  "recipient": "grok",
  "goal": "根据已授权资料整理一份摘要文件",
  "context": "只使用本次提供的资料，保留来源。",
  "inputs": [{"device_id": "this-device", "workspace_id": "work", "path": "input/brief.txt"}],
  "mode": "queue",
  "resources": ["workspace:work"],
  "require_artifact": true,
  "authorized_actions": ["read provided input", "write summary in work workspace"],
  "max_handoffs": 1,
  "deadline": "2030-01-01T12:00:00Z"
}
```

`origin` 省略时取当前 leader。任务创建后固定原入口、协调者和 leader 版本；切换 leader 不迁移旧任务。`id` 是调用方持久保存的幂等键，重复发送必须保留相同内容。文件引用使用所属设备、已配置工作区和 `/` 分隔的相对路径，提交前检查文件存在及可选 SHA256；输入文件仍是引用，不会自动冻结版本或跨电脑复制。

`authorized_actions` 是交接说明，不是新的用户批准或原生助手权限沙盒。V0 仅允许管理入口创建任务，普通 endpoint 凭据只能领取自己的消息和发送自己的回执；同一系统账号内具备完整文件权限的原生助手不属于额外隔离的安全域。

`max_handoffs` 在 V0 固定为 1，一次交接后仅返回结果。没有自动递归转派。`deadline` 限制尚未投递的消息；不会自行停止已经执行的原生任务，也不表示已落实 Token 或费用预算。

## 消息发送模式

| 模式 | 目标关系 | 接收约束 |
| --- | --- | --- |
| `queue` | 新独立任务 | 已核实会话空闲、没有未完成或不确定的交接、资源可用后投递 |
| `steer` | 为现有运行补充方向 | 必须填写 `target_run_id`，原运行保持进行，原生 Client 必须支持引导 |
| `interrupt` | 请求停止现有运行 | 必须填写 `target_run_id`；收到实际 `interrupted` 回执才释放旧运行资源 |

不支持的模式明确拒绝，无自动替换。停止成功不代表已经启动替代任务；恢复暂停也不是当前协议的能力。模式可按每条消息指定，客户端能力配置约束允许的模式，当前默认方式为 `queue`。

## 执行回执

```json
{
  "event_id": "event-example-accepted",
  "task_id": "task-example-001",
  "endpoint": "grok",
  "state": "accepted",
  "run_id": "native-run-example"
}
```

状态依次为 `waiting`、`accepted`、`running`，可以进入 `needs_user` 再恢复 `running`，最终为 `succeeded`、`failed` 或 `interrupted`。每个回执使用独立、稳定的 `event_id`；相同事件重复提交去重，同 ID 不同内容拒绝。接手后的 `run_id` 必须一致。

```json
{
  "event_id": "event-example-completed",
  "task_id": "task-example-001",
  "endpoint": "grok",
  "state": "succeeded",
  "run_id": "native-run-example",
  "summary": "摘要已生成并核对来源。",
  "artifacts": [{"device_id": "this-device", "workspace_id": "work", "path": "output/summary.md"}]
}
```

成功需要摘要；任务要求文件时还需至少一个可验证文件。边车检查文件归属、路径边界、软链接、存在性及可选校验值，并记录实际 SHA256。它核对可访问的成果证据，成果质量仍通过任务验收判断。

## 投递与返回

投递状态独立记录：`pending → dispatching → delivered / uncertain / failed`。Client 领取时先持久化 `dispatching`。原生传输成功仅确认 `delivered`；原生执行回执才确认接手和业务状态。只有 Client 重启时，已有 claim 保持 `dispatching`；Hub 重启后遗留 `dispatching` 转为 `uncertain`。两者都保留资源占用，不自动重发。

正式结果生成一条 `kind=result` 消息，返回任务的原入口。它不是新任务，不重新执行，不产生新交接。结果投递状态与业务成功状态独立；原入口暂不可用时保留成果和待返回消息。

## 本机 API

所有接口默认只监听数字形式的 loopback 地址。管理凭据保存在私有配置中；endpoint 凭据由管理密钥和 endpoint ID 派生，不能据此修改其他客户端或 leader。

| 接口 | 操作 | 权限 |
| --- | --- | --- |
| `GET /v1/state` | 查看本机记录 | 管理 |
| `POST /v1/tasks` | 提交完整任务 JSON | 管理 |
| `POST /v1/leader` | 设置后续任务 leader | 管理 |
| `POST /v1/session` | 核实并更新会话状态 | 管理 |
| `GET /v1/endpoint?endpoint=ID` | 查询自身协议版本、会话绑定及状态 | 对应 endpoint 或管理 |
| `POST /v1/inbox?endpoint=ID` | 原子领取下一条可处理消息 | 对应 endpoint 或管理 |
| `POST /v1/delivery?endpoint=ID` | 汇报投递证据 | 对应 endpoint 或管理 |
| `POST /v1/report?endpoint=ID` | 汇报原生执行及结果 | 对应 endpoint 或管理 |
| `GET /v1/events?endpoint=ID` | SSE 变更提示 | 对应 endpoint 或管理 |

SSE 首次连接发送一次变更提示，随后按事件推送；完整状态和消息通过授权接口读取。连接保活不轮询业务任务。独立 Client 应先订阅、再领取，重连后只领取仍为 `pending` 且满足条件的消息；已经领取但投递不确定的消息不会被重新领取。Hub 没有内置助手执行器，也不把 SSE 断开当作原生操作失败的证据。

Client 在连接与领取前先读取自身 `/v1/endpoint`。返回 `protocol_version: 1`、该 `endpoint` 和 `session`，不返回其他会话、任务或凭据。Client 校验产品类型、支持模式及原生会话 ID 与私有配置一致；不兼容协议或目标错配时拒绝消费，不能以不同会话执行后再冒认原绑定。

Client 和 Hub 约定协议版本 `1`。当前 Client 的 manifest 描述协议、会话类型与发送模式，Hub 不自动加载它或因此扩大 endpoint 的能力。每个 endpoint 使用一个 Client 进程；不同 Client 可以用不同语言实现，必须遵守领取后不盲重发、回执去重和授权边界。原生配置与凭据属于 Client 的私有文件，不能加入 Envelope 或 Hub 配置。详见 [独立 Client](CLIENTS.md)。
