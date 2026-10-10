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
