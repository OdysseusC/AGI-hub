"""Run this client separately from the Go Hub."""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from codex.adapter import create_adapter
from shared.runtime import main

if __name__ == "__main__":
    raise SystemExit(main(create_adapter))
