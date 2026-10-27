"""Real temporary Hub + real shared consumer, with local native substitutes only.

These regressions do not call Grok, WorkBuddy or a model.
The substitutes first receive a full handoff and send two scoped proof messages.
Grok verification uses the real administrative evidence/CAS transaction, never
fake idle. A WorkBuddy session without an actual current observation must remain
blocked with an actionable diagnostic rather than silently advertise ready.
"""

import hashlib
import hmac
import importlib.util
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import sys
import tempfile
import threading
import time
import unittest
import urllib.request

CLIENTS = Path(__file__).resolve().parents[1]
ROOT = CLIENTS.parent
sys.path.insert(0, str(CLIENTS))
from tests.support import LocalHTTP, respond
from workbuddy.cli_adapter import WorkBuddyCLIAdapter

spec = importlib.util.spec_from_file_location("grok_restart_fixture", CLIENTS / "grok-routine" / "adapter.py")
grok = importlib.util.module_from_spec(spec)
spec.loader.exec_module(grok)


def stop(process):
    if process is not None and process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=3)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=3)
    if process is not None and process.stdout is not None:
        process.stdout.close()


class ExternalClientRecoveryTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.build = tempfile.TemporaryDirectory(prefix="hub-client-recovery-binary-")
        cls.addClassCleanup(cls.build.cleanup)
        cls.binary = Path(cls.build.name) / ("agi-hub.exe" if os.name == "nt" else "agi-hub")
        environment = dict(os.environ, GOPROXY="off", GOSUMDB="off")
        subprocess.run(["go", "build", "-o", str(cls.binary), "./cmd/agi-hub"], cwd=ROOT,
                       env=environment, check=True, stdout=subprocess.DEVNULL)

    def setUp(self):
        temp = tempfile.TemporaryDirectory(prefix="hub-client-recovery-")
        self.addCleanup(temp.cleanup)
        self.folder = Path(temp.name).resolve()
        self.data = self.folder / "hub"
        subprocess.run([str(self.binary), "init", "--data", str(self.data)], check=True, stdout=subprocess.DEVNULL)
        self.config_file = self.data / "config.json"
        self.config = json.loads(self.config_file.read_text())
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            self.config["listen"] = "127.0.0.1:" + str(listener.getsockname()[1])
        self.base = "http://" + self.config["listen"]
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        self.session = "native-fixture-session"
        self.worker_token = self.token("worker")
        self.origin_token = self.token("origin")
        self.native_calls = self.folder / "native-calls.jsonl"
        self.process = None
        self.addCleanup(lambda: stop(self.process))

    def token(self, endpoint):
        return hmac.new(self.config["admin_token"].encode(), ("agi-hub/endpoint/" + endpoint).encode(), hashlib.sha256).hexdigest()

    def request(self, route, token=None, payload=None, headers=None):
        encoded = None if payload is None else json.dumps(payload).encode()
        request = urllib.request.Request(self.base + route, data=encoded, headers={
            "Authorization": "Bearer " + (token or self.config["admin_token"]),
            "Origin": self.base, "Content-Type": "application/json", **(headers or {}),
        })
        with self.opener.open(request, timeout=2) as response:
            return json.load(response)

    def start(self):
        self.process = subprocess.Popen([str(self.binary), "serve", "--data", str(self.data)],
                                        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        for _ in range(80):
            try:
                if self.request("/v1/health")["ready"]:
                    return
            except OSError:
                pass
            self.assertIsNone(self.process.poll(), "fixture Hub exited before health")
            time.sleep(0.025)
        self.fail("fixture Hub never became ready")

    def record_proof(self):
        # The fake native actor uses its own endpoint token, not administrator
        # privileges. A 202 alone is deliberately insufficient as proof here.
        for name in ("skill", "helper-nonce"):
            self.request("/v1/mail/send?endpoint=worker", self.worker_token, {
                "id": "native-proof-" + name, "recipient": "origin", "kind": "status",
                "content": "local native fixture verified " + name,
                "origin_ref": "synthetic-native-probe-" + name,
            })

    def register_native_queue(self):
        proof = self.request("/v1/state")["messages"]
        for name in ("skill", "helper-nonce"):
            self.assertEqual(proof["native-proof-" + name]["sender"], "worker")
        self.request("/v1/manage/native-queue", payload={
            "endpoint": "worker", "native_session_id": self.session,
            "verified": True, "evidence_ref": "native-proof-skill+native-proof-helper-nonce",
        })
        saved = json.loads(self.config_file.read_text())["bindings"][1]["endpoint"]
        self.assertTrue(saved["mailbox_native_queue_verified"])
        self.assertFalse(saved["idle_wake_verified"])
        self.assertFalse(saved.get("mailbox_while_busy_verified", False))
        self.assertEqual(self.request("/v1/state")["sessions"]["worker"]["state"], "unknown")

    def exercise(self, driver, restart):
        def binding(endpoint, native_driver, session, peers):
            return {"endpoint": {"id": endpoint, "device_id": self.config["device_id"],
                "driver": native_driver, "session_id": session, "enabled": True,
                "modes": ["queue"], "mailbox_protocol_version": 1,
                "idle_wake_verified": False}, "driver": {"type": "external"}, "allowed_peers": peers}
        self.config["bindings"] = [binding("origin", "codex", "human-entry", ["worker"]),
                                   binding("worker", driver, self.session, ["origin"])]
        self.config_file.write_text(json.dumps(self.config))
        self.start()
        config = {"base_url": self.base, "endpoint": "worker", "endpoint_token": self.worker_token}
        probe = {"message_id": "native-proof", "mailbox": True, "kind": "request", "mode": "queue",
                 "sender": "origin", "recipient": "worker", "correlation_id": "native-proof",
                 "root_origin": {"endpoint": "origin", "device_id": "fixture", "session_id": "human-entry"},
                 "content": "Read the local fixture skill and send its helper nonce. No business work."}
        if driver == "grok-routine":
            def native(handler):
                value = json.loads(handler.rfile.read(int(handler.headers["Content-Length"])))
                message = value.get("record", {})
                if message.get("recipient") != "worker" or not value.get("prompt"):
                    respond(handler, {}, status=400)
                    return
                with self.native_calls.open("a") as output:
                    output.write(json.dumps({"id": message["message_id"]}) + "\n")
                if message["message_id"] == "native-proof":
                    self.record_proof()
                respond(handler, {}, status=202)
            native_server = self.enterContext(LocalHTTP(native))
            config["native"] = {"routine_id": self.session, "url": native_server.base,
                                "body": {"record": "{{envelope}}", "prompt": "{{notification}}"}}
            adapter = grok.GrokRoutineAdapter(config)
        else:
            node = shutil.which("node")
            self.assertIsNotNone(node, "offline WorkBuddy fixture needs Node, never a real assistant")
            fixture = self.folder / "native-fixture.mjs"
            fixture.write_text("""import fs from 'node:fs';
const binding=JSON.parse(fs.readFileSync(new URL('./proof-binding.json',import.meta.url)));
const input=fs.readFileSync(0,'utf8');
const marker='Complete addressed message JSON:\\n'; const message=JSON.parse(input.slice(input.lastIndexOf(marker)+marker.length));
const args=process.argv.slice(2), session=args[args.indexOf('--resume')+1];
fs.appendFileSync(new URL('./native-calls.jsonl',import.meta.url),JSON.stringify({id:message.message_id,session})+'\\n');
if(message.message_id==='native-proof') for(const name of ['skill','helper-nonce']) {
 const response=await fetch(binding.base+'/v1/mail/send?endpoint=worker',{method:'POST',headers:{Authorization:'Bearer '+binding.token,'Content-Type':'application/json',Origin:binding.base},
 body:JSON.stringify({id:'native-proof-'+name,recipient:'origin',kind:'status',content:'local native fixture verified '+name,origin_ref:'synthetic-native-probe-'+name})});
 if(!response.ok)process.exit(2);
}
console.log(JSON.stringify({type:'result',session_id:session,is_error:false}));
""")
            (self.folder / "proof-binding.json").write_text(json.dumps({"base": self.base, "token": self.worker_token}))
            config["native"] = {"transport": "cli", "command": str(Path(node).resolve()),
                "entry_script": str(fixture), "cwd": str(self.folder), "config_dir": str(self.folder),
                "session_id": self.session, "timeout_seconds": 3}
            adapter = WorkBuddyCLIAdapter(config)
        self.assertEqual(adapter.deliver(probe)[0], "delivered", "local native protocol probe itself failed")
        snapshot = self.request("/v1/state")
        for name in ("skill", "helper-nonce"):
            self.assertEqual(snapshot["messages"]["native-proof-" + name]["sender"], "worker")
        self.assertEqual(self.calls(), ["native-proof"])
        if restart and driver == "grok-routine":
            self.register_native_queue()
        elif restart:
            # Record the already completed synthetic probe *before* restart.
            # This is a fixture's prior verified binding, never a repair action.
            self.config["bindings"][1]["endpoint"]["idle_wake_verified"] = (
                self.calls() == ["native-proof"] and all("native-proof-" + name in snapshot["messages"] for name in ("skill", "helper-nonce")))
            self.config_file.write_text(json.dumps(self.config))
            self.request("/v1/endpoint/session?endpoint=worker", self.worker_token,
                         {"state": "idle", "run_id": ""}, {"X-AGI-Hub-Native-Session": self.session})
        self.request("/v1/mail/send?endpoint=origin", self.origin_token, {
            "id": "ordinary-request", "recipient": "worker", "kind": "request",
            "content": "Process this authorized local fixture request.", "origin_ref": "fixture-human-input",
        })
        if restart:
            stop(self.process)
            self.start()
        before = self.request("/v1/endpoint?endpoint=worker", self.worker_token)
        self.assertEqual(before["session"]["state"], "unknown")
        self.assertIs(before["endpoint"]["idle_wake_verified"], restart and driver == "workbuddy")
        client_config = self.folder / "client.json"
        client_config.write_text(json.dumps(config))
        entry = CLIENTS / ("grok-routine" if driver == "grok-routine" else "workbuddy") / "main.py"
        consumer = subprocess.Popen([sys.executable, "-B", str(entry), "--config", str(client_config)],
                                    stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True)
        self.addCleanup(stop, consumer)
        ready, blocked, statuses = threading.Event(), threading.Event(), []
        def read_status():
            for line in consumer.stdout:
                value = json.loads(line)
                statuses.append(value)
                if value.get("state") == "ready":
                    ready.set()
                if value.get("code") in ("needs_native_queue_verification", "needs_native_observation"):
                    blocked.set()
        threading.Thread(target=read_status, daemon=True).start()
        if driver == "workbuddy" or not restart:
            self.assertTrue(blocked.wait(2), f"missing actionable admission status: {statuses}")
            expected = "needs_native_observation" if driver == "workbuddy" else "needs_native_queue_verification"
            report = next(value for value in statuses if value.get("code") == expected)
            self.assertTrue(report["action"])
            self.assertFalse(ready.is_set(), "blocked native transport advertised ready")
            self.assertIsNone(consumer.poll(), "consumer cannot recover from the actual registration event")
            self.assertEqual(self.calls(), ["native-proof"])
            self.assertEqual(self.request("/v1/state")["messages"]["ordinary-request"]["delivery"], "pending")
            if driver == "workbuddy":
                self.assertEqual(self.request("/v1/state")["sessions"]["worker"]["state"], "unknown")
                return  # No official current native observation: do not pretend recovery.
            self.assertIn("agi-hub native-queue", report["action"])
            self.register_native_queue()
        self.assertTrue(ready.wait(2), f"consumer never subscribed: {statuses}")
        # Prove the real event stream, claim path and delivery receipts work.
        # Status needs no model wake and cannot make an ordinary mail eligible.
        self.request("/v1/mail/send?endpoint=origin", self.origin_token, {
            "id": "event-liveness-control", "recipient": "worker", "kind": "status",
            "content": "Local event-stream liveness control only.", "correlation_id": "ordinary-request",
        })
        control_deadline = time.monotonic() + 1
        while time.monotonic() < control_deadline:
            control = self.request("/v1/state")["messages"]["event-liveness-control"]["delivery"]
            if control == "delivered":
                break
            time.sleep(0.01)
        self.assertEqual(control, "delivered", "consumer did not handle the real Hub change/status control")
        end = time.monotonic() + 0.6
        while time.monotonic() < end and "ordinary-request" not in self.calls():
            time.sleep(0.01)
        after = self.request("/v1/state")
        observation = after["sessions"]["worker"]["state"]
        delivery = after["messages"]["ordinary-request"]["delivery"]
        self.assertIn("ordinary-request", self.calls(),
            f"{driver}: consumer=ready, prior_native_proofs=2, restart={restart}, "
            f"idle_wake_verified={before['endpoint']['idle_wake_verified']}, "
            f"session={observation}, status_control={control}, ordinary_request={delivery}, "
            f"native_dispatches_after_probe={len(self.calls()) - 1}")
        self.assertEqual(self.calls(), ["native-proof", "ordinary-request"])
        self.assertEqual(observation, "unknown", "an independent Routine must not invent long-lived session idle")
        self.assertEqual(delivery, "delivered")

    def calls(self):
        if not self.native_calls.exists():
            return []
        return [json.loads(line)["id"] for line in self.native_calls.read_text().splitlines()]

    def test_grok_waits_for_explicit_registration_then_consumes_original_pending_request(self):
        self.exercise("grok-routine", restart=False)

    def test_registered_grok_queue_consumes_after_hub_restart_with_session_still_unknown(self):
        self.exercise("grok-routine", restart=True)

    def test_workbuddy_restart_requires_real_current_native_observation_without_false_ready(self):
        self.exercise("workbuddy", restart=True)


if __name__ == "__main__":
    unittest.main()
