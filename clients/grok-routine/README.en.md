# Grok Routine Client

[简体中文](README.md) | [English](README.en.md)

Version `0.2.0`, Hub protocol `1`, requiring adjacent `shared` runtime `0.2.0` and Python 3.11+. It uses the Python standard library. When copying this directory alone, also copy matching shared files and preserve the adjacent directory layout. It does not currently support addressed mailbox messages; do not configure `mailbox_protocol_version=1`.

This Client supports legacy queue tasks through either the full-body official Routine template or explicit local `file-webhook`. Neither `steer` nor `interrupt` is verified. Webhook acceptance confirms transport only; Grok must actively report execution and artifacts. The fixed Windows Codex/Grok installation passed separate legacy acceptance on 2026-10-16, as recorded below. Other installations and the new mailbox path are not covered.

## Configure and run

Register a `grok` endpoint and set its binding to `"driver": {"type": "external"}`. Export private configuration:

```sh
agi-hub client-config --endpoint grok --file /absolute/path/to/private/grok/client.json
```

The command exports only endpoint credentials and empty `native`, without overwriting an existing file. Use [client.example.json](client.example.json) to set the actual Routine's `native.url`, `native.headers`, `native.body` and the user-defined `native.routine_id` label. The Hub's `endpoint.driver` must be `grok-routine`, and `endpoint.session_id` must match that label. Each connection checks this binding first. The example request fields are placeholders, not a verified format; fill them in using the actual official structure of that Routine.

To send the full handoff, the request template must contain `{{notification}}`, `{{envelope_json}}`, or `{{envelope}}` occupying an entire JSON string value. Substitution happens once; placeholders inside message content stay unchanged. The default local Hub is `http://127.0.0.1:7331`; use the exported configuration.

Run from the repository root:

```sh
python3 clients/grok-routine/main.py --config /absolute/path/to/private/grok/client.json
```

On Windows, use `py -3` and local absolute paths. Keep personal webhook and authentication configuration outside the repository. Run only one consumer per endpoint. See the [Client documentation](../../docs/CLIENTS.en.md) for full details.

## Maintenance and tests

Grok Routine implementation, configuration, manifest and dedicated tests live in this directory. Test the adapter separately:

```sh
python3 clients/grok-routine/test_adapter.py -v
```

Common protocol and cross-Client compatibility tests live in `clients/tests`, with common fixtures in `clients/tests/support.py`. At the repository root, `python3 -m unittest discover -s clients/tests -v` includes all assistant-specific tests. They use local simulations only, without triggering real webhooks.

## Local file-webhook transport for legacy tasks

Explicit `native.transport="file-webhook"` retains the Windows integration. Omit `transport` to keep the existing full-body template mode. Neither mode supports addressed mailbox messages: keep `mailbox_protocol_version=0`; do not translate old bridge messages or receipts into new Hub tasks.

Create a dedicated, existing local absolute `inbox_dir`, receiver-state directory and artifact workspace outside the repository, separately for this Hub, endpoint and Routine. The inbox must be a real directory without symlink/reparse components. Fix the original Bot and an enabled webhook-only Routine; verify actual local file access, existing native tools and report execution. A cloud webhook does not itself provide local access. Bind driver `grok-routine`, external driver type and queue mode; `endpoint.session_id` must equal the stable `native.routine_id` label, which alone does not locate a platform session.

Export the endpoint-scoped configuration as above, then use this `native` shape without `body`:

```json
{
  "transport": "file-webhook",
  "routine_id": "hub-grok-routine",
  "inbox_dir": "/absolute/private/grok/inbox",
  "url": "<user-verified official Routine webhook URL>",
  "headers": {},
  "timeout_seconds": 30
}
```

Keep the real URL, headers and token only in private configuration. On Windows use actual local absolute paths and the verified Python executable. Install a matching adjacent shared runtime `0.2.0`, an endpoint-fixed report launcher and the filled [Routine instructions template](GROK_INSTRUCTIONS.en.md). The template is not an automatically discovered host skill. The receiver fixes its inbox, device/workspace mappings, report program and config; incoming text cannot change them.

The Client publishes a complete `agi-hub-grok-file-v1` JSON package before sending exactly `{"id":"hub-…"}` to the webhook. The package contains `id`, `endpoint`, `routine_id`, the unchanged full `envelope` and `notification` reporting instructions. Its filename is `<id>.json`; `id` is `hub-` plus the complete lowercase UTF-8 SHA-256 of `envelope.message_id`. Original task/result IDs and correlation remain inside the Envelope.

A persistent directory claim, same-directory temporary file and rename prevent duplicate publication/notification, including on exFAT. The receiver ignores temporary files and separately saves its processing state and stable Reports to prevent repeated business execution. Existing claims/packages or conflicts require reconciliation, not deletion and resend. Preserve packages, receipts and Reports during upgrades. Hub restart does not clear unresolved deliveries or prove a native session is idle.

| File-mode result | Delivery | Meaning |
| --- | --- | --- |
| HTTP 200 | `delivered` | Notification accepted; not business completion |
| HTTP 401/403 | `failed` | Explicit rejection; preserve evidence |
| Other responses, redirects, timeouts or ambiguous transport | `uncertain` | No automatic retry |
| Invalid Envelope/encoding/mode before notification | `failed` | No notification attempt |
| Missing/conflicting claim or package, publication/receipt-write failure | `uncertain` | Preserve all evidence and reconcile |

Redirects are never followed and the response body is not read. `timeout_seconds` covers sending/response-header waiting after connection; standard-library DNS cannot be portably interrupted, but no POST is sent after the deadline. Template mode retains its separate original 2xx-delivered semantics. Do not silently switch between modes.

For a new installation, retain false wake flags until one authorized independent probe proves the fixed session was idle, woke and actually read the new file with its tools. Only then update the matching wake capability and, after restart and a fresh actual state check, record native idle state. A separate real Hub task must verify actual accepted/running/terminal Reports, artifact SHA-256, associated result and original-entry consumption. HTTP 200, old-bridge success or a local simulation cannot replace that evidence. The fixed Windows installation passed separate bidirectional legacy acceptance on 2026-10-16; other installations and busy capabilities need their own checks. [Dated record](../../docs/NATIVE-VALIDATION.en.md#windows-grok-roundtrip-20261016)

A model-free protocol smoke test is available after building the Hub:

```sh
python3 tools/smoke_grok_client.py --hub /absolute/path/to/agi-hub
```

It uses a real local Hub/file Client and scoped report launcher with a simulated Routine, checks ID-only notification after complete publication, actual file consumption, Reports, artifact SHA-256, one associated result and no notification after adapter reconstruction. It never calls a real Grok webhook. Use the actual `.exe` on Windows; `--temporary-parent <existing-directory-outside-repository>` can test exFAT or another chosen filesystem. Native acceptance flags are unchanged.
