# Grok Routine Client

[简体中文](README.md) | [English](README.en.md)

Version `0.2.1`, Hub protocol `1`, implementing addressed mailbox protocol `1` with adjacent `shared` runtime `0.2.1` and Python 3.11+. It uses the Python standard library. When copying this directory alone, also copy matching shared files and preserve the adjacent directory layout. The Client can be updated independently, but old program files must actually be replaced; changing a manifest or Hub protocol field does not upgrade the consumer. This release's mailbox queue registration also requires the matching Hub's `native-queue` command; older Hubs are not bypassed.

This Client sends the complete Hub message to the user's configured official Routine webhook. It supports only `queue`; neither `steer` nor `interrupt` is verified. A webhook 2xx response, including 202, confirms HTTP transport only. It does not create a business reply or prove native acceptance or successful execution. Grok must explicitly send mailbox replies through its authorized native tools. The software has offline protocol tests and local Hub-process registration/restart regressions. Each actual Routine's fields, destination and native round trip still require user validation. Another computer's or legacy Task/Report validation does not establish mailbox readiness here.

## Configure and run

Register a `grok` endpoint and set its binding to `"driver": {"type": "external"}`. Export private configuration:

```sh
agi-hub client-config --data /absolute/path/to/private/hub --endpoint grok --file /absolute/path/to/private/grok/client.json
```

The command exports only endpoint credentials and empty `native`, without overwriting an existing file. Use [client.example.json](client.example.json) to set the actual Routine's `native.url`, `native.headers`, `native.body` and the user-defined `native.routine_id` label. That label alone is not a verified native session ID. Confirm that the webhook reaches the user's selected Routine.

The Webhook section of the [official Grok Bot Routines documentation](https://cursor.com/help/grok-bot/routines) allows a JSON body, which the Bot receives together with the Routine instruction. It specifies no fixed JSON field such as `notification`. Template fields are an agreement between the user and the selected Routine's instruction; there is no need to wait for an undocumented "official notification field." For example:

```json
{"body": {"agi_hub_notification": "{{notification}}"}}
```

`agi_hub_notification` is this example's convention, not a fixed platform schema. Make the existing Routine instruction explicitly read its complete handoff, check this endpoint and Routine binding, then follow the collaboration skill. First use a safe native probe to check actual reception, context reading and a Hub reply through its own fixed helper. Neither the example nor HTTP 2xx proves those checks passed, creates a new webhook or adds native permissions.

A Routine that accepts only `{id}` and reads legacy Task files for an old endpoint from a fixed directory uses a different custom contract. Sharing its webhook or passing an old workflow does not establish mailbox V1 or new-endpoint compatibility. Back up the original instruction and configuration, preserve the old branch and business behavior, then explicitly add an inline JSON branch for the new endpoint and validate it separately. Do not feed mailbox messages into the legacy Task-file branch or change Hub queue eligibility to bypass validation.

To send the full handoff, the request template must contain `{{notification}}`, `{{envelope_json}}`, or `{{envelope}}` occupying an entire JSON string value. `{{content}}` and `{{message_id}}` are also available. `{{task_id}}` belongs to legacy Task/Report only; mailbox templates containing it fail before any request. Substitution happens once, leaving placeholders in actual message content unchanged. Messages cannot alter the fixed URL, headers or Routine binding. The default local Hub is `http://127.0.0.1:7331`; use the exported configuration.

First complete the [Grok workspace setup in the installation guide](../../INSTALL.en.md#grok-workspace-installation) and read the [collaboration skill guide](../../docs/SKILLS.en.md). The target Routine must be able to read its installed skill, save message files inside the user's authorized local workspace and invoke the fixed-identity producer helper. Skill installation does not provide those native capabilities. A webhook or cloud Routine alone does not prove access to local helpers, files or the loopback Hub. Leave missing capabilities unconfigured; do not put credentials in prompts or expose an administrator interface to compensate.

The Grok workspace installation entry is `agi-hub-setup --host grok`, with explicit `--client-config` for this endpoint, a user-selected `--workspace`, a `--skills-dir` inside that workspace and the existing Hub's `--data`. See the installation guide for the complete command. It provides the fixed scoped helper and workspace `agi-hub-client/SKILL.md`, without guessing a native Grok skill directory or enabling a session. Give the actual workspace skill path to the selected Routine to read; installation output alone does not establish cloud loading or native-tool availability.

Save and enable the binding before starting the Client: `endpoint.driver` must be `grok-routine`, binding `driver.type` must be `external`, `endpoint.session_id` must exactly match `native.routine_id`, and `modes` must be `["queue"]`. Each connection and actual Hub change event verifies these fields and the enabled state. Explicitly select `mailbox_protocol_version:1` and configure each sender's authorized `allowed_peers`; `visible_peers` only controls directory visibility.

## Register the independent native queue

Each Routine webhook triggers an independent native cloud run. A fixed `routine_id` label is not a long-lived chat whose idleness can be inferred. Mailbox V1 requires explicit local endpoint registration of `mailbox_native_queue_verified`, which defaults to disabled. The Client never grants it from process startup, subscription or HTTP 2xx, and never writes idle, `idle_wake_verified` or busy-session verification flags.

Before first registration, retain two real, checkable native proofs: the selected Routine receives the probe's complete context and reads the collaboration skill; it explicitly returns the probe nonce or checked result through Hub using its own fixed-identity helper. Use a user-authorized safe installation probe. Manually forwarding unconsumed business mail does not prove normal mailbox delivery. Template tests or 2xx alone do not meet these requirements.

After checking the evidence, a local Hub administrator registers it through the matching Hub's formal entry. Replace the placeholders, quote paths separately and use the appropriate `.exe` on Windows:

```text
agi-hub native-queue --data <private-hub-directory> --endpoint <chosen-endpoint> --native-session <same-native.routine_id> --verified=true --evidence-ref <saved-native-probe-reference>
```

Registration accepts only local Hub administrator authority and requires an exact match to the current native session; endpoint credentials cannot self-register. Private configuration persists the current device, driver, native session and short `evidence_ref`. The latter is only an evidence reference, not a path to read or copied chat. Do not just edit a boolean. Rebinding invalidates verification; toggling the same binding or restarting Hub preserves registration. Changing the actual Routine or request contract also requires new validation.

Until registered, the Client reports `needs_native_queue_verification` and registration steps, keeps a read-only subscription, and neither advertises ready nor claims mail. The actual Hub registration event triggers revalidation and automatic consumption of the original still-valid, unclaimed pending message. Resending it or inventing idle is unnecessary. A registered independent native queue can claim under local MQ rules while `session=unknown` or idle; unknown is not changed to idle. A known busy state still requires separate existing busy-delivery verification, and needs_user still blocks ordinary messages. This flag proves neither native serialization, concurrency control nor approval authority. Expired or `dispatching/uncertain` records are never reset, assigned replacement IDs or replayed.

To revoke registration, use the same command and current binding with explicit `--verified=false` and a revocation record's `--evidence-ref`. Its event returns the Client to awaiting verification. Disablement, ACLs, leases, binding checks and uncertain-delivery protections continue to apply. The legacy Task/Report path gains no new queue eligibility.

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

The separate maintenance entry is disabled by default. It requires explicit user configuration and an independently validated maintenance branch in the selected Routine. A normal mailbox branch is not a maintenance entry; this guide establishes no particular computer's support for that channel or complete unattended recovery after an OS restart.

## Maintenance and tests

Grok Routine implementation, configuration, manifest and dedicated tests live in this directory. Test the adapter separately:

```sh
python3 clients/grok-routine/test_adapter.py -v
```

Common protocol and cross-Client compatibility tests live in `clients/tests`, with common fixtures in `clients/tests/support.py`. At the repository root, `python3 -m unittest discover -s clients/tests -v` includes all assistant-specific tests. They use local simulations only, without triggering real webhooks.

For independent offline Hub maintenance, see the [bounded recovery entry](RECOVERY.en.md). It is disabled by default and requires a verified dedicated branch in the existing Routine.
