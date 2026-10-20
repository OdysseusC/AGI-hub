"""Run the user's configured Grok Routine transport in its native session."""

import sys
from pathlib import Path

root = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(root))
from adapter import create_adapter
from shared.runtime import main

if __name__ == "__main__":
    raise SystemExit(main(create_adapter))
