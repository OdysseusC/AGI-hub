# 把这个安装包交给 Codex

AGI Hub 让你从已有的 AI 助手聊天发起协作，再把结果送回最初输入的聊天。Hub 保存和投递消息，分工由各助手的技能决定。

下载与你电脑匹配的 ZIP：Mac 的 Apple 芯片（M 系列）选 `macos-arm64`，Intel 芯片选 `macos-amd64`；Windows 的 Intel / AMD 电脑选 `windows-amd64`，ARM 电脑选 `windows-arm64`。完整解压，然后把**解压目录**拖给能操作本机文件的 Codex，并发送：

> 请读取这个目录的 START-HERE.md 和 docs/INSTALL-FOR-ASSISTANT.md，按文档帮我安装 AGI Hub。先核对系统与架构，检查是否已经安装并保留原配置。以当前 Codex 原聊天作为本次入口，先接入 WorkBuddy。可以运行本机安装器；需要我在 WorkBuddy 输入消息、登录或确认原生权限时，给我准确步骤。先用本地文件做一次可逆协作，最后在当前聊天给我验收结果。不要调用 Claude，不发送企业微信消息，不读取无关聊天历史。

安装器已经编译好，**安装 Hub 不需要 Go、npm 或 pip**。接通 Codex Desktop 仍需要当前宿主提供的官方桌面桥接；WorkBuddy CLI Client 需要现有 Python 3.11+、官方 CodeBuddy CLI 和本人登录。安装器不会下载运行时、自动登录或自动批准权限。

## 安装包里有什么

- `agi-hub` / `agi-hub.exe`：本机消息边车。
- `agi-hub-setup` / `agi-hub-setup.exe`：离线基础安装器。
- `agi-hub-endpoint` / `agi-hub-endpoint.exe`：只供配置后的单个助手收发与回报，不含管理能力；通过安装器生成其私有固定绑定后使用。
- `install.command` 或 `install.ps1`：对应系统的入口。
- `clients/`：产品独立 Client；Codex 与 WorkBuddy 分别接入各自原生能力。
- `docs/INSTALL-FOR-ASSISTANT.md`：交给安装助手执行的核对和验收步骤。
- `package-manifest.json`：公开文件清单与 SHA-256，用于检查解压文件是否完整；发行来源仍须由下载渠道和 Release 校验值确认。

所有个人配置、令牌与运行记录保存在系统用户配置目录下的 `agi-hub`，不写回 ZIP。程序按版本复制到其中的 `runtimes/portable-*`；可移动或删除原解压目录。

## 可以自己启动检查

macOS 在解压目录执行：

```sh
./agi-hub-setup --verify-only
./agi-hub-setup --host codex
```

Windows PowerShell：

```powershell
.\agi-hub-setup.exe --verify-only
.\agi-hub-setup.exe --host codex
```

不带参数只显示说明，不安装。macOS 下载隔离或 Windows 信誉提示由本人核对来源后处理；安装器没有签名、公证或管理员提权步骤。不要让助手关闭系统防护。

基础安装只创建私有配置、复制程序并安装明确选定宿主的技能。**“基础安装完成”不表示助手已经接通**：真实会话 ID、CLI 配置目录、接收方式和成果回传还要逐项核对。输出会提供一段不含凭据的 WorkBuddy 接入消息，可由本人复制到选定的 WorkBuddy 原聊天。

安装助手导出 WorkBuddy 专属 Client 配置、确认本次工作目录后，再用 `--client-config <专属配置文件> --workspace <已授权工作目录>` 调用安装器，即可生成只允许该助手 `mail-send / mail-state / peers；report 仅兼容旧任务` 的回报程序。不要为自动协作给整个 Hub 管理程序、Shell 或 Python 解释器开放宽泛权限。

如果安装器报告已有技能被修改、已有安装损坏或版本冲突，保留文件，让安装助手解释差异；不要删掉记录强制重装。一个 endpoint 同时只启动一个 Client。
