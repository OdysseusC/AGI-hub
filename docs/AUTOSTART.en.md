# Optional user-login startup

[简体中文](AUTOSTART.md) | [English](AUTOSTART.en.md)

From **0.5.0**, you can restore the Hub and registered standalone Clients when the current user next logs in to Windows or macOS. Fresh installation and upgrades leave this off by default. Installing files, registering a Client or opening management does not start a model. The Hub remains a message transport; the login runner manages only explicitly selected local processes.

## Platforms and timing

| Platform | Mechanism in this version | Effect |
| --- | --- | --- |
| macOS | A current-user LaunchAgent in `~/Library/LaunchAgents/` | Starts the runner at the next user login; registration does not load it immediately and sets no unlimited `KeepAlive` |
| Windows | A Task Scheduler logon task for the current user | Starts the runner at that user's next login; registration does not run the task immediately or configure unlimited restarts |
| Linux | Login startup unsupported | Source CI and offline checks only; no Linux release package |

User agents run within a logged-in macOS user's lifecycle, and Windows logon triggers target user logins. See [Apple Launch Daemons and Agents](https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html) and [Microsoft LogonTrigger](https://learn.microsoft.com/en-us/windows/win32/taskschd/logontrigger). This is user-login startup, not a system service running before sign-in.

`enable`, `disable` and saving in management change future login behavior only. They do not start, stop or reload the current Hub, Clients, native assistants or tasks. Disabling startup does not stop an existing runner; it continues its current lifecycle. Changing the Client selection also does not update a running instance.

## Register, then select

Use the actual installed Hub executable in a stable location and your existing private data directory. Replace every example path with the target computer's paths; do not use a temporary download directory, public repository or someone else's paths. Windows requires an actual `.exe`; use `&` in PowerShell when invoking a quoted executable path.

Inspect the saved plan and platform registration first:

```sh
agi-hub autostart status --data /absolute/private/agi-hub
```

Restore only the Hub, with no Clients selected:

```sh
agi-hub autostart enable --data /absolute/private/agi-hub --binary /absolute/stable/agi-hub --clients ''
```

For WorkBuddy, first verify that the endpoint is enabled and bound and that its existing Client configuration, official sign-in, session and native permissions work. Save a separate launch description, for example `/absolute/private/workbuddy/launch.json`:

```json
{
  "endpoint": "workbuddy",
  "enabled": false,
  "command": "/absolute/path/to/python3",
  "args": [
    "/absolute/stable/agi-hub/clients/workbuddy/main.py",
    "--config",
    "/absolute/private/workbuddy/client.json"
  ],
  "cwd": "/absolute/stable/agi-hub",
  "requires_host": false
}
```

`command` is an absolute executable path, `args` contains separate arguments, and `cwd` is an absolute working directory. No shell is used. This launches the Python mailbox consumer `main.py`, rather than invoking `codebuddy --resume` directly. The native CLI's working directory, configuration directory and session still come from its existing `client.json`. Keep the matching `clients/shared` next to the installed Client. On Windows, replace Python and every path with actual local absolute paths.

Register it, then explicitly select it:

```sh
agi-hub autostart register-client --data /absolute/private/agi-hub --endpoint workbuddy --file /absolute/private/workbuddy/launch.json
agi-hub autostart enable --data /absolute/private/agi-hub --binary /absolute/stable/agi-hub --clients workbuddy
```

A newly registered Client is always unselected; `enabled` in its description does not enable it. Registering an existing endpoint again preserves its prior selection. `enable` without `--clients` preserves existing choices. `--clients workbuddy,grok` replaces the selection with those registered endpoints; `--clients ''` explicitly clears it and restores only the Hub. Use your actual endpoint names. Unregistered Clients cannot be selected.

Management provides an optional next-login startup switch and checkboxes for registered Clients. The web interface selects existing registrations only; it cannot upload executable paths, arguments or private configuration files. Initial registration and launch-path changes require the local CLI on the target computer. A separately authorized [remote management page](LAN.en.md#open-another-computers-management-page) can also change these existing selections, still taking effect only at the target user's next login.

Disable future login startup or remove a registration:

```sh
agi-hub autostart disable --data /absolute/private/agi-hub
agi-hub autostart remove-client --data /absolute/private/agi-hub --endpoint workbuddy
```

These commands do not stop current processes. `autostart run` is the execution entry invoked by the login registration. Running it manually starts processes according to an enabled plan; it is not a read-only inspection command.

## Recovery limits by Client

| Client | Behavior in 0.5.0 |
| --- | --- |
| WorkBuddy | Can restore its registered standalone Python consumer. Each message still resumes the fixed official CLI session through the existing Client; sign-in, native permissions and business authorization are not expanded |
| Grok Routine | Can restore its standalone Python consumer through `clients/grok-routine/main.py --config <private-config>`. Client `0.2.0` implements queue mailbox protocol 1; the actual `0.1` Client must be upgraded. Requires the user's configured official Routine webhook, fixed-identity helper and workspace collaboration rules. Native acceptance remains pending; startup does not satisfy these prerequisites |
| Codex | Registration forces `requires_host=true`. It reports `needs_host_environment`; the runner does not launch it directly |
| Claude Code | Likewise records only a native-host requirement. Start the interactive Code process and its mod through the existing workflow; the runner executes neither `claude` nor `-p --resume` |

Minimal host-only descriptions are `{"endpoint":"codex","requires_host":true}` and `{"endpoint":"claude","requires_host":true}`, registered with the same `register-client` command. Do not provide launch commands or environment values for them. Codex requires naturally supplied official bridge environment, a valid signed process chain and the real caller identity. An initial binding cannot persist these conditions into permission for arbitrary future background processes. Claude retains its selected interactive host, plugin, fixed-session and user-required network prerequisites.

The launch description and credential-bearing `client.json` are separate configurations. A launch plan stores only the latter's path, not endpoint tokens, sign-in credentials, native IPC, pipe values or model keys. Optional `environment` accepts only the code's explicit OS, locale/timezone and proxy allowlist; proxy URLs cannot contain credentials. The runner does not capture the current assistant's proxy, host or key environment. Individual Clients can filter the environment further. Setting proxy variables does not verify a required proxy chain or egress address; hosts with such gates must retain those checks without falling back to direct connections. See the [Client documentation](CLIENTS.en.md).

## Readiness, retries and diagnosis

The runner first authenticates the local Hub using its existing private configuration. If a Hub is already ready, it attaches without taking ownership. If that externally owned Hub later disappears, the runner does not restart it. It starts its own Hub only when the local listener is confirmed unavailable; failed authentication or an unverifiable identity produces `needs_user`. Selected standalone Clients start only after Hub readiness. Disabled or unbound endpoints are not started.

Within one runner lifecycle, its owned Hub and each standalone Client that fails to start or exits abnormally receive at most **one initial start plus five retries**, delayed **1, 2, 4, 8 and 16 seconds**. Exhaustion preserves a failure state without an unlimited OS restart policy. A normally exiting Client is not restarted. These bounded retries restore processes only: they do not redeliver `dispatching/uncertain` messages, change native session state or replace each Client's authentication and delivery checks.

`autostart status` reports the future-login registration, saved selections and the latest runtime snapshot together. Snapshot data and PIDs can be stale. `running` means only that a process started; `hub_ready` means only that the local Hub authenticated and became ready. Neither proves a connected native assistant or completed business work. `needs_host_environment` requires startup from the native host; `needs_user` indicates prerequisites or authentication requiring attention.

The private data directory gains `startup.json`, `startup-state.json` and `logs/autostart/`. Runner logs record fixed states and attempt counts, omitting command arguments, environment values and child-process output. Stopping a runner cleans up only children created by that invocation; an attached Hub stays under its original owner's control.

## Verification status

This version passed offline tests for platform definitions, plan validation, process substitutes, bounded retries, ownership, management APIs and the web interface. They did not invoke real assistants. **Physical-computer reboot, user-login triggering, native-session restoration across a new lifecycle and complete target-environment round trips remain unaccepted.** Existing manually started native acceptance does not cover these checks. Registration, an installed platform definition or `running` must not be presented as verified unattended recovery.

```text
go test ./internal/autostart ./internal/app ./cmd/agi-hub
node --test internal/app/web/dashboard.test.cjs
```
