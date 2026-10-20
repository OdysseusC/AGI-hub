[简体中文](2026-10-20-lan-manager.md) | [English](2026-10-20-lan-manager.en.md)

# 局域网管理版本实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** 在独立分支交付可离线验证的跨 Hub 邮箱、显式自发现和本机管理页，并提交未合并 PR。

**Architecture:** `internal/mesh` 管理身份、配对、持久传输和有限中继；`internal/discovery` 管理 mDNS/DNS-SD；`internal/app` 将网络收发接到本机邮箱并提供有限管理 API。原生 Client 保持本机入口。页面通过本机 API/SSE 更新。

**Tech Stack:** Go、标准库网络/加密/存储；嵌入式 HTML/CSS/JavaScript；Go 与 Node 离线测试；原有 Python/打包检查。

---

### 1. 基线与协议

- [x] 从核对过的 `origin/main` 建立 `feat/lan-hub-manager` 工作区；保存已批准设计。
- [ ] 运行 `go test ./...`，记录基线。
- [ ] 固定跨机 `hub-id/endpoint-id` 格式及消息来源、关联、期限、文件归属；与本机旧格式共存。

### 2. 身份、配对、长连接和中继

**Files:** 新增 `internal/mesh/*.go` 与同目录测试。

- [ ] 先写失败测试：重启保留 Hub ID；未知公钥不能投递；双方配对确认后才连接。
- [ ] 实现私有身份持久化、默认关闭设置、短期邀请、待确认配对、固定公钥校验。
- [ ] 先写失败测试：三个临时 Hub 传递同名 endpoint 消息；移除直连后经第三个 Hub 送达。
- [ ] 实现 TLS 长连接、目录同步、有限路径防环、过期路线、断线退避重连和显式断开。
- [ ] 先写失败测试：重启待发送队列仍可交付，相同消息去重、同 ID 内容冲突拒绝，中继修改签名内容失败。
- [ ] 实现发送日志与目的端持久去重，区分目的 Hub ACK 和原生投递。
- [ ] `go test ./internal/mesh` 与 race 检查可用时通过。

### 3. 显式自发现

**Files:** 新增 `internal/discovery/*.go`、解析/生命周期测试。

- [ ] 先写失败测试：PTR/SRV/TXT/A 公告 roundtrip、压缩名称边界、畸形包拒绝、过期节点移除。
- [ ] 实现 `_agihub._tcp.local` 查询/公告、选定接口、停止公告与 goodbye、手动地址独立可用。
- [ ] 默认不启动 UDP socket；网络心跳与发现不读取聊天历史，不调用模型。
- [ ] `go test ./internal/discovery` 通过，提供可注入 socket/时钟的离线生命周期检查。

### 4. 本机邮箱与受限管理 API

**Files:** `internal/app/network.go`、`internal/app/management.go`、`internal/manager/mailbox.go`、`cmd/agi-hub/main.go` 及相关测试。

- [ ] 先写失败测试：qualified 收件人走 mesh，本机收件人保持现行为；目的 Hub 校验共享 endpoint 后持久存入原有邮箱。
- [ ] 保持关联与 `root_origin`，禁止远程身份冒充本机发送者；远程文件引用不按本机路径校验。
- [ ] 管理 API 只接受枚举动作、固定 endpoint 和严格 JSON；保留 loopback、Host/Origin 和管理员鉴权。
- [ ] 封装受管技能安装和配置引导，明确宿主步骤；启用前要求真实绑定，禁用不清理不确定投递。
- [ ] SSE 发布网络与管理变化；不会自动发送任何原生测试。
- [ ] 完成三 Hub 传输测试，以及双 Hub + 模拟 Client 的端到端离线测试。

### 5. 管理界面

**Files:** `internal/app/ui.go` 及嵌入式资源，浏览器逻辑测试。

- [ ] 将页面拆为本机/网络/消息三块，显示显式发现开关、发现/配对/在线计数、主机下 agents。
- [ ] 实现安装/绑定引导/启停、邀请/配对确认/断开/手动地址、错误提示和进行中状态。
- [ ] 状态只使用真实 API 数据，不硬编码成功；正文与显示名使用安全文本渲染。
- [ ] 本机浏览器验证首次空状态、列表、按钮、错误状态和窄屏；传输/安装不触发模型。

### 6. 集成、文档与 PR

- [ ] 规格审查与独立代码审查，修正再核对。
- [ ] 中文/英文文档、变更记录、配置示例和升级说明同步，记录真实跨机器验收边界。
- [ ] 运行 `go test ./...`、`go vet ./...`、原有 Node/Python 测试、文档检查及便携打包/邮箱冒烟。
- [ ] 功能分支提交并推送，创建 PR、附到当前聊天，查看 checks；不自动合并。
