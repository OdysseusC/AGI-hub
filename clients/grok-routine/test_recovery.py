"""Offline-only checks for the explicitly authorized maintenance transport."""

from __future__ import annotations

import contextlib
from datetime import datetime, timezone
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location("grok_recovery", Path(__file__).with_name("recovery.py"))
recovery = importlib.util.module_from_spec(spec)
spec.loader.exec_module(recovery)


class RecoveryTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.config_path = self.root / "native.json"
        self.plan_path = self.root / "plan.json"
        self.record = self.root / "attempt.json"
        self.now = datetime(2026, 10, 27, 12, tzinfo=timezone.utc)
        self.native = {
            "maintenance_contract": {"version": 1, "routine_id": "existing-grok-routine",
                                     "evidence_ref": "operator-verified-instruction-20261027"},
            "routine_id": "existing-grok-routine",
            "url": "https://routine.example.test/PRIVATE-WEBHOOK",
            "headers": {"Authorization": "Bearer PRIVATE-KEY"},
            "body": {"grok_pr9_mailbox": "{{notification}}"},
            "timeout_seconds": 12,
        }
        self.plan = {
            "type": "agi_hub_lab_recovery", "source": "out_of_band_maintenance", "hub_claimed": False,
            "attempt_id": "saved-recovery-attempt-1", "expires_at": "2026-10-27T12:15:00Z",
            "target": {"hub_id": "a" * 64, "device_id": "C-lab", "data_dir": "C:\\Users\\owner\\lab\\hub"},
            "root_entry": {"hub_id": "b" * 64, "endpoint": "codex-pr9"},
            "original_work_ref": "human-recovery-request-20261027",
        }
        self.calls = []
        self.save()

    def save(self, config=None, plan=None):
        self.config_path.write_text(json.dumps(self.native if config is None else config), encoding="utf-8")
        self.plan_path.write_text(json.dumps(self.plan if plan is None else plan), encoding="utf-8")

    def sender(self, url, body, headers, timeout):
        self.calls.append((url, body, headers, timeout))
        # The private, durable attempt must exist before the request is started.
        record = json.loads(self.record.read_text(encoding="utf-8"))
        self.assertEqual(record["state"], "sending")
        self.assertEqual(record["attempt_id"], self.plan["attempt_id"])
        wire = json.dumps(body, ensure_ascii=False, allow_nan=False).encode("utf-8")
        self.assertEqual(record["payload_sha256"], hashlib.sha256(wire).hexdigest())
        return "delivered", "PRIVATE PLATFORM RESPONSE MUST NOT ESCAPE"

    def run_recovery(self, sender=None):
        return recovery.recover(self.config_path, self.plan_path, self.record, now=self.now,
                                sender=self.sender if sender is None else sender)

    def test_one_maintenance_call_uses_fixed_native_target_without_hub_claim(self):
        result = self.run_recovery()
        self.assertEqual(result["state"], "transport_accepted")
        self.assertEqual(len(self.calls), 1)
        url, body, headers, timeout = self.calls[0]
        self.assertEqual((url, headers, timeout), (self.native["url"], self.native["headers"], 12))
        self.assertEqual(set(body), {"agi_hub_lab_recovery"})
        text = body["agi_hub_lab_recovery"]
        self.assertIn("not a Hub mailbox delivery", text)
        self.assertIn('"hub_claimed":false', text)
        self.assertIn(self.plan["attempt_id"], text)
        self.assertIn("Do not reboot", text)
        self.assertIn("check current UTC against expires_at", text)
        self.assertIn("already registered and enabled Clients", text)
        self.assertNotIn("AGI Hub addressed message.", text)
        self.assertNotIn('"delivery":"dispatching"', text)
        saved = self.record.read_text(encoding="utf-8")
        for secret in ("PRIVATE-KEY", "PRIVATE-WEBHOOK", "PRIVATE PLATFORM", "endpoint_token", self.plan["target"]["data_dir"]):
            self.assertNotIn(secret, saved + json.dumps(result))
        if os.name != "nt":
            self.assertEqual(self.record.stat().st_mode & 0o777, 0o600)

    def test_full_client_does_not_require_or_emit_an_endpoint_token(self):
        for include_token in (False, True):
            with self.subTest(include_token=include_token):
                self.record.unlink(missing_ok=True)
                config = {"base_url": "http://127.0.0.1:7331", "endpoint": "grok-pr9",
                          "maintenance_contract": self.native["maintenance_contract"],
                          "native": {key: value for key, value in self.native.items() if key != "maintenance_contract"}}
                if include_token:
                    config["endpoint_token"] = "PRIVATE-HUB-TOKEN"
                self.save(config=config)
                result = self.run_recovery()
                self.assertNotIn("PRIVATE-HUB-TOKEN", json.dumps(result) + self.record.read_text())

    def test_existing_attempt_refuses_a_second_call_even_after_uncertain_result(self):
        def timeout(*args):
            self.calls.append(args)
            raise TimeoutError("PRIVATE URL AND KEY")
        result = self.run_recovery(sender=timeout)
        self.assertEqual(result["state"], "uncertain")
        with self.assertRaises(recovery.RecoveryError) as caught:
            self.run_recovery()
        self.assertEqual(caught.exception.code, "attempt_already_exists")
        self.assertEqual(len(self.calls), 1)
        self.assertEqual(json.loads(self.record.read_text())["state"], "uncertain")

    def test_result_record_failure_never_retries_the_attempt(self):
        real_write = recovery._write_record
        def fail_result(stream, record):
            if record["state"] != "sending":
                raise OSError("PRIVATE IO DETAIL")
            real_write(stream, record)
        with patch.object(recovery, "_write_record", side_effect=fail_result):
            result = self.run_recovery()
        self.assertEqual(result["code"], "attempt_result_unconfirmed_no_retry")
        self.assertEqual(json.loads(self.record.read_text())["state"], "sending")
        with self.assertRaises(recovery.RecoveryError) as caught:
            self.run_recovery()
        self.assertEqual(caught.exception.code, "attempt_already_exists")
        self.assertEqual(len(self.calls), 1)

    def test_expired_or_unbounded_maintenance_never_creates_an_attempt(self):
        for expiry in ("2026-10-27T11:59:59Z", "2026-10-27T12:00:00Z", "2026-10-30T12:00:00Z", "2026-10-27", "not-a-time"):
            with self.subTest(expiry=expiry):
                plan = {**self.plan, "expires_at": expiry}
                self.save(plan=plan)
                with self.assertRaises(recovery.RecoveryError):
                    self.run_recovery()
                self.assertFalse(self.record.exists())
        self.assertEqual(self.calls, [])

    def test_plan_rejects_business_envelopes_and_free_form_prompts(self):
        for mutation in (
            {"instruction": "reboot or run arbitrary work"}, {"prompt": "PRIVATE PROMPT"},
            {"hub_claimed": True}, {"hub_claimed": 0}, {"type": "request"}, {"source": "hub"},
            {"attempt_id": ""}, {"attempt_id": "bad\nattempt"},
            {"target": {**self.plan["target"], "hub_id": "unknown"}},
            {"target": {**self.plan["target"], "data_dir": "relative/lab"}},
            {"target": {**self.plan["target"], "data_dir": "C:\\lab\\..\\business"}},
            {"target": {**self.plan["target"], "data_dir": "C:\\"}},
            {"root_entry": {**self.plan["root_entry"], "endpoint": "other/endpoint"}},
            {"original_work_ref": "do arbitrary work instead"},
        ):
            with self.subTest(mutation=mutation):
                self.save(plan={**self.plan, **mutation})
                with self.assertRaises(recovery.RecoveryError):
                    self.run_recovery()
                self.assertFalse(self.record.exists())
        self.assertEqual(self.calls, [])

    def test_existing_business_template_cannot_redirect_maintenance_payload(self):
        for body in ({"grok_pr9_mailbox": "{{notification}}"}, {"message": "{{envelope}}"},
                     {"message": "PRIVATE OLD BUSINESS BODY"}):
            with self.subTest(body=body):
                self.record.unlink(missing_ok=True)
                self.save(config={**self.native, "body": body})
                result = self.run_recovery()
                self.assertEqual(result["state"], "transport_accepted")
                self.assertEqual(set(self.calls[-1][1]), {"agi_hub_lab_recovery"})
                self.assertNotIn("PRIVATE OLD BUSINESS BODY", json.dumps(self.calls[-1][1]))

    def test_unverified_mismatched_or_free_form_maintenance_contract_never_sends(self):
        valid = self.native["maintenance_contract"]
        for contract in (None, {}, {**valid, "version": True}, {**valid, "version": 2},
                         {**valid, "routine_id": "another-routine"}, {**valid, "evidence_ref": ""},
                         {**valid, "evidence_ref": "bad\nrecord"}, {**valid, "prompt": "arbitrary work"},
                         {**valid, "body": {"grok_pr9_mailbox": "{{notification}}"}}):
            with self.subTest(contract=contract):
                config = {key: value for key, value in self.native.items() if key != "maintenance_contract"}
                if contract is not None:
                    config["maintenance_contract"] = contract
                self.save(config=config)
                with self.assertRaises(recovery.RecoveryError):
                    self.run_recovery()
                self.assertFalse(self.record.exists())
        self.assertEqual(self.calls, [])

    def test_invalid_config_and_duplicate_json_do_not_send(self):
        for config in ({**self.native, "url": "http://routine.example.test/secret"},
                       {**self.native, "headers": {"Host": "other.example.test"}},
                       {**self.native, "timeout_seconds": True},
                       {**self.native, "prompt": "PRIVATE PROMPT"}):
            self.save(config=config)
            with self.assertRaises(recovery.RecoveryError):
                self.run_recovery()
            self.assertFalse(self.record.exists())
        self.config_path.write_text('{"native":{},"native":{"secret":"PRIVATE"}}')
        with self.assertRaises(recovery.RecoveryError):
            self.run_recovery()
        self.assertEqual(self.calls, [])

    def test_fsync_failure_prevents_the_request(self):
        with patch.object(recovery.os, "fsync", side_effect=OSError("PRIVATE PATH")):
            with self.assertRaises(recovery.RecoveryError) as caught:
                self.run_recovery()
        self.assertEqual(caught.exception.code, "attempt_record_unavailable")
        self.assertEqual(self.calls, [])
        self.assertTrue(self.record.exists())

    def test_cli_errors_and_transport_results_never_print_sensitive_data(self):
        output = io.StringIO()
        with patch.object(recovery, "post_json_once", side_effect=TimeoutError("PRIVATE-KEY PRIVATE-WEBHOOK")), \
                contextlib.redirect_stdout(output):
            code = recovery.main(["--config", str(self.config_path), "--plan", str(self.plan_path), "--attempt-record", str(self.record)], now=self.now)
        self.assertNotEqual(code, 0)
        self.assertEqual(json.loads(output.getvalue())["state"], "uncertain")
        self.assertNotIn("PRIVATE", output.getvalue() + self.record.read_text())
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            self.assertNotEqual(recovery.main(["--key", "PRIVATE-KEY"]), 0)
        self.assertNotIn("PRIVATE", output.getvalue())


if __name__ == "__main__":
    unittest.main()
