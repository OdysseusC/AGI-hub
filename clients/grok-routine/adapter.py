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

    def deliver(self, envelope: dict) -> tuple[str, str]:
        if envelope["mode"] != "queue":
            return "failed", "Routine webhook has no verified steer or interrupt mode"
        try:
            body = expand(self.body, envelope, self.endpoint)
        except (ConfigurationError, TypeError, ValueError):
            return "failed", "invalid webhook template; no native request was started"
        return post_json_once(self.url, body, self.headers, self.timeout)
