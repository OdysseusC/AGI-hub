# 安装 AGI Hub

推荐下载本机平台的便携 ZIP，完整解压，把目录和 [START-HERE.md](START-HERE.md) 交给 Codex。首次安装、WorkBuddy 接入消息和原生验收统一见[安装助手指南](docs/INSTALL-FOR-ASSISTANT.md)。

| 系统 | CPU | 安装包后缀 |
| --- | --- | --- |
| macOS | Apple Silicon | `darwin-arm64.zip` |
| macOS | Intel | `darwin-amd64.zip` |
| Windows | Intel / AMD 64 位 | `windows-amd64.zip` |
| Windows | ARM64 | `windows-arm64.zip` |
| Linux | x86-64 | `linux-amd64.zip` |
| Linux | ARM64 | `linux-arm64.zip` |

安装器与 Hub 只依赖操作系统。所选助手的官方 CLI、运行时、登录和原生工具权限分别核对；某平台构建成功不表示该平台的原生助手接入已经实测。

升级前先确认原生执行已结束，再停止旧 Hub 和 Client，保留全部消息记录后替换程序。`0.2.0 / 0.2.1` 与 `0.2.2` 的进程锁不同，不支持混跑；不要修改端口来绕过已有实例。同一 API 端口已被占用时，CLI 会在加载日志前拒绝启动。`0.2.2` 及后续版本使用系统文件锁保护同一数据目录，锁文件由程序保留，不手动删除。

正式安装包由 GitHub 的 **Release** 工作流编译、检查并发布；下载对应版本的 ZIP，可用同页 `SHA256SUMS` 核对。PR 和 `main` 的检查运行也提供 `portable-packages` 临时构建产物，保留 14 天，适合提前验证。

本地构建仍可在仓库根目录运行 `go run ./tools/package`，得到六个平台的 ZIP 和 `dist/SHA256SUMS`。包内不包含用户配置、凭据或运行记录。发版操作见 [CONTRIBUTING.md](CONTRIBUTING.md)，源码构建方式仍见安装助手指南。
