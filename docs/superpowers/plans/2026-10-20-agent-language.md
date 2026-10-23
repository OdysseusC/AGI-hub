# Agent 协作语言实施计划

[简体中文](2026-10-20-agent-language.md) | [English](2026-10-20-agent-language.en.md)

> 执行方式：使用 superpowers:subagent-driven-development 分工，按测试失败、实现、复测推进。

**目标：** 将用户选择的 Agent 语言贯通管理页、目录、消息和已安装技能，不调用翻译模型。

**架构：** `internal/language` 负责纯标签校验和语言选择；Hub 保存配置、传播目录并携带提示；发送 Agent 负责翻译。受管技能采用固定模板和动态策略文件。

**技术：** Go 标准库、现有 Node/Python Client、无框架管理页。

## 1. 语言规则与持久化

- [ ] 在 `internal/language/language_test.go` 写默认及优先级失败测试：
  `Resolve("codex", "zh", "claude-code", "") == "en"`；
  `Resolve("codex", "auto", "claude-code", "zh") == "zh"`。
- [ ] 实现 `Normalize`、`Effective`、`Resolve`，测试非法标签与规范化。
- [ ] 在 `internal/manager/protocol.go` 增加端点和消息字段，注册及配置加载时校验；消息发布保留语言提示，重试不因新配置改变。
- [ ] 在 `internal/app` 测试再实现管理员语言保存、目录字段、重启和回滚；同步冲突明确报告。

## 2. 跨机与原生通知

- [ ] 扩展 `internal/mesh.Agent` 签名目录字段及边界校验；测试远端自定义 Claude endpoint 的语言传播。
- [ ] 在 `internal/app/network.go` 公布语言并为远端发送解析目标规则；网络消息只携带小型语言提示，保持单份正文。
- [ ] 三种 Client 通知提醒遵循消息语言、发送前读规则、面向用户按用户语言，不增加消息或自动重试。
- [ ] 运行 `go test ./internal/language ./internal/manager ./internal/app ./internal/mesh` 及对应 Node/Python 测试。

## 3. 动态技能

- [ ] 以临时配置和技能目录验证策略变化显示 outdated，旧清单升级，冲突和卸载不覆盖。
- [ ] 在 `internal/install` 生成受管 `communication-policy.md`，复用既有安装锁与清单；固定技能修订升为 7。
- [ ] 更新中英文技能及 `docs/SKILLS`；验证源版本保护和策略更新互不冲突。
- [ ] 运行 `go test ./internal/install`，使用 Codex 验证场景理解，不调用 Claude。

## 4. UI、文档与交付

- [ ] 在 `dashboard.test.cjs` 先测试中文、自定义标签、正确保存载荷、SSE 输入保留和安全显示，再实现控件。
- [ ] 更新 LAN 与版本文档的双语说明；浏览器检查桌面及窄屏。
- [ ] 执行全量 Go/Node/Python、文档检查和离线冒烟，复核独立审查结果。
- [ ] 提交到已有 feature 分支，更新 PR #9，检查 CI；不合并、不替换当前运行安装。
