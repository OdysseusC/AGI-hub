# LAN Hubs and local management

[简体中文](LAN.md) | [English](LAN.en.md)

Run one Hub per computer. Native assistants retain their local Clients and existing sessions. Explicitly paired Hubs exchange shared-agent directories and mailbox messages. The Hub does not choose collaborators, call models or treat a network receipt as completed assistant work.

## Enable networking and pair

1. Install and start a Hub on each computer using the [installation guide](../INSTALL.en.md). Open each computer's local management page, normally `http://127.0.0.1:7331`, and use its existing local management credential. Administrative APIs and native Clients remain on loopback.
2. Set the host name and a LAN listen address, such as `192.168.1.20:7347`. Enable LAN communication and save. The default LAN port is `7347`, separate from management; do not change the administrative listener to a LAN address. With `0.0.0.0:7347`, select an interface so management can derive its actual IPv4 address for advertisements and pairing. Selecting an interface does not narrow a wildcard TCP listener to that interface; enter the interface's specific address to restrict TCP reception.
3. Optionally enable discovery and explicitly select an interface. LAN communication and discovery both default to off. This version uses IPv4 mDNS/DNS-SD on the selected interface, service `_agihub._tcp.local.`, UDP port `5353`. The interface must be active, multicast-capable and have an IPv4 address. Service records can carry IPv4 or IPv6 addresses; discovery transport on IPv6-only networks is not implemented.
4. Generate a five-minute invitation on one computer and give it to the other. Paste the invitation on the other computer, optionally enter a numeric destination IP and port, and submit the request. Without an override, the invitation supplies the address. A manual address changes only the connection location; the invitation's Hub identity, public key and proof remain pinned. Hostnames and public addresses are unsupported; private, link-local and RFC 6598 shared addresses in `100.64.0.0/10` are accepted, while loopback is useful only for local tests. Unspecified addresses such as `0.0.0.0` are for listeners only, never advertisements or connection destinations.
5. On the inviting computer, check the request's Hub ID, name and address, then accept. The requesting user's submission and the inviting user's confirmation establish the pairing. Initial pairing requires direct connectivity; another Hub's trust cannot substitute for it.
6. Explicitly share the local assistants that should collaborate on both computers. Sharing requires an enabled endpoint, a real native session binding and mailbox protocol 1. Only shared assistants enter the remote directory.

If multicast is blocked, leave discovery off and exchange invitations with a manual address. Permit the selected LAN TCP port according to your network policy; UDP `5353` is needed only for discovery. Discovery does not scan subnets, ports or assistant sessions.

An existing private overlay such as Tailscale can use its numeric IP for manual pairing; addresses in `100.64.0.0/10` need no substitution, and private IPv6 ULA addresses also work. First verify that the two computers can already communicate directly, then select the overlay's concrete listen address and port. Hub neither installs nor configures an overlay, changes firewall or network permissions, nor depends on Tailscale. Do not assume that the overlay forwards mDNS; automatic discovery can remain disabled for manual pairing.

Turning discovery off withdraws announcements, stops searches and clears discovered candidates, while preserving pairings and authorized connections. Turning LAN communication off stops that transport while retaining persistent identity and pairings. “Disconnect” revokes trust in the selected Hub; connecting again requires a new pairing.

## Identity, directory and routing

The Hub ID is a 64-character lowercase hexadecimal value derived from its persistent public key. Changing a name or IP does not change identity. Address same-named assistants as `<Hub ID>/<endpoint>`; native session IDs remain in the destination computer's local bindings.

Discovery advertises only Hub ID, name, service address and protocol version. It carries no agent directory, credentials, message body or session history. Candidates are unauthenticated hints and never establish trust automatically. Cache records expire; a goodbye shortens their remaining lifetime to a one-second grace period. Disabled discovery does not refresh candidates.

The management page distinguishes discovered, paired, online, relayed and offline hosts. Counts describe this Hub's view. Paired hosts exchange signed directory and route advertisements containing only explicitly shared local agents.

Transport uses TLS 1.3 with pinned peer keys and reuses long-lived connections through HTTP keep-alive. An available direct link takes priority; otherwise breadth-first search selects a shorter route through valid neighbor advertisements. Paths and visited nodes are bounded by `maxHops = 8`, reject loops and expire with their advertisements. There is no latency learning, adaptive weighting or guarantee of a globally optimal route.

A relay supplies transport only. The destination verifies the original message signature and checks pairing and local sharing permissions. A trusting B and B trusting C does not make A trust C. A and C must establish their own direct pairing first; later, they can use B as a relay when their direct link is unavailable.

## Federated mailbox

Use the existing fixed-identity helper, `mail-send --file <absolute-path>`, with the full recipient address returned by the directory:

```json
{
  "id": "saved-lan-request-001",
  "recipient": "<64-character-lowercase-hex-Hub-ID>/codex",
  "kind": "request",
  "content": "Check the specified material within the user's authorized scope and reply with findings.",
  "origin_ref": "actual-human-request-reference",
  "max_messages": 16
}
```

Replace the Hub ID placeholder before submitting. Both sending endpoints must be explicitly shared, the destination Hub must be paired, and the recipient must appear in its valid shared directory. Relaying does not change the recipient or grant additional authority.

A successful submission returns a canonical `lan-…` message ID and `correlation_id`. Continue with that returned correlation ID, and use returned message IDs in `reply_to`; the original local `id` is not the canonical network ID. Preserve the original submission file and ID when reconciling a retry. Identical content under the same ID is idempotent; conflicting content is rejected.

Client responses for federated messages retain the original submission ID in `source_message_id` so clients can verify the mapping. Native assistants receive one copy of the body, without the internal network payload or transport fingerprint, to avoid duplicate context.

Each federation protocol 1 conversation has exactly two addressed endpoints. Its `max_messages` budget is assigned at creation: the initiating endpoint receives `ceil(max_messages / 2)` permits and the other receives `floor(max_messages / 2)`. A budget of 5 splits into 3 and 2; a budget of 16 splits into 8 and 8. Concurrent or disconnected writers cannot multiply permits, and an exhausted sender cannot borrow the other's remaining allocation.

Participants, original entry reference, total budget and deadline are immutable within the conversation. Contacting a third endpoint requires a separate user-authorized conversation. Never create a new correlation, change IDs or reset a budget to evade the current conversation's exhausted limit. Defaults are 16 total messages and 30 minutes; the budget range is 1–64, with a deadline in the next 24 hours at submission.

`network_pending` means the local Hub durably saved the message and is waiting for network delivery. `remote_received` means only that the destination Hub durably accepted it. Native Client claims, actual session consumption, tool execution and artifact verification still require their own evidence. `used_messages` counts messages observed by the current Hub, not a globally synchronized network total.

Canceling a mailbox conversation has local scope. Messages already queued in mesh or dispatched can continue, and remote native work is not canceled. Reconcile uncertain delivery instead of repeating side effects.

This version exchanges messages and shared directories without file synchronization. A remote path still belongs to its source computer and workspace; do not open it as a local path. Transfer required files through a separately authorized delivery mechanism.

## Agent collaboration language

Local agent cards offer 41 common languages and custom language tags such as `en`, `zh-Hans`, `nb` and `sr-Latn-RS`. Claude defaults to English; other agents default to `auto`. Chinese remains selectable. The preference covers inter-agent instructions, necessary context and replies, without overriding the user's requested final-response or deliverable language.

Saving updates the endpoint's `communication_language` and synchronizes `communication-policy.md` in registered managed skill installations. Only unmodified managed files are updated. The page reports synchronization outcomes and conflicts separately from a successful setting save. Unregistered platforms are not scanned or installed. Running agents must reread the rules; disk updates do not prove native loading.

Before each new send, the sending agent reads the latest directory and translates necessary instructions according to the recipient's `send_language`, then submits one concise message. A Claude participant's preference takes priority; between two Claude endpoints, the recipient wins. Otherwise prefer the recipient's explicit choice, then the sender's; if both are `auto`, the agent chooses appropriately. Remote preferences propagate through signed directories with refresh delay; use matching Hub versions.

Hub neither translates nor detects prose language and invokes no additional model. Preserve attachments, code, paths, identifiers and verbatim source passages; do not send full original and translated prompts together. A message's `communication_language` captures the submission-time rule. Later changes cannot rewrite existing messages or identical-ID retries. Human-facing answers follow the user's explicit language request, or their conversation language when unspecified. Actual compliance requires native validation; offline tests do not prove translation quality.

## Management APIs and installation states

These endpoints are available only on the local loopback management listener and require existing administrator authentication and Host/Origin checks. They are not exposed on the LAN listener.

| Endpoint | Input or result |
| --- | --- |
| `GET /v1/manage` | Local identity, controls, interfaces, agents, candidates, paired hosts and pending requests; excludes private keys and endpoint tokens |
| `POST /v1/manage/settings` | `name`, `network_enabled`, `discovery_enabled`, `listen`, `interface` |
| `POST /v1/manage/agent` | `endpoint`, `action`; `enable` may take a real `session_id`, while `language` requires `communication_language`; other actions are `install`, `disable`, `share`, `unshare` |
| `POST /v1/manage/network` | `action`: `invite`, `pair`, `accept` or `disconnect`; `pair` takes `invitation` and optional `address`; `accept` uses the pending request ID in `peer_id`, while `disconnect` uses the Hub ID |

Ordinary page updates use local SSE and do not wake models. An invalid interface, startup socket failure or runtime transport error disables discovery and exposes the error. A missing discovery adapter after restart does not make the existing local mailbox unavailable.

The install button invokes the repository's fixed managed-skill installer for Codex, WorkBuddy and Claude. It does not install or validate every native Client, Claude plugin or Grok Routine. Complete independent Client setup, private configuration and real session binding using the [Client documentation](CLIENTS.en.md). Files installed, session bound, Client connected and native capability verified are distinct states. Installation, discovery, pairing, health checks and routing call no models. Disabling an endpoint does not cancel native work already running.

## Data and verification scope

The data directory gains `network/identity.json`, `network/state.json` and `discovery.json`, holding the private host identity, network state and discovery preferences. Do not copy identity files, invitation proofs or private configuration into public documentation, repositories or logs. Preserve the identity file to retain the same Hub ID after restarting.

The first federated message upgrades the snapshot version in the local append-only `state.jsonl` journal to **4**. Older programs cannot safely interpret these fields. Back up the complete data directory before upgrading; a rollback requires matching software and a complete compatible backup. Do not point an older executable blindly at the upgraded directory.

Offline tests use temporary directories, simulated native Clients and injected discovery sockets. They cover parsing, expiry, lifecycle, pairing, directories, durable messages, replay, loop prevention and management operations. Development checks include:

```text
go test ./internal/discovery ./internal/mesh ./internal/manager ./internal/app
node --test internal/app/web/dashboard.test.cjs
```

These tests do not activate real LAN discovery or call models. Two physical computers, different interfaces, firewalls, multicast policies, sleep recovery and native assistant execution need separate validation in an authorized target environment. Existing local native acceptance does not automatically establish cross-computer acceptance.
