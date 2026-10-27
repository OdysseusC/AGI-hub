"""Local-only adapter tests; no messages are sent to real assistants."""

import copy
import json
import sys
import unittest
from pathlib import Path

CLIENTS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(CLIENTS))
import importlib.util
from unittest.mock import Mock, patch
from shared.runtime import drain, verify_binding
from shared.transport import ConfigurationError, HubError
from tests.support import LocalHTTP, envelope, registered_binding, respond
from tests.test_mail_clients import mail_envelope

spec = importlib.util.spec_from_file_location("agi_hub_grok_native_tests_adapter", Path(__file__).with_name("adapter.py"))
grok = importlib.util.module_from_spec(spec)
spec.loader.exec_module(grok)


def adapter_for(url="https://example.invalid/routine", *, body=None, headers=None):
    return grok.GrokRoutineAdapter({"endpoint": "endpoint-test", "native": {
        "routine_id": "routine-test", "url": url,
        "body": {"prompt": "{{notification}}"} if body is None else body,
        "headers": {} if headers is None else headers,
    }})


class AdapterTests(unittest.TestCase):
    def test_mailbox_formats_preserve_full_message_and_literal_placeholders(self):
        message = dict(mail_envelope(), task_id="", content="Literal {{task_id}} {{message_id}} {{notification}}")
        before = copy.deepcopy(message)
        body = {"notification": "{{notification}}", "record": "{{envelope}}", "json": "{{envelope_json}}",
                "content": "{{content}}", "id": "{{message_id}}", "constant": "no placeholder"}
        adapter = adapter_for(body=body)
        with patch.object(grok, "post_json_once", return_value=("delivered", "transport")) as send:
            self.assertEqual(adapter.deliver(message)[0], "delivered")
        transmitted = send.call_args.args[1]
        self.assertEqual(transmitted["record"], before)
        self.assertEqual(json.loads(transmitted["json"]), before)
        self.assertEqual(transmitted["content"], before["content"])
        self.assertEqual(transmitted["id"], before["message_id"])
        self.assertEqual(transmitted["constant"], "no placeholder")
        self.assertIn(before["content"], transmitted["notification"])
        self.assertEqual(message, before)

    def test_embedded_envelope_object_is_not_sent_as_a_partial_handoff(self):
        adapter = adapter_for(body={"prompt": "{{notification}}", "record": "prefix {{envelope}}"})
        with patch.object(grok, "post_json_once") as send:
            self.assertEqual(adapter.deliver(mail_envelope())[0], "failed")
        send.assert_not_called()

    def test_routine_driver_and_queue_binding_are_checked_before_claim(self):
        adapter = adapter_for()
        for changes in ({"driver": "other"}, {"session_id": "another-routine"}, {"modes": ["queue", "steer"]}, {"modes": []}):
            api = Mock(endpoint="endpoint-test")
            record = registered_binding(driver="grok-routine", session_id="routine-test")
            record["endpoint"].update(mailbox_protocol_version=1, **changes)
            api.get.return_value = record
            with self.subTest(changes=changes), self.assertRaises(ConfigurationError):
                verify_binding(api, adapter)
            api.post.assert_not_called()

    def test_message_metadata_cannot_change_fixed_routine_url_or_headers(self):
        requests = []
        def receive(handler):
            requests.append((handler.path, handler.headers["X-Routine-Target"], json.loads(handler.rfile.read(int(handler.headers["Content-Length"])))))
            respond(handler, {}, status=202)
        with LocalHTTP(receive) as local:
            adapter = adapter_for(local.base + "/fixed-routine", body={"record": "{{envelope}}"}, headers={"X-Routine-Target": "configured"})
            message = dict(mail_envelope(), url=local.base + "/must-not-use", routine_id="other-routine",
                           headers={"X-Routine-Target": "untrusted"})
            self.assertEqual(adapter.deliver(message)[0], "delivered")
        self.assertEqual(len(requests), 1)
        self.assertEqual(requests[0][:2], ("/fixed-routine", "configured"))
        self.assertEqual(requests[0][2]["record"], message)
        self.assertEqual(adapter.routine_id, "routine-test")

    def test_202_is_only_delivery_for_each_mail_kind_and_status_never_wakes(self):
        requests, calls = [], []
        pending = [dict(mail_envelope(kind), message_id="mail-" + kind) for kind in ("request", "reply", "result", "status")]
        api = Mock(endpoint="endpoint-test", mailbox=True)
        def post(name, payload=None):
            calls.append((name, payload))
            return pending.pop(0) if name == "mail/inbox" and pending else None
        api.post.side_effect = post
        def receive(handler):
            requests.append(json.loads(handler.rfile.read(int(handler.headers["Content-Length"]))))
            respond(handler, {"accepted": True}, status=202)
        with LocalHTTP(receive) as local:
            drain(api, adapter_for(local.base, body={"record": "{{envelope}}", "prompt": "{{notification}}"}))
        self.assertEqual([item["record"]["kind"] for item in requests], ["request", "reply", "result"])
        self.assertTrue(all(name in ("mail/inbox", "mail/delivery") for name, _ in calls))
        receipts = [payload for name, payload in calls if name == "mail/delivery"]
        self.assertEqual(len(receipts), 4)
        self.assertTrue(all(receipt["outcome"] == "delivered" for receipt in receipts))
        self.assertIn("business completion remain unverified", receipts[0]["detail"])
        self.assertIn("without native input", receipts[-1]["detail"])

    def test_native_timeout_stays_uncertain_without_resending(self):
        api = Mock(endpoint="endpoint-test", mailbox=True)
        api.post.side_effect = [mail_envelope(), {"ok": True}, None]
        with patch("shared.transport.request", side_effect=TimeoutError("private native detail")) as request:
            drain(api, adapter_for())
        request.assert_called_once()
        receipt = api.post.call_args_list[1].args[1]
        self.assertEqual(receipt["outcome"], "uncertain")
        self.assertNotIn("private native detail", receipt["detail"])
        self.assertEqual([call.args[0] for call in api.post.call_args_list], ["mail/inbox", "mail/delivery", "mail/inbox"])

    def test_unconfirmed_delivery_receipt_does_not_repeat_webhook(self):
        requests = []
        def receive(handler):
            requests.append(handler.rfile.read(int(handler.headers["Content-Length"])))
            respond(handler, {}, status=202)
        api = Mock(endpoint="endpoint-test", mailbox=True)
        api.post.side_effect = [mail_envelope(), HubError("lost receipt")]
        with LocalHTTP(receive) as local:
            with self.assertRaisesRegex(HubError, "delivery_receipt_unconfirmed"):
                drain(api, adapter_for(local.base))
        self.assertEqual(len(requests), 1)
        self.assertEqual([call.args[0] for call in api.post.call_args_list], ["mail/inbox", "mail/delivery"])

    def test_legacy_task_control_and_result_keep_their_original_report_context(self):
        for kind in ("task", "control", "result"):
            with self.subTest(kind=kind), patch.object(grok, "post_json_once", return_value=("delivered", "transport")) as send:
                self.assertEqual(adapter_for().deliver(envelope(kind=kind))[0], "delivered")
                prompt = send.call_args.args[1]["prompt"]
                self.assertIn('"task_id":"task-test"', prompt)
                self.assertNotIn("AGI Hub addressed message", prompt)

    def test_manifest_and_adapter_declare_the_same_mailbox_protocol(self):
        manifest = json.loads(Path(__file__).with_name("manifest.json").read_text(encoding="utf-8"))
        self.assertEqual(manifest["mailbox_protocol_version"], grok.GrokRoutineAdapter.mailbox_protocol_version)
        self.assertEqual(manifest["supported_modes"], ["queue"])

    def test_invalid_or_foreign_mail_is_rejected_before_any_webhook(self):
        adapter = grok.GrokRoutineAdapter({"endpoint": "endpoint-test", "native": {
            "routine_id": "routine-test", "url": "https://example.invalid/routine",
            "body": {"record": "{{envelope}}"},
        }})
        invalid = [
            dict(mail_envelope(), recipient="another-endpoint"),
            dict(mail_envelope(), mode="steer"),
            dict(mail_envelope(), mode="interrupt"),
            dict(mail_envelope(), kind="status"),
            dict(mail_envelope(), kind="task"),
            dict(mail_envelope(), kind="control"),
            dict(mail_envelope(), mailbox="true"),
            dict(mail_envelope(), task_id="legacy-task"),
            dict(mail_envelope(), correlation_id=""),
            dict(mail_envelope(), correlation_id=1),
            dict(mail_envelope(), content=None),
            dict(mail_envelope(), message_id=""),
            dict(mail_envelope(), root_origin=None),
            dict(mail_envelope(), root_origin={}),
        ]
        missing_mode = mail_envelope()
        missing_mode.pop("mode")
        invalid.append(missing_mode)
        for index, message in enumerate(invalid):
            with self.subTest(index=index), patch.object(grok, "post_json_once", return_value=("delivered", "transport")) as send:
                outcome, _ = adapter.deliver(message)
                self.assertEqual(outcome, "failed")
                send.assert_not_called()

    def test_registered_protocol_selects_mailbox_or_legacy_without_mixing(self):
        adapter = grok.GrokRoutineAdapter({"endpoint": "endpoint-test", "native": {
            "routine_id": "routine-test", "url": "https://example.invalid/routine",
            "body": {"prompt": "{{notification}}"},
        }})
        for version in (0, 1):
            with self.subTest(version=version):
                api = Mock(endpoint="endpoint-test")
                binding = registered_binding(driver="grok-routine", session_id="routine-test")
                binding["endpoint"]["mailbox_protocol_version"] = version
                api.get.return_value = binding
                verify_binding(api, adapter)
                self.assertIs(api.mailbox, version == 1)
                api.post.return_value = None
                drain(api, adapter)
                api.post.assert_called_once_with("mail/inbox" if version else "inbox")

    def test_mailbox_rejects_legacy_task_placeholder_before_webhook(self):
        adapter = grok.GrokRoutineAdapter({"endpoint": "endpoint-test", "native": {
            "routine_id": "routine-test", "url": "https://example.invalid/routine",
            "body": {"prompt": "{{notification}}", "legacy": "{{task_id}}"},
        }})
        with patch.object(grok, "post_json_once") as send:
            outcome, _ = adapter.deliver(mail_envelope())
        self.assertEqual(outcome, "failed")
        send.assert_not_called()

    def test_mailbox_without_task_id_delivers_complete_notification_once(self):
        requests = []
        def receive(handler):
            requests.append(json.loads(handler.rfile.read(int(handler.headers["Content-Length"]))))
            respond(handler, {"accepted": True}, status=202)
        message = mail_envelope()
        with LocalHTTP(receive) as local:
            adapter = grok.GrokRoutineAdapter({"endpoint": "endpoint-test", "native": {
                "routine_id": "routine-test", "url": local.base,
                "body": {"prompt": "{{notification}}"},
            }})
            outcome, detail = adapter.deliver(message)
        self.assertEqual(outcome, "delivered")
        self.assertIn("acceptance and business completion remain unverified", detail)
        self.assertEqual(len(requests), 1)
        self.assertIn('"correlation_id":"root-conversation"', requests[0]["prompt"])
        self.assertIn('"reply_to":"message-test"', requests[0]["prompt"])
        self.assertNotIn('"task_id"', requests[0]["prompt"])

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


if __name__ == "__main__":
    unittest.main()
