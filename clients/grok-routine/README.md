# Grok Routine Client

[English](README.md) | [简体中文](README.zh-CN.md)

Version `0.1.0`, Hub protocol `1`, requiring adjacent `shared` runtime `0.2.0` and Python 3.11+. It uses the Python standard library. When copying this directory alone, also copy matching shared files and preserve the adjacent directory layout. It does not currently support addressed mailbox messages; do not configure `mailbox_protocol_version=1`.

This Client sends the complete Hub message to the user's configured official Routine webhook. It supports only `queue`; neither `steer` nor `interrupt` is verified. A webhook 2xx response confirms transport only. Grok Bot must actively report acceptance and artifacts. The user's existing verification on another Windows computer does not validate this Client in the current environment. Its native round trip still needs actual testing.

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

On Windows, use `py -3` and local absolute paths. Keep personal webhook and authentication configuration outside the repository. Run only one consumer per endpoint. See the [Client documentation](../../docs/CLIENTS.md) for full details.

## Maintenance and tests

Grok Routine implementation, configuration, manifest and dedicated tests live in this directory. Test the adapter separately:

```sh
python3 clients/grok-routine/test_adapter.py -v
```

Common protocol and cross-Client compatibility tests live in `clients/tests`, with common fixtures in `clients/tests/support.py`. At the repository root, `python3 -m unittest discover -s clients/tests -v` includes all assistant-specific tests. They use local simulations only, without triggering real webhooks.
