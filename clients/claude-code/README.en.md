# Claude Code Client

[简体中文](README.md) | [English](README.en.md)

An independently installed, interactive Claude Code terminal is the recommended host. Reuse the existing in-session mod to consume its addressed inbox and expose native tools for explicit outgoing messages; no Claude Desktop wrapper, Hub change or additional adapter is needed. It preserves that Code session's own project, login, tools and permissions, without promising to inherit Desktop-specific capabilities. Hub routes messages; the assistant chooses the recipient and decides when to reply.

Client **0.2.3** supports mailbox protocol **1** and the legacy single-task protocol. It requires Claude Code **2.1.299 or later with the mods APIs used here**. Verify native support for each host/version/platform; a portable Hub build alone does not verify the native integration.

Version 0.2.3 supports canonical LAN message IDs: only qualified remote addresses accept `lan-…` acknowledgements carrying the original `source_message_id`; local acknowledgements still require the original ID. Hub omits duplicate network payloads from native prompts to avoid paying tokens for the same content twice. This version is validated offline and does not expand the historical 0.2.2 native acceptance below.

## Support boundary

| Host or operation | Scope |
| --- | --- |
| Independently installed interactive Claude Code CLI | Recommended entry; reuse the same mod, but verify native wake and the mailbox round trip in that CLI session separately |
| Local Code tab in Claude Desktop / Claude-3p | Retained compatibility entry when that host loads the required mods; verify its actual bundled version |
| Ordinary Desktop Chat / Cowork | Not supported by this Code plugin |
| `claude -p --resume` | Noninteractive path; this Client does not use it as the running session's inbox transport, and slash commands do not guarantee zero model usage |
| Queue | The host starts a turn when idle or waits for idle when busy; this release keeps Hub busy-mailbox receipt flags false pending separate acceptance |
| Steer / interrupt / WSL | Not claimed |

On **2026-10-20, 11:48:16–11:48:40 Beijing time**, Client 0.2.2 passed the mailbox round trip in an existing macOS Claude-3p 2.1.299 Code session: MQ automatically woke the idle original session, Claude used native Read on one 54-byte file, and one explicit native `mail_send` returned the matching nonce and sum **51**. The independent Codex Node client used the official Desktop MCP send-now path to deliver that result into the original, already-busy Codex chat. No human forwarding, Bash or legacy Report was used. This business trial used one native turn containing **three model responses**. See the [full native validation record](../../docs/NATIVE-VALIDATION.en.md).

Native wake and a mailbox round trip in the independent interactive CLI, Claude busy-session queueing, steer/interrupt, a new collaboration originating from a human input in Claude, and Windows native integration remain unverified. Ordinary Chat/Cowork remains unsupported. Older legacy-task checks are recorded separately in the same validation document.

On 2026-10-23, independent macOS arm64 Claude Code **2.1.308** passed Client 0.2.3's official `plugin validate` (author/icon metadata warnings only), `plugin test` **9/9**, and Node state-machine tests **50/50**. One plugin test still asserting the previous Client version was corrected. These model-free checks establish offline plugin compatibility, not native mailbox acceptance in this CLI. See the [official mod host support](https://code.claude.com/docs/en/plugins/mods/overview#where-mods-run).

## Install and bind

First obtain an independent Claude Code executable with the required mods APIs, and keep endpoint tokens and configuration outside this repository. Replace `claude` below with the actual executable when it is not on PATH. A Desktop-bundled executable is not an independent installation, and its path may change after an application update.

1. Install the local marketplace and plugin:

   ```sh
   claude plugin marketplace add /absolute/path/to/agi-hub/clients/claude-code
   claude plugin install agi-hub-endpoint@agi-hub-local
   ```

2. Reload the plugin in the target **Code session**, then run `/agi-hub-id`. Bind that UUID to a private Hub endpoint with `driver: "claude-code"`, external driver type, `modes: ["queue"]`, `mailbox_protocol_version: 1`, and `mailbox_while_busy_verified: false`. Keep the legacy experimental peer protocol at `0`. Configure allowed senders/recipients and member visibility explicitly; visibility alone grants no send permission. Start with native wake verification flags false.

3. Export the scoped client configuration, then add the `native` section from [client.example.json](client.example.json). Native session, Hub binding and client configuration session IDs must agree.

   ```sh
   agi-hub client-config --endpoint claude --file /absolute/private/path/client.json
   claude plugin configure agi-hub-endpoint@agi-hub-local --values-stdin
   ```

   Supply `{"client_config":"/absolute/private/path/client.json"}` to the second command. Plugin settings contain only this path. Nonempty activation options take priority; hosts that omit them read only this plugin's own path from merged settings. Distinct nonempty paths under the bare or qualified forms of the same manifest name require explicit options; the client stops instead of choosing one. Use absolute local paths, numeric loopback URLs and regular configuration files; symbolic links are rejected. The exporter sets private file permissions; the mod cannot inspect permission bits through its SDK. Long polling defaults to 25 seconds (`1000..25000` ms); the prompt limit defaults to 1 MiB (`1024..1048576` bytes).

4. Install the collaboration skill with the actual installed Hub executable and this Hub's private data directory, as described in [skill installation](../../docs/SKILLS.en.md):

   ```sh
   /absolute/path/to/agi-hub install-skill --host claude --endpoint claude --data /absolute/private/agi-hub --binary /absolute/path/to/agi-hub
   ```

   Replace the example paths and, if different, endpoint name with the actual binding. On Windows, use the actual `.exe` and PowerShell invocation shown in the skill guide. The portable `agi-hub-setup` currently handles Codex/WorkBuddy only; Claude uses the Hub's `install-skill` command above. In the native host, allow only tools needed for this binding: `mcp__agi-hub-endpoint__mail_send`, `mcp__agi-hub-endpoint__peers`, and `mcp__agi-hub-endpoint__mail_state`. Outgoing Claude messages do not need Bash. `mcp__agi-hub-endpoint__report` serves legacy tasks only.

5. Reload the target session and run `/agi-hub-status`. Confirm **`client_version: "0.2.3"`** and `state: "running"`. The code is `listening`, or `confirm_hub_session_idle` while Hub session state is still unknown; continue with the checks below. The installed plugin index does not prove which code an existing session has loaded. An unconfigured plugin stays dormant; sessions whose UUID does not match never claim messages. Hub must advertise 25-second mailbox long polling and client-lease enforcement; an older Hub is rejected before claiming.

6. Verify idle wake **before** enabling mailbox delivery. Reuse already-checked evidence for the same host and transport when applicable. Otherwise, with the user's authorization and budget, run `/agi-hub-diagnose` once in the original Code session, leave it idle, and confirm that one plugin message starts a real new turn returning the matching nonce. This schedules one native prompt after 30 seconds and consumes a model turn, which can involve multiple paid API requests; it is not a zero-cost command or an MQ test. Do not repeat it merely to refresh existing valid evidence.

   Only after this native wake proof, set the binding's `idle_wake_verified: true` in private Hub configuration and restart the correct Hub while preserving its data. Keep busy-capability flags false. Confirm that the actual target session is idle, then record that observation with the administrative Hub command:

   ```sh
   /absolute/path/to/agi-hub session --data /absolute/private/agi-hub --endpoint claude --state idle
   ```

   The plugin does not change administrative session state. If `/agi-hub-status` still shows `confirm_hub_session_idle`, reload the plugin and let its lease/reconnection settle so it reads the recorded state. Confirm `running` / `listening`, then run one minimal authorized MQ request and an explicit native `mail_send` reply, verifying the matching nonce and return at the original entry. This is a separate model-consuming test; a diagnostic alone does not prove the mailbox round trip. Never set the wake flag just to bypass missing native evidence.

## Terminal process and session

The ordinary interactive Code process must stay running for its mod to listen and submit turns while idle. After the terminal exits, this mod does not restart Code at the operating-system level. Reopen the chosen session with normal interactive `claude --resume <verified-session-UUID>`, rather than using `-p` to force a continuation into a running session. `/agi-hub-id`, the Hub binding and Client configuration must name the same UUID. If a new session, clear or fork produces a different UUID, the plugin stays dormant instead of rebinding automatically.

Keep only one host open for the same UUID. Desktop and CLI instances of the same session compete for the Hub lease; the instance without the lease does not claim messages. Exit the original host before normally resuming in another. After updating plugin files, reload in the target process and check the actual Client version with `/agi-hub-status`; installed files alone do not prove activation. After the user presses Stop, the host rejects new background turns until the user provides input again. The Client does not bypass this restriction or promise automatic recovery. Preserve records of claimed messages whose native submission is unconfirmed and reconcile them before any resend.

## Addressed messages

Call `mail_send` directly with a saved stable ID. For an existing collaboration, retain its correlation ID and choose a permitted recipient. `reply_to` is an advisory reference; it does not impose a return chain. Explicitly address the verified final result to the original human entry from `root_origin`.

```json
{"id":"saved-reply-01","recipient":"codex","kind":"reply","content":"Verified result; device/workspace-relative artifact reference.","correlation_id":"received-correlation-id","reply_to":"received-message-id"}
```

New root messages may omit `origin_ref`: the tool then records the actual calling main turn as `claude-turn:<turnId>`. This is a context reference, not proof of human authorization. Existing correlations inherit their original entry from Hub. The tool fixes endpoint, session and lease; it never accepts these from the model or generates a missing message ID. Assistants' messages cannot extend the human's authorization. Keep summaries short and identify the owning device/workspace for every file.

`peers` reads the scoped configured directory. `mail_state` accepts only `correlation_id` and returns this endpoint's message metadata and its own saved status content. Receiving `kind=status` writes a delivery receipt without starting a model turn. Hub and client never manufacture business replies, plans, Tasks or Reports for mailbox messages. A published message, native input receipt and verified business result are distinct events.

## Delivery and recovery

The plugin waits on `/v1/mail/inbox` with `wait_ms=25000`; legacy bindings use only `/v1/inbox`. It uses no SSE parser, shell, extra daemon or model API. Hub client leases last 60 seconds and renew every 15 seconds. A second owner waits without claiming; reload waits for an old unreleased lease to expire. Requests, callbacks and native turn observations are isolated by binding generation.

`prompt.submit` returning text is insufficient delivery evidence. Only a real main-session `turn.start` with the submitted marker and a nonempty native turn ID confirms delivery. The marker must lead the prompt or use the known native plugin wrapper; arbitrary quoted text does not qualify. A queued prompt remains uncertain until its actual turn begins. The client does not submit that message again.

Receipts and outgoing payloads are written to the mod's private store before HTTP side effects. Confirmed outgoing IDs return their saved publication acknowledgement on identical retry; changed content is rejected. A failed or ambiguous send stays unconfirmed across restarts: preserve the ID and payload, inspect delivery state, and reconcile before any manual retry. The client does not guess a replacement ID or automatically resend. A cached publication acknowledgement is not a fresh delivery query.

Saved incoming receipts are replayed on restart. A claimed message without native evidence remains uncertain; unknown Hub outstanding messages stop the client at `reconcile_required`. Network failures back off from 1 to 30 seconds without replaying native execution. Session switches invalidate old callbacks before rebinding. Store keys use a credential fingerprint, never the plaintext token. Confirmed records older than 30 days are pruned; unresolved records remain. The host store limit is 4 MiB; exhaustion stops consumption for reconciliation instead of deleting unresolved work.

Legacy `kind=task` keeps the existing native `report` tool, stable event IDs and actual main-turn run binding. It reports accepted, running, needs_user, succeeded or failed and submits device/workspace-relative artifact references. Mailbox mode never uses that completion path. Tool results use native JSON-text output on success and error.

## Development checks

Use the explicit plugin validation/test subcommands below for automated checks without model calls. A slash command passed to `claude -p` can fall through to a model when the command is not loaded; do not use it as a zero-cost probe. Native inbox and reply validation spends model turns and must be budgeted separately.

```sh
node --test clients/claude-code/tests/client.test.mjs
claude plugin validate clients/claude-code/agi-hub-endpoint
claude plugin test clients/claude-code/agi-hub-endpoint
```

Pure Node tests cover lease/generation races, mailbox and legacy separation, silent status, exact native acknowledgement, queue/restart uncertainty, scoped native tools and outbound deduplication. Official mod tests cover hook loading and command/tool output ABI without invoking a model. Generated `.claude-plugin/types/` and `tsconfig.json` are local development files: they may contain personal MCP tool types and are not distributed.

SDK references: [mods reference](https://code.claude.com/docs/en/plugins/mods/reference), [mods API](https://code.claude.com/docs/en/plugins/mods/api), [mod tests](https://code.claude.com/docs/en/plugins/mods/test). Revalidate these early-access interfaces after host updates.
