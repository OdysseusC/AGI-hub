"""Local-only protocol and cross-client compatibility verification."""

from __future__ import annotations

import contextlib
import importlib.util
import io
import json
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock, patch

CLIENTS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(CLIENTS))
from codex.adapter import CodexAdapter
from shared.notification import notification
from shared.runtime import HubAPI, consume, drain, load_config, verify_binding
from shared.transport import ConfigurationError, HubError, parse_address, post_json_once
from workbuddy.adapter import WorkBuddyAdapter
from tests.support import envelope, registered_binding, LocalHTTP, respond

spec = importlib.util.spec_from_file_location("agi_hub_grok_compatibility_adapter", CLIENTS / "grok-routine" / "adapter.py")
grok = importlib.util.module_from_spec(spec)
spec.loader.exec_module(grok)


class TransportTests(unittest.TestCase):
    def test_hub_rejects_remote_dns_credentials_and_extra_paths(self):
        for url in (
            "https://example.com", "http://localhost:47731", "http://192.168.1.1:47731",
            "http://secret@127.0.0.1:47731", "http://127.0.0.1:47731/v1",
            "http://127.0.0.1:47731?secret=value", "http://127.0.0.1:47731#x",
        ):
            with self.subTest(url=url), self.assertRaises(ConfigurationError):
                parse_address(url, hub=True)
        address = parse_address("http://[::1]:47731", hub=True)
        self.assertEqual(address.origin, "http://[::1]:47731")

    def test_native_redirect_is_not_followed_and_request_occurs_once(self):
        paths = []
        def callback(handler):
            paths.append(handler.path)
            handler.rfile.read(int(handler.headers.get("Content-Length", 0)))
            handler.send_response(307)
            handler.send_header("Location", "/must-not-be-called")
            handler.send_header("Content-Length", "0")
            handler.end_headers()
        with LocalHTTP(callback) as local:
            outcome, _ = post_json_once(local.base + "/trigger", envelope(), {"Authorization": "Bearer secret"}, 1)
        self.assertEqual(outcome, "uncertain")
        self.assertEqual(paths, ["/trigger"])

    def test_native_dropped_response_is_uncertain_without_retry(self):
        attempted = []
        def callback(handler):
            attempted.append(handler.rfile.read(int(handler.headers.get("Content-Length", 0))))
            handler.close_connection = True
        with LocalHTTP(callback) as local:
            outcome, detail = post_json_once(local.base, envelope(), {}, 1)
        self.assertEqual(outcome, "uncertain")
        self.assertEqual(len(attempted), 1)
        self.assertNotIn("private content", detail)

    def test_private_config_rejects_unknown_fields_and_duplicate_keys(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "client.json"
            path.write_text('{"base_url":"http://127.0.0.1:47731","endpoint":"one","endpoint":"two","endpoint_token":"secret"}')
            with self.assertRaises(ConfigurationError):
                load_config(path)
            path.write_text(json.dumps({"base_url": "http://127.0.0.1:47731", "endpoint": "one", "endpoint_token": "secret", "admin_token": "not allowed"}))
            with self.assertRaises(ConfigurationError):
                load_config(path)


class RuntimeTests(unittest.TestCase):
    def test_subscribe_before_claim_and_sse_keepalive_does_not_poll(self):
        sequence = []
        receipts = []
        def callback(handler):
            sequence.append(handler.path.split("?")[0])
            self.assertEqual(handler.headers["Authorization"], "Bearer scoped-test")
            self.assertEqual(handler.headers["Origin"], local.base)
            self.assertEqual(handler.headers["Host"], local.base.removeprefix("http://"))
            body = handler.rfile.read(int(handler.headers.get("Content-Length", 0)))
            if handler.path.startswith("/v1/endpoint"):
                respond(handler, registered_binding())
            elif handler.path.startswith("/v1/events"):
                # Short stream; business event is separate from keepalive comment.
                stream = b": connection keepalive\n\nevent: change\ndata: {}\n\n"
                handler.send_response(200)
                handler.send_header("Content-Type", "text/event-stream")
                handler.send_header("Content-Length", str(len(stream)))
                handler.end_headers()
                handler.wfile.write(stream)
            elif handler.path.startswith("/v1/inbox"):
                respond(handler, envelope() if sequence.count("/v1/inbox") == 1 else None)
            elif handler.path.startswith("/v1/delivery"):
                receipts.append(json.loads(body))
                respond(handler, {"ok": True})
        adapter = Mock()
        adapter.deliver.return_value = ("delivered", "transport only")
        with LocalHTTP(callback) as local:
            api = HubAPI({"base_url": local.base, "endpoint": "endpoint-test", "endpoint_token": "scoped-test"})
            verify_binding(api, adapter)
            with api.subscribe() as events:
                drain(api, adapter)
                self.assertEqual(sequence[:4], ["/v1/endpoint", "/v1/events", "/v1/inbox", "/v1/delivery"])
                iterator = events.changes()
                next(iterator)
                self.assertEqual(sequence.count("/v1/inbox"), 2)
        adapter.deliver.assert_called_once()
        adapter.validate_binding.assert_called_once_with(registered_binding()["endpoint"])
        self.assertEqual(receipts, [{"message_id": "message-test", "outcome": "delivered", "detail": "transport only"}])

    def test_receipt_failure_stops_without_repeating_native_dispatch(self):
        api = Mock()
        api.endpoint = "endpoint-test"
        api.post.side_effect = [envelope(), HubError("hub_request_unconfirmed")]
        adapter = Mock()
        adapter.deliver.return_value = ("delivered", "transport accepted")
        with self.assertRaisesRegex(HubError, "delivery_receipt_unconfirmed"):
            drain(api, adapter)
        adapter.deliver.assert_called_once()
        self.assertEqual(api.post.call_count, 2)

    def test_unknown_exception_reports_uncertain_once(self):
        api = Mock()
        api.endpoint = "endpoint-test"
        api.post.side_effect = [envelope(), {"ok": True}, None]
        adapter = Mock()
        adapter.deliver.side_effect = RuntimeError("SECRET SHOULD NEVER BE PRINTED")
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            drain(api, adapter)
        receipt = api.post.call_args_list[1].args[1]
        self.assertEqual(receipt["outcome"], "uncertain")
        self.assertNotIn("SECRET", output.getvalue() + receipt["detail"])
        adapter.deliver.assert_called_once()

    def test_network_reconnect_backoff_and_fatal_auth_do_not_spin(self):
        api = Mock()
        api.endpoint = "endpoint-test"
        api.get.return_value = registered_binding()
        api.subscribe.side_effect = [HubError("offline"), HubError("offline"), HubError("auth", 401)]
        sleeps = []
        with contextlib.redirect_stdout(io.StringIO()):
            code = consume(api, Mock(), sleep=sleeps.append)
        self.assertEqual(code, 1)
        self.assertEqual(sleeps, [1.0, 2.0])
        api.post.assert_not_called()

    def test_bad_protocol_disabled_or_foreign_endpoint_stops_before_subscription(self):
        bad_bindings = []
        for version in (2, True, None):
            record = registered_binding()
            record["protocol_version"] = version
            bad_bindings.append(record)
        disabled = registered_binding()
        disabled["endpoint"]["enabled"] = False
        bad_bindings.append(disabled)
        foreign = registered_binding()
        foreign["endpoint"]["id"] = "other-endpoint"
        bad_bindings.append(foreign)
        for record in bad_bindings:
            api, adapter = Mock(), Mock()
            api.endpoint = "endpoint-test"
            api.get.return_value = record
            with self.subTest(record=record), contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(consume(api, adapter, sleep=Mock()), 2)
            api.subscribe.assert_not_called()
            api.post.assert_not_called()
            adapter.deliver.assert_not_called()

    def test_reconnect_revalidates_binding_and_preserves_unknown_session(self):
        class DisconnectedEvents:
            def __enter__(self):
                return self
            def __exit__(self, *_):
                pass
            def changes(self):
                raise HubError("event_stream_disconnected")
                yield
        initial = registered_binding(state="unknown")
        disabled = registered_binding()
        disabled["endpoint"]["enabled"] = False
        api, adapter = Mock(), Mock()
        api.endpoint = "endpoint-test"
        api.get.side_effect = [initial, disabled]
        api.subscribe.return_value = DisconnectedEvents()
        api.post.return_value = None
        sleeps = []
        with contextlib.redirect_stdout(io.StringIO()):
            code = consume(api, adapter, sleep=sleeps.append)
        self.assertEqual(code, 2)
        self.assertEqual(api.get.call_count, 2)
        self.assertEqual(sleeps, [1.0])
        api.subscribe.assert_called_once()
        api.post.assert_called_once_with("inbox")
        self.assertEqual(initial["session"]["state"], "unknown")
        adapter.deliver.assert_not_called()

    def test_native_binding_mismatch_never_claims_or_dispatches(self):
        with patch("codex.adapter.shutil.which", return_value="/fake/codex"):
            adapter = CodexAdapter({"endpoint": "endpoint-test", "native": {"thread_id": "actual-thread"}})
        api = Mock()
        api.endpoint = "endpoint-test"
        api.get.return_value = registered_binding(session_id="different-thread")
        with contextlib.redirect_stdout(io.StringIO()), patch("codex.adapter.subprocess.Popen") as launch:
            self.assertEqual(consume(api, adapter, sleep=Mock()), 2)
        api.subscribe.assert_not_called()
        api.post.assert_not_called()
        launch.assert_not_called()

    def test_result_notification_never_requests_reexecution(self):
        text = notification("endpoint-test", envelope(kind="result"))
        self.assertIn("kind=result, not a new task", text)
        self.assertIn("Do not repeat", text)
        self.assertNotIn("Example:", text)
        self.assertIn('"kind":"result"', text)

    def test_execution_hint_is_per_task_and_not_the_fixed_conversation(self):
        first = envelope()
        second = dict(first, task_id="another-task")
        def hint(value):
            text = notification("endpoint-test", value)
            line = next(line for line in text.splitlines() if line.startswith('{"event_id":'))
            return json.loads(line)["run_id"]
        self.assertEqual(hint(first), hint(first))
        self.assertNotEqual(hint(first), hint(second))
        self.assertTrue(hint(first).startswith("hubrun-"))

    def test_busy_result_verification_requires_boolean(self):
        api = Mock(endpoint="endpoint-test")
        binding = registered_binding()
        api.get.return_value = binding
        adapter = Mock()
        for invalid in ("true", 1, None):
            binding["endpoint"]["result_queue_while_busy_verified"] = invalid
            with self.assertRaisesRegex(ConfigurationError, "invalid_hub_busy_result_verification"):
                verify_binding(api, adapter)
        adapter.validate_binding.assert_not_called()


class CrossClientCompatibilityTests(unittest.TestCase):
    def test_each_adapter_requires_registered_native_session_and_driver(self):
        with patch("codex.adapter.shutil.which", return_value="/fake/codex"):
            codex = CodexAdapter({"endpoint": "endpoint-test", "native": {"thread_id": "thread-test"}})
        workbuddy = WorkBuddyAdapter({"endpoint": "endpoint-test", "native": {"access_token": "test-token", "oauth_scopes": ["user.localassistant.invokable"]}})
        routine = grok.GrokRoutineAdapter({"endpoint": "endpoint-test", "native": {"routine_id": "routine-test", "url": "https://example.invalid/routine", "body": {"message": "{{notification}}"}}})
        for adapter, driver, session_id in (
            (codex, "codex", "thread-test"), (workbuddy, "workbuddy", "localassistant"),
            (routine, "grok-routine", "routine-test"),
        ):
            with self.subTest(driver=driver):
                adapter.validate_binding(registered_binding(driver=driver, session_id=session_id)["endpoint"])
                for wrong in (
                    registered_binding(driver="other-driver", session_id=session_id),
                    registered_binding(driver=driver, session_id="other-native-session"),
                ):
                    with self.assertRaises(ConfigurationError):
                        adapter.validate_binding(wrong["endpoint"])
                unsupported = registered_binding(driver=driver, session_id=session_id)
                unsupported["endpoint"]["modes"] = ["queue", "steer"]
                with self.assertRaises(ConfigurationError):
                    adapter.validate_binding(unsupported["endpoint"])

    def test_grok_keeps_http_transport_evidence_separate_from_workbuddy_receipt(self):
        def callback(handler):
            handler.rfile.read(int(handler.headers.get("Content-Length", 0)))
            respond(handler, {"code": 10001})
        with LocalHTTP(callback) as local:
            outcome, detail = post_json_once(local.base, envelope(), {}, 1)
        self.assertEqual(outcome, "delivered")
        self.assertIn("HTTP transport", detail)


def load_tests(loader, tests, pattern):
    suite = unittest.TestSuite([tests])
    for assistant in ("codex", "workbuddy", "grok-routine"):
        path = CLIENTS / assistant / "test_adapter.py"
        name = "agi_hub_" + assistant.replace("-", "_") + "_adapter_tests"
        spec = importlib.util.spec_from_file_location(name, path)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        suite.addTests(loader.loadTestsFromModule(module))
    return suite


if __name__ == "__main__":
    unittest.main()
