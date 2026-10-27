# 贡献指南

[简体中文](CONTRIBUTING.md) | [English](CONTRIBUTING.en.md)

保持 AGI Hub 简单：助手通过技能决定如何协作，Hub 负责定址消息、投递记录、限额和去重。新适配器必须说明实际的原生会话、登录和唤醒能力。仅调用模型 API 不代表接入了对应的原生助手。

## 修改

公开文档以无语言后缀的 `*.md` 展示中文，英文放在 `*.en.md`；导航固定中文在前、English 在后。安装给模型的协作技能仍只保留一份英文正文，避免重复上下文。

创建 `feat/...`、`fix/...` 或 `docs/...` 这类聚焦的分支，再向 `main` 提交拉取请求。不要把实现直接推送到 `main`。必须通过的 GitHub Actions 状态检查是 **`checks`**，它汇总各平台测试和便携包构建。分支保护在 GitHub 仓库设置中配置，与本文件分别维护。

在拉取请求中说明具体行为、验证方式和原生平台限制。个人配置、凭据、真实聊天记录和私密成果均留在仓库之外。自动化测试中不得加入模型或账号调用。

按改动范围运行相关检查；完整离线套件为：

```sh
go test -race ./...
go vet ./...
python3 -m unittest discover -s clients/tests -v
python3 -m unittest discover -s tools -p 'test_*.py' -v
python3 tools/check_docs.py
node --test clients/codex/test_desktop_mcp_client.mjs
node --test clients/claude-code/tests/client.test.mjs
go run ./tools/package
go build -o dist/agi-hub-smoke ./cmd/agi-hub
python3 tools/smoke_client.py --hub "$PWD/dist/agi-hub-smoke"
python3 tools/smoke_skill_sync.py --hub "$PWD/dist/agi-hub-smoke"
python3 tools/smoke_mailbox.py --hub "$PWD/dist/agi-hub-smoke"
python3 tools/smoke_startup.py --hub "$PWD/dist/agi-hub-smoke"
```

冒烟测试程序按开发电脑的平台构建，与四个 Windows/macOS 正式安装包分开。Windows 上将其命名为 `dist/agi-hub-smoke.exe`，并把该路径传给脚本。测试使用临时私有配置和模拟原生适配器。通过测试不证明原生应用的登录、空闲唤醒或忙时引导可用；这些需要单独且经授权的本机验收。

## 发布

编译和打包由 GitHub Actions 执行。每次拉取请求和推送到 `main` 都会在离线检查后上传 `portable-packages` 产物；功能分支推送不重复运行拉取请求检查。本地打包继续供开发使用。

发版时，先通过拉取请求合并版本号和更新记录，再推送指向该已审查 `main` 提交的严格 SemVer 标签，例如 `v0.3.0`。**Release** 工作流检查主分支祖先关系，在标签指向的确切提交运行完整的可复用检查，下载同一次运行的不可变产物，再核对来源提交、四项校验值及 macOS 包内程序版本。它将四个 Windows/macOS ZIP 和 `SHA256SUMS` 上传到草稿后发布。只有发布作业获得仓库写权限。Linux 继续作为内部 CI 主机，不发布 Linux 安装包。

Release 说明从 中文 `CHANGELOG.md` 和英文 `CHANGELOG.en.md` 对应版本的章节生成中文在前、英文可选的完整更新内容，分别附上按芯片选择的下载表和对应语言的固定版本安装、验收链接。任一文件中的版本章节缺失、空白或重复都会阻止发布。`python3 tools/check_docs.py` 核对文档语言配对、切换导航和本地链接。本地可用 `python3 tools/release_notes.py --tag v0.3.1 --repo OdysseusC/AGI-hub --output dist/release-notes.md` 预览；不要用通用构建说明替代该版本的更新内容。macOS ZIP 使用 `macos-{amd64,arm64}` 命名，内部运行时和校验清单仍使用 Go 的 `darwin` 平台名。

工作流也能以**已有** `vX.Y.Z` 标签手动运行；无论选择哪条分支触发，源码都来自标签提交。已有 Release 或草稿永不覆盖。若上传或发布失败，先检查保留的草稿和运行记录，再决定恢复方式；不要使用 `--clobber`，也不要静默替换已发布文件。协议或配置不兼容必须明确说明；升级时保留尚未核对的投递记录。

Hub、Client、技能和日志版本分别描述不同的兼容边界。已发布技能指令发生变化时，提升受管技能修订号。不要强制所有适配器使用相同版本号，也不要因为构建成功就声称支持未经实测的平台。
