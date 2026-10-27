# Changelog

[简体中文](CHANGELOG.md) | [English](CHANGELOG.en.md)

## 0.5.0 — 2026-10-27

- Add optional user-login startup, off by default: a current-user LaunchAgent on macOS and a current-user logon task on Windows. Installation and upgrades do not enable it automatically; Linux remains CI-only for this feature. Registration and saving startup switches affect the next login without starting or stopping current processes.
- Add `autostart status/enable/disable/register-client/remove-client/run`, a management startup switch and registered-Client checkboxes. Register absolute executables, separate arguments and working directories through the local CLI. New registrations start unselected; omitting `--clients` on `enable` preserves choices, while `--clients ''` clears them. The web interface accepts no executable commands or private configuration paths.
- Show configuration, installation, binding and language progress, results and errors inside the relevant agent card. Preserve confirmed action results when a later state refresh fails and ask for verification before retrying. Grok shows concrete Routine setup instructions without installing, enabling or connecting anything.
- Upgrade Grok Routine Client to `0.2.0` with queue mailbox protocol 1. Status messages do not trigger the webhook; HTTP 2xx records delivery only, and uncertain timeouts or receipts are not retried. Setup accepts an explicit `--host grok` workspace, actual Client configuration and fixed-identity helper while preserving private edits. Offline protocol checks pass; real Routine receipt, native execution and replies remain pending. The actual old Client must be upgraded.
- Authenticate the local Hub before starting selected consumers. Attach to an existing Hub without taking ownership. The runner's own Hub and each failed Client receive at most an initial start plus five retries after 1/2/4/8/16 seconds, retaining failure after exhaustion. No unlimited OS restart policy or redelivery of uncertain messages is introduced.
- Report system login settings separately from live process state. Failed queries show an unknown setting while preserving the actual runtime observation. Windows queries use .NET calls without PowerShell module discovery, retaining the restricted startup environment.
- WorkBuddy/Grok can register their actual standalone consumer entry points. Codex/Claude retain a forced native-host requirement and report `needs_host_environment` instead of being launched directly by the Go runner. Native IPC, pipe values, sign-in credentials and model keys are not saved. Authentication, native permissions and network gates remain in force. See the [startup guide](docs/AUTOSTART.en.md).
- Add an open-remote-management button and a separate management grant to paired-host cards. Grants default to off; pairing or agent sharing does not authorize administration. The browser stays on the current Hub's loopback page, while pinned mutually authenticated TLS connects directly to the target. The target's administrator token never enters the LAN or browser.
- Limit remote access to state, existing-agent management and registered startup selections. Exclude remote network settings, pairing/grant operations, recursive proxies, arbitrary URLs and relays. Recheck grants for each request, including reused connections; revocation takes effect and fresh pairing does not inherit access. Remote pages use manual refresh without remote SSE. See the [LAN guide](docs/LAN.en.md).
- Platform definitions, process substitutes, bounded retries, ownership, management authentication, two-Hub loopback TLS and web checks are offline verification without real-assistant calls. Physical-computer reboot, user login, remote management and new native-session lifecycles remain unaccepted. Earlier native round trips do not establish this version's recovery capability.

## 0.4.0 — 2026-10-26

- Add opt-in LAN Hub communication with stable host identities, explicit pairing and agent exports, reusable TLS connections, direct-first routing and bounded relays. Routing does not propagate trust.
- Discover Hubs with mDNS/DNS-SD on a selected interface, or connect through invitations and a manual address. Disabling discovery preserves paired connections.
- Add a local management page for managed skill installation, real session binding, enable/share controls, Hub/agent trees, pairing and mailbox state. Management and offline testing invoke no models.
- Federated mail preserves the original local binding, uses canonical message IDs, a durable outbox and deduplication, and distinguishes destination-Hub receipt from native execution. Protocol 1 fixes each conversation to an endpoint pair with disjoint message permits; files are not synchronized.
- Use receiver-local directory leases and signed persistent revisions so replayed old directories cannot renew their lease. Invitation expiry is checked by its issuer, without manual clock alignment between computers. Internal mesh/discovery protocol advances to 2, requiring both paired Hubs to update; mailbox protocol remains 1.
- Advance managed rules to revision 7. The first cross-host message upgrades the journal to version 4, which older binaries cannot read. Back up complete private data before upgrading; rollback requires a matching binary and backup. See the [LAN guide](docs/LAN.en.md).
- Claude Client 0.2.3 recognizes acknowledgements for canonical LAN message IDs. Native prompts contain one copy of the content, omitting duplicate network payload tokens. Installation and verification did not invoke Claude.
- Select each agent's collaboration language in management. Claude defaults to English; Chinese and other languages are selectable. Saving synchronizes managed skill policies. Sending agents translate necessary instructions, Hub calls no translation model, and human-facing replies follow the user's language.
- On October 23, isolated macOS arm64 and Windows 11 Hubs completed a native cross-host Codex round trip: a request from the original Mac chat automatically woke the idle original Windows chat, which executed the task and explicitly returned a result into the busy original Mac entry. The synthetic result, nonce and message correlation matched, using 2/2 messages without manual forwarding, queue clicks or Claude calls. See the [native acceptance record](docs/NATIVE-VALIDATION.en.md).
- Recommend independent interactive Claude Code as the integration host. Code 2.1.308 on macOS arm64 passed plugin validation, 9/9 host plugin tests and 50/50 Node state-machine tests without model calls. Native mailbox acceptance in the independent CLI still awaits sign-in and testing.
- This version still requires manual Hub and Client startup; optional autostart is planned for the next version. Windows busy reception, real multi-computer discovery, sleep recovery, long-running automatic state tracking and unattended recovery remain unaccepted. Other assistants' cross-host business tools are outside this Codex test's scope.

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
