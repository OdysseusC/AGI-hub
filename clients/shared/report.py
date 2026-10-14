"""Endpoint-scoped reporting helper; it never invokes a native assistant."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from shared.runtime import HubAPI, MAX_BYTES, load_config, read_json, status
from shared.transport import ConfigurationError, HubError


def main() -> int:
    parser = argparse.ArgumentParser(description="Submit one native execution report to AGI Hub")
    parser.add_argument("--config", required=True, type=Path)
    parser.add_argument("--file", required=True, help="Report JSON file, or - for stdin")
    args = parser.parse_args()
    try:
        config = load_config(args.config)
        if args.file == "-":
            data = sys.stdin.buffer.read(MAX_BYTES + 1)
            if len(data) > MAX_BYTES:
                raise ConfigurationError("report_too_large")
            report = json.loads(data)
        else:
            report = read_json(Path(args.file))
        if not isinstance(report, dict) or report.get("endpoint") != config["endpoint"]:
            raise ConfigurationError("report_endpoint_mismatch")
        if report.get("state") not in ("accepted", "running", "needs_user", "succeeded", "failed", "interrupted"):
            raise ConfigurationError("invalid_report_state")
        HubAPI(config).post("report", report)
        status("ready", "report_saved")
        return 0
    except ConfigurationError as exc:
        status("error", str(exc))
    except HubError:
        status("error", "report_unconfirmed_keep_stable_event_id")
    except (OSError, ValueError, UnicodeError):
        status("error", "cannot_read_report")
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
