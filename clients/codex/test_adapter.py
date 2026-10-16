"""Local-only adapter tests; no messages are sent to real assistants."""

import sys
import contextlib
import io
import json
import os
import tempfile
import threading
import unittest
from pathlib import Path

CLIENTS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(CLIENTS))
from unittest.mock import Mock, patch
import subprocess
from codex.adapter import CodexAdapter, create_adapter
from codex.app_server_adapter import CodexAppServerAdapter, ProxyError, ProxyRPC
from codex.desktop_mcp_adapter import CodexDesktopMcpAdapter, DesktopMcpRPC, McpError, PROTOCOL_VERSION
from shared.transport import ConfigurationError
from tests.support import envelope


class AdapterTests(unittest.TestCase):
    def test_codex_uses_argv_no_shell_and_uncertain_on_started_failure(self):
        with patch("codex.adapter.shutil.which", return_value="/fake/codex"):
            adapter = CodexAdapter({"endpoint": "endpoint-test", "native": {"thread_id": "thread literal ; $(no shell)", "executable": "codex"}})
        process = Mock()
        process.wait.return_value = 1
        with patch("codex.adapter.subprocess.Popen", return_value=process) as launch:
            outcome, _ = adapter.deliver(envelope())
        self.assertEqual(outcome, "uncertain")
        argv = launch.call_args.args[0]
        self.assertEqual(argv[:4], [adapter.executable, "queue", "--thread", "thread literal ; $(no shell)"])
        self.assertNotIn("shell", launch.call_args.kwargs)
        self.assertEqual(launch.call_args.kwargs["stdout"], subprocess.DEVNULL)
        self.assertEqual(launch.call_args.kwargs["stderr"], subprocess.DEVNULL)
        launch.assert_called_once()

    def test_codex_missing_executable_is_preflight_and_unverified_modes_not_started(self):
        config = {"endpoint": "endpoint-test", "native": {"thread_id": "thread-test"}}
        with patch("codex.adapter.shutil.which", return_value=None), self.assertRaises(ConfigurationError):
            CodexAdapter(config)
        with patch("codex.adapter.shutil.which", return_value="codex"):
            adapter = CodexAdapter(config)
        with patch("codex.adapter.subprocess.Popen") as launch:
            outcome, _ = adapter.deliver(envelope(mode="interrupt"))
        self.assertEqual(outcome, "failed")
        launch.assert_not_called()

    def test_codex_retains_resolved_absolute_path_and_rejects_windows_wrappers(self):
        config = {"endpoint": "endpoint-test", "native": {"thread_id": "thread-test"}}
        with patch("codex.adapter.shutil.which", return_value="/fake/bin/codex") as which:
            adapter = CodexAdapter(config)
        self.assertTrue(Path(adapter.executable).is_absolute())
        process = Mock()
        process.wait.return_value = 0
        with patch("codex.adapter.shutil.which", return_value="/changed/path/codex") as changed, patch("codex.adapter.subprocess.Popen", return_value=process) as launch:
            adapter.deliver(envelope())
        which.assert_called_once()
        changed.assert_not_called()
        self.assertEqual(launch.call_args.args[0][0], str(Path("/fake/bin/codex").resolve()))
        for extension in (".cmd", ".BAT"):
            with self.subTest(extension=extension), patch("codex.adapter.sys.platform", "win32"), patch("codex.adapter.shutil.which", return_value="/fake/codex" + extension):
                with self.assertRaisesRegex(ConfigurationError, "requires_actual_exe"):
                    CodexAdapter(config)


class AppServerAdapterTests(unittest.TestCase):
    def adapter(self, **changes):
        native = {"transport": "app-server", "result_mode": "steer", "thread_id": "original-thread-test", "timeout_seconds": 2}
        native.update(changes)
        with patch("codex.adapter.shutil.which", return_value="/fake/codex"):
            return create_adapter({"endpoint": "endpoint-test", "native": native})

    def metadata(self, state="active"):
        return {"thread": {"id": "original-thread-test", "status": {"type": state}, "canAcceptDirectInput": True, "preview": "PRIVATE HISTORY MUST NOT BE FORWARDED"}}

    def responses(self, ack=None):
        return [
            {}, self.metadata(), {"data": [{"id": "current-turn-test", "status": "inProgress", "items": []}]},
            {"turnId": "current-turn-test"} if ack is None else ack,
        ]

    def test_busy_result_reads_metadata_then_steers_guarded_current_turn_once(self):
        adapter = self.adapter()
        self.assertIsInstance(adapter, CodexAppServerAdapter)
        rpc = Mock()
        rpc.call.side_effect = self.responses()
        with patch("codex.app_server_adapter.ProxyRPC", return_value=rpc) as connect, contextlib.redirect_stdout(io.StringIO()) as output:
            outcome, detail = adapter.deliver(envelope(kind="result"))
        self.assertEqual(outcome, "delivered")
        connect.assert_called_once_with(adapter.executable, 2)
        self.assertEqual([call.args[0] for call in rpc.call.call_args_list], ["initialize", "thread/read", "thread/turns/list", "turn/steer"])
        rpc.notify.assert_called_once_with("initialized")
        self.assertEqual(rpc.call.call_args_list[1].args[1], {"threadId": "original-thread-test", "includeTurns": False})
        self.assertEqual(rpc.call.call_args_list[2].args[1], {"threadId": "original-thread-test", "limit": 1, "sortDirection": "desc", "itemsView": "notLoaded"})
        steer = rpc.call.call_args_list[3].args[1]
        self.assertEqual(steer["expectedTurnId"], "current-turn-test")
        self.assertEqual(steer["threadId"], "original-thread-test")
        self.assertIn("kind=result", steer["input"][0]["text"])
        self.assertNotIn("PRIVATE HISTORY", json.dumps(steer) + detail + output.getvalue())
        self.assertEqual(set(steer), {"threadId", "expectedTurnId", "input", "clientUserMessageId"})
        rpc.close.assert_called_once()

    def test_proxy_jsonl_handshake_keeps_original_daemon_and_ignores_notifications(self):
        adapter = self.adapter()
        captured = []
        class Capture(io.BytesIO):
            def close(self):
                captured.append(self.getvalue())
                super().close()
        replies = [
            {"method": "turn/notification", "params": {"private": "SECRET NEVER LOGGED"}},
            {"id": 1, "result": {}}, {"id": 2, "result": self.metadata()},
            {"id": 3, "result": {"data": [{"id": "current-turn-test", "status": "inProgress"}]}},
            {"id": 4, "result": {"turnId": "current-turn-test"}},
        ]
        process = Mock()
        process.stdin = Capture()
        process.stdout = io.BytesIO(b"".join(json.dumps(value).encode() + b"\n" for value in replies))
        process.poll.return_value = None
        process.wait.return_value = 0
        with patch("codex.app_server_adapter.subprocess.Popen", return_value=process) as launch, contextlib.redirect_stdout(io.StringIO()) as output:
            outcome, detail = adapter.deliver(envelope(kind="result"))
        self.assertEqual(outcome, "delivered")
        launch.assert_called_once()
        self.assertEqual(launch.call_args.args[0], [adapter.executable, "app-server", "proxy"])
        self.assertFalse(launch.call_args.kwargs["shell"])
        self.assertEqual(launch.call_args.kwargs["stderr"], subprocess.DEVNULL)
        sent = [json.loads(line) for line in captured[0].splitlines()]
        self.assertEqual([request["method"] for request in sent], ["initialize", "initialized", "thread/read", "thread/turns/list", "turn/steer"])
        self.assertNotIn("SECRET", detail + output.getvalue() + json.dumps(sent))
        self.assertNotIn("queue", launch.call_args.args[0])

    def test_turn_change_bad_ack_rpc_error_or_timeout_is_uncertain_without_queue_fallback(self):
        cases = [
            [ProxyError("proxy_timeout")],
            self.responses(ack={"turnId": "changed-turn"}),
            [{}, self.metadata(), {"data": [{"id": "completed-turn", "status": "completed"}]}],
            [{}, self.metadata(), {"data": [{"id": "current-turn-test", "status": "inProgress"}]}, ProxyError("native_rpc_rejected_or_unconfirmed")],
        ]
        for replies in cases:
            adapter = self.adapter()
            rpc = Mock()
            rpc.call.side_effect = replies
            with self.subTest(replies=len(replies)), patch("codex.app_server_adapter.ProxyRPC", return_value=rpc) as connect, patch("codex.adapter.subprocess.Popen") as old_queue:
                outcome, _ = adapter.deliver(envelope(kind="result"))
            self.assertEqual(outcome, "uncertain")
            connect.assert_called_once()
            old_queue.assert_not_called()
            self.assertLessEqual(sum(call.args[0] == "turn/steer" for call in rpc.call.call_args_list), 1)
            rpc.close.assert_called_once()

    def test_unverified_idle_or_unloaded_thread_never_starts_or_resumes(self):
        for state in ("idle", "notLoaded", "systemError"):
            adapter = self.adapter()
            rpc = Mock()
            rpc.call.side_effect = [{}, self.metadata(state)]
            with self.subTest(state=state), patch("codex.app_server_adapter.ProxyRPC", return_value=rpc):
                self.assertEqual(adapter.deliver(envelope(kind="result"))[0], "uncertain")
            self.assertEqual([call.args[0] for call in rpc.call.call_args_list], ["initialize", "thread/read"])

    def test_verified_idle_start_keeps_same_thread_and_default_tools(self):
        adapter = self.adapter(idle_start_verified=True)
        rpc = Mock()
        rpc.call.side_effect = [{}, self.metadata("idle"), {"turn": {"id": "idle-result-turn", "status": "inProgress"}}]
        with patch("codex.app_server_adapter.ProxyRPC", return_value=rpc):
            self.assertEqual(adapter.deliver(envelope(kind="result"))[0], "delivered")
        self.assertEqual([call.args[0] for call in rpc.call.call_args_list], ["initialize", "thread/read", "turn/start"])
        params = rpc.call.call_args_list[-1].args[1]
        self.assertEqual(params["threadId"], "original-thread-test")
        self.assertEqual(set(params), {"threadId", "input", "clientUserMessageId"})

    def test_result_only_transport_rejects_new_tasks_or_control_before_connection(self):
        adapter = self.adapter()
        messages = [envelope(), envelope(kind="result", mode="interrupt")]
        foreign = envelope(kind="result")
        foreign["recipient"] = "another-endpoint"
        messages.append(foreign)
        with patch("codex.app_server_adapter.ProxyRPC") as connect:
            for message in messages:
                self.assertEqual(adapter.deliver(message)[0], "failed")
        connect.assert_not_called()

    def test_proxy_rejects_invalid_or_truncated_frames_without_logging_native_text(self):
        for raw in (b"private non-JSON output\n", b'{"id":1,"result":{}}', b'{"id":1,"error":{"message":"private rejection"}}\n'):
            process = Mock()
            process.stdin = io.BytesIO()
            process.stdout = io.BytesIO(raw)
            process.poll.return_value = None
            process.wait.return_value = 0
            with self.subTest(raw=raw), patch("codex.app_server_adapter.subprocess.Popen", return_value=process) as launch, contextlib.redirect_stdout(io.StringIO()) as output:
                rpc = ProxyRPC("/fake/codex", 1)
                try:
                    with self.assertRaises(ProxyError) as error:
                        rpc.call("initialize", {})
                finally:
                    rpc.close()
            launch.assert_called_once()
            self.assertNotIn("private", str(error.exception) + output.getvalue())

    def test_proxy_deadline_stops_only_its_single_helper_process(self):
        released = threading.Event()
        class BlockedOutput:
            def readline(self, _limit):
                released.wait(1)
                return b""
            def close(self):
                released.set()
        process = Mock()
        process.stdin = io.BytesIO()
        process.stdout = BlockedOutput()
        process.poll.return_value = None
        process.wait.return_value = 0
        process.kill.side_effect = released.set
        with patch("codex.app_server_adapter.subprocess.Popen", return_value=process) as launch:
            rpc = ProxyRPC("/fake/codex", 5)
            # Exercise the actual race deterministically: the caller notices the
            # deadline while the timer has not run, then cleanup cancels it.
            rpc.timer.cancel()
            rpc.deadline = 0
            try:
                with self.assertRaisesRegex(ProxyError, "proxy_timeout"):
                    rpc.call("initialize", {})
                rpc.expire()  # A later timer callback must not kill twice.
            finally:
                rpc.close()
        launch.assert_called_once()
        process.kill.assert_called_once()


class DesktopMcpAdapterTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        launcher_name = "node.exe" if sys.platform == "win32" else "launch_codex_app_tools_mcp"
        self.launcher = self.root / launcher_name
        self.launcher.write_text("official launcher fixture, never executed")
        self.launcher.chmod(0o700)
        self.server = self.root / "server.mjs"
        self.server.write_text("public server fixture, never executed")
        self.native = {
            "transport": "desktop-mcp", "command": str(self.launcher),
            "args": [str(self.server)], "cwd": str(self.root),
            "thread_id": "bound-original-thread", "timeout_seconds": 2,
        }
        self.host = {
            "CODEX_THREAD_ID": "official-host-caller", "CODEX_APP_TOOLS_PIPE_PATH": "official-host-provided-path",
            "HOME": str(self.root), "PATH": "/usr/bin:/bin", "CODEX_MCP_NODE_PATH": "/public/runtime/node",
            "SECRET_TOKEN": "must-not-be-inherited", "NODE_OPTIONS": "must-not-be-inherited",
            "CODEX_APP_TOOLS_CALLER_HOST_ID": "not-in-approved-environment",
        }

    def adapter(self, **changes):
        native = dict(self.native, **changes)
        with patch.dict(os.environ, self.host, clear=True):
            return create_adapter({"endpoint": "endpoint-test", "native": native})

    def initialization(self):
        return {"protocolVersion": PROTOCOL_VERSION, "capabilities": {"tools": {}}, "serverInfo": {"name": "codex-app-tools", "version": "0.1.0"}}

    def tool_list(self):
        return {"tools": [{"name": "send_message_to_thread", "inputSchema": {
            "type": "object", "required": ["threadId", "prompt"],
            "properties": {"threadId": {"type": "string"}, "prompt": {"type": "string"}, "model": {"type": "string"}},
        }}]}

    def acknowledgement(self, thread="bound-original-thread"):
        return {"content": [{"type": "text", "text": json.dumps({"threadId": thread})}], "isError": False}

    def test_mcp_result_uses_fixed_tool_target_and_official_host_caller_once(self):
        adapter = self.adapter()
        self.assertIsInstance(adapter, CodexDesktopMcpAdapter)
        rpc = Mock()
        rpc.call.side_effect = [self.initialization(), self.tool_list(), self.acknowledgement()]
        message = envelope(kind="result")
        message["caller_thread_id"] = "untrusted-envelope-caller"
        with patch("codex.desktop_mcp_adapter.DesktopMcpRPC", return_value=rpc) as connect, contextlib.redirect_stdout(io.StringIO()) as output:
            outcome, detail = adapter.deliver(message)
        self.assertEqual(outcome, "delivered")
        connect.assert_called_once_with(adapter.command, adapter.cwd, adapter.environment, 2)
        self.assertEqual([call.args[0] for call in rpc.call.call_args_list], ["initialize", "tools/list", "tools/call"])
        rpc.notify.assert_called_once_with("notifications/initialized")
        params = rpc.call.call_args_list[-1].args[1]
        self.assertEqual(params["name"], "send_message_to_thread")
        self.assertEqual(set(params["arguments"]), {"threadId", "prompt"})
        self.assertEqual(params["arguments"]["threadId"], "bound-original-thread")
        self.assertIn("kind=result, not a new task", params["arguments"]["prompt"])
        self.assertIn('"caller_thread_id":"untrusted-envelope-caller"', params["arguments"]["prompt"])
        self.assertEqual(params["_meta"]["threadId"], "official-host-caller")
        self.assertEqual(set(params["_meta"]), {"threadId", "callId"})
        self.assertNotIn("official-host-provided-path", output.getvalue() + detail)
        rpc.close.assert_called_once()

    def test_mcp_stdio_is_jsonrpc_no_shell_and_filters_environment(self):
        adapter = self.adapter()
        captured = []
        class Capture(io.BytesIO):
            def close(self):
                captured.append(self.getvalue())
                super().close()
        values = [
            {"jsonrpc": "2.0", "method": "notifications/private", "params": {"private": "SECRET NATIVE HISTORY"}},
            {"jsonrpc": "2.0", "id": 1, "result": self.initialization()},
            {"jsonrpc": "2.0", "id": 2, "result": self.tool_list()},
            {"jsonrpc": "2.0", "id": 3, "result": self.acknowledgement()},
        ]
        process = Mock()
        process.stdin = Capture()
        process.stdout = io.BytesIO(b"".join(json.dumps(value).encode() + b"\n" for value in values))
        process.poll.return_value = None
        process.wait.return_value = 0
        with patch("codex.desktop_mcp_adapter.subprocess.Popen", return_value=process) as launch, contextlib.redirect_stdout(io.StringIO()) as output:
            outcome, detail = adapter.deliver(envelope(kind="result"))
        self.assertEqual(outcome, "delivered")
        launch.assert_called_once()
        self.assertEqual(launch.call_args.args[0], [str(self.launcher.resolve()), str(self.server.resolve())])
        self.assertFalse(launch.call_args.kwargs["shell"])
        self.assertEqual(launch.call_args.kwargs["cwd"], str(self.root.resolve()))
        self.assertEqual(launch.call_args.kwargs["stderr"], subprocess.DEVNULL)
        environment = launch.call_args.kwargs["env"]
        self.assertEqual(environment["CODEX_APP_TOOLS_PIPE_PATH"], self.host["CODEX_APP_TOOLS_PIPE_PATH"])
        self.assertNotIn("SECRET_TOKEN", environment)
        self.assertNotIn("NODE_OPTIONS", environment)
        self.assertNotIn("CODEX_APP_TOOLS_CALLER_HOST_ID", environment)
        sent = [json.loads(line) for line in captured[0].splitlines()]
        self.assertEqual([request["method"] for request in sent], ["initialize", "notifications/initialized", "tools/list", "tools/call"])
        self.assertTrue(all(request["jsonrpc"] == "2.0" for request in sent))
        self.assertNotIn("SECRET NATIVE HISTORY", detail + output.getvalue() + json.dumps(sent))
        self.assertNotIn("official-host-provided-path", json.dumps(sent))

    def test_mcp_discovery_failure_never_attempts_native_input(self):
        wrong_protocol = dict(self.initialization(), protocolVersion="incompatible")
        wrong_schema = self.tool_list()
        wrong_schema["tools"][0]["inputSchema"]["required"].append("permissionOverride")
        for replies in (
            [McpError("desktop_mcp_rpc_unconfirmed")],
            [wrong_protocol],
            [self.initialization(), McpError("desktop_mcp_closed")],
            [self.initialization(), {"tools": []}],
            [self.initialization(), wrong_schema],
        ):
            rpc = Mock()
            rpc.call.side_effect = replies
            with self.subTest(replies=len(replies)), patch("codex.desktop_mcp_adapter.DesktopMcpRPC", return_value=rpc):
                outcome, _ = self.adapter().deliver(envelope(kind="result"))
            self.assertEqual(outcome, "failed")
            self.assertNotIn("tools/call", [call.args[0] for call in rpc.call.call_args_list])
            rpc.close.assert_called_once()

    def test_mcp_attempted_error_missing_or_wrong_ack_is_uncertain_without_fallback(self):
        for acknowledgement in (
            McpError("desktop_mcp_timeout"), self.acknowledgement("different-thread"),
            {"content": [], "isError": False},
            {"content": [{"type": "text", "text": "private rejection"}], "isError": True},
            {"content": [{"type": "text", "text": "private non-JSON"}], "isError": False},
            {"content": [{"type": "text", "text": '{"threadId":"wrong","threadId":"bound-original-thread"}'}], "isError": False},
        ):
            rpc = Mock()
            rpc.call.side_effect = [self.initialization(), self.tool_list(), acknowledgement]
            with self.subTest(ack=type(acknowledgement).__name__), patch("codex.desktop_mcp_adapter.DesktopMcpRPC", return_value=rpc) as connect, patch("codex.adapter.subprocess.Popen") as queue_launch, contextlib.redirect_stdout(io.StringIO()) as output:
                outcome, detail = self.adapter().deliver(envelope(kind="result"))
            self.assertEqual(outcome, "uncertain")
            connect.assert_called_once()
            queue_launch.assert_not_called()
            self.assertEqual(sum(call.args[0] == "tools/call" for call in rpc.call.call_args_list), 1)
            self.assertNotIn("private", output.getvalue() + detail)
            rpc.close.assert_called_once()

    def test_mcp_requires_host_identity_and_explicit_launcher_configuration(self):
        for missing in ("CODEX_THREAD_ID", "CODEX_APP_TOOLS_PIPE_PATH"):
            host = {key: value for key, value in self.host.items() if key != missing}
            with self.subTest(missing=missing), patch.dict(os.environ, host, clear=True), self.assertRaises(ConfigurationError):
                create_adapter({"endpoint": "endpoint-test", "native": self.native})
        shell = self.root / "sh"
        shell.write_text("not an allowed launcher")
        shell.chmod(0o700)
        for changes in (
            {"command": str(shell)}, {"args": ["server.mjs"]},
            {"args": [str(self.server), "--interaction-client-id"]}, {"cwd": "."},
            {"thread_id": "\nspoofed"}, {"timeout_seconds": float("nan")},
            {"environment": {"CODEX_THREAD_ID": "caller override"}},
        ):
            with self.subTest(changes=list(changes)), self.assertRaises(ConfigurationError):
                self.adapter(**changes)

    def test_mcp_windows_node_exe_uses_server_path_and_rejects_cmd(self):
        node = self.root / "node.exe"
        node.write_bytes(b"public executable fixture")
        wrapper = self.root / "node.cmd"
        wrapper.write_text("wrapper fixture")
        with patch("codex.desktop_mcp_adapter.sys.platform", "win32"):
            adapter = self.adapter(command=str(node))
            self.assertEqual(adapter.command, [str(node.resolve()), str(self.server.resolve())])
            with self.assertRaisesRegex(ConfigurationError, "requires_actual_exe"):
                self.adapter(command=str(wrapper))

    def test_mcp_binding_and_result_only_enforcement_before_connection(self):
        adapter = self.adapter()
        adapter.validate_binding({"driver": "codex", "session_id": adapter.thread, "modes": ["queue"]})
        for binding in (
            {"driver": "workbuddy", "session_id": adapter.thread, "modes": ["queue"]},
            {"driver": "codex", "session_id": "different", "modes": ["queue"]},
            {"driver": "codex", "session_id": adapter.thread, "modes": ["steer"]},
        ):
            with self.assertRaises(ConfigurationError):
                adapter.validate_binding(binding)
        foreign = envelope(kind="result")
        foreign["recipient"] = "another-endpoint"
        with patch("codex.desktop_mcp_adapter.DesktopMcpRPC") as connect:
            for message in (envelope(), envelope(kind="control"), envelope(kind="result", mode="interrupt"), foreign):
                self.assertEqual(adapter.deliver(message)[0], "failed")
        connect.assert_not_called()

    def test_mcp_truncated_invalid_frames_and_deadline_stop_only_helper(self):
        for raw in (b'{"jsonrpc":"2.0","id":1,"result":{}}', b"private output\n", b'{"id":1,"result":{}}\n'):
            process = Mock()
            process.stdin = io.BytesIO()
            process.stdout = io.BytesIO(raw)
            process.poll.return_value = None
            process.wait.return_value = 0
            with self.subTest(raw=raw), patch("codex.desktop_mcp_adapter.subprocess.Popen", return_value=process):
                rpc = DesktopMcpRPC(["/public/node", "/public/server.mjs"], "/public", {}, 2)
                try:
                    with self.assertRaises(McpError):
                        rpc.call("initialize", {})
                finally:
                    rpc.close()
        released = threading.Event()
        class BlockedOutput:
            def readline(self, _limit):
                released.wait(1)
                return b""
            def close(self):
                released.set()
        process = Mock()
        process.stdin = io.BytesIO()
        process.stdout = BlockedOutput()
        process.poll.return_value = None
        process.wait.return_value = 0
        process.kill.side_effect = released.set
        with patch("codex.desktop_mcp_adapter.subprocess.Popen", return_value=process) as launch:
            rpc = DesktopMcpRPC(["/public/node", "/public/server.mjs"], "/public", {}, 5)
            rpc.timer.cancel()
            rpc.deadline = 0
            try:
                with self.assertRaisesRegex(McpError, "desktop_mcp_timeout"):
                    rpc.call("initialize", {})
                rpc.expire()
            finally:
                rpc.close()
        launch.assert_called_once()
        process.kill.assert_called_once()


if __name__ == "__main__":
    unittest.main()
