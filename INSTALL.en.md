# Install AGI Hub

[简体中文](INSTALL.md) | [English](INSTALL.en.md)

Download the portable ZIP for your platform, extract it fully, and hand the directory and [START-HERE.en.md](START-HERE.en.md) to Codex. The [assistant installation guide](docs/INSTALL-FOR-ASSISTANT.en.md) covers initial setup, the WorkBuddy onboarding message and native acceptance tests.

| System | CPU | Package suffix |
| --- | --- | --- |
| macOS | Apple silicon (M series) | `macos-arm64.zip` |
| macOS | Intel | `macos-amd64.zip` |
| Windows | Intel/AMD 64-bit | `windows-amd64.zip` |
| Windows | ARM64 | `windows-arm64.zip` |

On a Mac, open Apple menu → About This Mac. Choose `macos-arm64` when it shows an Apple M-series chip, or `macos-amd64` when it shows an Intel processor. `amd64` is also called x64/x86-64 and applies to Intel Macs. `darwin` is Go's internal name for macOS and may still appear in program versions and manifests. Packages are currently provided only for Windows and macOS.

The installer and Hub depend only on the operating system. Check the selected assistant's official CLI, runtime, login and native tool permissions separately. A successful platform build does not prove its native assistant integration has been tested.

## Grok workspace installation

This entry installs only collaboration rules and a fixed-identity sending helper that Grok can read through an authorized terminal on the target computer. For example, prepare these files on the user's explicitly selected Windows computer C. It does not upload or automatically load skills into a cloud Grok Bot/Routine, create a native session, or prove native reception, execution or replies.

First prepare an existing Hub endpoint with driver `grok-routine` and its own private `client.json` on the target computer. Verify the actual Routine configuration using the [Grok Client guide](clients/grok-routine/README.en.md). Export a dedicated configuration with `agi-hub client-config --data <existing-private-Hub-directory> --endpoint <Grok-endpoint-ID> --file <absolute-client.json-outside-the-repository>`; existing files are never overwritten. Keep configuration and credentials on that computer, never in chat.

Choose an existing authorized workspace with no symlinks, and explicitly select a `skills` subdirectory within it. Keep private Hub data and credential-bearing Client configuration separate from working material. Verify the fully extracted package for the target platform, then run on that computer. Replace every placeholder with an actual absolute path and use the user's selected Grok endpoint ID:

```text
agi-hub-setup --host grok --endpoint <Grok-endpoint-ID> --package-dir <verified-extracted-package> --data <existing-private-Hub-directory> --client-config <absolute-endpoint-client.json> --workspace <absolute-authorized-workspace> --skills-dir <absolute-skills-subdirectory-within-that-workspace>
```

On Windows, use the actual `agi-hub-setup.exe`. In PowerShell, invoke quoted executable paths with `&` and quote each path separately. `--client-config`, `--workspace` and `--skills-dir` are all required explicitly. The skills directory must be a workspace subdirectory; escaping paths, parent traversal and links are rejected. The installer may create a missing skills subdirectory. This entry accepts no `--report-binary` substitute, assumes no `~/.grok` location and never borrows a WorkBuddy identity.

The installer verifies that the Client configuration belongs to this Hub and endpoint, then reuses the existing fixed-identity helper:

- The helper holds only the Grok endpoint's credential and fixes the numeric loopback address and permitted message-file workspace. It cannot switch identity, administer the Hub or invoke an assistant directly.
- Workspace rules include `SKILL.md`, credential-free `installation.json` and the managed language policy. Private `binding.json` stays in the dedicated helper directory; Grok does not need to read its contents.
- The installation joins the existing registry for managed rule and language synchronization. Private edits and unowned files prevent overwrites, and synchronization does not recreate an intentionally removed skill.

The resulting workspace-files message proves file installation only. The installer starts no Hub service, Client or native assistant, enables neither endpoints nor startup, and prints no credentials. Grok must actually have the user's authorized C-terminal capability to read the supplied skill path, language policy and `installation.json`, then execute its fixed helper. Sending to a Routine webhook does not grant access to C's files or programs.

Normal helper commands are `peers`, `mail-send --file <saved-workspace-JSON>` and `mail-state --correlation-id <known-correlation-ID>`. Sender identity is fixed; Grok explicitly chooses the recipient in JSON, subject to Hub permissions. Normal collaboration sends through the Hub; manual browser forwarding is not a verified round trip. Validate the standalone consumer, native capabilities and actual replies separately. See [Clients and support boundaries](docs/CLIENTS.en.md).

## Upgrades and packages

Before upgrading, confirm native execution has finished, stop the old Hub and Clients, and preserve all message records before replacing programs. Versions `0.2.0 / 0.2.1` and `0.2.2` use different process locks and must not run together. Do not change ports to bypass an existing instance. If the API port is occupied, the CLI refuses to start before loading the journal. Version `0.2.2` and later protect a data directory with an operating-system file lock. The program retains the lock file; do not delete it manually.

Official packages are compiled, checked and published by GitHub's **Release** workflow. Each release page lists that version's changes, package selection and acceptance scope. Download the matching ZIP and verify it against `SHA256SUMS` on the same page. Pull request and `main` checks also provide temporary `portable-packages` build artifacts, retained for 14 days, for early verification.

For a local build, run `go run ./tools/package` at the repository root to produce four Windows/macOS ZIPs and `dist/SHA256SUMS`. Packages contain no user configuration, credentials or runtime records. See [CONTRIBUTING.md](CONTRIBUTING.en.md) for release operations and the assistant installation guide for building from source.
