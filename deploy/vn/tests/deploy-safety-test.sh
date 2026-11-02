#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail() { printf 'deployment safety: %s\n' "$*" >&2; exit 1; }

# macOS limits Unix socket paths to 104 bytes, even with a long TMPDIR.
control_path='ControlPath=\$ssh_dir/(ssh-%C|prod\.sock|drill\.sock)("| )'
for script in deploy.sh restore-drill.sh; do
  path=$root/deploy/vn/scripts/$script
  grep -qxF 'ssh_dir=$(mktemp -d /tmp/aboutme-ssh.XXXXXX)' "$path" ||
    fail "$script: SSH socket directory must use a short private /tmp path"
  awk '
    /^trap (on_exit|cleanup) EXIT$/ { cleanup_ready=1 }
    /^(work|run|ssh_dir)=\$\(mktemp -d/ && !cleanup_ready { exit 1 }
  ' "$path" || fail "$script: register cleanup before creating temporary directories"
  while IFS= read -r line; do
    [[ $line =~ $control_path ]] ||
      fail "$script: ControlPath must use a short name in the SSH socket directory"
  done < <(grep 'ControlPath=' "$path")
done

expect_mcp_flag() {
  local expected=$1 context=$2 actual
  actual=$(cimd_effective_mcp_flag) || fail "$context: cannot determine MCP_ENABLED"
  [[ $actual == "$expected" ]] || fail "$context: expected MCP_ENABLED=$expected, got $actual"
}
export ABOUTME_TEST_STATE=$work/state
mkdir -p "$ABOUTME_TEST_STATE/active" "$ABOUTME_TEST_STATE/enabled"

cat >"$work/systemctl" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
state=${ABOUTME_TEST_STATE:?}
if [[ -e $state/pause-admission && ( $* == *'start aboutme-crowdsec-retention-startup.service'* ||
  $* == *'is-active --quiet aboutme-crowdsec-retention-startup.service'* ) ]]; then
  touch "$state/admission-entered"
  while [[ ! -e $state/admission-release ]]; do sleep 0.01; done
  if [[ $1 == start ]]; then
    rm -f "$state/../run/crowdsec-retention-failed"
    touch "$state/../run/crowdsec-retention-ok"
    touch "$state/unwanted-startup"
  fi
fi
case $1 in
  is-active)
    [[ ! -e $state/inspection-failure ]] || exit 4
    [[ -e $state/active/${3} ]] ;;
  show)
    [[ ! -e $state/inspection-failure ]] || exit 1
    [[ ! -e $state/malformed-metadata ]] || { printf 'ActiveState=activating\n'; exit 0; }
    [[ " $* " == *' --all '* ]] || exit 1
    if [[ " $* " == *' --property=MainPID '* ]]; then
      unit=${!#}
      unit=${unit%.service}
      active=inactive pid=0
      [[ ! -e $state/active/$unit ]] || { active=active; pid=1; }
      [[ ! -e $state/transition-persists ]] || active=activating
      printf 'ActiveState=%s\nJob=\nMainPID=%s\nControlPID=0\n' "$active" "$pid"
    else
      printf 'ActiveState=%s\nJob=%s\n' "${NOTICE_UNIT_STATE:-inactive}" "${NOTICE_UNIT_JOB:-}"
    fi
    ;;
  is-enabled) [[ -e $state/enabled/${3} ]] ;;
  stop)
    [[ ! -e $state/transition-persists ]] || exit 124
    if [[ -e $state/timeout-malformed ]]; then touch "$state/malformed-metadata"; exit 124; fi
    [[ ! -e $state/fail-inspection-after-stop ]] || touch "$state/inspection-failure"
    shift
    for unit in "$@"; do rm -f "$state/active/$unit"; done
    ;;
  start)
    shift
    for unit in "$@"; do : >"$state/active/$unit"; done
    ;;
  enable)
    shift
    [[ ${1:-} == --now ]] && shift
    for unit in "$@"; do
      : >"$state/enabled/$unit"
      : >"$state/active/$unit"
    done
    ;;
  *) echo "unexpected systemctl call: $*" >&2; exit 2 ;;
esac
STUB
chmod +x "$work/systemctl"
export PATH=$work:$PATH

sed "s|/run/aboutme|$work/run|g" "$root/deploy/vn/scripts/deploy-safety.sh" >"$work/deploy-safety.sh"
# shellcheck source=/dev/null
source "$work/deploy-safety.sh"

# Edge mutations must join an admitted retention pass or watchdog tick.
# Redirect only host paths; execute the production lock and command helper.
sed -n '/^aboutme_edge_transition() (/,/^)/p' "$root/deploy/vn/scripts/deploy-safety.sh" |
  sed "s|/run/aboutme|$work/run|g" >"$work/edge-lock.sh"
mkdir -p "$work/run"
chmod 0755 "$work/run"
touch "$work/run/crowdsec-retention.lock"
chmod 0600 "$work/run/crowdsec-retention.lock"
# shellcheck source=/dev/null
source "$work/edge-lock.sh"
declare -F aboutme_edge_transition >/dev/null || fail 'missing edge serialization'
for transition in maintenance-up app-down render restore-previous app-up maintenance-down fail-closed; do
  exec {edge_guard}>"$work/run/crowdsec-retention.lock"
  flock -x "$edge_guard"
  (exec {edge_guard}>&-; aboutme_edge_transition touch "$work/$transition") &
  transition_pid=$!
  sleep 0.1
  [[ ! -e $work/$transition ]] || fail "$transition overlapped an admitted retention or watch pass"
  flock -u "$edge_guard"
  exec {edge_guard}>&-
  wait "$transition_pid"
  [[ -e $work/$transition ]] || fail "$transition did not resume after the pass joined"
done

touch "$ABOUTME_TEST_STATE/transition-persists" "$work/run/crowdsec-retention-ok"
if aboutme_edge_transition true; then fail 'accepted a surviving systemd transition'; fi
[[ -f $work/run/crowdsec-retention-failed && ! -e $work/run/crowdsec-retention-ok ]] ||
  fail 'uncertain edge mutation retained startup admission'
rm "$ABOUTME_TEST_STATE/transition-persists" "$work/run/crowdsec-retention-failed"

for fault in timeout-malformed fail-inspection-after-stop raw-exit-two; do
  touch "$work/run/crowdsec-retention-ok"
  touch "$ABOUTME_TEST_STATE/$fault"
  if [[ $fault == raw-exit-two ]]; then
    if aboutme_edge_transition bash -c 'exit 2'; then fail 'accepted raw command exit 2'; fi
  elif aboutme_edge_transition aboutme_stop_edge aboutme-caddy; then
    fail "$fault accepted an unproved completed mutation"
  fi
  [[ -f $work/run/crowdsec-retention-failed && ! -e $work/run/crowdsec-retention-ok ]] ||
    fail "$fault bypassed admission revocation"
  rm -f "$ABOUTME_TEST_STATE/$fault" "$ABOUTME_TEST_STATE/malformed-metadata" \
    "$ABOUTME_TEST_STATE/inspection-failure" "$work/run/crowdsec-retention-failed"
done

# Pause deployment at edge admission, then complete watchdog shutdown. Resuming
# the deployment must neither run startup retention nor remove its failure.
real_flock=$(command -v flock)
flock() {
  if [[ -e $ABOUTME_TEST_STATE/pause-admission && $* == '-x -w 65 '* ]]; then
    touch "$ABOUTME_TEST_STATE/admission-entered"
    while [[ ! -e $ABOUTME_TEST_STATE/admission-release ]]; do sleep 0.01; done
  fi
  "$real_flock" "$@"
}
touch "$ABOUTME_TEST_STATE/pause-admission"
(
  if aboutme_edge_start aboutme-maintenance; then echo accepted; else echo refused; fi
) >"$work/admission-result" &
admission_pid=$!
for ((i=0; i<100; i++)); do
  [[ ! -e $ABOUTME_TEST_STATE/admission-entered ]] || break
  sleep 0.01
done
[[ -e $ABOUTME_TEST_STATE/admission-entered ]] || fail 'edge admission fixture did not pause'
printf 'retention-failed\n' >"$work/run/crowdsec-retention-failed"
rm -f "$work/run/crowdsec-retention-ok" "$ABOUTME_TEST_STATE/active/aboutme-maintenance"
touch "$ABOUTME_TEST_STATE/admission-release"
wait "$admission_pid"
[[ $(cat "$work/admission-result") == refused &&
  -f $work/run/crowdsec-retention-failed && ! -e $work/run/crowdsec-retention-ok &&
  ! -e $ABOUTME_TEST_STATE/unwanted-startup &&
  ! -e $ABOUTME_TEST_STATE/active/aboutme-maintenance ]] || fail 'paused deployment reopened after watchdog shutdown'
unset -f flock
rm -f "$ABOUTME_TEST_STATE/pause-admission" "$ABOUTME_TEST_STATE/admission-entered" \
  "$ABOUTME_TEST_STATE/admission-release" "$work/run/crowdsec-retention-failed"
touch "$ABOUTME_TEST_STATE/transition-persists" "$work/run/crowdsec-retention-ok"
if aboutme_edge_transition aboutme_stop_edge aboutme-caddy; then fail 'accepted a timed-out systemctl client'; fi
[[ -f $work/run/crowdsec-retention-failed && ! -e $work/run/crowdsec-retention-ok ]] ||
  fail 'timed-out client left retention admission open'
rm "$ABOUTME_TEST_STATE/transition-persists" "$work/run/crowdsec-retention-failed"

for helper in step_maintenance_up step_app_down render_all step_app_up step_maintenance_down; do
  sed -n "/^$helper() {/,/^}/p" "$root/deploy/vn/scripts/deploy-host.sh" |
    grep -qE 'aboutme_edge_(transition|start)' || fail "$helper lacks edge serialization"
done

for unit in aboutme-caddy aboutme-server aboutme-web \
  aboutme-job-idempotency-expiry-sweep.timer \
  aboutme-job-media-deletion-sweep.timer \
  aboutme-job-privacy-retention-sweep.timer \
  aboutme-job-media-orphan-sweep.timer \
  aboutme-job@media-deletion-sweep.service; do
  : >"$ABOUTME_TEST_STATE/active/$unit"
done

aboutme_fail_closed 1
aboutme_assert_fail_closed

# Retention failure owns both edge modes, including static maintenance.
touch "$work/run/crowdsec-retention-failed"
rm -f "$ABOUTME_TEST_STATE/active/aboutme-maintenance"
if aboutme_fail_closed 1; then fail 'retention failure reopened maintenance'; fi
[[ ! -e $ABOUTME_TEST_STATE/active/aboutme-maintenance ]] || fail 'retention failure started maintenance'
rm "$work/run/crowdsec-retention-failed"
aboutme_fail_closed 1

for unit in aboutme-caddy aboutme-server aboutme-web \
  aboutme-job-idempotency-expiry-sweep.timer \
  aboutme-job-media-deletion-sweep.timer \
  aboutme-job-privacy-retention-sweep.timer \
  aboutme-job-media-orphan-sweep.timer \
  aboutme-job@media-deletion-sweep.service; do
  [[ ! -e $ABOUTME_TEST_STATE/active/$unit ]] || {
    echo "$unit still active after fail closed" >&2
    exit 1
  }
done
[[ -e $ABOUTME_TEST_STATE/active/aboutme-maintenance ]] || {
  echo "maintenance is not active after fail closed" >&2
  exit 1
}

# A timer restarted by another process must make the cutover guard fail.
: >"$ABOUTME_TEST_STATE/active/aboutme-job-media-orphan-sweep.timer"
if aboutme_assert_cutover_quiescent; then
  echo "cutover guard accepted a restarted job timer" >&2
  exit 1
fi
rm -f "$ABOUTME_TEST_STATE/active/aboutme-job-media-orphan-sweep.timer"
aboutme_assert_cutover_quiescent

# Exercise the actual stopped-app and Boolean database helpers with closed
# command fixtures, then invoke the isolated candidate-release matrix.
for helper in cimd_url_rows_exist cimd_serving_app_stopped; do
  sed -n "/^$helper() {/,/^}/p" "$root/deploy/vn/scripts/deploy-host.sh" >>"$work/cimd-helpers.sh"
done
for helper in cimd_env_mcp_flag cimd_effective_mcp_flag; do
  sed -n "/^$helper() {/,/^}/p" "$root/deploy/vn/scripts/deploy-host.sh" >>"$work/cimd-helpers.sh"
done
templates=$work/quadlet
mkdir -p "$templates"
sed "s|/etc/aboutme/|$work/cimd-|g" "$root/deploy/vn/host/quadlet/aboutme-server.container.in" >"$templates/aboutme-server.container.in"
# shellcheck source=/dev/null
source "$work/cimd-helpers.sh"
systemctl() {
  [[ $* == 'show -p ActiveState --value aboutme-server' ]] || return 1
  [[ ${CIMD_STATE:-inactive} != failure ]] || return 1
  printf '%s\n' "${CIMD_STATE:-inactive}"
}
runuser() {
  [[ $* == *'-u postgres -- psql -X -qAt -v ON_ERROR_STOP=1 -d aboutme'* ]] || return 1
  [[ $* == *'SELECT EXISTS (SELECT 1 FROM public.oauth_clients'* ]] || return 1
  [[ $* == *"to_jsonb(client)->>'metadata_url' IS NOT NULL)"* ]] || return 1
  [[ ${CIMD_QUERY:-f} != failure ]] || return 1
  printf '%s\n' "${CIMD_QUERY:-f}"
}
for CIMD_STATE in inactive failed; do
  export CIMD_STATE
  cimd_serving_app_stopped || { echo 'stopped app rejected' >&2; exit 1; }
done
for CIMD_STATE in active activating deactivating unknown failure; do
  export CIMD_STATE
  if cimd_serving_app_stopped; then echo 'unproved stopped app accepted' >&2; exit 1; fi
done
unset CIMD_STATE
for CIMD_QUERY in t f; do
  export CIMD_QUERY
  [[ $(cimd_url_rows_exist) == "$CIMD_QUERY" ]] || fail "URL-client query did not return $CIMD_QUERY"
done
if CIMD_QUERY=failure cimd_url_rows_exist; then echo 'database failure accepted' >&2; exit 1; fi
unset CIMD_QUERY
printf 'MCP_ENABLED=true\n' >"$work/cimd-server.env"
: >"$work/cimd-flags.env"
printf 'SMTP_HOST=smtp.example\n' >"$work/cimd-smtp.env"
expect_mcp_flag true 'base environment'
printf 'MCP_ENABLED=false\n' >"$work/cimd-flags.env"
expect_mcp_flag false 'flags environment override'
printf 'MCP_ENABLED=true\n' >>"$work/cimd-smtp.env"
expect_mcp_flag true 'SMTP environment override'
# The guard must refuse the pre-CIMD image under the actual effective flag.
sed -n '/^cimd_rollback_check() {/,/^}/p' "$root/deploy/vn/scripts/deploy-host.sh" >"$work/cimd-guard.sh"
(
  die() { exit 1; }
  # shellcheck source=/dev/null
  source "$work/cimd-guard.sh"
  CIMD_QUERY=t cimd_rollback_check 7999
) && { echo 'SMTP override allowed unsafe rollback' >&2; exit 1; }
printf 'MCP_ENABLED=false\n' >"$work/cimd-smtp.env"
expect_mcp_flag false 'SMTP environment disablement'
# Only Container entries set the container's environment.
for section in Service Install; do
  sed -i "/^\[$section\]$/a Environment=MCP_ENABLED=true" "$templates/aboutme-server.container.in"
  expect_mcp_flag false "inline $section environment"
  sed -i '/^Environment=MCP_ENABLED=/d' "$templates/aboutme-server.container.in"
done
sed -i '/^\[Container\]$/a Environment=MCP_ENABLED=true' "$templates/aboutme-server.container.in"
# Inline container variables override every environment file.
expect_mcp_flag true 'inline Container environment override'
if (
  die() { printf '%s\n' "$*" >&2; exit 1; }
  # shellcheck source=/dev/null
  source "$work/cimd-guard.sh"
  CIMD_QUERY=t cimd_rollback_check 7999
) >"$work/inline-rollback.out" 2>&1; then
  fail 'inline Container override allowed unsafe rollback'
fi
grep -qxF 'pre-CIMD rollback requires no URL clients or MCP_ENABLED=false' "$work/inline-rollback.out" ||
  fail 'inline Container rollback failed without the URL-client guard refusal'
sed -i '/^Environment=MCP_ENABLED=/d' "$templates/aboutme-server.container.in"
for configuration in 'MCP_ENABLED=invalid' $'MCP_ENABLED=true\nMCP_ENABLED=false' 'MCP_ENABLED="true"'; do
  printf '%s\n' "$configuration" >"$work/cimd-smtp.env"
  if cimd_effective_mcp_flag; then echo 'invalid SMTP override accepted' >&2; exit 1; fi
done
: >"$work/cimd-smtp.env"
: >"$work/cimd-flags.env"
for configuration in '' 'MCP_ENABLED=invalid' $'MCP_ENABLED=true\nMCP_ENABLED=false' 'MCP_ENABLED="true"'; do
  printf '%s\n' "$configuration" >"$work/cimd-server.env"
  if cimd_effective_mcp_flag; then echo 'unproved MCP configuration accepted' >&2; exit 1; fi
done
rm "$work/cimd-server.env"
if cimd_effective_mcp_flag; then echo 'missing MCP configuration accepted' >&2; exit 1; fi
bash "$root/deploy/vn/scripts/cimd-rollback-test.sh"

# Account-writer and send quiescence needs positive stopped-state evidence.
# A failed inspection cannot count as a joined server before revocation.
unset -f systemctl
: >"$ABOUTME_TEST_STATE/inspection-failure"
if aboutme_assert_cutover_quiescent >"$work/notice-out" 2>&1; then
  fail 'inspection failure accepted as send quiescence'
fi
rm "$ABOUTME_TEST_STATE/inspection-failure"
for NOTICE_UNIT_STATE in activating deactivating unknown; do
  export NOTICE_UNIT_STATE
  if aboutme_assert_cutover_quiescent >"$work/notice-out" 2>&1; then
    fail 'ambiguous account writer accepted as send quiescence'
  fi
done
unset NOTICE_UNIT_STATE
for NOTICE_UNIT_JOB in 0 1 invalid; do
  export NOTICE_UNIT_JOB
  if aboutme_assert_cutover_quiescent >"$work/notice-out" 2>&1; then fail 'nonempty server job accepted as send quiescence'; fi
done
unset NOTICE_UNIT_JOB
aboutme_assert_cutover_quiescent
echo "deployment safety tests passed"
