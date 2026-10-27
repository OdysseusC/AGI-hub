# Client protocol and compatibility tests

[简体中文](README.md) | [English](README.en.md)

`test_clients.py` retains common transport, event-consumption and cross-assistant compatibility checks. Shared local fake HTTP, message and binding fixtures live in `support.py`, independently of any assistant implementation.

Assistant-specific tests are maintained alongside their implementations: `clients/codex/test_adapter.py`, `clients/workbuddy/test_adapter.py` and `clients/grok-routine/test_adapter.py`. Independent offline maintenance tests are in `clients/grok-routine/test_recovery.py`; restart regressions with a real Hub and local native substitutes are in `test_restart_recovery.py`. Both are included by the command below. Each can also run directly; see its assistant README for commands.

Run the combined suite from the repository root:

```sh
python3 -m unittest discover -s clients/tests -v
```

The common tests' `load_tests` loads each assistant's tests under a unique module name, avoiding collisions between `test_adapter.py` files in different directories. This command combines shared and assistant-specific checks without duplicating tests. All transports and subprocesses are local simulations; no real assistant is called.
