"""Compatibility entry point for run-owned.py."""

from pathlib import Path
import runpy

if __name__ == "__main__":
    runpy.run_path(str(Path(__file__).with_name("run-owned.py")), run_name="__main__")
