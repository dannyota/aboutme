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
