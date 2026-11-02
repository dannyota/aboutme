#!/usr/bin/env bash
# Shared local tooling prerequisites and process helpers.
require_modern_bash() {
  if ((BASH_VERSINFO[0] < 4)); then
    printf 'Bash 4+ is required; put Homebrew bash on PATH on macOS\n' >&2
    return 1
  fi
}

require_gnu_tools() {
  local tool version
  for tool in "$@"; do
    version=$("$tool" --version 2>/dev/null) || version=
    if [[ $version != *'GNU coreutils'* ]]; then
      printf '%s from GNU coreutils is required on PATH\n' "$tool" >&2
      return 1
    fi
  done
}

port_listening() {
  local listeners
  if command -v ss >/dev/null; then
    listeners=$(ss -H -tln "sport = :$1" 2>/dev/null || true)
  elif command -v lsof >/dev/null; then
    listeners=$(lsof -nP -iTCP:"$1" -sTCP:LISTEN 2>/dev/null || true)
  else
    printf 'ss or lsof is required for port probes\n' >&2
    return 2
  fi
  [ -n "$listeners" ]
}

detach() {
  if command -v setsid >/dev/null; then
    setsid --fork "$@"
  else
    python3 -I -c 'import os,sys; pid=os.fork(); pid and sys.exit(0); os.setsid(); os.execvp(sys.argv[1],sys.argv[1:])' "$@"
  fi
}

file_mode() {
  stat -c '%a' "$1" 2>/dev/null || stat -f '%Lp' "$1" 2>/dev/null
}

# Metadata and hashing for host-side HTTPS browser checks.
platform_stat() {
  python3 -I -c 'import os,stat,sys
s = os.lstat(sys.argv[2])
print({"u": s.st_uid, "a": format(stat.S_IMODE(s.st_mode), "o"), "s": s.st_size, "h": s.st_nlink}[sys.argv[1]])' "$1" "$2"
}

platform_realpath() {
  python3 -I -c 'import pathlib,sys; print(pathlib.Path(sys.argv[1]).resolve(strict=True))' "$1"
}

platform_sha256sum() {
  python3 -I -c 'import hashlib,sys
paths = sys.argv[1:] or ["-"]
for path in paths:
    digest = hashlib.sha256()
    f = sys.stdin.buffer if path == "-" else open(path, "rb")
    for chunk in iter(lambda: f.read(131072), b""):
        digest.update(chunk)
    if f is not sys.stdin.buffer:
        f.close()
    escaped = "\\" in path or "\n" in path or "\r" in path
    name = path.replace("\\", "\\\\").replace("\n", "\\n").replace("\r", "\\r")
    print(("\\" if escaped else "") + digest.hexdigest() + "  " + name)' "$@"
}

platform_prune_evidence() {
  LC_ALL=C python3 -I -c 'import os,shutil,sys
root, prefix, keep = sys.argv[1], sys.argv[2] + ".", int(sys.argv[3])
with os.scandir(root) as entries:
    dirs = [(e.stat(follow_symlinks=False).st_mtime_ns, os.fsencode(e.path), e.path)
            for e in entries if e.name.startswith(prefix) and e.is_dir(follow_symlinks=False)]
for _, _, path in sorted(dirs, reverse=True)[keep:]:
    shutil.rmtree(path)' "$1" "$2" "$3"
}

# Numeric columns precede the untouched command remainder. A lost process makes
# inspection fail rather than turning incomplete ownership into an empty set.
platform_process_snapshot() {
  python3 -I - "$@" <<'PY'
import os, re, subprocess, sys
group = None
if len(sys.argv) == 3 and sys.argv[1] == "--group":
    group = sys.argv[2]
    sys.argv = sys.argv[:1]
args = ["ps", "-ww", "-axo", "pid=,ppid=,pgid=,uid=,stat=,args="]
if len(sys.argv) > 1:
    if any(not re.fullmatch(r"[1-9][0-9]*", p) for p in sys.argv[1:]):
        sys.exit(2)
    args = ["ps", "-ww", "-p", ",".join(sys.argv[1:]), "-o", "pid=,ppid=,pgid=,uid=,stat=,args="]
try:
    process = subprocess.Popen(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    try:
        output, errors = process.communicate(timeout=1)
    except subprocess.TimeoutExpired:
        process.kill()
        process.communicate()
        sys.exit(2)
    if process.returncode or errors or not output.strip():
        sys.exit(2)
    rows = []
    for line in output.splitlines():
        match = re.fullmatch(r"\s*([1-9][0-9]*)\s+([0-9]+)\s+([0-9]+)\s+([0-9]+)\s+([A-Za-z+<NsLlEXs-]+)\s+(.+)", line)
        if not match:
            sys.exit(2)
        pid, ppid, pgid, uid, state, command = match.groups()
        if int(pid) == process.pid or (group is not None and pgid != group):
            continue
        rows.append((pid, ppid, pgid, uid, str(os.getsid(int(pid))), state, command))
    if len(sys.argv) > 1 and {r[0] for r in rows} != set(sys.argv[1:]):
        sys.exit(2)
    for row in rows:
        print(" ".join(row))
except (OSError, subprocess.SubprocessError, UnicodeError):
    sys.exit(2)
PY
}

platform_group_members() {
  local pgid=$1 host
  [[ $pgid =~ ^[1-9][0-9]*$ ]] && [ "$pgid" -gt 1 ] || return 2
  host=$(uname -s) || return 2
  # Cross-check BSD selection against PGIDs in the same snapshot. Separate
  # listings and getsid calls race children exiting during a group drain.
  # Membership alone never grants authority to signal the group.
  python3 -I - "$pgid" "$host" <<'PY'
import re, subprocess, sys
group, host = sys.argv[1:]
if host == "Darwin":
    args = ["ps", "-ww", "-x", "-g", group, "-o", "pid=,pgid="]
elif host == "Linux":
    args = ["ps", "-ww", "-axo", "pid=,pgid="]
else:
    sys.exit(2)
try:
    result = subprocess.run(args, capture_output=True, text=True, timeout=1)
    if result.stderr:
        sys.exit(2)
    if not result.stdout.strip():
        # BSD ps returns 1 when its explicit selection matches no processes.
        sys.exit(0 if host == "Darwin" and result.returncode == 1 else 2)
    if result.returncode:
        sys.exit(2)
    members, seen = set(), set()
    for line in result.stdout.splitlines():
        match = re.fullmatch(r"\s*([1-9][0-9]*)\s+([0-9]+)\s*", line)
        if not match:
            sys.exit(2)
        pid, pgid = match.groups()
        if pid in seen or (host == "Darwin" and pgid != group):
            sys.exit(2)
        seen.add(pid)
        if pgid == group:
            members.add(pid)
    for pid in sorted(members, key=int):
        print(pid)
except (OSError, UnicodeError, subprocess.SubprocessError):
    sys.exit(2)
PY
}

# Exit 1 means no listeners; exit 2 means inspection could not prove owners.
platform_listener_pids() {
  local host
  host=$(uname -s) || return 2
  python3 -I - "$1" "$host" <<'PY'
import re, subprocess, sys
port, host = sys.argv[1:]
if not port.isdecimal() or not 1 <= int(port) <= 65535:
    sys.exit(2)
if host == "Linux":
    args = ["ss", "-H", "-ltnp", "sport = :" + port]
elif host == "Darwin":
    args = ["lsof", "-nP", "-iTCP:" + port, "-sTCP:LISTEN", "-Fpn"]
else:
    sys.exit(2)
try:
    result = subprocess.run(args, capture_output=True, text=True, timeout=1)
    if result.stderr or result.returncode not in ([0, 1] if host == "Darwin" else [0]):
        sys.exit(2)
    if not result.stdout:
        sys.exit(1)
    if result.returncode:
        sys.exit(2)
    pids = set()
    if host == "Darwin":
        pid, count = None, 0
        for line in result.stdout.splitlines():
            if re.fullmatch(r"p[1-9][0-9]*", line):
                if pid and not count:
                    sys.exit(2)
                pid, count = line[1:], 0
            elif pid and line.startswith("n") and line.endswith(":" + port):
                pids.add(pid)
                count += 1
            else:
                sys.exit(2)
        if not pid or not count:
            sys.exit(2)
    else:
        for line in result.stdout.splitlines():
            fields = line.split(None, 5)
            if len(fields) != 6 or fields[0] != "LISTEN" or not fields[3].endswith(":" + port):
                sys.exit(2)
            owners = re.fullmatch(r"users:\(\((.+)\)\)", fields[5])
            if not owners:
                sys.exit(2)
            for owner in owners[1].split("),("):
                match = re.fullmatch(r'"[^"\n]*",pid=([1-9][0-9]*),fd=[0-9]+', owner)
                if not match:
                    sys.exit(2)
                pids.add(match[1])
    for pid in sorted(pids, key=int):
        print(pid)
except (OSError, UnicodeError, subprocess.SubprocessError):
    sys.exit(2)
PY
}

platform_validate_anchor() {
  local pgid=$1 expected=$2 row pid ppid actual_group uid sid state command
  [[ $pgid =~ ^[1-9][0-9]*$ ]] && [ "$pgid" -gt 1 ] || return 1
  row=$(platform_process_snapshot "$pgid") || return 1
  read -r pid ppid actual_group uid sid state command <<<"$row"
  [ "$pid" = "$pgid" ] && [ "$actual_group" = "$pgid" ] && [ "$sid" = "$pgid" ] &&
    [ "$uid" = "$EUID" ] && [[ $state != [ZX]* ]] && [ "$command" = "$expected" ]
}

platform_signal_group() {
  local signal=$1 pgid=$2 expected=$3 own
  case $signal in TERM | KILL) ;; *) return 1 ;; esac
  [[ $pgid =~ ^[1-9][0-9]*$ ]] && [ "$pgid" -gt 1 ] || return 1
  own=$(ps -o pgid= -p "$BASHPID" 2>/dev/null) || return 1
  own=${own//[[:space:]]/}
  [[ $own =~ ^[1-9][0-9]*$ ]] && [ "$own" != "$pgid" ] || return 1
  platform_validate_anchor "$pgid" "$expected" || return 1
  kill -"$signal" -- "-$pgid"
}
