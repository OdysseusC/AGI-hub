# Agent collaboration language

[简体中文](2026-10-20-agent-language-design.md) | [English](2026-10-20-agent-language-design.en.md)

## Requirements

Each local agent has a collaboration-language setting on the management page. Claude defaults to English; other agents default to automatic selection. Chinese and other languages remain selectable, with no permanent Chinese prohibition. Human-facing replies follow the user's explicitly requested language, or their current conversation language when unspecified. Internal collaboration settings cannot override this.

The sending agent translates its authored prompt, necessary context and replies before submitting them to Hub. Hub invokes no translator, does not guess prose language, and neither resends nor creates messages automatically. Attachments, code, paths, identifiers, source material and required verbatim quotations stay unchanged. Do not duplicate the complete original and translated context.

## Data and resolution

Endpoints gain `communication_language`. An empty value or `auto` selects the default: `en` for `claude-code`/`claude`, otherwise `auto`. Accept structured language tags with optional script and region, such as `en`, `zh-Hans`, `nb` and `pt-BR`, rather than arbitrary instruction text.

Resolve a send in this order: recipient Claude's preference, sender Claude's preference, the recipient's explicit preference, then the sender's preference. If both are `auto`, the agent chooses according to the task. Between differently configured Claude endpoints the recipient wins. The directory returns each member's preference and caller-relative `send_language`. Signed remote directories include driver and language, avoiding product inference from endpoint names.

Messages retain a small `communication_language` hint without rewriting content. Changes affect subsequent submissions, while history and claimed messages remain unchanged. An identical ID retry returns its original record. Fixed native protocol framing and field names do not undergo model translation. Language is an expression rule, not authority or evidence of truth.

## Dynamic skill synchronization

Upgrade the static English skill template once and reference an adjacent managed `communication-policy.md`. This policy lists only the current endpoint and visible local peers. Remote preferences come from the latest scoped directory on demand. Saving in UI synchronizes registered, unmodified managed installations without discovering directories, restoring uninstalled skills or overwriting user edits. Include the policy in the ownership manifest and mark policy-only changes as outdated.

Keep the static source version separate from personal policy contents so changing languages does not trip source downgrade protection. A successful setting save with a failed installation sync reports partial failure and affected locations. A disk update does not prove a running native session has reread the rules. Do not wake models to refresh skills.

## UI and validation

Agent cards provide common languages, a custom tag option and a save button. Chinese is selectable; no fixed human-output language is imposed. Explain source-material exemptions and the need for running sessions to reread rules. SSE refreshes preserve unsaved selections.

Offline tests cover English defaults, explicit Chinese, precedence, invalid settings, persistence/restart, directory version changes, LAN propagation, stable retries, policy synchronization and edit protection, notification hints, and UI requests and input preservation. Tests invoke no Claude models and do not claim verified model compliance.
