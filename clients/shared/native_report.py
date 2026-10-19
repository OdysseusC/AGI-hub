"""Collaboration entry point for an installation's fixed, scoped launcher.

The launcher supplies all trusted binding values in code. Message data may only
select a report file inside that binding's workspace, or provide report stdin.
Mail sends use a separate, scoped route and the configured recipient allowlist.
This module cannot modify sessions/leader or invoke an
assistant directly. It never loads or sends an administrator credential.
"""

from __future__ import annotations

import json
import os
import stat
import sys
from pathlib import Path

from shared.runtime import HubAPI, MAX_BYTES, load_config, status
from shared.transport import ConfigurationError, HubError


def parse_report_args(argv: list[str]) -> dict[str, str]:
    if not argv or argv[0] != "report" or not all(isinstance(value, str) for value in argv):
        raise ConfigurationError("report_command_required")
    allowed = {"--data", "--endpoint", "--file"}
    values: dict[str, str] = {}
    index = 1
    while index < len(argv):
        argument = argv[index]
        if "=" in argument:
            name, value = argument.split("=", 1)
            index += 1
        else:
            name = argument
            if index + 1 >= len(argv):
                raise ConfigurationError("report_flag_requires_value")
            value = argv[index + 1]
            index += 2
        if name not in allowed or name in values or not value or value.startswith("--") or "\x00" in value:
            raise ConfigurationError("invalid_or_duplicate_report_flag")
        values[name] = value
    return values


def parse_mail_args(argv: list[str]) -> dict[str, str]:
    if not argv or argv[0] not in ("peers", "mail-send", "mail-state") or not all(isinstance(value, str) for value in argv):
        raise ConfigurationError("unsupported_collaboration_command")
    command = argv[0]
    allowed = {"--data", "--endpoint"}
    if command == "mail-send":
        allowed.add("--file")
    if command == "mail-state":
        allowed.add("--correlation-id")
    parsed: dict[str, str] = {}
    index = 1
    while index < len(argv):
        argument = argv[index]
        if "=" in argument:
            name, value = argument.split("=", 1)
            index += 1
        else:
            name = argument
            if index + 1 >= len(argv):
                raise ConfigurationError("collaboration_flag_requires_value")
            value = argv[index + 1]
            index += 2
        if name not in allowed or name in parsed or not value or value.startswith("--") or "\x00" in value:
            raise ConfigurationError("invalid_or_duplicate_collaboration_flag")
        parsed[name] = value
    if command == "mail-state" and not parsed.get("--correlation-id"):
        raise ConfigurationError("correlation_id_required")
    return parsed


def validate_mail_submission(value: dict) -> None:
    allowed = {
        "id", "recipient", "kind", "content", "correlation_id", "origin_ref",
        "reply_to", "deadline", "max_messages",
    }
    if set(value) - allowed:
        # Do not echo untrusted field names or message contents: they can carry
        # private data. The fixed schema in the error is sufficient to repair it.
        raise ConfigurationError("unsupported_mail_fields")
    if not isinstance(value.get("id"), str) or not value["id"].strip():
        raise ConfigurationError("mail_id_required")
    if not isinstance(value.get("recipient"), str) or not value["recipient"].strip():
        raise ConfigurationError("mail_recipient_required")
    if value.get("kind") not in ("request", "reply", "status", "result"):
        raise ConfigurationError("mail_kind_invalid")
    if not isinstance(value.get("content"), str) or not value["content"].strip():
        raise ConfigurationError("mail_content_required")


def decode_report(raw: bytes) -> dict:
    def unique(items):
        value = {}
        for key, child in items:
            if key in value:
                raise ConfigurationError("duplicate_report_key")
            value[key] = child
        return value
    if len(raw) > MAX_BYTES:
        raise ConfigurationError("report_too_large")
    try:
        report = json.loads(raw.decode("utf-8"), object_pairs_hook=unique)
    except (UnicodeError, ValueError) as exc:
        if isinstance(exc, ConfigurationError):
            raise
        raise ConfigurationError("invalid_report_json") from None
    if not isinstance(report, dict):
        raise ConfigurationError("report_requires_json_object")
    return report


def is_redirect(info: os.stat_result) -> bool:
    return stat.S_ISLNK(info.st_mode) or bool(
        getattr(info, "st_file_attributes", 0) & getattr(stat, "FILE_ATTRIBUTE_REPARSE_POINT", 0)
    )


def read_workspace_report(root: Path, filename: str) -> bytes:
    candidate = Path(filename)
    if ".." in candidate.parts:
        raise ConfigurationError("report_file_outside_workspace")
    if candidate.is_absolute():
        try:
            relative = candidate.relative_to(root)
        except ValueError:
            raise ConfigurationError("report_file_outside_workspace") from None
    else:
        relative = candidate
    if not relative.parts:
        raise ConfigurationError("report_requires_regular_file")
    candidate = root / relative
    current = root
    inspected = []
    for position, part in enumerate(relative.parts):
        current /= part
        info = current.lstat()
        if is_redirect(info):
            raise ConfigurationError("report_symlink_forbidden")
        if position < len(relative.parts) - 1 and not stat.S_ISDIR(info.st_mode):
            raise ConfigurationError("report_parent_requires_directory")
        inspected.append((current, info.st_dev, info.st_ino))
    expected = candidate.lstat()
    if not stat.S_ISREG(expected.st_mode):
        raise ConfigurationError("report_requires_regular_file")
    if expected.st_size > MAX_BYTES:
        raise ConfigurationError("report_too_large")
    flags = os.O_RDONLY | getattr(os, "O_BINARY", 0) | getattr(os, "O_NONBLOCK", 0)
    fd = None
    directory_fds = []
    try:
        if os.open in os.supports_dir_fd and hasattr(os, "O_NOFOLLOW") and hasattr(os, "O_DIRECTORY"):
            # On POSIX, anchor each traversal to an open directory and never
            # follow a replacement link between validation and opening the file.
            directory_flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW
            parent = os.open(root, directory_flags)
            directory_fds.append(parent)
            for part in relative.parts[:-1]:
                parent = os.open(part, directory_flags, dir_fd=parent)
                directory_fds.append(parent)
            fd = os.open(relative.parts[-1], flags | os.O_NOFOLLOW, dir_fd=parent)
        else:
            fd = os.open(candidate, flags)
        opened = os.fstat(fd)
        if not stat.S_ISREG(opened.st_mode) or (opened.st_dev, opened.st_ino) != (expected.st_dev, expected.st_ino):
            raise ConfigurationError("report_file_changed_before_read")
        if opened.st_size > MAX_BYTES:
            raise ConfigurationError("report_too_large")
        # Windows lacks standard-library openat: reject any changed or redirecting
        # component after opening as well, and require the physical path in root.
        for path, device, inode in inspected:
            after = path.lstat()
            if is_redirect(after) or (after.st_dev, after.st_ino) != (device, inode):
                raise ConfigurationError("report_file_changed_before_read")
        if not candidate.resolve(strict=True).is_relative_to(root):
            raise ConfigurationError("report_file_outside_workspace")
        with os.fdopen(fd, "rb") as stream:
            fd = None
            raw = stream.read(MAX_BYTES + 1)
        if len(raw) > MAX_BYTES:
            raise ConfigurationError("report_too_large")
        return raw
    finally:
        if fd is not None:
            os.close(fd)
        for directory in reversed(directory_fds):
            os.close(directory)


def main_for_binding(
    config_path: Path, data_dir: Path, endpoint: str, workspace_root: Path, argv: list[str],
) -> int:
    """Called only by a trusted launcher with fixed installation constants."""
    try:
        command = argv[0] if argv else ""
        options = parse_mail_args(argv) if command in ("peers", "mail-send", "mail-state") else parse_report_args(argv)
        if not config_path.is_absolute() or not data_dir.is_absolute() or not workspace_root.is_absolute():
            raise ConfigurationError("invalid_fixed_report_binding")
        root = workspace_root.resolve(strict=True)
        if not root.is_dir() or not isinstance(endpoint, str) or not endpoint:
            raise ConfigurationError("invalid_fixed_report_binding")
        if options.get("--endpoint", endpoint) != endpoint:
            raise ConfigurationError("report_endpoint_mismatch")
        if "--data" in options:
            supplied = Path(options["--data"])
            if not supplied.is_absolute() or os.path.normcase(str(supplied.resolve())) != os.path.normcase(str(data_dir.resolve())):
                raise ConfigurationError("report_data_binding_mismatch")
        config = load_config(config_path)
        if config["endpoint"] != endpoint:
            raise ConfigurationError("report_client_binding_mismatch")
        if command == "peers":
            print(json.dumps(HubAPI(config).get("catalog"), ensure_ascii=False, allow_nan=False), flush=True)
            return 0
        if command == "mail-state":
            value = HubAPI(config).get("mail/state", {"correlation_id": options["--correlation-id"]})
            print(json.dumps(value, ensure_ascii=False, allow_nan=False), flush=True)
            return 0
        filename = options.get("--file", "-")
        if filename == "-":
            report = decode_report(sys.stdin.buffer.read(MAX_BYTES + 1))
        else:
            report = decode_report(read_workspace_report(root, filename))
        if command == "mail-send":
            validate_mail_submission(report)
            value = HubAPI(config).post("mail/send", report)
            print(json.dumps(value, ensure_ascii=False, allow_nan=False), flush=True)
            return 0
        if report.get("endpoint") != endpoint:
            raise ConfigurationError("report_endpoint_mismatch")
        # The Hub performs the rest of the strict Report schema, event dedup,
        # run matching and artifact validation. Only scoped credentials are used.
        HubAPI(config).post("report", report)
        status("ready", "report_saved")
        return 0
    except ConfigurationError as exc:
        if command == "mail-send":
            print(json.dumps({
                "state": "error", "code": str(exc),
                "usage": "mail-send --file <workspace JSON>; recipient is JSON field; no --peer/--to/--collaboration-flag",
                "required_fields": ["id", "recipient", "kind", "content"],
                "optional_fields": ["correlation_id", "origin_ref", "reply_to", "deadline", "max_messages"],
            }), flush=True)
        else:
            status("error", str(exc))
    except HubError:
        code = "report_unconfirmed_keep_stable_event_id"
        if command == "mail-send":
            code = "mail_send_unconfirmed_keep_stable_id"
        elif command in ("peers", "mail-state"):
            code = "mail_query_unconfirmed"
        status("error", code)
    except (OSError, ValueError, UnicodeError):
        status("error", "report_file_unavailable_or_invalid")
    except Exception:
        status("error", "report_helper_stopped")
    return 1
