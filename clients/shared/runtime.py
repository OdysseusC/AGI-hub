"""Consume the persistent inbox after subscribing to Hub's event stream."""

from __future__ import annotations

import argparse
import http.client
import json
import sys
import time
from pathlib import Path
from typing import Callable, Protocol
from urllib.parse import urlencode

from shared.transport import ConfigurationError, HubError, parse_address, request, valid_header

MAX_BYTES = 1 << 20
RECOVERY_ACTIONS = {
    "needs_native_queue_verification": (
        "An administrator must verify the fixed Routine's native probe and record its evidence with "
        "agi-hub native-queue --data <private-hub-dir> --endpoint <endpoint> --native-session <routine-id> "
        "--verified=true --evidence-ref <verified-probe-reference>. This Client cannot grant verification."
    ),
    "needs_native_observation": (
        "Observe the bound WorkBuddy session through an official read-only native interface and record its actual state. "
        "A running Client, an old successful CLI exit, or an absent process does not establish idle."
    ),
}


def status(state: str, code: str) -> None:
    # Only fixed diagnostic codes. Never exception strings, native output or tokens.
    value = {"state": state, "code": code}
    if code in RECOVERY_ACTIONS:
        value["action"] = RECOVERY_ACTIONS[code]
    print(json.dumps(value), flush=True)


def read_json(path: Path) -> object:
    if path.stat().st_size > MAX_BYTES:
        raise ConfigurationError("config_too_large")
    def unique(items: list[tuple[str, object]]) -> dict[str, object]:
        result: dict[str, object] = {}
        for key, value in items:
            if key in result:
                raise ConfigurationError("duplicate_json_key")
            result[key] = value
        return result
    return json.loads(path.read_text(encoding="utf-8"), object_pairs_hook=unique)


def load_config(path: Path) -> dict:
    try:
        config = read_json(path)
    except (OSError, UnicodeError, ValueError) as exc:
        if isinstance(exc, ConfigurationError):
            raise
        raise ConfigurationError("cannot_read_private_config") from None
    if not isinstance(config, dict) or set(config) - {"base_url", "endpoint", "endpoint_token", "native"}:
        raise ConfigurationError("invalid_client_config_fields")
    for field in ("base_url", "endpoint", "endpoint_token"):
        if not isinstance(config.get(field), str) or not config[field].strip():
            raise ConfigurationError("missing_client_config_field")
    if len(config["endpoint"]) > 128 or any(ord(char) < 32 for char in config["endpoint"]):
        raise ConfigurationError("invalid_endpoint")
    if not valid_header("Authorization", "Bearer " + config["endpoint_token"]):
        raise ConfigurationError("invalid_endpoint_token")
    parse_address(config["base_url"], hub=True)
    if not isinstance(config.get("native", {}), dict):
        raise ConfigurationError("invalid_native_config")
    config.setdefault("native", {})
    return config


class Events:
    def __init__(self, conn: http.client.HTTPConnection, response: http.client.HTTPResponse):
        self.conn = conn
        self.response = response

    def __enter__(self) -> "Events":
        return self

    def __exit__(self, *_: object) -> None:
        self.response.close()
        self.conn.close()

    def changes(self):
        event = "message"
        size = 0
        try:
            while True:
                line = self.response.readline(MAX_BYTES + 1)
                if not line:
                    raise HubError("event_stream_disconnected")
                size += len(line)
                if size > MAX_BYTES:
                    raise HubError("invalid_event_stream")
                line = line.rstrip(b"\r\n")
                if not line:
                    if event == "change":
                        yield None
                    event, size = "message", 0
                elif line.startswith(b"event:"):
                    event = line[6:].strip().decode("utf-8")
                # Keepalive comments are not business events and do not poll inbox.
        except (OSError, http.client.HTTPException, UnicodeError):
            raise HubError("event_stream_disconnected") from None


class HubAPI:
    def __init__(self, config: dict):
        self.address = parse_address(config["base_url"], hub=True)
        self.endpoint = config["endpoint"]
        self.headers = {
            "Authorization": "Bearer " + config["endpoint_token"],
            "Host": self.address.authority,
            "Origin": self.address.origin,
            "Connection": "close",
        }
        for field in ("routine_id", "session_id", "thread_id"):
            if field in config.get("native", {}):
                session = config["native"][field]
                if not isinstance(session, str) or not session.strip() or not valid_header("X-AGI-Hub-Native-Session", session):
                    raise ConfigurationError("invalid_fixed_native_session")
                self.headers["X-AGI-Hub-Native-Session"] = session
                break

    def path(self, name: str, query: dict[str, str] | None = None) -> str:
        if query is not None and "endpoint" in query:
            raise ConfigurationError("query_cannot_override_fixed_endpoint")
        return "/v1/" + name + "?" + urlencode({"endpoint": self.endpoint, **(query or {})})

    def post(self, name: str, payload: object = None) -> object:
        return self.json_request("POST", name, payload)

    def get(self, name: str, query: dict[str, str] | None = None) -> object:
        return self.json_request("GET", name, query=query)

    def json_request(self, method: str, name: str, payload: object = None, *, query: dict[str, str] | None = None) -> object:
        try:
            body = b"" if payload is None else json.dumps(payload, ensure_ascii=False, allow_nan=False).encode("utf-8")
        except (TypeError, ValueError):
            raise HubError("invalid_hub_payload") from None
        if len(body) > MAX_BYTES:
            raise HubError("hub_payload_too_large")
        headers = dict(self.headers, **{"Content-Type": "application/json"})
        try:
            conn, response = request(self.address, method, self.path(name, query), body if method == "POST" else None, headers, 30)
        except (OSError, http.client.HTTPException, ValueError):
            raise HubError("hub_request_unconfirmed") from None
        try:
            if response.status != 200:
                raise HubError("hub_request_rejected", response.status)
            data = response.read(MAX_BYTES + 1)
            if len(data) > MAX_BYTES:
                raise HubError("hub_response_too_large")
            return json.loads(data)
        except (OSError, http.client.HTTPException, UnicodeError, ValueError):
            raise HubError("hub_response_unconfirmed") from None
        finally:
            response.close()
            conn.close()

    def subscribe(self) -> Events:
        headers = dict(self.headers, Accept="text/event-stream")
        try:
            conn, response = request(self.address, "GET", self.path("events"), None, headers, 45)
        except (OSError, http.client.HTTPException, ValueError):
            raise HubError("event_stream_unavailable") from None
        if response.status != 200 or response.getheader("Content-Type", "").split(";")[0] != "text/event-stream":
            rejected = response.status
            response.close()
            conn.close()
            raise HubError("event_stream_rejected", rejected)
        return Events(conn, response)


class Adapter(Protocol):
    def validate_binding(self, binding: dict) -> None: ...
    def deliver(self, envelope: dict) -> tuple[str, str]: ...


def verify_binding(api: HubAPI, adapter: Adapter) -> None:
    registered = api.get("endpoint")
    if not isinstance(registered, dict) or type(registered.get("protocol_version")) is not int or registered["protocol_version"] != 1:
        raise ConfigurationError("incompatible_hub_protocol")
    binding = registered.get("endpoint")
    if not isinstance(binding, dict) or binding.get("id") != api.endpoint:
        raise ConfigurationError("hub_endpoint_mismatch")
    if binding.get("enabled") is not True:
        raise ConfigurationError("hub_endpoint_disabled_or_invalid")
    if not isinstance(binding.get("modes"), list) or "queue" not in binding["modes"] or any(
        mode not in ("queue", "steer", "interrupt") for mode in binding["modes"]
    ):
        raise ConfigurationError("hub_endpoint_requires_queue_mode")
    if type(binding.get("idle_wake_verified")) is not bool:
        raise ConfigurationError("invalid_hub_wake_verification")
    if type(binding.get("result_queue_while_busy_verified", False)) is not bool:
        raise ConfigurationError("invalid_hub_busy_result_verification")
    mailbox_version = binding.get("mailbox_protocol_version", 0)
    if type(mailbox_version) is not int or mailbox_version not in (0, 1):
        raise ConfigurationError("incompatible_mailbox_protocol")
    if mailbox_version and getattr(adapter, "mailbox_protocol_version", 0) != mailbox_version:
        raise ConfigurationError("native_transport_does_not_support_mailbox_protocol")
    if type(binding.get("mailbox_while_busy_verified", False)) is not bool:
        raise ConfigurationError("invalid_hub_busy_mailbox_verification")
    native_queue = binding.get("mailbox_native_queue_verified", False)
    if type(native_queue) is not bool:
        raise ConfigurationError("invalid_hub_native_queue_verification")
    if native_queue and (mailbox_version != 1 or binding["modes"] != ["queue"]):
        raise ConfigurationError("invalid_hub_native_queue_contract")
    if native_queue and getattr(adapter, "supports_native_queue", False) is not True:
        raise ConfigurationError("native_transport_does_not_support_native_queue")
    session = registered.get("session")
    if not isinstance(session, dict) or session.get("state") not in ("unknown", "idle", "busy", "needs_user"):
        raise ConfigurationError("invalid_hub_session_state")
    # Unknown is valid observation, not an instruction to mark the session idle.
    # This API is read-only; only the Hub decides whether a message is eligible.
    adapter.validate_binding(binding)
    if mailbox_version == 1 and session["state"] == "unknown" and getattr(adapter, "requires_native_observation", False) is True:
        raise ConfigurationError("needs_native_observation")
    # A mailbox endpoint consumes only its addressed inbox. Never mix legacy
    # tasks or infer another transport when the opted-in protocol is unavailable.
    api.mailbox = mailbox_version == 1


def drain(api: HubAPI, adapter: Adapter) -> None:
    mailbox = getattr(api, "mailbox", False) is True
    inbox, delivery = ("mail/inbox", "mail/delivery") if mailbox else ("inbox", "delivery")
    while True:
        try:
            envelope = api.post(inbox)
        except HubError as exc:
            if exc.status == 422:
                # A deadline/control transition can reject a claim while committing
                # a new event. Wait for an actual event, never poll on HTTP 422.
                status("error", "inbox_rejected_waiting_for_event")
                return
            raise
        if envelope is None:
            return
        if (
            not isinstance(envelope, dict)
            or not isinstance(envelope.get("message_id"), str)
            or envelope.get("recipient") != api.endpoint
            or envelope.get("delivery") != "dispatching"
            or envelope.get("kind") not in (("request", "reply", "status", "result") if mailbox else ("task", "control", "result"))
            or (mailbox and (envelope.get("mailbox") is not True or bool(envelope.get("task_id"))))
            or (not mailbox and envelope.get("mailbox") is True)
        ):
            raise HubError("invalid_claimed_envelope")
        # Once claimed, never retry the native action, including after exceptions.
        try:
            if mailbox and envelope["kind"] == "status":
                outcome, detail = "delivered", "addressed status recorded without native input or a model wake"
            else:
                outcome, detail = adapter.deliver(envelope)
        except Exception:
            outcome, detail = "uncertain", "native client interrupted after claim; reconcile before retrying"
        if outcome not in ("delivered", "failed", "uncertain"):
            outcome, detail = "uncertain", "native adapter returned invalid evidence; reconcile before retrying"
        try:
            api.post(delivery, {"message_id": envelope["message_id"], "outcome": outcome, "detail": detail})
        except HubError:
            # Do not retry delivery or the native action. The Hub retains the claim.
            raise HubError("delivery_receipt_unconfirmed_reconcile_in_hub") from None


def consume(api: HubAPI, adapter: Adapter, *, sleep: Callable[[float], None] = time.sleep) -> int:
    backoff = 1.0
    announced = False
    blocked_code = None

    def can_claim():
        nonlocal announced, blocked_code
        try:
            verify_binding(api, adapter)
        except ConfigurationError as exc:
            code = str(exc)
            if code not in RECOVERY_ACTIONS:
                raise
            if blocked_code != code:
                status("needs_verification" if code == "needs_native_queue_verification" else "needs_user", code)
            blocked_code, announced = code, False
            return False
        blocked_code = None
        return True

    while True:
        connected_at = time.monotonic()
        try:
            # Verify the actual registered destination on every connection, before
            # claiming any message. Envelope content cannot change this binding.
            allowed = can_claim()
            with api.subscribe() as events:
                # Subscribe first: task creation between these steps cannot be lost.
                if allowed and not announced:
                    status("ready", "subscribed_native_completion_unverified")
                    announced = True
                if allowed:
                    drain(api, adapter)
                for _ in events.changes():
                    # Verification registration/revocation and binding changes
                    # arrive on the real event stream; never poll or infer idle.
                    if can_claim():
                        if not announced:
                            status("ready", "subscribed_native_completion_unverified")
                            announced = True
                        drain(api, adapter)
        except ConfigurationError as exc:
            status("error", str(exc))
            return 2
        except HubError as exc:
            status("error", exc.code)
            if exc.status in (401, 403, 404) or exc.code in (
                "delivery_receipt_unconfirmed_reconcile_in_hub", "invalid_claimed_envelope",
            ):
                return 1
            # Network reconnection only. A fresh subscription precedes each inbox
            # scan; dispatching/uncertain messages are never returned by the Hub.
            if time.monotonic() - connected_at >= 20:
                backoff = 1.0
            sleep(backoff)
            backoff = min(30.0, backoff * 2)


def main(factory: Callable[[dict], Adapter]) -> int:
    parser = argparse.ArgumentParser(description="Independent AGI Hub endpoint client")
    parser.add_argument("--config", required=True, type=Path, help="private client.json path")
    args = parser.parse_args()
    try:
        config = load_config(args.config)
        adapter = factory(config)
        return consume(HubAPI(config), adapter)
    except ConfigurationError as exc:
        status("error", str(exc))
        return 2
    except KeyboardInterrupt:
        return 0
    except Exception:
        status("error", "client_stopped_preserve_claim_and_reconcile")
        return 1
