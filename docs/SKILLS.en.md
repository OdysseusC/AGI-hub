[简体中文](SKILLS.md) | [English](SKILLS.en.md)

# Installing native-assistant skills

Each connected session installs the same `agi-hub-client` skill to discover members when needed, explicitly select recipients, handle associated replies and send final results to the session where the user first entered the request. The Hub stores addressed messages and delivery evidence only; assistants decide division of work, forwarding and aggregation. Skills do not listen for messages; native wake is the corresponding Client's job.

Mailbox mode supports Codex Desktop MCP, WorkBuddy CLI and Claude Code mod 0.2.2, with explicit `mailbox_protocol_version:1` and `allowed_peers`. Claude original macOS Code idle mailbox execution and busy Codex return passed on 2026-10-20; Grok retains mailbox `0`. Other hosts and Claude busy queuing remain unverified. Older single-handoff success does not prove the new mailbox loop. Claude uses fixed-identity native `peers`, `mail_send` and `mail_state` tools with a lease and JSON-text tool results, not Bash or automatic business replies.

## Installation

Use the [installation guide](INSTALL-FOR-ASSISTANT.en.md) to build/initialize the sidecar and configure only user-selected endpoints. Use the actual installed program and private data directory:

macOS / Linux:

```sh
/absolute/path/agi-hub install-skill --host codex --endpoint codex --data /absolute/path/private
/absolute/path/agi-hub install-skill --host workbuddy --endpoint workbuddy --data /absolute/path/private
/absolute/path/agi-hub install-skill --host claude --endpoint claude --data /absolute/path/private
```

Windows PowerShell:

```powershell
& 'C:\Users\you\bin\agi-hub.exe' install-skill --host codex --endpoint codex --data 'C:\Users\you\private\agi-hub'
& 'C:\Users\you\bin\agi-hub.exe' install-skill --host workbuddy --endpoint workbuddy --data 'C:\Users\you\private\agi-hub'
& 'C:\Users\you\bin\agi-hub.exe' install-skill --host claude --endpoint claude --data 'C:\Users\you\private\agi-hub'
```

These are example paths; replace them with actual local absolute paths and configured endpoints. The current AI assistant can perform installation without downloading scripts, extra package managers or changing host permissions. CLI `--binary` explicitly records the actual absolute executable the receiving assistant will use later.

The installer writes protocol instructions and installation metadata, not host tool permissions. Noninteractive CLI messaging still requires permission to read that metadata, run the selected messaging program and access this workspace. Discovery does not grant those operations. Users choose narrowly scoped official CLI configuration; handoff messages and skills cannot replace approval or authorize unrelated directories/commands.

| Host | Default skill directory | Boundary |
| --- | --- | --- |
| Codex | Current user's `~/.codex/skills` | Installs the protocol skill; verify loading in the actual existing session |
| WorkBuddy | Current user's `~/.workbuddy/skills` | Selected from local 5.8.2 scanning behavior; verify in the skill UI and a real task, not a guarantee for every version |
| Claude Code | Current user's `~/.claude/skills` | Installs Code-session receiving instructions; a separate `agi-hub-endpoint` mod wakes it. Ordinary Chat/Cowork/WSL are unsupported. |
| Other hosts | No default | Follow official documentation or a locally verified directory with explicit `--skills-dir`; identical instructions do not prove wake support |

Custom directory example:

```text
<installed-binary> install-skill --host <host> --endpoint <configured-endpoint> --data <absolute-data-dir> --skills-dir <verified-absolute-skills-dir>
```

Angle brackets are values to replace. Install only into the selected host; do not scan application configuration in bulk, extract logins, register other assistants or switch leaders. Verify each host's skill format/loading conditions separately. Where UI import is required, use the native skill UI and preserve the user's approval flow.

For a separate WorkBuddy CLI profile, use `--skills-dir` for the directory actually scanned and verified by that environment. The desktop default does not automatically apply to every CLI profile. Verify loading before configuring skill reads, the selected messaging helper and this workspace's file permissions.

In legacy Task/Report compatibility mode, a Claude Code mod providing `mcp__agi-hub-endpoint__report` is preferred: the plugin fixes endpoint and the actual task turn ID, without Bash reporting. Go CLI `report --file` is a fallback for other supported environments, not a way around Claude's lease, binding, report provenance or idempotency. Plugin, skill and permissions are installed separately. A skill file neither proves plugin loading nor idle wake. Claude Code validation used 2.1.299; actual N2/N3 outcomes are in [local records](NATIVE-VALIDATION.en.md).

## Installed files and updates

The directory is always `agi-hub-client/` under the skill root:

```text
agi-hub-client/
  SKILL.md
  installation.json
  .agi-hub-manifest.json
```

The public `SKILL.md` defaults to Chinese, with `SKILL.en.md` as the English option. The installer reads the English source and writes only the one English `SKILL.md` required by the host. The Chinese reference is for readers, not a second registered skill or duplicated model prompt.

`installation.json` contains `host`, `endpoint_id`, `binary_path`, `data_dir` and optional `hub_binary_path`. `binary_path` selects the messaging entry or legacy reporting program. A WorkBuddy helper can fix its own endpoint; the full Hub CLI explicitly uses metadata's data directory/endpoint. If maintenance uses another program, register it separately with `--hub-binary <actual-Hub-CLI>` without replacing the scoped helper or modifying permissions. Otherwise both uses share `binary_path`. Metadata contains no credentials, logins or history; the CLI authenticates the corresponding endpoint internally.

The ownership manifest records the installer identity and SHA256 of two managed files. Reinstalling identical content leaves files unchanged. Upgrades replace only files still matching the manifest and preserve other local files. Missing manifests, manual edits, symlink targets or symlink-containing paths refuse overwrite. The user can inspect/back up conflicts before explicitly choosing an installation location.

Files are written to same-directory temporaries, synchronized, then renamed. The installer never deletes an original to force an overwrite after failure. A host holding a file open can prevent updates on Windows; close it before retrying. Local installer locks coordinate concurrent installers. Inspect crash-left locks rather than blindly deleting a live one. An interrupted upgrade can leave files and manifest mismatched; stop and preserve them for reconciliation. This is not isolation from other processes under the same OS account.

### Automatic synchronization and versions

A successful `install-skill` registers the location in private `skill-installations.json` and synchronizes this Hub's registered managed locations; `serve` also synchronizes on startup. Existing installations must be explicitly reinstalled once with the original path, endpoint and reporting program to register them; other applications are not scanned. A deleted skill directory is an uninstall: synchronization reports `missing` without reinstalling it.

```sh
agi-hub skills-status
agi-hub sync-skills
agi-hub sync-skills --endpoint workbuddy
agi-hub peers --endpoint codex
agi-hub skill-rules --endpoint codex --content --known-version 'sha256:previous-digest'
```

All support `--data <private-data-directory>`. `skills-status` is read-only. Full synchronization is for local installation maintenance; a skill requests maintenance only for its own `--endpoint`. Each location returns `current / updated / conflict / missing / error / incompatible`, disk/expected versions and `host_load_state=unknown`. Conflicts fail the synchronization command but do not block journal recovery. Edits, unmanaged directories, symlinks, routing mismatches and occupied-file failures remain available for inspection.

Rule versions use full SHA256 independently of manifest format `1`. Maintainers must increment `install.SkillRevision` when changing embedded instructions. The private registry records accepted revision/digest, rejecting downgrades and changed content under one revision. Synchronization also verifies matching rules in the running Hub and CLI. Only an explicitly refused connection permits offline maintenance; timeout/authentication errors are not “offline.” Older programs predating managed synchronization can still explicitly overwrite skills, so do not run old installation scripts against the same location.

`GET /v1/catalog?endpoint=ID` returns only enabled members in that endpoint's `visible_peers`, empty by default, without credentials, session addresses, workspace paths or chat history. Set e.g. `"visible_peers": ["workbuddy", "claude"]` and a short `purpose` in a binding. Visibility grants no send permission; `allowed_peers` separately allows recipients. Configuration changes require restarting `serve`. The directory has its own digest including rule version and is queried as needed; lists are not written into every skill and updates do not wake models.

`GET /v1/skills?endpoint=ID` returns versions by default, body only with `content=1`, and omits unchanged content for matching `known_version`. If an older report-only installation cannot query, continue already-delivered compatibility tasks and state that synchronization queries are unavailable. Do not bypass limits with admin credentials. Addressed mail supports assistant-chosen forwarding/replies now, but a skill update cannot enable consumption on a host without mailbox support. See the [protocol](PROTOCOL.en.md).

### How updates take effect in each host

Checked on 2026-10-19. Installed files, host discovery and a model actually reading content are separate facts.

| Host | Update/loading mechanism | Role of wake/messages |
| --- | --- | --- |
| Codex | Official automatic detection; local `0.163.0-alpha.10.2` actually discovered the managed skill through zero-model `skills/list(forceReload:true)`. Restart as documented if absent. | An assistant can run an authorized installer, but writing files needs no model wake. Discovery does not prove the original chat read new content. [Official skills](https://learn.chatgpt.com/docs/build-skills) |
| Claude Code / local Claude-3p Code `2.1.299` | Watches local `SKILL.md`; `/reload-skills` if needed. Old content remains in history; read the latest when collaborating next. | A native command differs from injected ordinary text: sending `/reload-skills` text does not prove command execution. Avoid resuming large contexts for synchronization. [Skill lifecycle](https://code.claude.com/docs/en/skills#skill-content-lifecycle) |
| WorkBuddy Desktop `5.8.3` / CodeBuddy CLI `2.161.0` | Local desktop package watches skill directories; a new `--resume` process rescans its actual configuration directory. Resident CLI `/reload-plugins` also reloads Skills, Hooks, MCP/LSP and must be chosen separately. | Managed file writes need no model and do not remove old instructions from history. `CODEBUDDY_CONFIG_DIR` selects the actual directory; do not mix desktop/separate profiles. [Skills](https://www.codebuddy.cn/docs/cli/skills) · [Refresh commands](https://www.codebuddy.cn/docs/cli/slash-commands) |
| Grok Bot | Cloud Private skills and Routine are confirmed, but no general skill CRUD API or local directory is verified. Excluded from file synchronization. | Official chat-based skill saving consumes a model and needs result checks; Routine webhook only starts work. This maintenance path awaits local acceptance. [Official explanation](https://docs.x.ai/grok-bot/skills-routines-and-automations) |

Claude Client plugin-code updates separately use native plugin update and `/reload-plugins`; ordinary skill/directory changes do not reinstall the plugin. Do not automatically use `--force`, which may invalidate prompt caching for the next request. See [plugin updates](https://code.claude.com/docs/en/discover-plugins#keep-plugins-updated).

Message wake can therefore initiate user-authorized maintenance, but is not a universal skill-update mechanism. By default the local installer updates managed files directly. Native refresh/cloud save follows each adapter's verified support. Ordinary collaboration cannot add install locations, approve permissions or expand connectivity. See the [synchronization design](TEAM-ORCHESTRATION.en.md#installation-and-extension).

## Mailbox conventions

1. Check `mailbox:true` and match `recipient` to the installed endpoint; retain independent `message_id`, `correlation_id`, `root_origin` and optional `reply_to`.
2. When help is needed, query the directory and choose a member with `can_send=true` and mailbox protocol `1`. Save each independent ID and necessary content; continue the same correlation/authorization without resetting limits through a new correlation.
3. Explicitly choose each reply's `recipient`. `reply_to` is only a reference, not a mandatory return to the previous sender. Within user-permitted connectivity, final `result` may go directly to `root_origin.endpoint`.
4. After asking for help, the turn may end while the native Client later brings new mail. No parent task, waiting state, run ID or settled receipt is needed. Ending a turn does not automatically reply.
5. `status` enters local Client records without waking a model. Use `request/reply/result` for something requiring recipient judgment, with a clear operation scope. Avoid pointless “received/thanks” loops.

Full Hub CLI example:

```sh
agi-hub mail-send --data /absolute/private/agi-hub --endpoint codex --file /absolute/work/request.json
agi-hub mail-state --data /absolute/private/agi-hub --endpoint codex --correlation-id mail-example
```

A scoped helper fixing its endpoint uses its own `mail-send --file` and `mail-state --correlation-id`. See [message format](PROTOCOL.en.md#addressed-mailboxes). Metadata/file references carry only necessities. Identify each file's device/workspace without copying full history or private credentials.

Models explicitly send business progress, requests for personal action or result mail. Only Clients submit transport ACKs after actual CLI exit or Desktop MCP input confirmation. Delivery is not completion: new mail never calls old `report`, and the Hub never generates its results. The final entry verifies findings, sources and files; authorization remains the user's decision.

## Native acceptance

First test installation, bindings, duplicate publication, uncertainty and routing offline. Then verify native entry points with a small user-authorized task:

1. The actual original session starts from idle, or receives guidance through a verified busy transport.
2. At least one assistant-to-assistant handoff uses explicit sender-chosen recipients.
3. An independent result reaches the original input chat and is verified there, without another entry, manual carrying or fabricated execution receipts.
4. Duplicate notifications do not repeat side effects. Preserve uncertain delivery; explain required personal action. Installing a skill cannot approve native permissions.

See [Clients](CLIENTS.en.md) for support and [dated records](NATIVE-VALIDATION.en.md) for actual machine-specific processes/limits. Offline checks are not native wake acceptance; old protocol success does not prove a new mailbox loop.

## Legacy Task/Report native evidence

The following retains actual single-handoff records. Only old `kind=task/control` uses Report; old `kind=result` is display-only. New mail need not enter that state machine.

The independent WorkBuddy CLI verified actual skill loading and active reporting. After native denial and confirmation that the first task did not execute, narrow skill-reading, actual-workspace read/write and dedicated-report-helper rules were corrected before resuming from an absolute working directory. SSE claimed the second task in the same existing session, which completed on first execution. No tool/permission overrides were passed. Both sessions actively reported `accepted/running/succeeded` and created artifacts with verified SHA-256.

The two tasks used different execution IDs and correct result associations. Identical Submissions and saved native Reports deduplicated, and terminal resources were released. Results on 2026-10-14 returned through Codex's in-host official tool. On October 15, independent Node in the natural host environment called official Desktop MCP, obtained a successful matching-target ACK and guided input into the busy original chat. Resident Node then automatically completed a new MQ → fixed independent WorkBuddy CLI → native reports/files → independent MCP busy return loop, with no manual forwarding/clicks. A later idle diagnosis through the same Desktop MCP checked idle/old turn completed once, sent once, started a different turn and retained official desktop read_thread. Earlier full independent CLI MQ tests covered busy return; this standalone diagnosis added no MQ task and did not prove a complete idle business loop. App Server proxy lacking its control socket is a separate transport.

The original WorkBuddy session/configuration also passed two default authentication continuations that day. A query first failed local Bash/Read, then executed after precise rules were corrected and returned remote `850003 / authorization expired`. Five temporary rules were restored byte-for-byte. These historical stages remain; after user reauthorization, a fresh query returned candidates successfully.

That business flow used the original endpoint, session, working directory and `.workbuddy`, with a managed skill bound to an endpoint-scoped helper calling only `/v1/report`, and a separate artifact workspace. Four report-install permissions covered only skill reads, the artifact directory and selected helper. Four temporary query permissions were removed afterward; reporting permissions remained. The first delivery was not accepted under old diagnostic instructions; after zero tools/files/Reports were verified, the entry explicitly continued once. Stdin was a new message, and the Client's fixed positional instruction only identified the current handoff without expanding authorization. Task/resource locks and delivered state were preserved without requeue or fake reports.

The original session then actively reported `accepted/running/succeeded`. One bundled official contact search returned candidates matching the artifact's name, department and title. Core verified the file, generated an associated result and independent Node Desktop MCP automatically returned it to the busy original entry. Resources were released and the history prefix remained intact. No sandbox was disabled or private desktop env/auth/ticket copied. This does not prove first-attempt success without continuation, uniqueness across the address book, every WeCom function or complete desktop-tool injection. Skill discovery/installation does not authorize local tools; preserve genuine native denials rather than fabricating receipts. See [local verification](NATIVE-VALIDATION.en.md).
