"""Run this client with an explicitly granted official OAuth access token."""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from shared.runtime import main
from workbuddy.adapter import WorkBuddyAdapter

if __name__ == "__main__":
    raise SystemExit(main(WorkBuddyAdapter))
