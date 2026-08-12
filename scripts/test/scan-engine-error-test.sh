#!/usr/bin/env bash

# Black-box regression for connected Semgrep analysis failures. Semgrep can
# suppress an engine error and return success unless the caller opts out.
set -u -o pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
SYSTEM_PATH=/usr/bin:/bin
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-scan-engine-error.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT

repo=$WORK/repo
fake_bin=$WORK/bin
calls=$WORK/calls
mkdir -p "$repo/scripts" "$fake_bin"
cp "$ROOT/scripts/scan.sh" "$repo/scripts/scan.sh"
: >"$calls"

cat >"$fake_bin/make" <<'EOF'
#!/usr/bin/env bash
printf 'make|%s\n' "$*" >>"$CALL_LOG"
if [ "$#" -eq 2 ] && [ "$1" = tools-check ] && [ "$2" = ARGS=scan ]; then
  exit 0
fi
printf 'unexpected make invocation: %s\n' "$*" >&2
exit 64
EOF
chmod +x "$fake_bin/make"

cat >"$fake_bin/semgrep" <<'EOF'
#!/usr/bin/env bash
printf 'semgrep|%s\n' "$*" >>"$CALL_LOG"
fail_closed=0
for argument in "$@"; do
  if [ "$argument" = --no-suppress-errors ]; then
    fail_closed=1
  fi
done
printf 'semgrep analysis engine error: simulated worker crash\n' >&2
if [ "$fail_closed" -eq 1 ]; then
  exit 70
fi
exit 0
EOF
chmod +x "$fake_bin/semgrep"

cat >"$fake_bin/gitleaks" <<'EOF'
#!/usr/bin/env bash
printf 'gitleaks|%s\n' "$*" >>"$CALL_LOG"
exit 0
EOF
chmod +x "$fake_bin/gitleaks"

output=$(
  /usr/bin/env SEMGREP_APP_TOKEN=fixture-token \
    PATH="$fake_bin:$SYSTEM_PATH" CALL_LOG="$calls" \
    /bin/bash "$repo/scripts/scan.sh" 2>&1
)
status=$?
failed=0

if [ "$status" -eq 0 ]; then
  printf 'not ok - scan returned clean after a Semgrep engine error\n' >&2
  failed=1
fi
if [[ $output == *"Scan clean."* ]] || [[ $output == *"Scan gate passed."* ]]; then
  printf 'not ok - scan printed a passing result after a Semgrep engine error\n' >&2
  failed=1
fi
if [[ $output != *"semgrep analysis engine error: simulated worker crash"* ]]; then
  printf 'not ok - the Semgrep engine-error fixture was not observed\n' >&2
  failed=1
fi

tool_checks=0
semgrep_calls=0
gitleaks_calls=0
while IFS= read -r call; do
  case $call in
  'make|tools-check ARGS=scan')
    tool_checks=$((tool_checks + 1))
    ;;
  semgrep\|ci*)
    semgrep_calls=$((semgrep_calls + 1))
    ;;
  'gitleaks|detect --redact --no-banner')
    gitleaks_calls=$((gitleaks_calls + 1))
    ;;
  esac
done <"$calls"

if [ "$tool_checks" -ne 1 ]; then
  printf 'not ok - expected one pinned scanner tool check, observed %s\n' "$tool_checks" >&2
  failed=1
fi
if [ "$semgrep_calls" -ne 1 ]; then
  printf 'not ok - expected one connected Semgrep call, observed %s\n' "$semgrep_calls" >&2
  failed=1
fi
if [ "$gitleaks_calls" -ne 1 ]; then
  printf 'not ok - expected one explicit full-history gitleaks call, observed %s\n' "$gitleaks_calls" >&2
  failed=1
fi

if [ "$failed" -ne 0 ]; then
  printf '    observed status: %s\n' "$status" >&2
  printf '    observed output: %s\n' "${output//$'\n'/ | }" >&2
  printf '    observed calls: %s\n' "$(tr '\n' ';' <"$calls")" >&2
  exit 1
fi

printf 'ok - connected scan fails closed on a Semgrep engine error\n'
