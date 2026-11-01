"""Select the narrow prose gate. Errors always retain the full gate."""

import os
import re
import selectors
import subprocess
import sys
import time

MAX_OUTPUT = 16 * 1024 * 1024
TIME_LIMIT = 30


def git(arguments, deadline):
    remaining = deadline - time.monotonic()
    if remaining <= 0:
        raise TimeoutError()
    process = subprocess.Popen(
        ["git", *arguments], stdout=subprocess.PIPE, stderr=subprocess.DEVNULL
    )
    try:
        with selectors.DefaultSelector() as reader:
            reader.register(process.stdout, selectors.EVENT_READ)
            output = bytearray()
            while True:
                remaining = deadline - time.monotonic()
                if remaining <= 0 or not reader.select(remaining):
                    raise TimeoutError()
                chunk = os.read(process.stdout.fileno(), 65536)
                if not chunk:
                    break
                output.extend(chunk)
                if len(output) > MAX_OUTPUT:
                    raise ValueError("output limit")
        if process.wait(timeout=max(0, deadline - time.monotonic())) != 0:
            raise ValueError("git failed")
        return bytes(output)
    finally:
        if process.poll() is None:
            process.kill()
        process.wait()
        process.stdout.close()


def allowed(path):
    name = path.decode("utf-8", errors="strict")
    if any(ord(char) < 32 or 127 <= ord(char) <= 159 for char in name):
        return False
    if name.split("/")[-1] in ("AGENTS.md", "CLAUDE.md"):
        return False
    if name.startswith("docs/plans/traceability/"):
        return False
    return bool(
        re.fullmatch(r"docs/adr/[0-9]{4}-[^/]*\.md", name)
        or re.fullmatch(r"docs/plans/(?:[^/]+/)*[^/]*\.md", name)
    )


def parse_changes(data):
    records = data.split(b"\0")
    if not data or records.pop() != b"":
        raise ValueError("incomplete diff")
    changes = []
    index = 0
    while index < len(records):
        status = records[index]
        index += 1
        rename = re.fullmatch(rb"R[0-9]{1,3}", status)
        if rename and int(status[1:]) > 100:
            raise ValueError("invalid rename score")
        if status not in (b"A", b"M", b"D") and not rename:
            raise ValueError("unknown status")
        count = 2 if rename else 1
        paths = records[index:index + count]
        if len(paths) != count or not all(paths):
            raise ValueError("missing path")
        changes.append((status, paths))
        index += count
    return changes


def tree(commit, deadline):
    data = git(["ls-tree", "-rz", "--full-tree", commit, "--"], deadline)
    records = data.split(b"\0")
    if records.pop() != b"":
        raise ValueError("incomplete tree")
    entries = {}
    for record in records:
        metadata, path = record.split(b"\t", 1)
        mode, kind, oid = metadata.split(b" ")
        if not path or path in entries or not re.fullmatch(rb"[0-9a-f]{40,64}", oid):
            raise ValueError("invalid tree")
        entries[path] = (mode, kind)
    return entries


def select(event, base):
    if event != "pull_request":
        return "full"
    try:
        deadline = time.monotonic() + TIME_LIMIT
        commits = []
        for ref in (base, "HEAD"):
            commit = git(["rev-parse", "--verify", "--end-of-options", ref + "^{commit}"], deadline).strip()
            if not re.fullmatch(rb"[0-9a-f]{40,64}", commit):
                raise ValueError("invalid commit")
            commits.append(commit.decode("ascii"))
        changes = parse_changes(git([
            "diff", "--no-ext-diff", "--no-textconv", "--name-status", "-z",
            "--find-renames", *commits, "--",
        ], deadline))
        if not all(allowed(path) for _, paths in changes for path in paths):
            return "full"
        before, after = (tree(commit, deadline) for commit in commits)
        regular = (b"100644", b"blob")
        for status, paths in changes:
            old, new = paths[0], paths[-1]
            if status == b"A":
                valid = old not in before and after.get(new) == regular
            elif status == b"D":
                valid = before.get(old) == regular and new not in after
            else:
                valid = before.get(old) == regular and after.get(new) == regular
            if not valid:
                return "full"
        if time.monotonic() >= deadline:
            return "full"
        return "prose"
    except Exception:
        return "full"


if __name__ == "__main__":
    try:
        selection = select(sys.argv[1], sys.argv[2]) if len(sys.argv) == 3 else "full"
    except Exception:
        selection = "full"
    print(selection)
