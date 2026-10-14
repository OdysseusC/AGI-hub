"""Run a configured native resumed-session CLI or official OpenAPI client."""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from shared.runtime import main
from workbuddy.adapter import create_adapter

if __name__ == "__main__":
    raise SystemExit(main(create_adapter))
