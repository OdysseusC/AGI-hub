"""The webhook URL and exact body are provided by the user's Routine setup."""

import sys
from pathlib import Path

root = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(root))
from adapter import GrokRoutineAdapter
from shared.runtime import main

if __name__ == "__main__":
    raise SystemExit(main(GrokRoutineAdapter))
