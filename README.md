# AGI Hub

从你正在使用的原生 AI 助手出发，按需找其他助手协助。

AGI Hub 是常驻本机的消息边车。每个助手安装协作技能和自己的 Client，只关注自己的邮箱。助手决定分工、收件人和如何整合结果；Hub 负责持久消息、定向投递、权限、去重和基础限额。

可以从 Codex、WorkBuddy 或后续接入的任意助手开始，不需要切到一个新的聊天壳，也没有永久的逻辑主持人。相同模型的 API 调用不等于接入对应的原生助手。

- [把安装包交给 Codex：从这里开始](START-HERE.md)
- [安装与 WorkBuddy 接入引导](INSTALL.md)
- [架构](docs/ARCHITECTURE.md) · [消息协议](docs/PROTOCOL.md)
- [技能更新](docs/SKILLS.md) · [各助手 Client](docs/CLIENTS.md)
- [本机验收与支持范围](docs/NATIVE-VALIDATION.md)

## 工作方式

```text
用户 → A 的原生会话
       A 发消息给 B、C、D，各自进入对应邮箱
       B 需要 D 协助时，再发消息给 D
       各助手按上下文选择回复对象
       A 收到所需结果后，向用户交付
```

消息携带必要上下文、关联 ID、原入口引用和可选回复引用。`recipient` 始终由发送方明确选择，`root_origin` 用于记住最初的人类入口，`reply_to` 用于追踪关联；二者都不强制回信沿某条链返回。

Hub 不建立任务树、DAG、团队计划或统一智能路由，不判断谁该等谁，也不根据一条回复自动完成另一份工作。技能负责这些判断，并尽量少传上下文、少唤醒模型。

## 已实现

| 能力 | 行为 |
| --- | --- |
| 原生会话绑定 | 每个 endpoint 绑定具体产品、设备和会话，用户选择启用 |
| 寻址邮箱 | `request / reply / status / result`，助手显式收发与回复 |
| 权限与目录 | 可见成员和允许发送方向分别配置，默认不互通 |
| 可靠投递 | 稳定消息 ID、持久化后领取、投递回执；不确定时保留，不盲目重发 |
| 原入口上下文 | 同一协作保留最初会话，助手据此选择最终交付对象 |
| 基础限额 | 同一关联共享消息数和期限，转发不能复制剩余额度 |
| 节省模型消耗 | 状态记录不触发模型；目录按需读取，不复制整段历史或重复确认 |
| 技能维护 | 只同步登记的受管位置，保护用户修改、卸载和版本降级 |
| 本地状态页 | 查看邮箱投递与历史记录，不充当新的工作入口 |

传统的单次 Task/Report 接口保留兼容；新邮箱消息不依赖它，不自动生成业务回复。

本机 Codex ↔ WorkBuddy 原会话已完成一次纯邮箱协作：请求、核验回复和成果自动回到原入口；首轮曾缺少消息 ID，修正提示后经一次格式说明继续完成。[实测过程与限制](docs/NATIVE-VALIDATION.md#2026-10-19纯邮箱-codex--workbuddy-原会话)

## 技术与平台

| 部分 | 选择 |
| --- | --- |
| Hub | Go 标准库，单个可执行文件，零外部 Go 依赖 |
| 固定身份收发程序 | Go 可执行文件，只持对应 endpoint 凭据，供技能发送和查询 |
| 消息存储 | 本机带校验的追加快照日志，持久化后投递 |
| 事件 | 本机 HTTP loopback、SSE；不定时扫描聊天历史 |
| Codex Client | Node 标准库，通过官方 Desktop MCP 发到指定原聊天 |
| WorkBuddy Client | Python 标准库，通过官方 CodeBuddy CLI `--resume` 指定原会话 |
| 安装器 | Go 便携引导程序、包内校验清单和分系统启动入口 |
| 目标平台 | Windows、macOS、Linux，amd64/arm64 六目标构建；原生能力逐平台验收 |

不需要 Redis、RabbitMQ、数据库、Docker 或 Agent 调度框架。运行已编译 Hub 和安装器不需要 Go；按所选 Client 准备对应原生应用及其运行时。一个助手一个目录，可以独立维护和更新。

## 安装包

选对应系统的便携 ZIP，解压后将整个目录和 `START-HERE.md` 交给 Codex。可以这样说：

> 按这个目录的 START-HERE.md，帮我接通当前 Codex 和本机 WorkBuddy。保留已有配置，先检查实际环境，再引导我完成必要的会话和权限确认。

包内带 Hub、安装器、固定身份程序、Client 和公开说明。安装器将运行文件放入稳定的私有位置，不把凭据写回解压目录，不自动启动模型或启用未经验证的绑定。它会给出一段可复制到 WorkBuddy 的安装核对消息，接着帮助取得实际会话信息、配置对应 Client 和最小权限。

安装完成、技能被发现、空闲唤醒、业务执行与结果返回分别核对。当前还不是所有助手通用的一键连接器。[完整步骤](INSTALL.md)

## 最少的消息示例

先完成安装、会话绑定和双方 Client 核验。在原生助手的获准工作区保存消息文件：

```json
{
  "id": "saved-message-001",
  "recipient": "workbuddy",
  "kind": "request",
  "content": "只读核验明确给出的资料，简短返回结论和成果位置。原用户允许的范围和文件设备/工作区在这里说明。",
  "origin_ref": "actual-human-request-reference",
  "max_messages": 16
}
```

使用已安装的 endpoint 固定程序：

```text
<fixed-client> mail-send --file <absolute-workspace-message.json>
```

完整 Hub CLI 还要明确 `--data` 和 `--endpoint`；固定程序的身份已经写在私有绑定中，收件人只放在 JSON。后续协作沿用返回的 `correlation_id`，回复使用新消息 ID、明确的 `recipient` 和可选 `reply_to`。超时保留原文件与 ID，先核对状态。

## 支持范围

新邮箱路径先接 Codex 与 WorkBuddy CLI；Claude、Grok Bot 的已有单次接入各自保留，未启用新邮箱能力。普通 Claude Chat/Cowork、Code 页及独立 CLI 的能力分别判断，不把 MCP、webhook 接受通知或退出码成功当作业务完成。

文件目前只支持同机的明确工作区引用；不自动把本机路径变成另一台电脑可用的文件。账号、原生工具权限、外部发送确认仍由用户及宿主控制，助手间消息不能扩大授权。

多跳中的每个收件人由助手决定。Hub 能限制消息数量，不能承诺限制宿主内部全部模型调用或积分。投递不确定的消息需要对账；没有“换个 ID 再试”的自动降级。

## 从源码开发

```sh
go test -race ./...
go vet ./...
python3 -m unittest discover -s clients/tests
node --test clients/codex/test_desktop_mcp_client.mjs
go run ./tools/package
```

`dist/` 生成六个平台 ZIP 和 `SHA256SUMS`。构建需要 Go 1.22+；包内包含实际目标平台程序，用户安装不需要编译。测试使用模拟接入点；原生验收证据另记在 [NATIVE-VALIDATION.md](docs/NATIVE-VALIDATION.md)。

代码与个人配置从一开始分开，凭据、真实聊天和私密成果均放在仓库外，不进入安装包。
