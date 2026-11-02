#!/usr/bin/env bash
# Exercise file copies, database epochs, and lease admission across service jobs.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
installer=${1:-$root/deploy/vn/host/install.sh}
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
export FIXTURE_INSTALL FIXTURE_UID FIXTURE_GID FIXTURE_BUNDLE FIXTURE_SUPPORT
FIXTURE_INSTALL=$(command -v ginstall || command -v install)
FIXTURE_UID=$(id -u)
FIXTURE_GID=$(id -g)
FIXTURE_BUNDLE=$root/deploy/vn
FIXTURE_SUPPORT=$work/support.sh

fail() { printf 'install-admission-test: %s\n' "$*" >&2; exit 1; }

# Keep production admission; replace host paths, SQL responses, and fixture custody.
python3 - "$root" "$installer" "$work" <<'PY'
import pathlib,re,sys
root,installer,work=map(pathlib.Path,sys.argv[1:])
source=installer.read_text()
def block(start,end):
    return source.split(start,1)[1].split(end,1)[0]
copy_tree='copy_tree() {'+block('copy_tree() {','\n}\n')+'\n}\n'
helpers=''
if '# Serialize service operations with retention and watchdog admission.' in source:
    helpers=block('# Serialize service operations with retention and watchdog admission.', '# copy_tree')
postgres=block('if [ "$only" = postgres ]; then','\nfi\n')
postgres=postgres[re.search(r'^  install .*pgbackrest/pgbackrest.conf.*$',postgres,re.M).start():]
postgres=postgres.split('  say "PostgreSQL and pgBackRest installed"',1)[0]
ordinary=block('# 2. CrowdSec config','# 3. Packages')
ordinary=ordinary[ordinary.index('\n'):]
config=block('# 4. Configuration files.','[[ $(findmnt -n -o FSTYPE -T /run/aboutme)')
ordinary+=config[config.index('\n'):]
ordinary+='copy_script() {'+block('copy_script() {','\n}\n')+'\n}\n'
ordinary+='copy_script "$bundle/scripts/fence.sh"'+block(
    'copy_script "$bundle/scripts/fence.sh"','for f in "$bundle"/host/bin/')
ordinary+=next(line for line in source.splitlines()
               if line.startswith('copy_tree "$bundle/edge/crowdsec"'))+'\n'
ordinary+='\n'.join(line for line in source.splitlines() if line.endswith('systemctl daemon-reload'))+'\n'
for name,body in [('ordinary',ordinary),('postgres',postgres)]:
    body=helpers+body
    # Absolute destinations change; bundle source paths stay intact.
    body=body.replace(' /etc',' "$FIXTURE_CASE/etc"')
    body=body.replace('"/etc','"$FIXTURE_CASE/etc')
    body=body.replace('/usr/local/lib/aboutme','$FIXTURE_CASE/lib')
    body=body.replace('/run/aboutme','$FIXTURE_CASE/run')
    body=body.replace('/var/lib/aboutme','$FIXTURE_CASE/state')
    (work/(name+'.sh')).write_text(
        '#!/usr/bin/env bash\nset -euo pipefail\nsource "$FIXTURE_SUPPORT"\n'
        'tmp=$(mktemp -d "$FIXTURE_CASE/tmp.XXXXXX")\ntrap \'rm -rf -- "$tmp"\' EXIT\n'+copy_tree+body)
gate=(root/'deploy/vn/host/crowdsec-db.sh').read_text()
gate=gate.replace('/etc/postgresql/18/main',str(work/'host-placeholder'))
gate=gate.replace('getpwnam("postgres").pw_uid','getpwuid(os.getuid()).pw_uid')
gate=gate.replace('/usr/local/lib/aboutme',str(work/'lib-placeholder'))
gate=gate.replace('/var/lib/aboutme',str(work/'state-placeholder'))
# Keep the real process predicate, with a synthetic process tree only for Python.
gate=gate.replace('pathlib.Path("/proc")', 'pathlib.Path("'+str(work/'proc')+'")')
gate=gate.replace('/proc/sys/kernel/random/boot_id',str(work/'proc/sys/kernel/random/boot_id'))
gate+='\nsource "$FIXTURE_CASE/database-fixture.sh"\n'
(work/'gate-template.sh').write_text(gate)
lease=(root/'deploy/vn/host/crowdsec-retention-lease-check.sh').read_text()
lease=lease.replace('/run/aboutme',str(work/'run-placeholder'))
lease=lease.replace('/usr/local/lib/aboutme/crowdsec-db.sh', '$FIXTURE_CASE/gate.sh')
lease=lease.replace('entry.st_uid==entry.st_gid==0',
                    'entry.st_uid==os.getuid() and entry.st_gid==os.getgid()')
# Host command overhead must not age the fixture's lease.
lease=lease.replace('time.time()', 'float(os.environ["FIXTURE_LEASE_NOW"])')
(work/'lease-template.sh').write_text(lease)
proc=work/'proc'
(proc/'999').mkdir(parents=True)
(proc/'999/exe').symlink_to('/usr/lib/postgresql/18/bin/postgres')
(proc/'999/stat').write_text('999 (postgres) '+ ' '.join(['1']*20))
(proc/'sys/kernel/random').mkdir(parents=True)
(proc/'sys/kernel/random/boot_id').write_text('11111111-1111-4111-8111-111111111111\n')
PY

cat >"$work/database-fixture.sh" <<'SH'
# Replace SQL and host custody, retaining ready/process/lease admission.
crowdsec_db_root() { :; }
crowdsec_db_config() { :; }
crowdsec_db_mount() { :; }
crowdsec_db_held_lock() { [[ ${crowdsec_db_lock_fd:-} =~ ^[0-9]+$ ]]; }
crowdsec_db_admin() {
  local sql
  sql=$(cat)
  printf 't\n'
  [[ $sql != *pg_conf_load_time* ]] || cat "$FIXTURE_CASE/loaded"
}
crowdsec_db_psql() {
  cat >/dev/null
  cat "$FIXTURE_CASE/lib/crowdsec-schema-v1.8.1.tsv"
  printf 'check\ttrue\nidentity\t123:1577836800.000001\n'
}
SH

cat >"$FIXTURE_SUPPORT" <<'SH'
bundle=$FIXTURE_BUNDLE
# shellcheck disable=SC1091
source "$FIXTURE_CASE/gate.sh"
die() { printf 'install: %s\n' "$*" >&2; exit 1; }
# Lock inherited descriptions on both Linux and macOS; Homebrew flock reopens them.
flock() {
  python3 - "$@" <<'PY'
import fcntl,sys,time
args=sys.argv[1:]
mode=fcntl.LOCK_SH if '-s' in args else fcntl.LOCK_EX
wait=0 if '-n' in args else float(args[args.index('-w')+1])
deadline=time.monotonic()+wait
while True:
    try:
        fcntl.flock(int(args[-1]),mode|fcntl.LOCK_NB)
        break
    except BlockingIOError:
        if time.monotonic()>=deadline: sys.exit(1)
        time.sleep(0.01)
PY
}

admit() {
  [[ $FIXTURE_TICK == yes ]] || return 0
  # Match the watcher's fallback when an exclusive retention owner is busy.
  (
    local retention_admitted=yes
    exec {tick_retention}<"$FIXTURE_CASE/run/crowdsec-retention.lock"
    flock -s -n "$tick_retention" || retention_admitted=no
    exec {crowdsec_db_lock_fd}<"$FIXTURE_CASE/state/fence.lock"
    if ! flock -s -n "$crowdsec_db_lock_fd"; then
      [[ $retention_admitted == no ]] || {
        printf 'install-admission-test: operation omitted retention exclusion\n' >&2
        return 1
      }
      printf 'blocked\n' >>"$FIXTURE_CASE/blocked"
      return 0
    fi
    # shellcheck disable=SC1091
    source "$FIXTURE_CASE/lease.sh"
    local result=0
    crowdsec_db_process >/dev/null || result=1
    crowdsec_lease_check || result=1
    ((result == 0)) || {
      printf 'install-admission-test: lease refused at %s\n' "$*" >&2
      return 1
    }
    printf 'admitted\n' >>"$FIXTURE_CASE/admitted"
  )
}
stat() {
  # Map fixture custody to root for production lock validation.
  case $* in
    *"$FIXTURE_CASE/run"* | *"$FIXTURE_CASE/state"*)
      command stat "$@" | sed -E "s/^$FIXTURE_UID:$FIXTURE_GID:/0:0:/;s/^$FIXTURE_UID:/0:/" ;;
    *) command stat "$@" ;;
  esac
}
install() {
  local -a args=()
  while (($#)); do
    case $1 in
      -o | -g) shift 2 ;;
      *) args+=("$1"); shift ;;
    esac
  done
  # Map installed ownership to the fixture user, keeping GNU copying real.
  "$FIXTURE_INSTALL" -o "$FIXTURE_UID" -g "$FIXTURE_GID" "${args[@]}" || return 1
  case ${args[*]} in
    *"$FIXTURE_CASE/etc/postgresql/18/main/"*) admit "${args[@]}" ;;
    *) [[ ${FIXTURE_DRIFT:-no} == yes ]] || admit "${args[@]}" ;;
  esac
}
systemctl() {
  local verb=$1
  case $verb in
    reload | restart | daemon-reload)
      # A synchronous client can return just before systemd removes its job.
      printf '42\n' >"$FIXTURE_CASE/job"
      admit "$@" || return 1
      if [[ $verb == restart ]]; then
        printf '%s\n' "$(($(<"$FIXTURE_CASE/generation") + 1))" >"$FIXTURE_CASE/generation"
      fi
      [[ $FIXTURE_TICK != no ]] || : >"$FIXTURE_CASE/job"
      python3 - "$FIXTURE_CASE/loaded" <<'PY'
import decimal,pathlib,sys,time
pathlib.Path(sys.argv[1]).write_text(str(decimal.Decimal(time.time_ns())/1000000000)+'\n')
PY
      printf '%s\n' "$verb" >>"$FIXTURE_CASE/reloads"
      ;;
    show)
      if [[ -s $FIXTURE_CASE/job ]]; then
        [[ $* == *MainPID* ]] || admit pending-job
        printf 'ActiveState=reloading\nJob=42\n'
        [[ $* != *MainPID* ]] || printf 'MainPID=999\nInvocationID=%032x\n' "$(<"$FIXTURE_CASE/generation")"
        if [[ $* != *MainPID* && ${FIXTURE_STUCK:-no} != yes ]]; then
          : >"$FIXTURE_CASE/job"
        fi
      else
        printf 'ActiveState=active\nJob=\n'
        [[ $* != *MainPID* ]] || printf 'MainPID=999\nInvocationID=%032x\n' "$(<"$FIXTURE_CASE/generation")"
      fi
      ;;
    start)
      [[ $* == *aboutme-crowdsec-retention-failure.service ]] || return 2
      printf 'closed\n' >>"$FIXTURE_CASE/closed"
      admit failure-shutdown
      ;;
    is-enabled | is-active | stop) ;;
    *) return 2 ;;
  esac
}
# Keep the production command structure while replacing the manager boundary.
timeout() {
  [[ $1 != --kill-after=* ]] || shift
  shift
  "$@"
}
fixture_lease() {
  python3 - "$FIXTURE_CASE/run/crowdsec-retention-ok" "$1" <<'PY'
import os,pathlib,sys
path=pathlib.Path(sys.argv[1])
path.write_text(sys.argv[2]+'\n')
path.chmod(0o600)
os.utime(path,ns=(1577836800000000000,1577836800000000000))
PY
}
SH

snapshot() {
  python3 - "$FIXTURE_CASE" "$1" <<'PY'
import hashlib,json,pathlib,sys
root=pathlib.Path(sys.argv[1])
state={}
for tree in ('etc','lib'):
    for path in (root/tree).rglob('*'):
        if not path.is_file(): continue
        entry=path.lstat()
        # GNU install before 9.12 reapplies matching attributes, changing ctime.
        state[str(path.relative_to(root))]=[
            hashlib.sha256(path.read_bytes()).hexdigest(),entry.st_ino,
            entry.st_uid,entry.st_gid,entry.st_mode,entry.st_nlink,
            entry.st_mtime_ns]
saved=root/'snapshot.json'
if sys.argv[2]=='save': saved.write_text(json.dumps(state))
else:
    before=json.loads(saved.read_text())
    changed=[name for name in before.keys()|state.keys() if before.get(name)!=state.get(name)]
    assert not changed, 'unchanged file metadata changed: '+', '.join(changed)
PY
}

# Reapplying matching permissions may change ctime without changing admission.
(
  export FIXTURE_CASE=$work/metadata
  mkdir -p "$FIXTURE_CASE/etc" "$FIXTURE_CASE/lib"
  target=$FIXTURE_CASE/etc/config
  printf 'fixture\n' >"$target"
  chmod 0644 "$target"
  snapshot save
  chmod 0644 "$target"
  snapshot check
  for fault in content mtime mode missing; do
    snapshot save
    case $fault in
      content) printf 'changed\n' >>"$target" ;;
      mtime)
        python3 - "$target" <<'PY'
import os,sys
path=sys.argv[1]
entry=os.stat(path)
os.utime(path,ns=(entry.st_atime_ns,entry.st_mtime_ns+1000000000))
PY
        ;;
      mode) chmod 0600 "$target" ;;
      missing) rm -- "$target" ;;
    esac
    if snapshot check >/dev/null 2>&1; then fail "snapshot missed $fault drift"; fi
  done
  printf 'PASS snapshot-preserves-admission-metadata-contract\n'
)

for mode in ordinary postgres; do
  export FIXTURE_CASE=$work/$mode FIXTURE_TICK=no FIXTURE_DRIFT=no
  export FIXTURE_LEASE_NOW=1577836801
  mkdir -p "$FIXTURE_CASE/etc/pgbackrest" "$FIXTURE_CASE/etc/postgresql/18/main" \
    "$FIXTURE_CASE/etc/crowdsec/acquis.d" "$FIXTURE_CASE/lib" \
    "$FIXTURE_CASE/run" "$FIXTURE_CASE/state"
  chmod 0755 "$FIXTURE_CASE/run"
  printf '1\n' >"$FIXTURE_CASE/generation"
  touch "$FIXTURE_CASE/run/crowdsec-retention.lock" "$FIXTURE_CASE/state/fence.lock"
  chmod 0600 "$FIXTURE_CASE/run/crowdsec-retention.lock" "$FIXTURE_CASE/state/fence.lock"
  cp "$FIXTURE_BUNDLE/host/crowdsec-schema-v1.8.1.tsv" "$FIXTURE_CASE/lib/"
  cp "$FIXTURE_BUNDLE/host/crowdsec-retention.sh" "$FIXTURE_CASE/lib/"
  cp "$work/database-fixture.sh" "$FIXTURE_CASE/"
  sed -e "s|$work/host-placeholder|$FIXTURE_CASE/etc/postgresql/18/main|g" \
    -e "s|$work/lib-placeholder|$FIXTURE_CASE/lib|g" \
    -e "s|$work/state-placeholder|$FIXTURE_CASE/state|g" \
    "$work/gate-template.sh" >"$FIXTURE_CASE/gate.sh"
  sed "s|$work/run-placeholder|$FIXTURE_CASE/run|g" \
    "$work/lease-template.sh" >"$FIXTURE_CASE/lease.sh"
  bash "$work/$mode.sh"
  # Pin file mtimes before the simulated last successful PostgreSQL reload.
  python3 - "$FIXTURE_CASE" <<'PY'
import os,pathlib,sys
root=pathlib.Path(sys.argv[1])
for tree in ('etc','lib'):
    for path in (root/tree).rglob('*'):
        if path.is_file(): os.utime(path,ns=(1577836800000000000,1577836800000000000))
(root/'loaded').write_text('1577836801\n')
PY
  snapshot save
  export FIXTURE_TICK=yes
  # shellcheck disable=SC1090
  source "$FIXTURE_SUPPORT"
  crowdsec_db_epoch=''
  crowdsec_db_ready || fail 'initial database admission failed'
  fixture_lease "$crowdsec_db_epoch"
  admit initial || fail 'initial lease admission failed'
  : >"$FIXTURE_CASE/reloads"
  bash "$work/$mode.sh" || fail "$mode reinstall tripped authentication admission"
  snapshot check
  [[ ! -s $FIXTURE_CASE/job ]] || fail 'service job outlived installer lock'
  [[ $(grep -Ec '^(reload|restart)$' "$FIXTURE_CASE/reloads" || true) == 0 ]] || fail 'unchanged reinstall changed PostgreSQL'
  admit settled || fail 'lease admission failed after reinstall'
  printf 'PASS %s-unchanged-files-keep-admission-and-metadata\n' "$mode"

  # Calibrate both real predicates against an exposed reload job.
  printf '42\n' >"$FIXTURE_CASE/job"
  if crowdsec_db_process >/dev/null; then fail 'database process admitted a reload job'; fi
  printf '42\n' >"$FIXTURE_CASE/job"
  # shellcheck disable=SC1091
  source "$FIXTURE_CASE/lease.sh"
  if (crowdsec_lease_check); then fail 'lease admitted a reload job'; fi
  : >"$FIXTURE_CASE/job"
  admit calibrated || fail 'calibration did not restore lease admission'

  # Keep the real age predicate, including its exact lower and upper bounds.
  FIXTURE_LEASE_NOW=1577836800 crowdsec_lease_file || fail 'lease refused its creation time'
  FIXTURE_LEASE_NOW=1577837160 crowdsec_lease_file || fail 'lease refused its maximum age'
  if (FIXTURE_LEASE_NOW=1577837161 crowdsec_lease_file); then fail 'lease admitted an expired timestamp'; fi
  if (FIXTURE_LEASE_NOW=1577836799 crowdsec_lease_file); then fail 'lease admitted a future timestamp'; fi
  crowdsec_lease_file || fail 'lease clock calibration did not restore admission'

  # Real changes still need installation and a settled operation before admission.
  export FIXTURE_TICK=yes FIXTURE_DRIFT=yes
  for file in pg_hba.conf pg_ident.conf conf.d/90-aboutme.conf; do
    for fault in content mode missing ownership; do
      target=$FIXTURE_CASE/etc/postgresql/18/main/$file
      case $fault in
        content) printf '\n# fixture drift\n' >>"$target" ;;
        mode) chmod 0600 "$target" ;;
        missing) rm -- "$target" ;;
        ownership)
          ((EUID == 0)) || continue
          chown 1:1 "$target"
          ;;
      esac
      : >"$FIXTURE_CASE/reloads"
      : >"$FIXTURE_CASE/blocked"
      bash "$work/$mode.sh"
      [[ ! -s $FIXTURE_CASE/job ]] || fail 'service job outlived installer lock'
      [[ -s $FIXTURE_CASE/blocked ]] || fail 'watcher did not attempt admission during service job'
      expected=reload
      [[ $mode != postgres ]] || expected=restart
      [[ $(grep -c "^$expected$" "$FIXTURE_CASE/reloads") == 1 ]] || fail 'changed configuration missed service operation'
      cmp "$FIXTURE_BUNDLE/host/etc/postgresql/18/main/$file" "$target"
      python3 - "$target" "$FIXTURE_UID" "$FIXTURE_GID" <<'PY'
import os,stat,sys
entry=os.lstat(sys.argv[1])
mode=0o644 if sys.argv[1].endswith('90-aboutme.conf') else 0o640
assert (entry.st_uid,entry.st_gid,stat.S_IMODE(entry.st_mode))==(int(sys.argv[2]),int(sys.argv[3]),mode)
PY
      if [[ $mode == postgres ]]; then
        # A restart changes the epoch. Existing leases must remain refused.
        if (crowdsec_lease_check); then fail 'restart retained an old lease'; fi
        crowdsec_db_ready || fail 'restarted database did not settle'
        fixture_lease "$crowdsec_db_epoch"
      fi
      admit repaired || fail "$mode $file $fault repair failed lease admission"
    done
  done
  printf 'PASS %s-changed-files-settle-before-lease-admission\n' "$mode"
  if [[ $mode == ordinary ]]; then
    export FIXTURE_STUCK=yes
    if bash "$work/$mode.sh"; then fail 'unsettled reload job returned success'; fi
    [[ ! -e $FIXTURE_CASE/run/crowdsec-retention-ok &&
      -s $FIXTURE_CASE/run/crowdsec-retention-failed &&
      -s $FIXTURE_CASE/closed ]] || fail 'unsettled job did not revoke admission'
    : >"$FIXTURE_CASE/job"
    unset FIXTURE_STUCK
    printf 'PASS unsettled-job-revokes-before-lock-release\n'
  fi
done
