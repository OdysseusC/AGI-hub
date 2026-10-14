"""Local-only adapter tests; no messages are sent to real assistants."""

import sys
import unittest
from pathlib import Path

CLIENTS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(CLIENTS))
import json
from unittest.mock import patch
from shared.transport import ConfigurationError, post_json_once
from workbuddy.adapter import MESSAGE_URL, WorkBuddyAdapter, delivery_confirmed
from tests.support import envelope, LocalHTTP


class AdapterTests(unittest.TestCase):
    def test_workbuddy_requires_explicit_oauth_and_uses_documented_body(self):
        with self.assertRaisesRegex(ConfigurationError, "unconfigured_workbuddy"):
            WorkBuddyAdapter({"endpoint": "endpoint-test", "native": {}})
        config = {"endpoint": "endpoint-test", "native": {"access_token": "user-granted-test-token", "oauth_scopes": ["user.localassistant.invokable"]}}
        adapter = WorkBuddyAdapter(config)
        with patch("workbuddy.adapter.post_json_once", return_value=("delivered", "transport")) as send:
            adapter.deliver(envelope())
        send.assert_called_once()
        url, body, headers, _ = send.call_args.args
        self.assertEqual(url, MESSAGE_URL)
        self.assertEqual(set(body), {"content", "msg_type"})
        self.assertEqual(body["msg_type"], "text")
        self.assertIn('"message_id":"message-test"', body["content"])
        self.assertNotIn("user-granted-test-token", body["content"])
        self.assertEqual(headers, {"Authorization": "Bearer user-granted-test-token"})
        self.assertIs(send.call_args.kwargs["response_validator"], delivery_confirmed)

    def test_workbuddy_2xx_requires_confirmed_business_receipt_without_retry(self):
        cases = [
            (b'{"code":0,"msg":"success","request_id":"req-test","data":{"message_id":"msg-001"}}', "delivered"),
            (b'{"code":10001,"msg":"private error detail","data":{"message_id":"msg-001"}}', "uncertain"),
            (b'', "uncertain"),
            (b'<html>private upstream diagnostic</html>', "uncertain"),
            (b'{"code":false,"data":{"message_id":"msg-001"}}', "uncertain"),
            (b'{"code":0.0,"data":{"message_id":"msg-001"}}', "uncertain"),
            (b'{"code":0,"data":{"message_id":""}}', "uncertain"),
            (b'{"code":0,"data":{"message_id":"unexpected id\\n"}}', "uncertain"),
            (b'{"code":0,"data":{"message_id":17}}', "uncertain"),
            (b'{"code":1,"code":0,"data":{"message_id":"msg-001"}}', "uncertain"),
        ]
        for response_body, expected in cases:
            attempted = []
            def callback(handler):
                attempted.append(handler.rfile.read(int(handler.headers.get("Content-Length", 0))))
                handler.send_response(200)
                handler.send_header("Content-Type", "application/json")
                handler.send_header("Content-Length", str(len(response_body)))
                handler.end_headers()
                handler.wfile.write(response_body)
            with self.subTest(expected=expected, body=response_body), LocalHTTP(callback) as local:
                outcome, detail = post_json_once(local.base, envelope(), {}, 1, response_validator=delivery_confirmed)
            self.assertEqual(outcome, expected)
            self.assertEqual(len(attempted), 1)
            self.assertNotIn("private", detail)
            self.assertNotIn("msg-001", detail)


if __name__ == "__main__":
    unittest.main()
