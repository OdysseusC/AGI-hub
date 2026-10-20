[English](TEAM-ORCHESTRATION.md) | [简体中文](TEAM-ORCHESTRATION.zh-CN.md)

# Skill-driven mailbox collaboration

Users start work in a native assistant. Each assistant reads its own mailbox and decides how to handle work, ask for help and reply; the Hub only transports messages.

## Example

A sends separate messages to B, C and D, optionally sharing one correlation ID. If B needs D's help, B sends D another message. D decides from the context whether to reply to B or directly to A. A combines the received results according to the original user's request.

Every send must specify `recipient`. `root_origin` preserves the original user entry and `reply_to` preserves message association; neither creates a mandatory reply chain. An intermediate assistant does not have to finish all its “subtasks” before sending mail. The Hub has no parent/child work tree or waiting executor.

## Minimal conventions

- Native skills look up available, permitted members when needed and send a clear small objective, the original authorization scope, necessary context and file references.
- A received message is not new human authorization. Continue the existing correlation; do not use a new message or correlation ID to bypass uncertain delivery, limits or cancellation.
- Requests require actual handling, and replies must fit the existing work. Do not reply again unnecessarily. Thanks, acknowledgments and duplicate ACKs should not trigger a model.
- Final results normally go to the original entry for delivery to the user. Assistants decide who combines work; the Hub does not impose a permanent leader.
- Participants explicitly coordinate writes to a shared project or desktop. Do not indiscriminately copy all history or modify the same target concurrently.

## Messages and limits

`request / reply / status / result` are all explicit messages. Status does not wake a model by default. The Hub neither derives results automatically nor closes the user's entire work automatically.

A conversation contains only its original entry, participants, message count, deadline and closed flag. Each published message consumes one shared slot; retrying the same ID does not consume another. The default is 16 messages, at most 64. Near the limit, return the known result or actual obstacle first. A prompt is not a hard spending limit.

Delivery and business completion are separate. The Client records pending/dispatching/delivered/uncertain/failed; the assistant states business results in an explicit reply or status. An ambiguous native result is not automatically retried.

## Installation and extension

The skill stays small and stable. The member directory is fetched when needed, and managed revisions update the rules. Do not generate a separate skill for each assistant pair or wake models for directory changes.

Each product has its own Client using its actually supported CLI, messaging interface or webhook. A later Client can explicitly provide new sessions or specialist models without first introducing a universal Agent runtime. Calling a model API must not be presented as connecting its native desktop assistant.

Codex Desktop MCP and WorkBuddy CodeBuddy CLI are the first verification targets. Claude Code mod 0.2.2 implements mailbox protocol 1 with native messaging tools; original macOS Code idle execution and explicit return to busy original Codex passed on 2026-10-20. Claude busy queuing and other hosts still require acceptance. Grok retains its legacy integration with mailbox 0. Verification scope is recorded separately for each platform and host version.

For operation and installation, see [START-HERE](../START-HERE.md), the [protocol](PROTOCOL.md) and [skills](SKILLS.md).
