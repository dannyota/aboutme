#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
if [[ ${1:-} == systemd ]]; then
  [[ $EUID == 0 && ${GITHUB_ACTIONS:-} == true ]] || exit 2
  lib=/usr/local/lib/aboutme
  install -d -m 0755 "$lib" /run/aboutme /srv/data /var/lib/aboutme
  guest=$(mktemp -d /run/cutover-systemd.XXXXXX)
  truncate -s 64M "$guest/disk"
  mkfs.ext4 -q -L aboutme-data "$guest/disk"
  device=$(losetup --find --show "$guest/disk")
  mount "$device" /srv/data
  install -d -m 0755 /srv/data/aboutme
  install -d -m 0700 /srv/data/host-operations
  mount --bind /srv/data/aboutme /var/lib/aboutme
  cleanup_guest() {
    local result=$?
    trap - EXIT
    systemctl stop aboutme-watch.timer aboutme-watch.service aboutme-crowdsec-retention.timer \
      aboutme-crowdsec-vacuum.timer aboutme-crowdsec-retention.service aboutme-crowdsec-vacuum.service \
      aboutme-crowdsec-retention-startup.service aboutme-caddy.service aboutme-maintenance.service \
      aboutme-server.service aboutme-web.service crowdsec.service crowdsec-firewall-bouncer.service >/dev/null 2>&1 || true
    umount /var/lib/aboutme /srv/data || result=1
    losetup -d "$device" || result=1
    rm -rf -- "$guest"
    exit "$result"
  }
  trap cleanup_guest EXIT
  systemd_stage=helpers
  trap 'printf "FAIL systemd-cutover-boundary %s\n" "$systemd_stage" >&2' ERR
  for helper in crowdsec-db crowdsec-provision crowdsec-cutover-state crowdsec-privacy-notice-host \
    crowdsec-retention crowdsec-retention-lease-check crowdsec-retention-fail crowdsec-vacuum workload-lock; do
    install -m 0755 "$repo_root/deploy/vn/host/$helper.sh" "$lib/$helper.sh"
  done
  install -m 0755 "$repo_root/deploy/vn/scripts/fence.sh" "$lib/fence.sh"
  install -m 0755 "$repo_root/deploy/vn/scripts/deploy-safety.sh" "$lib/deploy-safety.sh"
  install -m 0644 "$repo_root/deploy/vn/host/crowdsec-schema-v1.8.1.tsv" "$lib/"
  printf '#!/bin/sh\nexit 0\n' >"$lib/alert.sh"
  chmod 0755 "$lib/alert.sh"
  install -d -m 0700 /run/aboutme/crowdsec
  install -d -o 10001 -g 10001 -m 0750 /run/aboutme/caddy-log
  install -m 0644 "$repo_root/deploy/vn/edge/crowdsec/config.yaml.local" /etc/crowdsec/config.yaml.postgresql
  systemd_stage=notice-initialize
  "$lib/fence.sh" init >/dev/null 2>&1
  "$lib/crowdsec-privacy-notice-host.sh" initialize >/dev/null 2>&1
  printf '{"release_tag":"v0.8.9","release_number":8009,"maintenance_tag":"v0.8.9"}\n' >/var/lib/aboutme/release.json
  chmod 0644 /var/lib/aboutme/release.json
  # Use the installed authority writer for a synthetic zero-recipient receipt.
  # The notice suite proves application reconciliation and delivery separately.
  # shellcheck source=/dev/null
  source "$lib/crowdsec-privacy-notice-host.sh"
  # shellcheck disable=SC2034 # The sourced authority writer consumes this mount identity.
  notice_identity=$(notice_mount_identity)
  notice_authority_check
  notice_safe_file /var/lib/aboutme/privacy-notice/restore-reconciliation
  [[ ! -s /var/lib/aboutme/privacy-notice/restore-reconciliation ]]
  jq -cn --arg epoch "$(cat /var/lib/aboutme/privacy-notice/epoch)" \
    '{schema_version:1,campaign_id:"crowdsec-postgres-paths-v1",content_sha256:"bb128b39501f09db59915f3f75759572a49349ec78fe17ad48b6ba40d500ffdc",cutoff_at:"2026-01-01T00:00:00.000000Z",expected_count:0,epoch:$epoch,accepted_count:0,deleted_count:0,activated_at:"2026-01-01T00:00:00.000000Z"}' |
    notice_wire activation 0 "$(date -u +%Y-%m-%dT%H:%M:%S.%6NZ)" | notice_replace activation.json
  if "$lib/fence.sh" check-current crowdsec.service >/dev/null 2>&1; then
    echo 'FAIL notice-reconciliation-start-refusal' >&2; exit 1
  fi
  notice_revoke restore-reconciliation
  "$lib/fence.sh" check-current crowdsec.service
  systemd_stage=units
  # Real manager invocations/cgroups and real PostgreSQL/retention; consumer
  # payloads here are bounded sleeps, not production images or cache proof.
  for unit in aboutme-server aboutme-web aboutme-caddy aboutme-maintenance crowdsec crowdsec-firewall-bouncer; do
    systemctl unmask --runtime "$unit.service" >/dev/null 2>&1
    cat >"/etc/systemd/system/$unit.service" <<UNIT
[Service]
Type=simple
ExecStartPre=$lib/fence.sh check-current $unit.service
ExecStart=/bin/sleep 600
UNIT
    install -d -m 0755 "/etc/systemd/system/$unit.service.d"
    cat >"/etc/systemd/system/$unit.service.d/99-cutover-fixture.conf" <<UNIT
[Service]
ExecStartPre=
ExecStartPre=$lib/fence.sh check-current $unit.service
UNIT
  done
  install -m 0644 "$repo_root/deploy/vn/host/etc/systemd/system/aboutme-crowdsec-retention-failure.service" /etc/systemd/system/aboutme-crowdsec-retention-failure.service
  for unit in aboutme-crowdsec-retention-startup aboutme-crowdsec-retention aboutme-crowdsec-vacuum; do
    install -m 0644 "$repo_root/deploy/vn/host/etc/systemd/system/$unit.service" "/etc/systemd/system/$unit.service"
  done
  for unit in aboutme-crowdsec-retention aboutme-crowdsec-vacuum; do
    install -m 0644 "$repo_root/deploy/vn/host/etc/systemd/system/$unit.timer" "/etc/systemd/system/$unit.timer"
  done
  install -m 0755 "$repo_root/deploy/vn/host/bin/watch.sh" "$lib/watch.sh"
  install -m 0644 "$repo_root/deploy/vn/host/etc/systemd/system/aboutme-watch.service" /etc/systemd/system/aboutme-watch.service
  install -m 0644 "$repo_root/deploy/vn/host/etc/systemd/system/aboutme-watch.timer" /etc/systemd/system/aboutme-watch.timer
  systemctl daemon-reload
  systemctl stop aboutme-watch.timer aboutme-watch.service aboutme-crowdsec-retention.timer aboutme-crowdsec-vacuum.timer
  systemctl disable aboutme-crowdsec-retention.timer aboutme-crowdsec-vacuum.timer >/dev/null 2>&1
  systemd_stage=reset
  owner=$("$lib/fence.sh" lock cutover v0.8.9)
  "$lib/fence.sh" mutation-start "$owner" v0.8.9 reset-db
  printf '{"operation_id":"%s","release_tag":"v0.8.9"}\n' "$owner" | "$lib/crowdsec-provision.sh" restore-reset >/dev/null 2>&1
  if "$lib/fence.sh" start-unit AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA v0.8.9 aboutme-server.service >/dev/null 2>&1; then exit 1; fi
  for unit in aboutme-migrate.service aboutme-job@privacy-retention-sweep.service aboutme-crowdsec-retention.timer; do
    if "$lib/fence.sh" start-unit "$owner" v0.8.9 "$unit" >/dev/null 2>&1; then exit 1; fi
  done
  systemd_stage=safety
  "$lib/fence.sh" start-unit "$owner" v0.8.9 aboutme-crowdsec-vacuum.service
  "$lib/fence.sh" start-unit "$owner" v0.8.9 aboutme-crowdsec-retention-startup.service
  if "$lib/crowdsec-retention-lease-check.sh" >/dev/null 2>&1; then exit 1; fi
  "$lib/fence.sh" start-unit "$owner" v0.8.9 aboutme-crowdsec-retention.service
  "$lib/fence.sh" cutover-mode "$owner" v0.8.9
  cp -p /run/aboutme/crowdsec-cutover/mode.json "$guest/mode.json"
  for fault in marker epoch outcomes; do
    jq --arg fault "$fault" 'if $fault=="outcomes" then .outcomes={} else .[$fault]="invalid" end' \
      "$guest/mode.json" >/run/aboutme/crowdsec-cutover/mode.json
    if "$lib/crowdsec-retention-lease-check.sh" >/dev/null 2>&1; then exit 1; fi
  done
  cp -p "$guest/mode.json" /run/aboutme/crowdsec-cutover/mode.json
  cp -p /run/aboutme/crowdsec-retention-ok "$guest/lease"
  for timestamp in '7 minutes ago' '+1 minute'; do
    touch -d "$timestamp" /run/aboutme/crowdsec-retention-ok
    if "$lib/crowdsec-retention-lease-check.sh" >/dev/null 2>&1; then exit 1; fi
  done
  cp -p "$guest/lease" /run/aboutme/crowdsec-retention-ok
  "$lib/fence.sh" cutover-watch "$owner" v0.8.9
  systemd_stage=consumers
  for unit in crowdsec crowdsec-firewall-bouncer aboutme-web aboutme-server aboutme-caddy; do
    "$lib/fence.sh" start-unit "$owner" v0.8.9 "$unit.service"
  done
  if "$lib/fence.sh" check-current aboutme-caddy.service >/dev/null 2>&1; then exit 1; fi
  if "$lib/fence.sh" start-unit "$owner" v0.8.9 aboutme-maintenance.service >/dev/null 2>&1; then exit 1; fi
  # A real masked timer forces a post-marker completion failure. The same
  # owner and restored marker must block every start until fresh safety passes.
  mv /etc/systemd/system/aboutme-crowdsec-vacuum.timer "$guest/vacuum.timer"
  ln -s /dev/null /etc/systemd/system/aboutme-crowdsec-vacuum.timer
  systemctl daemon-reload
  systemd_stage=completion-refusal
  if "$lib/fence.sh" cutover-complete "$owner" v0.8.9 >/dev/null 2>&1; then exit 1; fi
  [[ -f /var/lib/aboutme/cutover-mutation.json ]]
  [[ $("$lib/fence.sh" read | jq -r .operation_id) == "$owner" ]]
  for unit in aboutme-caddy crowdsec crowdsec-firewall-bouncer; do
    ! systemctl is-active --quiet "$unit.service"
    if "$lib/fence.sh" check-current "$unit.service" >/dev/null 2>&1; then exit 1; fi
  done
  rm /etc/systemd/system/aboutme-crowdsec-vacuum.timer
  mv "$guest/vacuum.timer" /etc/systemd/system/aboutme-crowdsec-vacuum.timer
  systemctl daemon-reload
  systemd_stage=completion-recovery
  systemctl stop aboutme-crowdsec-retention-startup.service
  for unit in aboutme-crowdsec-vacuum aboutme-crowdsec-retention-startup aboutme-crowdsec-retention; do
    "$lib/fence.sh" start-unit "$owner" v0.8.9 "$unit.service"
  done
  "$lib/fence.sh" cutover-mode "$owner" v0.8.9
  "$lib/fence.sh" cutover-watch "$owner" v0.8.9
  for unit in crowdsec crowdsec-firewall-bouncer aboutme-web aboutme-server aboutme-caddy; do
    "$lib/fence.sh" start-unit "$owner" v0.8.9 "$unit.service"
  done
  "$lib/fence.sh" cutover-complete "$owner" v0.8.9
  [[ $("$lib/fence.sh" read | jq -r '.operation_id // empty') == '' ]]
  [[ ! -e /var/lib/aboutme/cutover-mutation.json ]]
  "$lib/crowdsec-retention-lease-check.sh"
  echo 'PASS systemd-cutover-lifecycle'
  exit 0
fi
fence=$repo_root/deploy/vn/scripts/fence.sh
safety=$repo_root/deploy/vn/scripts/deploy-safety.sh
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin" "$work/fence"

cat >"$work/bin/id" <<'STUB'
#!/usr/bin/env bash
[[ ${1-} == -u ]] && echo 0
STUB
cat >"$work/bin/systemctl" <<'STUB'
#!/usr/bin/env bash
if [[ ${1-} == is-active && ${3-} == aboutme-maintenance ]]; then exit 0; fi
exit 1
STUB
cat >"$work/bin/pgrep" <<'STUB'
#!/usr/bin/env bash
exit 1
STUB
cat >"$work/bin/logger" <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
cat >"$work/bin/mountpoint" <<'STUB'
#!/usr/bin/env bash
exit "${MOUNT_ABSENT:-0}"
STUB
chmod +x "$work/bin/"*
export PATH=$work/bin:$PATH FENCE_DIR=$work/fence FENCE_SAFETY_LIB=$safety

for template in aboutme-server aboutme-web aboutme-caddy aboutme-migrate aboutme-db-setup; do
  grep -Fq "ExecStartPre=/usr/local/lib/aboutme/fence.sh check @RELEASE_NUMBER@ $template" \
    "$repo_root/deploy/vn/host/quadlet/$template.container.in" || {
    echo "$template does not call the start-time fence" >&2
    exit 1
  }
done
grep -Fq '"$lib/fence.sh" check "$number" "aboutme-job@$name"' \
  "$repo_root/deploy/vn/host/job-run.sh" || {
  echo "scheduled jobs do not call the start-time fence" >&2
  exit 1
}

"$fence" init
if MOUNT_ABSENT=1 "$fence" check 4007 aboutme-server 2>/dev/null; then
  echo "the server started without its data mount" >&2
  exit 1
fi
# Bind account requests to one kind and tag. Neither release nor clear bypasses
# the operation quiescence guard, including after a lost lock reply.
request=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA
[[ $("$fence" lock-for account_delete v0.7.9 "$request") == "$request" ]]
[[ $("$fence" lock-for account_delete v0.7.9 "$request") == "$request" ]]
if "$fence" lock-for account_delete v0.7.10 "$request" 2>/dev/null; then
  echo "a bound request changed its tag" >&2
  exit 1
fi
if "$fence" checkpoint "$request" v0.7.10 2>/dev/null; then
  echo "a checkpoint bypassed the bound tag" >&2
  exit 1
fi
cat >"$work/operation-safety.sh" <<'STUB'
aboutme_assert_operation_release() { [[ ${OPERATION_RELEASE_SAFE:-0} == 1 ]]; }
STUB
for verb in release clear; do
  if FENCE_SAFETY_LIB=$work/operation-safety.sh "$fence" "$verb" "$request" 2>/dev/null; then
    echo "an unresolved account request bypassed the release guard" >&2
    exit 1
  fi
done
OPERATION_RELEASE_SAFE=1 FENCE_SAFETY_LIB=$work/operation-safety.sh "$fence" release "$request"
if "$fence" lock-for account_delete v0.4.6 "$request" 2>/dev/null; then
  echo "an account request bypassed the release floor" >&2
  exit 1
fi
# A private copy redirects only the fixed marker path for CI fixtures.
mkdir -p "$work/operations"
sed "s|/srv/data/host-operations|$work/operations|g" "$fence" >"$work/marker-fence.sh"
chmod +x "$work/marker-fence.sh"
for marker in malformed symlink; do
  if [[ $marker == malformed ]]; then printf '{"bad":true}\n' >"$work/operations/key-change.json"
  else ln -s "$work/nonexistent" "$work/operations/key-change.json"; fi
  for unit in aboutme-server aboutme-web aboutme-caddy aboutme-migrate aboutme-job@media-deletion-sweep; do
    if "$work/marker-fence.sh" check 7009 "$unit" 2>/dev/null; then
      echo "a retained key marker allowed an image start" >&2
      exit 1
    fi
  done
  rm "$work/operations/key-change.json"
done
op=$("$fence" lock cutover v0.4.7)
"$fence" mutation-start "$op" v0.4.7 reset-db
for unit in aboutme-server aboutme-web aboutme-caddy \
  aboutme-job@media-deletion-sweep aboutme-migrate; do
  if "$fence" check 4007 "$unit" 2>/dev/null; then
    echo "$unit started while a cutover mutation was active" >&2
    exit 1
  fi
done
"$fence" check 4007 aboutme-db-setup
cp "$work/fence/cutover-mutation.json" "$work/reset-db-marker.json"
jq '.operation_id = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"' \
  "$work/reset-db-marker.json" >"$work/fence/cutover-mutation.json"
if "$fence" check 4007 aboutme-db-setup 2>/dev/null; then
  echo "db-setup started with a marker owned by another operation" >&2
  exit 1
fi
cp "$work/reset-db-marker.json" "$work/fence/cutover-mutation.json"
if "$fence" lock deploy v0.4.7 2>/dev/null; then
  echo "a deploy acquired the cutover lock" >&2
  exit 1
fi
if "$fence" release "$op" 2>/dev/null; then
  echo "the cutover lock released with an active mutation" >&2
  exit 1
fi
"$fence" mutation-clear "$op" reset-db
if "$fence" check 4007 aboutme-server 2>/dev/null; then
  echo "the server started while the cutover lock remained" >&2
  exit 1
fi
if "$fence" check 4007 aboutme-db-setup 2>/dev/null; then
  echo "db-setup started without its reset-db marker" >&2
  exit 1
fi
"$fence" mutation-start "$op" v0.4.7 media
if "$fence" check 4007 aboutme-db-setup 2>/dev/null; then
  echo "db-setup started for a media mutation" >&2
  exit 1
fi
if "$fence" recover-cutover-media "$op" wrong-phrase inspect 2>/dev/null; then
  echo "media recovery accepted no operator attestation" >&2
  exit 1
fi
"$fence" recover-cutover-media "$op" media-writer-stopped-and-result-inspected inspect
"$fence" release "$op"

deploy_op=$("$fence" lock deploy v0.4.7)
for unit in aboutme-server aboutme-web aboutme-caddy \
  aboutme-job@media-deletion-sweep aboutme-db-setup aboutme-migrate; do
  "$fence" check 4007 "$unit"
done
"$fence" release "$deploy_op"

printf '{"state":"active"}\n' >"$work/fence/cutover-mutation.json"
if "$fence" check 4007 aboutme-server 2>/dev/null; then
  echo "the server started with a malformed cutover marker" >&2
  exit 1
fi
if "$fence" check 4007 aboutme-db-setup 2>/dev/null; then
  echo "db-setup started with a malformed cutover marker" >&2
  exit 1
fi
rm -f "$work/fence/cutover-mutation.json"
ln -s "$work/reset-db-marker.json" "$work/fence/cutover-mutation.json"
if "$fence" check 4007 aboutme-db-setup 2>/dev/null; then
  echo "db-setup started with a symlinked cutover marker" >&2
  exit 1
fi

rm "$work/fence/cutover-mutation.json"
# Notice restore ownership has no reset-db exception and survives a new check
# process, as it does after reboot. Existing deploy/cutover assertions above
# keep their separate minimum-only and reset-db rules.
notice_op=$("$fence" lock notice_restore v0.4.7)
for unit in aboutme-server aboutme-web aboutme-caddy aboutme-migrate \
  aboutme-db-setup aboutme-job@media-deletion-sweep crowdsec crowdsec-firewall-bouncer; do
  if "$fence" check 4007 "$unit" >"$work/notice.out" 2>&1; then
    echo 'notice restore permitted a guarded start' >&2
    exit 1
  fi
done
"$fence" check 4007 aboutme-maintenance
"$fence" check 4007 postgresql@18-main
if "$fence" checkpoint "$request" v0.4.7 >/dev/null 2>&1; then
  echo 'notice restore accepted another owner' >&2
  exit 1
fi
"$fence" checkpoint "$notice_op" v0.4.7
if "$fence" notice-restore-release "$request" >/dev/null 2>&1; then
  echo 'strict notice release accepted a different owner' >&2
  exit 1
fi
# This lightweight fixture has no mounted authority. Successful exact release
# belongs to the complete mounted host-controller proof, not this setup reset.
jq 'del(.operation_id,.operation_kind,.operation_tag,.operation_started_at,.operation_checked_at)' \
  "$work/fence/fence.json" >"$work/unowned.json"
cp "$work/unowned.json" "$work/fence/fence.json"
if "$fence" notice-restore-release "$notice_op" >/dev/null 2>&1; then
  echo 'strict notice release retried against empty ownership' >&2
  exit 1
fi
for adoption in initializing adopted; do
  jq --arg state "$adoption" --arg op "$notice_op" \
    '.notice_authority={version:1,state:$state,operation_id:$op}' \
    "$work/fence/fence.json" >"$work/evidence.json"
  cp "$work/evidence.json" "$work/fence/fence.json"
  if [[ $adoption == initializing ]]; then
    for unit in aboutme-server aboutme-web aboutme-caddy aboutme-migrate \
      aboutme-db-setup aboutme-job@media-deletion-sweep crowdsec crowdsec-firewall-bouncer; do
      if "$fence" check 4007 "$unit" >/dev/null 2>&1; then
        echo 'initializing evidence permitted a start without ownership' >&2
        exit 1
      fi
    done
    "$fence" check 4007 aboutme-maintenance
    "$fence" check 4007 postgresql@18-main
  fi
done
preserved=$(jq -c .notice_authority "$work/fence/fence.json")
notice_op=$("$fence" lock deploy v0.4.7)
for verb in checkpoint raise; do
  "$fence" "$verb" "$notice_op" v0.4.7
  [[ $(jq -c .notice_authority "$work/fence/fence.json") == "$preserved" ]]
done
"$fence" release "$notice_op"
[[ $(jq -c .notice_authority "$work/fence/fence.json") == "$preserved" ]]
for evidence in 'null' '{}' '{"version":2,"state":"adopted"}' \
  '{"version":1,"state":"adopted","operation_id":"bad"}' \
  '{"version":1,"state":"adopted","operation_id":"bad","extra":true}'; do
  jq --argjson value "$evidence" '.notice_authority=$value' "$work/evidence.json" >"$work/fence/fence.json"
  if "$fence" check 4007 aboutme-server >/dev/null 2>&1; then
    echo 'malformed notice evidence permitted startup' >&2
    exit 1
  fi
done
# The unit set is exact; normalization cannot turn an unlisted name into authority.
# shellcheck source=../host/crowdsec-cutover-state.sh
source "$repo_root/deploy/vn/host/crowdsec-cutover-state.sh"
for unit in aboutme-server.service aboutme-web.service crowdsec.service \
  crowdsec-firewall-bouncer.service aboutme-caddy.service aboutme-maintenance.service \
  aboutme-crowdsec-retention-startup.service aboutme-crowdsec-retention.service \
  aboutme-crowdsec-vacuum.service; do
  cutover_unit_allowed "$unit"
done
for unit in aboutme-job@privacy-retention-sweep.service aboutme-migrate.service \
  aboutme-db-setup.service aboutme-crowdsec-retention.timer arbitrary.service \
  ../aboutme-server.service aboutme-server; do
  if cutover_unit_allowed "$unit"; then echo 'FAIL cutover-unit-allowlist'; exit 1; fi
done
# An argument or process environment is not live service membership.
if cutover_invocation aboutme-crowdsec-retention.service >/dev/null 2>&1; then
  echo 'FAIL cutover-invocation-membership'; exit 1
fi
echo "cutover fence tests passed"
