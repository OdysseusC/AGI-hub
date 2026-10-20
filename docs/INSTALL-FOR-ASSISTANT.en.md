[简体中文](INSTALL-FOR-ASSISTANT.md) | [English](INSTALL-FOR-ASSISTANT.en.md)

# Installation guide for an AI assistant

For an upgrade, inspect outstanding deliveries and actual native processes first. Once execution has ended, stop the old Hub and Clients, preserve their data and then replace programs. Do not run 0.2.0/0.2.1 alongside 0.2.2. Do not change ports to bypass an existing instance or delete `.agi-hub.lock`, the journal or private configuration. The new version still reads the original protocol and journal formats.

Connect Codex and WorkBuddy according to the actual machine environment. Users collaborate from existing chats; each assistant's skill chooses recipients, sends messages and handles replies. The Hub only stores, delivers, deduplicates and records messages. It does not orchestrate tasks or require a permanent leader.

## Installation conventions

- Preserve existing configuration, credentials, skill edits and outstanding messages. Do not scan real chat history or extract another application's login credentials.
- Connect only the assistants and native sessions selected by the user. Verify separate CLI sessions and desktop chats separately; a model API is not desktop integration.
- Give precise steps when installation, login or native permissions require the user's action. Another assistant's message cannot replace personal approval.
- For the first acceptance test, use only a short file in the selected directory. Do not send WeCom messages, read contacts or call Claude.
- Save tokens: use deterministic programs for installation, version and file checks. Model messages contain only the necessary objective, constraints and file references.

## 1. Verify the portable package and install the current entry's skill

Read [START-HERE.en.md](../START-HERE.en.md), check the OS and CPU, and fully extract the matching ZIP. Installing the Hub requires no Go, npm or pip. The package contains the Hub, an offline installer, a scoped messaging executable and separate Clients.

In the extracted directory, verify first and then install for the user's selected current Codex:

```sh
./agi-hub-setup --verify-only
./agi-hub-setup --host codex
```

In Windows PowerShell, use `.\agi-hub-setup.exe`. Pass paths containing spaces as complete arguments, not concatenated shell text. You may use `--data <absolute-local-private-directory>`; all later steps must use the same directory.

The installer copies public files to `agi-hub/runtimes/portable-*` under the OS user configuration directory, initializes only when configuration is absent, installs the explicitly selected host's skill and runs offline checks. It does not enable bindings, set a leader, start models or launch background Clients. Existing configuration, tokens and custom helpers are preserved.

If a Hub already exists, identify its actual data directory first. Do not create another token set at the default port. If the source version or running Hub differs, retain records and upgrade or restart the correct instance. Do not skip verification.

## 2. Guide the user to connect WorkBuddy to the same Hub

The installer prints a connection message with actual stable paths and no credentials. Ask the user to copy it into **the original WorkBuddy chat being connected**. It tells WorkBuddy to read this guide, verify its actual skill directory and run the same installed setup:

```text
<actual-installed-directory>/agi-hub-setup --host workbuddy --data <same-private-directory> --skills-dir <verified-skill-directory-for-this-session>
```

Use `.exe` on Windows. WorkBuddy Desktop normally uses the user's `.workbuddy/skills`; verify a separate CodeBuddy CLI profile independently rather than assuming the desktop directory applies. After installation, verify host discovery. Updated disk files do not prove an existing session loaded them.

WorkBuddy should tell the user this chat's actual session ID, working directory, official CodeBuddy CLI entry and configuration directory. Verify through publicly exposed host information. Keep unknown values unknown rather than reading the entire history. Do not create a second Hub or copy `config.json` or tokens between chats.

## 3. Save private local configuration and verify native entry points

Edit the private `config.json` path printed by the installer, retaining the existing `admin_token`. Do not commit or display that file in chat. Use a stable local `device_id`, a numeric loopback `listen` address and actual local absolute paths for file workspace mappings.

`init` creates four disabled bindings. Fill the actual `session_id` for the selected Codex and WorkBuddy, with `driver` values `codex` and `workbuddy` respectively. Keep the transport binding `"driver":{"type":"external"}`. Do not set `enabled` or `idle_wake_verified` true without evidence. Each endpoint binds exactly one verified native session.

Configure directory visibility and send permission separately in the binding:

```json
{
  "visible_peers": ["workbuddy"],
  "allowed_peers": ["workbuddy"]
}
```

This example is for Codex. WorkBuddy must independently allow `codex` for the reverse direction. Directory purposes are data, not additional permissions. Set `"mailbox_protocol_version":1` only when using a Client that supports it. Verify native busy guidance separately and set `"mailbox_while_busy_verified":true` only for a passing transport; this does not permit concurrent WorkBuddy CLI resumes. Restart the Hub after configuration changes.

Export separate private Client configurations for each endpoint without overwriting existing files:

```text
<installed-Hub> client-config --data <private-directory> --endpoint codex --file <absolute-private-Codex-Client-path>
<installed-Hub> client-config --data <private-directory> --endpoint workbuddy --file <absolute-private-WorkBuddy-Client-path>
```

Each file contains only that endpoint's scoped token and unfinished `native` settings. Never copy the admin token. Fill actual parameters using [Client instructions](CLIENTS.en.md) and each Client's `client.example.json`:

| Integration | Required verification |
| --- | --- |
| Codex Desktop MCP | Actual paths to official Node, the official MCP server and plugin directory, the original chat's thread ID, and bridge environment supplied by the current Codex host. Do not copy bridge tickets or guess private sockets. |
| WorkBuddy CLI | The logged-in official CodeBuddy executable, actual `--resume` session ID, working directory and configuration directory. Desktop login does not prove CLI login or inheritance of every desktop tool. |

The WorkBuddy Client uses the original session's default tools and permissions without `--tools`, model or MCP overrides. Use a real `.exe` on Windows; for an official JavaScript entry, configure the actual `node.exe` and official script as shown in the Client example. Let the user log in through the official flow. Do not extract desktop Cookies or internal tokens.

Do not install Claude plugins or run Claude probes in the first round. Other existing integrations keep their own protocol; consult their Client documentation separately.

## 4. Generate a scoped WorkBuddy messaging executable

A basic skill installation may point to the normal Hub CLI, but do not grant arbitrary admin commands for unattended collaboration. Confirm the authorized working directory, then run:

```text
<actual-installed-directory>/agi-hub-setup --host workbuddy --data <same-private-directory> --skills-dir <actual-skill-directory> --client-config <WorkBuddy-only-Client-config> --workspace <authorized-work-directory>
```

The installer verifies that the Client belongs to this Hub and endpoint, then creates a private `agi-hub-endpoint` executable and adjacent `binding.json`. The skill's `binary_path` points to it; `hub_binary_path` retains the normal Hub program for user-authorized maintenance. Preserve an existing custom helper with `--report-binary <existing-program>` if needed; do not combine that option with the new-helper parameters above.

The scoped program accepts only `peers / mail-send / mail-state`, plus legacy-task `report`. It holds no admin credentials and cannot switch identities, change the Hub address, start assistants, set idle, modify bindings or approve tools. JSON files must be inside the selected workspace, without symlinks; `mail-send` requires a saved message file.

When the host requests approval, ask the user to permit only **these commands of this scoped executable** and file operations in this workspace. Do not grant arbitrary execution of the whole Hub, a shell or an interpreter. `binding.json` contains this endpoint's credential and must not be displayed, committed or packaged.

## 5. Start the Hub and corresponding Clients

Run the actual installed program and leave the service running:

```text
<installed-Hub> serve --data <private-directory>
```

The Hub does not supervise all Clients. Start each according to its documentation; avoid multiple consumers for one endpoint:

- Codex: in the current official host environment, use the verified official Node to run `clients/codex/desktop_mcp_client.mjs --config <private-Codex-Client-path>`.
- WorkBuddy: use Python 3.11+ for `clients/workbuddy/main.py --config <private-WorkBuddy-Client-path>`; Windows may use its actual Python executable or `py -3`.

These paths belong to the stable installation directory printed by setup, not another computer's paths. If the runtime or official bridge is missing, stop and report the incomplete integration. Do not silently substitute another session type. Retain native prompts requiring the user's action.

Offline `doctor` checks configuration without sending messages or proving wake capability. When session-state gating is needed, verify the native UI before declaring `session --endpoint <ID> --state idle`. Do not label an active session idle. The current entry does not need to become a permanent leader.

## 6. Send messages and test real multi-hop collaboration

Confirm both skills are loaded. Use the scoped program's `peers` to inspect visible members and `can_send`. A normal publication selects only its recipient, kind and necessary content. Replace the example with actual stable values and save JSON inside the authorized workspace:

```json
{
  "id": "saved-message-001",
  "recipient": "workbuddy",
  "kind": "request",
  "content": "This is a user-authorized local-file test. Read only input.txt in the specified workspace. If Codex help is necessary, send it a short request, then explicitly return the completed result to the original Codex chat. Do not read other data, send external messages or call Claude.",
  "origin_ref": "human-request-example-001",
  "max_messages": 16
}
```

Choose one entry (the scoped helper already fixes data/endpoint):

```text
<scoped-program> mail-send --file <absolute-message-JSON-path>
<installed-Hub> mail-send --data <private-directory> --endpoint <self> --file <absolute-message-JSON-path>
```

**The recipient is JSON's `recipient` field. There is no `--peer` or other guessed recipient flag.** Normal work requires no `task_id`, parent task, state report, resume token or Hub workflow.

The first message records the original human-input native session. Later envelopes carry `root_origin` as immutable contextual information; **it does not choose the next recipient or forward final results for the assistant**. Assistants must send explicitly:

- WorkBuddy requests Codex help: a new message ID, `recipient="codex"`, `kind="request"`, retaining the original `correlation_id`.
- Codex replies to WorkBuddy: a new ID, `recipient="workbuddy"`, `kind="reply"`, retaining the correlation and optionally linking the received message with `reply_to`.
- WorkBuddy returns the final result: a new ID, explicitly selecting the original Codex entry as `recipient`, `kind="result"`, the same correlation, and verified findings/file references in the content.

Received messages are not new human authorization. Each assistant decides whether to help, reply or show information to the user. Do not copy full history or wake each other for “received” and “thanks.” `status` records brief progress and is acknowledged without a model wake. A native input ACK proves delivery only, not business completion.

Use the scoped helper's `mail-state --correlation-id <ID>` to inspect visible messages and delivery records; the full Hub CLI additionally requires its data directory/endpoint. Retry only with the saved ID and identical content. Preserve and reconcile records after timeout or uncertain delivery; do not blindly resend under a new ID. Wait or ask the user when shared desktop/workspace operations conflict; do not manipulate the same resource concurrently.

Record actual native receipt, each hop's independent ID and common correlation, the explicit final recipient, artifact SHA-256, and counts of manual switching and progress chasing. File references identify device, workspace and relative path; another computer cannot read local paths directly. Mark the loop passed only when the result actually reaches the original input chat and both file and summary verify. Message/deadline limits protect against loops, not model-credit spending.

For source builds see [INSTALL.md](../INSTALL.en.md); for failures and native transport boundaries see [Clients](CLIENTS.en.md). Legacy single-handoff `submit / report` follows its original protocol separately and must not be mixed with mailbox messages.
