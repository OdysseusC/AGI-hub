"""Existing Codex desktop thread delivery through the verified queue CLI shape."""

from __future__ import annotations

import os
import shutil
import subprocess
import sys

from shared.notification import notification
from shared.transport import ConfigurationError


class CodexAdapter:
    def __init__(self, config: dict):
        native = config["native"]
        if set(native) - {"transport", "thread_id", "executable", "timeout_seconds"} or native.get("transport", "queue") != "queue":
            raise ConfigurationError("invalid_codex_native_fields")
        self.endpoint = config["endpoint"]
        self.thread = native.get("thread_id", "")
        self.executable = native.get("executable", "codex")
        self.timeout = native.get("timeout_seconds", 30)
        if not isinstance(self.thread, str) or not self.thread.strip():
            raise ConfigurationError("unconfigured_codex_thread_id")
        if not isinstance(self.executable, str):
            raise ConfigurationError("unconfigured_codex_executable")
        resolved = shutil.which(self.executable)
        if not resolved:
            raise ConfigurationError("unconfigured_codex_executable")
        self.executable = os.path.realpath(resolved)
        if sys.platform == "win32" and os.path.splitext(self.executable)[1].lower() in (".cmd", ".bat"):
            raise ConfigurationError("windows_codex_requires_actual_exe")
        if isinstance(self.timeout, bool) or not isinstance(self.timeout, (int, float)) or not 1 <= self.timeout <= 300:
            raise ConfigurationError("invalid_codex_timeout")

    def validate_binding(self, binding: dict) -> None:
        if binding.get("driver") != "codex" or binding.get("session_id") != self.thread:
            raise ConfigurationError("codex_native_binding_mismatch")
        if binding.get("modes") != ["queue"]:
            raise ConfigurationError("codex_client_requires_queue_only_binding")

    def deliver(self, envelope: dict) -> tuple[str, str]:
        if envelope["mode"] != "queue":
            return "failed", "Codex queue client supports queue only; steer and interrupt are unverified"
        argv = [self.executable, "queue", "--thread", self.thread, "--message", notification(self.endpoint, envelope)]
        try:
            # No shell; each argument remains data. Neither output stream is logged.
            process = subprocess.Popen(argv, stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        except OSError:
            return "failed", "Codex executable did not start; no native process was launched"
        try:
            code = process.wait(timeout=self.timeout)
        except subprocess.TimeoutExpired:
            # This process is just the dispatch CLI, not the desktop assistant.
            process.kill()
            process.wait()
            return "uncertain", "Codex dispatch process started and timed out; reconcile before retrying"
        if code != 0:
            return "uncertain", "Codex dispatch process exited unsuccessfully; reconcile before retrying"
        return "delivered", "Codex queue transport completed; native acceptance and business completion remain unverified"


def create_adapter(config: dict):
    if config["native"].get("transport", "queue") == "app-server":
        from codex.app_server_adapter import CodexAppServerAdapter
        return CodexAppServerAdapter(config)
    return CodexAdapter(config)
