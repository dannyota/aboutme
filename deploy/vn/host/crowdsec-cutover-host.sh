#!/usr/bin/env bash
# Atomic host transition. See docs/design/crowdsec-cutover.md.
set -euo pipefail
umask 077
lib=/usr/local/lib/aboutme
fence=$lib/fence.sh
checkpoint() { "$fence" checkpoint "$op" "$tag" >/dev/null 2>&1; }
refuse() { printf 'crowdsec_cutover_refused\n' >&2; exit 1; }

activate_bundle() {
  checkpoint || return 1
  # The bundle is the exact reviewed release bundle copied before ownership.
  [[ -d $bundle/host && ! -L $bundle && $(stat -c %u "$bundle") == 0 ]] || return 1
  local helper file relative
  for helper in crowdsec-db crowdsec-provision crowdsec-cutover-state crowdsec-retention \
    crowdsec-retention-lease-check crowdsec-retention-fail crowdsec-vacuum crowdsec-window-check crowdsec-offline; do
    install -o root -g root -m 0755 "$bundle/host/$helper.sh" "$lib/$helper.sh" || return 1
  done
  install -o root -g root -m 0755 "$bundle/host/bin/watch.sh" "$lib/watch.sh"
  install -m 0644 "$bundle/host/crowdsec-schema-v1.8.1.tsv" "$lib/crowdsec-schema-v1.8.1.tsv"
  install -d -m 0755 "$lib/quadlet"
  for file in "$bundle"/host/quadlet/*; do install -m 0644 "$file" "$lib/quadlet/${file##*/}"; done
  for file in "$bundle"/host/etc/systemd/system/aboutme-crowdsec-* \
    "$bundle"/host/etc/systemd/system/aboutme-watch.service "$bundle"/host/etc/systemd/system/aboutme-watch.timer \
    "$bundle"/host/etc/systemd/system/crowdsec.service.d/90-aboutme-alert.conf \
    "$bundle"/host/etc/systemd/system/crowdsec-firewall-bouncer.service.d/90-aboutme-alert.conf; do
    [[ -f $file ]] || continue
    relative=${file#"$bundle/host/etc/"}
    install -d -m 0755 "/etc/$(dirname "$relative")"
    install -m 0644 "$file" "/etc/$relative" || return 1
  done
  install -d -m 0755 /etc/crowdsec/acquis.d /etc/crowdsec/parsers/s01-parse /etc/crowdsec/parsers/s02-enrich /etc/crowdsec/scenarios
  for file in config.yaml.local console.yaml profiles.yaml; do
    install -m 0644 "$bundle/edge/crowdsec/$file" "/etc/crowdsec/$file"
  done
  install -m 0644 "$bundle/edge/crowdsec/config.yaml.local" /etc/crowdsec/config.yaml.postgresql
  systemctl daemon-reload
}
quiesce() {
  checkpoint || return 1
  local unit load
  for unit in aboutme-crowdsec-retention.timer aboutme-crowdsec-vacuum.timer aboutme-watch.timer; do
    load=$(systemctl show --property=LoadState --value "$unit") || return 1
    [[ $load != not-found ]] || continue
    systemctl disable --now "$unit" >/dev/null 2>&1 || return 1
  done
  for unit in aboutme-watch.service aboutme-crowdsec-retention.service aboutme-crowdsec-vacuum.service \
    aboutme-crowdsec-retention-startup.service aboutme-caddy.service aboutme-maintenance.service \
    crowdsec-firewall-bouncer.service crowdsec.service aboutme-server.service aboutme-web.service; do
    load=$(systemctl show --property=LoadState --value "$unit") || return 1
    [[ $load != not-found ]] || continue
    systemctl stop "$unit" || return 1
  done
  # shellcheck source=../scripts/deploy-safety.sh
  source "$lib/deploy-safety.sh"
  aboutme_stop_jobs 60 || return 1
  # shellcheck source=crowdsec-cutover-state.sh
  source "$lib/crowdsec-cutover-state.sh"
  cutover_stopped
}
package_install() { # exact pinned package only
  checkpoint || return 1
  local spec=$1 current planned
  current=$(dpkg-query -W -f='${Status}\t${Version}' "${spec%%=*}" 2>/dev/null) || current=absent
  [[ $current != $'install ok installed\t'"${spec#*=}" ]] || return 0
  [[ $current == absent || $current == $'unknown ok not-installed\t' ]] || return 1
  [[ -z $(dpkg --audit 2>/dev/null) ]] || return 1
  planned=$(apt-get -s --no-install-recommends install "$spec" 2>/dev/null) || return 1
  # Reject dependency repair, replacements and unrelated package hooks.
  python3 -I - "$planned" "${spec%%=*}" <<'PY' >/dev/null 2>&1 || return 1
import sys
for line in sys.argv[1].splitlines():
    if line.startswith(('Inst ','Conf ')): assert line.split()[1]==sys.argv[2]
    assert not line.startswith('Remv ')
PY
  crowdsec_install_offline apt-get -o Dir::Log::Terminal=/dev/null -o Dir::Log::History=/dev/null \
    -o DPkg::Options::=--log=/dev/null install --no-install-recommends -y -qq "$spec" || return 1
  [[ $(dpkg-query -W -f='${Status}\t${Version}' "${spec%%=*}") == $'install ok installed\t'"${spec#*=}" ]]
}
remove_database_artifacts() {
  checkpoint && cutover_stopped || return 1
  # Resolve only database artifacts from the effective legacy storage config.
  # Never remove data directories or assets by glob.
  python3 -I - "$work/legacy-db" <<'PY' || return 1
import os,pathlib,stat,sys
path=pathlib.Path(sys.argv[1]); raw=path.read_text().strip()
if raw:
    db=pathlib.Path(raw)
    assert db.is_absolute() and db.name=='crowdsec.db'
    root=pathlib.Path('/var/lib/crowdsec/data').resolve(strict=True)
    assert db.parent.resolve(strict=True)==root
    for suffix in ('','-wal','-shm','-journal'):
        item=pathlib.Path(str(db)+suffix)
        if item.exists() or item.is_symlink():
            entry=item.lstat(); assert stat.S_ISREG(entry.st_mode) and entry.st_nlink==1
            item.unlink()
PY
}
rotate_credentials() {
  checkpoint || return 1
  # All generation goes directly to protected files on encrypted storage.
  [[ -d /etc/crowdsec && ! -L /etc/crowdsec && -d /srv/data/crowdsec && ! -L /srv/data/crowdsec &&
    $(stat -c %d /etc/crowdsec) == "$(stat -c %d /srv/data)" ]] || return 1
  install -d -o root -g root -m 0700 /etc/aboutme/secrets
  "$lib/crowdsec-db.sh" preflight-ready </dev/null >/dev/null 2>&1 || return 1
  /usr/bin/cscli machines add -a --force --error >/dev/null 2>&1 || return 1
  chmod 0600 /etc/crowdsec/local_api_credentials.yaml
  local name destination temp
  for name in aboutme-firewall aboutme-caddy; do
    destination=/srv/data/crowdsec/firewall-bouncer.key
    [[ $name != aboutme-caddy ]] || destination=/etc/aboutme/secrets/crowdsec-bouncer-key
    temp=$(mktemp "${destination}.XXXXXX") || return 1
    "$lib/crowdsec-db.sh" preflight-ready </dev/null >/dev/null 2>&1 || { rm -f "$temp"; return 1; }
    if ! /usr/bin/cscli bouncers add "$name" -o raw >"$temp" 2>/dev/null; then rm -f "$temp"; return 1; fi
    [[ -s $temp ]] || { rm -f "$temp"; return 1; }
    chmod 0600 "$temp" && mv -fT "$temp" "$destination" || return 1
  done
  install -d -m 0700 /etc/crowdsec/bouncers
  cat "$bundle/edge/crowdsec/bouncers/crowdsec-firewall-bouncer.yaml.local" >/etc/crowdsec/bouncers/crowdsec-firewall-bouncer.yaml.local
  printf 'api_key: %s\n' "$(cat /srv/data/crowdsec/firewall-bouncer.key)" >>/etc/crowdsec/bouncers/crowdsec-firewall-bouncer.yaml.local
  "$lib/secrets-host.sh" podman-sync --replace crowdsec-bouncer-key >/dev/null 2>&1
}
credentials_rejected() {
  local file code
  for file in "$work/old-firewall" "$work/old-caddy"; do
    [[ -s $file ]] || continue
    code=$(printf 'header = "X-Api-Key: %s"\n' "$(cat "$file")" |
      curl -s -o /dev/null -w '%{http_code}' --max-time 5 -K - http://127.0.0.1:8095/v1/decisions) || return 1
    [[ $code == 403 || $code == 401 ]] || return 1
  done
  if [[ -s $work/old-machine ]]; then
    code=$(python3 -I - "$work/old-machine" <<'PY' 2>/dev/null |
import json,sys,yaml
data=yaml.safe_load(open(sys.argv[1])); print(json.dumps({'machine_id':data['login'],'password':data['password']}))
PY
      curl -s -o /dev/null -w '%{http_code}' --max-time 5 -H 'Content-Type: application/json' --data-binary @- http://127.0.0.1:8095/v1/watchers/login) || return 1
    [[ $code == 401 || $code == 403 ]] || return 1
  fi
}
transition() {
  # Preserve only bounded credentials/config metadata in private tmpfs for the
  # rejection proof. Never import attack history or copy the legacy database.
  local file
  : >"$work/legacy-db"
  python3 -I - /etc/crowdsec/config.yaml /etc/crowdsec/config.yaml.local <<'PY' >"$work/legacy-db" 2>/dev/null || return 1
import os,sys,yaml
merged={}
for file in sys.argv[1:]:
    if os.path.exists(file): merged.update((yaml.safe_load(open(file)) or {}).get('db_config',{}))
if merged.get('type')=='sqlite': print(merged['db_path'])
PY
  for file in old-machine:/etc/crowdsec/local_api_credentials.yaml \
    old-firewall:/srv/data/crowdsec/firewall-bouncer.key old-caddy:/etc/aboutme/secrets/crowdsec-bouncer-key; do
    if [[ -f ${file#*:} && ! -L ${file#*:} ]]; then install -m 0600 "${file#*:}" "$work/${file%%:*}"; fi
  done
  quiesce && activate_bundle || return 1
  systemctl disable --now aboutme-crowdsec-retention.timer aboutme-crowdsec-vacuum.timer aboutme-watch.timer >/dev/null 2>&1 || return 1
  cutover_timers_disabled && cutover_watch_stopped || return 1
  # Reload peer policy only after every consumer is joined.
  systemctl restart postgresql@18-main.service || return 1
  install -d -m 0700 /run/aboutme/crowdsec
  # shellcheck source=crowdsec-offline.sh
  source "$lib/crowdsec-offline.sh"
  package_install crowdsec=1.8.1 || return 1
  crowdsec_remove_online /etc/crowdsec/config.yaml /etc/crowdsec/config.yaml.local /etc/crowdsec/online_api_credentials.yaml || return 1
  remove_database_artifacts || return 1
  printf '{"operation_id":"%s","release_tag":"%s"}\n' "$op" "$tag" |
    "$lib/crowdsec-provision.sh" restore-reset >/dev/null 2>&1 || return 1
  rotate_credentials && package_install crowdsec-firewall-bouncer-nftables=0.0.36 || return 1
  crowdsec_assert_postgresql && crowdsec_configure_local_detection || return 1
  local group
  for group in acquis.d parsers/s01-parse parsers/s02-enrich scenarios; do
    for file in "$bundle/edge/crowdsec/$group/"*.yaml; do
      [[ -f $file ]] || continue
      install -m 0644 "$file" "/etc/crowdsec/$group/${file##*/}" || return 1
    done
  done
  "$lib/crowdsec-window-check.sh" >/dev/null 2>&1 || return 1
  systemd-tmpfiles --create /etc/tmpfiles.d/aboutme.conf || return 1
  systemctl unmask --runtime crowdsec.service crowdsec-firewall-bouncer.service >/dev/null 2>&1 || return 1
  "$fence" raise "$op" "$tag" >/dev/null 2>&1 || return 1
  "$fence" start-unit "$op" "$tag" aboutme-crowdsec-vacuum.service || return 1
  "$fence" start-unit "$op" "$tag" aboutme-crowdsec-retention-startup.service || return 1
  "$fence" start-unit "$op" "$tag" aboutme-crowdsec-retention.service || return 1
  "$fence" cutover-mode "$op" "$tag" || return 1
  systemctl enable --now aboutme-watch.timer >/dev/null 2>&1 || return 1
  "$fence" cutover-watch "$op" "$tag" || return 1
  "$fence" start-unit "$op" "$tag" crowdsec.service || return 1
  credentials_rejected || return 1
  "$fence" start-unit "$op" "$tag" crowdsec-firewall-bouncer.service || return 1
  "$fence" start-unit "$op" "$tag" aboutme-web.service || return 1
  "$fence" start-unit "$op" "$tag" aboutme-server.service || return 1
  "$fence" start-unit "$op" "$tag" aboutme-caddy.service || return 1
  curl -fsS -o /dev/null --max-time 10 http://127.0.0.1:8080/readyz || return 1
  # Leave ownership and the marker retained for the primary's bounded external
  # enforcement/resource/backup proofs. Only exact completion opens scheduling.
  printf 'PASS cutover-serving-fenced\n'
}

if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
  [[ $EUID == 0 && $# == 4 && $1 == transition ]] || refuse
  op=$2 tag=$3 bundle=$(readlink -f "$4")
  checkpoint || refuse
  work=$(mktemp -d /run/aboutme/crowdsec-transition.XXXXXX)
  trap 'rm -rf -- "$work"' EXIT
  trap '"$fence" cutover-fail "$op" >/dev/null 2>&1; exit 1' HUP INT TERM
  if ! transition >/dev/null 2>&1; then "$fence" cutover-fail "$op" >/dev/null 2>&1 || true; refuse; fi
  printf 'PASS cutover-serving-fenced\n'
fi
