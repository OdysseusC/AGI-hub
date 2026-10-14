# 本机原生接入记录

这份记录只保存支持范围和验证方法，不包含个人联系人、真实聊天、会话 ID 或凭据。验证时间为 2026-10-14，环境为 macOS arm64、WorkBuddy 5.8.2。其他系统需要单独实测。

## WorkBuddy

| 检查 | 实际证据 | 当前判断 |
| --- | --- | --- |
| 自动安装接收技能 | Hub CLI 安装到本版本用户技能目录；原生已安装列表显示 `agi-hub-client` 且启用 | 技能发现通过；尚未验收主动 Hub 回报 |
| 普通桌面任务的企微只读能力 | 经原生界面提交用户指定的单个联系人查询；返回匹配结果与工具说明 | 原生查询基线通过；不是 MQ 自动闭环 |
| 本地助理空闲接收诊断 | 原生界面接收消息并显示运行结束，回复正文为空，未返回指定校验串 | 不能标记唤醒或接手验证通过 |
| 官方后台触发 | `localassistant/message` 有文档；本机尚无第三方应用与 OAuth 授权 | Client 实现存在，接入未配置 |
| CLI 投递现有桌面聊天 | 本版本随包提供 CodeBuddy Code CLI；帮助和公开实现未找到按桌面 task/thread 投递命令 | 没有已验证的 Codex queue 等价路径 |

查询测试未发送消息、修改联系人或扩大原生权限。普通任务的工具与官方持续本地助理的工具能否保持一致，仍需分别验证。

### CLI 的实际范围

当前 PATH 没有独立 `workbuddy / codebuddy / cbc` 命令。安装包内 CLI 的只读帮助可用：

```sh
CODEBUDDY_FORCE_LITE_WB_BUNDLE=1 \
  /Applications/WorkBuddy.app/Contents/Resources/app.asar.unpacked/cli/bin/codebuddy --help
```

这是当前 macOS 安装包的检查方法，不是通用安装或唤醒命令。帮助确认 `--resume / --session-id / --serve / --bg / attach / respawn / agents` 等能力；没有公开 `queue --thread` 等价命令。安装包公开实现中，恢复为后台 job 的路径创建一个新的 CLI 执行进程，不能据此认定注入现有桌面会话。

官方 [CodeBuddy HTTP API](https://www.codebuddy.cn/docs/cli/http-api) 提供向等待输入的 CLI job 回复与事件订阅。它适合作为独立 CLI 会话适配候选，不能冒称已保留 WorkBuddy 桌面聊天的企微登录及工具。官方 [WorkBuddy 任务对话](https://www.workbuddy.cn/docs/workbuddy/Conversation) 明确提供桌面排队和提前引导；这些 UI 能力仍需要可验证的外部入口才能供独立 Client 使用。

官方 [本地助理 OpenAPI](https://open.workbuddy.cn/docs/openapi) 是已公开的后台触发候选，需要注册第三方应用并由用户 OAuth 授权，没有普通桌面聊天的 session/task 定位参数。已登录 WorkBuddy 不等于已授权第三方应用。当前 Client 缺少授权时在领取任务前停止，不读取应用缓存凭据或内部 IPC ticket。

## 仍需验收

- 指定原生入口空闲时收到任务并启动处理，保留该入口的原生工具。
- 同一关联 ID 的真实接手、执行、成果验证与返回原入口。
- 原生需要用户处理时不自动批准，客户端重启或断线时不重复执行。
- 在 Windows、macOS、Linux 分别核实产品版本和原生能力；构建通过不代替这些测试。

返回 [Client 说明](CLIENTS.md) · [接入范围](ADAPTERS.md)
