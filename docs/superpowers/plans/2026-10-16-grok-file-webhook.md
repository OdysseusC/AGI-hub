# Grok file webhook Client implementation plan

> **For agentic workers:** Use superpowers:subagent-driven-development to implement and review the isolated Client change. This plan implements the user's requested integration into the existing Hub architecture.

**Goal:** Add the proven file-first, ID-only Grok Routine notification pattern to AGI Hub, including usable configuration and receiver instructions.

**Architecture:** Keep the Go broker's routing, protocol 1, scoped reporting and Codex Desktop Client unchanged. Extend `clients/grok-routine` with an explicit `file-webhook` transport. Publish immutable Hub packets into a separate local inbox before calling a user-configured Routine webhook with only `{id}`. Native Grok reports acceptance and completion through the existing scoped report helper; the broker validates artifacts and returns one result through the origin Client.

**Tech Stack:** Python 3.11+ standard library, existing Go Hub, local HTTP test servers; Windows/exFAT compatible file publication.

## Scope and decisions

- The existing template webhook transport remains compatible. `file-webhook` is opt-in and requires an existing absolute, real inbox directory.
- Hub message IDs contain `:` and cannot become Windows filenames. File IDs are `hub-` plus the full SHA-256 of the UTF-8 message ID. The original IDs remain unchanged inside the packet.
- A packet contains protocol `agi-hub-grok-file-v1`, file ID, endpoint, routine ID, complete envelope and the existing shared notification text. No credentials, callback URLs or executable commands are derived from message content.
- Exclusive persistent per-ID claim directories serialize publication and prevent notification replay across restarts. Write/flush/close a same-directory `.tmp`, then rename to `.json`. Preserve claims, messages and uncertain outcomes for reconciliation. Do not depend on hard links (the existing G: workspace uses exFAT).
- Only HTTP 200 confirms Grok webhook delivery. HTTP 401/403 means explicit rejection; every other status or uncertain transport remains uncertain. Never follow redirects or retry native dispatch automatically.
- Use a separately configured webhook-only Routine and a separate inbox. Do not modify the running legacy bridge, its protocol, messages, credentials, scheduled settings or existing Bot routing.

## Task 1: Client and behavior tests

Files: `clients/grok-routine/adapter.py`, `main.py`, `test_adapter.py`. Both transports stay in the adapter module so standalone loading remains compatible.

- [x] Add failing tests for complete file publication before the webhook, ID-only payload, task/result identifiers, unchanged literal task content and credential-free packets.
- [x] Add failing tests for duplicate/restarted/concurrent delivery, publication failure, invalid directories/redirects, HTTP 200 vs 202/204/401/403/429/500 and lost responses.
- [x] Implement the opt-in transport using the existing runtime contract (`validate_binding`, `deliver`) and retain legacy template behavior.
- [x] Run `python clients/grok-routine/test_adapter.py -v`, then `python -m unittest discover -s clients/tests -v`.

## Task 2: Configuration and Grok instructions

Files: `clients/grok-routine/client.example.json`, `manifest.json`, `README.md`, new `GROK_INSTRUCTIONS.md`; relevant sections of `README.md`, `docs/CLIENTS.md`, `docs/ADAPTERS.md`, `docs/INSTALL-FOR-ASSISTANT.md`.

- [x] Publish a private configuration example with no real URL/token/thread ID and a versioned manifest.
- [x] Document a dedicated Routine, its ID-only trigger, actual local-computer access requirement and stable inbox mapping.
- [x] Provide copyable receiver instructions: validate packet/recipient/routine/IDs; keep task-local processing state; report accepted/running/final with stable event IDs and task run ID; preserve unconfirmed reports; consume result without executing or replying again.
- [x] Document scoped reporting and artifact paths, Codex Desktop result return, disabled-by-default validation flags and migration boundaries.

## Task 3: Real Hub synthetic loop and review

Files: new `tools/smoke_grok_client.py`; `tools/package/main.go` only if required to include this runnable check in portable packages.

- [x] Exercise a real Hub process and the real Grok file transport against a local synthetic webhook. Its receiver reads the final packet, reports accepted/running/succeeded through a fixed scoped launcher, writes an artifact, and receives exactly one result at a synthetic origin.
- [x] Assert actual broker outcome, SHA-256 validation, released resources and no duplicate webhook on repeated drain or adapter recreation. Label all evidence as synthetic; do not claim a real Grok/Hub integration has been tested.
- [x] Run the Python suite, Node Codex Client regression tests, Go tests/vet and both synthetic smoke checks. Attempt race checks where the Windows toolchain supports them.
- [x] Review the diff for scope, secrets, preservation of current native tools and recovery semantics.

Delivery uses a reviewable branch/PR without merging main.

## Verification notes

Baseline tests exposed two existing Windows failures: append-only journal handles could not repair torn tails, and the Desktop MCP test fixture lacked the required `.exe` suffix. A separate commit uses a write handle for truncation while retaining append behavior, tests repair/append/reopen, and fixes only the platform-specific fixture name.

Independent review caught a deadline race where closing an HTTP connection could allow its implicit reconnection to send a late POST. The regression now passes: the watchdog shuts down the attached socket, automatic reconnection is disabled, and elapsed budgets are checked before sending. System DNS/connection delays can still postpone return; over-budget connections never start a POST.

Windows verification: 68 Python tests, 67 passed and one symlink capability skip; 9 Codex Node tests passed; Go tests and vet passed; both synthetic loops passed, including Grok file publication on exFAT. The Go race detector could not run because this toolchain has CGO disabled. Native Grok/Hub execution remains unverified, and the running legacy bridge was not changed.

All six portable targets built (Windows/macOS/Linux, amd64/arm64). Each archive contains the matching Grok Client, receiver instructions, scoped reporting runtime and synthetic smoke script, with no private `client.json`.
