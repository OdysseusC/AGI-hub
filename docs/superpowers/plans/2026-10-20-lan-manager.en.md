[简体中文](2026-10-20-lan-manager.md) | [English](2026-10-20-lan-manager.en.md)

# LAN manager implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Deliver offline-verified cross-Hub mail, explicit discovery and a local management page on a separate branch and unmerged PR.

**Architecture:** `internal/mesh` owns identity, pairing, durable transport and bounded relays; `internal/discovery` owns mDNS/DNS-SD; `internal/app` integrates local mail and narrow management APIs. Native clients remain local. The UI uses local APIs and SSE.

**Tech Stack:** Go networking, cryptography and storage; embedded HTML/CSS/JavaScript; Go/Node offline tests and existing Python/package checks.

---

### 1. Baseline and protocol

- [x] Create the `feat/lan-hub-manager` worktree from verified `origin/main`; save the approved design.
- [ ] Run `go test ./...` and record the baseline.
- [ ] Fix the cross-host `hub-id/endpoint-id` format, message origin, correlation, deadline and file ownership while retaining local addresses.

### 2. Identity, pairing, persistent connections and relays

**Files:** New `internal/mesh/*.go` and tests alongside them.

- [ ] Write failing tests first: restart retains Hub ID; unknown public keys cannot deliver; both parties must confirm pairing before connecting.
- [ ] Implement private identity persistence, default-off settings, short-lived invitations, pending pairing and pinned-key verification.
- [ ] Write failing tests first: three temporary Hubs exchange messages between same-named endpoints; removing the direct link permits delivery through the third Hub.
- [ ] Implement reusable TLS connections, directory synchronization, bounded loop-free paths, route expiry, reconnect backoff and explicit disconnect.
- [ ] Write failing tests first: restart preserves pending delivery; identical messages deduplicate, conflicting IDs fail, and a relay cannot modify signed content.
- [ ] Implement the outbox and durable destination deduplication; distinguish destination-Hub ACK from native delivery.
- [ ] Pass `go test ./internal/mesh` and race checks where available.

### 3. Explicit discovery

**Files:** New `internal/discovery/*.go`, parser and lifecycle tests.

- [ ] Write failing tests first: PTR/SRV/TXT/A announcement round trip, compressed-name bounds, malformed-packet rejection and candidate expiry.
- [ ] Implement `_agihub._tcp.local` queries/announcements, selected interface, stopping/goodbye and independently usable manual addresses.
- [ ] Default-off creates no UDP socket; network heartbeats and discovery read no chat history and invoke no models.
- [ ] Pass `go test ./internal/discovery`; provide offline lifecycle checks with injectable sockets and clocks.

### 4. Local mailbox and limited management API

**Files:** `internal/app/network.go`, `internal/app/management.go`, `internal/manager/mailbox.go`, `cmd/agi-hub/main.go` and related tests.

- [ ] Write failing tests first: qualified recipients use mesh, local recipients retain current behavior, and the destination checks exports before durably importing into its mailbox.
- [ ] Preserve correlation and `root_origin`, reject remote impersonation of a local sender, and never validate remote file references as local paths.
- [ ] Accept only enumerated actions, fixed endpoints and strict JSON in management APIs; retain loopback, Host/Origin and administrator authentication.
- [ ] Wrap managed skill installation and configuration guidance with explicit host steps; require real binding before enabling and retain uncertain delivery when disabling.
- [ ] Publish network/management changes through SSE without automatically sending native tests.
- [ ] Complete three-Hub transport and two-Hub simulated-Client end-to-end offline tests.

### 5. Management interface

**Files:** `internal/app/ui.go`, embedded resources and browser-logic tests.

- [ ] Split the page into local/network/message areas with explicit discovery, discovered/paired/online counts and agents beneath each Hub.
- [ ] Implement installation/binding guidance, enable/disable, invitations, pairing confirmation, disconnect, manual addresses, errors and in-progress states.
- [ ] Render actual API state, never hard-coded success; use safe text rendering for content and display names.
- [ ] Verify initial empty state, lists, buttons, errors and narrow screens in a local browser; transport and installation invoke no models.

### 6. Integration, documentation and PR

- [ ] Perform specification and independent code reviews, fix findings and recheck.
- [ ] Synchronize Chinese/English docs, changelogs, configuration examples and upgrade instructions; document real cross-machine validation limits.
- [ ] Run `go test ./...`, `go vet ./...`, existing Node/Python tests, documentation checks, portable packaging and mailbox smokes.
- [ ] Commit/push the feature branch, create and attach a PR, inspect checks, and do not merge automatically.
