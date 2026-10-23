# Codex Client

[简体中文](README.md) | [English](README.en.md)

Version `0.3.0`, Hub protocol `1`, optional addressed mailbox protocol `1`. The default entry is `desktop_mcp_client.mjs`, implemented with the Node.js 22+ standard library, without npm installation or third-party libraries. It uses the official `codex-app-tools` MCP server to deliver messages to the bound original desktop chat, preserving its tools, model and permissions.

Run only one consumer per Hub endpoint. When `mailbox_protocol_version=1` is explicitly configured, it consumes only `/v1/mail/inbox`: `request/reply/result` go to the original session, while `status` stays in the message record with a delivery receipt and does not wake a model. The session's skill chooses actions, collaborators and reply recipients from context. Every reply is sent explicitly; the Hub neither generates replies from business state nor forces a parent/child task chain. `reply_to` is only a reference. `root_origin` retains the native session where the user first asked, and final results should return there.

Native sends always use immediate input through the official `send_message_to_thread`, without a separate native queue. Set `mailbox_while_busy_verified=true` only after verifying the host's ability to guide an active original session. An MCP ACK proves receipt only: the Client does not claim business completion, call Report or declare the native turn finished. Existing bindings without mailbox enabled continue consuming only the legacy task queue; Desktop MCP returns only `kind=result`. Ordinary-task `steer / interrupt` is not implemented. Uncertain transport is neither retried nor downgraded to `codex queue`.

## Official Desktop MCP

`native.transport="desktop-mcp"` calls the fixed `send_message_to_thread` tool exposed by the official server. The Client initializes MCP and checks tool arguments, then subscribes to Hub SSE before claiming results. `threadId` is fixed in private configuration. Caller identity comes only from the host-provided `CODEX_THREAD_ID`. An Envelope cannot replace the target, caller, tool, launch arguments or permissions.

The ordinary desktop chat that starts the Client may differ from the destination chat: `CODEX_THREAD_ID` is the real caller, while `native.thread_id` is the fixed target and must match the Hub binding. Inherit the same Desktop host environment naturally; do not copy a pipe or forge the caller. The host restores the target chat's context and tools. Access, sub-session restrictions and actual wakeup still require validation on that host, with a strict target ACK check.

The official tool has no `mode` argument. The public implementation on this machine uses a `send-now` follow-up: it guides an active turn or starts a new one when idle. On the development Mac, an independent official Node process started the server, listed actual tools and made one call, returning `isError=false` and the matching original chat ID. The result entered the original chat immediately without the user clicking a queued item.

A full busy-session round trip also passed: start the Node consumer and confirm its SSE subscription, then submit a new task. WorkBuddy resumes the original CLI session by default and actively reports acceptance, execution and success. The Hub verifies the artifact and SHA-256 before generating an independent result. Node automatically claims it and uses official Desktop MCP to guide the running original Codex chat immediately. The task and both deliveries reached successful terminal states, with the correct result correlation and released resources. The return path did not rely on the model in the original chat calling a tool on its behalf. Both complete MQ round trips verified only busy-session result return on this machine. A separate idle-start diagnostic later passed on the same official Desktop MCP channel, as described below. A complete idle MQ round trip, ordinary task controls and other platforms remain unverified.

### Single idle-start diagnostic in the original chat

On 2026-10-15, after the preceding turn ended, an independent official Node program delayed once and made only one `read_thread` call before wakeup. It confirmed `thread.status.type=idle` and that the previous turn was `completed`. It then called official Desktop MCP `send_message_to_thread` once, receiving `isError=false` and an ACK matching the target chat.

The original chat actually received the message and started a new turn with a different turn ID from the previous completed turn. The official desktop `read_thread` tool remained available; execution did not move to a standalone CLI/SDK session. There was no repeated wakeup, manual forwarding or new MQ business task.

This diagnostic proves that the same official MCP transport can start the idle original chat on this machine. It is not a third MQ business round trip or proof of a complete idle MQ round trip. The two complete MQ acceptance runs still cover busy-session returns. [Separate evidence](../../docs/NATIVE-VALIDATION.en.md)

Desktop MCP and App Server proxy are separate entry points. The former uses the host-provided bridge environment through the official server; the latter connects to `app-server-control.sock`. The absence of a proxy control socket in this host does not make Desktop MCP unavailable.

The current production macOS host checks the signatures of the connecting process and its parent chain. The verified launch chain is an official Node Client directly starting the official Node server, executed normally from a Codex host that already provides the official environment. A Python parent chain caused the tool-discovery connection to close on this machine. Installing a system Node separately or launching outside the host in the background does not guarantee availability. Do not copy bridge tickets, persist pipe values, guess private sockets or change identities to connect.

## Configure and start

Register a `codex` endpoint in the Hub with `endpoint.driver="codex"` and `endpoint.session_id` equal to the verified native thread ID. Use `"driver":{"type":"external"}` for its binding. Export private configuration scoped to this endpoint only:

```sh
agi-hub client-config --endpoint codex --file /absolute/private/codex/client.json
```

The export creates a file with private permissions without printing credentials or overwriting existing configuration. Fill in `native` using [client.example.json](client.example.json):

| Field | Requirement |
| --- | --- |
| `transport` | `desktop-mcp` |
| `command` | Absolute executable path to the user-verified official Node or official `launch_codex_app_tools_mcp`; an actual `.exe` is required on Windows |
| `args` | Only one absolute path to the official `server.mjs`; no shell, identity arguments or message text |
| `cwd` | Absolute official plugin directory containing `server.mjs` |
| `thread_id` | Existing native desktop chat ID matching the Hub binding |
| `timeout_seconds` | Per-request MCP timeout, default `30`, range `1`–`300` seconds |

When migrating a customized older Client, export a fresh endpoint-scoped configuration and copy only the supported `native` fields listed above. The current Node Client strictly rejects the old custom `receive_tasks` field, even when `false`, with `invalid_codex_desktop_mcp_fields`. Review and back up the private configuration before removing that legacy field; do not relax unknown-field validation.

Obtain `command`, `args` and `cwd` from the actual installed official plugin manifest and runtime environment; do not guess directories. The Client inherits only normal operating-system variables and an allowlist of official Node/bridge variables. It does not read desktop credentials from configuration. If the host lacks `CODEX_THREAD_ID` or `CODEX_APP_TOOLS_PIPE_PATH`, it stops before claiming messages. Do not copy those values into `client.json`, documentation or logs.

Start the verified official Node directly from an entry with the official host environment. In this POSIX example, `exec` replaces the launch shell with Node, preserving the verified parent chain:

```sh
exec /absolute/path/to/official/node /absolute/path/to/agi-hub/clients/codex/desktop_mcp_client.mjs --config /absolute/private/codex/client.json
```

On Windows, use an actual `node.exe` and local absolute paths, without `.cmd` or `.bat` wrappers as the process entry. Bridge and wakeup capabilities on Windows, Linux and other macOS hosts require separate acceptance tests. The default Hub address is `http://127.0.0.1:7331`; only numeric loopback addresses are accepted. Use the actual exported configuration.

The Client rechecks the endpoint binding and MCP tools on initial startup and every Hub reconnection. It subscribes to SSE before claiming; keepalives do not trigger polling. On disconnection, it only reconnects and scans pending messages that the Hub can still deliver. Messages already `dispatching / uncertain` are not resent. The Client does not declare the session idle or handle permission prompts automatically.

It records `delivered` only when the tool returns `isError=false` and its single text block parses as a JSON ACK matching the original thread ID. Initialization or discovery failure before the tool call claims no message. Timeout, disconnection, tool errors or mismatched ACKs after a call begins are `uncertain`. A failure to persist the receipt stops the Client, retaining the Hub claim for reconciliation. An ACK proves native receipt, not business success or a verified idle wakeup.

## Retained Python entry

`main.py` still uses the Python 3.11+ standard library and depends on adjacent `shared` runtime `0.2.0`. Copy or pin the matching `clients/shared` alongside the Python Client. The Node Desktop MCP entry does not import shared; both use the same Hub protocol.

The Python entry depends on adjacent `shared` runtime `0.2.0`. Explicitly select one transport. Do not run Node and Python consumers simultaneously for the same endpoint:

| transport | Scope |
| --- | --- |
| `desktop-mcp` | Same official MCP and configuration; the Python parent chain did not pass host validation on the current Mac, requiring separate acceptance on hosts that actually allow it |
| `app-server` | Official `codex app-server proxy` connecting to a running daemon, for result return only; this host does not expose the default control socket |
| `queue` | Compatibility with `codex queue --thread ID --message TEXT`; proves queue receipt only, and a path requiring manual busy-session guidance is not automatic return |

App Server uses `thread/read` without bodies and `thread/turns/list` for metadata only. While busy, it returns results through `turn/steer` guarded by `expectedTurnId`; it handles neither ordinary tasks nor interrupt. Example `native`:

```json
{"transport":"app-server","thread_id":"<verified native thread ID>","executable":"codex","timeout_seconds":30,"result_mode":"steer","idle_start_verified":false}
```

App Server's own idle `turn/start` remains untested. A single idle Desktop MCP start does not validate it. Enable this App Server branch explicitly only after separately checking original-thread tool retention and idle startup. It does not automatically resume unloaded threads. Legacy queue configuration:

```json
{"transport":"queue","thread_id":"<verified native thread ID>","executable":"codex","timeout_seconds":30}
```

Start Python with:

```sh
python3 clients/codex/main.py --config /absolute/private/codex/client.json
```

## Maintenance and tests

Implementation, configuration, manifest and dedicated tests live in this directory. Node tests use only in-memory stdio and local HTTP, without starting the official server or sending native messages:

```sh
node --test clients/codex/test_desktop_mcp_client.mjs
python3 clients/codex/test_adapter.py -v
```

Common Python protocol tests live in `clients/tests`; run `python3 -m unittest discover -s clients/tests -v` at the repository root. See the [Client documentation](../../docs/CLIENTS.en.md) for updates and uncertain delivery, and the [acceptance record](../../docs/NATIVE-VALIDATION.en.md) for separate native evidence.
