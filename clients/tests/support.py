"""Local-only protocol fixtures. These helpers never invoke a native assistant."""

import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


def envelope(kind="task", mode="queue"):
    return {
        "message_id": "message-test", "task_id": "task-test", "kind": kind,
        "sender": "origin-test", "recipient": "endpoint-test", "mode": mode,
        "content": 'private content literal {{message_id}} `$(do not execute)`',
        "delivery": "dispatching",
    }



def registered_binding(driver="codex", session_id="thread-test", state="unknown"):
    return {
        "protocol_version": 1,
        "endpoint": {
            "id": "endpoint-test", "device_id": "local-test", "session_id": session_id,
            "driver": driver, "enabled": True, "modes": ["queue"], "idle_wake_verified": False,
        },
        "session": {"state": state},
    }



class LocalHTTP:
    def __init__(self, callback):
        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                callback(self)

            def do_POST(self):
                callback(self)

            def log_message(self, *_):
                pass

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.base = f"http://127.0.0.1:{self.server.server_port}"

    def __enter__(self):
        self.thread.start()
        return self

    def __exit__(self, *_):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()



def respond(handler, body, status=200, content_type="application/json"):
    encoded = json.dumps(body).encode()
    handler.send_response(status)
    handler.send_header("Content-Type", content_type)
    handler.send_header("Content-Length", str(len(encoded)))
    handler.end_headers()
    handler.wfile.write(encoded)

