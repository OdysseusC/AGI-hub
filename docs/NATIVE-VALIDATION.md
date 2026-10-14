# 本机原生接入记录

这份记录只保存支持范围和验证方法，不包含个人联系人、真实聊天、会话 ID 或凭据。验证时间为 2026-10-14，环境为 macOS arm64、WorkBuddy 5.8.2。其他系统需要单独实测。

## WorkBuddy

| 检查 | 实际证据 | 当前判断 |
| --- | --- | --- |
| 自动安装接收技能 | Hub CLI 安装到本版本用户技能目录；原生已安装列表显示 `agi-hub-client` 且启用 | 技能发现通过；尚未验收主动 Hub 回报 |
| 普通桌面任务的企微只读能力 | 经原生界面提交用户指定的单个联系人查询；返回匹配结果与工具说明 | 原生查询基线通过；不是 MQ 自动闭环 |
| 本地助理空闲接收诊断 | 原生界面接收消息并显示运行结束，回复正文为空，未返回指定校验串 | 不能标记唤醒或接手验证通过 |
| 官方后台触发 | `localassistant/message` 有文档；本机尚无第三方应用与 OAuth 授权 | Client 实现存在，接入未配置 |
| CLI 续聊实际验收 | 定位本次桌面验收任务的目录与会话 ID；随包及完整官方 CLI 2.161.0 分别执行 `resume + prompt`，都返回需要 `/login` | 原桌面登录未被本次直接 CLI 调用复用，没有 ACK 或原会话变化，未通过接入验收 |

查询测试未发送消息、修改联系人或扩大原生权限。普通任务的工具与官方持续本地助理的工具能否保持一致，仍需分别验证。

### CLI 的实际范围

当前 PATH 没有独立 `workbuddy / codebuddy / cbc` 命令。安装包内 CLI 的只读帮助可用：

```sh
CODEBUDDY_FORCE_LITE_WB_BUNDLE=1 \
  /Applications/WorkBuddy.app/Contents/Resources/app.asar.unpacked/cli/bin/codebuddy --help
```

这是当前 macOS 安装包的检查方法，不是通用安装或唤醒命令。帮助确认 `--resume / --session-id / --serve / --bg / attach / respawn / agents` 等能力。官方明确支持 `codebuddy --resume SESSION_ID "新消息" -p`，可恢复 CLI 历史并执行新一轮；可交互后台 job 也可以 attach 后输入。`respawn` 本身不接收新消息。[官方无头模式](https://www.workbuddy.cn/docs/cli/headless)

桌面安装包的公开启动实现使用 `--session-id conversationId`，目标任务的本地历史也具有对应文件名；这提供了 ID 映射依据，仍不能证明外部进程保留桌面登录和工具。

### `resume + prompt` 实测结果

本次只定位用户授权的验收任务，未扫描其他聊天正文。使用官方支持的 `CODEBUDDY_CONFIG_DIR` 指向该用户的 WorkBuddy 配置目录，工作目录与目标任务保持一致。[自定义配置目录](https://www.codebuddy.cn/docs/cli/installation#自定义配置目录)

两个执行入口分别测试：桌面随包 CLI，以及在仓库外私有测试目录安装的同版本完整官方 CLI。安装未替换桌面应用，未全局安装或自动更新。诊断关闭工具和 MCP，采用 `--no-session-persistence` 与一轮上限，要求返回固定 ACK 并核对原记录的大小与修改时间。

两者都在模型执行前返回：`Authentication required. Please use /login command to sign in to your account`。虽然退出码为 0，但 stdout 为空，没有 ACK；原记录没有变化，原桌面聊天也没有新回复。接手、上下文恢复、原生工具保留和后台唤醒均未获得成功证据。

随包 `ps` 另有 `/internal/ps` 映射缺失；这是独立诊断，不能拿来解释上述登录错误。本项目不调用未知内部路由，也没有提取桌面 token 或 IPC ticket 来绕过登录。后续如验证 CLI 本人登录，需要单独完成正式登录流程，再继续核对桌面同步和企业微信原生能力，不能将独立 CLI 执行自动算作接通桌面助手。

官方 [CodeBuddy HTTP API](https://www.codebuddy.cn/docs/cli/http-api) 提供向等待输入的 CLI job 回复与事件订阅。它适合作为独立 CLI 会话适配候选，不能冒称已保留 WorkBuddy 桌面聊天的企微登录及工具。官方 [WorkBuddy 任务对话](https://www.workbuddy.cn/docs/workbuddy/Conversation) 明确提供桌面排队和提前引导；这些 UI 能力仍需要可验证的外部入口才能供独立 Client 使用。

官方 [本地助理 OpenAPI](https://open.workbuddy.cn/docs/openapi) 是已公开的后台触发候选，需要注册第三方应用并由用户 OAuth 授权，没有普通桌面聊天的 session/task 定位参数。已登录 WorkBuddy 不等于已授权第三方应用。当前 Client 缺少授权时在领取任务前停止，不读取应用缓存凭据或内部 IPC ticket。

## 仍需验收

- 指定原生入口空闲时收到任务并启动处理，保留该入口的原生工具。
- 同一关联 ID 的真实接手、执行、成果验证与返回原入口。
- 原生需要用户处理时不自动批准，客户端重启或断线时不重复执行。
- 在 Windows、macOS、Linux 分别核实产品版本和原生能力；构建通过不代替这些测试。

返回 [Client 说明](CLIENTS.md) · [接入范围](ADAPTERS.md)
