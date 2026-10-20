# Repository work

[English](AGENTS.md) | [简体中文](AGENTS.zh-CN.md)

- Keep `main` releasable. Develop on a separate feature/fix/docs branch, run the relevant offline checks, and merge through a pull request. Do not commit or push directly to `main` or bypass its protections.
- Keep the Hub an addressed message pipe. Assistants and their skills choose recipients and coordinate work; do not introduce a task tree, central planner, forced return chain or automatic business replies for mailbox messages.
- Keep assistant Clients in their own directories and preserve native session/tool boundaries. Capabilities are verified per transport, product and platform.
- Never commit personal configuration, credentials, actual chat history or private artifacts. Package only the explicit public allowlist.
- Prefer offline tests. Real assistant calls must be necessary and authorized; minimize tokens, especially Claude. Record native acceptance limits truthfully.
- Release with a version tag, CHANGELOG and matching portable packages/checksums. See [CONTRIBUTING.md](CONTRIBUTING.md).
