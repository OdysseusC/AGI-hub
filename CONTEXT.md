# AGI Hub

[简体中文](CONTEXT.md) | [English](CONTEXT.en.md)

用户从任意已接入的原生助手工作。技能按需选择其他助手、发送定址消息并处理回复；最终成果由助手发回用户最初输入的会话。Hub 保存消息、目录、投递证据、去重与基础限额，不规划业务、不自动回信或汇总。

## 术语

**助手实例 Assistant instance**：具体助手产品及其电脑、宿主、会话、原生工具和本人登录环境。相同模型的 API 调用不等于接入该助手。

**会话 Session**：可明确定位的原生持续对话。邮箱绑定到实际会话；模型一次回合结束不代表整个用户工作结束。

**原入口 Root origin**：本次用户输入所在会话。消息中的 `root_origin` 固定保存 endpoint、设备、原生会话及输入引用，供技能判断最终回传位置；它不是 Hub 强制路由规则，也不是新的用户授权证明。

**邮箱 Mailbox**：一个已配置 endpoint 的定址收件箱。Client 只领取发给自己的邮件，翻译宿主实际支持的唤醒方式；Hub 不根据消息正文选择接收者。

**消息 Message**：独立 ID、明确发送者与接收者、类别和必要正文。`request/reply/status/result` 描述内容，不建立父子任务状态机。回复由助手明确发送，不由模型回合结束或 Report 自动生成。

**关联 Conversation / Correlation**：一组消息共用的 `correlation_id`，保存原入口、参与者、截止时间和消息总量。它不包含计划、依赖或完成条件。

**关联引用 Reply to**：可选的先前消息 ID，仅用于理解上下文；不强制回原发送者。收件人始终由发送助手在用户允许范围内选择。

**共治 Peer collaboration**：任意获准入口可发起、回应或转交；无需永久 leader 或新聊天入口。对等互通不表示可以冒充用户、扩大授权或共享其他助手的权限。

**技能 Collaboration skill**：说明如何节省上下文、发现可见成员、检查授权、选择接收者、调用邮箱及核验成果。技能自身不监听消息、不提供唤醒，也不自动批准工具权限。

**边车客户端 Client**：连接指定原生助手与 Hub 的收发实现，可以是独立进程或宿主插件。CLI、原桌面会话、云端 webhook、专业模型仍须分别说明能力；不能相互冒认。

**状态消息 Status**：不需要收件模型判断的进度或需本人处理说明。Client 保存并 ACK，不注入模型。需助手实际处理的内容使用 request、reply 或 result。

**投递 Delivery**：pending、dispatching、delivered、uncertain、failed 描述传输。Client 的 ACK 不是业务完成；原生工作进度由助手另发消息说明。

**投递不确定 Uncertain delivery**：操作可能已经发生但缺少确认。保留原记录，不自动重新消费或换 ID 重发。

**成果 Artifact**：可核对的文件或结构化结果。文件说明设备、工作区和位置；邮箱不自动跨电脑复制。最终入口按用户需求核对质量与证据。

**待用户处理 Needs user**：需要人类判断、信息或原生操作批准的业务情况，由助手说明；其他助手不能代替用户批准。它不是新邮箱的 Hub 任务阶段。

当前邮箱协议 `1` 已有 Codex Desktop MCP、WorkBuddy CLI 和 Claude Code mod `0.2.2` 的原生验收证据。2026-10-20 的 Claude 实测覆盖本机 macOS Claude-3p `2.1.299` 既有 Code 会话空闲唤醒、原生 Read 读文件、显式 `mail_send`，再经 Hub 和独立 Codex Desktop MCP 返回忙碌原入口。Claude 仅支持 Code 会话 queue，普通 Chat/Cowork、忙时 steer 不支持；其他宿主和平台另行验收，见[原生记录](docs/NATIVE-VALIDATION.md)。Grok 邮箱仍为 `0`；旧版单次 Task/Report 与其 原生验收记录保留兼容，不作为新邮箱已接通的证明。旧默认 leader 只属于兼容任务接口。
