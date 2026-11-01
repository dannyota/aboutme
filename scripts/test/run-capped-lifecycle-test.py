"""Compatibility entry point for the owned-process lifecycle suite."""

from pathlib import Path
import runpy
import sys

if len(sys.argv) > 1 and sys.argv[1].endswith("/run-capped.py"):
    sys.argv[1] = str(Path(sys.argv[1]).with_name("run-owned.py"))
runpy.run_path(str(Path(__file__).with_name("test_run_owned_lifecycle.py")), run_name="__main__")
