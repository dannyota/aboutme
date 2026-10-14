#!/usr/bin/env bash
# Shared unit-state guards for deploy and cutover operations.

aboutme_jobs=(
  idempotency-expiry-sweep
  media-deletion-sweep
  privacy-retention-sweep
  media-orphan-sweep
)

aboutme_active() { systemctl is-active --quiet "$1"; }

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
  aboutme_assert_jobs_stopped
}

aboutme_fail_closed() { # wait seconds
  local seconds=$1 rc=0
  aboutme_stop_jobs "$seconds" || rc=1
  systemctl stop aboutme-caddy aboutme-server aboutme-web || rc=1
  aboutme_wait_stopped aboutme-caddy 60 || rc=1
  aboutme_wait_stopped aboutme-server 60 || rc=1
  aboutme_wait_stopped aboutme-web 60 || rc=1
  systemctl start aboutme-maintenance || rc=1
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
  jq -e '.closureQuiescent==true and .fenceHeld==false and .reserved==false and .pendingVerb=="none" and .intent=="none" and (.phase=="planned" or .phase=="closed" or (.phase=="serving" and (.action.writeOutcome=="committed" or .action.writeOutcome=="absent_after_unknown"))) and (.action.writeOutcome!="unknown")' <<<"$state" >/dev/null || return 1
  unit_state=$(systemctl show --property=ActiveState --property=Job "aboutme-operation@$id.service") || return 1
  [[ $(wc -l <<<"$unit_state") == 2 ]] && grep -qxE 'ActiveState=(inactive|failed)' <<<"$unit_state" && grep -qxF 'Job=0' <<<"$unit_state" || return 1
  containers=$(podman ps -a --format '{{.Names}}') || return 1
  [[ $containers != *"aboutme-operation-$id-"* ]] || return 1
  count=$(timeout 30 runuser -u postgres -- psql -XAt -v ON_ERROR_STOP=1 --dbname aboutme --set=prefix="hostop:$id:" <<'SQL'
SELECT count(*) FROM pg_stat_activity WHERE left(application_name, length(:'prefix')) = :'prefix';
SQL
  ) || return 1
  [[ $count == 0 ]]
}
