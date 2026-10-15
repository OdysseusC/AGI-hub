"""Result returns through the official, host-provided Codex Desktop MCP server.

Only the configured native thread receives input. The MCP server owns its
connection to the desktop; this client never opens or discovers private IPC.
"""

from __future__ import annotations

import json
import math
import os
from pathlib import Path
import queue
import subprocess
import sys
import threading
import time
import uuid

from shared.notification import notification
from shared.transport import ConfigurationError

PROTOCOL_VERSION = "2025-11-25"
MAX_FRAME_BYTES = 4 << 20
TOOL_NAME = "send_message_to_thread"
OFFICIAL_ENVIRONMENT = {
    "CODEX_APP_TOOLS_PIPE_PATH", "CODEX_MCP_NODE_PATH",
    "CODEX_BROWSER_USE_NODE_PATH", "CODEX_ELECTRON_RESOURCES_PATH",
    "CODEX_CLI_PATH", "CODEX_THREAD_ID", "XDG_CACHE_HOME",
}
OS_ENVIRONMENT = {
    "PATH", "HOME", "USER", "LOGNAME", "SHELL", "TMPDIR", "TMP", "TEMP",
    "LANG", "LC_ALL", "LC_CTYPE", "SYSTEMROOT", "SystemRoot", "WINDIR",
    "USERPROFILE", "HOMEDRIVE", "HOMEPATH", "APPDATA", "LOCALAPPDATA",
    "PROGRAMFILES", "PROGRAMFILES(X86)", "COMMONPROGRAMFILES", "SYSTEMDRIVE",
    "PATHEXT", "NUMBER_OF_PROCESSORS", "PROCESSOR_ARCHITECTURE",
}


class McpError(RuntimeError):
    """Fixed diagnostic codes only; native response bodies are never surfaced."""


def unique_object(pairs):
    value = {}
    for key, item in pairs:
        if key in value:
            raise ValueError("duplicate key")
        value[key] = item
    return value


class DesktopMcpRPC:
    """One bounded stdio connection, with no reconnect or tool-call retry."""

    def __init__(self, command: list[str], cwd: str, environment: dict, timeout: float):
        self.deadline = time.monotonic() + timeout
        self.process = subprocess.Popen(
            command, cwd=cwd, env=environment, stdin=subprocess.PIPE,
            stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, shell=False,
        )
        self.frames = queue.Queue(maxsize=8)
        self.failure = None
        self.lock = threading.Lock()
        self.expired = False
        self.closed = False
        self.next_id = 0
        self.timer = threading.Timer(timeout, self.expire)
        self.timer.daemon = True
        self.reader = threading.Thread(target=self.read_frames, daemon=True)
        self.reader.start()
        self.timer.start()

    def expire(self):
        with self.lock:
            if self.expired or self.closed:
                return
            self.expired = True
            self.failure = "desktop_mcp_timeout"
            try:
                self.process.kill()  # Only this official stdio helper, never the desktop.
            except OSError:
                pass

    def read_frames(self):
        try:
            while True:
                line = self.process.stdout.readline(MAX_FRAME_BYTES + 1)
                if not line:
                    self.failure = self.failure or "desktop_mcp_closed"
                    return
                if len(line) > MAX_FRAME_BYTES or not line.endswith(b"\n"):
                    raise ValueError("invalid frame")
                value = json.loads(line, object_pairs_hook=unique_object)
                if not isinstance(value, dict) or value.get("jsonrpc") != "2.0":
                    raise ValueError("invalid frame")
                # Do not answer native approval, sampling, or permission requests.
                if type(value.get("id")) is int and "method" not in value:
                    try:
                        self.frames.put_nowait(value)
                    except queue.Full:
                        self.failure = "unexpected_desktop_mcp_frames"
                        return
        except Exception:
            self.failure = self.failure or "invalid_desktop_mcp_frame"

    def send(self, value: dict):
        if time.monotonic() >= self.deadline or self.failure == "desktop_mcp_timeout":
            self.expire()
            raise McpError("desktop_mcp_timeout")
        encoded = json.dumps(dict(value, jsonrpc="2.0"), ensure_ascii=False, allow_nan=False).encode("utf-8") + b"\n"
        if len(encoded) > MAX_FRAME_BYTES:
            raise McpError("desktop_mcp_input_too_large")
        try:
            self.process.stdin.write(encoded)
            self.process.stdin.flush()
        except (OSError, ValueError):
            raise McpError("desktop_mcp_write_unconfirmed") from None

    def notify(self, method: str):
        self.send({"method": method})

    def call(self, method: str, params: dict) -> dict:
        self.next_id += 1
        request_id = self.next_id
        self.send({"id": request_id, "method": method, "params": params})
        while True:
            remaining = self.deadline - time.monotonic()
            if remaining <= 0 or self.failure == "desktop_mcp_timeout":
                self.expire()
                raise McpError("desktop_mcp_timeout")
            try:
                response = self.frames.get(timeout=min(remaining, 0.1))
            except queue.Empty:
                if self.failure:
                    raise McpError(self.failure)
                continue
            if response.get("id") != request_id:
                raise McpError("unexpected_desktop_mcp_response")
            if "error" in response or not isinstance(response.get("result"), dict):
                raise McpError("desktop_mcp_rpc_unconfirmed")
            return response["result"]

    def close(self):
        self.timer.cancel()
        with self.lock:
            self.closed = True
        try:
            if self.process.poll() is None:
                self.process.terminate()
                try:
                    self.process.wait(timeout=1)
                except subprocess.TimeoutExpired:
                    self.process.kill()
                    self.process.wait(timeout=1)
        except (OSError, subprocess.TimeoutExpired):
            pass
        for stream in (self.process.stdin, self.process.stdout):
            try:
                stream.close()
            except (OSError, ValueError):
                pass
        self.reader.join(timeout=0.2)


def absolute_file(value, code: str) -> Path:
    if not isinstance(value, str) or not Path(value).is_absolute():
        raise ConfigurationError(code)
    try:
        path = Path(value).resolve(strict=True)
        if not path.is_file():
            raise OSError()
        return path
    except (OSError, ValueError):
        raise ConfigurationError(code) from None


class CodexDesktopMcpAdapter:
    def __init__(self, config: dict):
        native = config["native"]
        if set(native) - {"transport", "command", "args", "cwd", "thread_id", "timeout_seconds"} or native.get("transport") != "desktop-mcp":
            raise ConfigurationError("invalid_codex_desktop_mcp_fields")
        self.endpoint = config["endpoint"]
        self.thread = native.get("thread_id")
        if not isinstance(self.thread, str) or not self.thread or self.thread != self.thread.strip() or any(ord(char) < 32 for char in self.thread):
            raise ConfigurationError("unconfigured_codex_thread_id")
        command = absolute_file(native.get("command"), "invalid_codex_desktop_mcp_command")
        if sys.platform == "win32":
            if command.suffix.lower() != ".exe":
                raise ConfigurationError("windows_codex_desktop_mcp_requires_actual_exe")
        elif command.name not in ("launch_codex_app_tools_mcp", "node", "nodejs") or not os.access(command, os.X_OK):
            raise ConfigurationError("codex_desktop_mcp_requires_official_launcher_or_node")
        args = native.get("args")
        if not isinstance(args, list) or len(args) != 1:
            raise ConfigurationError("invalid_codex_desktop_mcp_server_argument")
        server = absolute_file(args[0], "invalid_codex_desktop_mcp_server_argument")
        if server.name != "server.mjs":
            raise ConfigurationError("invalid_codex_desktop_mcp_server_argument")
        cwd = native.get("cwd")
        if not isinstance(cwd, str) or not Path(cwd).is_absolute():
            raise ConfigurationError("invalid_codex_desktop_mcp_cwd")
        try:
            directory = Path(cwd).resolve(strict=True)
            if not directory.is_dir() or server.parent != directory:
                raise OSError()
        except (OSError, ValueError):
            raise ConfigurationError("invalid_codex_desktop_mcp_cwd") from None
        self.timeout = native.get("timeout_seconds", 30)
        if isinstance(self.timeout, bool) or not isinstance(self.timeout, (int, float)) or not math.isfinite(self.timeout) or not 1 <= self.timeout <= 300:
            raise ConfigurationError("invalid_codex_timeout")
        # These values are supplied by the official host, never by the Envelope,
        # private client.json, a copied ticket, or a guessed native socket path.
        self.caller = os.environ.get("CODEX_THREAD_ID", "")
        if not self.caller or self.caller != self.caller.strip() or any(ord(char) < 32 for char in self.caller):
            raise ConfigurationError("codex_desktop_mcp_requires_host_thread")
        if not os.environ.get("CODEX_APP_TOOLS_PIPE_PATH"):
            raise ConfigurationError("codex_desktop_mcp_requires_host_environment")
        self.command = [str(command), str(server)]
        self.cwd = str(directory)
        self.environment = {key: value for key, value in os.environ.items() if key in OFFICIAL_ENVIRONMENT | OS_ENVIRONMENT}

    def validate_binding(self, binding: dict) -> None:
        if binding.get("driver") != "codex" or binding.get("session_id") != self.thread:
            raise ConfigurationError("codex_native_binding_mismatch")
        if binding.get("modes") != ["queue"]:
            raise ConfigurationError("codex_client_requires_queue_only_binding")

    @staticmethod
    def validate_tool(result: dict):
        tools = result.get("tools")
        if not isinstance(tools, list):
            raise McpError("desktop_mcp_tool_unavailable")
        matching = [tool for tool in tools if isinstance(tool, dict) and tool.get("name") == TOOL_NAME]
        if len(matching) != 1:
            raise McpError("desktop_mcp_tool_unavailable")
        schema = matching[0].get("inputSchema")
        if not isinstance(schema, dict) or schema.get("type") != "object" or set(schema.get("required", [])) != {"threadId", "prompt"}:
            raise McpError("desktop_mcp_tool_schema_unverified")
        properties = schema.get("properties")
        if not isinstance(properties, dict) or any(not isinstance(properties.get(key), dict) or properties[key].get("type") != "string" for key in ("threadId", "prompt")):
            raise McpError("desktop_mcp_tool_schema_unverified")

    def deliver(self, envelope: dict) -> tuple[str, str]:
        if envelope.get("recipient") != self.endpoint or envelope.get("kind") != "result" or envelope.get("mode") != "queue":
            return "failed", "Codex Desktop MCP accepts only this endpoint's result returns; no native input was attempted"
        rpc = None
        attempted = False
        try:
            prompt = notification(self.endpoint, envelope)
            params = {
                "name": TOOL_NAME, "arguments": {"threadId": self.thread, "prompt": prompt},
                "_meta": {"threadId": self.caller, "callId": "agi-hub-" + str(uuid.uuid4())},
            }
            # Reject oversize complete JSON before launching or invoking native input.
            if len(json.dumps(params, ensure_ascii=False, allow_nan=False).encode("utf-8")) + 256 > MAX_FRAME_BYTES:
                raise McpError("desktop_mcp_input_too_large")
            rpc = DesktopMcpRPC(self.command, self.cwd, self.environment, self.timeout)
            initialized = rpc.call("initialize", {
                "protocolVersion": PROTOCOL_VERSION, "capabilities": {},
                "clientInfo": {"name": "agi-hub", "title": "AGI Hub", "version": "0.1.0"},
            })
            if initialized.get("protocolVersion") != PROTOCOL_VERSION or not isinstance(initialized.get("capabilities"), dict) or not isinstance(initialized["capabilities"].get("tools"), dict) or not isinstance(initialized.get("serverInfo"), dict) or initialized["serverInfo"].get("name") != "codex-app-tools":
                raise McpError("desktop_mcp_handshake_unverified")
            rpc.notify("notifications/initialized")
            self.validate_tool(rpc.call("tools/list", {}))
            attempted = True
            result = rpc.call("tools/call", params)
            content = result.get("content")
            if result.get("isError") is not False or not isinstance(content, list) or len(content) != 1 or not isinstance(content[0], dict) or content[0].get("type") != "text" or not isinstance(content[0].get("text"), str):
                raise McpError("desktop_mcp_delivery_unconfirmed")
            try:
                acknowledgement = json.loads(content[0]["text"], object_pairs_hook=unique_object)
            except (ValueError, TypeError):
                raise McpError("desktop_mcp_delivery_unconfirmed") from None
            if not isinstance(acknowledgement, dict) or acknowledgement.get("threadId") != self.thread:
                raise McpError("desktop_mcp_target_ack_mismatch")
            return "delivered", "Official Codex Desktop MCP acknowledged send-now input to the bound original thread; business completion remains separate"
        except Exception:
            if attempted:
                return "uncertain", "Codex Desktop MCP native input was attempted without a verified target acknowledgement; reconcile before retrying"
            return "failed", "Codex Desktop MCP preflight or tool discovery failed; no native input was attempted"
        finally:
            if rpc is not None:
                rpc.close()
