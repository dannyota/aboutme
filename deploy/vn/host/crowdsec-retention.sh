#!/usr/bin/env bash
# shellcheck source-path=SCRIPTDIR
# Fixed-schema retention and stopped-consumer reconciliation.
# See docs/design/crowdsec-http-privacy.md, Live retention and failure.

retention_psql() {
  timeout 12 env -i PATH=/usr/bin:/bin PGPASSFILE=/dev/null PGSSLMODE=disable \
    /usr/lib/postgresql/18/bin/psql -X -qAt -w -v ON_ERROR_STOP=1 \
    -v pass_time="$retention_time" -h /run/postgresql -U crowdsec -d crowdsec 2>/dev/null
}

retention_gate() {
  /usr/local/lib/aboutme/crowdsec-db.sh preflight-ready <"$retention_work/epoch" >/dev/null 2>&1
}

retention_sql() {
  cat <<'SQL'
SET statement_timeout = '10s';
SET lock_timeout = '2s';
SET jit = off;
SET TIME ZONE 'UTC';
CREATE TEMP TABLE bounds AS SELECT :'pass_time'::timestamptz AS at,
  :'pass_time'::timestamptz - interval '23 hours' AS cutoff;
CREATE TEMP VIEW stale_alerts AS
SELECT a.id FROM public.alerts a, bounds b
WHERE least(a.created_at,a.updated_at,a.started_at,a.stopped_at) <= b.cutoff
OR EXISTS (SELECT FROM public.events e WHERE e.alert_events = a.id
  AND least(e.created_at,e.updated_at,e.time) <= b.cutoff)
OR EXISTS (SELECT FROM public.meta m WHERE m.alert_metas = a.id
  AND least(m.created_at,m.updated_at) <= b.cutoff);
CREATE TEMP VIEW candidates AS
SELECT 'a'::text AS kind, a.id, 0::bigint AS alert_id, a.xmin::text AS version
FROM public.alerts a JOIN stale_alerts s ON s.id=a.id
UNION ALL
SELECT 'd', d.id, d.alert_decisions, d.xmin::text FROM public.decisions d, bounds b
WHERE least(d.created_at,d.updated_at) <= b.cutoff OR d.until <= b.at
OR EXISTS (SELECT FROM stale_alerts a WHERE a.id=d.alert_decisions)
UNION ALL
SELECT 'e', e.id, 0, e.xmin::text FROM public.events e WHERE e.alert_events IS NULL
UNION ALL
SELECT 'm', m.id, 0, m.xmin::text FROM public.meta m WHERE m.alert_metas IS NULL;
SQL
}

retention_select_sql() {
  cat <<'SQL'
WITH anchors AS (
  SELECT c.*, 1 + CASE WHEN c.kind='a' THEN
    (SELECT count(*) FROM public.decisions d WHERE d.alert_decisions=c.id)
    ELSE 0 END AS cost
  FROM candidates c WHERE c.kind<>'d'
    OR NOT EXISTS (SELECT FROM stale_alerts a WHERE a.id=c.alert_id)
), budget AS (
  SELECT *, sum(cost) OVER (ORDER BY kind,id) AS used FROM anchors
), selected AS (
  SELECT kind,id,alert_id,version FROM budget WHERE used<=1000
  UNION ALL
  SELECT c.kind,c.id,c.alert_id,c.version FROM candidates c
  WHERE c.kind='d' AND EXISTS
    (SELECT FROM budget b WHERE b.kind='a' AND b.id=c.alert_id AND b.used<=1000)
)
SELECT kind,id,alert_id,version FROM selected ORDER BY kind,id LIMIT 1000;
SQL
}

retention_validate() {
  retention_gate || return 1
  { retention_sql; cat <<'SQL'
DO $valid$
BEGIN
  IF EXISTS (SELECT FROM public.alerts WHERE created_at IS NULL OR updated_at IS NULL
      OR started_at IS NULL OR stopped_at IS NULL OR NOT isfinite(created_at)
      OR NOT isfinite(updated_at) OR NOT isfinite(started_at) OR NOT isfinite(stopped_at))
    OR EXISTS (SELECT FROM public.decisions WHERE created_at IS NULL OR updated_at IS NULL
      OR until IS NULL OR NOT isfinite(created_at) OR NOT isfinite(updated_at)
      OR NOT isfinite(until) OR until <= created_at OR until - created_at > interval '22 hours'
      OR alert_decisions IS NULL OR NOT EXISTS (SELECT FROM public.alerts WHERE id = alert_decisions))
    OR EXISTS (SELECT FROM public.events WHERE created_at IS NULL OR updated_at IS NULL
      OR time IS NULL OR NOT isfinite(created_at) OR NOT isfinite(updated_at) OR NOT isfinite(time)
      OR (alert_events IS NOT NULL AND NOT EXISTS (SELECT FROM public.alerts WHERE id = alert_events)))
    OR EXISTS (SELECT FROM public.meta WHERE created_at IS NULL OR updated_at IS NULL
      OR NOT isfinite(created_at) OR NOT isfinite(updated_at)
      OR (alert_metas IS NOT NULL AND NOT EXISTS (SELECT FROM public.alerts WHERE id = alert_metas)))
  THEN RAISE EXCEPTION 'invalid_state'; END IF;
END
$valid$;
SQL
  } | retention_psql >/dev/null
}

retention_stopped() {
  local unit state group container status
  for unit in "${retention_consumers[@]}"; do
    state=$(systemctl show --all --property=ActiveState --property=Job \
      --property=MainPID --property=ControlPID --property=ControlGroup "$unit" 2>/dev/null) || return 1
    [[ $(wc -l <<<"$state") == 5 ]] || return 1
    grep -qxE 'ActiveState=(inactive|failed)' <<<"$state" &&
      grep -qxF MainPID=0 <<<"$state" &&
      grep -qxF ControlPID=0 <<<"$state" || return 1
    if [[ $mode != startup ]]; then grep -qxF 'Job=' <<<"$state" || return 1; fi
    group=$(sed -n 's/^ControlGroup=//p' <<<"$state")
    [[ -z $group ]] && continue
    [[ $group == /system.slice/*.service && $group != *..* ]] || return 1
    if [[ -e /sys/fs/cgroup$group/cgroup.events ]]; then
      grep -qxF 'populated 0' "/sys/fs/cgroup$group/cgroup.events" || return 1
    fi
  done
  for container in aboutme-caddy aboutme-maintenance; do
    status=0
    podman container exists "$container" >/dev/null 2>&1 || status=$?
    case $status in
      0) [[ $(podman inspect --format '{{.State.Running}} {{.State.Pid}}' "$container" 2>/dev/null) == 'false 0' ]] || return 1 ;;
      1) ;;
      *) return 1 ;;
    esac
  done
}

retention_stop() {
  timeout 45 systemctl stop "${retention_consumers[@]}" >/dev/null 2>&1 || true
  retention_stopped && return 0
  timeout 5 systemctl kill --kill-whom=all --signal=SIGKILL \
    "${retention_consumers[@]}" >/dev/null 2>&1 || return 1
  timeout 5 systemctl stop "${retention_consumers[@]}" >/dev/null 2>&1 || return 1
  retention_stopped
}

retention_clear_firewall() {
  local family table chain set presence
  for family in ip ip6; do
    if [[ $family == ip ]]; then
      table=crowdsec chain=crowdsec-chain set=crowdsec-blacklists
    else
      table=crowdsec6 chain=crowdsec6-chain set=crowdsec6-blacklists
    fi
    timeout 2 nft -j list tables >"$retention_work/tables" 2>/dev/null || return 1
    presence=$(python3 -I - "$retention_work/tables" "$family" "$table" <<'PY'
import json,sys
data=json.load(open(sys.argv[1],encoding='utf-8'))['nftables']
assert isinstance(data,list)
tables=set()
for item in data:
    assert isinstance(item,dict) and len(item)==1
    if 'metainfo' in item:
        assert isinstance(item['metainfo'],dict)
        continue
    assert set(item)=={'table'} and isinstance(item['table'],dict)
    entry=item['table']
    assert entry['family'] in ('ip','ip6','inet','arp','bridge','netdev')
    assert isinstance(entry['name'],str) and 0<len(entry['name'])<=255
    identity=(entry['family'],entry['name'])
    assert identity not in tables
    tables.add(identity)
print('present' if (sys.argv[2],sys.argv[3]) in tables else 'absent')
PY
) || return 1
    case $presence in absent) continue ;; present) ;; *) return 1 ;; esac
    timeout 2 nft -j list table "$family" "$table" >"$retention_work/table" 2>/dev/null || return 1
    python3 -I - "$retention_work/table" "$family" "$table" "$chain" "$set" <<'PY' || return 1
import json,sys
path,family,table,chain,set_name=sys.argv[1:]
data=json.load(open(path,encoding='utf-8'))['nftables']
seen=[]
for item in data:
    assert len(item)==1
    kind,value=next(iter(item.items()))
    if kind=='metainfo': continue
    assert kind in ('table','chain','set','rule','element')
    allowed={
        'table':{'family','name','handle','flags'},
        'chain':{'family','table','name','handle','type','hook','prio','policy'},
        'set':{'family','table','name','handle','type','flags','elem'},
        'rule':{'family','table','chain','handle','expr'},
        'element':{'family','table','name','elem'},
    }
    assert set(value)<=allowed[kind]
    assert value['family']==family
    if kind=='table': assert value['name']==table and value.get('flags',[])==[]
    else:
        assert value['table']==table
        if kind=='chain':
            assert value['name']==chain and value['type']=='filter'
            assert value['hook']=='input' and value['prio']==-10 and value['policy']=='accept'
        if kind in ('set','element'): assert value['name']==set_name
        if kind=='set':
            assert value['type']==('ipv4_addr' if family=='ip' else 'ipv6_addr')
            assert value['flags']==['timeout']
        if kind=='rule':
            assert value['chain']==chain
            assert value['expr']==[
                {'match':{'op':'==','left':{'payload':{'protocol':family,'field':'saddr'}},'right':'@'+set_name}},
                {'drop':None}]
    seen.append(kind)
assert seen.count('table')==seen.count('chain')==seen.count('set')==seen.count('rule')==1
PY
    timeout 2 nft delete table "$family" "$table" >/dev/null 2>&1 || return 1
  done
}

retention_firewall_init() {
  timeout 2 nft -f - >/dev/null 2>&1 <<'NFT'
table ip crowdsec {
  set crowdsec-blacklists { type ipv4_addr; flags timeout; }
  chain crowdsec-chain { type filter hook input priority -10; policy accept; ip saddr @crowdsec-blacklists drop; }
}
table ip6 crowdsec6 {
  set crowdsec6-blacklists { type ipv6_addr; flags timeout; }
  chain crowdsec6-chain { type filter hook input priority -10; policy accept; ip6 saddr @crowdsec6-blacklists drop; }
}
NFT
}

retention_firewall_rebuilt() {
  local end=$((SECONDS + 20))
  while ((SECONDS < end)); do
    retention_gate || return 1
    retention_psql >"$retention_work/current-ssh" <<'SQL' || return 1
SET statement_timeout = '10s';
SET lock_timeout = '2s';
SELECT coalesce(json_agg(DISTINCT value), '[]'::json) FROM public.decisions
WHERE until > now() AND NOT simulated AND type='ban' AND scenario LIKE '%ssh%';
SQL
    timeout 2 nft -j list set ip crowdsec crowdsec-blacklists >"$retention_work/ipv4" 2>/dev/null || return 1
    timeout 2 nft -j list set ip6 crowdsec6 crowdsec6-blacklists >"$retention_work/ipv6" 2>/dev/null || return 1
    if python3 -I - "$retention_work" <<'PY'
import ipaddress,json,pathlib,sys
root=pathlib.Path(sys.argv[1])
def address(value):
    item=ipaddress.ip_address(value)
    return str(item.ipv4_mapped if getattr(item,'ipv4_mapped',None) else item)
expected={address(value) for value in json.loads((root/'current-ssh').read_text())}
actual=set()
for path in ('ipv4','ipv6'):
    data=json.loads((root/path).read_text())['nftables']
    sets=[item['set'] for item in data if 'set' in item]
    assert len(sets)==1
    for element in sets[0].get('elem',[]):
        if isinstance(element,dict): element=element['elem']['val']
        actual.add(address(element))
assert actual==expected
PY
    then return 0; fi
    sleep 1
  done
  return 1
}

retention_reconcile() {
  local acknowledgement_seconds=$1
  ((acknowledgement_seconds > 0 && acknowledgement_seconds <= 20)) || return 1
  # Pinned consumers have no applied-deletion acknowledgement. The bounded
  # interface takes the stopped-consumer path immediately.
  if [[ $retention_quiescent == no ]]; then
    retention_stop && retention_clear_firewall || return 1
    retention_quiescent=yes
  fi
}

retention_delete() {
  retention_gate || return 1
  {
    retention_sql
    printf '%s\n' 'CREATE TEMP TABLE selected (kind text, id bigint, alert_id bigint, version text, PRIMARY KEY(kind,id));' \
      'COPY selected FROM STDIN WITH (FORMAT text, DELIMITER '\''|'\'');'
    cat "$retention_work/candidates"
    printf '\\.\n'
    cat <<'SQL'
BEGIN;
LOCK TABLE public.alerts, public.decisions, public.events, public.meta IN SHARE ROW EXCLUSIVE MODE;
DO $check$
BEGIN
  IF EXISTS (SELECT FROM selected s WHERE NOT EXISTS
    (SELECT FROM candidates c WHERE c.kind=s.kind AND c.id=s.id
      AND c.alert_id=s.alert_id AND c.version=s.version))
    OR EXISTS (SELECT FROM public.decisions d JOIN selected a ON a.kind='a' AND a.id=d.alert_decisions
      WHERE NOT EXISTS (SELECT FROM selected s WHERE s.kind='d' AND s.id=d.id
        AND s.alert_id=d.alert_decisions AND s.version=d.xmin::text))
  THEN RAISE EXCEPTION 'changed_candidates'; END IF;
END
$check$;
DELETE FROM public.decisions WHERE id IN (SELECT id FROM selected WHERE kind='d');
DELETE FROM public.events WHERE id IN (SELECT id FROM selected WHERE kind='e');
DELETE FROM public.meta WHERE id IN (SELECT id FROM selected WHERE kind='m');
DELETE FROM public.alerts WHERE id IN (SELECT id FROM selected WHERE kind='a');
COMMIT;
SQL
  } | retention_psql >/dev/null
}

retention_trim_feed() {
  local active
  local feed=/run/aboutme/caddy-log/crowdsec
  [[ -d $feed && ! -L $feed && $(stat -c '%u:%g:%a' "$feed") == 10001:10001:700 &&
    $(findmnt -n -o FSTYPE -T "$feed") == tmpfs ]] || return 1
  for active in serving maintenance serving-bouncer maintenance-bouncer; do
    [[ -f $feed/$active.json && ! -L $feed/$active.json &&
      $(stat -c '%u:%g:%a:%h' "$feed/$active.json") == 10001:10001:600:1 ]] || return 1
    setpriv --reuid 10001 --regid 10001 --clear-groups \
      truncate -s 0 -- "$feed/$active.json" || return 1
  done
  if find "$feed" -maxdepth 1 -type l -regextype posix-extended \
    -regex '.*/(serving|maintenance)(-bouncer)?-[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}-[0-9]{2}-[0-9]{2}\.[0-9]{3}-(time|size)\.json' \
    -print -quit | grep -q .; then return 1; fi
  setpriv --reuid 10001 --regid 10001 --clear-groups \
    find "$feed" -maxdepth 1 -type f -regextype posix-extended \
    -regex '.*/(serving|maintenance)(-bouncer)?-[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}-[0-9]{2}-[0-9]{2}\.[0-9]{3}-(time|size)\.json' \
    -delete || return 1
  [[ -d /run/aboutme/crowdsec && ! -L /run/aboutme/crowdsec &&
    $(stat -c '%u:%g:%a' /run/aboutme/crowdsec) == 0:0:700 &&
    $(findmnt -n -o FSTYPE -T /run/aboutme/crowdsec) == tmpfs ]] || return 1
  python3 -I - <<'PY'
import os,re,stat
root='/run/aboutme/crowdsec'
active=re.compile(r'crowdsec(?:_api|-firewall-bouncer)?\.log')
roll=re.compile(r'crowdsec(?:_api|-firewall-bouncer)?-\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}\.\d{3}\.log')
directory=os.open(root,os.O_DIRECTORY|os.O_NOFOLLOW|os.O_RDONLY)
for name in os.listdir(directory):
    assert active.fullmatch(name) or roll.fullmatch(name)
    fd=os.open(name,os.O_WRONLY|os.O_NOFOLLOW|os.O_NONBLOCK,dir_fd=directory)
    entry=os.fstat(fd)
    assert stat.S_ISREG(entry.st_mode) and entry.st_uid==entry.st_gid==0 and entry.st_nlink==1
    assert not entry.st_mode & 0o022
    if active.fullmatch(name): os.ftruncate(fd,0)
    else: os.unlink(name,dir_fd=directory)
    os.close(fd)
os.close(directory)
PY
}

retention_prepare_feed() {
  local feed=/run/aboutme/caddy-log/crowdsec active
  [[ -d /run/aboutme/caddy-log && ! -L /run/aboutme/caddy-log &&
    $(findmnt -n -o FSTYPE -T /run/aboutme/caddy-log) == tmpfs ]] || return 1
  [[ ! -L /run/aboutme/crowdsec ]] || return 1
  if [[ ! -e /run/aboutme/crowdsec ]]; then
    install -d -o root -g root -m 0700 /run/aboutme/crowdsec
  fi
  if [[ ! -e $feed && ! -L $feed ]]; then
    install -d -o 10001 -g 10001 -m 0700 "$feed"
    for active in serving maintenance serving-bouncer maintenance-bouncer; do
      install -o 10001 -g 10001 -m 0600 /dev/null "$feed/$active.json"
    done
  fi
}

retention_verify() {
  retention_validate || return 1
  local result
  result=$({ retention_sql; cat <<'SQL'
SELECT NOT EXISTS (SELECT FROM candidates)
AND NOT EXISTS (SELECT FROM public.alerts, bounds WHERE least(created_at,updated_at,started_at,stopped_at) <= at - interval '23 hours 30 minutes')
AND NOT EXISTS (SELECT FROM public.decisions, bounds WHERE least(created_at,updated_at) <= at - interval '23 hours 30 minutes')
AND NOT EXISTS (SELECT FROM public.events, bounds WHERE least(created_at,updated_at,time) <= at - interval '23 hours 30 minutes')
AND NOT EXISTS (SELECT FROM public.meta, bounds WHERE least(created_at,updated_at) <= at - interval '23 hours 30 minutes');
SQL
  } | retention_psql) || return 1
  [[ $result == t ]] && retention_gate
}

retention_main() {
  set -euo pipefail
  umask 077
  [[ $EUID == 0 && $# == 1 && ( $1 == startup || $1 == runtime ) ]] || return 1
  local mode=$1 unit state candidates
  local retention_work retention_time retention_quiescent=no
  local -a retention_consumers=(aboutme-caddy.service aboutme-maintenance.service \
    crowdsec-firewall-bouncer.service crowdsec.service)
  local -a restart=()
  [[ -d /run/aboutme && ! -L /run/aboutme && $(stat -c '%u:%g:%a' /run/aboutme) == 0:0:755 ]] || return 1
  [[ ! -L /run/aboutme/crowdsec-retention.lock ]] || return 1
  exec {retention_lock}>/run/aboutme/crowdsec-retention.lock
  [[ $(stat -c '%u:%g:%a:%h' /run/aboutme/crowdsec-retention.lock) == 0:0:600:1 ]] || return 1
  flock -x -w 2 "$retention_lock" || return 1
  retention_work=$(mktemp -d /run/aboutme/crowdsec-retention.XXXXXX)
  # Capture the fixed-prefix temporary path before this local leaves scope.
  # shellcheck disable=SC2064
  trap "rm -rf -- '$retention_work'" EXIT
  : >"$retention_work/epoch"
  /usr/local/lib/aboutme/crowdsec-db.sh preflight-ready </dev/null \
    3>"$retention_work/epoch" >/dev/null 2>&1 || return 1
  [[ $(wc -c <"$retention_work/epoch") == 65 ]] &&
    grep -qxE '[0-9a-f]{64}' "$retention_work/epoch" || return 1
  [[ $(timedatectl show -p NTPSynchronized --value) == yes ]] || return 1
  while read -r device _; do
    [[ $device == Filename || $device == /dev/zram* ]] || return 1
  done </proc/swaps
  retention_time=$(date -u +%Y-%m-%dT%H:%M:%S.%NZ)
  if [[ $mode == startup ]]; then
    retention_stopped && retention_clear_firewall && retention_firewall_init || return 1
    retention_quiescent=yes
    retention_prepare_feed || return 1
  else
    /usr/local/lib/aboutme/crowdsec-retention-lease-check.sh || return 1
    for unit in "${retention_consumers[@]}"; do
      state=$(systemctl show --property=ActiveState --value "$unit") || return 1
      case $state in active) restart+=("$unit") ;; inactive | failed) ;; *) return 1 ;; esac
    done
  fi
  retention_validate && retention_trim_feed || return 1
  while :; do
    retention_gate || return 1
    { retention_sql; retention_select_sql; } |
      retention_psql >"$retention_work/candidates" || return 1
    python3 -I - "$retention_work/candidates" <<'PY' || return 1
import pathlib,re,sys
lines=pathlib.Path(sys.argv[1]).read_text().splitlines()
assert len(lines)<=1000
assert all(re.fullmatch(r'[adem]\|[1-9][0-9]{0,18}\|(?:0|[1-9][0-9]{0,18})\|[0-9]{1,10}',line) for line in lines)
for line in lines:
    kind,identity,parent,version=line.split('|')
    assert (kind=='d' and parent!='0') or (kind!='d' and parent=='0')
PY
    [[ -s $retention_work/candidates ]] || break
    candidates=$(grep -c '^d|' "$retention_work/candidates" || true)
    if ((candidates > 0)); then retention_reconcile 20 || return 1; fi
    retention_delete || return 1
  done
  retention_verify || return 1
  if [[ $mode == runtime && $retention_quiescent == yes ]]; then
    # Existing valid authorization permits rebuild. A lease that expires
    # during cleanup fails closed rather than authorizing an incomplete pass.
    /usr/local/lib/aboutme/crowdsec-retention-lease-check.sh || return 1
    retention_firewall_init || return 1
    local retention_restart_unit
    for retention_restart_unit in crowdsec.service crowdsec-firewall-bouncer.service aboutme-caddy.service aboutme-maintenance.service; do
      if [[ " ${restart[*]} " == *" $retention_restart_unit "* ]]; then
        if [[ $retention_restart_unit == aboutme-caddy.service || $retention_restart_unit == aboutme-maintenance.service ]]; then
          retention_firewall_rebuilt || return 1
        fi
        retention_gate || return 1
        timeout 10 systemctl start "$retention_restart_unit" >/dev/null 2>&1 || return 1
        systemctl is-active --quiet "$retention_restart_unit" || return 1
        if [[ $retention_restart_unit == crowdsec-firewall-bouncer.service ]]; then
          retention_firewall_rebuilt || return 1
        fi
      fi
    done
    retention_verify || return 1
  fi
  retention_gate || return 1
  install -o root -g root -m 0600 "$retention_work/epoch" "$retention_work/lease"
  mv -fT -- "$retention_work/lease" /run/aboutme/crowdsec-retention-ok
  rm -f /run/aboutme/crowdsec-retention-failed
}

if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
  set -euo pipefail
  [[ $EUID == 0 && $# == 1 && ( $1 == startup || $1 == runtime ) ]] || exit 2
  deadline=60
  [[ $1 != startup ]] || deadline=300
  if ! timeout --signal=TERM --kill-after=2s "$deadline" \
    env -i PATH=/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin /usr/bin/bash -c \
    'source /usr/local/lib/aboutme/crowdsec-retention.sh; retention_main "$1"' retention "$1" \
    >/dev/null 2>&1; then
    /usr/local/lib/aboutme/crowdsec-retention-fail.sh >/dev/null 2>&1 || true
    exit 1
  fi
fi
