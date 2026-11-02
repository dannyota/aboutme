#!/usr/bin/env bash
set -Eeuo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
source scripts/lib/platform.sh
work=$(mktemp -d "${TMPDIR:-/tmp}/platform-helpers.XXXXXX")
trap 'if [ -n "${anchor_pid:-}" ]; then kill -KILL -- "-$anchor_pid" 2>/dev/null || true; fi; rm -rf -- "$work"' EXIT
printf abc >"$work/file"
chmod 0600 "$work/file"
mkdir "$work/directory"
chmod 0700 "$work/directory"
ln -s file "$work/link"
[ "$(platform_stat u "$work/file")" = "$(id -u)" ]
[ "$(platform_stat a "$work/file")" = 600 ]
[ "$(platform_stat a "$work/directory")" = 700 ]
[ "$(platform_stat s "$work/file")" = 3 ]
[ "$(platform_stat h "$work/file")" = 1 ]
source <(sed -n '/^browser_stat() {$/,/^}$/p' deploy/dev-https-browser/run.sh)
[ "$(browser_stat u "$work/file")" = "$(id -u)" ]
[ "$(browser_stat a "$work/file")" = 600 ]
[ "$(browser_stat s "$work/file")" = 3 ]
[ "$(browser_stat h "$work/file")" = 1 ]
mkdir "$work/bin"
cat >"$work/bin/uname" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "${LISTENER_OS:-Darwin}"
EOF
cat >"$work/bin/stat" <<'EOF'
#!/usr/bin/env bash
exit 1
EOF
chmod 0700 "$work/bin/uname" "$work/bin/stat"
(
  export PATH="$work/bin:$PATH"
  [ "$(browser_stat u "$work/file")" = "$(id -u)" ]
  [ "$(browser_stat a "$work/file")" = 600 ]
  [ "$(browser_stat s "$work/file")" = 3 ]
  [ "$(browser_stat h "$work/file")" = 1 ]
)
[ "$(platform_realpath "$work/link")" = "$(cd "$work" && pwd -P)/file" ]
[ "$(platform_stat s "$work/link")" = 4 ]
if platform_stat u "$work/missing" 2>/dev/null || platform_realpath "$work/missing" 2>/dev/null; then
  printf '%s\n' 'missing path accepted' >&2
  exit 1
fi
[ "$(platform_sha256sum "$work/file" | awk '{print $1}')" = ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad ]
[ "$(printf abc | platform_sha256sum | awk '{print $1}')" = ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad ]
(
  cd "$work"
  names=('back\slash' $'new\nline' $'carriage\rreturn')
  for name in "${names[@]}"; do
    printf abc >"$name"
  done
  digest=ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad
  printf '\\%s  back\\\\slash\n\\%s  new\\nline\n\\%s  carriage\\rreturn\n' "$digest" "$digest" "$digest" >expected
  platform_sha256sum "${names[@]}" >actual
  if command -v sha256sum >/dev/null && sha256sum --version 2>/dev/null | head -n 1 | grep -q 'GNU coreutils'; then
    sha256sum "${names[@]}" >gnu
    cmp expected gnu
    cmp gnu actual
  else
    cmp expected actual
  fi
)
mkdir "$work/evidence"
for name in 'proof.a space' 'proof.b space' 'proof.c space'; do
  mkdir "$work/evidence/$name"
done
mkdir "$work/unrelated"
ln -s "$work/unrelated" "$work/evidence/proof.link"
python3 - "$work/evidence" <<'PY'
import os, sys
for name in os.listdir(sys.argv[1]):
    path = os.path.join(sys.argv[1], name)
    if not os.path.islink(path):
        os.utime(path, ns=(1000000000, 1000000000))
PY
platform_prune_evidence "$work/evidence" proof 2
[ ! -e "$work/evidence/proof.a space" ]
[ -d "$work/evidence/proof.b space" ] && [ -d "$work/evidence/proof.c space" ]
[ -L "$work/evidence/proof.link" ] && [ -d "$work/unrelated" ]
mkdir "$work/ties"
mkdir "$work/ties/proof.a" "$work/ties/proof.Z"
python3 - "$work/ties" <<'PY'
import os, sys
for name in os.listdir(sys.argv[1]):
    os.utime(os.path.join(sys.argv[1], name), ns=(1000000000, 1000000000))
PY
first=$(printf '1 %s\n' "$work/ties/proof.a" "$work/ties/proof.Z" | LC_ALL=C sort -rn | head -n 1)
survivor=${first##*/}
[ "$survivor" = proof.a ]
LC_ALL=C platform_prune_evidence "$work/ties" proof 1
[ -d "$work/ties/$survivor" ]
[ ! -e "$work/ties/proof.Z" ]
# Snapshot commands keep spaces and obtain numeric session IDs on both hosts.
row=$(platform_process_snapshot "$$")
read -r pid ppid pgid uid sid state command <<<"$row"
[ "$pid" = "$$" ] && [ "$uid" = "$EUID" ]
[[ $sid =~ ^[1-9][0-9]*$ && -n $command ]]
for bad in 0 1 -1 invalid; do
  if platform_signal_group TERM "$bad" ignored; then exit 1; fi
done
if platform_signal_group TERM "$pgid" ignored; then
  printf 'signalled own group\n' >&2; exit 1
fi
# Every listener, including IPv6 and multiple owners, needs an identified PID.
cat >"$work/bin/lsof" <<'EOF'
#!/usr/bin/env bash
case $LISTENER_CASE in
empty | omitted) exit 1 ;;
hidden) printf 'n[::1]:20443\n' ;;
malformed) printf 'p123\nn127.0.0.1:wrong\n' ;;
error) printf 'inspection denied\n' >&2; exit 1 ;;
*) printf 'p123\nn127.0.0.1:20443\np456\nn[::1]:20443\n' ;;
esac
EOF
chmod 0700 "$work/bin/lsof"
[ "$(PATH="$work/bin:$PATH" LISTENER_CASE=valid platform_listener_pids 20443)" = $'123\n456' ]
for scenario in hidden malformed error; do
  if PATH="$work/bin:$PATH" LISTENER_CASE=$scenario platform_listener_pids 20443; then exit 1; fi
done
status=0
PATH="$work/bin:$PATH" LISTENER_CASE=empty platform_listener_pids 20443 || status=$?
[ "$status" = 1 ]
# Linux must see sockets that an unprivileged lsof omits entirely, including
# a hidden listener beside another socket with a visible owner.
cat >"$work/bin/ss" <<'EOF'
#!/usr/bin/env bash
case $LISTENER_CASE in
empty) exit 0 ;;
error) printf 'inspection denied\n' >&2; exit 1 ;;
malformed) printf 'malformed\n'; exit 0 ;;
hidden | omitted) printf 'LISTEN 0 4096 [::1]:20443 [::]:*\n'; exit 0 ;;
esac
printf 'LISTEN 0 4096 127.0.0.1:20443 0.0.0.0:* users:(("node",pid=123,fd=8),("node",pid=456,fd=9))\n'
if [ "$LISTENER_CASE" = mixed ]; then
  printf 'LISTEN 0 4096 [::1]:20443 [::]:*\n'
fi
EOF
chmod 0700 "$work/bin/ss"
[ "$(PATH="$work/bin:$PATH" LISTENER_OS=Linux LISTENER_CASE=valid platform_listener_pids 20443)" = $'123\n456' ]
for scenario in hidden omitted mixed malformed error; do
  status=0
  PATH="$work/bin:$PATH" LISTENER_OS=Linux LISTENER_CASE=$scenario platform_listener_pids 20443 || status=$?
  [ "$status" = 2 ] || { printf 'Linux listener %s returned %s, expected busy or unknown\n' "$scenario" "$status" >&2; exit 1; }
done
status=0
PATH="$work/bin:$PATH" LISTENER_OS=Linux LISTENER_CASE=empty platform_listener_pids 20443 || status=$?
[ "$status" = 1 ]
# Force the Python detach path and verify complete argv and UID checks.
mkdir "$work/fallback"
ln -s "$(command -v python3)" "$work/fallback/python3"
cat >"$work/anchor with spaces.sh" <<'EOF'
#!/bin/bash
trap 'exit 0' TERM
printf '%s\n' "$$" >"$1"
/bin/sleep 300 &
wait "$!"
EOF
PATH="$work/fallback" detach /bin/bash "$work/anchor with spaces.sh" "$work/anchor.pid" public-token
for _ in $(seq 1 50); do [ -s "$work/anchor.pid" ] && break; sleep 0.02; done
anchor_pid=$(<"$work/anchor.pid")
expected="/bin/bash $work/anchor with spaces.sh $work/anchor.pid public-token"
platform_validate_anchor "$anchor_pid" "$expected"
platform_group_members "$anchor_pid" | grep -Fxq "$anchor_pid"
if platform_signal_group TERM "$anchor_pid" "$expected-other"; then exit 1; fi
kill -0 "$anchor_pid"
# Controlled ps rows still use a real process for os.getsid.
cat >"$work/bin/ps" <<'EOF'
#!/usr/bin/env bash
if [ "$1" = -o ]; then exec /bin/ps "$@"; fi
printf '%s\n' "$SNAPSHOT_ROW"
EOF
chmod 0700 "$work/bin/ps"
SNAPSHOT_ROW="$anchor_pid 1 $anchor_pid $EUID S $expected"
export SNAPSHOT_ROW
PATH="$work/bin:$PATH" platform_validate_anchor "$anchor_pid" "$expected"
for SNAPSHOT_ROW in \
  "$anchor_pid 1 $anchor_pid 999999 S $expected" \
  "$anchor_pid 1 $anchor_pid $EUID Z $expected" \
  "$anchor_pid 1 $anchor_pid $EUID S $expected extra" \
  "$anchor_pid 1 $anchor_pid $EUID S" \
  'malformed'; do
  if PATH="$work/bin:$PATH" platform_validate_anchor "$anchor_pid" "$expected"; then exit 1; fi
done
kill -0 "$anchor_pid"
platform_signal_group TERM "$anchor_pid" "$expected"

# Draining membership must not compare separate, changing process lists or
# resolve sessions for children that can exit after ps takes its snapshot.
(
  export PATH="$work/bin:$PATH" GROUP_ANCHOR=$$ GROUP_UID=$EUID
  export GROUP_PS_LOG=$work/group-ps.calls GROUP_PS_CASE=draining GROUP_OS=Darwin
  cat >"$work/bin/uname" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$GROUP_OS"
EOF
  cat >"$work/bin/ps" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$GROUP_PS_LOG"
case $GROUP_PS_CASE in
error) printf 'inspection denied\n' >&2; exit 1 ;;
silent-error) exit 9 ;;
empty) exit 1 ;;
empty-success) exit 0 ;;
absent) printf '1 1\n'; exit 0 ;;
failed-output) printf '%s %s\n' "$GROUP_ANCHOR" "$GROUP_ANCHOR"; exit 1 ;;
malformed) printf 'malformed\n'; exit 0 ;;
duplicate) printf '%s %s\n%s %s\n' "$GROUP_ANCHOR" "$GROUP_ANCHOR" "$GROUP_ANCHOR" "$GROUP_ANCHOR"; exit 0 ;;
foreign) printf '%s 1\n' "$GROUP_ANCHOR"; exit 0 ;;
esac
case " $* " in
*' -axo '*)
  if [[ $* == *pid=,pgid= ]]; then
    printf '900000001 %s\n1 1\n%s %s\n' "$GROUP_ANCHOR" "$GROUP_ANCHOR" "$GROUP_ANCHOR"
  else
    printf '%s 1 %s %s S anchor\n900000001 %s %s %s S exiting-child\n' \
      "$GROUP_ANCHOR" "$GROUP_ANCHOR" "$GROUP_UID" "$GROUP_ANCHOR" "$GROUP_ANCHOR" "$GROUP_UID"
  fi
  ;;
*" -g $GROUP_ANCHOR "*)
  if [[ $* == *pid=,pgid= ]]; then
    printf '%s %s\n' "$GROUP_ANCHOR" "$GROUP_ANCHOR"
  else
    printf '%s\n' "$GROUP_ANCHOR"
  fi
  ;;
*) exit 9 ;;
esac
EOF
  for GROUP_OS in Darwin Linux; do
    : >"$GROUP_PS_LOG"
    members=$(platform_group_members "$GROUP_ANCHOR")
    if [ "$GROUP_OS" = Darwin ]; then
      [ "$members" = "$GROUP_ANCHOR" ]
    else
      [ "$members" = "$(printf '%s\n900000001' "$GROUP_ANCHOR")" ]
    fi
    [ "$(wc -l <"$GROUP_PS_LOG")" -eq 1 ]
    for GROUP_PS_CASE in error silent-error empty-success failed-output malformed duplicate; do
      status=0
      platform_group_members "$GROUP_ANCHOR" || status=$?
      [ "$status" = 2 ]
    done
    GROUP_PS_CASE=empty
    status=0
    members=$(platform_group_members "$GROUP_ANCHOR") || status=$?
    [ -z "$members" ]
    if [ "$GROUP_OS" = Darwin ]; then [ "$status" = 0 ]; else [ "$status" = 2 ]; fi
    if [ "$GROUP_OS" = Linux ]; then
      GROUP_PS_CASE=absent
      members=$(platform_group_members "$GROUP_ANCHOR")
      [ -z "$members" ]
    fi
    GROUP_PS_CASE=draining
  done
  GROUP_OS=Darwin GROUP_PS_CASE=foreign
  status=0
  platform_group_members "$GROUP_ANCHOR" || status=$?
  [ "$status" = 2 ]
  for invalid in 0 1 -1 invalid; do
    : >"$GROUP_PS_LOG"
    status=0
    platform_group_members "$invalid" || status=$?
    [ "$status" = 2 ] && [ ! -s "$GROUP_PS_LOG" ]
  done
)

# Linux inspection needs ss even when lsof is unavailable.
ln -s "$(command -v bash)" "$work/fallback/bash"
cat >"$work/fallback/uname" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' Linux
EOF
cat >"$work/fallback/ss" <<'EOF'
#!/usr/bin/env bash
if [ "$LISTENER_CASE" = hidden ]; then
  printf 'LISTEN 0 4096 [::1]:20443 [::]:*\n'
else
  printf 'LISTEN 0 4096 127.0.0.1:20443 0.0.0.0:* users:(("node",pid=123,fd=8),("node",pid=456,fd=9))\n'
fi
EOF
chmod 0700 "$work/fallback/ss" "$work/fallback/uname"
[ "$(PATH="$work/fallback" LISTENER_CASE=valid platform_listener_pids 20443)" = $'123\n456' ]
if PATH="$work/fallback" LISTENER_CASE=hidden platform_listener_pids 20443; then exit 1; fi
printf '%s\n' 'platform helper tests: PASS'
