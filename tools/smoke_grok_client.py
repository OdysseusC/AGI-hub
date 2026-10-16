"""Exercise the real Hub and Grok file transport with a local synthetic Routine.

No native assistant, real webhook, existing bridge, or personal config is used.
"""

from __future__ import annotations

import argparse
import hashlib
import importlib.util
import json
import queue
import re
import socket
import subprocess
import sys
import tempfile
import threading
from datetime import datetime, timedelta, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.request import ProxyHandler, Request, build_opener

REPO = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(REPO / "clients"))
from shared.runtime import HubAPI, drain, verify_binding


def run(binary: str, temporary_parent: Path | None = None) -> None:
    spec = importlib.util.spec_from_file_location("grok_smoke_adapter", REPO / "clients/grok-routine/adapter.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    with tempfile.TemporaryDirectory(prefix="agi-hub-grok-synthetic-", dir=temporary_parent) as temporary:
        root = Path(temporary).resolve()
        data, work, inbox = root / "data", root / "work", root / "inbox"
        work.mkdir()
        inbox.mkdir()
        (work / "reports").mkdir()
        subprocess.run([binary, "init", "--data", str(data)], check=True, stdout=subprocess.DEVNULL, timeout=10)
        cfg = json.loads((data / "config.json").read_text())
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        origin_id, grok_id, routine_id = "synthetic-origin", "synthetic-grok", "synthetic-routine"
        cfg.update(listen=f"127.0.0.1:{port}", leader=origin_id, workspaces={"work": str(work)})
        # Verification flags belong only to these synthetic endpoints.
        cfg["bindings"] = [{"endpoint": {
            "id": eid, "device_id": cfg["device_id"], "session_id": session,
            "enabled": True, "idle_wake_verified": True, "modes": ["queue"], "driver": driver,
        }, "driver": {"type": "external"}} for eid, session, driver in (
            (origin_id, origin_id, "synthetic-test"), (grok_id, routine_id, "grok-routine"),
        )]
        (data / "config.json").write_text(json.dumps(cfg), encoding="utf-8")
        for eid in (origin_id, grok_id):
            subprocess.run([binary, "client-config", "--data", str(data), "--endpoint", eid,
                            "--file", str(root / eid / "client.json")], check=True, stdout=subprocess.DEVNULL, timeout=10)
        private_config = root / grok_id / "client.json"
        launcher = root / "report_launcher.py"
        launcher.write_text(
            "import sys\nfrom pathlib import Path\n"
            f"sys.path.insert(0, {str(REPO / 'clients')!r})\n"
            "from shared.native_report import main_for_binding\n"
            f"raise SystemExit(main_for_binding(Path({str(private_config)!r}), Path({str(data)!r}), "
            f"{grok_id!r}, Path({str(work)!r}), sys.argv[1:]))\n", encoding="utf-8",
        )
        process = subprocess.Popen([binary, "serve", "--data", str(data)], stdout=subprocess.PIPE,
                                   stderr=subprocess.PIPE, text=True)
        webhook = None
        server_thread = None
        try:
            startup = queue.Queue(maxsize=1)
            threading.Thread(target=lambda: startup.put(process.stdout.readline()), daemon=True).start()
            try:
                assert "listening" in startup.get(timeout=10), "synthetic Hub failed to start"
            except queue.Empty:
                raise RuntimeError("synthetic Hub startup timed out") from None
            opener = build_opener(ProxyHandler({}))

            def admin(method, path, body=None):
                request = Request("http://" + cfg["listen"] + path, method=method,
                                  data=None if body is None else json.dumps(body).encode(),
                                  headers={"Authorization": "Bearer " + cfg["admin_token"],
                                           "Content-Type": "application/json"})
                with opener.open(request, timeout=5) as response:
                    return json.load(response)

            for eid in (origin_id, grok_id):
                admin("POST", "/v1/session", {"endpoint": eid, "state": "idle"})
            notifications, packets, results, errors = [], [], [], []

            class SyntheticRoutine(BaseHTTPRequestHandler):
                def log_message(self, *_):
                    pass

                def do_POST(self):
                    status = 200
                    try:
                        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
                        assert set(body) == {"id"} and re.fullmatch(r"hub-[0-9a-f]{64}", body["id"])
                        assert self.headers.get("Idempotency-Key") == body["id"]
                        notifications.append(body)
                        # A complete published packet must exist BEFORE the HTTP request.
                        packet = json.loads((inbox / (body["id"] + ".json")).read_text(encoding="utf-8"))
                        packets.append(packet)
                        assert packet["protocol"] == "agi-hub-grok-file-v1"
                        assert packet["id"] == body["id"]
                        assert packet["endpoint"] == grok_id and packet["routine_id"] == routine_id
                        message = packet["envelope"]
                        assert message["recipient"] == grok_id and message["kind"] == "task"
                        assert message["message_id"] == "task:synthetic-grok-task"
                        assert body["id"] == "hub-" + hashlib.sha256(message["message_id"].encode()).hexdigest()
                        assert json.loads(message["content"])["goal"] == "Produce a synthetic Grok artifact"
                        assert "actively send complete Report JSON" in packet["notification"]
                        common = {"task_id": message["task_id"], "endpoint": grok_id, "run_id": "synthetic-grok-run"}
                        for state in ("accepted", "running", "succeeded"):
                            report = dict(common, event_id="synthetic-grok-" + state, state=state, summary="Synthetic " + state)
                            if state == "succeeded":
                                (work / "result.txt").write_text("Synthetic Grok file transport artifact\n", encoding="utf-8")
                                report["artifacts"] = [{"device_id": cfg["device_id"], "workspace_id": "work", "path": "result.txt"}]
                            path = work / "reports" / (state + ".json")
                            path.write_text(json.dumps(report), encoding="utf-8")
                            reported = subprocess.run([sys.executable, str(launcher), "report", "--data", str(data),
                                                       "--endpoint", grok_id, "--file", str(path)],
                                                      capture_output=True, text=True, timeout=10)
                            assert reported.returncode == 0, "fixed scoped reporter failed"
                    except Exception as exc:
                        errors.append(exc)
                        status = 500
                    self.send_response(status)
                    self.send_header("Content-Length", "0")
                    self.end_headers()

            webhook = ThreadingHTTPServer(("127.0.0.1", 0), SyntheticRoutine)
            server_thread = threading.Thread(target=webhook.serve_forever, daemon=True)
            server_thread.start()
            client_config = json.loads(private_config.read_text())
            client_config["native"] = {"transport": "file-webhook", "routine_id": routine_id,
                                       "url": f"http://127.0.0.1:{webhook.server_port}/routine",
                                       "headers": {}, "inbox_dir": str(inbox), "timeout_seconds": 30}
            private_config.write_text(json.dumps(client_config), encoding="utf-8")
            worker = HubAPI(client_config)
            origin = HubAPI(json.loads((root / origin_id / "client.json").read_text()))

            class SyntheticOrigin:
                def validate_binding(self, binding):
                    assert binding["driver"] == "synthetic-test" and binding["session_id"] == origin_id

                def deliver(self, envelope):
                    assert envelope["kind"] == "result"
                    assert envelope["reply_to"] == "task:synthetic-grok-task"
                    assert envelope["task_id"] == envelope["correlation_id"] == "synthetic-grok-task"
                    results.append(json.loads(envelope["content"]))
                    return "delivered", "Synthetic origin observation, no native assistant"

            adapter, observer = module.create_adapter(client_config), SyntheticOrigin()
            verify_binding(worker, adapter)
            verify_binding(origin, observer)
            with worker.subscribe(), origin.subscribe():
                admin("POST", "/v1/tasks", {
                    "id": "synthetic-grok-task", "origin": origin_id, "recipient": grok_id,
                    "goal": "Produce a synthetic Grok artifact", "mode": "queue", "require_artifact": True,
                    "resources": ["workspace:work"], "authorized_actions": ["Write synthetic result.txt"], "max_handoffs": 1,
                    "deadline": (datetime.now(timezone.utc) + timedelta(minutes=5)).isoformat(timespec="seconds"),
                })
                drain(worker, adapter)
                if errors:
                    raise errors[0]
                drain(origin, observer)
                drain(worker, adapter)
                drain(origin, observer)
            assert len(notifications) == len(packets) == len(results) == 1
            # Persistent client evidence prevents resending even after recreation.
            assert adapter.deliver(packets[0]["envelope"])[0] == "delivered"
            assert module.create_adapter(client_config).deliver(packets[0]["envelope"])[0] == "delivered"
            assert len(notifications) == 1
            snapshot = admin("GET", "/v1/state")
            assert snapshot["tasks"]["synthetic-grok-task"]["execution"] == "succeeded"
            assert len(snapshot["messages"]) == 2
            assert all(message["delivery"] == "delivered" for message in snapshot["messages"].values())
            assert results[0]["artifacts"][0]["sha256"] == hashlib.sha256((work / "result.txt").read_bytes()).hexdigest()
            assert snapshot["resource_owners"] == {}
            print(json.dumps({"real_hub_grok_file_transport": True, "synthetic_routine": True,
                              "id_only_webhooks": 1, "scoped_reports": 3, "verified_artifacts": 1,
                              "returned_results": 1, "restart_dedup": True, "native_assistant_messages": 0}))
        finally:
            if webhook is not None:
                webhook.shutdown()
                webhook.server_close()
                server_thread.join(timeout=5)
            process.terminate()
            try:
                process.communicate(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.communicate(timeout=5)
            if sys.platform != "win32":
                assert process.returncode == 0, "synthetic Hub did not shutdown normally"


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--hub", required=True, type=Path, help="built Hub executable for this platform")
    parser.add_argument("--temporary-parent", type=Path, help="existing directory on the filesystem to exercise")
    args = parser.parse_args()
    parent = args.temporary_parent.resolve(strict=True) if args.temporary_parent else None
    run(str(args.hub.resolve(strict=True)), parent)
