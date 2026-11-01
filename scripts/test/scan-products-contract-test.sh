#!/usr/bin/env bash

# Black-box contract for full-history secret scanning and scanner failures.
# The fake tools make no network calls.
set -u -o pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
SYSTEM_PATH=/usr/bin:/bin
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-scan-products.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT

repo=$WORK/repo
fake_bin=$WORK/bin
calls=$WORK/calls
mkdir -p "$repo/scripts" "$fake_bin"
cp "$ROOT/scripts/scan.sh" "$repo/scripts/scan.sh"

cat >"$fake_bin/make" <<'EOF'
#!/usr/bin/env bash
printf 'make|%s\n' "$*" >>"$CALL_LOG"
if [ "$#" -eq 2 ] && [ "$1" = tools-check ] && [ "$2" = ARGS=scan ]; then
  exit "${TOOLS_FIXTURE_STATUS:-0}"
fi
printf 'unexpected make invocation: %s\n' "$*" >&2
exit 64
EOF

cat >"$fake_bin/gitleaks" <<'EOF'
#!/usr/bin/env bash
printf 'gitleaks|%s\n' "$*" >>"$CALL_LOG"
exit "${GITLEAKS_FIXTURE_STATUS:-0}"
EOF
chmod +x "$fake_bin"/*

failures=0

fail() {
  printf 'not ok - %s\n' "$1" >&2
  failures=$((failures + 1))
}

run_case() {
  local name=$1 gitleaks_status=$2 expected=$3 tools_status=${4:-0}
  local output status gitleaks_calls failures_before

  : >"$calls"
  failures_before=$failures
  output=$(
    /usr/bin/env GITLEAKS_FIXTURE_STATUS="$gitleaks_status" \
      TOOLS_FIXTURE_STATUS="$tools_status" \
      PATH="$fake_bin:$SYSTEM_PATH" CALL_LOG="$calls" \
      /bin/bash "$repo/scripts/scan.sh" 2>&1
  )
  status=$?

  if [ "$expected" = pass ] && [ "$status" -ne 0 ]; then
    fail "$name returned $status; all configured scanners succeeded"
  elif [ "$expected" = fail ] && [ "$status" -eq 0 ]; then
    fail "$name returned success after a configured scanner failure"
  fi

  gitleaks_calls=$(grep -c '^gitleaks|detect --redact --no-banner$' "$calls" || true)
  local expected_calls=1
  [ "$tools_status" -eq 0 ] || expected_calls=0
  if [ "$gitleaks_calls" -ne "$expected_calls" ]; then
    fail "$name made $gitleaks_calls full-history gitleaks calls; expected $expected_calls"
  fi

  if [ "$failures" -ne "$failures_before" ]; then
    printf '    %s status: %s\n' "$name" "$status" >&2
    printf '    %s output: %s\n' "$name" "${output//$'\n'/ | }" >&2
    printf '    %s calls: %s\n' "$name" "$(tr '\n' ';' <"$calls")" >&2
  fi
}

run_case 'gitleaks succeeds' 0 pass
run_case 'gitleaks fails' 19 fail
run_case 'toolchain unavailable' 0 fail 23

if [ "$failures" -ne 0 ]; then
  exit 1
fi

printf 'ok - scan runs full-history gitleaks and propagates scanner failures\n'
