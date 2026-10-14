"""Official WorkBuddy local-assistant OpenAPI, distinct from ordinary chats."""

from __future__ import annotations

import json
import re

from shared.notification import notification
from shared.transport import ConfigurationError, post_json_once, valid_header

MESSAGE_URL = "https://www.workbuddy.cn/openapi/v2/localassistant/message"
INVOKE_SCOPE = "user.localassistant.invokable"
MESSAGE_ID = re.compile(r"[A-Za-z0-9._:-]{1,256}\Z")


def delivery_confirmed(response: bytes) -> bool:
    """An HTTP status alone is weaker evidence than the official receipt."""
    def unique(items):
        result = {}
        for key, value in items:
            if key in result:
                raise ValueError("duplicate response key")
            result[key] = value
        return result
    try:
        receipt = json.loads(response, object_pairs_hook=unique)
    except (UnicodeError, ValueError):
        return False
    if not isinstance(receipt, dict) or type(receipt.get("code")) is not int or receipt["code"] != 0:
        return False
    data = receipt.get("data")
    message_id = data.get("message_id") if isinstance(data, dict) else None
    return isinstance(message_id, str) and MESSAGE_ID.fullmatch(message_id) is not None


class WorkBuddyAdapter:
    def __init__(self, config: dict):
        native = config["native"]
        if set(native) - {"access_token", "oauth_scopes", "timeout_seconds"}:
            raise ConfigurationError("invalid_workbuddy_native_fields")
        self.endpoint = config["endpoint"]
        self.token = native.get("access_token", "")
        self.timeout = native.get("timeout_seconds", 30)
        scopes = native.get("oauth_scopes", [])
        if not isinstance(self.token, str) or not self.token.strip() or self.token.startswith("<"):
            raise ConfigurationError("unconfigured_workbuddy_official_oauth_access_token")
        if not valid_header("Authorization", "Bearer " + self.token):
            raise ConfigurationError("invalid_workbuddy_access_token")
        if not isinstance(scopes, list) or not all(isinstance(scope, str) for scope in scopes) or INVOKE_SCOPE not in scopes:
            raise ConfigurationError("unconfigured_workbuddy_invokable_scope")
        if isinstance(self.timeout, bool) or not isinstance(self.timeout, (int, float)) or not 1 <= self.timeout <= 300:
            raise ConfigurationError("invalid_workbuddy_timeout")

    def validate_binding(self, binding: dict) -> None:
        if binding.get("driver") != "workbuddy" or binding.get("session_id") != "localassistant":
            raise ConfigurationError("workbuddy_requires_localassistant_binding")

    def deliver(self, envelope: dict) -> tuple[str, str]:
        if envelope["mode"] != "queue":
            return "failed", "WorkBuddy local-assistant API has no verified steer or interrupt mode"
        body = {"content": notification(self.endpoint, envelope), "msg_type": "text"}
        return post_json_once(
            MESSAGE_URL, body, {"Authorization": "Bearer " + self.token}, self.timeout,
            response_validator=delivery_confirmed,
        )
