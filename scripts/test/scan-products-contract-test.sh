#!/usr/bin/env bash

# Black-box contract for the phase scan's connected product selection and
# aggregate scanner status. The fake tools make no network calls.
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
  exit 0
fi
printf 'unexpected make invocation: %s\n' "$*" >&2
exit 64
EOF

cat >"$fake_bin/semgrep" <<'EOF'
#!/usr/bin/env bash
printf 'semgrep' >>"$CALL_LOG"
printf '|%s' "$@" >>"$CALL_LOG"
printf '\n' >>"$CALL_LOG"

[ "${1:-}" = ci ] || exit 64
for required in --code --supply-chain --secrets --no-suppress-errors; do
  count=0
  for argument in "$@"; do
    if [ "$argument" = "$required" ]; then
      count=$((count + 1))
    fi
  done
  if [ "$count" -ne 1 ]; then
    printf 'required Semgrep product option %s appeared %s times\n' \
      "$required" "$count" >&2
    exit 64
  fi
done

exit "${SEMGREP_FIXTURE_STATUS:-0}"
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
  local name=$1 semgrep_status=$2 gitleaks_status=$3 expected=$4
  local output status semgrep_calls gitleaks_calls failures_before

  : >"$calls"
  failures_before=$failures
  output=$(
    /usr/bin/env SEMGREP_APP_TOKEN=fixture-token \
      SEMGREP_FIXTURE_STATUS="$semgrep_status" \
      GITLEAKS_FIXTURE_STATUS="$gitleaks_status" \
      PATH="$fake_bin:$SYSTEM_PATH" CALL_LOG="$calls" \
      /bin/bash "$repo/scripts/scan.sh" 2>&1
  )
  status=$?

  if [ "$expected" = pass ] && [ "$status" -ne 0 ]; then
    fail "$name returned $status; all configured scanners succeeded"
  elif [ "$expected" = fail ] && [ "$status" -eq 0 ]; then
    fail "$name returned success after a configured scanner failure"
  fi

  semgrep_calls=$(grep -c '^semgrep|' "$calls" || true)
  gitleaks_calls=$(grep -c '^gitleaks|detect --redact --no-banner$' "$calls" || true)
  if [ "$semgrep_calls" -ne 1 ]; then
    fail "$name made $semgrep_calls connected Semgrep calls; expected one"
  fi
  if [ "$gitleaks_calls" -ne 1 ]; then
    fail "$name made $gitleaks_calls full-history gitleaks calls; expected one"
  fi

  if [ "$failures" -ne "$failures_before" ]; then
    printf '    %s status: %s\n' "$name" "$status" >&2
    printf '    %s output: %s\n' "$name" "${output//$'\n'/ | }" >&2
    printf '    %s calls: %s\n' "$name" "$(tr '\n' ';' <"$calls")" >&2
  fi
}

run_case 'all scanners succeed' 0 0 pass
run_case 'Semgrep fails' 17 0 fail
run_case 'gitleaks fails' 0 19 fail

if [ "$failures" -ne 0 ]; then
  exit 1
fi

printf 'ok - phase scan selects Code, Supply Chain, and Secrets and propagates scanner failures\n'
