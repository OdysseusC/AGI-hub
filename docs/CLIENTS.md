[English](CLIENTS.md) | [简体中文](CLIENTS.zh-CN.md)

# Independent assistant Clients

Each AI assistant has its own Client claiming explicitly addressed local Hub messages and reporting delivery. Codex Desktop MCP and WorkBuddy CLI implement addressed mailboxes; Claude Code mod 0.2.2 also implements mailbox 1 through native tools, with original macOS Code idle execution and return to busy original Codex verified on 2026-10-20. Grok retains legacy single-task compatibility with mailbox capability `0`. The Go Hub neither plans tasks nor generates replies. Clients translate actual supported native entries; replacing one requires neither recompiling the Hub nor closing other assistants.

Clients are maintained separately from the Hub and may be independent processes or loaded by the native assistant. There are no Go dynamic libraries or embedded Python. Implementations may use different languages under one protocol. Python implementations exist; independent Node Codex Desktop MCP sending has been tested, and a resident MQ Client is available with local simulations.

## Layout and technology

```text
clients/
  shared/             # Standard-library HTTP, SSE, receipts and notification formatting
  codex/              # Addressed Desktop MCP mail / legacy queue and app-server
  workbuddy/          # Official CLI resume / localassistant OpenAPI
  grok-routine/       # User-configured official Routine webhook
  claude-code/        # Original-session mod, native report tool and local marketplace
  tests/              # Simulated transport tests, no real assistants
```

Python Clients maintain `manifest.json`, `main.py`, `adapter.py` and `client.example.json` in product directories; other languages have their own entry points. A manifest declares protocol, entry, session kinds and sending modes, not verified native capabilities. Set a Hub endpoint's `idle_wake_verified=true` only after testing the user's actual machine, assistant version and target session. Busy result receipt requires separate verification.

### One repository first; separate repositories later

The first version maintains Go Hub, product Clients and shared runtime together. WorkBuddy/Codex Clients are `0.3.0`, Grok Routine `0.1.0`, and Claude Code `0.2.2`. All use interface protocol `1`, with mailbox capability declared separately. Python entries use shared `0.2.0`; Codex declares it under `alternate_entrypoints.python`. `clients/shared/manifest.json` records the library version and is not an assistant entry point. Codex Node Desktop MCP uses Node 22+ standard libraries without npm or Python shared. Existing Python queue/app-server/Desktop MCP transports keep separate boundaries.

Clients have independent maintenance boundaries. A separately copied/released Python Client must include its specified `clients/shared` version under the same parent layout. Node Desktop MCP has no Python dependency; Claude Code mod depends only on host mods APIs and distributes its complete plugin/marketplace. Pin a repository commit for matching sources/manifests. These version constraints are maintenance conventions, not automatic downloads or dependency resolution. Shared can later become a versioned library and Clients separate repositories without changing the protocol.

Go Hub needs neither Python nor Node.js. Python Clients require Python 3.11+ without third-party Python packages. Codex Desktop MCP uses Node 22+; local macOS verification also requires the verified official signed Node and natural host launch chain. Claude Code plugin development/validation uses 2.1.299 without another Node/Python runtime. Automatic runtime installation, background-service supervision and OAuth renewal remain future installation work; they do not change the Hub protocol.

## Current addressed mailboxes

A Hub binding must explicitly set `mailbox_protocol_version:1` to claim `/v1/mail/inbox`; otherwise it retains the legacy inbox. Configure sender `allowed_peers` separately; `visible_peers` controls directory visibility only. Defaults are off, not automatically enabled by skill installation or MCP discovery.

After verifying both original sessions and wake channels, fill these fields within their complete bindings:

```json
{
  "endpoint": {"mailbox_protocol_version": 1},
  "visible_peers": ["workbuddy"],
  "allowed_peers": ["workbuddy"]
}
```

This is a Codex field fragment, not a complete configuration; WorkBuddy's corresponding lists use `codex`. Repository `config.example.json` keeps mailboxes off, connectivity lists empty and unbound assistants disabled for explicit user selection at install. Set `mailbox_while_busy_verified:true` only after verifying busy guidance for that entry.

| Client / transport | Mailbox capability | Native behavior and boundaries |
| --- | --- | --- |
| Codex Desktop MCP | `1` | `send_message_to_thread` directly guides the fixed existing chat for `request/reply/result`; no native-queue fallback; `status` is saved locally without a model wake |
| WorkBuddy CLI | `1` | Official CodeBuddy CLI `--resume` targets an existing session; full Envelope through stdin; ACK only after actual exit and matching result session |
| Codex queue / app-server | `0` | Legacy transports; queued input, separate SDK or another session cannot impersonate mailbox wake of the original chat |
| WorkBuddy OpenAPI | `0` | Official local-assistant candidate; requires personal OAuth and cannot target an ordinary Chat's original `session_id` |
| Claude Code mod 0.2.2 | `1` | In-session queue mode with fixed-identity native `peers/mail_send/mail_state`, leased event long polling, no Bash or automatic reply; original macOS Code idle execution/busy Codex return passed on 2026-10-20; no busy steer |
| Grok Routine | `0` | Retains official webhook single-task transport; new bidirectional mailbox Client is not verified |

`mode:"queue"` is the current mailbox Envelope format, not the native sending mechanism. `mailbox_while_busy_verified` is enabled only after actual busy-guidance verification and is separate from legacy `result_queue_while_busy_verified`. Mailboxes require no parent task, waiting state, execution Report or settled operation. Native session state and in-flight delivery determine capacity. New messages do not create tasks/replies automatically or return along a fixed task tree.

Assistants send mail with the same `correlation_id` and explicit recipients. `root_origin` preserves the original input session so skills can choose the final destination; `reply_to` only associates messages. Clients neither divide work nor reply for models. Read [dated verification](NATIVE-VALIDATION.md): old-protocol native tests do not automatically cover the new mailbox loop.

## Legacy native integration and acceptance records

The following preserves actual Task/Report compatibility history. Its run IDs, resources and automatic result return do not apply to new mailboxes.

WorkBuddy explicitly chooses `native.transport="cli"` or `"openapi"` without fallback. Original-configuration login errors on 2026-10-14, successful authentication continuations on October 15, local query denials and remote `850003 / authorization expired` remain historical records. After user reauthorization, the original endpoint/session/working directory/`.workbuddy` returned contact candidates matching artifacts, actively reported and automatically returned through busy MQ. The first delivery was not accepted under old diagnostic instructions. After zero tools/files/Reports were verified, one explicit continuation followed entry checks, preserving task/locks/delivery without requeue or fake receipts. A fixed positional instruction clarified the current handoff; full Envelope still entered stdin without tool/permission/MCP/model overrides. This verifies the selected candidate, not address-book uniqueness or full desktop-tool inheritance. See [dated records](NATIVE-VALIDATION.md).

Real macOS arm64 MQ execution has native and file evidence. The first task was explicitly denied and confirmed unexecuted before narrow skill reads, actual acceptance-workspace read/write and scoped-report-helper permissions plus absolute cwd were corrected. The native session actively sent `accepted/running/succeeded`; file SHA-256 matched. SSE claimed a second task, resumed the same logged-in independent CLI without tool/permission overrides, and completed all three reports and new-version artifacts on first execution, again with matching SHA-256.

The tasks used different execution IDs. Results had new `message_id`, correct `reply_to` and `correlation_id`. Identical Submission/saved Report retries added neither messages nor executions; terminal resources were released. `delivered` remained distinct from success, which was supported by native receipts and actual files.

On 2026-10-14 the running Codex entry used official desktop tools with `send-now` to guide both results back. On October 15 an independent Node client in the natural host environment initialized official Desktop MCP, discovered 57 tools and actually called `send_message_to_thread`. It received `isError=false` and a matching thread ACK; `codex_delegation` immediately entered the busy original chat without native queue or manual clicks.

The same machine's Python parent chain failed macOS signature checks while official Node succeeded; conclusions apply only to those launch chains. App Server proxy lacked a daemon control socket, a separate issue that does not disprove Desktop MCP. Resident Node MQ Client 0.2.0 then subscribed before a new task and automatically handed it to the fixed independent WorkBuddy CLI. Native reports/artifacts were verified before a new associated result was automatically claimed and sent through official Desktop MCP into the busy original chat. No entry/operator manually forwarded, queued or clicked; terminal resources were released. Two busy MQ returns passed. The second corrected the causal sequence in its delivered artifact. Both current files matched Core SHA-256, used distinct hubrun IDs, had four delivered task/result envelopes and released locks. A later single idle diagnosis through the same Desktop MCP delayed once, checked original-chat idle/old-turn completion once, sent once, started a different turn and retained official desktop read_thread. It added no MQ task and does not prove a complete idle MQ loop. The later original-session contact flow separately required one explicit continuation after entry checks and also returned while the original entry was busy. See [segmented evidence](NATIVE-VALIDATION.md).

CLI sources: [official headless mode](https://www.workbuddy.cn/docs/cli/headless). OpenAPI sources: [official OpenAPI](https://open.workbuddy.cn/docs/openapi) and [local-assistant description](https://www.codebuddy.cn/docs/workbuddy/From-Beginner-to-Expert-Guide/Function-Description/Assistant). Neither transport reads desktop Cookies, cached tokens or IPC tickets or treats private application authentication as public API authorization.

## Configuration and startup

Register the endpoint in Hub with `"driver": {"type": "external"}` and an independent Client. Do not run two consumers for one endpoint. Native identity parameters belong in private Client configuration, never the Hub admin secret.

On every connection, `GET /v1/endpoint?endpoint=ID` verifies interface protocol `1`, endpoint ID/enabled state, native destination and mailbox support for that transport. After selecting mailbox capability, Codex/WorkBuddy subscribe SSE before atomically claiming mail; Claude uses event long polling in both mailbox and legacy modes. Types/bindings must match:

| Client | Hub `endpoint.driver` | Hub `endpoint.session_id` |
| --- | --- | --- |
| Codex | `codex` | Exactly matches private `native.thread_id` |
| WorkBuddy CLI | `workbuddy` | Exactly matches private `native.session_id` |
| WorkBuddy OpenAPI | `workbuddy` | Must be `localassistant`, the dedicated persistent local assistant |
| Grok Routine | `grok-routine` | Exactly matches private `native.routine_id`, the user's label for the Routine |
| Claude Code mod | `claude-code` | Exactly matches private `native.session_id` and current `$.session.id()` |

`endpoint.driver` identifies the assistant, unlike the binding transport `driver.type="external"`. Incompatible protocols, disabled endpoints or mismatched sessions/types stop consumption. Message bodies cannot override destinations. `unknown` stays unknown rather than being changed to idle by the Client; Hub decides eligibility. Reconnects revalidate bindings.

In legacy Task/Report, `endpoint.session_id` is the reused destination and `Report.run_id` identifies one task execution. Do not confuse them. New mail uses neither execution Reports nor run IDs. Legacy receivers prefer actual native run/turn IDs; if unavailable, use the notification's task-specific `hubrun-...` and label it a Hub identifier. Keep one identifier per task rather than reusing the fixed session ID across tasks.

Legacy `result_queue_while_busy_verified` defaults false. Only verified busy result input permits enabling it. The Hub can then deliver `kind=result` in `busy`/`needs_user` while still waiting for unresolved transport. This does not start ordinary busy `kind=task`, release resources, prove steer/interrupt or approve native prompts. `unknown` remains ineligible.

Evidence belongs to the actual transport: in-host Codex guidance success does not prove a separate Python proxy. Reverify after changing transports.

Export private endpoint configuration through the Hub admin CLI:

```sh
agi-hub client-config --endpoint workbuddy --file /absolute/path/to/private/workbuddy/client.json
```

`--file` must be an absolute local path. The command creates with `0600`, `O_EXCL`, never overwrites or prints credentials. It exports only endpoint scope and `native: {}`, not Hub admin secrets or assistant login data. Windows permissions still depend on user ACLs. Fill `native` from that Client's example. Default port is `7331`; private Hub configuration determines actual `base_url`.

Or manually create repository-external private `client.json` from its example. Only four top-level fields are accepted:

```json
{
  "base_url": "http://127.0.0.1:7331",
  "endpoint": "workbuddy",
  "endpoint_token": "<this endpoint's credential issued by the local Hub admin>",
  "native": {}
}
```

`base_url` must be numeric local loopback without LAN addresses, DNS names, embedded credentials or extra paths. `endpoint_token` accesses its own mailbox/events/receipts and sends only to `allowed_peers`; it cannot change leaders/configuration or create admin tasks. Requests set required Host, Origin and Authorization. Independent Python/Node ignores environment proxies and redirects. Claude's host HTTP API follows redirects, stripping Authorization and other nonpreserved headers cross-origin, so a fixed local numeric loopback URL is still required.

Start required Python Clients at repository root. For Node Desktop MCP see [Codex Client](../clients/codex/README.md). macOS needs the host-verified official signed Node absolute path, not an arbitrary PATH Node presented as the same launch chain:

```sh
python3 clients/codex/main.py --config /path/to/private/codex/client.json
python3 clients/workbuddy/main.py --config /path/to/private/workbuddy/client.json
python3 clients/grok-routine/main.py --config /path/to/private/grok/client.json
```

Windows may substitute `py -3` and its actual absolute configuration path. Match the real Hub port; an example port is not discovery. Restrict private configuration to the local user and do not commit real tokens, thread IDs, chat or artifacts.

### Claude Code mod configuration

Claude Code has no external consumer process. Export with `agi-hub client-config --endpoint claude --file <absolute-private-path>` and fill [Claude Code settings](../clients/claude-code/client.example.json): `native.transport="mod"`, target `session_id`, `long_poll_ms`, `max_prompt_bytes`. Plugin options store only the private configuration's absolute path, not a token. Nonempty activation options take priority. When the host supplies an empty option despite saved settings, the Client reads only its own plugin path via `$.plugin.name` and `$.settings.read()`; conflicting own paths stop the Client rather than selecting one.

Install `agi-hub-endpoint@agi-hub-local` from the local marketplace with official `claude plugin install`. Configure with `claude plugin configure agi-hub-endpoint@agi-hub-local --values-stdin`, supplying `{"client_config":"/absolute/private/path/client.json"}` on stdin, or use the host plugin UI. In the already-open target Code session run `/reload-plugins`, `/agi-hub-id` and `/agi-hub-status` to verify original UUID/listening state. If CLI is not on PATH, use the actual Claude Code binary supplied by this desktop host, not another computer's version directory. See [full instructions](../clients/claude-code/README.md).

The plugin acquires/renews the Hub's 60-second atomic lease and confirms ownership before prompt submission. `/clear`, `/resume` and `/branch` recheck the session; mismatches sleep rather than consuming the old endpoint. Persistence is isolated by Hub/endpoint/session. `outstanding` shows only its unresolved deliveries for reconciliation, not a redelivery queue.

Mailbox mode requires `/v1/endpoint` capabilities `mailbox_long_poll_max_ms:25000` and `mailbox_client_lease:true` before claiming, so older protocol-1 Hubs cannot silently ignore waits/leases. It waits on `/v1/mail/inbox?endpoint=claude&wait_ms=25000`; mutating mailbox routes are lease-fenced. Native `peers`, `mail_send` and `mail_state` tools fix identity/lease and return JSON text. Explicit model sends create business replies, not turn completion or Reports. Only queue is supported; keep `mailbox_while_busy_verified=false`. Original macOS Code idle mailbox execution and explicit return to busy original Codex passed on 2026-10-20; other native scopes remain unverified.

For legacy compatibility, the `report` tool accepts only bound received `kind=task`, fixes endpoint and the actual first-report `claude-turn:<turn-id>`, retains that run_id and requires identical complete Reports for one event_id. `kind=result` is display-only without reporting/execution. Native permissions and user authorization remain; plugin handoff cannot approve prompts.

Enable `idle_wake_verified` only after actual idle wake is verified through a native inbox test or diagnosis; an extra paid N2 is not mandatory. Busy result capability needs separate acceptance. Historical N2/N3 and the 2026-10-20 original macOS Claude-3p 2.1.299 mailbox loop are recorded separately. The latter used one native business turn but three billed model-response usage records, not one model request or zero cost. These results do not enable flags for other hosts. See [actual records](NATIVE-VALIDATION.md#2026-10-20-pure-mailbox-original-claude-code-session-and-codex-return).

### WorkBuddy CLI configuration

The [WorkBuddy example](../clients/workbuddy/client.example.json) uses `native.transport="cli"`. Verify the selected profile and existing session first. If login is required, use the official personal flow before configuration. The Client resumes existing IDs only, never creates sessions. `command` is an absolute executable, not a command string or argv array. `cwd`/`config_dir` must be existing local absolute directories and `session_id` must exactly match Hub. `timeout_seconds` defaults to `120`, range `1..600`.

Complete Envelope and mailbox/compatibility instructions are written to a temporary regular stdin file; stdout also uses a regular temporary, fully parsed then removed. Task text never enters argv or a shell. Arguments are fixed:

```text
<command> [<entry_script>] --resume <session_id> --print --output-format json --input-format text
```

There are no tool, permission, MCP, model or profile overrides or `--no-session-persistence`. The session/profile defaults apply and chats persist. `CODEBUDDY_CONFIG_DIR` selects configuration, not isolated credential storage; official CLI may share product authentication. A profile is not an account security boundary.

Failure does not imply unchanged history. A failed default-persistence original-session resume on 2026-10-14 still appended history; its original prefix was verified intact. The same configuration/session passed default authentication continuation on October 15, so the earlier error is not the current conclusion. Preserve uncertainty and reconcile history/native results rather than blindly resending after failure or empty output.

Optional `environment` accepts only these string values, not arbitrary variables:

| Key | Allowed values |
| --- | --- |
| `CODEBUDDY_FORCE_LITE_WB_BUNDLE` | `"0"`, `"1"` |
| `DISABLE_AUTOUPDATER` | `"0"`, `"1"` |
| `CODEBUDDY_INTERNET_ENVIRONMENT` | `"internal"`, `"external"`, `"ioa"`, `"cloudhosted"`, `"selfhosted"` |

The example uses `internal`; choose the site where the user officially logged in. It selects CLI service environment, not extra/inherited desktop tools. Match the bundle flag to the actual entry. Do not put tokens, desktop IPC arguments or permission overrides in `environment`.

On Windows `command` must be an actual `.exe`; `.cmd`/`.bat` are rejected. An official JavaScript entry may use actual `node.exe` and optional `entry_script`, e.g. `command="C:/Program Files/nodejs/node.exe"`, `entry_script="C:/absolute/path/to/official/codebuddy.js"`. The script must be an existing regular `.js`, `.mjs`, `.cjs` or file named `codebuddy`. Invocation remains separate argv without a shell. Existing Node.js and official CLI are required; runtime installation is not automatic.

### WorkBuddy OpenAPI configuration (mailbox off)

`native.transport="openapi"` remains an explicit alternative. Old configurations omitting `transport` retain OpenAPI behavior, never silently try CLI. Replace the entire CLI `native` object with this, without mixing fields:

```json
{
  "transport": "openapi",
  "access_token": "<short-lived access_token from the user's official OAuth grant>",
  "oauth_scopes": ["user.localassistant.invokable"],
  "timeout_seconds": 30
}
```

OpenAPI uses `Authorization: Bearer access_token`, `{"content":"complete notification","msg_type":"text"}` and `user.localassistant.invokable`. Configured `oauth_scopes` records the user's grant; the platform still authenticates it. Missing token/scope yields `unconfigured` before claims. There are no app secrets, refresh tokens or automatic refresh. Stable long-term operation still needs official app authorization/renewal design. An attempted request with an expired token stays uncertain and is not resent. Personal CLI login does not authorize third-party OpenAPI.

### Grok Routine configuration (legacy, mailbox off)

`native.routine_id` is the user's stable label for the official webhook and must match `endpoint.session_id`. `native.body` must carry the full message. `"{{envelope}}"` becomes a JSON object, `{{envelope_json}}` complete JSON text, `{{notification}}` the full notification/report instructions. `{{content}}`, `{{task_id}}`, `{{message_id}}` are also supported but cannot replace the full envelope. Replacement runs once; placeholders inside task content are not interpreted again. Object placeholders must occupy an entire JSON string value.

### Codex executable configuration

Desktop MCP, queue and App Server proxy are separate transports with separate acceptance. Independent official Node Desktop MCP actually guided the busy original chat on 2026-10-15. That acceptance used resident MQ Client 0.2.0; current 0.3.0 adds addressed mail with Node 22+. See the Codex directory for startup. Internal model-tool calls cannot substitute for independent Client evidence. Only the fixed chat/permitted sender tool is used; discovery is not authorization to call every tool.

Local macOS signature validation rejected the Python parent chain while official Node passed. Missing App Server daemon sockets do not disprove Desktop MCP. Do not start another daemon, copy logins or access private IPC as a replacement original chat.

Original queue/app-server Python configuration boundaries:

Codex executable resolution is fixed to an absolute path on Client startup, never rediscovered from PATH during delivery. Windows requires actual `codex.exe`; `.cmd`/`.bat` wrappers are rejected to prevent shell interpretation. Hub `endpoint.session_id` and `native.thread_id` must refer to the same verified native thread.

## Mail consumption, transport receipts and assistant replies

1. Verify binding/mailbox capability, then read the durable mailbox: Codex/WorkBuddy subscribe SSE first; Claude uses event long polling. The Hub atomically changes `pending` to `dispatching`; another message is not claimed while that endpoint has `dispatching/uncertain`.
2. `status` is saved locally and acknowledged without a model. Attempt native `request/reply/result` once. WorkBuddy resumes the existing session; Codex Desktop MCP directly guides the fixed existing chat without native queue; Claude submits into the bound original Code session.
3. Send `/v1/mail/delivery` only after WorkBuddy CLI exits with a complete, non-error, matching-session result, Codex MCP confirms input, or Claude observes the exact message marker in its native main-session `turn.start`. ACK is transport evidence, not business success. Models express progress, required personal action or artifacts through another mail message, never the original delivery receipt.
4. A receiving assistant replies explicitly with its scoped `mail-send` helper or Claude's native `mail_send` tool, a new ID, original `correlation_id`, relevant `reply_to` and a permitted recipient. It may send directly to the original entry instead of the previous sender. Ending a model turn never automatically replies.

The first publication defaults to 16 messages/30 minutes; status counts too. `mail-state` lists only the caller's sent/received delivery records. Visible `status` bodies may be read; `request/reply/result` remain metadata-only without copying task context.

SSE keepalives do not query business state. Backoff/reconnect claims only new `pending`. Missing confirmation after a network attempt, a started CLI timeout, incomplete output or mismatched session remain `uncertain` without resending. Restarting only the Client leaves claims unchanged; Hub restart converts `dispatching` to `uncertain`. Unconfirmed receipts stop consumption for reconciliation. Never erase records, change IDs or fake native idle to bypass reservations.

Files identify device/workspace/relative path. Mailboxes do not copy files or verify business artifacts automatically; the final entry checks as required. Native permissions remain with the user; skills/messages cannot approve tools. New mode needs no Report helper. A scoped messaging helper calls only local mailbox/directory APIs, not hidden tools.

### Legacy Task/Report compatibility

Only old `kind=task/control` without `mailbox:true` continues native `accepted/running/needs_user/succeeded/failed`. Legacy terminal Reports generate `kind=result` to the original entry; old results are display-only. Claude's native `report` and `clients/shared/report.py --config ... --file ...` apply only to compatibility, not execution receipts for new mail. See above and [full history](NATIVE-VALIDATION.md).

## Independent updates and replacement

Clients can be started, stopped and replaced separately without changing Hub protocol. There is no supervisor for every Client or automatic download, verification/execution of unknown plugins.

Update/reinstall Claude Code through official plugin tools, then `/reload-plugins` in the target session. It stops old listeners, reacquires the lease and reconciles saved records rather than resending on reload. Resolve claimed delivery state first; reload cannot clear uncertainty.

For safe updates, pause sends to the endpoint and obtain explicit receipts for claimed messages. Stopping a Client after native handoff does not stop the native assistant. New-mail assistants must still explicitly send their results; old tasks use Reports. Replace the product directory after stopping the old Client, then restart it; other Clients and Hub may stay running.

If `dispatching`, `uncertain` or unsaved receipts exist, reconcile actual native execution before resuming. Do not erase records, set idle or use a new ID to bypass uncertainty. Forced mid-delivery shutdown requires conservative recovery, not a claim of lossless hot update.

New Clients implement `validate_binding(endpoint)` and `deliver(envelope) -> (outcome, detail)`, declare actual `session_kinds`, mailbox capability and modes, then reuse shared runtime or implement the protocol independently. Mailbox `1` requires verified native binding/wake; adding a flag cannot make an old transport safely consume new mail. Validate native type/destination without allowing task text to rewrite configuration. Hub/Client interface protocol is `1`; reject incompatible changes. Replacing Python files does not alter native logins. Separate CLI/SDK sessions must be labeled in manifests/support notes.

## Multiple platforms and computers

Python uses standard-library HTTP, subprocess and files; Codex Desktop MCP uses Node, Claude Code host mods APIs. Go builds native programs; users install required Client runtimes. Verify Windows/macOS/Linux native CLIs, official MCP launch/signature chains and environment separately. One language/host startup does not prove another environment.

The current Hub is local loopback only. Another computer needs its own Client and a later authorized cross-device transport; local SSE is not automatically a remote service. File references carry `device_id`, `workspace_id` and workspace-relative paths. They are neither copied automatically nor directly usable by remote assistants.

## Verification

Use Node state-machine tests and official `plugin validate` / `plugin test` for offline Claude checks. A `claude -p` slash command is not guaranteed to avoid model calls; an intended configuration-only probe actually invoked the model. Do not use it as a zero-cost preflight. See the [incident and measured usage](NATIVE-VALIDATION.md#2026-10-20-pure-mailbox-original-claude-code-session-and-codex-return).

```sh
python3 -m unittest discover -s clients/tests -v
```

Local fake HTTP and subprocesses test subscription/claim ordering, Host/Origin, redirect rejection, uncertain delivery, one native attempt, stopping after failed receipts, result semantics, WorkBuddy CLI bindings/I/O, OAuth boundaries and one-pass templates. Tests neither send actual assistant messages nor read contacts or prove native integration.

For new-mail native acceptance, begin with a low-cost local-file task. Verify original-session wake, explicit assistant-chosen recipients, associated replies, artifacts, final return and uncertainty recovery. Retain old acceptance/execution Report evidence without presenting it as new-mail business completion. Business-system access/contact messaging still needs concrete human instructions and any native personal confirmation.
