#!/usr/bin/env bash
# Shared bounded consumer shutdown and retention failure action.

retention_quiescent() {
  local unit state group container status
  local -a properties
  for unit in "${protected_units[@]}"; do
    properties=(--property=ActiveState --property=Job)
    if [[ $unit == *.service ]]; then
      properties+=(--property=MainPID --property=ControlPID --property=ControlGroup)
    fi
    state=$(timeout --foreground --kill-after=1s 1 "$systemctl_cmd" show --all "${properties[@]}" "$unit" 2>/dev/null) || return 1
    grep -qxE 'ActiveState=(inactive|failed)' <<<"$state" &&
      grep -qxF 'Job=' <<<"$state" || return 1
    if [[ $unit == *.timer ]]; then
      [[ $(wc -l <<<"$state") == 2 ]] || return 1
      continue
    fi
    [[ $(wc -l <<<"$state") == 5 ]] &&
      grep -qxF MainPID=0 <<<"$state" && grep -qxF ControlPID=0 <<<"$state" || return 1
    group=$(sed -n 's/^ControlGroup=//p' <<<"$state")
    [[ -z $group ]] && continue
    [[ $group == /system.slice/*.service && $group != *..* ]] || return 1
    if [[ -e /sys/fs/cgroup$group/cgroup.events ]]; then
      grep -qxF 'populated 0' "/sys/fs/cgroup$group/cgroup.events" || return 1
    fi
  done
  if ((EUID == 0)); then
    for container in aboutme-caddy aboutme-maintenance; do
      status=0
      timeout --foreground --kill-after=1s 1 podman container exists "$container" >/dev/null 2>&1 || status=$?
      case $status in
        0) [[ $(timeout --foreground --kill-after=1s 1 podman inspect --format '{{.State.Running}} {{.State.Pid}}' "$container" 2>/dev/null) == 'false 0' ]] || return 1 ;;
        1) ;;
        *) return 1 ;;
      esac
    done
  fi
}

retention_stop_main() {
  timeout --foreground --kill-after=1s 35 "$systemctl_cmd" stop "${protected_units[@]}" || true
  if ! retention_quiescent; then
    timeout --foreground --kill-after=1s 3 "$systemctl_cmd" kill --kill-whom=all --signal=SIGKILL "${protected_units[@]}" || true
    timeout --foreground --kill-after=1s 3 "$systemctl_cmd" stop "${protected_units[@]}" || true
  fi

  retention_quiescent
}

retention_stop() {
  # The whole stop proof, including escalation and metadata, shares one budget.
  # Keep two seconds for forced termination inside the 60-second stop bound.
  {
    declare -p systemctl_cmd protected_units
    declare -f retention_quiescent retention_stop_main
    printf 'retention_stop_main\n'
  } | timeout --signal=TERM --kill-after=2s 58 bash
}

retention_fail_main() {
  set -euo pipefail
  exec >/dev/null 2>/dev/null
  if ((EUID == 0)) && {
    [[ -n ${ABOUTME_RETENTION_SYSTEMCTL:-} ]] ||
      [[ -n ${ABOUTME_RETENTION_ALERT:-} ]] ||
      [[ -n ${ABOUTME_RETENTION_FAILURE_MARKER:-} ]] ||
      [[ -n ${ABOUTME_RETENTION_FEED_DIR:-} ]] ||
      [[ -n ${ABOUTME_RETENTION_SUCCESS_MARKER:-} ]];
  }; then
    exit 2
  fi

  systemctl_cmd=${ABOUTME_RETENTION_SYSTEMCTL:-/usr/bin/systemctl}
  alert=${ABOUTME_RETENTION_ALERT:-/usr/local/lib/aboutme/alert.sh}
  marker=${ABOUTME_RETENTION_FAILURE_MARKER:-/run/aboutme/crowdsec-retention-failed}
  feed_dir=${ABOUTME_RETENTION_FEED_DIR:-/run/aboutme/caddy-log/crowdsec}
  success=${ABOUTME_RETENTION_SUCCESS_MARKER:-/run/aboutme/crowdsec-retention-ok}
  consumers=(
    aboutme-caddy.service
    aboutme-maintenance.service
    crowdsec-firewall-bouncer.service
    crowdsec.service
  )
  protected_units=("${consumers[@]}" aboutme-crowdsec-retention-startup.service)

  rm -f -- "$success"
  printf 'retention-failed\n' >"$marker"
  retention_stop || {
    timeout --kill-after=1s 5 "$alert" crowdsec-retention "CrowdSec retention failed" || true
    exit 1
  }

  failed=0
  if ((EUID == 0)); then
    /usr/bin/setpriv --reuid 10001 --regid 10001 --clear-groups \
      /usr/bin/rm -rf --one-file-system -- "$feed_dir" || failed=1
    if [[ -d /run/aboutme/crowdsec && ! -L /run/aboutme/crowdsec &&
      $(stat -c '%u:%g:%a' /run/aboutme/crowdsec) == 0:0:700 &&
      $(findmnt -n -o FSTYPE -T /run/aboutme/crowdsec) == tmpfs ]]; then
      /usr/bin/rm -rf --one-file-system -- /run/aboutme/crowdsec || failed=1
    elif [[ -e /run/aboutme/crowdsec || -L /run/aboutme/crowdsec ]]; then
      failed=1
    fi
  else
    rm -rf --one-file-system -- "$feed_dir" || failed=1
  fi
  timeout --kill-after=1s 5 "$alert" crowdsec-retention "CrowdSec retention failed" || true
  exit "$failed"
}
if [[ ${BASH_SOURCE[0]} == "$0" ]]; then retention_fail_main "$@"; fi
