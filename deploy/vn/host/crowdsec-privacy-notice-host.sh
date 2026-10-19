#!/usr/bin/env bash
# Root authority remains outside restored database state. See the notice design.
readonly LEGACY_RELEASE_TAG='v0.8.6'
readonly LEGACY_RELEASE_SHA256='90c0fc03a9d9b5e48f28b7679cc58b27f22c9cb6e04d06ed4a80355e301c4cd8'
readonly LEGACY_SERVER_IMAGE='ghcr.io/dannyota/aboutme-server@sha256:7aafadf9a3233e3ba39b0a8b1c87efd45a76cccae7196528b94cd867f5cd5190'
readonly LEGACY_WEB_IMAGE='ghcr.io/dannyota/aboutme-web@sha256:6c6b12c7cb789ef3e5c51b10d72a30acc587a497816550e8fcc94b3da698d017'
readonly LEGACY_FENCE_SHA256='ff47480f3397119d8fcb1c2eb6a2344c8f4ed1e416b039a9875fb7879546b0bb'
readonly LEGACY_DEPLOY_HOST_SHA256='314057b0233ed415dbdb1c200c6e4f42aaf8dac5827f972b24b8c412d9c3d31f'
readonly LEGACY_DEPLOY_SAFETY_SHA256='fecc8b21e17a300579d13708e74fdb9e4c09b0808b1939ef27d2bc225feafd9a'

notice_invalid() { printf 'invalid_state\n' >&2; return 1; }
notice_overrides_absent() {
  local name
  for name in FENCE_DIR ABOUTME_WORKLOAD_LOCK NOTICE_AUTHORITY_DIR NOTICE_OPERATION_ID \
    RECOVERY_WAIT_SECONDS DB_NAME SMOKE_TABLE PG_TARGET NOTICE_COMMAND NOTICE_CONTROLLER; do
    [[ ! -v $name ]] || return 1
  done
}
notice_mount_identity() {
  python3 -I - <<'PY'
import json,os,re,stat,subprocess,sys
try:
    labels=subprocess.run(['blkid','-t','LABEL=aboutme-data','-o','device'],check=True,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL).stdout.decode('ascii').splitlines()
    assert len(labels)==1 and labels[0].startswith('/dev/')
    block=os.stat(labels[0]); assert stat.S_ISBLK(block.st_mode)
    device=(os.major(block.st_rdev),os.minor(block.st_rdev))
    def safe(path):
        current='/'
        for part in path.strip('/').split('/'):
            current=os.path.join(current,part); s=os.lstat(current)
            assert stat.S_ISDIR(s.st_mode) and s.st_uid==0 and s.st_gid==0 and s.st_mode&0o022==0
        return s
    data=safe('/srv/data'); source=safe('/srv/data/aboutme'); bound=safe('/var/lib/aboutme')
    mounts=[]
    def unescape(text): return re.sub(r'\\([0-7]{3})',lambda m:chr(int(m[1],8)),text)
    for line in open('/proc/self/mountinfo',encoding='ascii'):
        left,right=line.rstrip('\n').split(' - ',1); a=left.split(); b=right.split()
        mounts.append((int(a[0]),tuple(map(int,a[2].split(':'))),unescape(a[3]),unescape(a[4]),b[0],unescape(b[1])))
    dm=[m for m in mounts if m[3]=='/srv/data']; bm=[m for m in mounts if m[3]=='/var/lib/aboutme']
    assert len(dm)==len(bm)==1
    d,b=dm[0],bm[0]
    assert d[1]==device and d[2]=='/' and d[4]=='ext4' and os.path.realpath(d[5])==os.path.realpath(labels[0])
    assert b[1]==device and b[2]=='/aboutme' and b[4]=='ext4'
    assert source.st_dev==bound.st_dev and source.st_ino==bound.st_ino
    assert (os.major(source.st_dev),os.minor(source.st_dev))==device
    assert not any(m[3].startswith(('/srv/data/aboutme/','/var/lib/aboutme/')) for m in mounts)
    print(json.dumps(dict(data_mount_id=d[0],bind_mount_id=b[0],device_major=device[0],device_minor=device[1],data_root_inode=data.st_ino,aboutme_inode=bound.st_ino),separators=(',',':')))
except Exception:
    sys.exit(1)
PY
}
notice_recheck_mount() {
  local current
  current=$(notice_mount_identity) || return 1
  [[ $current == "$notice_identity" ]]
}
notice_safe_file() {
  local path=$1 mode=${2:-444}
  [[ -f $path && ! -L $path && $(stat -c '%u:%g:%a:%h' "$path") == "0:0:$mode:1" ]]
}
notice_absent() { [[ ! -e $1 && ! -L $1 ]]; }
notice_authority_check() {
  local name
  [[ -d /var/lib/aboutme/privacy-notice && ! -L /var/lib/aboutme/privacy-notice && \
    $(stat -c '%u:%g:%a' /var/lib/aboutme/privacy-notice) == 0:0:755 ]] || return 1
  for name in epoch send.json activation.json restore-reconciliation capture-in-progress; do
    notice_absent "/var/lib/aboutme/privacy-notice/$name" || notice_safe_file "/var/lib/aboutme/privacy-notice/$name" || return 1
  done
  notice_safe_file /var/lib/aboutme/privacy-notice/epoch || return 1
  [[ $(wc -c </var/lib/aboutme/privacy-notice/epoch) == 37 ]] || return 1
  grep -qxE '[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}' /var/lib/aboutme/privacy-notice/epoch
}
notice_replace() {
  local name=$1 tmp
  notice_recheck_mount || return 1
  tmp=$(mktemp "/var/lib/aboutme/privacy-notice/.$name.XXXXXX") || return 1
  cat >"$tmp" && chown root:root "$tmp" && chmod 0444 "$tmp" && sync "$tmp" && \
    mv -f "$tmp" "/var/lib/aboutme/privacy-notice/$name" && sync /var/lib/aboutme/privacy-notice
}
notice_revoke() {
  local name=$1
  notice_recheck_mount || return 1
  if ! notice_absent "/var/lib/aboutme/privacy-notice/$name"; then
    notice_safe_file "/var/lib/aboutme/privacy-notice/$name" || return 1
    rm -f "/var/lib/aboutme/privacy-notice/$name" || return 1
  fi
  sync /var/lib/aboutme/privacy-notice
}
notice_epoch_rotate() {
  local previous=${1:-} next
  next=$(python3 -I -c 'import uuid; print(uuid.uuid4())') || return 1
  [[ $next != "$previous" ]] || return 1
  printf '%s\n' "$next" | notice_replace epoch || return 1
  notice_epoch=$next
}
notice_unit_stopped() {
  local result
  result=$(systemctl show --all --property=ActiveState --property=Job "$1") || return 1
  [[ $(wc -l <<<"$result") == 2 ]] && grep -qxE 'ActiveState=(inactive|failed)' <<<"$result" && grep -qxF 'Job=' <<<"$result"
}
notice_app_absent() {
  notice_absent /var/lib/aboutme/release.json && notice_absent /etc/containers/systemd/aboutme-server.container || return 1
  notice_unit_stopped aboutme-server || return 1
  local rc=0
  pgrep -f '([/]server( |$)|[a]boutme-server|[/]pgbackrest.*restore)' >/dev/null || rc=$?
  [[ $rc == 1 ]]
}
notice_quiesce() {
  # shellcheck source=../scripts/deploy-safety.sh
  source /usr/local/lib/aboutme/deploy-safety.sh
  aboutme_stop_jobs 60 || return 1
  systemctl start aboutme-maintenance && systemctl stop aboutme-server || return 1
  notice_unit_stopped aboutme-server && aboutme_assert_cutover_quiescent
}
notice_installed_admission() {
  local record tag number image
  notice_safe_file /var/lib/aboutme/release.json 644 || return 1
  record=$(jq -ce 'select(keys==["caddy_image","maintenance_image","maintenance_number","maintenance_tag","release_number","release_tag","server_image","web_image"])' /var/lib/aboutme/release.json) || return 1
  tag=$(jq -r .release_tag <<<"$record")
  [[ $tag =~ ^v(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})$ ]] || return 1
  number=$((10#${BASH_REMATCH[1]} * 1000000 + 10#${BASH_REMATCH[2]} * 1000 + 10#${BASH_REMATCH[3]}))
  [[ $number == "$(jq -r .release_number <<<"$record")" ]] || return 1
  image=$(jq -r .server_image <<<"$record")
  [[ $image =~ ^ghcr\.io/dannyota/aboutme-server@sha256:[0-9a-f]{64}$ ]] || return 1
  notice_safe_file /etc/containers/systemd/aboutme-server.container 644 &&
    grep -qxF "Image=$image" /etc/containers/systemd/aboutme-server.container || return 1
  printf '%s\n' "$tag"
}
notice_restore_host_check() {
  if notice_absent /var/lib/aboutme/release.json; then notice_app_absent; return; fi
  [[ $(notice_installed_admission) == "$notice_admission" ]] || return 1
  # Only a separately reviewed fixed full-host controller may reach this branch.
  # shellcheck source=../scripts/deploy-safety.sh
  source /usr/local/lib/aboutme/deploy-safety.sh
  aboutme_assert_cutover_quiescent || return 1
  local unit
  for unit in crowdsec crowdsec-firewall-bouncer aboutme-crowdsec-retention.timer aboutme-migrate aboutme-db-setup; do
    notice_unit_stopped "$unit" || return 1
  done
}
notice_fence() { /usr/local/lib/aboutme/fence.sh "$@"; }
notice_read_owner() {
  local state
  notice_recheck_mount || return 1
  state=$(notice_fence read 2>/dev/null) || return 1
  [[ $(jq -r '.operation_id // empty' <<<"$state") == "$notice_operation" && \
    $(jq -r '.operation_kind // empty' <<<"$state") == notice_restore && \
    $(jq -r '.operation_tag // empty' <<<"$state") == "$notice_admission" ]]
}
notice_restore_begin() {
  (($# == 0)) || { notice_invalid; return 1; }
  [[ $(id -u) == 0 ]] && notice_overrides_absent || return 1
  notice_identity=$(notice_mount_identity) || return 1
  notice_authority_check || return 1
  local state
  state=$(notice_fence read 2>/dev/null) || return 1
  [[ $(jq -r '.notice_authority.state // empty' <<<"$state") == adopted && \
    $(jq -r '.operation_id // empty' <<<"$state") == '' ]] || return 1
  if notice_absent /var/lib/aboutme/release.json; then
    notice_app_absent || return 1
    notice_admission=$(jq -er .minimum_tag <<<"$state") || return 1
  else notice_admission=$(notice_installed_admission) || return 1; fi
  notice_operation=$(notice_fence lock notice_restore "$notice_admission" 2>/dev/null) || return 1
  [[ $notice_operation =~ ^[A-Za-z0-9_-]{42}[AEIMQUYcgkosw048]$ ]] && notice_read_owner
}
notice_restore_pre_restore() {
  (($# == 0)) || { notice_invalid; return 1; }
  if ! notice_absent /var/lib/aboutme/release.json; then notice_quiesce || return 1; fi
  notice_read_owner && notice_restore_host_check && notice_authority_check || return 1
  local previous
  previous=$(cat /var/lib/aboutme/privacy-notice/epoch) || return 1
  notice_replace restore-reconciliation </dev/null && notice_epoch_rotate "$previous" && \
    notice_revoke send.json && notice_revoke activation.json || return 1
  notice_restore_denial
}
notice_restore_denial() {
  notice_read_owner && notice_restore_host_check && notice_authority_check || return 1
  [[ $(cat /var/lib/aboutme/privacy-notice/epoch) == "$notice_epoch" && \
    -f /var/lib/aboutme/privacy-notice/restore-reconciliation && ! -s /var/lib/aboutme/privacy-notice/restore-reconciliation ]] || return 1
  notice_absent /var/lib/aboutme/privacy-notice/send.json && notice_absent /var/lib/aboutme/privacy-notice/activation.json
}
notice_restore_postcheck() {
  (($# == 0)) || { notice_invalid; return 1; }
  notice_restore_denial
}
notice_restore_finish() {
  (($# == 0)) || { notice_invalid; return 1; }
  [[ ${notice_completed:-} == yes && ${notice_helper_joined:-} == yes ]] || return 1
  notice_restore_denial && notice_fence notice-restore-release "$notice_operation" >/dev/null 2>&1
}
notice_wire() {
  python3 -I - "$@" 5<&0 <<'PY'
import datetime,json,os,re,sys
mode=sys.argv[1]
def integer(v): assert type(v) is int and 0<=v<=9223372036854775807
def pairs(rows):
    out={}
    for k,v in rows:
        assert k not in out; out[k]=v
    return out
def number(v):
    assert re.fullmatch(r'0|[1-9][0-9]*',v); return int(v)
def invalid(v): raise ValueError()
def obj(v,keys): assert type(v) is dict and set(v)==set(keys.split())
def epoch(v): assert type(v) is str and re.fullmatch(r'[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}',v)
def tag(v): assert type(v) is str and re.fullmatch(r'v(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})',v)
def stamp(v,seconds=False):
    assert type(v) is str and re.fullmatch(r'\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d'+('' if seconds else r'\.\d{6}')+'Z',v)
    return datetime.datetime.strptime(v,'%Y-%m-%dT%H:%M:%SZ' if seconds else '%Y-%m-%dT%H:%M:%S.%fZ')
try:
    bound=1024 if mode=='context' else 256 if mode=='completion' else 4096
    data=os.fdopen(5,'rb').read(bound+2); assert 0<len(data)<=bound and data.endswith(b'\n') and not data.startswith(b'\xef\xbb\xbf')
    text=data.decode('utf-8'); v=json.loads(text,object_pairs_hook=pairs,parse_int=number,parse_float=invalid,parse_constant=invalid)
    if mode in ('context','completion'): assert '\n' not in text[:-1] and '\r' not in text
    json.dumps(v,ensure_ascii=False).encode('utf-8')
    if mode=='context':
        obj(v,'version operation_id admission_tag mode target mount_identity'); assert v['version']==1 and type(v['version']) is int
        assert re.fullmatch(r'[A-Za-z0-9_-]{42}[AEIMQUYcgkosw048]',v['operation_id']); tag(v['admission_tag'])
        assert v['mode'] in ('latest','time')
        if v['mode']=='latest': assert v['target'] is None
        else: stamp(v['target'],True)
        obj(v['mount_identity'],'data_mount_id bind_mount_id device_major device_minor data_root_inode aboutme_inode')
        for n in v['mount_identity'].values(): integer(n)
    elif mode=='completion':
        obj(v,'version operation_id epoch'); assert v['version']==1 and type(v['version']) is int
        assert re.fullmatch(r'[A-Za-z0-9_-]{42}[AEIMQUYcgkosw048]',v['operation_id']); epoch(v['epoch'])
    else:
        common='schema_version campaign_id content_sha256 cutoff_at expected_count'
        assert v['schema_version']==1 and type(v['schema_version']) is int
        assert v['campaign_id']=='crowdsec-postgres-paths-v1' and v['content_sha256']=='bb128b39501f09db59915f3f75759572a49349ec78fe17ad48b6ba40d500ffdc'
        integer(v['expected_count']); cutoff=stamp(v['cutoff_at'])
        now=stamp(sys.argv[3])
        assert cutoff<=now
        if mode in ('send','activation'):
            obj(v,common+' epoch '+('issued_at' if mode=='send' else 'accepted_count deleted_count activated_at')); epoch(v['epoch'])
            assert stamp(v['issued_at'] if mode=='send' else v['activated_at'])<=now
            if mode=='activation':
                integer(v['accepted_count']); integer(v['deleted_count']); assert v['accepted_count']+v['deleted_count']==v['expected_count']
        elif mode=='report':
            obj(v,common+' result state actual_count counts failure_counts recovery_count recovery_counts activation_count sealed_at first_activated_at activated_at aborted_at last_accepted_at')
            for k in ('actual_count','recovery_count','activation_count'): integer(v[k])
            obj(v['counts'],'unqueued queued accepted deleted failed cancelled')
            for k in ('failure_counts','recovery_counts'): obj(v[k],'expired attempts_exhausted provider_rejected payload_invalid job_missing delivery_failed')
            for group in ('counts','failure_counts','recovery_counts'):
                for n in v[group].values(): integer(n)
                integer(sum(v[group].values()))
            c=v['counts']; assert sum(c.values())==v['actual_count'] and sum(v['failure_counts'].values())==c['failed'] and sum(v['recovery_counts'].values())==v['recovery_count']
            state=v['state']; assert state in ('capturing','sealed','activated','aborted','retired')
            assert v['actual_count']<=v['expected_count'] if state in ('capturing','retired') else v['actual_count']==v['expected_count']
            for k in ('sealed_at','first_activated_at','activated_at','aborted_at','last_accepted_at'):
                if v[k] is not None: assert cutoff<=stamp(v[k])<=now
            assert (v['sealed_at'] is None)==(state=='capturing')
            assert (v['first_activated_at'] is None)==(v['activation_count']==0)
            assert (v['activated_at'] is not None)==(state=='activated')
            assert (v['last_accepted_at'] is None)==(c['accepted']==0)
            if v['first_activated_at'] is not None: assert stamp(v['first_activated_at'])>=stamp(v['sealed_at'])
            if state=='activated': assert stamp(v['activated_at'])>=max(stamp(v['sealed_at']),stamp(v['first_activated_at']),stamp(v['last_accepted_at']) if c['accepted'] else cutoff)
            complete=c['accepted']+c['deleted']==v['expected_count']; result=v['result']; code=int(sys.argv[2])
            allowed={'sealed':code==0 and state=='sealed','pending':code==2 and state in ('capturing','sealed'),'queue_full':code==2 and state=='sealed','ready':code==0 and state in ('sealed','activated') and complete,'activated':code==0 and state=='activated' and complete,'blocked':code==1 and (c['failed']>0 or state in ('aborted','retired'))}
            assert allowed.get(result,False)
        else: raise ValueError()
    print(json.dumps(v,separators=(',',':'),ensure_ascii=False))
except Exception:
    sys.exit(1)
PY
}

notice_bootstrap() {
  local state evidence operation existing kind
  notice_identity=$(notice_mount_identity) || return 1
  state=$(notice_fence read 2>/dev/null) || {
    notice_absent /var/lib/aboutme/fence.json || return 1
    notice_fence init >/dev/null 2>&1 || return 1
    state=$(notice_fence read 2>/dev/null) || return 1
  }
  evidence=$(jq -r '.notice_authority.state // empty' <<<"$state")
  if [[ $evidence == adopted ]]; then notice_authority_check; return; fi
  existing=$(jq -r '.operation_id // empty' <<<"$state")
  if [[ $evidence == initializing ]]; then
    operation=$(jq -r .notice_authority.operation_id <<<"$state")
    [[ $existing == "$operation" ]] || return 1
    kind=$(jq -r .operation_kind <<<"$state")
    [[ $kind == notice_restore || $kind == cutover ]] || return 1
  else
    [[ $evidence == '' && $existing == '' ]] && notice_absent /var/lib/aboutme/privacy-notice && notice_app_absent || return 1
    operation=$(notice_fence lock notice_restore "$(jq -r .minimum_tag <<<"$state")" 2>/dev/null) || return 1
  fi
  if [[ ! -v notice_boot_fd ]]; then
    exec {notice_boot_fd}>>/run/aboutme/workload.lock || return 1
    flock -x -w 300 "$notice_boot_fd" || return 1
  fi
  notice_recheck_mount || return 1
  notice_fence notice-authority "$operation" initializing >/dev/null 2>&1 || return 1
  if notice_absent /var/lib/aboutme/privacy-notice; then
    install -d -o root -g root -m 0755 /var/lib/aboutme/privacy-notice && sync /var/lib/aboutme || return 1
  fi
  [[ $(stat -c '%u:%g:%a' /var/lib/aboutme/privacy-notice) == 0:0:755 && ! -L /var/lib/aboutme/privacy-notice ]] || return 1
  if notice_absent /var/lib/aboutme/privacy-notice/restore-reconciliation; then notice_replace restore-reconciliation </dev/null || return 1; fi
  if notice_absent /var/lib/aboutme/privacy-notice/epoch; then notice_epoch_rotate || return 1; fi
  notice_authority_check && notice_absent /var/lib/aboutme/privacy-notice/send.json && \
    notice_absent /var/lib/aboutme/privacy-notice/activation.json && notice_absent /var/lib/aboutme/privacy-notice/capture-in-progress && sync /var/lib/aboutme/privacy-notice || return 1
  notice_fence notice-authority "$operation" adopted >/dev/null 2>&1 || return 1
  exec {notice_boot_fd}>&-
  if [[ ${kind:-notice_restore} == cutover ]]; then
    notice_fence mutation-clear "$operation" quiesce >/dev/null 2>&1 && notice_fence release "$operation" >/dev/null 2>&1
  else notice_fence notice-restore-release "$operation" >/dev/null 2>&1; fi
}
notice_adopt() {
  local state tag record base name installed metadata resuming=no
  notice_identity=$(notice_mount_identity) || return 1
  exec {notice_bundle_fd}>>/srv/data/host-operations/.bundle.lock || return 1
  flock -x -w 30 "$notice_bundle_fd" || return 1
  notice_recheck_mount || return 1
  # The incoming reviewed helper reads intent even before its installed copy.
  base=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
  if [[ -f $base/../scripts/fence.sh ]]; then base=$(cd "$base/../scripts" && pwd); fi
  for name in fence deploy-host deploy-safety; do [[ -f $base/$name.sh && ! -L $base/$name.sh ]] || return 1; done
  state=$("$base/fence.sh" read 2>/dev/null) || return 1
  if [[ $(jq -r '.notice_authority.state // empty' <<<"$state") == initializing ]]; then
    notice_operation=$(jq -er .notice_authority.operation_id <<<"$state") || return 1
    [[ $(jq -r .operation_id <<<"$state") == "$notice_operation" && $(jq -r .operation_kind <<<"$state") == cutover ]] || return 1
    # Only this recorded adoption may repair an interrupted installed copy.
    for name in fence deploy-host deploy-safety; do
      installed=/usr/local/lib/aboutme/$name.sh
      notice_absent "$installed" && continue
      [[ -f $installed && ! -L $installed ]] || return 1
      metadata=$(stat -c '%u:%g:%a:%h' "$installed") || return 1
      [[ $metadata == 0:0:600:1 || $metadata == 0:0:755:1 ]] || return 1
    done
    resuming=yes
  else
    [[ $(jq -r '.notice_authority // empty' <<<"$state") == '' ]] && notice_absent /var/lib/aboutme/privacy-notice || return 1
  fi
  record=/var/lib/aboutme/release.json
  notice_safe_file "$record" 644 || return 1
  [[ $(sha256sum "$record" | cut -d' ' -f1) == "$LEGACY_RELEASE_SHA256" && \
    $(jq -r .release_tag "$record") == "$LEGACY_RELEASE_TAG" && \
    $(jq -r .server_image "$record") == "$LEGACY_SERVER_IMAGE" && $(jq -r .web_image "$record") == "$LEGACY_WEB_IMAGE" ]] || return 1
  tag=$(jq -r .release_tag "$record")
  if [[ $resuming == no ]]; then
    for name in fence deploy-host deploy-safety; do notice_safe_file "/usr/local/lib/aboutme/$name.sh" 755 || return 1; done
    [[ $(sha256sum /usr/local/lib/aboutme/fence.sh | cut -d' ' -f1) == "$LEGACY_FENCE_SHA256" && \
      $(sha256sum /usr/local/lib/aboutme/deploy-host.sh | cut -d' ' -f1) == "$LEGACY_DEPLOY_HOST_SHA256" && \
      $(sha256sum /usr/local/lib/aboutme/deploy-safety.sh | cut -d' ' -f1) == "$LEGACY_DEPLOY_SAFETY_SHA256" ]] || return 1
    notice_operation=$(notice_fence lock cutover "$tag" 2>/dev/null) || return 1
  fi
  # shellcheck source=../scripts/deploy-safety.sh
  source "$base/deploy-safety.sh"
  aboutme_stop_jobs 60 || return 1
  exec {notice_boot_fd}>>/run/aboutme/workload.lock || return 1
  flock -x -w 300 "$notice_boot_fd" || return 1
  aboutme_fail_closed 60 && aboutme_assert_cutover_quiescent || return 1
  "$base/fence.sh" checkpoint "$notice_operation" "$tag" >/dev/null 2>&1 || return 1
  if [[ $resuming == no ]]; then notice_fence mutation-start "$notice_operation" "$tag" quiesce >/dev/null 2>&1 || return 1; fi
  # The legacy helper cannot write evidence; use the incoming reviewed helper.
  "$base/fence.sh" notice-authority "$notice_operation" initializing >/dev/null 2>&1 || return 1
  for name in fence deploy-host deploy-safety; do
    notice_recheck_mount && install -o root -g root -m 0755 "$base/$name.sh" "/usr/local/lib/aboutme/$name.sh" || return 1
  done
  for name in fence deploy-host deploy-safety; do
    notice_safe_file "/usr/local/lib/aboutme/$name.sh" 755 && cmp -s "$base/$name.sh" "/usr/local/lib/aboutme/$name.sh" || return 1
  done
  aboutme_assert_cutover_quiescent && notice_bootstrap
}

if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
  set -euo pipefail
  notice_run() {
    [[ $(id -u) == 0 && $# == 1 ]] && notice_overrides_absent || return 1
    local dependency
    for dependency in fence.sh workload-lock.sh deploy-safety.sh; do
      if [[ $1 == adopt && $dependency != workload-lock.sh ]]; then continue; fi
      notice_safe_file "/usr/local/lib/aboutme/$dependency" 755 || return 1
    done
    install -d -o root -g root -m 0755 /run/aboutme || return 1
    case $1 in initialize) notice_bootstrap ;; adopt) notice_adopt ;; *) return 1 ;; esac
  }
  if ! notice_run "$@" >/dev/null 2>&1; then notice_invalid; exit 1; fi
fi
