"""Local-only adapter tests; no messages are sent to real assistants."""

import sys
import unittest
from pathlib import Path

CLIENTS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(CLIENTS))
import importlib.util
from unittest.mock import patch
from shared.transport import ConfigurationError
from tests.support import envelope

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


if __name__ == "__main__":
    unittest.main()
