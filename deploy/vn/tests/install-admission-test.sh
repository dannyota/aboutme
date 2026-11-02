#!/usr/bin/env bash
# Exercise real file copies and authentication admission before PostgreSQL reload.
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

# Retain the production predicate; replace only host paths and the postgres UID.
python3 - "$root" "$installer" "$work" <<'PY'
import pathlib,re,sys
root,installer,work=map(pathlib.Path,sys.argv[1:])
source=installer.read_text()
def block(start,end):
    return source.split(start,1)[1].split(end,1)[0]
copy_tree='copy_tree() {'+block('copy_tree() {','\n}\n')+'\n}\n'
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
for name,body in [('ordinary',ordinary),('postgres',postgres)]:
    # Absolute destinations change; bundle source paths stay intact.
    body=body.replace(' /etc',' "$FIXTURE_CASE/etc"')
    body=body.replace('"/etc','"$FIXTURE_CASE/etc')
    body=body.replace('/usr/local/lib/aboutme','$FIXTURE_CASE/lib')
    (work/(name+'.sh')).write_text(
        '#!/usr/bin/env bash\nset -euo pipefail\nsource "$FIXTURE_SUPPORT"\n'+copy_tree+body)
gate=(root/'deploy/vn/host/crowdsec-db.sh').read_text()
gate=gate.replace('/etc/postgresql/18/main',str(work/'host-placeholder'))
gate=gate.replace('getpwnam("postgres").pw_uid','getpwuid(os.getuid()).pw_uid')
(work/'gate-template.sh').write_text(gate)
PY

cat >"$FIXTURE_SUPPORT" <<'SH'
bundle=$FIXTURE_BUNDLE
# shellcheck disable=SC1091
source "$FIXTURE_CASE/gate.sh"
crowdsec_db_admin() {
  cat >/dev/null
  printf 't\n%s\n' "$(<"$FIXTURE_CASE/loaded")"
}
admit() {
  [[ $FIXTURE_TICK == yes ]] || return 0
  crowdsec_db_auth || {
    printf 'install-admission-test: authentication refused before reload at %s\n' "$*" >&2
    return 1
  }
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
  "$FIXTURE_INSTALL" -o "$FIXTURE_UID" -g "$FIXTURE_GID" "${args[@]}"
  admit "${args[@]}"
}
systemctl() {
  case $1 in
    reload | restart)
      admit "$@"
      python3 - "$FIXTURE_CASE/loaded" <<'PY'
import decimal,pathlib,sys,time
pathlib.Path(sys.argv[1]).write_text(str(decimal.Decimal(time.time_ns())/1000000000))
PY
      printf '%s\n' "$1" >>"$FIXTURE_CASE/reloads"
      ;;
    stop) ;;
    *) return 2 ;;
  esac
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
        state[str(path.relative_to(root))]=[
            hashlib.sha256(path.read_bytes()).hexdigest(),entry.st_ino,
            entry.st_uid,entry.st_gid,entry.st_mode,entry.st_nlink,
            entry.st_mtime_ns,entry.st_ctime_ns]
saved=root/'snapshot.json'
if sys.argv[2]=='save': saved.write_text(json.dumps(state))
else:
    before=json.loads(saved.read_text())
    changed=[name for name in before.keys()|state.keys() if before.get(name)!=state.get(name)]
    assert not changed, 'unchanged file metadata changed: '+', '.join(changed)
PY
}

for mode in ordinary postgres; do
  export FIXTURE_CASE=$work/$mode FIXTURE_TICK=no
  mkdir -p "$FIXTURE_CASE/etc/pgbackrest" "$FIXTURE_CASE/etc/postgresql/18/main" \
    "$FIXTURE_CASE/etc/crowdsec/acquis.d" "$FIXTURE_CASE/lib"
  sed "s|$work/host-placeholder|$FIXTURE_CASE/etc/postgresql/18/main|g" \
    "$work/gate-template.sh" >"$FIXTURE_CASE/gate.sh"
  bash "$work/$mode.sh"
  # Pin file mtimes before the simulated last successful PostgreSQL reload.
  python3 - "$FIXTURE_CASE" <<'PY'
import os,pathlib,sys
root=pathlib.Path(sys.argv[1])
for tree in ('etc','lib'):
    for path in (root/tree).rglob('*'):
        if path.is_file(): os.utime(path,ns=(1577836800000000000,1577836800000000000))
(root/'loaded').write_text('1577836801')
PY
  snapshot save
  export FIXTURE_TICK=yes
  # shellcheck disable=SC1090
  source "$FIXTURE_SUPPORT"
  crowdsec_db_auth || fail 'initial authentication admission failed'
  bash "$work/$mode.sh" || fail "$mode reinstall tripped authentication admission"
  snapshot check
  [[ $(wc -l <"$FIXTURE_CASE/reloads") == 2 ]] || fail 'missing PostgreSQL reload boundary'
  printf 'PASS %s-unchanged-files-keep-admission-and-metadata\n' "$mode"

  # Real changes still need installation and a reload before admission.
  export FIXTURE_TICK=no
  for file in pg_hba.conf pg_ident.conf; do
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
      bash "$work/$mode.sh"
      cmp "$FIXTURE_BUNDLE/host/etc/postgresql/18/main/$file" "$target"
      python3 - "$target" "$FIXTURE_UID" "$FIXTURE_GID" <<'PY'
import os,stat,sys
entry=os.lstat(sys.argv[1])
assert (entry.st_uid,entry.st_gid,stat.S_IMODE(entry.st_mode))==(int(sys.argv[2]),int(sys.argv[3]),0o640)
PY
      crowdsec_db_auth || fail "$mode $file $fault repair failed admission after reload"
    done
  done
  printf 'PASS %s-changed-peer-files-still-install\n' "$mode"
done
