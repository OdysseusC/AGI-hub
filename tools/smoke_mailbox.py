"""Exercise a real Hub mailbox with synthetic native adapters and zero models."""

from __future__ import annotations

import argparse
import contextlib
import json
import socket
import subprocess
import sys
import tempfile
import time
from datetime import datetime, timedelta, timezone
from pathlib import Path
from urllib.request import ProxyHandler, Request, build_opener

REPO = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(REPO / "clients"))
from shared.runtime import HubAPI, drain, verify_binding
from shared.transport import HubError


def run(binary: Path) -> None:
    with tempfile.TemporaryDirectory(prefix="agi-hub-mailbox-smoke-") as temporary:
        root = Path(temporary).resolve()
        data = root / "data"
        subprocess.run([str(binary), "init", "--data", str(data)], check=True, stdout=subprocess.DEVNULL)
        cfg_path = data / "config.json"
        cfg = json.loads(cfg_path.read_text())
        with socket.socket() as reservation:
            reservation.bind(("127.0.0.1", 0))
            cfg["listen"] = f"127.0.0.1:{reservation.getsockname()[1]}"
        names = ("mail-a", "mail-b", "mail-c", "mail-d", "mail-outsider")
        cfg.update(leader="", workspaces={})
        cfg["bindings"] = [{
            "endpoint": {
                "id": name, "device_id": cfg["device_id"], "session_id": name + "-native-chat",
                "enabled": True, "idle_wake_verified": True, "modes": ["queue"],
                "driver": "synthetic-mail-test", "mailbox_protocol_version": 1,
            },
            "driver": {"type": "external"},
            "visible_peers": [other for other in names if other != name],
            "allowed_peers": [other for other in names if other != name],
        } for name in names]
        cfg_path.write_text(json.dumps(cfg))
        apis = {}
        for name in names:
            path = root / name / "client.json"
            subprocess.run([str(binary), "client-config", "--data", str(data), "--endpoint", name,
                            "--file", str(path)], check=True, stdout=subprocess.DEVNULL)
            apis[name] = HubAPI(json.loads(path.read_text()))

        log = (root / "hub.log").open("w+")
        process = subprocess.Popen([str(binary), "serve", "--data", str(data)], stdout=log, stderr=log)
        opener = build_opener(ProxyHandler({}))

        def admin(method: str, route: str, body: object = None):
            address = "http://" + cfg["listen"]
            request = Request(address + route, method=method,
                              data=None if body is None else json.dumps(body).encode(),
                              headers={"Authorization": "Bearer " + cfg["admin_token"],
                                       "Content-Type": "application/json", "Origin": address})
            with opener.open(request, timeout=5) as response:
                return json.load(response)

        try:
            # Bounded startup readiness only; business messages use the real
            # subscription/inbox protocol, never a background polling loop.
            start_by = time.monotonic() + 10
            while True:
                try:
                    admin("GET", "/v1/state")
                    break
                except OSError:
                    assert process.poll() is None, "synthetic mailbox Hub failed to start"
                    assert time.monotonic() < start_by, "synthetic mailbox Hub startup timed out"
                    time.sleep(0.025)
            for name in names:
                admin("POST", "/v1/session", {"endpoint": name, "state": "idle"})
            original_sessions = admin("GET", "/v1/state")["sessions"]
            api_a, api_b, api_c, api_d = (apis[name] for name in names[:4])
            native_calls: dict[str, list[str]] = {name: [] for name in names}
            received: dict[str, list[dict]] = {name: [] for name in names}
            published: list[dict] = []

            def publish(api: HubAPI, payload: dict) -> dict:
                value = api.post("mail/send", payload)
                assert value["mailbox"] is True and not value.get("task_id"), "mail became a task"
                assert value["recipient"] == payload["recipient"], "Hub changed explicit recipient"
                published.append(value)
                return value

            correlation = ""
            root_origin = None

            class SyntheticAdapter:
                mailbox_protocol_version = 1

                def __init__(self, name: str):
                    self.name = name

                def validate_binding(self, binding: dict) -> None:
                    assert binding["id"] == self.name
                    assert binding["driver"] == "synthetic-mail-test"
                    assert binding["session_id"] == self.name + "-native-chat"

                def deliver(self, message: dict) -> tuple[str, str]:
                    assert message["recipient"] == self.name, "foreign mailbox consumed"
                    assert message["kind"] != "status", "status woke a synthetic native adapter"
                    native_calls[self.name].append(message["message_id"])
                    received[self.name].append(message)
                    if message["message_id"] == "uncertain-first":
                        # A second independent consumer cannot claim the next
                        # pending message while this actual delivery is in flight.
                        assert api_b.post("mail/inbox") is None, "concurrent mailbox dispatch escaped reservation"
                        return "uncertain", "synthetic ambiguous native delivery; reconcile before retrying"
                    assert message["correlation_id"] == correlation
                    assert message["root_origin"] == root_origin
                    if self.name == "mail-b":
                        publish(api_b, {"id": "b-asks-d", "recipient": "mail-d", "kind": "request",
                                        "content": "B-to-D private detail", "correlation_id": correlation,
                                        "reply_to": message["message_id"]})
                    elif self.name == "mail-c":
                        publish(api_c, {"id": "c-result-a", "recipient": "mail-a", "kind": "result",
                                        "content": "C independently checked its part", "correlation_id": correlation})
                    elif self.name == "mail-d":
                        # Deliberately address the human's entry A, including for
                        # B's request. reply_to is a hint, not a forced B route.
                        publish(api_d, {"id": "d-reply-" + message["message_id"], "recipient": "mail-a",
                                        "kind": "reply", "content": "D verified the requested detail",
                                        "correlation_id": correlation, "reply_to": message["message_id"]})
                    return "delivered", "synthetic native adapter returned; no model was started"

            adapters = {name: SyntheticAdapter(name) for name in names}
            for name in names:
                verify_binding(apis[name], adapters[name])
                assert apis[name].mailbox is True

            with contextlib.ExitStack() as stack:
                for api in apis.values():
                    stack.enter_context(api.subscribe())
                root_payload = {
                    "id": "a-root-b", "recipient": "mail-b", "kind": "request",
                    "content": "B checks one bounded part of the original human request",
                    "origin_ref": "synthetic-original-human-input",
                    "deadline": (datetime.now(timezone.utc) + timedelta(minutes=5)).isoformat(timespec="seconds"),
                }
                first = publish(api_a, root_payload)
                correlation, root_origin = first["correlation_id"], first["root_origin"]
                assert root_origin == {"endpoint": "mail-a", "device_id": cfg["device_id"],
                                       "session_id": "mail-a-native-chat", "request_ref": root_payload["origin_ref"]}
                for recipient in ("mail-c", "mail-d"):
                    publish(api_a, {"id": "a-root-" + recipient[-1], "recipient": recipient, "kind": "request",
                                    "content": "Independent assistance, same human request", "correlation_id": correlation})
                drain(api_b, adapters["mail-b"])
                drain(api_c, adapters["mail-c"])
                drain(api_d, adapters["mail-d"])
                # Synthetic B chooses its own final destination explicitly. This
                # send is test adapter behavior, never generated by a Report.
                publish(api_b, {"id": "b-result-a", "recipient": "mail-a", "kind": "result",
                                "content": "B completed its bounded check", "correlation_id": correlation})
                publish(api_b, {"id": "b-status-a", "recipient": "mail-a", "kind": "status",
                                "content": "B saved a concise progress observation", "correlation_id": correlation})
                drain(api_a, adapters["mail-a"])
                assert len(native_calls["mail-a"]) == 4, "status caused a model wake or result was lost"
                assert len(native_calls["mail-b"]) == 1 and len(native_calls["mail-c"]) == 1
                assert len(native_calls["mail-d"]) == 2
                assert "d-reply-b-asks-d" in native_calls["mail-a"]
                assert not any(value["sender"] == "mail-d" and value["recipient"] == "mail-b" for value in published)
                assert len(published) == 9

                before_duplicate = {name: list(calls) for name, calls in native_calls.items()}
                repeated = api_a.post("mail/send", root_payload)
                assert repeated["message_id"] == first["message_id"]
                for name in names:
                    drain(apis[name], adapters[name])
                assert native_calls == before_duplicate, "duplicate ID caused another native invocation"

                view_a = api_a.get("mail/state", {"correlation_id": correlation})
                view_b = api_b.get("mail/state", {"correlation_id": correlation})
                assert view_a["max_messages"] == 16 and view_a["used_messages"] == 9
                assert view_b["used_messages"] == 9, "conversation budget was duplicated per sender"
                assert view_a["root_origin"] == root_origin == view_b["root_origin"]
                assert "b-asks-d" not in {row["message_id"] for row in view_a["messages"]}
                assert "a-root-c" not in {row["message_id"] for row in view_b["messages"]}
                assert all("content" not in row for row in view_a["messages"] + view_b["messages"] if row["kind"] != "status")
                assert next(row for row in view_a["messages"] if row["message_id"] == "b-status-a")["content"] == "B saved a concise progress observation"
                assert "B-to-D private detail" not in json.dumps(view_a)
                for action in (
                    lambda: apis["mail-outsider"].get("mail/state", {"correlation_id": correlation}),
                    lambda: api_a.get("state"),
                    lambda: api_b.post("mail/delivery", {"message_id": "c-result-a", "outcome": "delivered", "detail": "foreign"}),
                    lambda: api_b.post("mail/send", {"id": "cannot-expand-budget", "recipient": "mail-d", "kind": "request",
                                                     "content": "should be rejected", "correlation_id": correlation, "max_messages": 64}),
                ):
                    try:
                        action()
                    except HubError:
                        pass
                    else:
                        raise AssertionError("mail scope, privacy, or shared budget expansion was accepted")

                uncertain = publish(api_a, {"id": "uncertain-first", "recipient": "mail-b", "kind": "request",
                                            "content": "Synthetic uncertainty test", "origin_ref": "synthetic-second-human-input"})
                publish(api_a, {"id": "blocked-after-uncertain", "recipient": "mail-b", "kind": "request",
                                "content": "Must remain pending", "correlation_id": uncertain["correlation_id"]})
                drain(api_b, adapters["mail-b"])
                uncertain_count = len(native_calls["mail-b"])
                drain(api_b, adapters["mail-b"])
                assert len(native_calls["mail-b"]) == uncertain_count
                uncertain_view = api_a.get("mail/state", {"correlation_id": uncertain["correlation_id"]})
                deliveries = {row["message_id"]: row["delivery"] for row in uncertain_view["messages"]}
                assert deliveries == {"uncertain-first": "uncertain", "blocked-after-uncertain": "pending"}
                assert uncertain_view["used_messages"] == 2

            snapshot = admin("GET", "/v1/state")
            assert snapshot["tasks"] == {} and snapshot["event_ids"] == {}, "mail created task/report state"
            assert snapshot["sessions"] == original_sessions, "mail inferred a business session state"
            assert len(snapshot["messages"]) == 11, "Hub generated an unsolicited business message"
            assert snapshot["resource_owners"] == {}
            assert all(value["root_origin"] == root_origin for value in published[:9])
            print(json.dumps({
                "real_hub_mailbox": True, "explicit_messages": 11,
                "shared_conversation_messages": 9, "independent_recipients": 3,
                "direct_d_to_original_a": True, "forced_parent_chain": False,
                "duplicate_native_dispatches": 0, "silent_status_messages": 1,
                "uncertain_blocks_next_dispatch": True, "private_state_scopes": True,
                "synthetic_native_dispatches": sum(map(len, native_calls.values())),
                "generated_tasks": 0, "automatic_business_replies": 0, "native_model_calls": 0,
            }))
        finally:
            if process.poll() is None:
                process.terminate()
            try:
                process.communicate(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.communicate(timeout=5)
            log.close()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--hub", required=True, type=Path, help="built Hub executable for this platform")
    run(parser.parse_args().hub.resolve(strict=True))
