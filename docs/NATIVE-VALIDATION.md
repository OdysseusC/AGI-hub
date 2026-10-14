# 本机原生接入记录

这份记录只保存支持范围和验证方法，不包含个人联系人、真实聊天、会话 ID 或凭据。验证时间为 2026-10-14，环境为 macOS arm64、WorkBuddy 5.8.2。其他系统需要单独实测。

## WorkBuddy

| 检查 | 实际证据 | 当前判断 |
| --- | --- | --- |
| 自动安装接收技能 | Hub CLI 安装到本版本用户技能目录；原生已安装列表显示 `agi-hub-client` 且启用 | 技能发现通过；尚未验收主动 Hub 回报 |
| 普通桌面任务的企微只读能力 | 经原生界面提交用户指定的单个联系人查询；返回匹配结果与工具说明 | 原生查询基线通过；不是 MQ 自动闭环 |
| 本地助理空闲接收诊断 | 原生界面接收消息并显示运行结束，回复正文为空，未返回指定校验串 | 不能标记唤醒或接手验证通过 |
| 官方后台触发 | `localassistant/message` 有文档；本机尚无第三方应用与 OAuth 授权 | Client 实现存在，接入未配置 |
| CLI 直接复用桌面登录 | 正式 CLI 登录前，定位本次桌面验收任务的目录与会话 ID；随包及完整官方 CLI 2.161.0 首次执行 `resume + prompt`，都返回需要 `/login` | 原桌面登录未被直接调用复用，没有原会话变化 |
| CLI 本人正式登录与独立续聊 | 完整官方 CLI 在隔离配置目录正式登录；新建测试会话后退出执行进程，再按同一 ID 续聊，正确回答上一轮讨论的两种编程语言 | 独立 CLI 的消息执行与上下文续接通过；不是 WorkBuddy 桌面接入 |
| 已登录 CLI 恢复原桌面 ID | 隔离配置目录返回找不到会话；改用原 WorkBuddy 配置目录后，完整 CLI 与随包 CLI 均复述出原只读查询的上下文 | 原历史续接通过；无工具、无持久化，不代表原生工具继承 |
| CLI 工具开启对照 | 同版本完整及随包 CLI 开启 `Bash,Read` 后，都返回需要 `/login`；随包 CLI 仅复述上下文的工具开启对照也同样失败，关闭工具再测成功 | 工具开启路径未通过；未实际重新查询企微，不能启用为原生 Client |

查询测试未向联系人发送消息、修改联系人或扩大原生权限。普通任务的工具与官方持续本地助理的工具能否保持一致，仍需分别验证。

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

正式 CLI 登录前的首次测试中，两者都在模型执行前返回：`Authentication required. Please use /login command to sign in to your account`。虽然退出码为 0，但 stdout 为空，没有 ACK；原记录没有变化，原桌面聊天也没有新回复。这一阶段的接手、上下文恢复、原生工具保留和后台唤醒均未获得成功证据。

随包 `ps` 另有 `/internal/ps` 映射缺失；这是独立诊断，不能拿来解释上述登录错误。本项目不调用未知内部路由，也没有提取桌面 token 或 IPC ticket 来绕过登录。

### 正式登录后的续聊测试

用户已完成完整官方 CLI 的本人登录，CLI 明确显示登录成功。此次登录使用仓库外的私有隔离配置目录和空测试工作目录，没有手工读取或复制登录凭据。配置目录隔离不代表凭据存储隔离：公开实现还有按产品认证 ID 选择文件的共享认证存储，不能把配置路径当成账户安全边界。

在隔离配置目录中，用原桌面工作目录与原会话 ID 执行 `--resume`，返回 `No conversation found with session ID`。原桌面历史的大小与修改时间不变。CLI 2.161.0 的本机实现将历史保存在当前配置根的 `projects` 下；`--resume` 接受会话 ID，没有找到公开的独立历史目录或 JSONL 路径恢复入口。这个结果只说明此次配置目录没有原桌面会话。

随后在空测试工作目录内创建独立 CLI 会话，讨论“Go 编写消息队列核心、Python 编写助手客户端”。第一轮执行进程退出后，再用同一 ID 发第二条消息，只问上一轮采用哪两种语言；正式答复正确返回“核心：Go；客户端：Python”。两次执行均无工具或 MCP，均收到同一测试会话 ID 的成功结果。由此确认独立 CLI 可在没有执行进程的状态下续接消息和上下文，未验证原桌面聊天同步、企微工具、忙时引导或打断。

固定校验串的初次诊断被模型拒绝，第二轮返回 `UNKNOWN`，即使 CLI 的结果仍标为 `success`，也未算业务完成。后续的正常设计问答才满足上下文验收条件。

该版本 `--output-format json` 实际输出消息数组，包含旧历史，末尾为 `type=result` 的结果对象。验收读取末尾结果并核对任务要求，没有把整段历史当成果转发。该版本实现的 `num_turns` 取活动历史条目数，不能当作本次模型调用次数，也不能只凭退出码或 `subtype=success` 判断完成。

### 原桌面历史续接与工具保留对照

完成正式 CLI 登录后，再用原 WorkBuddy 配置根、原任务工作目录和原会话 ID 测试。完整官方 CLI 与 WorkBuddy 随包 CLI 在工具关闭时，都正确复述原只读查询的姓名、部门与职务，并返回同一原会话 ID。随后重复随包的无工具基线仍成功。所有这些测试均为 `--no-session-persistence`，原历史的大小与修改时间未变。

工具开启路径没有通过：两种 CLI 分别用 `Bash,Read`、单人只读查询目标和明确的只读命令许可测试，都在执行前返回 `Authentication required`。去掉额外命令许可、把轮次上限降为一轮，并改为仅复述已有上下文后，随包 CLI 仍返回相同登录错误。因此已经观察到无工具路径与工具开启路径的差别；没有证据认定具体根因，也没有实际完成新的企业微信搜索。未继续在 WorkBuddy 目录重新登录或改写受保护凭据来绕过此问题。

原历史结果较长时，首次通过 stdout 管道捕获的 JSON 恰为 65536 字节且不完整，没有作为有效结果使用。无工具只读重测改用权限受限的临时普通文件接收完整输出，解析末尾结果后删除临时内容。未来 CLI Client 也必须拒绝残缺输出并保留不确定状态，不能仅靠进程退出码宣称完成。

当前结论是：CLI 的独立续聊和原历史续接均有成功证据；WorkBuddy 原生工具保留、桌面新消息同步、忙时行为与 MQ 自动闭环仍未通过。未向原桌面历史持久化追加诊断消息，GUI 新消息同步未验收；未启用未经验证的 CLI 适配器。已有桌面界面的企微只读查询成功不能替代这项 CLI 验收。

当前 WorkBuddy Client 仍使用本地助理 OpenAPI，CLI 登录没有配置它所需的第三方 OAuth，也没有启用 Hub 的原生唤醒标记。这次独立 CLI 测试没有经过 MQ，不能算 Hub 自动闭环。

官方 [CodeBuddy HTTP API](https://www.codebuddy.cn/docs/cli/http-api) 提供向等待输入的 CLI job 回复与事件订阅。它适合作为独立 CLI 会话适配候选，不能冒称已保留 WorkBuddy 桌面聊天的企微登录及工具。官方 [WorkBuddy 任务对话](https://www.workbuddy.cn/docs/workbuddy/Conversation) 明确提供桌面排队和提前引导；这些 UI 能力仍需要可验证的外部入口才能供独立 Client 使用。

官方 [本地助理 OpenAPI](https://open.workbuddy.cn/docs/openapi) 是已公开的后台触发候选，需要注册第三方应用并由用户 OAuth 授权，没有普通桌面聊天的 session/task 定位参数。已登录 WorkBuddy 不等于已授权第三方应用。当前 Client 缺少授权时在领取任务前停止，不读取应用缓存凭据或内部 IPC ticket。

## 仍需验收

- 指定原生入口空闲时收到任务并启动处理，保留该入口的原生工具。
- 同一关联 ID 的真实接手、执行、成果验证与返回原入口。
- 原生需要用户处理时不自动批准，客户端重启或断线时不重复执行。
- 在 Windows、macOS、Linux 分别核实产品版本和原生能力；构建通过不代替这些测试。

返回 [Client 说明](CLIENTS.md) · [接入范围](ADAPTERS.md)
