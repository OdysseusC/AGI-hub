# 零模型 Mailbox 集成验收

`smoke_mailbox.py` 启动实际 Hub 进程，通过真实本机 HTTP、endpoint token、SSE 订阅与公共 Python Client runtime 验证寻址消息。原生投递由内存适配器模拟，不启动 Codex、WorkBuddy、Claude 或其他模型，不读取个人 Hub 配置。

在仓库根目录构建当前系统的 Hub，然后运行：

```sh
go build -o /absolute/temporary/agi-hub ./cmd/agi-hub
python3 tools/smoke_mailbox.py --hub /absolute/temporary/agi-hub
```

Windows 使用实际 `.exe` 路径，Python 可使用 `py -3`。需要 Python 3.11+，没有第三方依赖。

脚本在临时目录生成五个测试成员，结束后清理数据和 Hub 进程。所有“唤醒已验证”配置只属于内存适配器，不会写入用户安装。

覆盖的消息路径为 A 同时向 B、C、D 请求协助；B 再向 D 发消息；D 自行选择直接回复 A；C 和 B 分别显式把结果发给 A。`reply_to` 仅作参考，没有强制返回 B 的父子链。最终原入口和人类输入引用在同一关联中保持一致。

同时检查：

- 公共 Client 只领取自己 Mailbox 的消息；投递回执不能越过 endpoint 范围。
- 重复消息 ID 不重复调用适配器，不增加共享消息计数。
- `status` 保存并确认投递，适配器调用次数不增加。
- 不确定投递保留记录，后续消息维持 pending，另一消费者不能并发领取。
- 关联默认上限为 16 条，各发送者共享计数，不能扩大上限。
- 状态查询只显示本人收发记录；本人 `status` 正文可按需读取，普通工作正文及其他成员间消息不复制；无关成员不能读取关联。
- 没有创建 Task、Report 或自动业务回复，也不根据 Mailbox 消息推断会话业务状态。

通过时 stdout 输出一条 JSON，包含 `native_model_calls: 0`。这份验收证明实际 Hub 与 Client 协议兼容，原生助手的登录、技能加载、闲时唤醒和忙时引导仍须在各宿主单独验收。
