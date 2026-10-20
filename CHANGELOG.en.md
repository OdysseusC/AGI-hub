# Changelog

[简体中文](CHANGELOG.md) | [English](CHANGELOG.en.md)

## 0.3.1 — 2026-10-20

- Default public documents to Chinese, with complete English versions in `*.en.md` and Chinese-first language navigation.
- Show Chinese changes and downloads first on Releases, followed by a full optional English section, both drawn from version-specific changelogs.
- Keep both languages in packages while installing only one English model skill; update navigation references and its managed revision without changing message protocols or native integration capabilities.

## 0.3.0 — 2026-10-20

- Add Claude Code mod `0.2.2` mailbox tools for member discovery, explicit message sending and scoped state queries, with fixed endpoint/session binding and a consumer lease. Support is Code-only and queue-only, without busy steer or ordinary Chat/Cowork integration.
- When activation options are absent, read only this plugin's own `client_config` path from official merged settings. Explicit options take priority; conflicting configured paths stop the Client rather than selecting one.
- Pass the native mailbox round trip on the local macOS Claude-3p `2.1.299` original Code session at 11:48 Beijing time: idle wake, native Read of a 54-byte synthetic file, explicit native `mail_send`, and independent Desktop MCP send-now return to the busy original Codex chat. Two delivered messages carried the matching nonce and sum 51, without human forwarding, Task/Report or Bash. This is machine-specific evidence, not all-platform acceptance; see the [native record](docs/NATIVE-VALIDATION.en.md) for usage and preliminary diagnostics.
- Provide separate complete English and Simplified Chinese public documentation, with English as the default and language-switch links throughout. Preserve historical acceptance evidence and support limits.
- Generate full English and Chinese Release notes from both version-specific changelog sections, with language-specific installation and acceptance links. Missing, empty or duplicate sections in either language block publication.

## 0.2.3 — 2026-10-19

- Focus official packages on Windows and macOS, each with amd64 and arm64 builds. Linux packages are no longer published.
- Use clearer `macos-*` download names: choose `macos-arm64` for Apple silicon or `macos-amd64` for Intel. The internal `darwin` platform identifier remains compatible.
- Generate Release pages with that version's changes, download and selection instructions for all four packages, and installation and acceptance scope. Block publication when the corresponding version section is missing.
- Verify checksums for all four packages during GitHub publication and run the packaged programs on macOS to verify their version. The message protocol, personal configuration and journal format are unchanged.

## 0.2.2 — 2026-10-19

- Use operating-system file locks for data directories, avoiding random Hub startup failures caused by reserved Windows ports. The operating system releases the lock on process exit, without polling or deleting a lock file.
- Preserve the failed release-check record for `v0.2.1`. Resume official package publication with this version without moving existing tags or overwriting release files.

## 0.2.1 — 2026-10-19 (release checks failed)

- Automatically check pull requests and the main branch through GitHub Actions, providing downloadable portable build artifacts for six platform/architecture targets.
- Trigger compilation, verification and Release publication from version tags. Require tags to come from main's history and match the program version; never overwrite existing release files.
- Reuse cross-platform checks and artifacts from the same run, without compiling and uploading manually from the development machine.

## 0.2.0 — 2026-10-19

- Add skill-driven addressed mailboxes: assistants explicitly send requests, replies, status and results, choosing permitted recipients from context. The Hub creates no task tree or forced return chain.
- Retain original entry context and share a message limit and deadline within one collaboration. Deduplicate stable IDs, preserve uncertain deliveries, serialize native channel use and block misdelivery when the original entry is rebound.
- Enable the mailbox protocol independently in Codex and WorkBuddy Clients. Status messages do not wake a model; legacy task APIs remain compatible. Claude and Grok do not yet enable the new mailbox protocol.
- Add portable packages for six platform/architecture targets, an offline verification installer, a fixed-identity messaging helper, and onboarding instructions for Codex and WorkBuddy. Keep personal configuration outside packages.
- Add CI across three operating systems, offline multi-assistant mailbox acceptance tests and a feature-branch contribution process.
- Fix Windows file access permissions for truncated-journal recovery and detection of a stopped local service during offline skill installation.

See the [native validation record](docs/NATIVE-VALIDATION.en.md) for actual native integration acceptance scope and limitations.
