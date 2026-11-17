#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
retry=$root/deploy/apt-retry.sh
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail() { printf 'apt-retry: %s\n' "$*" >&2; exit 1; }

[[ -f $retry ]] || fail "missing $retry"
cat >"$work/flaky" <<'STUB'
#!/bin/sh
count=$(($(cat "$FLAKY_STATE" 2>/dev/null || echo 0) + 1))
printf '%s\n' "$count" >"$FLAKY_STATE"
printf 'args:%s\n' "$*" >>"$FLAKY_STATE.args"
[ "$count" -gt "$FLAKY_FAILURES" ]
STUB
chmod +x "$work/flaky"

run_case() {
  local failures=$1 expected_status=$2 expected_attempts=$3 status=0
  rm -f "$work/state" "$work/state.args"
  FLAKY_STATE=$work/state FLAKY_FAILURES=$failures APT_RETRY_DELAYS='0 0' \
    sh "$retry" "$work/flaky" -o Acquire::Retries=5 install pkg >"$work/out" 2>"$work/err" || status=$?
  [[ $status == "$expected_status" ]] || fail "$failures failures: exit $status, want $expected_status"
  [[ $(cat "$work/state") == "$expected_attempts" ]] || fail "$failures failures: $(cat "$work/state") attempts, want $expected_attempts"
  [[ $(sort -u "$work/state.args") == 'args:-o Acquire::Retries=5 install pkg' ]] ||
    fail "$failures failures: arguments changed between attempts"
}
run_case 0 0 1
[[ ! -s $work/err ]] || fail 'a first-try success printed a retry message'
run_case 1 0 2
grep -qxF 'apt-retry: attempt 1 failed; retrying in 0s' "$work/err" || fail 'first retry was not announced'
run_case 2 0 3
grep -qxF 'apt-retry: attempt 2 failed; retrying in 0s' "$work/err" || fail 'second retry was not announced'
run_case 3 1 3
grep -qxF 'apt-retry: giving up after 3 attempts' "$work/err" || fail 'the final failure was not reported'

dockerfile=$(<"$root/deploy/server.Dockerfile")
[[ $dockerfile == *'--mount=type=bind,source=deploy/apt-retry.sh,target=/run/apt-retry.sh'* ]] ||
  fail 'server image does not mount the apt retry wrapper'
for step in update '-y --no-install-recommends upgrade' '-y --no-install-recommends install'; do
  [[ $dockerfile == *"sh /run/apt-retry.sh \$apt $step"* ]] || fail "server image apt $step does not retry"
done
[[ $dockerfile != *'&& $apt '* ]] || fail 'server image runs apt without the retry wrapper'
grep -qE '^APT_RETRY_DELAYS=\$\{APT_RETRY_DELAYS:-60 180\}$' "$retry" || fail 'default delays are not 60 and 180 seconds'

echo 'apt retry tests passed'
