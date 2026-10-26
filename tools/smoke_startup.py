#!/usr/bin/env python3
"""Exercise real startup processes in temporary data; no OS login items or models."""
import argparse
import json
import os
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


def read_snapshot(path):
    if os.name != "nt":
        return json.loads(path.read_text())
    # Observe a live snapshot without preventing its atomic replacement. CRT
    # open/read_text does not request FILE_SHARE_DELETE on Windows.
    import ctypes
    import msvcrt
    from ctypes import wintypes

    kernel = ctypes.WinDLL("kernel32", use_last_error=True)
    kernel.CreateFileW.argtypes = (wintypes.LPCWSTR, wintypes.DWORD, wintypes.DWORD,
                                   wintypes.LPVOID, wintypes.DWORD, wintypes.DWORD,
                                   wintypes.HANDLE)
    kernel.CreateFileW.restype = wintypes.HANDLE
    kernel.CloseHandle.argtypes = (wintypes.HANDLE,)
    kernel.CloseHandle.restype = wintypes.BOOL
    handle = kernel.CreateFileW(str(path), 0x80000000, 0x7, None, 3, 0x00200080, None)
    if handle in (None, ctypes.c_void_p(-1).value):
        raise ctypes.WinError(ctypes.get_last_error())
    try:
        descriptor = msvcrt.open_osfhandle(handle, os.O_RDONLY | os.O_NOINHERIT)
    except BaseException:
        kernel.CloseHandle(handle)
        raise
    # Ownership transfers to the CRT descriptor, then to the file object.
    with os.fdopen(descriptor, "rb") as snapshot:
        body = snapshot.read((1 << 20) + 1)
    if len(body) > 1 << 20:
        raise ValueError("fixture runtime snapshot exceeds its size limit")
    return json.loads(body.decode("utf-8"))


def capture_owned_hub(state, runner):
    """Pin this fixture runner's Windows child before stopping its Job Object."""
    if os.name != "nt" or runner is None or runner.poll() is not None:
        return None
    try:
        runtime = state()
    except (OSError, ValueError):
        return None  # No trustworthy snapshot yet; never guess a child PID.
    if not isinstance(runtime, dict) or runtime.get("pid") != runner.pid:
        return None
    components = runtime.get("components")
    if not isinstance(components, list):
        return None
    hub = next((c for c in components if isinstance(c, dict) and c.get("id") == "hub"), {})
    pid = hub.get("pid")
    if type(pid) is not int or pid <= 0 or pid in (runner.pid, os.getpid()) or not hub.get("attempts"):
        return None  # Attached Hubs have PID/attempts zero and are not ours.

    import ctypes
    from ctypes import wintypes

    kernel = ctypes.WinDLL("kernel32", use_last_error=True)
    kernel.OpenProcess.argtypes = (wintypes.DWORD, wintypes.BOOL, wintypes.DWORD)
    kernel.OpenProcess.restype = wintypes.HANDLE
    kernel.WaitForSingleObject.argtypes = (wintypes.HANDLE, wintypes.DWORD)
    kernel.WaitForSingleObject.restype = wintypes.DWORD
    kernel.CloseHandle.argtypes = (wintypes.HANDLE,)
    kernel.CloseHandle.restype = wintypes.BOOL
    handle = kernel.OpenProcess(0x00100000, False, pid)  # SYNCHRONIZE only.
    if not handle:
        error = ctypes.get_last_error()
        if error == 87:  # ERROR_INVALID_PARAMETER: child already disappeared.
            return None
        raise ctypes.WinError(error)
    return kernel, handle


def wait_owned_hub(captured):
    if captured is None:
        return
    import ctypes

    kernel, handle = captured
    try:
        result = kernel.WaitForSingleObject(handle, 12000)
        if result == 0x00000102:  # WAIT_TIMEOUT
            raise AssertionError("owned Windows Hub did not exit after stopping its runner")
        if result != 0:  # WAIT_OBJECT_0
            raise ctypes.WinError(ctypes.get_last_error())
    finally:
        if not kernel.CloseHandle(handle):
            raise ctypes.WinError(ctypes.get_last_error())


def diagnose_startup_status(binary, data, admin_token):
    """Only the private fixture's redacted status/error, never its config."""
    try:
        result = subprocess.run(
            [binary, "autostart", "status", "--data", str(data)],
            capture_output=True, text=True, timeout=20,
        )
        diagnostic = {"returncode": result.returncode,
                      "stdout": result.stdout[:8192], "stderr": result.stderr[:8192]}
    except subprocess.TimeoutExpired as failure:
        diagnostic = {"timeout_seconds": 20,
                      "stdout": str(failure.stdout or "")[:8192],
                      "stderr": str(failure.stderr or "")[:8192]}
    print(json.dumps({"startup_status_diagnostic": diagnostic}).replace(admin_token, "[redacted]"), flush=True)


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

        def request(path, timeout=2):
            req = urllib.request.Request("http://" + config["listen"] + path,
                                         headers={"Authorization": "Bearer " + config["admin_token"]})
            with opener.open(req, timeout=timeout) as response:
                return json.load(response)

        def state():
            return read_snapshot(data / "startup-state.json")

        def start_runner():
            return subprocess.Popen([binary, "autostart", "run", "--data", str(data)],
                                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

        runner = manual = owned_hub = None

        def stop_runner():
            nonlocal runner, owned_hub
            try:
                if owned_hub is None:
                    # Also cover failures during health/host-gate startup waits.
                    owned_hub = capture_owned_hub(state, runner)
            finally:
                try:
                    stop(runner)
                finally:
                    runner = None
                    captured, owned_hub = owned_hub, None
                    # Windows TerminateProcess and kill-on-close jobs complete
                    # asynchronously. Wait for our pinned child before rmtree.
                    wait_owned_hub(captured)

        try:
            runner = start_runner()
            wait_for(lambda: request("/v1/health").get("ready"), "owned Hub did not become ready")
            wait_for(lambda: any(c["state"] == "needs_host_environment" for c in state()["components"]),
                     "host-only Client was not gated")
            runtime = state()
            hub = next(c for c in runtime["components"] if c["id"] == "hub")
            owned_hub = capture_owned_hub(state, runner)
            assert hub["attempts"] == 1
            before = request("/v1/state")
            assert not before.get("tasks") and not before.get("messages")
            duplicate = start_runner()
            assert duplicate.wait(timeout=10) != 0, "duplicate startup runner was allowed"
            assert request("/v1/health")["ready"]
            # Platform status can run a bounded 15s PowerShell query on Windows;
            # keep health probes short while allowing that management budget.
            view = request("/v1/manage", timeout=20)["autostart"]
            if view.get("runtime_active") is not True:
                diagnose_startup_status(binary, data, config["admin_token"])
            assert view.get("runtime_active") is True, {
                key: view.get(key) for key in ("supported", "detail", "runtime_active")
            }
            assert all("command" not in c and "environment" not in c for c in view["clients"])
            stop_runner()

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
            stop_runner()
            assert manual.poll() is None and request("/v1/health")["ready"], "attached Hub was stopped"
            assert request("/v1/state") == before, "startup changed mailbox or business state"
        finally:
            try:
                stop_runner()
            finally:
                stop(manual)
    return {"startup_process_smoke": "passed", "registered_os_login_items": 0, "native_model_calls": 0}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--hub", required=True)
    options = parser.parse_args()
    print(json.dumps(run(str(Path(options.hub).resolve()))))
