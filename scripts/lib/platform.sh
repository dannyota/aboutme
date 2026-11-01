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
    python3 -c 'import os,sys; pid=os.fork(); pid and sys.exit(0); os.setsid(); os.execvp(sys.argv[1],sys.argv[1:])' "$@"
  fi
}

file_mode() {
  stat -c '%a' "$1" 2>/dev/null || stat -f '%Lp' "$1" 2>/dev/null
}
