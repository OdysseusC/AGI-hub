[简体中文](PROTOCOL.md) | [English](PROTOCOL.en.md)

# Message and receipt protocol

Current collaboration uses addressed mailboxes: skills select recipients, Clients wake their native sessions, and the Hub stores messages, delivery evidence, deduplication and basic limits only. All IDs below are examples. Legacy Task/Report remains compatible in the second half; it is not the new mailbox processing flow.

## Addressed mailboxes

```json
{
  "id": "mail-example-001",
  "recipient": "workbuddy",
  "kind": "request",
  "content": "Check the provided material and return only necessary findings; files belong to this-device's work workspace.",
  "origin_ref": "human-request-example",
  "max_messages": 16,
  "deadline": "2030-01-01T12:00:00Z"
}
```

Call `POST /v1/mail/send?endpoint=codex`, or save that JSON and run:

```sh
agi-hub mail-send --data /absolute/private/agi-hub --endpoint codex --file /absolute/work/request.json
agi-hub mail-state --data /absolute/private/agi-hub --endpoint codex --correlation-id mail-example-001
```

| Field | Convention |
| --- | --- |
| `id` | Caller-saved message idempotency key; retry the same ID with identical content |
| `recipient` | One explicitly selected endpoint; contact another member with a separate message/ID |
| `kind` | `request`, `reply`, `status`, `result`: content categories, not Hub workflows |
| `content` | Nonempty text, at most 64 KiB; concise JSON, task summary and device/workspace-qualified file references |
| `correlation_id` | Reused by later messages; initially defaults to the first message ID, which is also the only valid ID for a new conversation |
| `origin_ref` | Required caller-provided reference to the first human input, not new authorization proof |
| `reply_to` | Optional same-conversation message visible to the sender; does not choose a recipient |
| `max_messages` / `deadline` | New conversations default to 16 messages/30 minutes, at most 64; later messages inherit without increasing/extending |

The Hub captures `root_origin` from the registered sender: `endpoint`, `device_id`, `session_id`, `request_ref`. It helps the skill identify the original entry and **does not force a reply path**. A may ask B, B ask D, and D explicitly reply to B or directly to A within user-permitted connectivity. Final return to the original session is an assistant/skill convention; the Hub neither sends for assistants nor aggregates results. If a selected recipient is the original endpoint but has been rebound to another device/session, delivery is refused while original context is retained.

Later senders must already participate in the conversation. Both sides require mailbox protocol `1`, and sender `allowed_peers` must include the recipient. `visible_peers` controls discovery only, not permission. Members may adjust division of work; messages cannot replace user approval, share others' permissions or expand original authorization.

Returned Envelopes have `mailbox:true`, independent `message_id`, `correlation_id`, `root_origin`, explicit `sender/recipient`, and empty `task_id`. `mode:"queue"` is the Hub envelope format, not a requirement to use native queue: Codex Desktop MCP still guides directly and WorkBuddy uses original-session `--resume`. Mailboxes create no Tasks and require no parent task, waiting state, run ID, execution Report or settled receipt.

## Delivery evidence and limits

`pending → dispatching → delivered / uncertain / failed` describes transport only. Atomic claims reserve endpoint transport capacity. No other message is claimed or automatically resent while `dispatching/uncertain` remains unresolved. Hub restart preserves unfinished `dispatching` as `uncertain`.

Only the Client acknowledges native transport: WorkBuddy after complete CLI exit/matching result session, Codex after Desktop MCP confirms native input, via `/v1/mail/delivery`. A model's `status/result` is another message, not completion of the original delivery. `delivered` proves neither acceptance of business work nor success/quality. Assistants send those explicitly and the original entry verifies them.

`request/reply/result` is claimed for verified idle wake, verified busy guidance or separately registered independent native queue capability, without deriving native idle from business phase. `status` is recorded/acknowledged locally without entering a session or starting a model. Every publication, including status, consumes the shared conversation total. Limits/deadlines reject new publications. Expiration/cancellation neither stops actions already performed nor guarantees a cap on host tokens/credits. Keep messages short, avoid acknowledgment loops and return necessary results before exhausting the allowance.

`mail-cancel --endpoint <original-entry> --correlation-id <ID>` closes the conversation and withdraws unsent messages only. Delivered, dispatching or uncertain actions retain real state without fabricated business interruption. Same-ID retries still return only the existing record.

## Independent native queue registration

In 0.5.0, `mailbox_native_queue_verified` is a separate local delivery qualification for mailbox protocol 1 and an exactly `["queue"]` fixed binding. It permits claims in actual `unknown/idle`; known `busy` still requires separate `mailbox_while_busy_verified`, and `needs_user` blocks business mail. In-flight, uncertainty, deadline, permission and binding checks remain. It changes no native state and proves neither serialization, concurrency safety, completion nor approval.

After checking native evidence, the local administrator calls `POST /v1/manage/native-queue` with `{"endpoint":"grok","native_session_id":"actual-routine","verified":true,"evidence_ref":"local-native-probe-001"}`. Revocation explicitly sets `false`. This uses local management authentication; endpoint credentials cannot self-grant, and remote management does not expose it. See the CLI in the [Grok README](../clients/grok-routine/README.en.md).

Configuration also stores a device/driver/session/evidence binding anchor. The reference is recorded without reading its path. Missing or mismatched anchors invalidate qualification; rebinding clears it, while the same binding preserves it across disable/enable and restarts. Saving language or enablement cannot resurrect a configuration revocation from old runtime state. This field adds no mesh advertisement protocol or business scheduler.

## Current mailbox API

All interfaces use numeric local loopback and validate Host, Origin and endpoint credentials. Another computer cannot directly use local URLs/file paths.

| Endpoint | Operation |
| --- | --- |
| `POST /v1/mail/send?endpoint=ID` | Publish to an explicit user-permitted recipient |
| `POST /v1/mail/inbox?endpoint=ID[&wait_ms=N]` | Atomically claim eligible mail; optionally await events up to 25 seconds |
| `POST /v1/mail/delivery?endpoint=ID` | Client sends `message_id/outcome/detail` for mail received by itself only |
| `GET /v1/mail/state?endpoint=ID&correlation_id=ID` | Conversation context/allowance and own sent/received delivery records; visible status includes its body, other mail metadata only |
| `POST /v1/mail/cancel?endpoint=ID` | Original entry closes the conversation with `{"correlation_id":"ID"}` |

State queries return bodies only for status sent/received by the caller, not other participants' mail or request/reply/result bodies.

Each uses its corresponding endpoint credential. `GET /v1/endpoint`, directory/rules and SSE remain available. Events signal change only; claim full messages through the mailbox. Opt in with `mailbox_protocol_version:1`: Codex Desktop MCP, WorkBuddy CLI and Claude Code mod 0.2.2 implement it; Claude's original macOS Code idle mailbox execution and busy Codex return passed on 2026-10-20, while Grok Client 0.2.1 can explicitly enable `1`, with actual native round-trip acceptance pending. Other native scopes require separate acceptance. Interface protocol remains `1`, distinct from disk format. First successful mailbox publication upgrades the journal to `3`; the current reader accepts `1/2/3`, while old programs must reject newer records. Unpublished v2 prototype records remain diagnostic only, not resumed parent/child workflows.

Mailbox `wait_ms` uses the same `1..25000` validation, event subscription/recheck, cancellation and HTTP-200 `null` timeout behavior as legacy long polling below. Claude mailbox mode first requires `/v1/endpoint` to advertise `capabilities: {"mailbox_long_poll_max_ms":25000,"mailbox_client_lease":true}`. Its send/inbox/delivery/cancel mutations require the active `X-AGI-Hub-Lease`; older Hubs missing these declarations are rejected before claims. Native `mail_send/peers/mail_state` tools return JSON text with fixed identity; ordinary CLI callers cannot bypass the lease. Other drivers retain their existing behavior.

## Legacy Task/Report compatibility protocol

The remainder records the original single-task API, execution Reports and historical native tests. New mail does not enter that state machine or generate Report-derived replies.

### Task handoff

```json
{
  "id": "task-example-001",
  "origin": "codex",
  "recipient": "grok",
  "goal": "Prepare a summary file from authorized material",
  "context": "Use only the supplied material and preserve sources.",
  "inputs": [{"device_id": "this-device", "workspace_id": "work", "path": "input/brief.txt"}],
  "mode": "queue",
  "resources": ["workspace:work"],
  "require_artifact": true,
  "authorized_actions": ["read provided input", "write summary in work workspace"],
  "max_handoffs": 1,
  "deadline": "2030-01-01T12:00:00Z"
}
```

Omitted `origin` uses the current leader. Creation fixes the entry, coordinator and leader epoch; switching leaders does not migrate old work. The caller persists `id` as an idempotency key and retries identical content. File references identify their device/configured workspace with `/`-separated relative paths. Submission checks existence/optional SHA256, but references neither freeze input versions nor copy files between machines.

`authorized_actions` describes handoff scope, not new user approval or a native permission sandbox. V0 permits only admin task creation; endpoint credentials claim/report only their own messages. Native assistants with full same-account file access are not separately isolated security domains.

V0 fixes `max_handoffs` at 1: one handoff and a result, without recursive delegation. `deadline` limits unsent messages, not already-running native work or actual token/spending budgets.

### Message association and session identity

The Hub converts a Submission into a complete Envelope. Native entries preserve:

| Field | Meaning |
| --- | --- |
| `message_id` | Independent identifier for this message; handoff/result use different values |
| `task_id` | Task identity across handoff, Reports and result |
| `correlation_id` | Conversation chain; V0 equals the original task ID |
| `reply_to` | Answered message; omitted on original task, result points to its `message_id` |
| `sender` / `recipient` | Endpoints for this message; execution results return to the saved original entry |
| `kind` | `task`, `control`, `result`: execute, request native control or display only |
| `content` | Complete Submission or terminal Task as JSON text with necessary context/artifact references |

For example, original `message_id=task:task-example-001`, `task_id=correlation_id=task-example-001`; result `message_id=result:task-example-001`, `reply_to=task:task-example-001`, retaining task/correlation. Hub determines the result recipient from saved work; receivers do not switch entries or create another task to reply.

`Endpoint.session_id` is the fixed native destination; `Report.run_id` identifies one task execution. Multiple tasks may share a session but not reuse its fixed ID as their run IDs. Prefer actual exposed native run/turn IDs; otherwise the notification's task-specific `hubrun-...` may be explicitly labeled a Hub execution identifier, not native. Keep it consistent throughout one task. Control still requires a genuinely matching `target_run_id`; a Hub identifier does not prove native control.

### Sending modes

| Mode | Target relation | Admission |
| --- | --- | --- |
| `queue` | New independent task | Verified idle session, no unfinished/uncertain handoff and available resources |
| `steer` | Guide an existing run | Required `target_run_id`; original run continues; native Client must support guidance |
| `interrupt` | Request existing-run stop | Required `target_run_id`; release old resources only after actual `interrupted` receipt |

Unsupported modes are rejected without substitution. Successful stop does not start replacement work; resuming pauses is not supported. Mode may be selected per message within Client capability declarations; current default is `queue`.

`result_queue_while_busy_verified` defaults false and is independently verified from `idle_wake_verified`. It declares only that the native entry accepts `kind=result` while `busy`/`needs_user`. It still blocks unresolved `dispatching/uncertain`, starts no ordinary busy task, releases no resources and changes no run; unknown state still awaits checking. It is not proof of steer, interrupt, immediate display or prompt approval. Evidence is transport-specific: independent Node Desktop MCP busy guidance passed on 2026-10-15; App Server proxy lacked its daemon control socket. One transport's result or a new field cannot establish another's loop. Subsequent actual busy MQ evidence is recorded separately.

### Execution Reports

```json
{
  "event_id": "event-example-accepted",
  "task_id": "task-example-001",
  "endpoint": "grok",
  "state": "accepted",
  "run_id": "native-run-example"
}
```

States progress `waiting`, `accepted`, `running`, optionally `needs_user` and back to running, then `succeeded`, `failed` or `interrupted`. Each Report has its own stable `event_id`. Identical event retries deduplicate; changed content under one ID is rejected. Accepted `run_id` stays consistent.

```json
{
  "event_id": "event-example-completed",
  "task_id": "task-example-001",
  "endpoint": "grok",
  "state": "succeeded",
  "run_id": "native-run-example",
  "summary": "The summary was created and sources checked.",
  "artifacts": [{"device_id": "this-device", "workspace_id": "work", "path": "output/summary.md"}]
}
```

Success requires a summary and, when requested, a verifiable artifact. The sidecar checks ownership, path boundaries, symlinks, existence and optional checksum, then records actual SHA256. It verifies accessible evidence; task acceptance still judges quality.

### Delivery and return

Delivery is independently `pending → dispatching → delivered / uncertain / failed`. Claims persist dispatching first. Native transport success confirms delivery only; native execution receipts confirm acceptance/business state. Restarting only the Client retains dispatching; Hub restart converts it to uncertain. Both retain reservations without automatic resend.

Once a recipient actively reports execution with a stable run ID, a late transport timeout/exit failure cannot downgrade confirmed delivery. Matching recipient/native execution evidence makes that update an idempotent no-op. Transport-only confirmation, failures without a run identifier and conflicting result receipts remain rejected; this must not swallow every error.

A formal result creates one `kind=result` back to the task's original entry. It is not a new task, does not rerun work or create another handoff. Result transport and business success are separate; temporarily unavailable origins retain artifacts and pending returns.

Native permissions may allow messages but deny skill metadata, reporting or file creation. The first independent WorkBuddy CLI task was explicitly denied and verified unexecuted before narrow report/file rules and absolute cwd were corrected and it resumed. SSE claimed a second task, resumed the same existing CLI by default and completed on first execution. Both had actual native `accepted/running/succeeded` and matching SHA-256 files; transport receipts alone cannot infer them. If reporting works, the native session reports `needs_user`; otherwise retain delivery/native prompts rather than fake state.

Both tasks used different run IDs and independently identified results with correct `reply_to`/task correlation. Identical Submission/saved Report retries add no messages/executions. Native terminal resource release was verified.

On 2026-10-14, Codex in-host official tools guided two results back. On October 15, independent Node initialized/listed official Desktop MCP's 57 tools and actually called `send_message_to_thread`: `isError=false`, matching thread ID and `codex_delegation` entering the busy original chat without clicks/native queue. Resident Node then subscribed before a new task and automatically claimed it, resumed the fixed independent WorkBuddy CLI, received actual execution Reports, checked files/SHA-256 and returned associated results through independent MCP, releasing resources. Two busy MQ loops passed. A later separate idle diagnosis checked original idle/old completed once, sent once, started a different turn and retained official desktop read_thread. This verified native idle start, not a complete idle MQ business loop, since no MQ task was added. Missing App Server control sockets and Python-chain signature failure are separate observations, not Desktop MCP failure.

The original WorkBuddy session also passed authentication continuation, encountered Bash/Read denials and then remote `850003 / authorization expired`. After reauthorization, a fresh original-endpoint/session/configuration contact query returned candidates matching artifact name/department/title. The first delivered input was not accepted under older diagnostic instructions; zero tools/files/Reports were checked. One explicit entry-checked continuation was recorded only in existing delivered detail, preserving task/locks without requeue, new IDs or fabricated Reports. Actual native Reports, Core artifact checks and a new associated result automatically returning to the busy entry followed. Fixed handoff instructions neither change the full stdin Envelope nor expand source authorization. This does not prove first-attempt unattended success, whole-address-book uniqueness or complete desktop-tool inheritance.

Two earlier independent-CLI busy MQ returns had distinct hubruns, four delivered task/result messages, current files matching Core hashes and released resources; the second artifact corrected that causal sequence. The later original-session contact flow has separate raw tool/file/return/resource evidence and does not alter deduplication, authorization or artifact contracts. A complete idle MQ loop and other platforms remained unverified. See [dated records](NATIVE-VALIDATION.en.md).

### Local API

Interfaces default to numeric loopback only. Admin credentials stay private; endpoint credentials derive from admin secret plus endpoint ID and cannot change other Clients/leaders.

| Endpoint | Operation | Permission |
| --- | --- | --- |
| `GET /v1/state` | Local records | Admin |
| `POST /v1/tasks` | Submit complete task JSON | Admin |
| `POST /v1/leader` | Leader for subsequent tasks | Admin |
| `POST /v1/session` | Verify/update native session state | Admin |
| `GET /v1/endpoint?endpoint=ID` | Own protocol, binding and state | Own endpoint or admin |
| `GET /v1/catalog?endpoint=ID` | Explicitly visible enabled members, directory digest/rule version | Own enabled endpoint or admin |
| `GET /v1/skills?endpoint=ID[&content=1&known_version=HASH]` | Rules digest, optional body omitted when unchanged | Own enabled endpoint or admin |
| `GET /v1/skill-release` | Installer checks running Hub rule revision/digest | Admin, read-only |
| `POST /v1/inbox?endpoint=ID[&wait_ms=N]` | Atomic claim, optionally await changes up to 25 seconds | Own endpoint or admin |
| `POST /v1/client-lease?endpoint=ID` | Atomically acquire/renew/release consumer lease | Own endpoint or admin |
| `POST /v1/delivery?endpoint=ID` | Transport evidence | Own endpoint or admin |
| `POST /v1/report?endpoint=ID` | Native execution/results | Own endpoint or admin |
| `GET /v1/events?endpoint=ID` | SSE change hints | Own endpoint or admin |

SSE emits an initial change hint and later event-driven hints; authorized APIs provide full state/messages. Keepalives do not poll business tasks. Subscribe before claiming, and after reconnect claim only still-pending eligible messages, never claimed/uncertain ones. The Hub has no assistant executor and treats no SSE disconnect as proof of native failure.

The on-demand directory is brief data, not full state: only enabled `visible_peers`, empty by default, without native addresses, private paths, credentials or history. User-configured `purpose` is neither executable policy nor model ranking. Verification fields remain declarations. Visibility grants no task-submission permission or change to single-handoff behavior. Rule-file updates and directory/version reads neither create messages nor change native state/wake models. See [skill maintenance/loading](SKILLS.en.md).

Clients read their `/v1/endpoint` before connecting/claiming. It returns `protocol_version:1`, own endpoint/session and `outstanding`, containing only this endpoint's dispatching/uncertain messages for restart reconciliation. It is not a claim list or disclosure of other endpoints/admin secrets. Match product, modes and native session to private configuration. Reject mismatch/incompatibility rather than executing elsewhere while claiming the original binding.

Hub/Client protocol is `1`. Manifests describe protocol/session/modes, not automatically loaded capabilities. Each endpoint has one active consumer, external or in-session. Languages may differ; all preserve no-blind-resend, deduplicated receipts and authorization. Native configuration/credentials stay private to Clients, not Envelopes or Hub configuration. See [Clients](CLIENTS.en.md).

#### Event long polling

Without `wait_ms`, claims stay immediate. `wait_ms=N` accepts one decimal integer `1..25000`; empty, repeated, noninteger or out-of-range values return HTTP 400. Eligible work returns immediately. Otherwise subscribe/recheck/wait, claim immediately on arrival, or return HTTP 200 JSON `null` at timeout. Disconnect cancels waiting.

Long polling never reclaims dispatching/uncertain or changes resource/session eligibility. Competing waiters can claim a message at most once. Claude's mod cannot read SSE streams, so long polling provides equivalent event wake without querying native business history. Other Clients can retain SSE unchanged.

#### Atomic consumer lease

Claude Code (`driver="claude-code"`) uses an atomic Hub lease instead of a non-atomic write/read lock file to prevent desktop/terminal double consumption of one original session. Structural example only, no real lease:

```json
{"owner_id":"consumer-instance-example"}
```

First success returns `lease_id`, `expires_at`, `ttl_ms:60000`. Same-owner acquisition is idempotent. Renew with matching owner/lease, release with `"release":true`. Another owner, expired generation or mismatched lease yields HTTP 409. Owner identifies this Client load, not only fixed session_id. Leases exist only in the running Hub and must be reacquired after restart.

Claude inbox/delivery/report requests require `X-AGI-Hub-Lease: <lease_id>`; Hub validates and mutates in one critical section. Missing/expired leases return 409, including admin requests: admin cannot bypass active ownership. Other drivers keep their behavior. Lease is neither endpoint authentication nor same-account isolation.

Reconfirm the lease before each native submission. Lost claim responses, expired leases or missing local delivery records preserve outstanding/task reservations for reconciliation, never another claim/submission of the prompt. Persist outcomes first and send exactly saved `outcome + detail`; restart resends only that receipt. Unknown native outcomes stay uncertain rather than treating network failure as proof of no execution.
