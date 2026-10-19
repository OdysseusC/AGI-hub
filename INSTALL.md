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

开发者在仓库根目录运行 `go run ./tools/package`，得到六个平台的 ZIP 和 `dist/SHA256SUMS`。包内不包含用户配置、凭据或运行记录。源码构建方式仍见安装助手指南。
