#!/usr/bin/env bash

# Tests scripts/ci-background.sh: a started command's output and exit status
# reach the waiting step, a timeout fails, and misuse is refused.
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-ci-background.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT
BG=$ROOT/scripts/ci-background.sh
export CI_BACKGROUND_DIR=$WORK/state

fail() {
  printf 'ci-background-test: %s\n' "$*" >&2
  exit 1
}

bash "$BG" start ok bash -c 'echo hello from ok'
bash "$BG" start bad bash -c 'echo about to fail; exit 7'
bash "$BG" start slow sleep 30

bash "$BG" wait ok 30 >"$WORK/ok.out" 2>&1 || fail "a passing command failed its wait"
grep -Fxq 'hello from ok' "$WORK/ok.out" || fail "wait did not print the command's output"

status=0
bash "$BG" wait bad 30 >"$WORK/bad.out" 2>&1 || status=$?
[ "$status" -eq 7 ] || fail "wait exited $status for a command that exited 7"
grep -Fxq 'about to fail' "$WORK/bad.out" || fail "wait hid a failing command's output"

status=0
bash "$BG" wait slow 1 >"$WORK/slow.out" 2>&1 || status=$?
[ "$status" -eq 1 ] || fail "wait exited $status after its time limit, want 1"
grep -Fq 'slow did not finish within 1s' "$WORK/slow.out" ||
  fail "wait did not report its time limit"

status=0
bash "$BG" start ok true >/dev/null 2>&1 || status=$?
[ "$status" -eq 1 ] || fail "a second start of one name exited $status, want 1"

status=0
bash "$BG" wait never 1 >/dev/null 2>&1 || status=$?
[ "$status" -eq 1 ] || fail "waiting for a name never started exited $status, want 1"

for args in 'start Bad true' 'start ../x true' 'start ok' 'wait ok x' 'run ok'; do
  status=0
  # shellcheck disable=SC2086 # each case is a word list on purpose
  bash "$BG" $args >/dev/null 2>&1 || status=$?
  [ "$status" -eq 64 ] || fail "'$args' exited $status, want 64"
done

printf 'ci-background tests passed\n'
