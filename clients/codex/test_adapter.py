"""Local-only adapter tests; no messages are sent to real assistants."""

import sys
import unittest
from pathlib import Path

CLIENTS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(CLIENTS))
from unittest.mock import Mock, patch
import subprocess
from codex.adapter import CodexAdapter
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


if __name__ == "__main__":
    unittest.main()
