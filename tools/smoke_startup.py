#!/usr/bin/env python3
"""Exercise real startup processes in temporary data; no OS login items or models."""
import argparse
import json
from pathlib import Path
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request


def wait_for(check, detail, seconds=20):
    end = time.monotonic() + seconds
    while time.monotonic() < end:
        try:
            result = check()
            if result:
                return result
        except (OSError, ValueError, KeyError, urllib.error.URLError):
            pass
        time.sleep(0.1)
    raise AssertionError(detail)


def port():
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return listener.getsockname()[1]


def stop(process):
    if process is None or process.poll() is not None:
        return
    process.terminate()
    try:
        process.wait(timeout=12)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait(timeout=5)


def run(binary):
    with tempfile.TemporaryDirectory(prefix="agi-hub-startup-smoke-") as temp:
        data = Path(temp) / "private"
        subprocess.run([binary, "init", "--data", str(data)], check=True, stdout=subprocess.DEVNULL)
        config_file = data / "config.json"
        config = json.loads(config_file.read_text())
        config["listen"] = "127.0.0.1:" + str(port())
        config_file.write_text(json.dumps(config))
        # This test plan is private to the fixture. It does not register an OS
        # trigger, activate an endpoint or ask a native assistant to do anything.
        (data / "startup.json").write_text(json.dumps({
            "version": 1, "enabled": True, "binary": binary,
            "clients": [{"endpoint": "codex", "enabled": True, "requires_host": True}],
        }))
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

        def request(path):
            req = urllib.request.Request("http://" + config["listen"] + path,
                                         headers={"Authorization": "Bearer " + config["admin_token"]})
            with opener.open(req, timeout=2) as response:
                return json.load(response)

        def state():
            return json.loads((data / "startup-state.json").read_text())

        def start_runner():
            return subprocess.Popen([binary, "autostart", "run", "--data", str(data)],
                                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

        runner = manual = None
        try:
            runner = start_runner()
            wait_for(lambda: request("/v1/health").get("ready"), "owned Hub did not become ready")
            wait_for(lambda: any(c["state"] == "needs_host_environment" for c in state()["components"]),
                     "host-only Client was not gated")
            runtime = state()
            hub = next(c for c in runtime["components"] if c["id"] == "hub")
            assert hub["attempts"] == 1
            before = request("/v1/state")
            assert not before.get("tasks") and not before.get("messages")
            duplicate = start_runner()
            assert duplicate.wait(timeout=10) != 0, "duplicate startup runner was allowed"
            assert request("/v1/health")["ready"]
            view = request("/v1/manage")["autostart"]
            assert view["runtime_active"] is True
            assert all("command" not in c and "environment" not in c for c in view["clients"])
            stop(runner)
            runner = None

            def no_listener():
                with socket.socket() as connection:
                    connection.settimeout(0.5)
                    return connection.connect_ex(("127.0.0.1", int(config["listen"].rsplit(":", 1)[1]))) != 0

            wait_for(no_listener, "owned Hub survived stopped runner")
            # A separately started Hub is attached, never adopted or terminated.
            manual = subprocess.Popen([binary, "serve", "--data", str(data)],
                                      stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            wait_for(lambda: request("/v1/health").get("ready"), "manual Hub not ready")
            runner = start_runner()
            wait_for(lambda: state()["pid"] == runner.pid and any(
                c["id"] == "hub" and c["state"] == "hub_ready" and c["pid"] == 0
                for c in state()["components"]), "existing Hub was not attached")
            stop(runner)
            runner = None
            assert manual.poll() is None and request("/v1/health")["ready"], "attached Hub was stopped"
            assert request("/v1/state") == before, "startup changed mailbox or business state"
        finally:
            stop(runner)
            stop(manual)
    return {"startup_process_smoke": "passed", "registered_os_login_items": 0, "native_model_calls": 0}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--hub", required=True)
    options = parser.parse_args()
    print(json.dumps(run(str(Path(options.hub).resolve()))))
