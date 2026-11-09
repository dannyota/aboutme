#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
exec python3 - "$ROOT" "$@" <<'PY'
import argparse
import difflib
from pathlib import Path
import re
import sys
import tempfile
import tomllib


def multiline_string(value):
    escapes = {"\\": "\\\\", '"': '\\"', "\b": "\\b", "\t": "\\t",
               "\f": "\\f", "\r": "\\r"}
    escaped = "".join(
        escapes.get(char, f"\\u{ord(char):04x}" if (ord(char) < 32 and char != "\n")
                    or ord(char) == 127 else char)
        for char in value
    )
    return '"""\n' + escaped + '"""'


def render(sources):
    common = (sources / "common.md").read_text(encoding="utf-8").strip()
    if not common:
        raise ValueError("common.md must not be empty")
    fragments = sorted(sources.glob("*.toml.in"))
    if not fragments:
        raise ValueError("no role fragments found")
    fields = {"name", "description", "model", "model_reasoning_effort", "sandbox_mode"}
    rendered = {}
    for fragment in fragments:
        header, separator, role = fragment.read_text(encoding="utf-8").partition("\n\n")
        metadata = tomllib.loads(header)
        name = fragment.name.removesuffix(".toml.in")
        if (not separator or not role.strip() or set(metadata) != fields
                or any(not isinstance(value, str) or not value for value in metadata.values())
                or not re.fullmatch(r"[a-z_]+", name) or metadata["name"] != name):
            raise ValueError(f"invalid role fragment: {fragment.name}")
        instructions = common + "\n\n" + role.strip() + "\n\n"
        rendered[name + ".toml"] = (
            header + "\ndeveloper_instructions = " + multiline_string(instructions) + "\n"
        ).encode("utf-8")
    return rendered


def check(output, rendered):
    matches = True
    with tempfile.TemporaryDirectory(prefix="aboutme-codex-agents.") as temporary:
        candidate = Path(temporary)
        for name, content in rendered.items():
            (candidate / name).write_bytes(content)
        names = set(rendered) | {path.name for path in output.glob("*.toml")}
        for name in sorted(names):
            actual = (output / name).read_bytes() if (output / name).is_file() else b""
            expected = (candidate / name).read_bytes() if name in rendered else b""
            if name in rendered and (output / name).is_file() and actual == expected:
                continue
            matches = False
            print(f"codex-agents: generated file differs: {name}", file=sys.stderr)
            sys.stderr.writelines(difflib.unified_diff(
                actual.decode("utf-8").splitlines(keepends=True),
                expected.decode("utf-8").splitlines(keepends=True),
                fromfile=str(output / name), tofile=f"generated/{name}",
            ))
    if not matches:
        print("codex-agents: run bash scripts/generate-codex-agents.sh", file=sys.stderr)
    return matches


root = Path(sys.argv[1])
parser = argparse.ArgumentParser(prog="scripts/generate-codex-agents.sh")
parser.add_argument("--check", action="store_true", help="check outputs without changing them")
parser.add_argument("--output-dir", type=Path, default=root / ".codex/agents")
args = parser.parse_args(sys.argv[2:])
try:
    rendered = render(root / "scripts/codex-agents")
    if args.check:
        if not check(args.output_dir, rendered):
            sys.exit(1)
        print("codex-agents: generated files are current")
    else:
        args.output_dir.mkdir(parents=True, exist_ok=True)
        for name, content in rendered.items():
            (args.output_dir / name).write_bytes(content)
        print(f"codex-agents: rendered {len(rendered)} roles into {args.output_dir}")
except (OSError, ValueError) as error:
    parser.exit(1, f"codex-agents: {error}\n")
PY
