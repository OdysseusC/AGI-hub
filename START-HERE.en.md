# Hand this package to Codex

[简体中文](START-HERE.md) | [English](START-HERE.en.md)

AGI Hub lets you start collaboration from an existing AI assistant chat and return the result to the chat where you first asked. The Hub stores and delivers messages; each assistant's skill decides how to divide the work.

Download the ZIP that matches your computer. On a Mac, choose `macos-arm64` for Apple silicon (M series), or `macos-amd64` for Intel. On Windows, choose `windows-amd64` for Intel/AMD, or `windows-arm64` for ARM. Extract it fully, drag the **extracted directory** into a Codex that can operate local files, and send:

> Read START-HERE.en.md and docs/INSTALL-FOR-ASSISTANT.en.md in this directory and help me install AGI Hub. Check the system and architecture first. Detect any existing installation and preserve its configuration. Use the current original Codex chat as this collaboration's entry, and connect WorkBuddy first. You may run the local installer. Give me exact steps whenever I need to enter a message in WorkBuddy, log in or confirm native permissions. First run a reversible collaboration using local files, then give me the actual acceptance results in this chat. Do not call Claude, send WeCom messages or read unrelated chat histories.

The installer is precompiled: **installing the Hub requires no Go, npm or pip**. Connecting Codex Desktop still requires an official desktop bridge provided by the current host. The WorkBuddy CLI Client requires existing Python 3.11+, the official CodeBuddy CLI and your own login. The installer does not download runtimes, log in or approve permissions automatically.

## Package contents

- `agi-hub` / `agi-hub.exe`: the local message sidecar.
- `agi-hub-setup` / `agi-hub-setup.exe`: the offline base installer.
- `agi-hub-endpoint` / `agi-hub-endpoint.exe`: messaging and reporting for one configured assistant, without administrative capabilities; use it after the installer creates its private fixed binding.
- `install.command` or `install.ps1`: the launcher for your platform.
- `clients/`: separate product Clients; Codex and WorkBuddy connect to their own native capabilities.
- `docs/INSTALL-FOR-ASSISTANT.en.md`: checks and acceptance steps for the installation assistant.
- `package-manifest.json`: public file list and SHA-256 hashes to check extracted file integrity; confirm the release's origin separately through the download channel and Release checksums.

Personal configuration, tokens and runtime records are stored under `agi-hub` in your operating system's user configuration directory, never back in the ZIP. Versioned programs are copied into `runtimes/portable-*` there. You can move or delete the original extracted directory afterward.

## Run the initial checks yourself

In the extracted directory on macOS:

```sh
./agi-hub-setup --verify-only
./agi-hub-setup --host codex
```

In Windows PowerShell:

```powershell
.\agi-hub-setup.exe --verify-only
.\agi-hub-setup.exe --host codex
```

Without arguments, the installer only displays instructions. Handle macOS download quarantine or Windows reputation prompts yourself after verifying the source. The installer has no signing, notarization or administrator-elevation step. Do not ask an assistant to disable system protections.

Base installation creates private configuration, copies programs and installs the skill only for the explicitly selected host. **Base installation does not mean the assistant is connected**: verify the actual session ID, CLI configuration directory, receive mechanism and artifact return separately. The output includes a credential-free WorkBuddy onboarding message that you can copy into the selected original WorkBuddy chat.

After the installation assistant exports a WorkBuddy-specific Client configuration and confirms the authorized workspace, run the installer with `--client-config <scoped-config-file> --workspace <authorized-workspace>`. This provisions a helper restricted to that assistant's `mail-send / mail-state / peers`, with `report` only for legacy tasks. Do not grant broad permission to the full Hub administrative program, a shell or a Python interpreter for automatic collaboration.

If the installer reports an edited skill, a damaged installation or a version conflict, preserve the files and ask the installation assistant to explain the differences. Do not delete records to force a reinstall. Run only one Client per endpoint at a time.
