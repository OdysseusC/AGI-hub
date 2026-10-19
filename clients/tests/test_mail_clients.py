"""Offline addressed-message invariants; no native model is started."""

import contextlib
import io
import json
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock, patch
from urllib.parse import parse_qs, urlsplit

CLIENTS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(CLIENTS))
from shared.native_report import main_for_binding
from shared.notification import notification
from shared.runtime import drain, verify_binding
from shared.transport import ConfigurationError, HubError
from tests.support import LocalHTTP, envelope, registered_binding, respond


def mail_envelope(kind="request"):
    value = dict(envelope(kind=kind), mailbox=True, root_origin={
        "endpoint": "original-entry", "device_id": "device-test",
        "session_id": "original-chat", "request_ref": "human-turn-test",
    }, correlation_id="root-conversation")
    value.pop("task_id")
    return value


class MailRuntimeTests(unittest.TestCase):
    def test_supported_native_binding_required_before_claim(self):
        api = Mock(endpoint="endpoint-test")
        record = registered_binding()
        record["endpoint"]["mailbox_protocol_version"] = 1
        api.get.return_value = record
        unsupported = Mock(mailbox_protocol_version=0)
        with self.assertRaisesRegex(ConfigurationError, "does_not_support_mailbox_protocol"):
            verify_binding(api, unsupported)
        unsupported.validate_binding.assert_not_called()
        supported = Mock(mailbox_protocol_version=1)
        verify_binding(api, supported)
        self.assertIs(api.mailbox, True)
        supported.validate_binding.assert_called_once()
        api.post.assert_not_called()

    def test_mailbox_uses_only_its_inbox_and_receipts_after_native_exit(self):
        sequence = []
        pending = iter([mail_envelope(), None])
        api = Mock(endpoint="endpoint-test", mailbox=True)
        def post(name, payload=None):
            sequence.append(name)
            return next(pending) if name == "mail/inbox" else {"ok": True}
        api.post.side_effect = post
        adapter = Mock()
        def deliver(_message):
            sequence.append("matching_native_result_after_exit")
            return "delivered", "native transport confirmed"
        adapter.deliver.side_effect = deliver
        drain(api, adapter)
        self.assertEqual(sequence, ["mail/inbox", "matching_native_result_after_exit", "mail/delivery", "mail/inbox"])
        self.assertNotIn("report", sequence)

    def test_status_is_recorded_without_native_input_or_automatic_business_reply(self):
        api = Mock(endpoint="endpoint-test", mailbox=True)
        message = mail_envelope("status")
        api.post.side_effect = [message, {"ok": True}, None]
        adapter = Mock()
        drain(api, adapter)
        adapter.deliver.assert_not_called()
        self.assertEqual([call.args[0] for call in api.post.call_args_list], ["mail/inbox", "mail/delivery", "mail/inbox"])
        receipt = api.post.call_args_list[1].args[1]
        self.assertEqual(receipt["outcome"], "delivered")
        self.assertIn("without native input", receipt["detail"])

    def test_no_automatic_reply_report_or_settlement_for_any_mail_kind(self):
        for kind in ("request", "reply", "result"):
            api = Mock(endpoint="endpoint-test", mailbox=True)
            api.post.side_effect = [mail_envelope(kind), {"ok": True}, None]
            adapter = Mock()
            adapter.deliver.return_value = "delivered", "native evidence"
            drain(api, adapter)
            adapter.deliver.assert_called_once()
            self.assertEqual([call.args[0] for call in api.post.call_args_list], ["mail/inbox", "mail/delivery", "mail/inbox"])

    def test_receipt_failure_stops_without_repeating_native_action(self):
        api = Mock(endpoint="endpoint-test", mailbox=True)
        api.post.side_effect = [mail_envelope(), HubError("lost receipt")]
        adapter = Mock()
        adapter.deliver.return_value = "delivered", "native exit verified"
        with self.assertRaisesRegex(HubError, "receipt_unconfirmed"):
            drain(api, adapter)
        adapter.deliver.assert_called_once()
        self.assertEqual(api.post.call_count, 2)

    def test_mail_and_legacy_inboxes_never_mix(self):
        for mailbox, message in ((True, envelope()), (False, mail_envelope()), (True, dict(mail_envelope(), task_id="legacy-task"))):
            api = Mock(endpoint="endpoint-test", mailbox=mailbox)
            api.post.return_value = message
            adapter = Mock()
            with self.assertRaisesRegex(HubError, "invalid_claimed_envelope"):
                drain(api, adapter)
            adapter.deliver.assert_not_called()

    def test_message_notifications_preserve_choice_and_do_not_impose_parent_chain(self):
        for kind in ("request", "reply", "result"):
            text = notification("endpoint-test", mail_envelope(kind))
            for expected in ("root_origin", "mail-send", "recipient you choose", "advisory reference", "Save tokens"):
                self.assertIn(expected, text)
            self.assertNotIn("parent_task_id", text)
            self.assertNotIn("waiting_reply", text)
            self.assertNotIn("resume_message_id", text)
            self.assertIn('"id":"<new-saved-stable-id>"', text)
            self.assertIn("REQUIRED new stable id", text)
        with self.assertRaisesRegex(ValueError, "does_not_wake"):
            notification("endpoint-test", mail_envelope("status"))


class FixedMailHelperTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.workspace = self.root / "workspace"
        self.workspace.mkdir()
        self.data = self.root / "data"
        self.data.mkdir()
        self.config = self.root / "client.json"
        self.endpoint = "workbuddy-test"
        self.config.write_text(json.dumps({
            "base_url": "http://127.0.0.1:7331", "endpoint": self.endpoint,
            "endpoint_token": "fixed-private-scoped-token", "native": {},
        }))
        self.submission = {"id": "stable-message", "recipient": "codex-test", "kind": "reply", "content": "small bounded result", "correlation_id": "root-conversation"}

    def invoke(self, args):
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            code = main_for_binding(self.config, self.data, self.endpoint, self.workspace, args)
        self.assertNotIn("fixed-private-scoped-token", output.getvalue())
        return code, output.getvalue()

    def test_send_uses_only_scoped_route_preserves_stable_payload_and_explicit_recipient(self):
        path = self.workspace / "message.json"
        path.write_text(json.dumps(self.submission))
        api = Mock()
        api.post.return_value = {"message_id": "stable-message", "correlation_id": "root-conversation"}
        with patch("shared.native_report.HubAPI", return_value=api):
            code, output = self.invoke(["mail-send", "--endpoint", self.endpoint, "--data", str(self.data), "--file", "message.json"])
        self.assertEqual(code, 0)
        api.post.assert_called_once_with("mail/send", self.submission)
        self.assertEqual(json.loads(output)["message_id"], "stable-message")

    def test_catalog_and_state_are_single_scoped_reads(self):
        api = Mock()
        api.get.side_effect = [{"members": []}, {"messages": []}]
        with patch("shared.native_report.HubAPI", return_value=api):
            self.assertEqual(self.invoke(["peers"])[0], 0)
            self.assertEqual(self.invoke(["mail-state", "--correlation-id", "known-conversation"])[0], 0)
        self.assertEqual(api.get.call_count, 2)
        api.get.assert_any_call("catalog")
        api.get.assert_any_call("mail/state", {"correlation_id": "known-conversation"})
        api.post.assert_not_called()

    def test_http_mail_routes_keep_fixed_scope_and_encode_conversation_reference(self):
        calls = []
        def callback(handler):
            address = urlsplit(handler.path)
            query = parse_qs(address.query)
            self.assertEqual(query["endpoint"], [self.endpoint])
            self.assertEqual(handler.headers["Authorization"], "Bearer fixed-private-scoped-token")
            self.assertEqual(handler.headers["Origin"], local.base)
            calls.append(address.path)
            if handler.command == "POST":
                body = json.loads(handler.rfile.read(int(handler.headers["Content-Length"])))
                self.assertEqual(body, self.submission)
                respond(handler, {"message_id": body["id"]})
            else:
                self.assertEqual(query["correlation_id"], ["conversation / ? &"])
                respond(handler, {"messages": []})
        path = self.workspace / "message.json"
        path.write_text(json.dumps(self.submission))
        with LocalHTTP(callback) as local:
            config = json.loads(self.config.read_text())
            config["base_url"] = local.base
            self.config.write_text(json.dumps(config))
            self.assertEqual(self.invoke(["mail-send", "--file", "message.json"])[0], 0)
            self.assertEqual(self.invoke(["mail-state", "--correlation-id", "conversation / ? &"])[0], 0)
        self.assertEqual(calls, ["/v1/mail/send", "/v1/mail/state"])

    def test_helper_rejects_authority_path_task_and_flag_overrides(self):
        path = self.workspace / "message.json"
        invalid = [dict(self.submission, origin="other-entry"), dict(self.submission, mode="interrupt"), dict(self.submission, parent_task_id="old-model"), dict(self.submission, id=""), dict(self.submission, kind="task")]
        with patch("shared.native_report.HubAPI") as api:
            for body in invalid:
                path.write_text(json.dumps(body))
                self.assertEqual(self.invoke(["mail-send", "--file", "message.json"])[0], 1)
            for args in (
                ["mail-send", "--endpoint", "other", "--file", "message.json"],
                ["mail-send", "--file", "../outside.json"],
                ["mail-send", "--file", "one", "--file=two"],
                ["mail-state", "--file", "message.json"], ["mail-state"],
                ["peers", "--correlation-id", "bad"], ["mail-send", "--admin-token", "bad"],
                ["peer-send", "--file", "message.json"], ["peer/settled", "--file", "message.json"],
            ):
                self.assertEqual(self.invoke(args)[0], 1)
        api.assert_not_called()

    def test_ambiguous_send_retains_original_id_without_automatic_retry(self):
        path = self.workspace / "message.json"
        path.write_text(json.dumps(self.submission))
        api = Mock()
        api.post.side_effect = HubError("lost response")
        with patch("shared.native_report.HubAPI", return_value=api):
            code, output = self.invoke(["mail-send", "--file", "message.json"])
        self.assertEqual(code, 1)
        self.assertIn("mail_send_unconfirmed_keep_stable_id", output)
        api.post.assert_called_once_with("mail/send", self.submission)

    def test_invented_routing_flags_produce_actionable_usage_without_http(self):
        with patch("shared.native_report.HubAPI") as api:
            for flag in ("--peer", "--to", "--collaboration-flag"):
                code, output = self.invoke(["mail-send", flag, "codex"])
                self.assertEqual(code, 1)
                self.assertIn("mail-send --file <workspace JSON>", json.loads(output)["usage"])
                self.assertIn("recipient is JSON field", output)
        api.assert_not_called()

    def test_missing_id_and_unknown_fields_explain_exact_schema_without_echoing_private_data(self):
        path = self.workspace / "message.json"
        missing = dict(self.submission)
        del missing["id"]
        unknown = dict(self.submission, **{"private-field-name": "private-body-value"})
        with patch("shared.native_report.HubAPI") as api:
            for body, expected in (
                (missing, "mail_id_required"), (unknown, "unsupported_mail_fields"),
                (dict(self.submission, recipient=""), "mail_recipient_required"),
                (dict(self.submission, kind="task"), "mail_kind_invalid"),
                (dict(self.submission, content=""), "mail_content_required"),
            ):
                path.write_text(json.dumps(body))
                code, output = self.invoke(["mail-send", "--file", "message.json"])
                self.assertEqual(code, 1)
                error = json.loads(output)
                self.assertEqual(error["code"], expected)
                self.assertEqual(error["required_fields"], ["id", "recipient", "kind", "content"])
                self.assertIn("reply_to", error["optional_fields"])
                self.assertNotIn("private-field-name", output)
                self.assertNotIn("private-body-value", output)
        api.assert_not_called()


if __name__ == "__main__":
    unittest.main()
