"""User-configured official Routine webhook, without a guessed platform schema."""

from __future__ import annotations

import json
import re
import hashlib
import http.client
import os
import stat
import socket
import threading
import time
from pathlib import Path

from shared.notification import notification
from shared.transport import ConfigurationError, connection, parse_address, post_json_once, valid_header

PLACEHOLDER = re.compile(r"\{\{(envelope|envelope_json|notification|content|task_id|message_id)\}\}")


def has_complete_envelope(value: object) -> bool:
    if isinstance(value, str):
        return value == "{{envelope}}" or "{{envelope_json}}" in value or "{{notification}}" in value
    if isinstance(value, dict):
        return any(has_complete_envelope(child) for child in value.values())
    if isinstance(value, list):
        return any(has_complete_envelope(child) for child in value)
    return False


def expand(value: object, envelope: dict, endpoint: str) -> object:
    if isinstance(value, str):
        if value == "{{envelope}}":
            return envelope
        if "{{envelope}}" in value:
            raise ConfigurationError("envelope_object_must_be_entire_value")
        replacements = {
            "envelope_json": json.dumps(envelope, ensure_ascii=False, allow_nan=False),
            "notification": notification(endpoint, envelope),
            "content": envelope["content"], "task_id": envelope["task_id"], "message_id": envelope["message_id"],
        }
        # One pass: placeholders in the actual task content remain literal data.
        return PLACEHOLDER.sub(lambda match: replacements[match.group(1)], value)
    if isinstance(value, list):
        return [expand(child, envelope, endpoint) for child in value]
    if isinstance(value, dict):
        return {key: expand(child, envelope, endpoint) for key, child in value.items()}
    return value


class GrokRoutineAdapter:
    def __init__(self, config: dict):
        native = config["native"]
        if set(native) - {"routine_id", "url", "headers", "body", "timeout_seconds"}:
            raise ConfigurationError("invalid_grok_native_fields")
        self.endpoint = config["endpoint"]
        self.routine_id = native.get("routine_id", "")
        self.url = native.get("url", "")
        self.headers = native.get("headers", {})
        self.body = native.get("body")
        self.timeout = native.get("timeout_seconds", 30)
        if not isinstance(self.routine_id, str) or not self.routine_id.strip() or any(ord(char) < 32 for char in self.routine_id):
            raise ConfigurationError("unconfigured_grok_routine_id")
        if not isinstance(self.url, str) or not self.url.strip() or self.url.startswith("<"):
            raise ConfigurationError("unconfigured_grok_official_routine_webhook")
        parse_address(self.url)
        if not isinstance(self.headers, dict) or not all(
            isinstance(k, str) and isinstance(v, str) and valid_header(k, v) for k, v in self.headers.items()
        ):
            raise ConfigurationError("invalid_grok_headers")
        if not has_complete_envelope(self.body):
            raise ConfigurationError("grok_body_requires_complete_envelope_placeholder")
        if isinstance(self.timeout, bool) or not isinstance(self.timeout, (int, float)) or not 1 <= self.timeout <= 300:
            raise ConfigurationError("invalid_grok_timeout")

    def validate_binding(self, binding: dict) -> None:
        if binding.get("driver") != "grok-routine" or binding.get("session_id") != self.routine_id:
            raise ConfigurationError("grok_native_binding_mismatch")
        if binding.get("modes") != ["queue"]:
            raise ConfigurationError("grok_routine_requires_queue_only_binding")

    def deliver(self, envelope: dict) -> tuple[str, str]:
        if envelope["mode"] != "queue":
            return "failed", "Routine webhook has no verified steer or interrupt mode"
        try:
            body = expand(self.body, envelope, self.endpoint)
        except (ConfigurationError, TypeError, ValueError):
            return "failed", "invalid webhook template; no native request was started"
        return post_json_once(self.url, body, self.headers, self.timeout)


FILE_PROTOCOL = "agi-hub-grok-file-v1"
MAX_PACKET_BYTES = 4 << 20
FILE_OUTCOMES = {
    "delivered": "Grok webhook returned HTTP 200; assistant acceptance and business completion remain unverified",
    "failed": "Grok webhook rejected authentication with HTTP 401 or 403; no automatic retry",
    "uncertain": "Grok webhook delivery is unconfirmed; reconcile preserved files before any new dispatch",
}
UNCERTAIN_FILES = ("uncertain", "Grok file delivery evidence is incomplete or conflicting; reconcile preserved files before any new dispatch")


def _real_path(path: Path, *, directory: bool) -> None:
    """Reject symlinks and Windows junction/reparse traversal, including parents."""
    for current in (path, *path.parents):
        info = current.lstat()
        if stat.S_ISLNK(info.st_mode) or getattr(info, "st_file_attributes", 0) & 0x400:
            raise ValueError("redirected_grok_path")
        if current != path or directory:
            if not stat.S_ISDIR(info.st_mode):
                raise ValueError("invalid_grok_directory")
        elif not stat.S_ISREG(info.st_mode):
            raise ValueError("invalid_grok_file")


def _encode(value: object) -> bytes:
    return json.dumps(value, ensure_ascii=False, allow_nan=False, sort_keys=True, separators=(",", ":")).encode("utf-8")


def _publish(path: Path, encoded: bytes) -> None:
    """The persistent mkdir claim grants exclusive publication for this file ID."""
    _real_path(path.parent, directory=True)
    if os.path.lexists(path):
        raise FileExistsError("grok_file_already_exists")
    temporary = path.with_name(path.name + ".tmp")
    # Exclusive creation also preserves incomplete writes for reconciliation.
    with temporary.open("xb") as stream:
        stream.write(encoded)
        stream.flush()
        os.fsync(stream.fileno())
    _real_path(path.parent, directory=True)
    if os.path.lexists(path):
        raise FileExistsError("grok_file_already_exists")
    temporary.rename(path)


def _read_file(path: Path) -> bytes:
    _real_path(path, directory=False)
    with path.open("rb") as stream:
        data = stream.read(MAX_PACKET_BYTES + 1)
    if len(data) > MAX_PACKET_BYTES:
        raise ValueError("oversized_grok_file")
    return data


def _request_once(address, body: bytes, headers: dict, timeout: float):
    """Bound slow response headers; never send after an over-budget DNS lookup."""
    conn = connection(address, timeout)
    deadline = time.monotonic() + timeout
    expired = threading.Event()

    def stop_socket():
        expired.set()
        active = conn.sock
        if active is not None:
            try:
                active.shutdown(socket.SHUT_RDWR)
            except OSError:
                pass
            # Keep the shut-down socket attached until request() unwinds.
            # close() would set conn.sock=None and permit implicit reconnection.

    timer = None
    try:
        # Python's system DNS resolver has no portable cancellation deadline.
        # A late lookup may delay return, but must not start a late native POST.
        conn.connect()
        conn.auto_open = 0
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise TimeoutError("grok_connect_deadline")
        conn.sock.settimeout(remaining)
        timer = threading.Timer(remaining, stop_socket)
        timer.daemon = True
        timer.start()
        if expired.is_set() or time.monotonic() >= deadline:
            raise TimeoutError("grok_request_deadline")
        conn.request("POST", address.path, body=body, headers=headers)
        response = conn.getresponse()
        if expired.is_set() or time.monotonic() >= deadline:
            response.close()
            raise TimeoutError("grok_response_deadline")
        return conn, response
    except Exception:
        conn.close()
        raise
    finally:
        if timer is not None:
            timer.cancel()


class GrokFileWebhookAdapter(GrokRoutineAdapter):
    """Publish complete local packets, then wake the configured Routine once."""

    def __init__(self, config: dict):
        native = config["native"]
        if set(native) - {"transport", "routine_id", "url", "headers", "timeout_seconds", "inbox_dir"}:
            raise ConfigurationError("invalid_grok_file_native_fields")
        if native.get("transport") != "file-webhook":
            raise ConfigurationError("invalid_grok_transport")
        # Reuse the existing URL, routine and timeout validation without accepting
        # a configurable payload: this mode's webhook body is always only {id}.
        super().__init__({**config, "native": {
            key: value for key, value in {**native, "body": "{{envelope}}"}.items()
            if key not in {"transport", "inbox_dir"}
        }})
        reserved = {"host", "content-length", "connection", "transfer-encoding", "content-type", "idempotency-key"}
        if any(key.lower() in reserved for key in self.headers):
            raise ConfigurationError("reserved_grok_file_header")
        raw_inbox = native.get("inbox_dir")
        if not isinstance(raw_inbox, str) or not raw_inbox or any(ord(char) < 32 for char in raw_inbox):
            raise ConfigurationError("invalid_grok_inbox_directory")
        self.inbox = Path(raw_inbox)
        try:
            if not self.inbox.is_absolute() or ".." in self.inbox.parts:
                raise ValueError("invalid_grok_inbox_directory")
            _real_path(self.inbox, directory=True)
        except (OSError, ValueError):
            raise ConfigurationError("grok_inbox_requires_existing_absolute_real_directory") from None

    def _previous(self, claim: Path, packet_path: Path, fingerprint: str) -> tuple[str, str]:
        try:
            _real_path(claim, directory=True)
            if _read_file(claim / "claim.json") != _encode({"packet_sha256": fingerprint}):
                return UNCERTAIN_FILES
            if hashlib.sha256(_read_file(packet_path)).hexdigest() != fingerprint:
                return UNCERTAIN_FILES
            receipt = json.loads(_read_file(claim / "delivery.json"))
            if not isinstance(receipt, dict) or receipt.get("status") not in FILE_OUTCOMES:
                return UNCERTAIN_FILES
            if receipt != {"packet_sha256": fingerprint, "status": receipt["status"]}:
                return UNCERTAIN_FILES
            return receipt["status"], FILE_OUTCOMES[receipt["status"]]
        except (OSError, ValueError, TypeError):
            return UNCERTAIN_FILES

    def _notify(self, file_id: str) -> tuple[str, str]:
        address = parse_address(self.url)
        headers = {**self.headers, "Content-Type": "application/json", "Host": address.authority,
                   "Connection": "close", "Idempotency-Key": file_id}
        try:
            conn, response = _request_once(address, _encode({"id": file_id}), headers, self.timeout)
        except (OSError, http.client.HTTPException, ValueError):
            return "uncertain", FILE_OUTCOMES["uncertain"]
        try:
            status = "delivered" if response.status == 200 else "failed" if response.status in (401, 403) else "uncertain"
            return status, FILE_OUTCOMES[status]
        finally:
            # Response bodies can include private platform data. Never retain them.
            response.close()
            conn.close()

    def deliver(self, envelope: dict) -> tuple[str, str]:
        if envelope.get("mode") != "queue":
            return "failed", "Routine webhook has no verified steer or interrupt mode"
        try:
            if envelope.get("recipient") != self.endpoint or envelope.get("kind") not in {"task", "result", "control"}:
                raise ValueError("invalid_grok_envelope")
            message_id = envelope["message_id"]
            if not isinstance(message_id, str) or not message_id:
                raise ValueError("invalid_grok_message_id")
            file_id = "hub-" + hashlib.sha256(message_id.encode("utf-8")).hexdigest()
            canonical_envelope = json.loads(_encode(envelope))
            packet = {"protocol": FILE_PROTOCOL, "id": file_id, "endpoint": self.endpoint,
                      "routine_id": self.routine_id, "envelope": canonical_envelope,
                      "notification": notification(self.endpoint, canonical_envelope)}
            encoded = _encode(packet)
            if len(encoded) > MAX_PACKET_BYTES:
                raise ValueError("oversized_grok_packet")
        except (KeyError, TypeError, ValueError):
            return "failed", "invalid Grok packet; no native request was started"
        fingerprint = hashlib.sha256(encoded).hexdigest()
        claim_root = self.inbox / ".claims"
        claim = claim_root / file_id
        packet_path = self.inbox / (file_id + ".json")
        try:
            _real_path(self.inbox, directory=True)
            claim_root.mkdir(exist_ok=True)
            _real_path(claim_root, directory=True)
            try:
                claim.mkdir()
            except FileExistsError:
                return self._previous(claim, packet_path, fingerprint)
            _publish(claim / "claim.json", _encode({"packet_sha256": fingerprint}))
            _publish(packet_path, encoded)
            result = self._notify(file_id)
            _publish(claim / "delivery.json", _encode({"packet_sha256": fingerprint, "status": result[0]}))
            return result
        except (OSError, ValueError, TypeError):
            # Never remove an uncertain claim or replay a possibly accepted request.
            return UNCERTAIN_FILES


def create_adapter(config: dict):
    transport = config["native"].get("transport")
    if transport is None:
        return GrokRoutineAdapter(config)
    if transport == "file-webhook":
        return GrokFileWebhookAdapter(config)
    raise ConfigurationError("invalid_grok_transport")
