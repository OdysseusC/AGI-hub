"""User-configured official Routine webhook, without a guessed platform schema."""

from __future__ import annotations

import json
import re

from shared.notification import notification
from shared.transport import ConfigurationError, parse_address, post_json_once, valid_header

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
        def replace(match: re.Match) -> str:
            name = match.group(1)
            if name == "envelope_json":
                return json.dumps(envelope, ensure_ascii=False, allow_nan=False)
            if name == "notification":
                return notification(endpoint, envelope)
            if name == "task_id" and envelope.get("mailbox") is True:
                raise ConfigurationError("mailbox_does_not_support_task_id_placeholder")
            return envelope[name]
        # One pass: placeholders in the actual task content remain literal data.
        return PLACEHOLDER.sub(replace, value)
    if isinstance(value, list):
        return [expand(child, envelope, endpoint) for child in value]
    if isinstance(value, dict):
        return {key: expand(child, envelope, endpoint) for key, child in value.items()}
    return value


class GrokRoutineAdapter:
    mailbox_protocol_version = 1
    supports_native_queue = True

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
        if binding.get("mailbox_protocol_version", 0) == 1 and binding.get("mailbox_native_queue_verified") is not True:
            raise ConfigurationError("needs_native_queue_verification")

    def deliver(self, envelope: dict) -> tuple[str, str]:
        if not isinstance(envelope, dict) or envelope.get("recipient") != self.endpoint:
            return "failed", "Routine webhook rejected a foreign recipient before dispatch"
        if envelope.get("mode") != "queue":
            return "failed", "Routine webhook has no verified steer or interrupt mode"
        mailbox = envelope.get("mailbox", False)
        if type(mailbox) is not bool:
            return "failed", "Routine webhook rejected invalid mailbox metadata"
        kinds = ("request", "reply", "result") if mailbox else ("task", "control", "result")
        required = ("message_id", "sender", "content", "correlation_id") if mailbox else ("message_id", "task_id", "content")
        if envelope.get("kind") not in kinds or not all(
            isinstance(envelope.get(field), str) and envelope[field].strip()
            for field in required
        ):
            return "failed", "Routine webhook requires a complete supported envelope"
        if mailbox:
            origin = envelope.get("root_origin")
            if (
                envelope.get("task_id", "") != ""
                or not isinstance(origin, dict)
                or not all(isinstance(origin.get(field), str) and origin[field].strip() for field in ("endpoint", "device_id"))
                or not isinstance(origin.get("session_id"), str)
            ):
                return "failed", "Routine webhook rejected malformed addressed message metadata"
        try:
            body = expand(self.body, envelope, self.endpoint)
        except (ConfigurationError, KeyError, TypeError, ValueError):
            return "failed", "invalid webhook template; no native request was started"
        return post_json_once(self.url, body, self.headers, self.timeout)
