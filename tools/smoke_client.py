"""Exercise a real Hub process and Python protocol with synthetic adapters only."""

from __future__ import annotations

import argparse
import hashlib
import json
import socket
import subprocess
import sys
import tempfile
from datetime import datetime, timedelta, timezone
from pathlib import Path
from urllib.request import ProxyHandler, Request, build_opener

REPO = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(REPO / "clients"))
from shared.runtime import HubAPI, drain, verify_binding


def run(binary: str) -> None:
    with tempfile.TemporaryDirectory(prefix="agi-hub-synthetic-") as temporary:
        root = Path(temporary).resolve()
        data, work = root / "data", root / "work"
        work.mkdir()
        subprocess.run([binary, "init", "--data", str(data)], check=True, stdout=subprocess.DEVNULL)
        cfg = json.loads((data / "config.json").read_text())
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        cfg.update(listen=f"127.0.0.1:{port}", leader="synthetic-origin", workspaces={"work": str(work)})
        endpoints = ("synthetic-origin", "synthetic-worker")
        # These flags apply exclusively to the synthetic test adapters, never
        # to a real assistant or the user's installation.
        cfg["bindings"] = [{"endpoint": {
            "id": eid, "device_id": cfg["device_id"], "session_id": eid,
            "enabled": True, "idle_wake_verified": True, "modes": ["queue"],
            "driver": "synthetic-test",
        }, "driver": {"type": "external"}} for eid in endpoints]
        (data / "config.json").write_text(json.dumps(cfg))
        for eid in endpoints:
            subprocess.run([binary, "client-config", "--data", str(data), "--endpoint", eid,
                            "--file", str(root / eid / "client.json")], check=True, stdout=subprocess.DEVNULL)
        process = subprocess.Popen([binary, "serve", "--data", str(data)], stdout=subprocess.PIPE,
                                   stderr=subprocess.PIPE, text=True)
        try:
            assert "listening" in process.stdout.readline(), "synthetic Hub failed to start"
            opener = build_opener(ProxyHandler({}))

            def admin(method, path, body=None):
                request = Request("http://" + cfg["listen"] + path, method=method,
                                  data=None if body is None else json.dumps(body).encode(),
                                  headers={"Authorization": "Bearer " + cfg["admin_token"],
                                           "Content-Type": "application/json"})
                with opener.open(request, timeout=5) as response:
                    return json.load(response)

            for eid in endpoints:
                admin("POST", "/v1/session", {"endpoint": eid, "state": "idle"})
            worker = HubAPI(json.loads((root / "synthetic-worker" / "client.json").read_text()))
            origin = HubAPI(json.loads((root / "synthetic-origin" / "client.json").read_text()))
            calls, results = [], []

            class SyntheticWorker:
                def validate_binding(self, binding):
                    assert binding["driver"] == "synthetic-test" and binding["session_id"] == binding["id"]

                def deliver(self, envelope):
                    calls.append(envelope["message_id"])
                    assert json.loads(envelope["content"])["goal"] == "Produce synthetic test artifact"
                    common = {"task_id": envelope["task_id"], "endpoint": "synthetic-worker", "run_id": "synthetic-run"}
                    worker.post("report", dict(common, event_id="synthetic-accepted", state="accepted", summary="Simulated acceptance"))
                    (work / "result.txt").write_text("Synthetic integration artifact\n")
                    # Completion can arrive before the transport receipt.
                    worker.post("report", dict(common, event_id="synthetic-done", state="succeeded", summary="Simulated result",
                                              artifacts=[{"device_id": cfg["device_id"], "workspace_id": "work", "path": "result.txt"}]))
                    return "delivered", "Synthetic transport; no native assistant"

            class SyntheticOrigin:
                def validate_binding(self, binding):
                    assert binding["session_id"] == "synthetic-origin"

                def deliver(self, envelope):
                    assert envelope["kind"] == "result"
                    results.append(json.loads(envelope["content"]))
                    return "delivered", "Synthetic result observation"

            wa, oa = SyntheticWorker(), SyntheticOrigin()
            verify_binding(worker, wa)
            verify_binding(origin, oa)
            with worker.subscribe(), origin.subscribe():
                admin("POST", "/v1/tasks", {
                    "id": "synthetic-task", "origin": endpoints[0], "recipient": endpoints[1],
                    "goal": "Produce synthetic test artifact", "mode": "queue", "require_artifact": True,
                    "resources": ["workspace:work"], "authorized_actions": ["Write synthetic result.txt"], "max_handoffs": 1,
                    "deadline": (datetime.now(timezone.utc) + timedelta(minutes=5)).isoformat(timespec="seconds"),
                })
                drain(worker, wa)
                drain(origin, oa)
                drain(worker, wa)
                drain(origin, oa)
            snapshot = admin("GET", "/v1/state")
            assert len(calls) == 1 and len(results) == 1
            assert snapshot["tasks"]["synthetic-task"]["execution"] == "succeeded"
            assert results[0]["artifacts"][0]["sha256"] == hashlib.sha256((work / "result.txt").read_bytes()).hexdigest()
            assert snapshot["resource_owners"] == {}
            print(json.dumps({"real_hub_python_protocol": True, "synthetic_task": True, "dispatches": 1,
                              "verified_artifacts": 1, "returned_results": 1, "native_assistant_messages": 0}))
        finally:
            process.terminate()
            process.communicate(timeout=5)
            # Windows TerminateProcess is not a graceful SIGTERM exit.
            if sys.platform != "win32":
                assert process.returncode == 0, "synthetic Hub did not shutdown normally"


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--hub", required=True, type=Path, help="built Hub executable for this platform")
    run(str(parser.parse_args().hub.resolve(strict=True)))
