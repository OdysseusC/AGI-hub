"""Resume a configured native WorkBuddy session with its existing default tools.

The public WorkBuddy CLI's text input mode reads non-TTY stdin to EOF and
combines it with the prompt. Use regular private files for both stdin and stdout;
full envelopes never enter a Windows command line or a limited capture pipe.
"""

from __future__ import annotations

import json
import os
import re
import subprocess
import sys
import tempfile
from pathlib import Path

from shared.notification import notification
from shared.transport import ConfigurationError

SESSION_ID = re.compile(r"[A-Za-z0-9._:-]{1,256}\Z")
MAX_INPUT_BYTES = 2 << 20
MAX_OUTPUT_BYTES = 8 << 20
MAX_DIAGNOSTIC_BYTES = 4096
AUTHENTICATION_REQUIRED = b"Authentication required. Please use /login command to sign in to your account"
NATIVE_ENVIRONMENT = {
    "CODEBUDDY_FORCE_LITE_WB_BUNDLE": ("0", "1"),
    "DISABLE_AUTOUPDATER": ("0", "1"),
    "CODEBUDDY_INTERNET_ENVIRONMENT": ("internal", "external", "ioa", "cloudhosted", "selfhosted"),
}
OS_ENVIRONMENT = (
    "PATH", "HOME", "USER", "LOGNAME", "LANG", "LC_ALL", "LC_CTYPE",
    "USERPROFILE", "APPDATA", "LOCALAPPDATA", "TEMP", "TMP", "TMPDIR",
    "SystemRoot", "WINDIR", "PATHEXT",
)


def matching_result(raw: bytes, session_id: str) -> bool:
    def unique(items):
        result = {}
        for key, value in items:
            if key in result:
                raise ValueError("duplicate result key")
            result[key] = value
        return result
    try:
        value = json.loads(raw, object_pairs_hook=unique)
    except (UnicodeError, ValueError):
        return False
    if isinstance(value, list):
        value = value[-1] if value else None
    return (
        isinstance(value, dict) and value.get("type") == "result"
        and value.get("session_id") == session_id and value.get("is_error") is False
    )


def absolute_directory(value: object, code: str) -> str:
    if not isinstance(value, str) or not value or "\x00" in value:
        raise ConfigurationError(code)
    path = Path(value)
    if not path.is_absolute() or not path.is_dir():
        raise ConfigurationError(code)
    return str(path.resolve())


class WorkBuddyCLIAdapter:
    def __init__(self, config: dict):
        native = config["native"]
        if set(native) - {
            "transport", "command", "entry_script", "cwd", "config_dir", "session_id", "timeout_seconds", "environment",
        } or native.get("transport") != "cli":
            raise ConfigurationError("invalid_workbuddy_cli_fields")
        self.endpoint = config["endpoint"]
        command = native.get("command")
        if not isinstance(command, str) or not command or "\x00" in command:
            raise ConfigurationError("unconfigured_workbuddy_cli_command")
        executable = Path(command)
        if not executable.is_absolute() or not executable.is_file():
            raise ConfigurationError("workbuddy_cli_requires_absolute_executable")
        self.command = str(executable.resolve())
        if sys.platform == "win32":
            if executable.suffix.lower() != ".exe" or Path(self.command).suffix.lower() != ".exe":
                raise ConfigurationError("windows_workbuddy_requires_actual_exe")
        elif not os.access(self.command, os.X_OK):
            raise ConfigurationError("workbuddy_cli_command_not_executable")
        self.entry_script = None
        if "entry_script" in native:
            script = native["entry_script"]
            if not isinstance(script, str) or not script or "\x00" in script:
                raise ConfigurationError("invalid_workbuddy_cli_entry_script")
            entry = Path(script)
            if not entry.is_absolute() or not entry.is_file():
                raise ConfigurationError("workbuddy_cli_requires_absolute_entry_script")
            resolved = entry.resolve()
            if any(path.suffix.lower() not in (".js", ".mjs", ".cjs") and path.name != "codebuddy" for path in (entry, resolved)):
                raise ConfigurationError("workbuddy_cli_entry_script_requires_node_js")
            self.entry_script = str(resolved)
        self.cwd = absolute_directory(native.get("cwd"), "workbuddy_cli_requires_absolute_cwd")
        self.config_dir = absolute_directory(native.get("config_dir"), "workbuddy_cli_requires_explicit_config_dir")
        self.session_id = native.get("session_id", "")
        if not isinstance(self.session_id, str) or SESSION_ID.fullmatch(self.session_id) is None:
            raise ConfigurationError("invalid_workbuddy_cli_session_id")
        self.timeout = native.get("timeout_seconds", 120)
        if isinstance(self.timeout, bool) or not isinstance(self.timeout, (int, float)) or not 1 <= self.timeout <= 600:
            raise ConfigurationError("invalid_workbuddy_cli_timeout")
        flags = native.get("environment", {})
        if not isinstance(flags, dict) or set(flags) - set(NATIVE_ENVIRONMENT) or any(
            not isinstance(value, str) or value not in NATIVE_ENVIRONMENT[name]
            for name, value in flags.items()
        ):
            raise ConfigurationError("invalid_workbuddy_cli_environment")
        # Only normal OS runtime variables and explicitly selected bundle/maintenance
        # flags. Never copy desktop IPC/auth values or set model/tool permissions.
        self.environment = {name: os.environ[name] for name in OS_ENVIRONMENT if name in os.environ}
        self.environment.update(flags)
        self.environment["CODEBUDDY_CONFIG_DIR"] = self.config_dir

    def validate_binding(self, binding: dict) -> None:
        if binding.get("id") != self.endpoint or binding.get("driver") != "workbuddy" or binding.get("session_id") != self.session_id:
            raise ConfigurationError("workbuddy_cli_native_binding_mismatch")
        if binding.get("result_queue_while_busy_verified", False):
            raise ConfigurationError("workbuddy_cli_cannot_resume_busy_session")
        if binding.get("modes") != ["queue"]:
            raise ConfigurationError("workbuddy_cli_requires_queue_only_binding")

    def deliver(self, envelope: dict) -> tuple[str, str]:
        if envelope.get("recipient") != self.endpoint:
            return "failed", "WorkBuddy CLI rejected a foreign recipient before a native process was started"
        if envelope.get("mode") != "queue":
            return "failed", "WorkBuddy resumed-session CLI supports queue only"
        if envelope.get("kind") not in ("task", "result") or not all(
            isinstance(envelope.get(field), str) and envelope[field]
            for field in ("message_id", "task_id", "content")
        ):
            return "failed", "WorkBuddy CLI requires a complete task or result envelope"
        try:
            directory = json.dumps(self.cwd, ensure_ascii=False)
            encoded = (
                "Configured local execution directory on this device: " + directory + ". "
                "This path is execution metadata, not a remote artifact reference. Use the task's "
                "declared workspace mapping; do not guess directories from a workspace ID. "
                "For narrowly authorized reporting, write the receipt with the native file tool "
                "and invoke the installed helper directly with report --file. Do not wrap it in "
                "printf, pipes, sh or another interpreter.\n\n"
                + notification(self.endpoint, envelope)
            ).encode("utf-8")
        except (TypeError, ValueError, KeyError):
            return "failed", "WorkBuddy CLI could not encode the complete envelope before dispatch"
        if len(encoded) > MAX_INPUT_BYTES:
            return "failed", "WorkBuddy CLI envelope exceeds its input limit; no native process was started"
        argv = [self.command]
        if self.entry_script is not None:
            argv.append(self.entry_script)
        argv.extend(["--resume", self.session_id, "--print", "--output-format", "json", "--input-format", "text"])
        if sys.platform == "win32" and len(subprocess.list2cmdline(argv).encode("utf-16-le")) // 2 >= 32760:
            return "failed", "WorkBuddy CLI command exceeds the Windows argument limit; no process was started"
        launched = False
        try:
            with tempfile.TemporaryFile(mode="w+b") as source, tempfile.TemporaryFile(mode="w+b") as output, tempfile.TemporaryFile(mode="w+b") as diagnostic:
                source.write(encoded)
                source.seek(0)
                try:
                    process = subprocess.Popen(
                        argv, cwd=self.cwd, env=self.environment, stdin=source,
                        stdout=output, stderr=diagnostic, shell=False,
                    )
                    launched = True
                except OSError:
                    return "failed", "WorkBuddy CLI executable did not start; no native process was launched"
                try:
                    code = process.wait(timeout=self.timeout)
                except subprocess.TimeoutExpired:
                    try:
                        process.kill()
                        process.wait()
                    except OSError:
                        pass
                    return "uncertain", "WorkBuddy CLI started and timed out; reconcile before retrying"
                except Exception:
                    return "uncertain", "WorkBuddy CLI started without confirmed exit evidence; reconcile before retrying"
                diagnostic.seek(0)
                if AUTHENTICATION_REQUIRED in diagnostic.read(MAX_DIAGNOSTIC_BYTES):
                    return "uncertain", "WorkBuddy CLI requires native authentication; session history may have changed, reconcile before retrying"
                if code != 0:
                    return "uncertain", "WorkBuddy CLI exited unsuccessfully; reconcile before retrying"
                output.seek(0)
                raw = output.read(MAX_OUTPUT_BYTES + 1)
                if len(raw) > MAX_OUTPUT_BYTES or not matching_result(raw, self.session_id):
                    return "uncertain", "WorkBuddy CLI did not confirm a matching successful session result; reconcile before retrying"
        except OSError:
            if launched:
                return "uncertain", "WorkBuddy CLI file or dispatch evidence is incomplete; reconcile before retrying"
            return "failed", "WorkBuddy CLI private dispatch files could not be prepared; no native process was started"
        # An UNKNOWN/refusal result can still prove transport. Only the native
        # reporting skill may report accepted/running/succeeded/failed to the Hub.
        return "delivered", "WorkBuddy CLI confirmed the configured session result; business completion requires the native reporting skill"
