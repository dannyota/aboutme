#!/usr/bin/env bash
set -Eeuo pipefail

exec python3 - "$@" <<'PY'
import argparse
import json
import os
from pathlib import Path
import re
import tomllib

parser = argparse.ArgumentParser(prog="scripts/codex-agent.sh")
parser.add_argument("--model")
parser.add_argument("--effort")
parser.add_argument("--search", action="store_true")
parser.add_argument("role")
parser.add_argument("worktree")
parser.add_argument("brief_file")
parser.add_argument("report_file")
args = parser.parse_args()
if not re.fullmatch(r"[a-z_]+", args.role):
    parser.error("role must match ^[a-z_]+$")

role_file = Path(args.worktree) / ".codex/agents" / f"{args.role}.toml"
try:
    with role_file.open("rb") as source:
        role = tomllib.load(source)
    model = args.model if args.model is not None else role["model"]
    effort = args.effort if args.effort is not None else role["model_reasoning_effort"]
    sandbox = role["sandbox_mode"]
    instructions = role["developer_instructions"]
    if not all(isinstance(value, str) and value for value in (model, effort, sandbox, instructions)):
        parser.error("role settings must be nonempty strings")
    brief = Path(args.brief_file).read_text(encoding="utf-8")
except (OSError, ValueError, KeyError) as error:
    parser.error(str(error))

command = ["codex"] + (["--search"] if args.search else []) + [
    "exec", "-m", model, "-c", f"model_reasoning_effort={effort}",
    "-c", "developer_instructions=" + json.dumps(instructions, ensure_ascii=False),
    "-s", sandbox, "-C", args.worktree, "--ephemeral", "-o", args.report_file, brief,
]
with open(os.devnull, "rb") as source:
    os.dup2(source.fileno(), 0)
os.execvp(command[0], command)
PY
