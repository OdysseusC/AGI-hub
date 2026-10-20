[English](ARCHITECTURE.md) | [简体中文](ARCHITECTURE.zh-CN.md)

# Architecture

AGI Hub is a local sidecar for addressed messages. Users start work in any connected native assistant; each assistant handles its own mailbox and chooses collaborators and reply recipients. The Hub does not perform intelligent planning or team scheduling.

## Three parts

| Part | Responsibility |
| --- | --- |
| Collaboration skill | Decide when to ask for help, provide necessary context, choose recipients, combine replies, verify and deliver results |
| Hub | User-configured directory and allowed recipients, persistence, addressed claims, deduplication, transport receipts and conversation limits |
| Per-assistant Client | Listen to its own mailbox, deliver and wake through actual native interfaces, and retain real transport evidence |

A Client can be a separate process or an in-product plugin. Installing an API for the same model, MCP tools or a skill does not prove that the corresponding native session is connected.

## Messages

```text
A → B / C / D mailboxes
B → D mailbox
D → B or A mailbox (D explicitly chooses)
A combines the necessary messages → user
```

Each message has a stable ID, sender, recipient, purpose, necessary body and correlation ID. `root_origin` preserves the initial human entry as context; `reply_to` links the message being answered. Neither forces a return path. Assistants may explicitly select other permitted recipients.

The Hub has no parent/child tasks, dependency graph, waiting conditions or overall completion state. New mailbox messages do not create Tasks, require Reports or generate replies automatically from business status. Assistants verify business completion and result quality, send them explicitly and ultimately explain them to the user.

`request / reply / result` can enter native sessions through a Client; `status` is recorded and acknowledged by the Client without a separate model call. Messages contain only necessary summaries and file references. Skills fetch the directory and versions when needed rather than repeatedly injecting history.

## Native execution and capacity

The Hub persists a pending message as dispatching before returning it to the Client. While a mailbox has a dispatching or uncertain message, it does not allocate another execution message.

The WorkBuddy Client serially invokes the configured official CLI with the original `--resume` session, working directory and configuration directory. It confirms transport only after the process exits and its result matches the session. A business assistant's status message cannot release this transport reservation early, so no parent-task waiting state or additional settled protocol is needed.

Claude Code Client 0.2.2 runs inside the bound original Code session. It negotiates mailbox long polling and lease fencing, then uses native `peers`, `mail_send` and `mail_state` tools with fixed identity; no Bash or automatic business Report/reply is needed. It supports queue only, not busy steer. On 2026-10-20, the existing macOS Claude-3p Code session passed idle mailbox execution and an explicit result return to the busy original Codex chat. Other hosts and Claude busy queuing still require acceptance. See the [native record](NATIVE-VALIDATION.md#2026-10-20-pure-mailbox-original-claude-code-session-and-codex-return).

The Codex Client delivers directly to the bound original chat through official Desktop MCP. An input ACK means acceptance of input, not that the existing turn ended or the business work completed. Busy input must be configured and verified separately; falling back to native queue cannot be presented as guidance.

This constrains Client delivery and native-session capacity only. Assistants must still coordinate writes to a shared project or desktop explicitly. Receiving a message does not authorize concurrent writes to the same target.

## Permissions and recovery

The Hub listens only on a numeric loopback address by default and checks Host/Origin. Admin and endpoint credentials are separate. Directory `visible_peers` and send permission `allowed_peers` are configured independently and default to empty. Clients and scoped executables hold only their own credentials; messages cannot change their identity or native destination.

An identical ID and body is idempotent; a changed body under the same ID is rejected. A failure that may already have delivered remains uncertain: no blind resend, new ID or new session. After a restart, the Hub retains leftover dispatching messages as uncertain for Client reconciliation. It does not poll chat history to guess business completion.

Each conversation record contains only the fixed original entry, participants, total message allowance, deadline and closed flag for provenance, permissions and reliability checks. It does not manage business plans. Branches share one message counter; forwarding cannot duplicate the allowance. Expiration or an exhausted allowance stops new messages, without claiming to limit every model call or credit used inside a host.

The original entry can close a conversation and withdraw unsent messages. Running or uncertain native actions retain their actual state; the Hub cannot falsely mark them stopped. A human-input reference is traceability data, not cryptographic proof of human authorization. Assistants remain bound by the original user scope and host permissions.

## Storage and platforms

The Go standard library provides local HTTP, SSE and an append-only snapshot journal. Records have SHA256 checksums and are synchronized after commit. An operating-system file lock protects the same private data directory; its file is never removed and the OS releases ownership when the process exits. New mailbox fields use a recognizable journal version so old programs cannot silently discard them and overwrite records. Checksums detect corruption, not processes with the same user's file permissions. Stop the old Hub and Clients before an upgrade; see [installation](../INSTALL.md).

A snapshot is currently limited to 4 MiB and the journal to 128 MiB. Long-term archiving and cross-computer message/file transport are not implemented. File references must identify their device and workspace; another computer cannot use a local path directly.

The Hub, installer and scoped executable are published in four Windows/macOS packages: amd64 and arm64 for each OS. Mac download names use `macos`; Go's internal platform identity remains `darwin`. Linux can still be used for internal builds and offline checks, but has no release package. Native product support, retained tools and idle/busy wake behavior are tested separately; a successful build does not verify native integration on every system.

## Skills and installation

The installer maintains only user-registered locations. It leaves identical files untouched, preserves user edits and does not reinstall a manually removed skill. Skill revisions and manifest format versions are recorded separately; downgrades are rejected. Disk version and the version loaded by an existing session are separate facts.

Give `START-HERE.md` directly to Codex so it can inspect the machine, install stable paths, prepare a WorkBuddy connection message and guide session/minimum-permission checks. The installer does not enable unverified bindings or start paid models.

The old single-handoff Task/Report path remains independent compatibility functionality. Its legacy leader is only that interface's default entry and does not control mailbox messages. See the [protocol](PROTOCOL.md), [collaboration conventions](TEAM-ORCHESTRATION.md) and [skill maintenance](SKILLS.md).
