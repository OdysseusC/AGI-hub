# Client protocol and compatibility tests

[简体中文](README.md) | [English](README.en.md)

`test_clients.py` retains common transport, event-consumption and cross-assistant compatibility checks. Shared local fake HTTP, message and binding fixtures live in `support.py`, independently of any assistant implementation.

Assistant-specific tests are maintained alongside their implementations: `clients/codex/test_adapter.py`, `clients/workbuddy/test_adapter.py` and `clients/grok-routine/test_adapter.py`. Each can also run directly; see its assistant README for commands.

Run the combined suite from the repository root:

```sh
python3 -m unittest discover -s clients/tests -v
```

The common tests' `load_tests` loads each assistant's tests under a unique module name, avoiding collisions between `test_adapter.py` files in different directories. This command combines shared and assistant-specific checks without duplicating tests. All transports and subprocesses are local simulations; no real assistant is called.

## Legacy file-mode smoke check

After building the local Hub, run `python3 tools/smoke_grok_client.py --hub /absolute/path/to/agi-hub` (actual `.exe` on Windows). The test uses a real Hub and file Client with a local simulated Routine and endpoint-scoped reporter. It verifies publication before ID-only webhook, file consumption, actual Reports, artifact SHA-256, one associated result and no resend after adapter reconstruction. It does not call a real assistant/webhook or alter native capability flags. `--temporary-parent <existing-directory-outside-repository>` selects a filesystem such as exFAT. This tests legacy Task/Report, separately from the mailbox suite.
