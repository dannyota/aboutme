#!/usr/bin/env bash
# Exercises startup failure evidence without launching services.
set -Eeuo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
unset DEV_HTTPS_DIAGNOSTICS_ARTIFACT_DIR DEV_HTTPS_PID_AUDIT_FILE
fail() { printf 'dev-https-diagnostics-test: %s\n' "$*" >&2; exit 1; }
mkdir -p "$work/diagnostics" "$work/log" "$work/run" "$work/bin"
cat >"$work/dev-https.sh" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
STARTUP_ARMED=1
CURRENT_SERVICE=server
LOG_DIR=$FIXTURE/log
RUN_DIR=$FIXTURE/run
logfile() { printf '%s/%s.log' "$LOG_DIR" "$1"; }
redact_logs() { sed 's/synthetic-private-value/[REDACTED]/g'; }
trap 'printf "rollback-started\n" >&2' EXIT
start_stack() { start_server; }
start_server() { start_service; }
start_service() { validate_session_anchor || return 7; }
validate_session_anchor() { return 1; }
if [ "${BREAK_DIAGNOSTICS:-0}" = 1 ]; then
  for target in timeline rejections report; do
    suffix=log
    [ "$target" != report ] || suffix=txt
    ln -s "$FIXTURE/missing/$target" "$DEV_HTTPS_DIAGNOSTICS_DIR/$target.$$.$suffix"
  done
fi
start_stack
EOF
cat >"$work/bin/lsof" <<'EOF'
#!/usr/bin/env bash
printf 'p123\nn127.0.0.1:20441\n'
EOF
cat >"$work/bin/ss" <<'EOF'
#!/usr/bin/env bash
port=${!#}
port=${port##*:}
printf 'LISTEN 0 4096 127.0.0.1:%s 0.0.0.0:* users:(("fixture",pid=123,fd=8))\n' "$port"
EOF
chmod +x "$work/bin/lsof" "$work/bin/ss"
printf 'server stdout\nserver stderr synthetic-private-value\n' >"$work/log/server.log"
printf 'database=stub provisioned=yes migrated=yes\n' >"$work/diagnostics/database"
printf 'server readiness: not reached\n' >"$work/diagnostics/probe-server"
printf '%s\n' "$$" >"$work/run/server.pid"
printf 'version=2\ncheckout=/synthetic checkout\npid=%s\npgid=%s\nlaunch_token=synthetic-public-token\nprivate=synthetic-private-value\n' "$$" "$$" >"$work/run/session"
status=0
FIXTURE=$work PATH="$work/bin:$PATH" DEV_HTTPS_DIAGNOSTICS_DIR="$work/diagnostics" \
  DEV_HTTPS_DIAGNOSTICS_ARTIFACT_DIR="$work/artifacts" \
  BASH_ENV="$root/scripts/dev-https-test-diagnostics.sh" \
  bash "$work/dev-https.sh" up >"$work/output" 2>&1 || status=$?
[ "$status" -eq 7 ] || fail "diagnostics changed exit 7 to $status"
for expected in 'startup failure status=7' 'function=validate_session_anchor' \
  'startup timeline' 'combined stdout and stderr' \
  'step=start_server duration=' \
  'rejected' \
  'server stdout' 'server stderr [REDACTED]' 'server readiness: not reached' \
  'database=stub provisioned=yes migrated=yes' '123' \
  'process state' 'session record' 'version=2' 'pgid=' 'rollback-started'; do
  grep -Fq -- "$expected" "$work/output" || fail "missing evidence: $expected"
done
awk -v pid="$$" '
  /process state/ { processes=1; next }
  / record / { processes=0 }
  processes && $1 == pid && NF == 5 { found++ }
  END { exit !(found == 1) }
' "$work/output" || fail 'process snapshot lost the supervisor pidfile PID'
! compgen -G "$work/diagnostics/locations.*.log" >/dev/null || fail 'diagnostics wrote a command trail'
! grep -Fq 'synthetic-private-value' "$work/output" || fail 'diagnostics exposed private log content'
[ "$(grep -Fc 'startup failure status=' "$work/output")" -eq 1 ] || fail 'failure evidence repeated'
awk '/startup failure status=/ { evidence=NR } /rollback-started/ { exit !(evidence && evidence < NR) }' \
  "$work/output" || fail 'diagnostics ran after rollback'
artifacts=("$work/artifacts/"*.txt)
[ "${#artifacts[@]}" -eq 1 ] || fail 'sanitized artifact report count is wrong'
[ -f "${artifacts[0]}" ] || fail 'sanitized artifact report is missing'
grep -Fq 'server stderr [REDACTED]' "${artifacts[0]}" || fail 'artifact lost the service stderr'
! grep -Fq 'synthetic-private-value' "${artifacts[0]}" || fail 'artifact exposed private log content'

# Audit and service pidfiles can name the same process.
printf '%s\n' "$$" "$$" invalid >"$work/pids"
printf '%s\n' "$$" >"$work/run/web.pid"

# A probe that ran retains its last response alongside the same failure.
printf 'server readiness: exit=0 response=stubbed-success body=empty\n' >"$work/diagnostics/probe-server"
status=0
FIXTURE=$work PATH="$work/bin:$PATH" DEV_HTTPS_PID_AUDIT_FILE="$work/pids" \
  DEV_HTTPS_DIAGNOSTICS_DIR="$work/diagnostics" \
  BASH_ENV="$root/scripts/dev-https-test-diagnostics.sh" \
  bash "$work/dev-https.sh" up >"$work/output" 2>&1 || status=$?
[ "$status" -eq 7 ] || fail 'probe evidence changed the failure status'
grep -Fq 'server readiness: exit=0 response=stubbed-success body=empty' "$work/output" ||
  fail 'diagnostics lost the last readiness response'
awk -v pid="$$" '
  /process state/ { processes=1; next }
  / record / { processes=0 }
  processes && $1 == pid && NF == 5 { found++ }
  END { exit !(found == 1) }
' "$work/output" || fail 'process snapshot did not retain one row for duplicate PIDs'

# Broken log targets must preserve the startup failure status.
status=0
BREAK_DIAGNOSTICS=1 FIXTURE=$work PATH="$work/bin:$PATH" \
  DEV_HTTPS_DIAGNOSTICS_DIR="$work/diagnostics" \
  BASH_ENV="$root/scripts/dev-https-test-diagnostics.sh" \
  bash "$work/dev-https.sh" up >"$work/output" 2>&1 || status=$?
[ "$status" -eq 7 ] || fail "diagnostic write failure changed exit 7 to $status"
grep -Fq 'rollback-started' "$work/output" || fail 'diagnostic write failure prevented rollback'

# A successful startup emits no failure evidence and needs no diagnostic tools.
sed -e 's/return 7/return 0/' -e 's/return 1/return 0/' "$work/dev-https.sh" >"$work/success"
mv "$work/success" "$work/dev-https.sh"
DEV_HTTPS_DIAGNOSTICS_DIR="$work/diagnostics" FIXTURE=$work \
  BASH_ENV="$root/scripts/dev-https-test-diagnostics.sh" \
  bash "$work/dev-https.sh" up >"$work/output" 2>&1 || fail 'diagnostics failed a successful startup'
! grep -Fq 'startup failure status=' "$work/output" || fail 'success emitted failure evidence'
BREAK_DIAGNOSTICS=1 DEV_HTTPS_DIAGNOSTICS_DIR="$work/diagnostics" FIXTURE=$work \
  BASH_ENV="$root/scripts/dev-https-test-diagnostics.sh" \
  bash "$work/dev-https.sh" up >"$work/output" 2>&1 || fail 'diagnostic write failure changed exit 0'
! grep -Fq 'startup failure status=' "$work/output" || fail 'write failure reported a successful startup as failed'
printf 'dev-https diagnostics tests: PASS\n'
