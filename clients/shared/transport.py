"""Single-attempt HTTP; no redirect, proxy, or native-request replay."""

from __future__ import annotations

import http.client
import ipaddress
import json
from dataclasses import dataclass
from typing import Callable
from urllib.parse import urlsplit


class ConfigurationError(ValueError):
    """The message is a fixed, credential-free diagnostic code."""


class HubError(RuntimeError):
    def __init__(self, code: str, status: int | None = None):
        super().__init__(code)
        self.code = code
        self.status = status


@dataclass(frozen=True)
class Address:
    scheme: str
    host: str
    port: int
    authority: str
    path: str

    @property
    def origin(self) -> str:
        return f"{self.scheme}://{self.authority}"


def parse_address(value: str, *, hub: bool = False) -> Address:
    try:
        parsed = urlsplit(value)
        host = parsed.hostname
        port = parsed.port
    except (TypeError, ValueError):
        raise ConfigurationError("invalid_url") from None
    if (
        not isinstance(value, str)
        or parsed.scheme not in ("http", "https")
        or not host
        or parsed.username is not None
        or parsed.password is not None
        or parsed.fragment
        or any(char.isspace() for char in value)
    ):
        raise ConfigurationError("invalid_url")
    try:
        numeric = ipaddress.ip_address(host)
        local = numeric.is_loopback
    except ValueError:
        local = host.lower() == "localhost"
        numeric = None
    if hub:
        # Numeric loopback avoids DNS and guarantees local credentials stay local.
        if numeric is None or not local or parsed.query or parsed.path not in ("", "/"):
            raise ConfigurationError("hub_requires_numeric_loopback_base_url")
    elif parsed.scheme != "https" and not local:
        raise ConfigurationError("native_requires_https_or_loopback")
    actual_port = port if port is not None else (443 if parsed.scheme == "https" else 80)
    if not 1 <= actual_port <= 65535:
        raise ConfigurationError("invalid_port")
    authority_host = f"[{host}]" if ":" in host else host
    # Preserve an explicit port to match Hub's exact Host/Origin checks.
    authority = f"{authority_host}:{port}" if port is not None else authority_host
    path = parsed.path or "/"
    if parsed.query:
        path += "?" + parsed.query
    return Address(parsed.scheme, host, actual_port, authority, path)


def valid_header(name: str, value: str) -> bool:
    allowed = "!#$%&'*+-.^_`|~"
    return bool(name) and all(char.isascii() and (char.isalnum() or char in allowed) for char in name) and all(
        (ord(char) >= 32 or char == "\t") and ord(char) != 127 for char in value
    )


def connection(address: Address, timeout: float) -> http.client.HTTPConnection:
    cls = http.client.HTTPSConnection if address.scheme == "https" else http.client.HTTPConnection
    return cls(address.host, address.port, timeout=timeout)


def request(
    address: Address,
    method: str,
    path: str,
    body: bytes | None,
    headers: dict[str, str],
    timeout: float,
) -> tuple[http.client.HTTPConnection, http.client.HTTPResponse]:
    conn = connection(address, timeout)
    try:
        conn.request(method, path, body=body, headers=headers)
        return conn, conn.getresponse()
    except Exception:
        conn.close()
        raise


def post_json_once(
    url: str, body: object, headers: dict[str, str], timeout: float,
    *, response_validator: Callable[[bytes], bool] | None = None,
) -> tuple[str, str]:
    try:
        address = parse_address(url)
        if not all(isinstance(k, str) and isinstance(v, str) and valid_header(k, v) for k, v in headers.items()):
            raise ConfigurationError("invalid_native_header")
        if any(k.lower() in ("host", "content-length", "connection", "transfer-encoding") for k in headers):
            raise ConfigurationError("reserved_native_header")
        encoded = json.dumps(body, ensure_ascii=False, allow_nan=False).encode("utf-8")
        if len(encoded) > 1 << 20:
            raise ConfigurationError("native_message_too_large")
    except (ConfigurationError, TypeError, ValueError):
        return "failed", "native configuration rejected before a request was started"
    safe_headers = {"Content-Type": "application/json", "Host": address.authority, "Connection": "close"}
    safe_headers.update(headers)
    try:
        conn, response = request(address, "POST", address.path, encoded, safe_headers, timeout)
    except (OSError, http.client.HTTPException, ValueError):
        return "uncertain", "native request attempted without a confirmed response; reconcile before retrying"
    try:
        # Never print/store platform response bodies, which can contain private data.
        if 200 <= response.status < 300:
            if response_validator is not None:
                try:
                    data = response.read((1 << 20) + 1)
                    valid = len(data) <= 1 << 20 and response_validator(data)
                except Exception:
                    valid = False
                if not valid:
                    return "uncertain", "native response did not confirm platform delivery; reconcile before retrying"
                return "delivered", "native platform response confirmed message delivery; assistant acceptance and business completion remain unverified"
            return "delivered", "native HTTP transport returned 2xx; acceptance and business completion remain unverified"
        return "uncertain", f"native HTTP transport returned {response.status}; reconcile before retrying"
    finally:
        response.close()
        conn.close()
