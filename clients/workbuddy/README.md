# WorkBuddy Client

[English](README.md) | [简体中文](README.zh-CN.md)

Version `0.3.0`, Hub protocol `1`, optional addressed mailbox protocol `1`, requiring adjacent `shared` runtime `0.2.0` and Python 3.11+. It uses the Python standard library. When copying this directory alone, also copy matching shared files and preserve their adjacent directory layout.

Addressed messages support only the verified CLI transport. Explicitly set `mailbox_protocol_version=1` and permitted peers on the user-selected endpoint. OpenAPI does not support it yet; the Client rejects mismatched capability configuration before claiming messages. Once enabled, it consumes only `/v1/mail/inbox`, without mixing in legacy tasks. The CLI resumes the fixed original session for `request/reply/result`. The skill decides whether to act and whom to message, without a forced parent/child return chain or automatic business replies. `status` is stored and acknowledged without starting the CLI or model.

The Client persists a `/v1/mail/delivery` receipt only after the CLI exits and the complete final JSON matches the fixed `session_id` with `is_error=false`. Delivery retains its occupancy lock to prevent concurrent resumes of the same session. Mailboxes have no task Report, waiting state, continuation receipt or settled interface. Model-generated progress cannot replace delivery evidence for the original message. Timeout, authentication errors, mismatched output and lost receipts retain their state for reconciliation, without blind retries. Busy-session resume remains disabled.

The fixed endpoint helper retains legacy-task `report` and adds `peers`, `mail-send --file <JSON-in-workspace>` and `mail-state --correlation-id <known-correlation-ID>`. It uses only its own endpoint token and cannot manage a leader, change sessions or call native assistants directly. The skill explicitly chooses recipients for replies and final results with `mail-send`, preserving `correlation_id`. Final results go to the original session identified by `root_origin`. Noninteractive permissions should cover only the relevant commands of this fixed program and the selected workspace; do not add shell or tool overrides.

The Client explicitly selects official CLI session resume or the official persistent local-assistant OpenAPI, supporting only `queue`. Real MQ acceptance passed with a standalone CLI on this machine. After the user renewed authorization on 2026-10-15, a fresh single-contact query in the original WorkBuddy session, working directory and original `.workbuddy` also completed native reporting, artifact verification and automatic return to the busy entry. The first turn did not accept the work; the entry reconciled it and explicitly continued once. Candidate fields matched the artifact, which does not establish uniqueness across the full directory or inheritance of every desktop tool. The manifest requires separate verification per transport, host and platform. `cli_validation` records contact lookup and active reports from the original local session after explicit continuation, while `desktop_inheritance_verified=false` remains. OpenAPI has not passed acceptance. [Local validation record](../../docs/NATIVE-VALIDATION.md)

After confirming that the first acceptance task was explicitly rejected by the native application without execution, narrow permissions were configured for skill `Read`, actual workspace `Read/Write` and the dedicated reporting helper's `Bash` execution, and the absolute working directory was corrected. The original task then resumed. The native assistant actively reported `accepted/running/succeeded` and generated a file with a matching SHA-256. SSE automatically claimed a second task, using default `resume` on the same existing CLI session. Its first execution emitted all three native receipts and wrote a new artifact version with a matching SHA-256, without tool or permission override arguments.

The two tasks had distinct execution identifiers and correct result correlations. Duplicate Submissions and persisted Reports were deduplicated, and terminal resources were released. The two October 14 artifacts returned to the original chat through tools inside the Codex host. On October 15, an independent Node process also sent through official Desktop MCP: both a successful ACK matching the thread ID and immediate `codex_delegation` input in the busy original chat were observed, without manual clicks or a native queue. After subscribing, resident Node MQ Client 0.2.0 automatically completed another real task: resume the fixed standalone CLI coordination session, receive native accepted/running/succeeded reports, verify the file and SHA-256, then send the correlated result immediately to the busy original chat through independent Desktop MCP. Two complete MQ busy-return runs passed. The second delivery corrected the documented sequence to “Report → Hub verification and result → Client consumption → Desktop MCP return.” Both current files matched Core SHA-256 values, tasks used different hubrun identifiers, all four corresponding messages were delivered, and resources were released. A later independent diagnostic on the same official Codex Desktop MCP channel started the idle original chat: the old turn was completed, the new turn differed, and read_thread remained available. That diagnostic created no new MQ task. The two earlier standalone CLI full MQ round trips verified busy returns; a complete idle MQ round trip remains unverified. The missing App Server proxy control socket does not make Desktop MCP unavailable.

The original WorkBuddy configuration path reported a login requirement on October 14; two authenticated continuations succeeded on October 15. A new query first encountered local noninteractive Bash/Read restrictions. After exact rules were corrected, it executed but the remote service returned `850003 / authorization expired`. The user then renewed authorization. This business run bound directly to the original `workbuddy` endpoint, session, working directory and `.workbuddy`, without borrowing the standalone CLI test session. The first delivery arrived, but the model did not accept the work because of old diagnostic constraints. Reconciliation confirmed zero tools, files and Reports. Stdin was already a new user message; failure to accept does not mean the CLI replayed history.

The entry reconciled and explicitly continued once, preserving the same task and lock. It recorded an operator continuation only in the existing `delivered` detail, without requeueing, a new ID or fabricated receipts. The Client gained a fixed current-handoff positional instruction; the full Envelope still goes through stdin. The native assistant then genuinely reported `accepted/running/succeeded`, executed the newer officially bundled contact search once, and received a successful raw tool result containing `users`. Candidate name, department and role matched the artifact. Core checked the device, workspace, relative path and SHA-256, generated a correlated result, and an independent Node Desktop MCP process automatically guided it back to the busy original entry. The lock was released and the original history prefix remained intact. The current entry performed reconciliation and explicit continuation under existing user authorization; this was neither another user confirmation nor an automatic Broker retry. Do not describe it as automatic success on the original session's first turn, or as verification of full-directory uniqueness, all WeCom operations or complete desktop-tool injection.

This run selected the absolute path to officially bundled wecom-cli `1.3.4-workbuddy-local.281`. Another `1.1.0` on the default PATH was not the execution entry. Bundled 1.3 has no `auth` subcommand; 1.1's `auth status` cannot establish its login state. The temporary five exact allow rules from the earlier remote-authorization diagnosis were removed by restoring settings to their original bytes. Offline counterexamples against the public parser confirmed rejection of appended commands, changed keywords and redirects. The managed skill used a fixed endpoint-scoped `/v1/report` helper and a separate artifact workspace. Four reporting-install permissions covered only the skill, artifact directory and specified helper. Four temporary single-query permissions were removed; only the four reporting-install permissions remained. No private desktop env/auth/ticket was read or copied, the sandbox was not disabled, and remote search was not retried blindly.

## Configure and run

Register the user-selected endpoint and set its binding to `"driver": {"type": "external"}`, with `endpoint.driver` equal to `workbuddy`. For CLI, `endpoint.session_id` must equal `native.session_id`. For OpenAPI it must be `localassistant`, identifying the official persistent dedicated assistant. The Client checks the binding before subscribing and claiming on every connection. Export private configuration:

```sh
agi-hub client-config --endpoint workbuddy --file /absolute/path/to/private/workbuddy/client.json
```

The command exports only endpoint credentials and empty `native`, without overwriting an existing file. Configure the selected transport using [client.example.json](client.example.json). The default local Hub is `http://127.0.0.1:7331`; use the exported configuration. Run from the repository root:

```sh
python3 clients/workbuddy/main.py --config /absolute/path/to/private/workbuddy/client.json
```

On Windows, use `py -3` and that computer's absolute paths. Keep personal configuration outside the repository. Do not run two Clients for the same endpoint concurrently. Do not set `idle_wake_verified` to `true` before accepting wakeup and reporting in the target session.

The fixed `session_id` identifies a long-lived session, not a task's `run_id`. Reports use the actual native execution ID. If none is public, use the notification's task-specific `hubrun-...` and explain that it belongs to the Hub. Keep one execution identifier for the same task, without reusing a session ID across tasks.

### CLI session resume

The example uses `native.transport="cli"`. First verify the user-selected configuration directory and existing session. If login is required, use the official user login flow. The Client only resumes existing IDs; it neither logs in nor creates a session on the user's behalf. Required fields:

| Field | Requirement |
| --- | --- |
| `command` | Absolute path to the actual executable; one string, not a full command or argv array |
| `cwd` | Absolute path to an existing local working directory |
| `config_dir` | Absolute path to an existing local CLI configuration directory, assigned to `CODEBUDDY_CONFIG_DIR` by the Client |
| `session_id` | An existing session ID in that CLI environment, exactly matching the Hub binding |

Optional `timeout_seconds` defaults to `120`, with a range of `1`–`600` seconds. Optional `entry_script` is the absolute path to an existing regular local JavaScript file with extension `.js`, `.mjs`, `.cjs`, or filename `codebuddy`. Windows requires an actual `.exe` and rejects `.cmd` / `.bat`. Configure an actual `node.exe` with an official `entry_script`, for example `command="C:/Program Files/nodejs/node.exe"` and `entry_script="C:/absolute/path/to/official/codebuddy.js"`. This requires installed Node.js and the official CLI.

Optional `environment` accepts only string values: `CODEBUDDY_FORCE_LITE_WB_BUNDLE` and `DISABLE_AUTOUPDATER` are each `"0"` or `"1"`; `CODEBUDDY_INTERNET_ENVIRONMENT` is `"internal"`, `"external"`, `"ioa"`, `"cloudhosted"` or `"selfhosted"`. The example's `internal` must match the user's actual login site, and the bundle flag must match the installed entry. These do not add desktop tools. Credentials, IPC arguments and arbitrary environment variables are forbidden. Separate configuration directories do not imply isolated credential storage; the official CLI may use the product's shared authentication store.

The invocation arguments are fixed:

```text
<command> [<entry_script>] --resume <session_id> --print --output-format json --input-format text <fixed-current-handoff-instruction>
```

The final positional argument is the Client's built-in short instruction to process the current handoff within existing authorization. It contains no task body and expands no permission. The full Envelope and reporting instructions enter stdin through a temporary regular file; stdout is also read completely through a temporary regular file. Task content is not concatenated into the command line or a shell. No tool, permission, MCP, model or profile override arguments are passed. Session persistence is not disabled: the target CLI session's default working environment is used, and continuations persist by default. See the official [headless mode documentation](https://www.workbuddy.cn/docs/cli/headless).

A failed attempt with default persistence may still append history. On 2026-10-14, a default resume of the original desktop ID requested login yet appended to the original history; its existing prefix was verified intact. On October 15, default authenticated continuation on the same original path passed. Do not assume failure means no session changes or ignore earlier uncertainty because a later run succeeded. Preserve uncertain state, reconcile native records and avoid blind re-execution.

After checking the complete final JSON for `type=result`, a matching `session_id` and a non-error result, the Client records only `delivered`. Login errors, timeout, truncated or mismatched output remain `uncertain`, without retries. Exit code `0`, result `success`, model refusal or `UNKNOWN` cannot replace native acceptance and completion Reports; artifacts still require verification. Temporary output may contain old history, and the Client does not expose history bodies as diagnostics.

Installing a skill does not automatically grant noninteractive execution permissions. Verify skill metadata reads, execution of the specified reporting program and file permissions for the current artifact workspace in the official CLI's own configuration. The Client neither appends permission overrides nor approves for the user. The standalone CLI passed narrow configuration and actual file-generation acceptance; that configuration does not authorize other workspaces or commands. After renewed authorization, entry reconciliation and one explicit continuation, the original WorkBuddy session also completed a single-contact query and report. Temporary query permissions were removed. Earlier local denial and remote authorization expiry remain in history, not current blockers. When reporting is available, describe native permission problems with `needs_user`; when it is blocked too, retain native prompts and existing delivery evidence without fabricating receipts.

After verifying a terminal native Report, the Hub allocates a separate result `message_id`, points `reply_to` to the original handoff message, and returns to the saved original entry while retaining `task_id` and `correlation_id`. `kind=result` displays results without executing the task again. Optional `result_queue_while_busy_verified` describes only a verified native capability to receive results while busy, and defaults off. It does not mean WorkBuddy CLI can guide or interrupt an active run; it is not currently marked verified.

### Official local-assistant OpenAPI

Explicitly set `native.transport="openapi"`. Legacy configurations without `transport` still mean OpenAPI and never automatically try CLI. Replace the entire `native` with:

```json
{
  "transport": "openapi",
  "access_token": "<short-lived access_token from the user's official OAuth grant>",
  "oauth_scopes": ["user.localassistant.invokable"],
  "timeout_seconds": 30
}
```

This transport uses official `/openapi/v2/localassistant/message` and targets the persistent local assistant, not an ordinary chat ID. The API has no ordinary desktop-session targeting parameter. The user must grant a short-lived access token through the formal third-party OAuth flow; CLI login grants no such permission. Missing token or invokable scope stops the Client before it claims a message.

Delivery is recorded only for HTTP 2xx, integer business `code` equal to `0`, and a valid nonempty string `data.message_id`. Other responses remain uncertain without retries. The native assistant must still actively report acceptance and artifacts. No desktop Cookie, cached token or IPC ticket is read, and automatic token renewal is unsupported. Official authorization and a real round trip remain pending acceptance. [Official OpenAPI](https://open.workbuddy.cn/docs/openapi)

## Maintenance and tests

WorkBuddy implementation, configuration, manifest and dedicated tests live in this directory. Test the adapter separately:

```sh
python3 clients/workbuddy/test_adapter.py -v
```

Common protocol and cross-Client compatibility tests live in `clients/tests`, with common fixtures in `clients/tests/support.py`. At the repository root, `python3 -m unittest discover -s clients/tests -v` includes all assistant-specific tests. They use local simulations only, without actual official API calls. See the [Client documentation](../../docs/CLIENTS.md) for protocol and recovery.
