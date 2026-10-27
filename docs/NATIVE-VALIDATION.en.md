[简体中文](NATIVE-VALIDATION.md) | [English](NATIVE-VALIDATION.en.md)

# Native integration acceptance records

Recorded 2026-10-14 through 2026-10-23. Earlier tests used macOS arm64; October 23 added a native Codex mailbox round trip between Windows 11 and macOS arm64. October 14 used WorkBuddy 5.8.2 and official CodeBuddy CLI 2.161.0; October 16 added an original local Claude-3p Desktop Code session with bundled Claude Code 2.1.299. WorkBuddy was 5.8.3 on October 19, when zero-model skill-maintenance checks were added. Only methods, results and scope are public: no credentials, personal contacts, real session or Hub IDs, IP addresses or private absolute paths. Other platforms, hosts and assistant combinations require separate acceptance; compilation is not a substitute.

## 2026-10-19: Managed skill synchronization, zero model calls

Existing managed skills for local Codex, Claude Code and WorkBuddy were explicitly updated/registered only after their original manifests passed validation, using one private Hub. Rule revision was `1`, body SHA256 `5cb0cf94da3b55863c0b219f924cde722c7d9ebace27c762ce49f90f4ce096f5`. Backups stayed outside the repository. Native tool permissions, plugin code and account grants were unchanged.

WorkBuddy retained its dedicated report helper as `binary_path`, adding only optional `hub_binary_path` for queries/maintenance rather than replacing reporting with the full CLI. All three returned `skills-status=current`, `host_load_state=unknown`; a second synchronization left all three modification times identical. Configuration made only those three enabled entries mutually visible, enabling no other endpoint or new task-send permission.

After actual Hub startup, each endpoint's directory contained only the other two and matching rule digests. Matching `known_version` returned no body. Native session state stayed unknown: no fake idle, business tasks or update wake messages. Native model calls for all maintenance were **0**. Correct disk/catalog state does not prove any old session's model read new text.

Host checks: Codex `0.163.0-alpha.10.2` actually discovered the skill through independent app-server `skills/list(forceReload:true)` without creating a thread/turn. Local Claude Code `2.1.299` contained `/reload-skills`, and official docs describe file watching while historical content remains. WorkBuddy `5.8.3` contained skill-directory file event listeners; a new CodeBuddy CLI `2.161.0` process scanned its selected profile. No Claude continuation, `/reload-plugins --force` or WorkBuddy extension reload was used to refresh. Grok cloud skill maintenance was not tested or included in local synchronization. See [host mechanisms and official sources](SKILLS.en.md#how-updates-take-effect-in-each-host).

Software verification passed Go race/vet and six Windows/macOS/Linux amd64/arm64 builds. Actual-Hub simulated protocol smoke retained one handoff, file checks and one return. Skill smoke covered three registered installs, idempotent timestamps, edited-file preservation, no reinstall after deletion, catalog version change after disabling and zero generated messages. Tests rejected downgrades, changed content under one revision and Hub/CLI source mismatch. Standard-library checks covered skill frontmatter/fences. Upstream skill validation did not pass because local PyYAML was absent; no dependency was added for it.

## 2026-10-14: WorkBuddy default resume and history

The Client used configured existing session ID, cwd and profile with `--resume`. Complete Envelope entered through a regular stdin file; private regular temporaries captured stdout. Only noninteractive output formatting was added: no `--tools`, MCP/model/permission overrides or persistence-disabling flag.

Before official CLI login, both full official CLI and desktop-bundled CLI resumed the original desktop session with `Authentication required`. The user then logged in personally in an official CLI profile outside the repository. An independent existing session under that profile resumed and remembered the previous turn, proving continuation for that independent CLI path.

The original WorkBuddy profile still failed default resume that day, including site-environment comparisons. One failed default-persistence execution using final argv/stdin appended the original history; its old byte prefix was verified intact. Failure, empty stdout or exit code 0 cannot justify safe resend. This October 14 observation does not override the successful original-path evidence on October 15 below.

Earlier diagnostics disabling tools or selecting `Bash,Read` changed conditions. Repeating history does not prove default tools or WeCom access. Profiles are not account-isolation boundaries; official implementations may share authentication storage. No desktop token/IPC ticket was extracted or login state forcibly rewritten. See [official configuration directories](https://www.codebuddy.cn/docs/cli/installation) and [headless mode](https://www.workbuddy.cn/docs/cli/headless).

## 2026-10-14: Independent CLI MQ execution and reporting

A real MQ message entered an existing formally logged-in independent CLI session through Python Client, retaining context and installed `agi-hub-client`. Its first call was denied noninteractive Read/Write/Bash, producing neither Reports nor artifacts. Hub kept work unfinished rather than treating chat explanations or CLI success as business completion.

After verifying zero earlier file writes/Reports, narrow rules covered only skill reads, private acceptance-workspace read/write and an endpoint-specific report helper, with an explicit absolute workspace, before resuming the same task. No `-y`, `bypassPermissions` or blanket Bash grant. The native session actively sent accepted/running/succeeded, created files and passed Hub SHA-256. Native file tools saved Report JSON, followed by direct helper `report --file`, without a pipe or second interpreter. See [official permission rules](https://www.workbuddy.cn/docs/cli/iam).

Resident SSE automatically claimed a second task and resumed the same existing CLI by default. It produced all three Reports and new files on its first execution without manual resume. Tasks used distinct execution IDs, results had independent message IDs/correct `reply_to`/`correlation_id`, and identical Submission/saved-success-Report retries did not increase task/result counts. Terminal resources were released.

The helper held only the endpoint token and called only `/v1/report`: no Hub admin config reads, assistant startup or endpoint switching. Report files were workspace-bound, rejecting escapes, symlinks and other subcommands. This proves independent CLI MQ execution/reporting/files, not execution of original WorkBuddy Desktop WeCom tools.

## 2026-10-14: Segmented Codex return-channel results

`codex queue` saved a diagnostic but only entered the current turn after the user manually selected guidance: queue evidence only. Two later real MQ artifacts were guided into the same original chat by the running Codex entry's in-host official `send_message_to_thread` using send-now, without manual clicks or queue as immediate-return evidence. Actual delivery receipts were retained.

The separate App Server Client used official `app-server proxy`, read only thread metadata/turn ID/status, and returned results through `turn/steer` with `expectedTurnId`. Idle startup was off by default; it neither resumed unloaded threads nor fell back to queue/new exec/resume. Actual proxy exited while connecting its control socket. This host managed its own app-server over stdio without exposing that daemon control socket. See [official protocol](https://learn.chatgpt.com/docs/app-server).

That failure describes only App Server proxy, not official Desktop MCP. Later independent Desktop MCP evidence follows.

## 2026-10-15: Original WorkBuddy session and profile resume

Using the original WorkBuddy session and `.workbuddy`, official `codebuddy` default `--resume` passed two read-only authentication continuations without tool/permission/MCP overrides. This verifies normal authenticated continuation on that original path; it must no longer be summarized as “the original profile cannot log in.”

A fresh contact search was initially denied local noninteractive Bash/Read and never reached remote execution. Precise allow rules were then corrected for outer JSON/shell-quote normalization based on the public command parser. Offline negative cases still rejected appended commands, changed keywords and redirections, without blanket Bash permission.

Another default original-session `codebuddy --resume` actually executed the fresh search through an absolute path to official bundled wecom-cli `1.3.4-workbuddy-local.281`, not PATH's other `1.1.0`. Native tool evidence was checked: `completed` with raw remote `errcode=850003`, `authorization expired`, not merely the assistant's final interpretation. No contact data was obtained.

The obstacle therefore moved from local rules to remote authorization: that robot's contact grant had expired. CLI login, actual search execution and data retrieval were recorded separately. Later user reauthorization enabled a new original-session query/MQ return below. `850003` remains historical, not a current blocker, while success still does not prove full desktop-tool injection.

Bundled 1.3 has no `auth` subcommand; 1.1 `auth status` was not used as evidence for it. No private desktop env/auth/ticket was read/copied, sandbox disabled or remote search blindly retried. Five temporary allow rules were restored to original settings bytes. No contact messages were sent.

## 2026-10-15: Independent Node immediate guidance through official Desktop MCP

An independent client ran under the official Node runtime in the natural host environment, spawned official Node/Desktop MCP server, initialized MCP/listed 57 tools, then actually called `send_message_to_thread` for the original chat. The ACK had `isError=false` and a matching thread ID; `codex_delegation` immediately entered the busy original chat without manually selecting guidance or using native queue.

This was a real independent-process MCP call, not the current model's internal sending tool masquerading as an external Client. Tool discovery does not authorize all 57 tools; this evidence covers only the selected original session's sender tool.

The same machine's Python parent chain failed macOS signature checks while official Node's natural host chain passed. This is launch-chain-specific, not “Python cannot use MCP.” Missing App Server daemon sockets were separate and not this Desktop MCP blocker. No new daemon or private application IPC was introduced as an imitation original chat.

This section records transport-level send/busy-guidance evidence. Resident Node MQ Client 0.2.0 later completed two real busy-return loops, and idle original-chat startup was tested separately below. None implies the scope of another.

## 2026-10-15: Complete busy-return MQ loops

Both real local busy-return MQ runs passed. Clients subscribed before new task submission. Each showed:

1. Hub persisted a task; SSE prompted WorkBuddy Client to claim it automatically.
2. Default `--resume` restored the fixed independent CLI coordination session without tool/permission/MCP overrides.
3. That native session actively sent accepted/running/succeeded and created actual artifacts.
4. Core verified device/workspace/relative path/SHA-256, then created a new terminal result linked to the original handoff through `reply_to`, retaining task/correlation IDs.
5. An independent Node consumer claimed the result and sent it through official Desktop MCP to the original Codex chat as `codex_delegation` inside its then-busy original turn.
6. No entry/operator manual forwarding, native queue or manual guidance clicks; file verification and terminal resource release were checked.

Distinct task-specific hubrun IDs accompanied four delivered task/result envelopes. Both current artifacts matched Core SHA-256 and resource locks were empty. The second artifact corrected return sequencing: execution assistant completion Report first, Hub verification/result creation next, Codex Client/Desktop MCP return last. The corrected artifact also returned automatically.

Thus two complete busy-return MQ loops passed locally. Execution used a fixed independent CLI coordination session; these acceptance-report artifacts are not contact retrieval evidence for the original WorkBuddy session. That later query and one entry-checked explicit continuation are below. A single Codex Desktop MCP idle start is recorded separately; a full idle MQ loop, complete WorkBuddy desktop-tool inheritance and other platforms remained unverified.

## 2026-10-15: One idle start through the same Desktop MCP

After the prior turn ended, an independent official Node program delayed once and called `read_thread` once before sending, verifying `thread.status.type=idle` and the previous turn `completed`. It did not loop/poll native state.

One official Desktop MCP `send_message_to_thread` returned a successful matching-target ACK. The original chat actually received input and started a new turn, whose ID differed from the completed one. Official desktop read_thread remained callable; no separate CLI/SDK session replaced the chat.

This separately verifies local original-chat idle startup through that official MCP, with actual pre-send idle state, one send, a real new turn and retained desktop tools, not merely a tool ACK.

No repeated wake, manual forwarding or new task occurred. This standalone diagnosis added no MQ business task and was not a complete idle MQ loop. Earlier independent CLI loops covered busy return. At that time, App Server proxy idle turn/start and other platforms still needed testing.

## 2026-10-15: Original WorkBuddy single-contact query and MQ return

After user reauthorization, business work bound the original `workbuddy` endpoint, session, cwd and `.workbuddy`, not the earlier independent CLI test session. A managed skill used an endpoint-fixed helper calling only `/v1/report`; artifacts had a separate workspace. Four report-install permissions narrowly covered skill reads, artifact-directory writes and the helper, not unrelated directories/commands.

The first real MQ input was delivered but not accepted under older diagnostic limits: zero tool calls, files and Reports were verified. Its stdin already was a new user message, so CLI history replay was not the explanation. Client then added a fixed short positional instruction identifying current work, while full Envelope remained stdin with no tool/permission/MCP/model override or authorization expansion.

One explicit continuation after entry checks was necessary. It retained task/resource locks and added operator continuation only to existing delivered detail: no requeue, new task/message IDs or fabricated Reports. The current Codex entry performed it under existing user authorization without another click; it was not an automatic Broker retry. Do not call this first-attempt automatic success.

Verified afterward:

1. Original-session `accepted/running/succeeded`, with all three event IDs individually matched to Core and one task/execution association.
2. Exactly one newer bundled official contact search. Raw `function_call_result` returned `users`; artifact name/department/title matched candidates, without claiming address-book uniqueness.
3. Core checked device/workspace/relative path/SHA-256 and generated a new result linked through `reply_to` with matching task/correlation.
4. Independent Node/Desktop MCP automatically guided that result into the busy original Codex chat without forwarding/clicks. Locks released, all messages delivered, WorkBuddy returned idle and its original history prefix stayed intact.

No private desktop env/auth/ticket was read/copied. Four temporary query permissions were removed; only four narrow report-install permissions remained. The helper ran outside the repository independently of a development checkout. Local denial, remote expired authorization and later success are sequential stages. Single-contact retrieval now passed; the old expiry is not a current blocker. Scope is original session, specified query, active reports, artifact checks and automatic busy return, not every WeCom operation, all GUI-tool injection or WorkBuddy OpenAPI acceptance.

## 2026-10-16: Original Claude-3p Code session

The host was an open local Claude-3p Desktop Code page, native entry `claude-desktop-3p`, Claude Code `2.1.299`. The in-session `agi-hub-endpoint` mod's final version was `0.1.3`; no separate CLI/SDK session executed the tested work. It bound the original UUID and retained the original transcript's byte prefix. Times below are Beijing time (UTC+8).

### N2: One idle wake

Run `/agi-hub-diagnose` once in the original session, then leave it idle. At 17:16:35 the transcript contained one plugin-sourced random-nonce message. At 17:16:45 Claude automatically started a turn and returned only that nonce, with no tools or duplicate messages. This proves actual idle wake, not only `prompt.submit` ACK.

Client `0.1.0` did not recognize the host-added plugin source wrapper, so local diagnosis still showed `submitted_unconfirmed`. `0.1.1` strictly matched the observed full wrapper and first-line marker, with positive/negative tests. N3 later confirmed the same wrapper and actual main-thread turn ID. To avoid another expensive long-context run, a fresh standalone N2 was not repeated on the final version. `idle_wake_verified` was enabled only after actual wake; Claude busy-result capability remained false.

### N3: Full loop completed after a fix and continuation

At 17:27:56 one MQ task was submitted, automatically claimed by the listening plugin and injected into the same idle Code session. Six synthetic status records required native reads, independent classification, JSON/Markdown artifacts, readback and native report-tool return. No Bash reporting, model API substitute or external CLI resume.

Hub saved the first accepted Report, but the handler incorrectly returned an object directly in native `result`, which the host rejected. Model retry of the identical accepted event deduplicated without a new task. The model still read input, created/read both files, but did not complete running/succeeded Reports. Completed files were not Hub task success.

`0.1.3` made success/error/unbound branches return JSON text and added a real host `$.tool.call` regression. Upgrade preserved private delivery/task records, with hot reload only waiting for the old lease to expire: no uninstall, cleared records or prompt resubmission. After independently verifying six judgments/counts, the entry sent exactly one user-authorized report-only continuation for the same task. It called only native running/succeeded, without file I/O, new task or redispatch.

Native running succeeded at 17:44:14 and succeeded at 17:45:06. Three unique complete Report fingerprints matched Core events and retained the `claude-turn:*` for the first actual main-thread turn. This does not mean all three Reports occurred in one physical turn. Both current file hashes matched Core and bytes were unchanged by continuation. Each of six states had one record; three needed human attention.

Core generated one result with correct `reply_to`, task/correlation inheritance. At 17:45:06 independent Node MQ Client automatically used official Desktop MCP send-now into the busy original Codex chat. It actually entered the current turn without entry-side manual sending, native queue or clicks. Envelope `mode=queue` was Hub business mode, not use of native Codex queue. Task/result were delivered, Claude idle and resources empty. Execution Reports replace delivery detail with receipt-confirmation text, so final detail need not preserve initial transport wording.

The loop therefore completed, recorded as “N3 completed after a fix and one report continuation.” First-round unattended acceptance did not pass, and no fresh final-version task was repeated to claim otherwise, avoiding extra long-context token use.

### Automation and support boundaries

Repository `go test -race ./...` and `go vet ./...` passed. Final Client had 30 Node state-machine tests, five host `plugin test` cases and `plugin validate`. The new native tool check verifies JSON-text ABI, not registration alone. Existing Python Client 54 and Codex Node nine tests passed. These request no Claude model. Private evidence stores original UUID, times, Report fingerprints, artifact hashes, transcript prefix and test output outside repository/packages.

Additional fixes replaced racy file-lock write/read with an atomic Hub lease, isolated binding changes, fenced late requests, waited through reload lease expiry, partitioned token fingerprints, reconciled lost claim responses and stopped treating `prompt.submit` resolution alone as delivery proof. Automated recovery/safety cases do not constitute native N1/N4–N9 verification. Zero-model unbound CLI commands also do not prove complete terminal integration.

Claude busy queuing, real needs_user prompts, recovery after desktop exit, reverse work with Claude as origin and Windows/Linux native integration still required separate acceptance. Claude idle execution/Codex busy return are covered here; complete business return with Codex itself idle remained unverified. Provider-debited credits were not measured, and the screenshot's cumulative spending was not attributed to this test.

## 2026-10-19: Pure-mailbox Codex ↔ original WorkBuddy session

The Hub remained a message pipe. Codex/WorkBuddy opted into mailbox protocol 1, reusing original Codex Desktop chat and WorkBuddy/CodeBuddy CLI session without a new coordinator chat. Claude model calls were zero; no WeCom data query or external messaging occurred.

The first request starting 16:38 resumed original WorkBuddy through a resident Client. Cached old instructions produced a help request without required stable `id`, rejected locally before Hub. Generic errors caused repeated edits to incorrect fields, so it was not unattended success. The helper's field errors were clarified and a short complete JSON example added to native notifications. After confirming old CLI exit/no unresolved delivery, Clients were updated and the original entry explicitly sent one formatting clarification in the same conversation, without redispatching the original message.

WorkBuddy then explicitly requested Codex. Independent Node/Desktop MCP send-now automatically guided the busy original chat without queue clicks or manual forwarding. Codex read synthetic input, summed `[3,5,8]` to 16 and explicitly replied with the nonce. WorkBuddy Client waited for first CLI exit and a real delivery receipt before resuming the same session for that reply. WorkBuddy independently reread/checked nonce/sum, wrote result.json and sent a new result to original Codex. It actually entered the current chat and was not generated by Hub.

The entry read back and checked nonce, total, checked_by and correlation_id and calculated SHA-256. WorkBuddy honestly stated its narrow permissions prevented shasum rather than claiming hash verification; Codex hashed without expanding permissions. Five messages, including one format clarification, shared correlation/original-entry context. No Task/Report, parent waiting or forced return tree was created. Real UUIDs/times/hashes remain private diagnostics.

Conclusion: **native two-way collaboration and artifact return passed after one format correction**. This does not claim zero-intervention first pass, three real assistants in parallel, Claude new-mail integration or native support on other platforms. Actual-Hub simulated native adapters separately cover three-direction free routing, shared limits, duplicate publication, silent status and retained uncertainty with zero models.

## 2026-10-20: Pure-mailbox original Claude Code session and Codex return

### Original session and configuration fix

The real host was macOS arm64, Claude-3p Desktop's already-open local Code session with bundled Claude Code **2.1.299** and in-session plugin **0.2.2**. The existing session UUID stayed unchanged; its previous transcript byte prefix had the same hash afterward. No independent CLI/SDK session replaced it. The host's bypass mode already existed and was unchanged: this test neither granted additional native permissions nor copied authentication data.

The Desktop host loaded the new plugin but passed empty activation `client_config` despite a saved plugin setting. The fix keeps nonempty activation options first; otherwise it reads only its own path from merged settings using the SDK's `$.plugin.name` string property and `$.settings.read()`. An unqualified manifest name checks bare and own marketplace-qualified settings together; conflicting paths stop instead of selecting one. Other plugins' configurations and credentials are not selected. This fix was offline-checked before the real mailbox request.

### Actual mailbox loop

All times below are Beijing time (UTC+8). At **11:48:16.902**, the leased Client automatically submitted one MQ request through `$.prompt.submit` into the idle original Code session. Its exact marker was observed in the real main-session `turn.start`, confirming delivered input rather than relying on the submit promise alone.

The native tool calls were exactly one **`Read`**, for a **54-byte synthetic input file**, and one **`mcp__agi-hub-endpoint__mail_send`**, explicitly sending a result to the original Codex entry. The file's nonce and sum **51** matched the result. Claude used no Bash, legacy report or further assistant delegation, ended with “已回传”, and was idle at **11:48:40.870**. The result was an explicit assistant send, not a Hub-generated business reply.

The independent Codex Node Client automatically used official Desktop MCP **send-now** to deliver that result into the busy original Codex chat. Actual receipt was confirmed there, without human forwarding, native queue selection or a click to guide. The conversation used **two delivered mailbox messages within a four-message cap**: one request and one result. Existing Task count stayed **7 → 7**, while message count changed **18 → 20**. No new Task/Report workflow or forced return tree was created.

Conclusion: **original-Claude idle mailbox execution and explicit result return to busy original Codex passed**. This was one native business turn after configuration fixes, not a claim that installation/preflight required no intervention.

### Model usage and preflight incident

That one native business turn produced **three actual model API responses with separate usage IDs**. The first wrote **272,896 input tokens** to cache; later responses reported cache hits of **272,896** and **273,092** tokens. The three responses produced **363 output tokens in total**. One native turn is not one model request, and this was not zero-cost execution. The small input file did not remove the existing session's long context. A mailbox message cap limits messages, not the host's model requests or provider credits; actual credits charged were not measured here.

Before this business trial, a separate CLI configuration probe intended to execute only a slash command unexpectedly invoked the model. The initial private audit contains **at least two usage records**, with **63,840 cache-creation input tokens** and **2,218 output tokens** visible in those records. Nested attempts mean the total request count and total consumption remain unknown; these figures are not a complete bill. The probe was stopped and private audit evidence preserved. No further CLI `-p` probe was used. **A slash command passed to `claude -p` is not guaranteed to avoid model calls.** Offline checks use Node tests and official `plugin validate` / `plugin test`; native model trials are explicit and counted separately.

The Desktop UI also displayed a provider-health `ERR_CONNECTION_CLOSED` banner. The actual Code business turn, native tools and return nevertheless succeeded, so that banner is not recorded as a blocked native integration.

### Verified scope

This pass covers only this macOS arm64 Claude-3p Code version, the existing idle Claude session and return to the busy original Codex chat. Claude busy queuing, steer, reverse work initiated by a human in Claude, Windows native integration and other hosts remain unverified. No extra paid N2 or repeat business task is needed to establish the observed idle wake. The October 16 legacy N2/N3 and October 19 WorkBuddy records above retain their original conclusions; neither is rewritten as evidence of this new mailbox pass.

Real session IDs, paths, nonces, usage IDs and transcript hashes remain private and are excluded from the repository and packages.

## 2026-10-23: Windows Codex idle execution and return to the original macOS entry

The running code was `1387677`, with Hub **0.4.0**. macOS arm64 and Windows 11 used paired, isolated lab Hubs. The user started an independent Node Client from the existing original Windows Codex chat; it passed official Desktop MCP preflight and subscribed to SSE. Its launch configuration contained the old custom `native.receive_tasks` field. The Windows assistant backed it up and removed only that key. Strict unknown-field validation remained enabled; offline regression checks covered both rejection of the old field and successful removal of that single key.

After confirming that the Windows bootstrap turn had completed and the original chat had no active turn, one MQ request containing a nonce and `17+29+43` was sent. The independent Client used official **send-now** to start a new native turn in the original chat, beginning at **01:54:46.300 UTC** and completing at **02:02:23.740 UTC**. The Windows model used its own native exec tool to save the complete result JSON and invoked the lab's endpoint-fixed helper with `mail-send` to explicitly send one result, exiting with code 0. The saved JSON's nonce, answer **89** and canonical association matched the Hub records.

The independent macOS Client then automatically guided that `kind=result` into the original Codex chat where the user first asked, which was busy at the time. Actual native input was received there, and official `read_thread` confirmed it was still the same active turn. `correlation_id`, `reply_to` and `root_origin` stayed associated, using **2/2 messages of the two-message budget**, without manual forwarding or selecting a native queue. Grok assisted only with evidence collection and administrative state registration after native observation; it did not send the request or result on either assistant's behalf. After confirming no new Windows active turn, it restored the lab's idle state. This trial made **0 Claude calls**; the original Windows production Hub, Client and old queue were unchanged.

**The verified scope is this small synthetic task's cross-machine mailbox path: idle wake in the original Windows chat, native tool execution, an explicit result send and automatic return to the busy original macOS entry.** Windows busy-session receipt was not tested, and its busy verification flags remain false. Session state was registered from actual observations; continuous automatic state tracking and unattended recovery across the full lifecycle remain unverified. This pass does not cover other assistants' cross-machine business work. Source code permits different caller and target chats, but this trial started from the target's original chat and does not validate that alternative launch path.

Real session and Hub IDs, IP addresses, the nonce, private paths and original delivery/turn evidence remain outside the repository and are excluded from public documents and packages.

## 2026-10-27: real-state reconstruction after a Hub restart

At 07:12 UTC, an isolated loopback Hub and empty mailbox tested Node Client `0.3.1` while the original local Codex Desktop chat remained active. On first connection, `unknown` became `busy` with the actual in-progress turn ID confirmed by native `read_thread`. Restarting only that test Hub reset state to `unknown`; the Client automatically reconnected and reconstructed the same real state. Initial drain, the initial SSE event and the first state broadcast caused three metadata reads per connection, then stabilized. Six total reads formed no feedback loop.

The test attempted no native send, submitted no business message and changed no wake-verification flags. Additional model calls and Claude calls were both zero. The existing main Hub, business Clients and C's configuration stayed unchanged; test processes were cleaned up. This proves only **state reconstruction after a Hub restart while the native Client already runs**. It does not verify OS restart, automatic host startup, continuous busy-to-idle notifications or C's Windows GUI acceptance. Native turn IDs, private configuration and original evidence remain outside the repository.

## Further acceptance work

- Keep separate evidence for the earlier two independent CLI busy returns, original-session single-contact work after an entry-checked continuation, one idle-start diagnosis and the October 23 cross-machine mailbox loop. Receipt of a complete business result while the original macOS entry is idle still needs separate acceptance, retaining no-resend behavior after disconnect/timeout/uncertainty.
- Test other WeCom/desktop capabilities only under user authorization. A matching single candidate proves neither every function nor whole-address-book uniqueness. Retain local denials, remote 850003 and reauthorized success history without blind retries.
- Verify App Server proxy connectivity/idle turn/start independently; Desktop MCP idle success is not a substitute. Native prompts, associated receipts and artifacts follow each transport's evidence.
- Verify official launch chains separately on Windows/macOS hosts outside this trial's scope and on Linux; do not generalize success on these two specific computers to every platform.

WorkBuddy OpenAPI `localassistant/message` remains a separate candidate requiring an app and personal OAuth; that real grant is not configured here. CLI login does not grant it, and it is not an arbitrary ordinary desktop-chat targeting interface. See [official OpenAPI](https://open.workbuddy.cn/docs/openapi).

Back to [Clients](CLIENTS.en.md) · [Integration scope](ADAPTERS.en.md)
