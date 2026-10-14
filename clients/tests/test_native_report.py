"""Local-only tests for the fixed report launcher. No native assistant is run."""

import contextlib
import io
import json
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock, patch

CLIENTS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(CLIENTS))
from shared.native_report import main_for_binding
from shared.runtime import MAX_BYTES
from shared.transport import HubError
from tests.support import LocalHTTP, respond


class NativeReportTests(unittest.TestCase):
    def setUp(self):
        self.private = tempfile.TemporaryDirectory()
        self.addCleanup(self.private.cleanup)
        self.root = Path(self.private.name).resolve()
        self.workspace = self.root / "workspace"
        self.workspace.mkdir()
        self.data = self.root / "data"
        self.data.mkdir()
        self.config = self.root / "client.json"
        self.endpoint = "workbuddy-report-test"
        self.report = {"event_id": "stable-event-test", "task_id": "task-test", "endpoint": self.endpoint, "state": "accepted", "run_id": "native-run-test"}
        self.config.write_text(json.dumps({"base_url": "http://127.0.0.1:7331", "endpoint": self.endpoint, "endpoint_token": "private-scoped-test", "native": {}}))

    def invoke(self, args):
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            code = main_for_binding(self.config, self.data, self.endpoint, self.workspace, args)
        self.assertNotIn("private-scoped-test", output.getvalue())
        self.assertNotIn(str(self.root), output.getvalue())
        return code, output.getvalue()

    def test_fixed_scope_submits_one_report_with_go_compatible_flags(self):
        saved = []
        def callback(handler):
            self.assertEqual(handler.path, "/v1/report?endpoint=" + self.endpoint)
            self.assertEqual(handler.headers["Authorization"], "Bearer private-scoped-test")
            saved.append(json.loads(handler.rfile.read(int(handler.headers["Content-Length"]))))
            respond(handler, {"ok": True})
        path = self.workspace / "report.json"
        path.write_text(json.dumps(self.report))
        with LocalHTTP(callback) as local:
            config = json.loads(self.config.read_text())
            config["base_url"] = local.base
            self.config.write_text(json.dumps(config))
            code, output = self.invoke(["report", "--data", str(self.data), "--endpoint", self.endpoint, "--file", str(path)])
        self.assertEqual(code, 0)
        self.assertEqual(saved, [self.report])
        self.assertEqual(json.loads(output), {"state": "ready", "code": "report_saved"})

    def test_stdin_cannot_change_fixed_endpoint_and_does_not_require_native_transport(self):
        api = Mock()
        for explicit in (False, True):
            stream = io.TextIOWrapper(io.BytesIO(json.dumps(self.report).encode()))
            args = ["report", "--file=-"] if explicit else ["report"]
            with patch("shared.native_report.HubAPI", return_value=api), patch("shared.native_report.sys.stdin", stream):
                code, _ = self.invoke(args)
            self.assertEqual(code, 0)
        self.assertEqual(api.post.call_count, 2)
        api.post.assert_called_with("report", self.report)

    def test_wrong_commands_duplicate_flags_and_binding_overrides_are_rejected(self):
        bad_args = (
            ["leader", "--endpoint", self.endpoint],
            ["report", "--endpoint", self.endpoint, "--endpoint=" + self.endpoint],
            ["report", "--data", str(self.data), "--data=" + str(self.data)],
            ["report", "--file", "one.json", "--file=two.json"],
            ["report", "--config", str(self.config)],
            ["report", "--endpoint", "another-endpoint"],
            ["report", "--data", str(self.workspace)],
            ["report", "--data", "relative-data"],
        )
        with patch("shared.native_report.HubAPI") as api:
            for args in bad_args:
                with self.subTest(args=args):
                    self.assertEqual(self.invoke(args)[0], 1)
        api.assert_not_called()

    def test_file_must_be_inside_workspace_regular_and_without_link_components(self):
        outside = self.root / "outside.json"
        outside.write_text(json.dumps(self.report))
        linked_file = self.workspace / "linked.json"
        linked_directory = self.workspace / "linked-dir"
        try:
            linked_file.symlink_to(outside)
            linked_directory.symlink_to(self.root, target_is_directory=True)
            symlink_args = [str(linked_file), str(linked_directory / "outside.json")]
        except OSError:
            symlink_args = []
        with patch("shared.native_report.HubAPI") as api:
            for filename in (str(outside), "../outside.json", str(self.workspace), *symlink_args):
                with self.subTest(filename=filename):
                    self.assertEqual(self.invoke(["report", "--file", filename])[0], 1)
        api.assert_not_called()

    def test_report_endpoint_duplicate_json_and_size_are_checked_before_network(self):
        path = self.workspace / "report.json"
        bodies = (
            json.dumps(dict(self.report, endpoint="another-endpoint")),
            '{"endpoint":"one","endpoint":"two"}',
            '[]',
            'not JSON private detail',
            'x' * (MAX_BYTES + 1),
        )
        with patch("shared.native_report.HubAPI") as api:
            for body in bodies:
                path.write_text(body)
                with self.subTest(size=len(body)):
                    code, output = self.invoke(["report", "--file", "report.json"])
                    self.assertEqual(code, 1)
                    self.assertNotIn("private detail", output)
        api.assert_not_called()

    def test_unconfirmed_report_preserves_stable_event_without_automatic_retry(self):
        path = self.workspace / "report.json"
        path.write_text(json.dumps(self.report))
        api = Mock()
        api.post.side_effect = HubError("network detail")
        with patch("shared.native_report.HubAPI", return_value=api):
            code, output = self.invoke(["report", "--file", str(path)])
        self.assertEqual(code, 1)
        self.assertEqual(json.loads(output)["code"], "report_unconfirmed_keep_stable_event_id")
        api.post.assert_called_once_with("report", self.report)


if __name__ == "__main__":
    unittest.main()
