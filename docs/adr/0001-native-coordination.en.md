---
status: accepted
---

[简体中文](0001-native-coordination.md) | [English](0001-native-coordination.en.md)

# Skills decide collaboration; the Hub provides addressed mailboxes

Users start work in any connected native assistant. Skills discover permitted members; assistants explicitly choose each message's recipient and decide whether to forward, reply or combine results. Assistants send final results back to the session where the user first entered the request. The Hub provides only directory/send permissions, durable messages, delivery evidence, idempotency and shared message limits. It creates no team plan, dependency graph, parent/child task, waiting state or automatic reply.

`correlation_id` associates mail, `root_origin` preserves the original entry and `reply_to` is only a contextual reference. These fields do not force routing: B may ask D for help, and D may explicitly reply to B or directly to A within the permitted scope. The original entry still judges business success and result quality. A transport ACK confirms only native input or CLI transport. Status mail is recorded locally without waking a model; new mail uses neither Task/Report nor settled.

The directory is fetched on demand. Rule changes synchronize managed skills without rewriting every prompt or starting models for member changes. Codex Desktop MCP, WorkBuddy CLI and Claude Code mod 0.2.2 support opt-in mailbox protocol `1`; Claude's original macOS Code idle mailbox execution and busy Codex return passed on 2026-10-20, while other native scopes remain unverified and Grok stays at `0`. The interface protocol remains `1`. The first mailbox publication uses journal format `3` so old programs cannot drop new fields. Historical v1 single-handoff tasks and unpublished v2 prototype diagnostics remain readable; the latter are not resumed.

The tradeoff is that skills and assistants own division of work, return routing and authorization scope. The Hub cannot guarantee a model will reply or infer overall completion from a result's kind. This decision replaces the unpublished parent/child task prototype. The [protocol](../PROTOCOL.en.md), [skill maintenance](../SKILLS.en.md) and [Client support](../CLIENTS.en.md) describe the implementation.
