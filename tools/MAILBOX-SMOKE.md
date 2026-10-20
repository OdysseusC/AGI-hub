# Zero-model mailbox integration acceptance

[English](MAILBOX-SMOKE.md) | [简体中文](MAILBOX-SMOKE.zh-CN.md)

`smoke_mailbox.py` starts an actual Hub process and verifies addressed messages through real local HTTP, endpoint tokens, SSE subscriptions and the common Python Client runtime. An in-memory adapter simulates native delivery. It starts no Codex, WorkBuddy, Claude or other model and reads no personal Hub configuration.

Build the Hub for the current system at the repository root, then run:

```sh
go build -o /absolute/temporary/agi-hub ./cmd/agi-hub
python3 tools/smoke_mailbox.py --hub /absolute/temporary/agi-hub
```

On Windows, use the actual `.exe` path and optionally `py -3`. Python 3.11+ is required, with no third-party dependencies.

The script creates five test members in a temporary directory and cleans up the data and Hub process afterward. Every “wakeup verified” configuration belongs only to the in-memory adapter and is never written to a user's installation.

The message path covers A requesting help from B, C and D concurrently; B messaging D; D choosing to reply directly to A; and C and B explicitly sending results to A. `reply_to` is only a reference, with no parent/child chain forcing a return through B. The final root origin and human request reference remain consistent within the correlation.

It also checks:

- The common Client claims only messages from its own mailbox; delivery receipts cannot cross endpoint boundaries.
- Duplicate message IDs neither call the adapter again nor increase the shared message count.
- `status` is stored and acknowledged without increasing adapter calls.
- Uncertain delivery retains its record, later messages stay pending, and another consumer cannot claim concurrently.
- The default correlation limit is 16 messages, shared by all senders without increasing it.
- State queries show only messages sent or received by the caller. Its own `status` content is available on demand; ordinary work content and messages between other members are not copied. Unrelated members cannot read the correlation.
- No Task, Report or automatic business reply is created, and mailbox messages do not imply session business state.

On success, stdout contains one JSON record with `native_model_calls: 0`. This validates compatibility between the actual Hub and Client protocol. Native assistant login, skill loading, idle wakeup and busy guidance still require separate acceptance in each host.
