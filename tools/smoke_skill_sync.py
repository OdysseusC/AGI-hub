"""Exercise managed skill maintenance using a real Hub; never invoke a model."""

import argparse
import json
import shutil
import socket
import subprocess
import tempfile
from pathlib import Path


def run(binary: Path):
    with tempfile.TemporaryDirectory(prefix="agi-hub-skills-") as temporary:
        root = Path(temporary).resolve()
        data = root / "data"

        def cli(*args, success=True):
            result = subprocess.run([str(binary), *args, "--data", str(data)], capture_output=True, text=True, timeout=15)
            assert (result.returncode == 0) == success, result.stderr
            return json.loads(result.stdout) if result.stdout.strip().startswith(("{", "[")) else None

        cli("init")
        config_path = data / "config.json"
        config = json.loads(config_path.read_text())
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            config["listen"] = f"127.0.0.1:{sock.getsockname()[1]}"
        names = ("codex", "workbuddy", "claude")
        config["bindings"] = [{"endpoint": {
            "id": name, "device_id": config["device_id"], "session_id": "synthetic-" + name,
            "enabled": True, "idle_wake_verified": False, "modes": ["queue"], "driver": "synthetic-test",
        }, "driver": {"type": "external"}, "visible_peers": ["workbuddy"] if name == "codex" else []} for name in names]
        config_path.write_text(json.dumps(config))
        for name in names:
            cli("install-skill", "--host", name, "--endpoint", name, "--skills-dir", str(root / name), "--binary", str(binary))

        def start():
            # Log to a file: startup conflict diagnostics cannot fill a pipe.
            log = (root / "hub.log").open("a")
            process = subprocess.Popen([str(binary), "serve", "--data", str(data)], stdout=subprocess.PIPE, stderr=log, text=True)
            log.close()
            assert "listening" in process.stdout.readline(), "Hub failed to start"
            return process

        def stop(process):
            process.terminate()
            process.communicate(timeout=5)

        process = start()
        try:
            catalog = cli("peers", "--endpoint", "codex")
            assert [peer["id"] for peer in catalog["peers"]] == ["workbuddy"]
            assert cli("peers", "--endpoint", "claude")["peers"] == []
            rules = cli("skill-rules", "--endpoint", "codex")
            assert "content" not in rules and rules["version"] == catalog["rules_version"]
            assert "content" not in cli("skill-rules", "--endpoint", "codex", "--content", "--known-version", rules["version"])
            paths = [root / name / "agi-hub-client/SKILL.md" for name in names]
            before = [path.stat().st_mtime_ns for path in paths]
            assert all(row["state"] == "current" and row["host_load_state"] == "unknown" for row in cli("sync-skills"))
            assert before == [path.stat().st_mtime_ns for path in paths]
            paths[2].write_text("user maintained instructions\n")
            shutil.rmtree(paths[0].parent)
            states = {row["endpoint_id"]: row["state"] for row in cli("sync-skills", success=False)}
            assert states == {"codex": "missing", "claude": "conflict", "workbuddy": "current"}
            assert not paths[0].parent.exists() and paths[2].read_text() == "user maintained instructions\n"
            state = cli("status")
            assert state["tasks"] == {} and state["messages"] == {}
        finally:
            stop(process)
        config["bindings"][1]["endpoint"]["enabled"] = False
        config_path.write_text(json.dumps(config))
        process = start()  # Conflicting files must not prevent Hub startup.
        try:
            updated = cli("peers", "--endpoint", "codex")
            assert updated["peers"] == [] and updated["version"] != catalog["version"]
            assert cli("status")["messages"] == {}
        finally:
            stop(process)
        print(json.dumps({"real_hub_skill_sync": True, "registered_hosts": 3, "no_op_mtime_preserved": True,
                          "conflict_preserved": True, "uninstall_preserved": True, "directory_revocation": True,
                          "host_loaded_version": "unknown", "native_model_calls": 0}))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--hub", required=True, type=Path)
    run(parser.parse_args().hub.resolve(strict=True))
