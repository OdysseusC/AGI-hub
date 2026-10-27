# 安装 AGI Hub

[简体中文](INSTALL.md) | [English](INSTALL.en.md)

推荐下载本机平台的便携 ZIP，完整解压，把目录和 [START-HERE.md](START-HERE.md) 交给 Codex。首次安装、WorkBuddy 接入消息和原生验收统一见[安装助手指南](docs/INSTALL-FOR-ASSISTANT.md)。

| 系统 | CPU | 安装包后缀 |
| --- | --- | --- |
| macOS | Apple 芯片（M 系列） | `macos-arm64.zip` |
| macOS | Intel 芯片 | `macos-amd64.zip` |
| Windows | Intel / AMD 64 位 | `windows-amd64.zip` |
| Windows | ARM64 | `windows-arm64.zip` |

Mac 在“苹果菜单 → 关于本机”查看：显示“芯片 Apple M…”选择 `macos-arm64`，显示“处理器 Intel…”选择 `macos-amd64`。`amd64` 也叫 x64 / x86-64，适用于 Intel Mac；`darwin` 是 macOS 在 Go 中的内部系统名称，程序版本和校验清单中仍可能显示它。当前只提供 Windows 和 macOS 安装包。

安装器与 Hub 只依赖操作系统。所选助手的官方 CLI、运行时、登录和原生工具权限分别核对；某平台构建成功不表示该平台的原生助手接入已经实测。

## Grok 工作区安装

此入口只把 Grok 可通过已授权 terminal 读取的协作规则和固定身份发信工具落到目标电脑工作区。例如，在用户明确选定的 Windows 电脑 C 上准备文件；它不向云端 Grok Bot/Routine 上传或自动加载技能，不创建原生会话，也不证明原生收件、执行或回信已通过。

先在目标电脑准备现有 Hub 的 Grok endpoint（`driver` 为 `grok-routine`）及其专属私有 `client.json`，按 [Grok Client 说明](clients/grok-routine/README.md)核对真实 Routine 配置。专属配置可用 `agi-hub client-config --data <现有Hub私有目录> --endpoint <Grok端点ID> --file <仓库外绝对路径/client.json>` 导出；不覆盖既有文件。配置和凭据只留目标本机，不贴入聊天。

选择一个本人授权、已存在且不经过符号链接的工作区，并在其内部明确指定 `skills` 子目录。Hub 私有数据和含凭据的 Client 配置应与工作资料分开存放。核验完整解压的本机安装包后，在目标电脑运行；下列占位值须替换为实际绝对路径，endpoint 使用本人选定的 Grok 端点 ID：

```text
agi-hub-setup --host grok --endpoint <Grok端点ID> --package-dir <已核验的解压包目录> --data <现有Hub私有目录> --client-config <端点专属client.json绝对路径> --workspace <已授权工作区绝对路径> --skills-dir <该工作区内的skills绝对路径>
```

Windows 使用实际 `agi-hub-setup.exe`；在 PowerShell 中调用带空格的程序路径时用 `&`，并将每个路径分别加引号。`--client-config`、`--workspace`、`--skills-dir` 均必须显式提供。`skills-dir` 必须在工作区子目录内，拒绝越界、父目录跳转和链接；不存在的 skills 子目录可由安装器创建。此入口不接受 `--report-binary` 替代，不猜 `~/.grok`，也不借用 WorkBuddy 身份。

安装器核对 Client 配置确属当前 Hub 与所选 endpoint，然后复用现有固定身份 helper：

- helper 只持 Grok endpoint 凭据，固定本机数字 loopback 和允许的消息文件工作区；不能切换身份、管理 Hub 或直接启动助手。
- 工作区规则包含 `SKILL.md`、不含凭据的 `installation.json` 和受管语言策略；私有 `binding.json` 留在专属 helper 目录，不需要 Grok 读取其内容。
- 技能进入现有 registry；之后按原有受管同步更新语言和规则。已有私人修改或未知归属文件会阻止覆盖，删除的技能不会被同步擅自重建。

输出中的“工作区协作文件已落盘”只证明文件安装。安装器不启动 Hub 服务、Client 或原生助手，不启用 endpoint 或自启动，不打印凭据。Grok 必须实际具备本人授权的 C terminal 能力，才能按给出的规则路径读取协作技能、语言策略及 `installation.json`，再执行其中固定的 helper。只向 Routine webhook 发消息，不会自动获得 C 的文件或执行权限。

固定 helper 的正常协作命令为 `peers`、`mail-send --file <工作区内已保存JSON>` 和 `mail-state --correlation-id <已知关联ID>`。发件身份固定；收件人由 Grok 在 JSON 中明确选择，仍受 Hub 权限约束。正常协作经 Hub 发送，不能把人工网页转发当作闭环。独立消费者的收件投递、原生能力和真实往返另行验证，详见 [Client 与支持边界](docs/CLIENTS.md)。

## 升级与安装包

升级前先确认原生执行已结束，再停止旧 Hub 和 Client，保留全部消息记录后替换程序。`0.2.0 / 0.2.1` 与 `0.2.2` 的进程锁不同，不支持混跑；不要修改端口来绕过已有实例。同一 API 端口已被占用时，CLI 会在加载日志前拒绝启动。`0.2.2` 及后续版本使用系统文件锁保护同一数据目录，锁文件由程序保留，不手动删除。

正式安装包由 GitHub 的 **Release** 工作流编译、检查并发布；每次发布页面会直接列出本版更新点、安装包选择和验收范围。下载对应版本的 ZIP，可用同页 `SHA256SUMS` 核对。PR 和 `main` 的检查运行也提供 `portable-packages` 临时构建产物，保留 14 天，适合提前验证。

本地构建仍可在仓库根目录运行 `go run ./tools/package`，得到四个 Windows / macOS ZIP 和 `dist/SHA256SUMS`。包内不包含用户配置、凭据或运行记录。发版操作见 [CONTRIBUTING.md](CONTRIBUTING.md)，源码构建方式仍见安装助手指南。
