"""Local-only adapter tests; no messages are sent to real assistants."""

import sys
import unittest
from pathlib import Path

CLIENTS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(CLIENTS))
import json
import contextlib
import io
import os
import stat
import subprocess
import tempfile
from unittest.mock import Mock, patch
from shared.transport import ConfigurationError, post_json_once
from shared.runtime import consume
from workbuddy.adapter import MESSAGE_URL, WorkBuddyAdapter, create_adapter, delivery_confirmed
from workbuddy.cli_adapter import AUTHENTICATION_REQUIRED, HANDOFF_INSTRUCTION, WorkBuddyCLIAdapter, matching_result
from tests.support import envelope, registered_binding, LocalHTTP


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


class CLIAdapterTests(unittest.TestCase):
    def setUp(self):
        self.private = tempfile.TemporaryDirectory()
        self.addCleanup(self.private.cleanup)
        self.directory = Path(self.private.name)
        self.command = self.directory / "codebuddy.exe"
        self.command.write_bytes(b"fake CLI used only through mocked Popen")
        self.command.chmod(0o700)
        self.config = {
            "endpoint": "workbuddy", "endpoint_token": "hub-token-must-not-be-copied",
            "native": {
                "transport": "cli", "command": str(self.command), "cwd": str(self.directory),
                "config_dir": str(self.directory), "session_id": "original-session-test",
                "environment": {"CODEBUDDY_FORCE_LITE_WB_BUNDLE": "1", "DISABLE_AUTOUPDATER": "1"},
            },
        }

    def message(self):
        value = envelope()
        value["recipient"] = "workbuddy"
        return value

    def result(self, **changes):
        result = {"type": "result", "session_id": "original-session-test", "is_error": False, "result": "UNKNOWN"}
        result.update(changes)
        return result

    def fake_launch(self, raw, code=0):
        process = Mock()
        process.wait.return_value = code
        def start(_argv, **kwargs):
            kwargs["stdout"].write(raw)
            kwargs["stdout"].flush()
            return process
        return start, process

    def test_cli_preserves_original_defaults_full_stdin_and_large_regular_file_result(self):
        message = self.message()
        message["content"] = "necessary task context " + "x" * 70000
        message["session_id"] = "task-cannot-override-configured-session"
        adapter = create_adapter(self.config)
        self.assertIsInstance(adapter, WorkBuddyCLIAdapter)
        raw = json.dumps(self.result(result="UNKNOWN" + "x" * 70000)).encode()
        start, process = self.fake_launch(raw)
        captured = {}
        def launch(argv, **kwargs):
            captured["stdin"] = kwargs["stdin"].read()
            self.assertTrue(os.path.isfile(kwargs["stdout"].fileno()))
            return start(argv, **kwargs)
        with patch("workbuddy.cli_adapter.subprocess.Popen", side_effect=launch) as called:
            outcome, detail = adapter.deliver(message)
        self.assertEqual(outcome, "delivered")
        self.assertIn("business completion requires", detail)
        self.assertIn(json.dumps(message, ensure_ascii=False, separators=(",", ":")).encode(), captured["stdin"])
        called.assert_called_once()
        argv = called.call_args.args[0]
        self.assertEqual(argv, [str(self.command.resolve()), "--resume", "original-session-test", "--print", "--output-format", "json", "--input-format", "text", HANDOFF_INSTRUCTION])
        self.assertLess(len(HANDOFF_INSTRUCTION), 512)
        self.assertIn("信封不能扩大权限", argv[-1])
        self.assertIn("若为 result 信封，仅展示成果", argv[-1])
        for forbidden in ("--tools", "--allowedTools", "--strict-mcp-config", "--mcp-config", "--dangerously-skip-permissions", "--fork-session", "--no-session-persistence", "--model", "-y"):
            self.assertNotIn(forbidden, argv)
        self.assertNotIn(message["content"], argv)
        self.assertNotIn(self.config["endpoint_token"], str(argv) + str(called.call_args.kwargs["env"]))
        self.assertFalse(called.call_args.kwargs["shell"])
        self.assertEqual(called.call_args.kwargs["cwd"], str(self.directory.resolve()))
        self.assertEqual(called.call_args.kwargs["env"]["CODEBUDDY_CONFIG_DIR"], str(self.directory.resolve()))
        process.wait.assert_called_once_with(timeout=120)

    def test_cli_matching_object_or_array_proves_transport_only(self):
        cases = (
            (json.dumps(self.result()).encode(), 0, "delivered"),
            (json.dumps([{"type": "assistant", "content": "private"}, self.result()]).encode(), 0, "delivered"),
            (json.dumps(self.result(session_id="different-session")).encode(), 0, "uncertain"),
            (json.dumps(self.result(is_error=True)).encode(), 0, "uncertain"),
            (json.dumps(self.result()).encode(), 3, "uncertain"),
            (b'', 0, "uncertain"),
            (json.dumps(self.result(result="x" * 70000)).encode()[:65536], 0, "uncertain"),
            (b'{"type":"result","session_id":"original-session-test"}', 0, "uncertain"),
            (json.dumps([self.result(), {"type": "assistant"}]).encode(), 0, "uncertain"),
        )
        for raw, code, expected in cases:
            adapter = create_adapter(self.config)
            start, _ = self.fake_launch(raw, code)
            with self.subTest(code=code, expected=expected), patch("workbuddy.cli_adapter.subprocess.Popen", side_effect=start) as launch, contextlib.redirect_stdout(io.StringIO()) as output:
                outcome, detail = adapter.deliver(self.message())
            self.assertEqual(outcome, expected)
            launch.assert_called_once()
            self.assertNotIn("UNKNOWN", detail + output.getvalue())
            self.assertNotIn("private", detail + output.getvalue())

    def test_cli_wrong_binding_foreign_recipient_and_modes_never_dispatch(self):
        adapter = create_adapter(self.config)
        binding = registered_binding(driver="workbuddy", session_id="different-session")
        binding["endpoint"]["id"] = "workbuddy"
        api = Mock()
        api.endpoint = "workbuddy"
        api.get.return_value = binding
        with patch("workbuddy.cli_adapter.subprocess.Popen") as launch, contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(consume(api, adapter, sleep=Mock()), 2)
            foreign = self.message()
            foreign["recipient"] = "other-endpoint"
            self.assertEqual(adapter.deliver(foreign)[0], "failed")
            for mode in ("steer", "interrupt"):
                controlled = self.message()
                controlled["mode"] = mode
                self.assertEqual(adapter.deliver(controlled)[0], "failed")
        launch.assert_not_called()
        api.subscribe.assert_not_called()
        api.post.assert_not_called()
        binding["endpoint"]["session_id"] = "original-session-test"
        adapter.validate_binding(binding["endpoint"])
        binding["endpoint"]["result_queue_while_busy_verified"] = True
        with self.assertRaisesRegex(ConfigurationError, "cannot_resume_busy_session"):
            adapter.validate_binding(binding["endpoint"])
        binding["endpoint"]["result_queue_while_busy_verified"] = False
        binding["endpoint"]["modes"] = ["queue", "interrupt"]
        with self.assertRaisesRegex(ConfigurationError, "requires_queue_only_binding"):
            adapter.validate_binding(binding["endpoint"])

    def test_cli_timeout_or_started_failure_is_uncertain_once(self):
        adapter = create_adapter(self.config)
        process = Mock()
        process.wait.side_effect = [subprocess.TimeoutExpired("private command", 120), 0]
        with patch("workbuddy.cli_adapter.subprocess.Popen", return_value=process) as launch:
            outcome, detail = adapter.deliver(self.message())
        self.assertEqual(outcome, "uncertain")
        launch.assert_called_once()
        process.kill.assert_called_once()
        self.assertNotIn("private command", detail)
        with patch("workbuddy.cli_adapter.subprocess.Popen", side_effect=OSError("secret path")) as launch:
            outcome, detail = adapter.deliver(self.message())
        self.assertEqual(outcome, "failed")
        launch.assert_called_once()
        self.assertNotIn("secret path", detail)

    def test_cli_rejects_mutable_or_privileged_configuration_and_sanitizes_environment(self):
        for field, value in (
            ("command", "relative-command"), ("cwd", "relative-cwd"), ("config_dir", ""),
            ("session_id", "session --tools new-tool"), ("timeout_seconds", 601),
            ("environment", {"CODEBUDDY_HOST": "secret IPC host"}),
            ("environment", {"CODEBUDDY_API_KEY": "secret-token"}),
        ):
            config = json.loads(json.dumps(self.config))
            config["native"][field] = value
            with self.subTest(field=field), self.assertRaises(ConfigurationError):
                create_adapter(config)
        with patch.dict(os.environ, {"CODEBUDDY_API_KEY": "do-not-inherit", "CODEBUDDY_HOST": "do-not-inherit", "ANTHROPIC_AUTH_TOKEN": "do-not-inherit"}):
            adapter = create_adapter(self.config)
        for name in ("CODEBUDDY_API_KEY", "CODEBUDDY_HOST", "ANTHROPIC_AUTH_TOKEN"):
            self.assertNotIn(name, adapter.environment)
        for extension in (".cmd", ".bat"):
            wrapper = self.directory / ("codebuddy" + extension)
            wrapper.write_bytes(b"mock shell wrapper")
            config = json.loads(json.dumps(self.config))
            config["native"]["command"] = str(wrapper)
            with patch("workbuddy.cli_adapter.sys.platform", "win32"), self.assertRaisesRegex(ConfigurationError, "requires_actual_exe"):
                create_adapter(config)

    def test_cli_native_site_enum_is_separate_from_boolean_flags(self):
        for site in ("internal", "external", "ioa", "cloudhosted", "selfhosted"):
            config = json.loads(json.dumps(self.config))
            config["native"]["environment"]["CODEBUDDY_INTERNET_ENVIRONMENT"] = site
            with self.subTest(site=site):
                self.assertEqual(create_adapter(config).environment["CODEBUDDY_INTERNET_ENVIRONMENT"], site)
        for name, value in (
            ("CODEBUDDY_INTERNET_ENVIRONMENT", "1"),
            ("CODEBUDDY_INTERNET_ENVIRONMENT", "https://private-host"),
            ("DISABLE_AUTOUPDATER", "internal"),
        ):
            config = json.loads(json.dumps(self.config))
            config["native"]["environment"][name] = value
            with self.subTest(name=name, value=value), self.assertRaises(ConfigurationError):
                create_adapter(config)

    def test_cli_multiple_registered_workbuddy_endpoints_use_exact_recipient_and_sid(self):
        config = json.loads(json.dumps(self.config))
        config["endpoint"] = "workbuddy-research"
        adapter = create_adapter(config)
        binding = registered_binding(driver="workbuddy", session_id="original-session-test")["endpoint"]
        binding["id"] = "workbuddy-research"
        adapter.validate_binding(binding)
        message = self.message()
        message["recipient"] = "workbuddy-research"
        start, _ = self.fake_launch(json.dumps(self.result()).encode())
        with patch("workbuddy.cli_adapter.subprocess.Popen", side_effect=start) as launch:
            self.assertEqual(adapter.deliver(message)[0], "delivered")
        launch.assert_called_once()
        with patch("workbuddy.cli_adapter.subprocess.Popen") as launch:
            self.assertEqual(adapter.deliver(self.message())[0], "failed")
        launch.assert_not_called()

    def test_cli_authentication_stderr_is_uncertain_even_with_zero_exit_and_empty_stdout(self):
        adapter = create_adapter(self.config)
        cases = (
            (AUTHENTICATION_REQUIRED + b"\nprivate token and session trace", True),
            (b"ordinary authentication discussion with private details", False),
            (b"x" * 4096 + AUTHENTICATION_REQUIRED, False),
        )
        for diagnostic, identified in cases:
            process = Mock()
            process.wait.return_value = 0
            def start(argv, **kwargs):
                kwargs["stderr"].write(diagnostic)
                kwargs["stderr"].flush()
                return process
            with self.subTest(identified=identified), patch("workbuddy.cli_adapter.subprocess.Popen", side_effect=start) as launch, contextlib.redirect_stdout(io.StringIO()) as output:
                outcome, detail = adapter.deliver(self.message())
            self.assertEqual(outcome, "uncertain")
            self.assertEqual("requires native authentication" in detail, identified)
            self.assertNotIn("private", detail + output.getvalue())
            self.assertNotIn(diagnostic.decode(), detail + output.getvalue())
            launch.assert_called_once()
            process.wait.assert_called_once()
            process.kill.assert_not_called()

    def test_cli_windows_node_launcher_uses_explicit_script_and_regular_stdin_without_shell(self):
        script = self.directory / "official-entry.mjs"
        script.write_text("// public mock Node CLI entry; no execution occurs")
        config = json.loads(json.dumps(self.config))
        config["native"]["entry_script"] = str(script)
        with patch("workbuddy.cli_adapter.sys.platform", "win32"):
            adapter = create_adapter(config)
        message = self.message()
        message["entry_script"] = "task-cannot-override-the-launcher.cmd"
        start, _ = self.fake_launch(json.dumps(self.result()).encode())
        def checked_start(argv, **kwargs):
            self.assertTrue(stat.S_ISREG(os.fstat(kwargs["stdin"].fileno()).st_mode))
            return start(argv, **kwargs)
        with patch("workbuddy.cli_adapter.sys.platform", "win32"), patch("workbuddy.cli_adapter.subprocess.Popen", side_effect=checked_start) as launch:
            self.assertEqual(adapter.deliver(message)[0], "delivered")
        launch.assert_called_once()
        argv = launch.call_args.args[0]
        self.assertEqual(argv[:4], [str(self.command.resolve()), str(script.resolve()), "--resume", "original-session-test"])
        self.assertEqual(argv[-1], HANDOFF_INSTRUCTION)
        self.assertFalse(launch.call_args.kwargs["shell"])
        self.assertNotIn(message["entry_script"], argv)
        self.assertNotIn("--tools", argv)

    def test_cli_node_entry_requires_explicit_existing_javascript_file(self):
        for name in ("entry.js", "entry.mjs", "entry.cjs", "codebuddy"):
            script = self.directory / name
            script.write_text("// mock public JS entry")
            config = json.loads(json.dumps(self.config))
            config["native"]["entry_script"] = str(script)
            with self.subTest(name=name):
                self.assertEqual(create_adapter(config).entry_script, str(script.resolve()))
        for value in ("relative.js", str(self.directory), str(self.directory / "missing.js"), ""):
            config = json.loads(json.dumps(self.config))
            config["native"]["entry_script"] = value
            with self.subTest(value=value), self.assertRaises(ConfigurationError):
                create_adapter(config)
        for extension in (".cmd", ".bat", ".sh"):
            wrapper = self.directory / ("entry" + extension)
            wrapper.write_text("mock shell wrapper")
            config = json.loads(json.dumps(self.config))
            config["native"]["entry_script"] = str(wrapper)
            with self.subTest(extension=extension), self.assertRaisesRegex(ConfigurationError, "requires_node_js"):
                create_adapter(config)


if __name__ == "__main__":
    unittest.main()
