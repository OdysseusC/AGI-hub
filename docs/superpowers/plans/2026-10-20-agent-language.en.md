# Agent collaboration-language implementation plan

[简体中文](2026-10-20-agent-language.md) | [English](2026-10-20-agent-language.en.md)

> Execution: use superpowers:subagent-driven-development with failing tests, implementation and verification.

**Goal:** Carry user-selected agent languages through management, directories, messages and installed skills without translation-model calls.

**Architecture:** Pure `internal/language` helpers validate tags and resolve preferences. Hub persists settings and propagates directory and message hints. Sending agents translate. Managed skills combine a static template with a dynamic policy file.

**Stack:** Go standard library, existing Node/Python Clients, framework-free dashboard.

## 1. Rules and persistence

- [ ] Add failing default/precedence tests in `internal/language/language_test.go`:
  `Resolve("codex", "zh", "claude-code", "") == "en"` and
  `Resolve("codex", "auto", "claude-code", "zh") == "zh"`.
- [ ] Implement `Normalize`, `Effective` and `Resolve`, testing invalid tags and canonicalization.
- [ ] Add endpoint/message fields in `internal/manager/protocol.go`, validate registration/configuration, and persist message hints without changing retries when settings change.
- [ ] Test then implement administrative saves, directory fields, restart and rollback in `internal/app`; report synchronization conflicts explicitly.

## 2. LAN and native notifications

- [ ] Extend signed `internal/mesh.Agent` directory fields and bounds; test remote custom-named Claude endpoints.
- [ ] Advertise preferences and resolve remote send rules in `internal/app/network.go`; carry a small hint without duplicating body content.
- [ ] Update three Client notifications to follow message language, reread rules before sending, and respect the human's language without automatic messages or retries.
- [ ] Run `go test ./internal/language ./internal/manager ./internal/app ./internal/mesh` and corresponding Node/Python tests.

## 3. Dynamic skills

- [ ] Use temporary configurations and skill directories to test policy-only outdated state, old-manifest upgrades, and edit/uninstall protection.
- [ ] Generate managed `communication-policy.md` in `internal/install` using existing locks/manifests; advance static skill revision to 7.
- [ ] Update bilingual skills and `docs/SKILLS`; verify source-version protection remains independent of policy changes.
- [ ] Run `go test ./internal/install` and check scenario comprehension using Codex, without Claude calls.

## 4. UI, documentation and delivery

- [ ] Add failing `dashboard.test.cjs` cases for Chinese, custom tags, save payloads, unsaved-input retention and safe rendering before implementing controls.
- [ ] Update bilingual LAN and version documentation; inspect desktop and narrow layouts in a browser.
- [ ] Run full Go/Node/Python, documentation and offline smoke checks, and address independent review findings.
- [ ] Commit on the existing feature branch, update PR #9 and inspect CI; do not merge or replace the running installation.
