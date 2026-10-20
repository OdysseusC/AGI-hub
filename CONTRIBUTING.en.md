# Contributing

[简体中文](CONTRIBUTING.md) | [English](CONTRIBUTING.en.md)

Keep AGI Hub small: assistants decide how to collaborate through their skills; the Hub handles addressed messages, delivery records, limits and deduplication. A new adapter must describe its actual native session, login and wake capabilities. Model API access alone is not a native assistant integration.

## Changes

Public `*.md` documents default to Chinese; complete English versions use `*.en.md`. Put Chinese first in language navigation. The installed model skill still contains only one English instruction body to avoid duplicate context.

Create a focused branch such as `feat/...`, `fix/...` or `docs/...` and open a pull request to `main`. Do not push implementation work directly to `main`. The required GitHub Actions status check is **`checks`**, which aggregates the platform tests and portable builds. Branch protection is configured in GitHub repository settings, separately from this file.

Explain the concrete behavior, how it was verified, and any native platform limitations in the pull request. Keep personal configuration, credentials, real conversation histories and private artifacts outside the repository. Never add model or account calls to automated tests.

Run the checks relevant to your change; the full offline suite is:

```sh
go test -race ./...
go vet ./...
python3 -m unittest discover -s clients/tests -v
python3 -m unittest discover -s tools -p 'test_*.py' -v
python3 tools/check_docs.py
node --test clients/codex/test_desktop_mcp_client.mjs
node --test clients/claude-code/tests/client.test.mjs
go run ./tools/package
go build -o dist/agi-hub-smoke ./cmd/agi-hub
python3 tools/smoke_client.py --hub "$PWD/dist/agi-hub-smoke"
python3 tools/smoke_skill_sync.py --hub "$PWD/dist/agi-hub-smoke"
python3 tools/smoke_mailbox.py --hub "$PWD/dist/agi-hub-smoke"
python3 tools/smoke_grok_client.py --hub "$PWD/dist/agi-hub-smoke"
```

The smoke binary is built for the development machine, separately from the four Windows/macOS release packages. On Windows, name it `dist/agi-hub-smoke.exe` and pass that path to the scripts. These tests use temporary private configurations and synthetic native adapters. Passing them does not prove a native application's login, idle wake or busy guidance works; those require a separate, authorized local acceptance test.

## Releases

Compilation and packaging run on GitHub Actions. Each pull request and push to `main` uploads a `portable-packages` artifact after the offline checks; feature pushes do not duplicate the pull request run. Local packaging remains available for development.

For a release, merge the version and changelog update through a pull request, then push a strict SemVer tag such as `v0.3.0` pointing to that reviewed `main` commit. The **Release** workflow checks main ancestry, runs the full reusable checks at the exact tag commit, downloads that run's immutable artifact, and verifies its commit, four checksums and packaged binary version on macOS. It uploads four Windows/macOS ZIPs and `SHA256SUMS` into a draft before publishing. Only the publication job receives repository write permission. Linux remains an internal CI host, with no Linux release packages.

Release notes show full Chinese changes first, with English as an optional section, from the exact version sections in Chinese `CHANGELOG.md` and English `CHANGELOG.en.md`, followed by chip-specific download tables and pinned installation/acceptance links in each language. A missing, empty or duplicate version section in either file blocks publication. `python3 tools/check_docs.py` checks language pairs, navigation and local documentation links. Preview locally with `python3 tools/release_notes.py --tag v0.3.1 --repo OdysseusC/AGI-hub --output dist/release-notes.md`; do not replace the version's changes with a generic build message. macOS ZIP names use `macos-{amd64,arm64}` while the internal runtime and manifest use Go's `darwin` platform name.

The workflow can also be run manually with an **existing** `vX.Y.Z` tag; its source is the tag commit, regardless of the selected dispatch branch. An existing release or draft is never overwritten. If upload or publication fails, inspect the preserved draft and run before choosing recovery; do not use `--clobber` or silently replace published assets. Document protocol or configuration incompatibilities explicitly; preserve unresolved delivery records during upgrades.

Hub, Client, skill and journal versions describe different compatibility boundaries. Increase the managed skill revision when its released instructions change. Avoid coupling all adapters to an identical version number or claiming an untested platform is supported merely because it builds.
