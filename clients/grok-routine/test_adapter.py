"""Local-only adapter tests; no messages are sent to real assistants."""

import sys
import unittest
import hashlib
import json
import tempfile
import threading
import time
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

CLIENTS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(CLIENTS))
import importlib.util
from unittest.mock import patch
from shared.transport import ConfigurationError
from tests.support import LocalHTTP, envelope, respond

spec = importlib.util.spec_from_file_location("agi_hub_grok_native_tests_adapter", Path(__file__).with_name("adapter.py"))
grok = importlib.util.module_from_spec(spec)
spec.loader.exec_module(grok)


class AdapterTests(unittest.TestCase):
    def test_grok_expansion_is_single_pass_and_requires_full_envelope(self):
        config = {"endpoint": "endpoint-test", "native": {"routine_id": "routine-test", "url": "https://example.invalid/routine", "body": {"prompt": "{{content}}", "record": "{{envelope}}"}}}
        adapter = grok.GrokRoutineAdapter(config)
        with patch.object(grok, "post_json_once", return_value=("delivered", "transport")) as send:
            adapter.deliver(envelope())
        body = send.call_args.args[1]
        self.assertIn("{{message_id}}", body["prompt"])
        self.assertEqual(body["record"], envelope())
        config["native"]["body"] = {"prompt": "{{content}}"}
        with self.assertRaisesRegex(ConfigurationError, "complete_envelope"):
            grok.GrokRoutineAdapter(config)


class FileWebhookTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.inbox = Path(self.temp.name).resolve()

    def config(self, url="https://example.invalid/private-webhook", **native):
        return {"endpoint": "endpoint-test", "native": {
            "transport": "file-webhook", "routine_id": "routine-test", "url": url,
            "headers": {"Authorization": "secret-test-token"}, "inbox_dir": str(self.inbox),
            **native,
        }}

    def adapter(self, config=None):
        self.assertTrue(hasattr(grok, "create_adapter"), "file-webhook transport factory is required")
        return grok.create_adapter(config or self.config())

    def file_id(self, item):
        return "hub-" + hashlib.sha256(item["message_id"].encode("utf-8")).hexdigest()

    def test_publishes_complete_packet_before_id_only_webhook(self):
        item = envelope()
        item["message_id"] = "task:中文/windows?invalid"
        seen = []
        def receive(handler):
            body = json.loads(handler.rfile.read(int(handler.headers["Content-Length"])))
            packet = json.loads((self.inbox / (body["id"] + ".json")).read_text(encoding="utf-8"))
            seen.append((body, handler.headers["Idempotency-Key"], packet))
            respond(handler, {"accepted": True})
        with LocalHTTP(receive) as server:
            adapter = self.adapter(self.config(server.base))
            self.assertEqual(adapter.deliver(item)[0], "delivered")
        file_id = self.file_id(item)
        body, key, packet = seen[0]
        self.assertEqual(body, {"id": file_id})
        self.assertEqual(key, file_id)
        self.assertEqual(packet["protocol"], "agi-hub-grok-file-v1")
        self.assertEqual(packet["id"], file_id)
        self.assertEqual(packet["endpoint"], "endpoint-test")
        self.assertEqual(packet["routine_id"], "routine-test")
        self.assertEqual(packet["envelope"], item)
        self.assertIn(item["content"], packet["notification"])
        self.assertNotIn("secret-test-token", json.dumps(packet))
        self.assertNotIn(server.base, json.dumps(packet))
        self.assertFalse(list(self.inbox.glob("*.tmp")))

    def test_duplicate_and_restart_reuse_evidence_without_notify(self):
        calls = []
        def receive(handler):
            calls.append(handler.path)
            respond(handler, {})
        with LocalHTTP(receive) as server:
            config = self.config(server.base)
            adapter = self.adapter(config)
            original = adapter.deliver(envelope())
            self.assertEqual(original[0], "delivered")
            self.assertEqual(adapter.deliver(envelope()), original)
            self.assertEqual(self.adapter(config).deliver(envelope()), original)
        self.assertEqual(len(calls), 1)

    def test_conflicting_envelope_or_missing_receipt_never_notify_again(self):
        calls = []
        def receive(handler):
            calls.append(handler.path)
            respond(handler, {})
        with LocalHTTP(receive) as server:
            adapter = self.adapter(self.config(server.base))
            item = envelope()
            self.assertEqual(adapter.deliver(item)[0], "delivered")
            before = (self.inbox / (self.file_id(item) + ".json")).read_bytes()
            altered = {**item, "content": "different"}
            self.assertEqual(adapter.deliver(altered)[0], "uncertain")
            self.assertEqual((self.inbox / (self.file_id(item) + ".json")).read_bytes(), before)
            (self.inbox / ".claims" / self.file_id(item) / "delivery.json").unlink()
            self.assertEqual(adapter.deliver(item)[0], "uncertain")
        self.assertEqual(len(calls), 1)

    def test_concurrent_delivery_has_only_one_webhook(self):
        started, release = threading.Event(), threading.Event()
        calls = []
        def receive(handler):
            calls.append(handler.path)
            started.set()
            release.wait(5)
            respond(handler, {})
        with LocalHTTP(receive) as server, ThreadPoolExecutor(max_workers=2) as executor:
            config = self.config(server.base)
            first = executor.submit(self.adapter(config).deliver, envelope())
            self.assertTrue(started.wait(5))
            try:
                self.assertEqual(self.adapter(config).deliver(envelope())[0], "uncertain")
            finally:
                release.set()
            self.assertEqual(first.result(timeout=5)[0], "delivered")
        self.assertEqual(len(calls), 1)

    def test_existing_packet_or_incomplete_claim_is_not_overwritten(self):
        item = envelope()
        packet = self.inbox / (self.file_id(item) + ".json")
        packet.write_text('{"existing":true}', encoding="utf-8")
        adapter = self.adapter()
        with patch.object(grok, "_request_once", side_effect=AssertionError("must not notify")):
            self.assertEqual(adapter.deliver(item)[0], "uncertain")
            self.assertEqual(packet.read_text(encoding="utf-8"), '{"existing":true}')
            packet.unlink()
            self.assertEqual(adapter.deliver(item)[0], "uncertain")

    def test_http_status_is_strict_and_uncertain_or_failed_is_never_retried(self):
        for status, expected in [(200, "delivered"), (202, "uncertain"), (204, "uncertain"),
                                 (302, "uncertain"), (401, "failed"), (403, "failed"),
                                 (429, "uncertain"), (500, "uncertain")]:
            with self.subTest(status=status):
                calls = []
                def receive(handler):
                    calls.append(handler.path)
                    respond(handler, {"secret": "do-not-record-response"}, status=status)
                with LocalHTTP(receive) as server:
                    config = self.config(server.base)
                    item = {**envelope(), "message_id": f"task:{status}"}
                    result = self.adapter(config).deliver(item)
                    self.assertEqual(result[0], expected)
                    self.assertNotIn("do-not-record-response", result[1])
                    self.assertEqual(self.adapter(config).deliver(item), result)
                self.assertEqual(len(calls), 1)

    def test_lost_response_is_uncertain_without_retry_or_secret_leak(self):
        adapter = self.adapter()
        with patch.object(grok, "_request_once", side_effect=OSError("private-url-secret")) as send:
            result = adapter.deliver(envelope())
            self.assertEqual(result[0], "uncertain")
            self.assertNotIn("private-url-secret", result[1])
            self.assertEqual(self.adapter().deliver(envelope()), result)
            self.assertEqual(send.call_count, 1)

    def test_slow_response_headers_cannot_extend_request_deadline(self):
        def receive(handler):
            try:
                handler.connection.sendall(b"HTTP/1.1 200 OK\r\nX-Slow: ")
                for _ in range(25):
                    handler.connection.sendall(b"a")
                    time.sleep(0.1)
                handler.connection.sendall(b"\r\nContent-Length: 0\r\n\r\n")
            except OSError:
                pass
        with LocalHTTP(receive) as server:
            adapter = self.adapter(self.config(server.base, timeout_seconds=1))
            start = time.monotonic()
            result = adapter.deliver(envelope())
            elapsed = time.monotonic() - start
        self.assertEqual(result[0], "uncertain")
        self.assertLess(elapsed, 2.3)

    def test_deadline_does_not_allow_http_client_to_reconnect_and_send_late(self):
        calls = []
        def receive(handler):
            calls.append(handler.path)
            respond(handler, {})
        original_connection = grok.connection
        def delayed_connection(address, timeout):
            conn = original_connection(address, timeout)
            original_request = conn.request
            def delayed_request(*args, **kwargs):
                time.sleep(1.2)
                return original_request(*args, **kwargs)
            conn.request = delayed_request
            return conn
        with LocalHTTP(receive) as server, patch.object(grok, "connection", side_effect=delayed_connection):
            result = self.adapter(self.config(server.base, timeout_seconds=1)).deliver(envelope())
        self.assertEqual(result[0], "uncertain")
        self.assertEqual(calls, [])

    def test_publication_failure_never_sends(self):
        adapter = self.adapter()
        with patch.object(grok, "_request_once", side_effect=AssertionError("must not notify")):
            with patch.object(Path, "rename", side_effect=OSError("private-path")):
                result = adapter.deliver(envelope())
            self.assertEqual(result[0], "uncertain")
            self.assertNotIn("private-path", result[1])
            self.assertEqual(adapter.deliver(envelope())[0], "uncertain")

    def test_result_packet_retains_result_semantics(self):
        item = envelope(kind="result")
        with LocalHTTP(lambda handler: respond(handler, {})) as server:
            self.assertEqual(self.adapter(self.config(server.base)).deliver(item)[0], "delivered")
        packet = json.loads((self.inbox / (self.file_id(item) + ".json")).read_text(encoding="utf-8"))
        self.assertEqual(packet["envelope"]["kind"], "result")
        self.assertIn("not a new task", packet["notification"])

    def test_strict_config_and_binding(self):
        invalid = [
            {"inbox_dir": "relative/inbox"}, {"inbox_dir": str(self.inbox / "missing")},
            {"body": {"prompt": "{{notification}}"}}, {"transport": "unknown"},
            {"url": "http://external.invalid"}, {"timeout_seconds": float("nan")},
            {"timeout_seconds": True}, {"routine_id": "bad\nID"},
            {"headers": {"Idempotency-Key": "untrusted"}},
            {"headers": {"Host": "external.invalid"}}, {"command": ["anything"]},
        ]
        for values in invalid:
            with self.subTest(values=values), self.assertRaises(ConfigurationError):
                self.adapter(self.config(**values))
        adapter = self.adapter()
        adapter.validate_binding({"driver": "grok-routine", "session_id": "routine-test", "modes": ["queue"]})
        for binding in [
            {"driver": "grok-routine", "session_id": "other", "modes": ["queue"]},
            {"driver": "grok-routine", "session_id": "routine-test", "modes": ["queue", "steer"]},
        ]:
            with self.assertRaises(ConfigurationError):
                adapter.validate_binding(binding)
        self.assertEqual(adapter.deliver(envelope(mode="steer"))[0], "failed")

    def test_symlink_inbox_or_packet_is_rejected(self):
        target = self.inbox / "real"
        target.mkdir()
        link = self.inbox / "link"
        try:
            link.symlink_to(target, target_is_directory=True)
        except (OSError, NotImplementedError):
            self.skipTest("platform does not allow creating symlinks")
        with self.assertRaises(ConfigurationError):
            self.adapter(self.config(inbox_dir=str(link)))
        item = envelope()
        packet = self.inbox / (self.file_id(item) + ".json")
        secret = target / "secret.json"
        secret.write_text("secret", encoding="utf-8")
        packet.symlink_to(secret)
        with patch.object(grok, "_request_once", side_effect=AssertionError("must not notify")):
            self.assertEqual(self.adapter().deliver(item)[0], "uncertain")
        self.assertEqual(secret.read_text(encoding="utf-8"), "secret")

    def test_factory_preserves_legacy_template_transport(self):
        config = {"endpoint": "endpoint-test", "native": {
            "routine_id": "routine-test", "url": "https://example.invalid/routine",
            "body": {"record": "{{envelope}}"},
        }}
        self.assertIsInstance(self.adapter(config), grok.GrokRoutineAdapter)


if __name__ == "__main__":
    unittest.main()
