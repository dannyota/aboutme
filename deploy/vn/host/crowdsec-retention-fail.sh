#!/usr/bin/env bash
# Stops every CrowdSec consumer after a retention failure. All output and the
# marker are fixed so no attack address reaches the journal or alert mail.
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
"$systemctl_cmd" stop "${protected_units[@]}" || true
still_active=()
for unit in "${protected_units[@]}"; do
  if "$systemctl_cmd" is-active --quiet "$unit"; then
    still_active+=("$unit")
  fi
done
if ((${#still_active[@]})); then
  "$systemctl_cmd" kill --kill-whom=all --signal=SIGKILL "${still_active[@]}" || true
  "$systemctl_cmd" stop "${still_active[@]}" || true
fi

failed=0
for unit in "${protected_units[@]}"; do
  "$systemctl_cmd" is-active --quiet "$unit" && failed=1
done
if ((failed)); then
  "$alert" crowdsec-retention "CrowdSec retention failed" || true
  exit 1
fi
if ((EUID == 0)); then
  /usr/bin/setpriv --reuid 10001 --regid 10001 --clear-groups \
    /usr/bin/rm -rf --one-file-system -- "$feed_dir" || failed=1
else
  rm -rf --one-file-system -- "$feed_dir" || failed=1
fi
"$alert" crowdsec-retention "CrowdSec retention failed" || true
exit "$failed"
