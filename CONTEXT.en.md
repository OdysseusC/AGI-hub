# AGI Hub

[简体中文](CONTEXT.md) | [English](CONTEXT.en.md)

Users work from any connected native assistant. Skills choose other assistants as needed, send addressed messages and process replies. Assistants send final artifacts back to the chat where the user first asked. The Hub stores messages, a directory, delivery evidence, deduplication records and basic limits. It does not plan business work or automatically reply or summarize.

## Terminology

**Assistant instance**: a specific assistant product together with its computer, host, session, native tools and the user's logged-in environment. Calling the same model through an API does not connect that assistant.

**Session**: a native ongoing conversation that can be explicitly located. Mailboxes bind to actual sessions. The end of one model turn does not mean the user's work is complete.

**Root origin**: the session where the user entered this request. A message's `root_origin` retains the endpoint, device, native session and request reference so the skill can determine where to deliver the final result. It is neither a Hub-enforced routing rule nor proof of new user authorization.

**Mailbox**: the addressed inbox of a configured endpoint. A Client claims only messages addressed to itself and translates wakeups into mechanisms actually supported by its host. The Hub does not choose recipients from message content.

**Message**: an independent ID, explicit sender and recipient, kind and necessary content. `request/reply/status/result` describe content without creating a parent/child task state machine. Assistants explicitly send replies; neither the end of a model turn nor a Report generates one automatically.

**Conversation / Correlation**: messages sharing a `correlation_id`, which retains the root origin, participants, deadline and total message allowance. It contains no plan, dependencies or completion conditions.

**Reply to**: an optional previous message ID, used only to understand context. It does not force a reply to the original sender. The sending assistant always chooses a recipient within the user's authorized scope.

**Peer collaboration**: any permitted entry can initiate, respond or forward, without a permanent leader or a new chat entry point. Peer communication does not permit impersonating the user, expanding authorization or sharing another assistant's permissions.

**Collaboration skill**: instructions for conserving context, discovering visible members, checking authorization, choosing recipients, calling the mailbox and verifying artifacts. The skill itself does not listen for messages, provide wakeups or approve tool permissions automatically.

**Client**: the messaging implementation connecting a specific native assistant to the Hub, as a separate process or host plugin. CLI sessions, original desktop sessions, cloud webhooks and specialized models must describe their capabilities separately; none can stand in for another without evidence.

**Status**: progress or a request for human action that does not require the receiving model's judgment. The Client stores and acknowledges it without injecting it into the model. Use request, reply or result for content that an assistant must actually process.

**Delivery**: pending, dispatching, delivered, uncertain and failed describe transport. A Client ACK is not business completion. Assistants describe native work progress through separate messages.

**Uncertain delivery**: an operation may have occurred but lacks confirmation. Retain the original record; do not consume it again automatically or resend under a new ID.

**Artifact**: a verifiable file or structured result. Files identify their device, workspace and location. Mailboxes do not copy them between computers automatically. The final entry checks quality and evidence against the user's needs.

**Needs user**: a business situation requiring human judgment, information or native approval, described by the assistant. Other assistants cannot approve on the user's behalf. It is not a Hub task phase in the new mailbox protocol.

Mailbox protocol `1` has native acceptance evidence for Codex Desktop MCP, WorkBuddy CLI and Claude Code mod `0.2.2`. The Claude test on 2026-10-20 covered idle wake of an existing macOS Claude-3p `2.1.299` Code session, native file Read and explicit `mail_send`, then return through the Hub and independent Codex Desktop MCP into the busy original entry. Claude is Code-only and queue-only; ordinary Chat/Cowork and busy steer are unsupported. Other hosts/platforms require separate acceptance. See the [native record](docs/NATIVE-VALIDATION.en.md). Grok Routine Client `0.2.0` implements queue mailbox protocol `1` with offline coverage; real Routine receipt, native execution and fixed-identity replies still require configuration and acceptance. The actual `0.1` Client must be upgraded, rather than changing its declaration alone. Legacy one-shot Task/Report and its native acceptance records remain compatible, without proving that the new mailbox is connected. The old default leader belongs only to the compatible task interface.
