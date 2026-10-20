[简体中文](2026-10-20-lan-manager-design.md) | [English](2026-10-20-lan-manager-design.en.md)

# LAN Hubs and the local management page

This design implements the approved scope on an isolated feature branch and PR: one Hub per computer, stable host-plus-endpoint addressing, explicit discovery, persistent connections, basic relaying, reliable mail and a small management UI. It does not automatically merge or modify the existing installation.

## Boundaries

Hub remains an addressed message transport, not a planner or model caller. Native clients retain loopback access. LAN traffic has a separate listener and identity. Legacy Task/Report remains local; federation carries mailbox messages. Installation, discovery, pairing, health checks, routing and offline tests invoke no models.

## Identity, discovery and pairing

Persist a host key and stable Hub ID; names and IP addresses can change without changing identity. Address endpoints as `hub-id/endpoint-id`; native session binding stays local to the destination.

LAN communication and multicast discovery are separate, explicit, default-off controls. The first version uses IPv4 mDNS/DNS-SD `_agihub._tcp.local.` on a selected IPv4-capable interface, advertising only host identity, name, service address and protocol. Service records support A and AAAA. A manual numeric LAN address can override the invitation's connection location while retaining its pinned identity and public-key checks. Discovery contains no credentials, chat history or mail and grants no trust. Turning discovery off does not disconnect paired hosts.

Pair through an expiring invitation and confirmation by both parties, then pin the peer key. Initial pairing requires direct connectivity; an unknown relay cannot establish trust. Advertise only explicitly shared local agents. Distinguish discovered, paired, connected, relayed and offline states. Reachability does not grant permission; trust is not transitive. A and C must pair with each other before later communicating through B.

## Transport and reliability

Paired hosts use TLS 1.3 and persistent HTTP keep-alive connections for directory, routes and mail. Packets have stable IDs, source and destination hosts/endpoints, correlation, expiry and a bounded hop count. Authenticate the origin end to end; a relay cannot impersonate it, expand authority or modify content. The destination checks local bindings and sharing policy.

Prefer an available direct connection, otherwise use breadth-first search through valid neighbor advertisements to select a shorter relay path. Bound paths and visited nodes with `maxHops = 8`, prevent loops and expire routes with their advertisements. No latency adaptation, learned weights or aggressive topology optimization. Direct connections can be reused only within established trust.

Persist outgoing mail before accepting it. Reconnect and resend using the same ID. Identical duplicates are idempotent; conflicting duplicates are rejected. A network acknowledgement confirms durable destination-Hub receipt, not native delivery or business completion. Preserve existing uncertain-native-delivery semantics.

Federation protocol 1 fixes each conversation to two endpoints addressed as `<64-character-lowercase-hex-Hub-ID>/<endpoint>`, with canonical `lan-…` IDs returned on submission. Divide the total message budget into `ceil(max_messages/2)` permits for the origin and `floor(max_messages/2)` for its peer; permits cannot be copied or borrowed. A third endpoint requires a separate user-authorized conversation, never a new correlation to bypass exhausted limits. Cancellation is local; already queued or dispatched mesh messages can continue. The first federated write upgrades journal snapshots to v4, so older binaries cannot be used for a blind downgrade.

Federate messages and directories in this version. File references retain host/workspace ownership and are not interpreted as local files or automatically synchronized.

## Management page

Extend the existing local web page and SSE. Show this Hub's name, network/discovery toggles and counts; list local agents, expandable remote Hubs and their visible agents, and recent mail. Counts describe this Hub's view. UI updates do not invoke models.

Local agent actions install managed skills, guide configuration/binding, enable or disable, and explicitly share endpoints. Installation uses fixed in-repository APIs, not arbitrary shell commands. Managed skill installation is available for Codex, WorkBuddy and Claude; independent native Clients, the Claude plugin and Grok Routine remain explicit follow-up setup steps. Distinguish installed files, pending binding, configuration, unverified connectivity and validated capability. Disabling an endpoint does not cancel a native action already running.

Network actions create invitations, request/accept pairing, disconnect and add manual addresses. Do not expose private configuration or tokens. Administrative APIs stay on loopback.

See [LAN Hubs and local management](../../LAN.en.md) for operational steps, API inputs and data migration.

## Acceptance

Offline tests cover default-off networking, discovery lifecycle and expiry, protocol parsing, stable identity, unknown-peer rejection, confirmed pairing, expired invitations, same-name endpoints, three-Hub relaying, loop prevention, reconnect/restart, identical and conflicting duplicates, permission changes, idempotent installation and local API validation. Temporary installations and simulated native clients test mail entering and leaving local mailboxes; browser checks cover the tree and error states.

Real multi-computer multicast, different interfaces/firewalls and native assistants need separate user-environment validation. Report these limits in the PR. Do not deploy over the existing installation, invoke Claude or merge automatically.
