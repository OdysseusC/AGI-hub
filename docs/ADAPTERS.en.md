[简体中文](ADAPTERS.md) | [English](ADAPTERS.en.md)

# Adapters and support scope

This page distinguishes implemented delivery transports, official candidate capabilities and native verification still needed on the current machine. Using the same model's API or a new CLI/SDK session does not connect the desktop assistant the user is actually using.

## Current independent Clients

| Directory | Native entry | Supported modes | Verification so far |
| --- | --- | --- | --- |
| `clients/codex` | Official Desktop MCP / existing-thread queue / app-server result guidance | `queue` / results only | Independent Node Desktop MCP guidance verified on 2026-10-15; resident Node Client passed a full MQ busy-return loop. App Server proxy lacked its control socket; verify the three transports separately. |
| `clients/workbuddy` | Official CLI session resume or local-assistant OpenAPI | `queue` | Independent CLI MQ passed. After user reauthorization, entry checks and one explicit continuation, the original session passed a fresh single-contact query, active reports and automatic busy return. Full desktop-tool inheritance and OpenAPI remain unverified. |
| `clients/grok-routine` | Local file + ID-only official webhook, or compatible body template | Legacy `queue`, mailbox `0` | Fixed Windows sessions passed bidirectional Task/Report on 2026-10-16; other installations and addressed mailboxes are unverified. |
| `clients/claude-code` | In-session Claude Code mod using `prompt.submit`, native mailbox tools and legacy report | `queue` | Client 0.2.2 passed idle mailbox execution in the existing macOS Claude-3p 2.1.299 Code session and an explicit result return to busy original Codex on 2026-10-20. Historical N2/N3 evidence remains separate. Busy queuing/other platforms remain unverified. |

The Hub contains no assistant drivers. Python Clients use Python 3.11+ standard libraries. Independent Codex Desktop MCP sending was tested with Node; a resident MQ Client is available and has local simulations. Each product directory holds its adapter, manifest and configuration example; shared code handles only the local protocol. Claude Code runs its Client through host mods APIs. Implementations share the local REST protocol, with SSE or event long polling for wake notifications. See [Client configuration and startup](CLIENTS.en.md).

Clients do not read other assistants' credentials or history. Native parameters come only from user-configured private files. Program output and HTTP response bodies are not exposed as public delivery diagnostics. One repository currently holds one directory per assistant; separate repositories can follow maintenance/release needs later.

Current addressed mail is implemented by Codex Desktop MCP, WorkBuddy CLI and Claude Code mod 0.2.2; Grok, WorkBuddy OpenAPI and Codex queue/app-server keep mailbox 0. The historical task/report evidence below does not prove a new mailbox loop.

## Codex Desktop MCP

On 2026-10-15, an independent Node client in the natural host environment used the official Node runtime to start the official Desktop MCP server, completed initialize and tools/list, and discovered 57 tools. A real `send_message_to_thread` call returned `isError=false` and a matching thread ID ACK. The message also entered the busy original chat immediately as `codex_delegation`, without manually selecting guidance or using native queue.

A Python parent chain on the same machine failed macOS signature verification; the official Node launch chain succeeded. This is a constraint on the actual host/launch chain, not a language's general capabilities. Preserve that boundary rather than copying credentials or assembling private IPC. Discovering all tools does not authorize the Hub to call them all; the user fixes the target thread and permitted sending tool.

This evidence came from a real independent process, not a model's internal tool sending on its behalf. Later, resident Node MQ Client 0.2.0 subscribed before a new task and completed MQ → fixed independent WorkBuddy CLI session → native reports/file checks → associated result → independent Desktop MCP. Results entered the busy original chat directly, without manual forwarding, native queue or clicks, and resources were released. Two local busy-return MQ runs passed. A separate diagnosis through the same official MCP also started the idle original chat: the old turn was completed, the new turn differed and official read_thread remained available. That idle diagnosis created no MQ task; a complete idle MQ business loop remained unverified. See [local records](NATIVE-VALIDATION.en.md).

## Codex queue

The Client invokes separate argv arguments without a shell:

```text
codex queue --thread <native session ID> --message <complete notification>
```

The example creates only a disabled binding. The user supplies actual IDs and verified values:

```json
{
  "endpoint": {
    "id": "codex",
    "device_id": "this-device",
    "session_id": "",
    "enabled": false,
    "idle_wake_verified": false,
    "result_queue_while_busy_verified": false,
    "modes": ["queue"],
    "driver": "codex"
  },
  "driver": {
    "type": "external"
  }
}
```

`thread_id / executable / timeout_seconds` belong in the independent Client's private `native` object. The Hub stores only endpoint bindings and capability declarations. Notifications include the complete envelope, existing authorization scope and reporting instructions. Result notifications display results only; they must not restart the original task or recursively report it again.

Official `codex queue` accepts an existing session UUID or exact name. Use a UUID for duplicate names; image attachments are unsupported. This Client implements only the local default connection and does not expose `--remote`. See the [official queue reference](https://learn.chatgpt.com/docs/cli/reference).

Official App Server provides `thread/resume`, `turn/start`, `turn/steer`, `turn/interrupt` and execution events; steer must match the active turn. This project implements a results-only App Server branch with protocol simulations. The current host did not expose a separate proxy, and ordinary task control/external desktop guidance have not passed native verification. Starting a separate App Server does not prove reuse of the existing desktop session or its native tools. See [official App Server](https://learn.chatgpt.com/docs/app-server).

On 2026-10-14, two WorkBuddy results were guided back to the original chat using Codex's in-host official send-now tool. Independent Desktop MCP later supplied the evidence above. It is a different entry from App Server proxy: the missing daemon control socket does not disprove Desktop MCP, and Desktop MCP success does not prove proxy connectivity. The resident Node Client passed complete busy MQ return and the same Desktop MCP passed one idle-start diagnosis. Neither verifies App Server proxy's idle turn/start. Ordinary task control and other platforms require separate checks.

## Webhook / Grok Routine

The webhook URL and body must come from the user's verified platform configuration. The Client does not guess official routes, payloads or authentication. They live in private `native.url / native.headers / native.body`. A cloud Routine also needs a real authorized reporting path back to this machine; request success alone does not prove the loop. There is no cross-device relay or public ingress deployment.

`body` must be valid JSON. The following template rules are syntax examples, not an actual Routine request body:

| Placeholder in a JSON string value | Replacement |
| --- | --- |
| `{{content}}` | Envelope content as unchanged text |
| `{{envelope_json}}` | Complete Envelope JSON escaped as a string |
| `{{task_id}}` | Associated task ID |
| `{{message_id}}` | Message ID |
| `{{notification}}` | Complete Envelope plus acceptance/reporting instructions |
| Entire string value exactly `{{envelope}}` | Complete JSON object |

Replacement operates on structured JSON values before serialization. It neither concatenates unescaped content nor interprets placeholders inside task text a second time. `{{envelope}}` cannot have a string prefix/suffix. A template must carry the full envelope or notification; a summary alone cannot replace correlation IDs, authorization and file references.

Only HTTPS or HTTP to localhost/loopback is allowed. Redirects are not followed, preventing configured headers from reaching another address. In template mode a 2xx means `delivered`; non-2xx responses, disconnects or timeouts after an attempt remain `uncertain`. Clearly invalid configuration is `failed`.

Legacy file-webhook separately passed native Task/Report acceptance on the fixed Windows installation on 2026-10-16. Old file-bridge evidence and new Hub evidence remain distinct; neither validates the new mailbox path. See the dated Windows record below.

## Extend with your own Client

Add an assistant directory implementing local protocol consumption and its native adapter; no product-specific Go Hub branch is needed. Python Clients call `validate_binding(binding)` to verify the actual destination, then `deliver(envelope) -> (outcome, detail)` for one delivery attempt, optionally sharing the common runtime. The Hub neither executes arbitrary configured commands nor automatically loads unknown modules.

For legacy Task/Report, before declaring `steer` or `interrupt`, confirm that the native entry targets the bound session/run, supports that meaning and can return the matching `run_id` and actual execution receipt. The kernel requires `target_run_id` to match an active sidecar-managed run. Sending control text as an ordinary queued message is not equivalent. Supplied Clients have no passing native verification for these modes; candidate code and local simulations are insufficient.

`result_queue_while_busy_verified` separately declares busy receipt of `kind=result` and defaults off. It is not `steer` or `interrupt`. Ordinary tasks, unknown state, unresolved delivery and resource coordination still follow the [message protocol](PROTOCOL.en.md).

Failure to start a process is `failed`; a nonzero exit, timeout or cancellation after startup is `uncertain`. Successful exit confirms transport only. Legacy native task completion still requires Reports and artifact checks; new mailbox work returns explicit assistant messages and does not use Report.

## Claude Code in-session plugin

`clients/claude-code` provides the `agi-hub-endpoint` mod directly inside the user's bound original Claude Code session. It verifies the destination with official `$.session.id()`, submits the complete plugin-sourced handoff through `$.prompt.submit({text})`, and registers fixed-identity native `peers`, `mail_send`, `mail_state` plus legacy `mcp__agi-hub-endpoint__report`. It does not start another `--resume` process, impersonate a user message or report through Bash. See [configuration and installation](../clients/claude-code/README.en.md).

Every Code session loading the plugin gets diagnostic commands, but only the session matching private configuration and the Hub binding consumes messages. An atomic Hub Client lease replaces a non-atomic lock file; another process cannot claim/report simultaneously for that endpoint. Session changes stop the old listener and revalidate the new session. Persistence is isolated by Hub address, endpoint and session. Restart reconciles saved delivery receipts rather than resubmitting claimed prompts.

Client 0.2.2 supports mailbox 1 after negotiating Hub `mailbox_long_poll_max_ms:25000` and `mailbox_client_lease:true`. It uses leased `/v1/mail/inbox` and native sends rather than Bash, automatic Reports or automatic business replies. Old Hubs missing these capabilities are rejected before claims. Only queue is supported; no busy steer. The 2026-10-20 macOS original-session idle mailbox loop passed, including native Read, one explicit result send and independent return into busy original Codex. See the [dated evidence and cost limits](NATIVE-VALIDATION.en.md#2026-10-20-pure-mailbox-original-claude-code-session-and-codex-return).

The mods HTTP API cannot read streaming SSE, so the Client uses inbox long polling up to 25 seconds. Message submission events wake waiting requests immediately. A `null` timeout starts another wait without querying business history. Native delivery confirmation, execution reports and artifact checks remain separate; ambiguous HTTP or `prompt.submit` results are not retried.

Development and validation used Claude Code **2.1.299**. On 2026-10-16, the original macOS Claude-3p Code session passed one N2 idle wake. N3 automatically accepted work and created artifacts; after fixing report-tool output, one explicit report continuation let Core verify both files and automatically return to the busy original Codex chat. The MQ task was not redispatched, and no fresh final-version unattended task was repeated. Busy queuing was not tested. See [actual process and boundaries](NATIVE-VALIDATION.en.md#2026-10-16-original-claude-3p-code-session).

macOS, Windows and Linux share dependency-free plugin source, but host versions and native capabilities require separate verification. The target Code session must remain open with the plugin loaded. The plugin does not launch an exited desktop application.

Ordinary Claude Chat, Cowork and WSL Code sessions are unsupported. Desktop Code and terminal-interactive Code results are recorded separately. Cross-session sockets, Channels and separate CLI resume are not fallback delivery paths, to avoid silently changing execution environments. See [official mods API](https://code.claude.com/docs/en/plugins/mods/api) and [plugin installation](https://code.claude.com/docs/en/discover-plugins).

## WorkBuddy: CLI and official local assistant

Client `0.2.1` explicitly selects `native.transport="cli"` or `"openapi"` without automatic switching. Old configurations omitting `transport` retain OpenAPI behavior. CLI resume is a candidate for existing ordinary sessions; verify environment, desktop identity and tool retention separately. Independent CLI execution alone is not a WorkBuddy Desktop connection. See [configuration and operation](../clients/workbuddy/README.en.md).

### CLI session resume candidate

The CLI uses the user's official login and configured absolute executable, working/configuration directories and session ID. It receives `--resume ID --print --output-format json --input-format text` plus the Client's fixed brief positional instruction for the current handoff. The complete notification enters through a regular stdin file; task text is absent from argv, and complete stdout is parsed from a regular file. Windows may use actual `node.exe` plus the official JavaScript entry instead of a `.cmd` wrapper, without a shell. No tool, permission, MCP, model or profile override is passed; resumed chats persist by default. Fixed instructions do not expand user authorization. See [official headless execution](https://www.workbuddy.cn/docs/cli/headless).

In the formally logged-in private CLI configuration, the first MQ task was explicitly denied by native permissions and confirmed unexecuted. Narrow skill-reading, actual-workspace file access and scoped-report-helper rules plus an absolute working directory were corrected before resuming the same task. SSE automatically claimed the second task, resumed the same existing independent CLI without tool/permission overrides, and completed on its first execution. Both had native `accepted/running/succeeded` and matching SHA-256 artifacts. Their execution IDs differed, result associations were correct, duplicate submissions/reports were deduplicated and terminal resources were released.

Default resume of the original WorkBuddy configuration/session returned `Authentication required` on 2026-10-14. Two default read-only authentication continuations succeeded on October 15. A new contact query first failed local Bash/Read permissions; after precise rules were corrected, official bundled wecom-cli `1.3.4-workbuddy-local.281` executed and returned remote `850003 / authorization expired`. These are historical stages. After user reauthorization, a fresh query in the original endpoint, session, working directory and `.workbuddy` returned candidates matching the artifact, followed by native `accepted/running/succeeded`, Core file verification and independent Node Desktop MCP automatic busy return. The first delivery was not accepted; after verifying zero tools, files and Reports, the entry explicitly continued once, retaining task/resource locks without requeue or fabricated receipts. The original history prefix remained intact. This must not be described as automatic success on the first attempt.

Failed default-persistence attempts can append history. The original prefix was verified intact on October 14; failure must not be interpreted as no original-session change. Preserve actual results for reconciliation instead of blindly resending. Narrow skill, report and file permissions passed independent CLI execution, without authorizing other directories/commands. Personal login may use shared product authentication storage; separate configuration directories do not isolate credentials. Protected tokens are not read or copied. See [local verification](NATIVE-VALIDATION.en.md).

### Official persistent local-assistant OpenAPI

Official Open API provides local-assistant status, delivery and history:

| Capability | Official endpoint | Scope |
| --- | --- | --- |
| Read-only online status | `GET /openapi/v2/localassistant` | `user.localassistant.readable` |
| Invoke local assistant | `POST /openapi/v2/localassistant/message` | `user.localassistant.invokable` |
| Read replies | `GET /openapi/v2/localassistant/message` | `user.localassistant.readable` |

The domain is `https://www.workbuddy.cn`. Delivery returns `message_id`; the docs do not provide ordinary desktop-task `session_id`, device selection or a local-assistant completion event. Incremental history polls for replies. Cloud `/tasks` and ACP SSE are cloud-session features, not local-assistant capabilities. The Client implements official message delivery but has not completed a local interface test with real authorization. It does not poll history instead of receiver-originated Reports. See [official Open API](https://open.workbuddy.cn/docs/openapi).

The Client uses `Authorization: Bearer access_token`, body `{"content":"complete notification","msg_type":"text"}` and formally granted OAuth `user.localassistant.invokable`. Missing configuration prevents claims. It does not read desktop Cookies, cached tokens or private IPC tickets. Development configuration accepts only a user-explicitly-granted short-lived access token, without app secrets, refresh tokens or automatic renewal.

After HTTP success, it also checks that official JSON has strictly integer `code=0` and a valid nonempty `data.message_id`. Otherwise delivery remains uncertain with no automatic resend. Even a valid delivery receipt does not prove business execution/completion.

The official assistant uses a fixed dedicated directory and one persistent session, unlike ordinary multi-session tasks with optional working directories. Third-party APIs require app review and user OAuth. Existing desktop WeCom login does not grant that third-party app access. Long-term authorization/renewal for a purely local open-source install remains to be confirmed. See the [assistant guide](https://www.workbuddy.cn/docs/workbuddy/From-Beginner-to-Expert-Guide/Function-Description/Assistant) and [third-party integration](https://open.workbuddy.cn/docs/third-party-app).

Native assistants also need the [report skill](SKILLS.en.md) and actual acceptance/reporting verification. The skill interprets messages/reports; the Client consumes externally and wakes sessions. Neither replaces the other. Supervised Computer Use may verify a native entry, without proving official API authorization or ordinary desktop-task background wake capability.

Separate actual evidence covers independent WorkBuddy CLI MQ execution/artifacts, original-session authentication continuation, and the reauthorized original-session single-contact business flow with automatic busy return. The last required one explicit continuation after entry checks, verified only matching returned candidates, and did not prove address-book uniqueness, all WeCom operations or complete desktop-tool injection. Four temporary query permissions were removed; four narrow report-installation permissions remained. Resident Codex busy MQ return and one original-chat idle start through the same Desktop MCP were tested. The idle diagnosis added no business task; a complete idle MQ loop and App Server proxy remained unverified. CLI/OpenAPI selection stays explicit, and CLI login grants no third-party OpenAPI permission. See [local records and CLI basis](NATIVE-VALIDATION.en.md).

## Native integration acceptance

Verify each adapter's actual bound session, idle acceptance, busy behavior, permission prompts, associated receipts, artifact files, return to the original entry and failure reconciliation. State the tested OS, host version and session type. If an entry can only start a separate CLI/SDK session, label that difference.

Back to [README](../README.en.md) · [Architecture](ARCHITECTURE.en.md) · [Installation guide](INSTALL-FOR-ASSISTANT.en.md)

## Windows legacy task/file integration

Codex Node Client `0.3.1` can explicitly enable `native.receive_tasks=true` for legacy `kind=task / mode=queue`, after fixed original-chat instructions and an endpoint-scoped report helper are installed. It defaults to results, rejects control and does not alter mailbox negotiation, Hub resource/session gates or verified capability flags. The original chat submits real Reports; the Client only delivers. Python Desktop MCP/App Server remain results-only, Python queue retains its existing task behavior and Python rejects this Node-only option. [Codex details](../clients/codex/README.en.md)

Grok Client `0.2.0` uses `native.transport="file-webhook"`, an existing absolute `inbox_dir`, fixed Routine label and private URL/headers, without `body`. It publishes the full Envelope and report instructions atomically, then sends only the SHA-256-derived ID. Persistent directory claims work on exFAT; the Grok receiver separately saves processing state and stable Reports. File mode accepts only HTTP 200 as delivered, treats 401/403 as failed and retains other attempted/ambiguous deliveries as uncertain without retry. Omitted transport keeps the original full-body template and its 2xx semantics. Do not enable mailbox protocol 1 for Grok. [Configuration and receipt rules](../clients/grok-routine/README.en.md) · [Fixed Routine/report template](../clients/grok-routine/GROK_INSTRUCTIONS.en.md)

The 2026-10-16 Windows self-check and independent bidirectional Codex/Grok run verified actual native file reading, Reports, artifacts/SHA-256, associated result consumption and released resources. Existing sessions and tested tools remained available. The scope does not include Windows Claude, all native tools, other installations or new busy-capability flags. [Dated acceptance](NATIVE-VALIDATION.en.md#windows-grok-roundtrip-20261016)
