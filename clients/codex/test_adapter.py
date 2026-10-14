"""Local-only adapter tests; no messages are sent to real assistants."""

import sys
import contextlib
import io
import json
import threading
import unittest
from pathlib import Path

CLIENTS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(CLIENTS))
from unittest.mock import Mock, patch
import subprocess
from codex.adapter import CodexAdapter, create_adapter
from codex.app_server_adapter import CodexAppServerAdapter, ProxyError, ProxyRPC
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


if __name__ == "__main__":
    unittest.main()
