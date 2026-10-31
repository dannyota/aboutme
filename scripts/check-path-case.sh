#!/usr/bin/env bash
set -euo pipefail
git ls-files -z | python3 -I -c '
import sys
import unicodedata

groups = {}
for raw in sys.stdin.buffer.read().split(b"\0"):
    if not raw:
        continue
    path = raw.decode("utf-8", "surrogateescape")
    prefix = path
    # Include directory prefixes: Docs/x.md and docs/y.md also clash on macOS.
    while prefix:
        key = unicodedata.normalize("NFC", prefix).casefold()
        groups.setdefault(key, {}).setdefault(prefix, set()).add(path)
        prefix = prefix.rpartition("/")[0]

status = 0
for key, spellings in sorted(groups.items()):
    if len(spellings) < 2:
        continue
    print(f"check-path-case: case-insensitive path collision ({ascii(key)}):")
    for path in sorted(set().union(*spellings.values())):
        print(f"  {ascii(path)}")
    status = 1
sys.exit(status)
'
