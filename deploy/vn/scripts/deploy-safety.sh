#!/usr/bin/env bash
# Shared unit-state guards for deploy and cutover operations.

aboutme_jobs=(
  idempotency-expiry-sweep
  media-deletion-sweep
  privacy-retention-sweep
  media-orphan-sweep
)

aboutme_active() { systemctl is-active --quiet "$1"; }

aboutme_edges_stable() {
  local unit result
  for unit in aboutme-caddy.service aboutme-maintenance.service; do
    result=$(timeout --kill-after=1s 1 systemctl show --all --property=ActiveState --property=Job \
      --property=MainPID --property=ControlPID "$unit") || return 2
    [[ $(wc -l <<<"$result") == 4 ]] || return 2
    grep -qxF 'Job=' <<<"$result" && grep -qxF ControlPID=0 <<<"$result" || return 1
    if grep -qxF ActiveState=active <<<"$result"; then
      grep -qxE 'MainPID=[1-9][0-9]*' <<<"$result" || return 1
    else
      grep -qxE 'ActiveState=(inactive|failed)' <<<"$result" &&
        grep -qxF MainPID=0 <<<"$result" || return 1
    fi
  done
}

# Take retention before the fence, as retention and the watcher do. Keep app
# readiness waits outside this lock so overdue cleanup still fails closed.
aboutme_edge_transition() (
  umask 077
  [[ -d /run/aboutme && ! -L /run/aboutme &&
    $(stat -c '%u:%a' /run/aboutme) == "$EUID:755" &&
    ! -L /run/aboutme/crowdsec-retention.lock ]] || return 1
  exec {edge_lock}>/run/aboutme/crowdsec-retention.lock
  [[ $(stat -c '%u:%a:%h' /run/aboutme/crowdsec-retention.lock) == "$EUID:600:1" ]] || return 1
  # Join the 60-second pass and its two-second forced termination before changing
  # either edge mode. This does not extend retention's whole-pass deadline.
  flock -x -w 65 "$edge_lock" || return 1
  local result=0
  aboutme_edges_stable || result=$?
  # Refuse an unavailable inspection before issuing any manager mutation.
  ((result != 2)) || return 1
  if ((result == 0)); then "$@" || result=$?; fi
  if ((result == 0)); then aboutme_edges_stable || result=$?; fi
  if ((result != 0)); then
    # A timed-out systemctl client can leave its manager job running. Revoke
    # admission before releasing the lock, then request the full stop action.
    rm -f /run/aboutme/crowdsec-retention-ok
    printf 'retention-failed\n' >/run/aboutme/crowdsec-retention-failed
    timeout --kill-after=1s 3 systemctl start --no-block aboutme-crowdsec-retention-failure.service || true
    return 1
  fi
)

aboutme_start_edge() {
  local rc=0
  [[ ! -e /run/aboutme/crowdsec-retention-failed &&
    ! -L /run/aboutme/crowdsec-retention-failed ]] || return 1
  timeout --kill-after=1s 10 systemctl start "$@" || rc=$?
  if [[ -e /run/aboutme/crowdsec-retention-failed ||
    -L /run/aboutme/crowdsec-retention-failed ]]; then
    timeout --kill-after=1s 10 systemctl stop aboutme-caddy aboutme-maintenance || true
    return 1
  fi
  return "$rc"
}

aboutme_edge_start() {
  # Admission is checked inside the lock. Deployment cannot renew a failed gate;
  # provisioning establishes startup before modern edge units need it.
  aboutme_edge_transition aboutme_start_edge "$@"
}

aboutme_stop_edge() {
  # Missing units on first deploy are harmless only with positive stable-state
  # proof from aboutme_edge_transition after the command returns.
  local unit result rc=0
  timeout --kill-after=1s 10 systemctl stop "$@" || rc=$?
  for unit in "$@"; do
    result=$(timeout --kill-after=1s 1 systemctl show --all --property=ActiveState --property=Job "$unit") || {
      if ((rc == 124 || rc == 137)); then return "$rc"; fi
      return 2
    }
    [[ $(wc -l <<<"$result") == 2 ]] || return 2
    grep -qxE 'ActiveState=(inactive|failed)' <<<"$result" &&
      grep -qxF 'Job=' <<<"$result" || return 1
  done
}

aboutme_wait_stopped() { # unit seconds
  local unit=$1 seconds=$2 i
  for ((i = 0; i < seconds; i++)); do
    aboutme_active "$unit" || return 0
    sleep 1
  done
  return 1
}

aboutme_stop_jobs() { # wait seconds
  local seconds=$1 job rc=0 timers=() services=()
  for job in "${aboutme_jobs[@]}"; do
    timers+=("aboutme-job-$job.timer")
    services+=("aboutme-job@$job.service")
  done
  systemctl stop "${timers[@]}" || rc=1
  systemctl stop "${services[@]}" || rc=1
  for job in "${services[@]}"; do
    aboutme_wait_stopped "$job" "$seconds" || rc=1
  done
  aboutme_assert_jobs_stopped || rc=1
  return "$rc"
}

aboutme_assert_jobs_stopped() {
  local job bad=0
  for job in "${aboutme_jobs[@]}"; do
    if aboutme_active "aboutme-job-$job.timer"; then
      printf 'deploy-safety: job timer %s is active\n' "$job" >&2
      bad=1
    fi
    if aboutme_active "aboutme-job@$job.service"; then
      printf 'deploy-safety: job %s is active\n' "$job" >&2
      bad=1
    fi
  done
  ((bad == 0))
}

aboutme_assert_cutover_quiescent() {
  aboutme_active aboutme-maintenance || {
    echo "deploy-safety: maintenance is not active" >&2
    return 1
  }
  if aboutme_active aboutme-server; then
    echo "deploy-safety: server is active" >&2
    return 1
  fi
  # Preserve the existing isolated fence fixture; installed quiescence needs
  # positive stopped state and an empty queued-start job, including mail sends.
  if [[ ${FENCE_DIR:-/var/lib/aboutme} == /var/lib/aboutme ]]; then
    local result
    result=$(systemctl show --all --property=ActiveState --property=Job aboutme-server) || return 1
    [[ $(wc -l <<<"$result") == 2 ]] && grep -qxE 'ActiveState=(inactive|failed)' <<<"$result" && grep -qxF 'Job=' <<<"$result" || return 1
  fi
  aboutme_assert_jobs_stopped
}

aboutme_fail_closed() { # wait seconds
  local seconds=$1 rc=0
  aboutme_stop_jobs "$seconds" || rc=1
  aboutme_edge_transition aboutme_stop_edge aboutme-caddy aboutme-server aboutme-web || rc=1
  aboutme_wait_stopped aboutme-caddy 60 || rc=1
  aboutme_wait_stopped aboutme-server 60 || rc=1
  aboutme_wait_stopped aboutme-web 60 || rc=1
  aboutme_edge_start aboutme-maintenance || rc=1
  aboutme_assert_fail_closed || rc=1
  return "$rc"
}

aboutme_assert_fail_closed() {
  local unit bad=0
  aboutme_assert_jobs_stopped || bad=1
  for unit in aboutme-caddy aboutme-server aboutme-web; do
    if aboutme_active "$unit"; then
      printf 'deploy-safety: %s is active\n' "$unit" >&2
      bad=1
    fi
  done
  aboutme_active aboutme-maintenance || {
    echo "deploy-safety: maintenance is not active" >&2
    bad=1
  }
  ((bad == 0))
}

aboutme_assert_operation_release() { # request id
  local id=$1 state unit_state containers count
  mountpoint -q /srv/data || return 1
  [[ $id =~ ^[A-Za-z0-9_-]{42}[AEIMQUYcgkosw048]$ ]] || return 1
  [[ -r /usr/local/lib/aboutme/operation-state.sh && ! -L /usr/local/lib/aboutme/operation-state.sh ]] || return 1
  state=$(bash -c 'source /usr/local/lib/aboutme/operation-state.sh; operation_state load "$1"' bash "$id") || return 1
  jq -e '.closureQuiescent==true and .fenceHeld==false and .reserved==(.phase=="planned") and .pendingVerb=="none" and .intent=="none" and (.phase=="planned" or .phase=="closed" or (.phase=="serving" and (.action.writeOutcome=="committed" or .action.writeOutcome=="absent_after_unknown"))) and (.action.writeOutcome!="unknown")' <<<"$state" >/dev/null || return 1
  unit_state=$(systemctl show --all --property=ActiveState --property=Job "aboutme-operation@$id.service") || return 1
  [[ $(wc -l <<<"$unit_state") == 2 ]] && grep -qxE 'ActiveState=(inactive|failed)' <<<"$unit_state" && grep -qxF 'Job=' <<<"$unit_state" || return 1
  containers=$(podman ps -a --format '{{.Names}}') || return 1
  [[ $containers != *"aboutme-operation-$id-"* ]] || return 1
  count=$(timeout 30 runuser -u postgres -- psql -XAt -v ON_ERROR_STOP=1 --dbname aboutme --set=prefix="hostop:$id:" <<'SQL'
SELECT count(*) FROM pg_stat_activity WHERE left(application_name, length(:'prefix')) = :'prefix';
SQL
  ) || return 1
  [[ $count == 0 ]]
}
