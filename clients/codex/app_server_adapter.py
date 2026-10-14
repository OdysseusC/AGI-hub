"""Result-only delivery to the running Codex daemon through its official proxy.

Thread reads exclude history; turns/list loads IDs/status only. A steer ACK
confirms delivery to the current guarded turn, never business completion.
"""

from __future__ import annotations

import json
import queue
import subprocess
import threading
import time
import uuid

from codex.adapter import CodexAdapter
from shared.notification import notification
from shared.transport import ConfigurationError

MAX_FRAME_BYTES = 4 << 20


class ProxyError(RuntimeError):
    """Only fixed diagnostic codes, never native response text."""


class ProxyRPC:
    def __init__(self, executable: str, timeout: float):
        self.deadline = time.monotonic() + timeout
        self.process = subprocess.Popen(
            [executable, "app-server", "proxy"], stdin=subprocess.PIPE,
            stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, shell=False,
        )
        self.frames = queue.Queue(maxsize=8)
        self.failure = None
        self.expire_lock = threading.Lock()
        self.expired = False
        self.closed = False
        self.next_id = 0
        self.timer = threading.Timer(timeout, self.expire)
        self.timer.daemon = True
        self.reader = threading.Thread(target=self.read_frames, daemon=True)
        self.reader.start()
        self.timer.start()

    def expire(self):
        # The caller and timer can observe the same deadline concurrently. Stop
        # the helper exactly once even if the timer has not been scheduled yet.
        with self.expire_lock:
            if self.expired or self.closed:
                return
            self.expired = True
            self.failure = "proxy_timeout"
            try:
                self.process.kill()  # Never the existing daemon.
            except OSError:
                pass

    def read_frames(self):
        try:
            while True:
                line = self.process.stdout.readline(MAX_FRAME_BYTES + 1)
                if not line:
                    self.failure = self.failure or "proxy_closed"
                    return
                if len(line) > MAX_FRAME_BYTES or not line.endswith(b"\n"):
                    self.failure = "invalid_proxy_frame"
                    return
                value = json.loads(line)
                if not isinstance(value, dict):
                    self.failure = "invalid_proxy_frame"
                    return
                # Ignore notifications and server requests. This transport does
                # not respond to approval/tool requests or enlarge permissions.
                if type(value.get("id")) is int and "method" not in value:
                    try:
                        self.frames.put_nowait(value)
                    except queue.Full:
                        self.failure = "unexpected_proxy_frames"
                        return
        except Exception:
            self.failure = self.failure or "invalid_proxy_frame"

    def send(self, value: dict):
        if time.monotonic() >= self.deadline or self.failure == "proxy_timeout":
            self.expire()
            raise ProxyError("proxy_timeout")
        encoded = json.dumps(value, ensure_ascii=False, allow_nan=False).encode("utf-8") + b"\n"
        if len(encoded) > MAX_FRAME_BYTES:
            raise ProxyError("proxy_input_too_large")
        try:
            self.process.stdin.write(encoded)
            self.process.stdin.flush()
        except (OSError, ValueError):
            raise ProxyError(self.failure or "proxy_write_unconfirmed") from None

    def notify(self, method: str):
        self.send({"method": method})

    def call(self, method: str, params: dict) -> dict:
        self.next_id += 1
        request_id = self.next_id
        self.send({"id": request_id, "method": method, "params": params})
        while True:
            remaining = self.deadline - time.monotonic()
            if remaining <= 0 or self.failure == "proxy_timeout":
                self.expire()
                raise ProxyError("proxy_timeout")
            try:
                # Bounded waits notice EOF while keeping the native overall deadline.
                response = self.frames.get(timeout=min(remaining, 0.1))
            except queue.Empty:
                if self.failure:
                    raise ProxyError(self.failure)
                continue
            if response.get("id") != request_id:
                raise ProxyError("unexpected_proxy_response")
            if "error" in response or not isinstance(response.get("result"), dict):
                raise ProxyError("native_rpc_rejected_or_unconfirmed")
            return response["result"]

    def close(self):
        self.timer.cancel()
        with self.expire_lock:
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


class CodexAppServerAdapter(CodexAdapter):
    def __init__(self, config: dict):
        native = config["native"]
        if set(native) - {"transport", "result_mode", "idle_start_verified", "thread_id", "executable", "timeout_seconds"} or native.get("transport") != "app-server":
            raise ConfigurationError("invalid_codex_app_server_fields")
        if native.get("result_mode", "steer") != "steer":
            raise ConfigurationError("codex_app_server_requires_result_steer")
        if type(native.get("idle_start_verified", False)) is not bool:
            raise ConfigurationError("invalid_codex_idle_start_verification")
        base = {"endpoint": config["endpoint"], "native": {
            key: value for key, value in native.items() if key in {"thread_id", "executable", "timeout_seconds"}
        }}
        super().__init__(base)
        self.idle_start_verified = native.get("idle_start_verified", False)

    def deliver(self, envelope: dict) -> tuple[str, str]:
        if envelope.get("recipient") != self.endpoint or envelope.get("kind") != "result" or envelope.get("mode") != "queue":
            return "failed", "Codex app-server transport accepts only this endpoint's result returns; no native process was started"
        rpc = None
        try:
            text = notification(self.endpoint, envelope)
            rpc = ProxyRPC(self.executable, self.timeout)
            rpc.call("initialize", {
                "clientInfo": {"name": "agi_hub", "title": "AGI Hub", "version": "0.1.0"},
                "capabilities": {"experimentalApi": True},
            })
            rpc.notify("initialized")
            metadata = rpc.call("thread/read", {"threadId": self.thread, "includeTurns": False})
            thread = metadata.get("thread")
            if not isinstance(thread, dict) or thread.get("id") != self.thread or not isinstance(thread.get("status"), dict):
                raise ProxyError("native_thread_mismatch")
            state = thread["status"].get("type")
            if thread.get("canAcceptDirectInput") is False:
                raise ProxyError("native_direct_input_unavailable")
            common = {
                "threadId": self.thread, "input": [{"type": "text", "text": text}],
                "clientUserMessageId": str(uuid.uuid4()),
            }
            if state == "active":
                turns = rpc.call("thread/turns/list", {
                    "threadId": self.thread, "limit": 1, "sortDirection": "desc", "itemsView": "notLoaded",
                }).get("data")
                if not isinstance(turns, list) or len(turns) != 1 or not isinstance(turns[0], dict):
                    raise ProxyError("native_active_turn_unconfirmed")
                turn_id = turns[0].get("id")
                if turns[0].get("status") != "inProgress" or not isinstance(turn_id, str) or not turn_id:
                    raise ProxyError("native_active_turn_unconfirmed")
                acknowledgement = rpc.call("turn/steer", dict(common, expectedTurnId=turn_id))
                if acknowledgement.get("turnId") != turn_id:
                    raise ProxyError("native_steer_ack_mismatch")
                return "delivered", "Codex running daemon acknowledged result steering to its guarded current turn; user presentation remains unverified"
            if state == "idle":
                if not self.idle_start_verified or thread.get("canAcceptDirectInput") is not True:
                    raise ProxyError("native_idle_start_unverified")
                turn = rpc.call("turn/start", common).get("turn")
                if not isinstance(turn, dict) or not isinstance(turn.get("id"), str) or not turn["id"] or turn.get("status") not in ("inProgress", "completed"):
                    raise ProxyError("native_start_ack_unconfirmed")
                return "delivered", "Codex running daemon acknowledged verified idle result input in the original thread; user presentation remains unverified"
            # Unloaded threads are not resumed automatically: desktop tool/context
            # retention must first be proven for that separate native operation.
            raise ProxyError("native_thread_state_unavailable")
        except ProxyError as exc:
            return "uncertain", "Codex app-server result delivery unconfirmed (" + str(exc) + "); reconcile before retrying"
        except Exception:
            return "uncertain", "Codex app-server result delivery is unconfirmed; reconcile before retrying"
        finally:
            if rpc is not None:
                rpc.close()
