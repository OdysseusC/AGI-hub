# Install AGI Hub

[English](INSTALL.md) | [简体中文](INSTALL.zh-CN.md)

Download the portable ZIP for your platform, extract it fully, and hand the directory and [START-HERE.md](START-HERE.md) to Codex. The [assistant installation guide](docs/INSTALL-FOR-ASSISTANT.md) covers initial setup, the WorkBuddy onboarding message and native acceptance tests.

| System | CPU | Package suffix |
| --- | --- | --- |
| macOS | Apple silicon (M series) | `macos-arm64.zip` |
| macOS | Intel | `macos-amd64.zip` |
| Windows | Intel/AMD 64-bit | `windows-amd64.zip` |
| Windows | ARM64 | `windows-arm64.zip` |

On a Mac, open Apple menu → About This Mac. Choose `macos-arm64` when it shows an Apple M-series chip, or `macos-amd64` when it shows an Intel processor. `amd64` is also called x64/x86-64 and applies to Intel Macs. `darwin` is Go's internal name for macOS and may still appear in program versions and manifests. Packages are currently provided only for Windows and macOS.

The installer and Hub depend only on the operating system. Check the selected assistant's official CLI, runtime, login and native tool permissions separately. A successful platform build does not prove its native assistant integration has been tested.

Before upgrading, confirm native execution has finished, stop the old Hub and Clients, and preserve all message records before replacing programs. Versions `0.2.0 / 0.2.1` and `0.2.2` use different process locks and must not run together. Do not change ports to bypass an existing instance. If the API port is occupied, the CLI refuses to start before loading the journal. Version `0.2.2` and later protect a data directory with an operating-system file lock. The program retains the lock file; do not delete it manually.

Official packages are compiled, checked and published by GitHub's **Release** workflow. Each release page lists that version's changes, package selection and acceptance scope. Download the matching ZIP and verify it against `SHA256SUMS` on the same page. Pull request and `main` checks also provide temporary `portable-packages` build artifacts, retained for 14 days, for early verification.

For a local build, run `go run ./tools/package` at the repository root to produce four Windows/macOS ZIPs and `dist/SHA256SUMS`. Packages contain no user configuration, credentials or runtime records. See [CONTRIBUTING.md](CONTRIBUTING.md) for release operations and the assistant installation guide for building from source.
