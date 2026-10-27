# Grok Routine Client

[简体中文](README.md) | [English](README.en.md)

Version `0.2.0`, Hub protocol `1`, implementing addressed mailbox protocol `1` with adjacent `shared` runtime `0.2.0` and Python 3.11+. It uses the Python standard library. When copying this directory alone, also copy matching shared files and preserve the adjacent directory layout. The Client can be updated independently, but the old `0.1.0` program must actually be replaced; changing its manifest or the Hub protocol field does not upgrade an old consumer.

This Client sends the complete Hub message to the user's configured official Routine webhook. It supports only `queue`; neither `steer` nor `interrupt` is verified. A webhook 2xx response, including 202, confirms HTTP transport only. It does not create a business reply or prove native acceptance or successful execution. Grok must explicitly send mailbox replies through its authorized native tools. This version has offline protocol tests only; the real Routine fields, destination and native round trip still require user configuration and validation. Another computer's or legacy Task/Report validation does not establish mailbox readiness here.

## Configure and run

Register a `grok` endpoint and set its binding to `"driver": {"type": "external"}`. Export private configuration:

```sh
agi-hub client-config --data /absolute/path/to/private/hub --endpoint grok --file /absolute/path/to/private/grok/client.json
```

The command exports only endpoint credentials and empty `native`, without overwriting an existing file. Use [client.example.json](client.example.json) to set the actual Routine's `native.url`, `native.headers`, `native.body` and the user-defined `native.routine_id` label. That label alone is not a verified native session ID. Confirm that the webhook reaches the user's selected Routine and use its actual official request structure; the example fields are placeholders, not a validated format.

To send the full handoff, the request template must contain `{{notification}}`, `{{envelope_json}}`, or `{{envelope}}` occupying an entire JSON string value. `{{content}}` and `{{message_id}}` are also available. `{{task_id}}` belongs to legacy Task/Report only; mailbox templates containing it fail before any request. Substitution happens once, leaving placeholders in actual message content unchanged. Messages cannot alter the fixed URL, headers or Routine binding. The default local Hub is `http://127.0.0.1:7331`; use the exported configuration.

First complete the [Grok workspace setup in the installation guide](../../INSTALL.en.md#grok-workspace-installation) and read the [collaboration skill guide](../../docs/SKILLS.en.md). The target Routine must be able to read its installed skill, save message files inside the user's authorized local workspace and invoke the fixed-identity producer helper. Skill installation does not provide those native capabilities. A webhook or cloud Routine alone does not prove access to local helpers, files or the loopback Hub. Leave missing capabilities unconfigured; do not put credentials in prompts or expose an administrator interface to compensate.

The Grok workspace installation entry is `agi-hub-setup --host grok`, with explicit `--client-config` for this endpoint, a user-selected `--workspace`, a `--skills-dir` inside that workspace and the existing Hub's `--data`. See the installation guide for the complete command. It provides the fixed scoped helper and workspace `agi-hub-client/SKILL.md`, without guessing a native Grok skill directory or enabling a session. Give the actual workspace skill path to the selected Routine to read; installation output alone does not establish cloud loading or native-tool availability.

Save and enable the binding before starting the Client: `endpoint.driver` must be `grok-routine`, binding `driver.type` must be `external`, `endpoint.session_id` must exactly match `native.routine_id`, and `modes` must be `["queue"]`. Each connection verifies these fields and the enabled state. Explicitly select `mailbox_protocol_version:1` and configure each sender's authorized `allowed_peers`; `visible_peers` only controls directory visibility. Hub delivery also requires an observed, verified receiving session. Process startup or HTTP 2xx must not automatically mark it idle, set `idle_wake_verified`, or enable busy-session verification.

Run from the repository root:

```sh
python3 clients/grok-routine/main.py --config /absolute/path/to/private/grok/client.json
```

On Windows, use `py -3` and local absolute paths. Keep personal webhook and authentication configuration outside the repository. Run only one consumer per endpoint. See the [Client documentation](../../docs/CLIENTS.en.md) for full details.

## Mailbox delivery and explicit replies

The Hub binding selects the protocol: `mailbox_protocol_version:1` consumes only `/v1/mail/inbox` and records `/v1/mail/delivery`; `0` retains the legacy Task/Report inbox. The Client neither mixes them nor falls back automatically. V1 `request/reply/result` hand off the complete Envelope. Shared runtime records delivery for `status` without a webhook or model wake; the adapter also rejects direct status delivery.

When a reply is needed, Grok chooses an authorized recipient from the current context, saves new message JSON inside the fixed helper's workspace, then invokes that installed helper with `mail-send --file <saved-message-file>`. For example:

```json
{
  "id": "saved-new-reply-id",
  "recipient": "chosen-authorized-endpoint",
  "kind": "reply",
  "content": "brief verified result",
  "correlation_id": "received-correlation-id",
  "reply_to": "received-message-id"
}
```

A new reply has its own saved stable ID and inherits the received correlation. `reply_to` is a reference, not a forced return route. Check `root_origin` and the collaboration skill for final delivery. The fixed helper uses only its endpoint's scoped credential and authorized workspace; it provides no administrator, session-control or native-execution capability. Mailbox messages do not use legacy Task Report, and neither webhook responses nor model-turn completion automatically create a reply.

Each claim starts at most one webhook request. On timeout, non-2xx, or an unconfirmed delivery receipt, preserve the original message and ID and reconcile Hub state with native evidence. Do not change IDs, resend the webhook blindly, reset claims, or generate automatic acceptance/success reports. The Client does not automatically consume existing `dispatching/uncertain` records again.

Normal assistant collaboration uses AGI Hub mailboxes. CUA/GUI is reserved for initial recovery, installation guidance and actual GUI-button verification. Once Hub access is restored, typing into the Grok window must not substitute for ordinary delivery or explicit replies.

## Maintenance and tests

Grok Routine implementation, configuration, manifest and dedicated tests live in this directory. Test the adapter separately:

```sh
python3 clients/grok-routine/test_adapter.py -v
```

Common protocol and cross-Client compatibility tests live in `clients/tests`, with common fixtures in `clients/tests/support.py`. At the repository root, `python3 -m unittest discover -s clients/tests -v` includes all assistant-specific tests. They use local simulations only, without triggering real webhooks.
