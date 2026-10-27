# Repository work

[简体中文](AGENTS.md) | [English](AGENTS.en.md)

- Default public documentation to Chinese (`*.md`), with complete optional English (`*.en.md`); put Chinese first in navigation and Releases. Install only one English model skill body.
- Keep `main` releasable. Develop on a separate feature/fix/docs branch, run the relevant offline checks, and merge through a pull request. Do not commit or push directly to `main` or bypass its protections.
- Keep the Hub an addressed message pipe. Assistants and their skills choose recipients and coordinate work; do not introduce a task tree, central planner, forced return chain or automatic business replies for mailbox messages.
- Route routine assistant collaboration through the configured AGI Hub mailbox. Use native UI only for initial setup, recovery while the Hub is offline, and explicitly requested UI acceptance. Once the Hub is online, complete missing member/Client setup rather than continuing work through chat windows. A UI conversation is not evidence of Hub integration.
- Keep assistant Clients in their own directories and preserve native session/tool boundaries. Capabilities are verified per transport, product and platform.
- Never commit personal configuration, credentials, actual chat history or private artifacts. Package only the explicit public allowlist.
- Prefer offline tests. Real assistant calls must be necessary and authorized; minimize tokens, especially Claude. Record native acceptance limits truthfully.
- Release with a version tag, CHANGELOG and matching portable packages/checksums. See [CONTRIBUTING.md](CONTRIBUTING.en.md).
