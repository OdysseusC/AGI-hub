# AGI Hub

[简体中文](README.md) | [English](README.en.md)

Public documentation opens in Chinese by default. Select **English** above for the full English version.

Start in the native AI assistant you already use, and bring in other assistants when needed.

AGI Hub is a local message sidecar. Each assistant has a collaboration skill and its own Client, which consumes only its own mailbox. Assistants decide how to divide work, choose recipients and combine results; the Hub handles durable messages, addressed delivery, permissions, deduplication and basic limits.

Start from Codex, WorkBuddy or any assistant added later. There is no new chat shell to switch to and no permanent leader. Calling the same model through an API does not connect its native assistant.

- [Hand the package to Codex: start here](START-HERE.en.md)
- [Installation and WorkBuddy onboarding](INSTALL.en.md)
- [Architecture](docs/ARCHITECTURE.en.md) · [Message protocol](docs/PROTOCOL.en.md)
- [Skill updates](docs/SKILLS.en.md) · [Assistant Clients](docs/CLIENTS.en.md)
- [Native acceptance tests and support limits](docs/NATIVE-VALIDATION.en.md)

## How it works

```text
User → A's native session
       A sends messages to B, C and D, each into its own mailbox
       B sends a message to D when it needs D's help
       Each assistant chooses whom to reply to from the context
       A delivers the outcome to the user after receiving the needed results
```

Messages carry necessary context, a correlation ID, the original entry reference and an optional reply reference. The sender always chooses `recipient` explicitly. `root_origin` records the initial human entry, and `reply_to` traces related messages; neither forces replies along a return chain.

The Hub does not create task trees, DAGs, team plans or central intelligent routing. It does not decide who should wait for whom or complete other work automatically when a reply arrives. Skills make those decisions while keeping context and model wakeups small.

## Implemented

| Capability | Behavior |
| --- | --- |
| Native session binding | Each endpoint binds a specific product, device and session; the user chooses whether to enable it |
| Addressed mailboxes | Assistants explicitly send and receive `request / reply / status / result` messages |
| Permissions and directory | Visible members and permitted sending directions are configured separately; communication is disabled by default |
| Reliable delivery | Stable message IDs, claims after persistence and delivery receipts; uncertain delivery is retained without blind retries |
| Original entry context | A collaboration retains the initial session so assistants can choose where to deliver the final result |
| Basic limits | Messages in one correlation share a count limit and deadline; forwarding cannot duplicate the remaining allowance |
| Lower model usage | Status records do not wake a model; the directory is read on demand, without copying full histories or repeated acknowledgements |
| Skill maintenance | Sync only registered managed locations, respecting user edits and uninstall choices and preventing version downgrades |
| Local status page | Inspect mailbox delivery and history without making it a new work entry point |

The legacy one-shot Task/Report interface remains compatible. New mailbox messages do not depend on it or generate business replies automatically.

The original Codex ↔ WorkBuddy sessions on the development machine completed a pure mailbox collaboration: requests, verification replies and an artifact returned automatically to the original entry. The first attempt omitted a message ID; after improving the prompt, one explicit format explanation allowed it to complete. [Actual test and limitations](docs/NATIVE-VALIDATION.en.md)

On 2026-10-20 at 11:48 Beijing time, Claude Code mod `0.2.2` also passed a mailbox round trip in the existing macOS Claude-3p `2.1.299` Code session. MQ woke it from idle; native Read checked one 54-byte synthetic file, and native `mail_send` returned the matching nonce and sum **51** through the Hub. The independent Codex Node Client delivered it by official Desktop MCP send-now into the busy original Codex chat. Both messages were delivered, the original history prefix stayed intact, and no human forwarding, Task/Report or Bash was used. This one business turn made three model API requests with 363 output tokens. [Evidence, usage and diagnostic limits](docs/NATIVE-VALIDATION.en.md)

## Technology and platforms

| Component | Choice |
| --- | --- |
| Hub | Go standard library, one executable, no external Go dependencies |
| Fixed-identity messaging helper | Go executable holding only the corresponding endpoint's credentials, used by skills to send and query |
| Message storage | Local checksummed append-only snapshot journal; messages are persisted before delivery |
| Events | Local HTTP loopback and SSE; no scheduled scanning of chat histories |
| Codex Client | Node standard library; official Desktop MCP targets a specific original chat |
| WorkBuddy Client | Python standard library; official CodeBuddy CLI `--resume` targets a specific original session |
| Claude Code Client | Mod 0.2.2 with native mailbox tools; queue-only Code sessions, verified on the local Claude-3p Mac |
| Installer | Portable Go bootstrap, package checksums and platform-specific launchers |
| Target platforms | Windows and macOS, each with amd64/arm64 packages; native capabilities are accepted separately per platform |

No Redis, RabbitMQ, database, Docker or agent scheduling framework is required. Running the compiled Hub and installer does not require Go. Prepare the native application and runtime required by your selected Client. Each assistant has its own directory and can be maintained and updated independently.

## Packages

Official packages are compiled and checked by GitHub Actions and published in [GitHub Releases](https://github.com/OdysseusC/AGI-hub/releases/latest). Pull requests offer CI build artifacts; version tags trigger publication without a manual build and upload from the development machine.

For a Mac, choose `macos-arm64` for Apple silicon (M series) or `macos-amd64` for Intel. For Windows, choose `windows-amd64` for Intel/AMD or `windows-arm64` for ARM. Each release lists its changes and download instructions directly. See the [installation guide](INSTALL.en.md).

Download the portable ZIP for your platform, extract it fully, and hand the entire directory and `START-HERE.en.md` to Codex. For example:

> Follow START-HERE.en.md in this directory to connect the current Codex chat and local WorkBuddy. Preserve existing configuration, inspect the actual environment first, and guide me through the necessary session and permission confirmations.

The package includes the Hub, installer, fixed-identity helper, Clients and public documentation. The installer copies runtime files to a stable private location. It does not write credentials back into the extracted directory, start a model or enable unverified bindings. It prints a message you can copy to WorkBuddy, then helps obtain actual session information, configure the corresponding Client and grant minimal permissions.

Check installation, skill discovery, idle wakeup, business execution and result return separately. This is not yet a universal one-click connector for all assistants. [Full steps](INSTALL.en.md)

## Minimal message example

First complete installation, session binding and verification of both Clients. Save the message in an authorized workspace of the native assistant:

```json
{
  "id": "saved-message-001",
  "recipient": "workbuddy",
  "kind": "request",
  "content": "Read-only verification of the explicitly provided material. Return a brief conclusion and artifact location. State the user's authorized scope and the files' device/workspace here.",
  "origin_ref": "actual-human-request-reference",
  "max_messages": 16
}
```

Use the installed fixed-identity endpoint helper:

```text
<fixed-client> mail-send --file <absolute-workspace-message.json>
```

The full Hub CLI also requires explicit `--data` and `--endpoint`. The fixed helper's identity is already stored in its private binding; the recipient belongs only in the JSON. Follow-up messages reuse the returned `correlation_id`, with a new message ID, explicit `recipient` and optional `reply_to`. On timeout, keep the original file and ID and check the status first.

## Support limits

The new mailbox path has native acceptance evidence for Codex, WorkBuddy CLI and Claude Code mod `0.2.2`. Claude evidence covers queue delivery to the existing Code session on the tested macOS Claude-3p `2.1.299` host, not all platforms or hosts. Ordinary Claude Chat/Cowork and busy-session steer are unsupported; standalone CLI sessions require separate verification. Grok Bot retains its existing one-shot integration without the new mailbox capability enabled. MCP or webhook acceptance, or a successful exit code, does not prove business completion.

Files currently use explicit workspace references on the same machine. A local path is not automatically made usable on another computer. Accounts, native tool permissions and confirmation of external sends remain under the user and host's control. Messages between assistants cannot expand authorization.

Assistants choose every recipient in multi-hop collaboration. The Hub can limit message counts, but cannot promise to cap all model calls or credits consumed inside a host. Reconcile uncertain deliveries; there is no automatic fallback that retries with a new ID.

## Develop from source

```sh
go test -race ./...
go vet ./...
python3 -m unittest discover -s clients/tests
node --test clients/codex/test_desktop_mcp_client.mjs
go run ./tools/package
```

`dist/` contains four Windows/macOS ZIPs and `SHA256SUMS`. Building requires Go 1.22+. Packages contain actual target-platform executables, so users do not need to compile them. Tests use synthetic adapters; native acceptance evidence is recorded separately in [NATIVE-VALIDATION.md](docs/NATIVE-VALIDATION.en.md).

Code and personal configuration are separate from the start. Credentials, real chats and private artifacts stay outside the repository and packages.
