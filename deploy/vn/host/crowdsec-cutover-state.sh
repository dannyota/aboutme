#!/usr/bin/env bash
# shellcheck source-path=SCRIPTDIR
# Source-only composition under the canonical fence lock.
# See docs/design/crowdsec-cutover.md.

cutover_unit_allowed() {
  case $1 in
    aboutme-server.service | aboutme-web.service | crowdsec.service | \
    crowdsec-firewall-bouncer.service | aboutme-caddy.service | aboutme-maintenance.service | \
    aboutme-crowdsec-retention-startup.service | aboutme-crowdsec-retention.service | \
    aboutme-crowdsec-vacuum.service) return 0 ;;
    *) return 1 ;;
  esac
}
cutover_safe() {
  [[ -f $1 && ! -L $1 && $(stat -c '%u:%g:%a:%h' "$1") == "0:0:$2:1" ]]
}
cutover_paths() {
  [[ $EUID == 0 && -d /run/aboutme && ! -L /run/aboutme &&
    $(stat -c '%u:%g:%a' /run/aboutme) == 0:0:755 &&
    $(findmnt -n -o FSTYPE -T /run/aboutme) == tmpfs ]] || return 1
  cutover_run=/run/aboutme/crowdsec-cutover
  if [[ ! -e $cutover_run && ! -L $cutover_run ]]; then
    install -d -o root -g root -m 0700 "$cutover_run" || return 1
  fi
  [[ -d $cutover_run && ! -L $cutover_run && $(stat -c '%u:%g:%a' "$cutover_run") == 0:0:700 ]]
}
cutover_write() { # fixed destination, JSON on stdin
  local tmp
  tmp=$(mktemp "$cutover_run/.state.XXXXXX") || return 1
  if ! cat >"$tmp" || ! chmod 0600 "$tmp" || ! mv -fT "$tmp" "$1"; then
    rm -f -- "$tmp"; return 1
  fi
}
cutover_invocation() { # exact unit; prove this process belongs to it
  local state
  cutover_unit_allowed "$1" || return 1
  state=$(systemctl show --all --property=InvocationID --property=ControlGroup "$1" 2>/dev/null) || return 1
  python3 -I - "$state" <<'PY' 2>/dev/null
import pathlib,re,sys
try:
    rows=sys.argv[1].splitlines(); assert len(rows)==2
    state=dict(row.split('=',1) for row in rows)
    group=state['ControlGroup']; invocation=state['InvocationID']
    assert re.fullmatch(r'[0-9a-f]{32}',invocation)
    assert group.startswith('/system.slice/') and '..' not in group
    actual=[row.split(':',2)[2] for row in pathlib.Path('/proc/self/cgroup').read_text().splitlines() if row.startswith('0::')]
    assert len(actual)==1 and (actual[0]==group or actual[0].startswith(group+'/'))
    print(invocation)
except Exception: sys.exit(1)
PY
}
cutover_binding() {
  cutover_safe /var/lib/aboutme/fence.json 644 &&
    cutover_safe /var/lib/aboutme/cutover-mutation.json 600 || return 1
  cutover_document=$(jq -ce '
    select(.operation_kind=="cutover" and (.operation_id|type=="string" and test("^[A-Za-z0-9_-]{42}[AEIMQUYcgkosw048]$"))
      and (.operation_tag|type=="string" and test("^v[0-9]+\\.[0-9]+\\.[0-9]+$"))
      and (.minimum_release|type=="number" and .>=0 and .==floor)
      and (.operation_started_at|type=="string") and (.operation_checked_at|type=="string"))
  ' /var/lib/aboutme/fence.json) || return 1
  cutover_owner=$(jq -r .operation_id <<<"$cutover_document")
  cutover_tag=$(jq -r .operation_tag <<<"$cutover_document")
  jq -e --arg owner "$cutover_owner" 'keys==["mutation_kind","operation_id","started_at","state"]
    and .operation_id==$owner and .mutation_kind=="reset-db" and .state=="active"' \
    /var/lib/aboutme/cutover-mutation.json >/dev/null || return 1
  cutover_marker=$(sha256sum /var/lib/aboutme/cutover-mutation.json | cut -d' ' -f1)
  [[ $cutover_tag =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)$ ]] || return 1
  cutover_release=$((10#${BASH_REMATCH[1]}*1000000+10#${BASH_REMATCH[2]}*1000+10#${BASH_REMATCH[3]}))
  jq -e --argjson release "$cutover_release" '.minimum_release<=$release' <<<"$cutover_document" >/dev/null
}
cutover_record() { # root-only record, exact binding and optional unit
  cutover_safe "$1" 600 || return 1
  jq -e --arg owner "$cutover_owner" --arg tag "$cutover_tag" --arg marker "$cutover_marker" \
    --arg unit "${2:-}" '
    .version==1 and .owner==$owner and .tag==$tag and .marker==$marker
    and (if $unit=="" then true else .unit==$unit end)
  ' "$1" >/dev/null 2>&1
}
cutover_stopped() { # positive systemd, process and container quiescence
  local unit state group result
  for unit in aboutme-server.service aboutme-web.service aboutme-caddy.service aboutme-maintenance.service \
    crowdsec.service crowdsec-firewall-bouncer.service; do
    state=$(systemctl show --all --property=ActiveState --property=Job --property=MainPID \
      --property=ControlPID --property=ControlGroup "$unit" 2>/dev/null) || return 1
    [[ $(wc -l <<<"$state") == 5 ]] && grep -qxE 'ActiveState=(inactive|failed)' <<<"$state" &&
      grep -qxF Job= <<<"$state" && grep -qxF MainPID=0 <<<"$state" && grep -qxF ControlPID=0 <<<"$state" || return 1
    group=$(sed -n 's/^ControlGroup=//p' <<<"$state")
    if [[ -n $group ]]; then
      [[ $group == /system.slice/*.service && $group != *..* ]] || return 1
      [[ ! -e /sys/fs/cgroup$group/cgroup.events ]] || grep -qxF 'populated 0' "/sys/fs/cgroup$group/cgroup.events" || return 1
    fi
  done
  for unit in aboutme-server aboutme-web aboutme-caddy aboutme-maintenance; do
    result=0
    podman container exists "$unit" >/dev/null 2>&1 || result=$?
    case $result in
      0) [[ $(podman inspect --format '{{.State.Running}} {{.State.Pid}}' "$unit" 2>/dev/null) == 'false 0' ]] || return 1 ;;
      1) ;;
      *) return 1 ;;
    esac
  done
}
cutover_timers_disabled() {
  local unit state enabled
  for unit in aboutme-crowdsec-retention.timer aboutme-crowdsec-vacuum.timer; do
    enabled=$(systemctl is-enabled "$unit" 2>/dev/null) || true
    [[ $enabled == disabled || $enabled == masked ]] || return 1
    state=$(systemctl show --all --property=ActiveState --property=Job "$unit" 2>/dev/null) || return 1
    [[ $(wc -l <<<"$state") == 2 ]] && grep -qxF ActiveState=inactive <<<"$state" && grep -qxF Job= <<<"$state" || return 1
  done
}
cutover_watch_stopped() {
  local unit state
  for unit in aboutme-watch.timer aboutme-watch.service; do
    state=$(systemctl show --all --property=ActiveState --property=Job "$unit" 2>/dev/null) || return 1
    [[ $(wc -l <<<"$state") == 2 ]] && grep -qxE 'ActiveState=(inactive|failed)' <<<"$state" && grep -qxF Job= <<<"$state" || return 1
  done
}
cutover_bootstrap() {
  cutover_paths && cutover_binding && cutover_timers_disabled && cutover_stopped && cutover_watch_stopped || return 1
  local unit=aboutme-crowdsec-retention.service invocation
  invocation=$(cutover_invocation "$unit") || return 1
  cutover_record "$cutover_run/spent-$unit.json" "$unit" || return 1
  jq -e --arg invocation "$invocation" --argjson now "$(date +%s)" \
    '.invocation==$invocation and .deadline>=$now and .issued<=$now' \
    "$cutover_run/spent-$unit.json" >/dev/null
}
cutover_mode() { # caller already holds the canonical lock and proved lease epoch
  cutover_paths && cutover_binding && cutover_timers_disabled || return 1
  cutover_record "$cutover_run/mode.json" || return 1
  jq -e --arg epoch "${crowdsec_db_epoch:?complete database gate required}" '
    .epoch==$epoch and (.outcomes|keys)==["aboutme-crowdsec-retention-startup.service","aboutme-crowdsec-retention.service","aboutme-crowdsec-vacuum.service"]
    and all(.outcomes[]; .epoch==$epoch and (.invocation|test("^[0-9a-f]{32}$")))
  ' "$cutover_run/mode.json" >/dev/null
}
cutover_start_prepare() { # called by fence while holding its exclusive lock
  local id=$1 tag=$2 unit=$3 seconds=60
  cmd_checkpoint "$id" "$tag"
  cutover_paths && cutover_binding && cutover_unit_allowed "$unit" || die 'cutover start refused'
  [[ $cutover_owner == "$id" && $cutover_tag == "$tag" ]] || die 'cutover owner differs'
  cutover_timers_disabled || die 'cutover scheduling is active'
  [[ ! -e $cutover_run/permit.json && ! -L $cutover_run/permit.json ]] || die 'an unconsumed permit remains'
  local state
  state=$(systemctl show --all --property=ActiveState --property=Job "$unit") || die 'unit state unavailable'
  [[ $(wc -l <<<"$state") == 2 ]] && grep -qxE 'ActiveState=(inactive|failed)' <<<"$state" &&
    grep -qxF Job= <<<"$state" || die 'unit is active or has a pending job'
  case $unit in
    aboutme-crowdsec-vacuum.service | aboutme-crowdsec-retention-startup.service | aboutme-crowdsec-retention.service)
      if [[ ! -f $cutover_run/mode.json ]]; then cutover_stopped && cutover_watch_stopped || die 'bootstrap is not quiescent'; fi
      [[ $unit == aboutme-crowdsec-retention.service ]] || seconds=300 ;;
    *)
      # Full mode and a successful watchdog tick precede any consumer.
      cutover_internal_lease && cutover_mode || die 'cutover mode missing'
      cutover_record "$cutover_run/watch.json" || die 'cutover watchdog missing'
      jq -e --arg epoch "${crowdsec_db_epoch:?complete database gate required}" '.epoch==$epoch' "$cutover_run/watch.json" >/dev/null || die 'watchdog epoch differs'
      ;;
  esac
  case $unit in
    aboutme-caddy.service | aboutme-maintenance.service)
      local other=aboutme-maintenance.service
      [[ $unit != aboutme-maintenance.service ]] || other=aboutme-caddy.service
      ! systemctl is-active --quiet "$other" || die 'the other Caddy mode remains active'
      if [[ -e $cutover_run/caddy.json ]]; then
        cutover_record "$cutover_run/caddy.json" "$unit" || die 'another final Caddy mode was selected'
      else cutover_new_record "$unit" | cutover_write "$cutover_run/caddy.json" || die 'Caddy binding failed'; fi ;;
  esac
  rm -f -- "$cutover_run/spent-$unit.json" "$cutover_run/outcome-$unit.json"
  cutover_new_record "$unit" | jq --argjson issued "$(date +%s)" --argjson seconds "$seconds" \
    '.issued=$issued | .deadline=($issued+$seconds)' | cutover_write "$cutover_run/permit.json" || die 'permit write failed'
  # Persist that ordinary release can never clear a post-start cutover.
  jq '.crowdsec_cutover_started=true' <<<"${doc:?owning fence document required}" | store
}
cutover_new_record() {
  jq -cn --arg owner "$cutover_owner" --arg tag "$cutover_tag" --arg marker "$cutover_marker" --arg unit "${1:-}" \
    '{version:1,owner:$owner,tag:$tag,marker:$marker,unit:$unit}'
}
cutover_consume() { # exact unit; caller holds exclusive fence lock
  local unit=$1 invocation
  cutover_paths && cutover_binding && cutover_unit_allowed "$unit" || return 1
  cutover_record "$cutover_run/permit.json" "$unit" || return 1
  case $unit in
    aboutme-crowdsec-*.service) ;;
    *) cutover_internal_lease && cutover_mode || return 1 ;;
  esac
  invocation=$(cutover_invocation "$unit") || return 1
  jq -e --argjson now "$(date +%s)" '.issued<=$now and $now<=.deadline' "$cutover_run/permit.json" >/dev/null || return 1
  jq --arg invocation "$invocation" '.invocation=$invocation' "$cutover_run/permit.json" |
    cutover_write "$cutover_run/spent-$unit.json" || return 1
  rm -- "$cutover_run/permit.json"
}
cutover_start_finish() {
  local id=$1 tag=$2 unit=$3 state invocation
  cmd_checkpoint "$id" "$tag"
  cutover_paths && cutover_binding && cutover_record "$cutover_run/spent-$unit.json" "$unit" || die 'unit did not consume its permit'
  case $unit in
    aboutme-crowdsec-*.service)
      cutover_record "$cutover_run/outcome-$unit.json" "$unit" || die 'safety outcome missing'
      invocation=$(jq -r .invocation "$cutover_run/spent-$unit.json")
      jq -e --arg invocation "$invocation" '.invocation==$invocation' "$cutover_run/outcome-$unit.json" >/dev/null || die 'saved outcome refused'
      ;;
    *)
      state=$(systemctl show --all --property=InvocationID --property=Result --property=ExecMainStatus "$unit") || die 'unit result unavailable'
      invocation=$(sed -n 's/^InvocationID=//p' <<<"$state")
      systemctl is-active --quiet "$unit" && grep -qxF Result=success <<<"$state" &&
        jq -e --arg invocation "$invocation" '.invocation==$invocation' "$cutover_run/spent-$unit.json" >/dev/null || die 'unit outcome differs'
      ;;
  esac
  rm -f -- "$cutover_run/spent-$unit.json" "$cutover_run/permit.json"
}
cutover_safety_result() { # ExecStartPost proves a new successful safety invocation
  local unit=$1 invocation state
  case $unit in aboutme-crowdsec-retention-startup.service | aboutme-crowdsec-retention.service | aboutme-crowdsec-vacuum.service) ;; *) return 1 ;; esac
  # Ordinary scheduled safety passes have no cutover outcome to record.
  load
  [[ $(jq -r '.operation_kind // empty' <<<"${doc:?owning fence document required}") == cutover ]] || return 0
  cutover_paths && cutover_binding || return 1
  invocation=$(cutover_invocation "$unit") || return 1
  cutover_record "$cutover_run/spent-$unit.json" "$unit" || return 1
  jq -e --arg invocation "$invocation" '.invocation==$invocation' "$cutover_run/spent-$unit.json" >/dev/null || return 1
  state=$(systemctl show --all --property=ExecMainStatus --property=Result "$unit") || return 1
  grep -qxF ExecMainStatus=0 <<<"$state" && grep -qxF Result=success <<<"$state" && cutover_internal_db || return 1
  cutover_new_record "$unit" | jq --arg epoch "${crowdsec_db_epoch:?complete database gate required}" --arg invocation "$invocation" \
    '.epoch=$epoch | .invocation=$invocation' | cutover_write "$cutover_run/outcome-$unit.json"
}
cutover_internal_db() {
  # Source-only helpers reuse the fence command's live canonical descriptor.
  # shellcheck source=crowdsec-db.sh
  source /usr/local/lib/aboutme/crowdsec-db.sh
  # shellcheck disable=SC2034 # The sourced DB and lease checks consume this held descriptor.
  crowdsec_db_lock_fd=${fd:?held canonical fence descriptor required}
  crowdsec_db_held_lock && crowdsec_db_ready
}
cutover_internal_lease() {
  # shellcheck source=crowdsec-retention-lease-check.sh
  source /usr/local/lib/aboutme/crowdsec-retention-lease-check.sh
  # shellcheck disable=SC2034 # The sourced DB and lease checks consume this held descriptor.
  crowdsec_db_lock_fd=${fd:?held canonical fence descriptor required}
  crowdsec_lease_check
}
cutover_create_mode() {
  cmd_checkpoint "$1" "$2"
  cutover_paths && cutover_binding && cutover_stopped && cutover_timers_disabled && cutover_internal_db || die 'mode prerequisites refused'
  local unit results='{}' record
  for unit in aboutme-crowdsec-vacuum.service aboutme-crowdsec-retention-startup.service aboutme-crowdsec-retention.service; do
    record=$cutover_run/outcome-$unit.json
    cutover_record "$record" "$unit" && jq -e --arg epoch "${crowdsec_db_epoch:?complete database gate required}" '.epoch==$epoch' "$record" >/dev/null || die 'safety outcome missing'
    results=$(jq --arg unit "$unit" --argjson record "$(cat "$record")" '.+{($unit):$record}' <<<"$results")
  done
  # Validate the normal epoch lease without accepting a bootstrap invocation.
  # shellcheck source=crowdsec-retention-lease-check.sh
  source /usr/local/lib/aboutme/crowdsec-retention-lease-check.sh
  crowdsec_lease_file || die 'fresh lease missing'
  [[ ${crowdsec_lease_epoch:?validated normal lease required} == "${crowdsec_db_epoch:?complete database gate required}" ]] || die 'lease epoch differs'
  cutover_new_record | jq --arg epoch "${crowdsec_db_epoch:?complete database gate required}" --argjson outcomes "$results" \
    '.epoch=$epoch | .outcomes=$outcomes' | cutover_write "$cutover_run/mode.json" || die 'mode write failed'
}
cutover_watch_pass() {
  cmd_checkpoint "$1" "$2"
  cutover_internal_lease && cutover_mode || die 'cutover watchdog refused'
  cutover_new_record | jq --arg epoch "${crowdsec_db_epoch:?complete database gate required}" '.epoch=$epoch' |
    cutover_write "$cutover_run/watch.json" || die 'watchdog outcome write failed'
}
cutover_restore_marker() {
  # Never create ownership after an uncertain durable release.
  load
  [[ ${owner:?loaded fence owner required} == "$1" && $(jq -r .operation_kind <<<"${doc:?owning fence document required}") == cutover ]] || return 1
  if [[ ! -e ${mutation_file:?canonical mutation path required} && ! -L $mutation_file ]]; then
    jq -cn --arg owner "${owner:?loaded fence owner required}" --arg ts "$(now)" \
      '{operation_id:$owner,mutation_kind:"reset-db",state:"active",started_at:$ts}' | store_mutation
  fi
}
cutover_failure() {
  local id=$1
  systemctl disable --now aboutme-crowdsec-retention.timer aboutme-crowdsec-vacuum.timer >/dev/null 2>&1 || true
  systemctl stop aboutme-caddy.service aboutme-maintenance.service aboutme-server.service aboutme-web.service \
    crowdsec-firewall-bouncer.service crowdsec.service >/dev/null 2>&1 || true
  if cutover_paths; then rm -f -- "$cutover_run/"*.json; fi
  cutover_restore_marker "$id" && cutover_stopped
}
cutover_complete() {
  cmd_checkpoint "$1" "$2"
  cutover_internal_lease && cutover_mode || die 'completion admission refused'
  local unit
  for unit in aboutme-server.service aboutme-web.service aboutme-caddy.service crowdsec.service crowdsec-firewall-bouncer.service; do
    systemctl is-active --quiet "$unit" || die 'final consumer state refused'
  done
  ! systemctl is-active --quiet aboutme-maintenance.service || die 'maintenance remains active'
  cutover_safe /var/lib/aboutme/release.json 644 &&
    jq -e --arg tag "$2" '.release_tag==$tag and .maintenance_tag==$tag' /var/lib/aboutme/release.json >/dev/null || die 'release state differs'
  rm -f -- "$cutover_run/permit.json" "$cutover_run/"spent-*.json
  # Any error after removal restores the marker before this exclusive lock exits.
  rm -- "${mutation_file:?canonical mutation path required}" && sync "${dir:?canonical fence directory required}" || { cutover_failure "$1"; return 1; }
  if ! systemctl enable --now aboutme-crowdsec-retention.timer aboutme-crowdsec-vacuum.timer >/dev/null 2>&1; then
    cutover_failure "$1"; return 1
  fi
  rm -f -- "$cutover_run/mode.json"
  if ! systemctl enable --now aboutme-watch.timer >/dev/null 2>&1 || ! cutover_internal_lease; then
    cutover_failure "$1"; return 1
  fi
  # This is the watchdog's fixed retention tick under the held canonical lock.
  # Full scheduled watch jobs retain their shared guard before all checks.
  if ! jq 'del(.operation_id,.operation_kind,.operation_tag,.operation_started_at,.operation_checked_at,.crowdsec_cutover_started)' <<<"${doc:?owning fence document required}" | store; then
    # The durable release may have succeeded despite a lost response. Failure
    # recovery never recreates an absent owner; the caller must inspect it.
    cutover_failure "$1" || true
    return 1
  fi
  rm -f -- "$cutover_run/"*.json
}
