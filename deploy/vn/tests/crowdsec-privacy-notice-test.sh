#!/usr/bin/env bash
# Hosted, isolated guest filesystem proof. This does not prove provider identity, physical power-loss durability, delivery, or full-host reconciliation.
set -euo pipefail; export LC_ALL=C
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
fail() { printf 'FAIL notice host %s\n' "$1" >&2; exit 1; }
primitive=$root/deploy/vn/host/crowdsec-privacy-notice-host.sh; wrapper=$root/deploy/vn/scripts/crowdsec-privacy-notice.sh
controller_source=$root/deploy/vn/scripts/restore-drill.sh; [[ -f $primitive && -f $wrapper ]] || fail 'missing implementation'
legacy_fields=(LEGACY_RELEASE_TAG LEGACY_RELEASE_SHA256 LEGACY_SERVER_IMAGE LEGACY_WEB_IMAGE
  LEGACY_FENCE_SHA256 LEGACY_DEPLOY_HOST_SHA256 LEGACY_DEPLOY_SAFETY_SHA256)
for field in "${legacy_fields[@]}"; do
  grep -Eq "^readonly $field='[^']+'$" "$primitive" || fail 'missing closed legacy pin'
done
[[ $(id -u) == 0 ]] || fail 'isolated root runner required'
# Owner commands share the replaceable directory and use only the pinned app identity, socket, and bounded offline container. These structural assertions
# supplement the filesystem proof; they do not prove runtime network isolation.
grep -Fq -- '--network=none' "$wrapper" || fail 'owner command network'; grep -Eq -- '--memory(=|[[:space:]])(256[mM]|268435456)' "$wrapper" || fail 'owner command memory'
grep -Fq 'aboutme_app' "$wrapper" || fail 'owner command database identity'; grep -Fq '/run/postgresql' "$wrapper" || fail 'owner command database socket'
grep -Fq 'server_image' "$wrapper" || fail 'owner command release pin'; grep -Fq 'privacy-notice:/var/lib/aboutme/privacy-notice:ro' "$wrapper" || fail 'owner authority directory mount'
grep -Fq 'authorize-send' "$wrapper" || fail 'explicit send authority action'; grep -Fq 'content_sha256' "$wrapper" || fail 'owner manifest validation'
grep -Fq 'publication_date' "$wrapper" || fail 'owner publication validation'; grep -Fq 'expected_count' "$wrapper" || fail 'owner aggregate validation'
grep -Fq 'accepted_count' "$wrapper" || fail 'owner completion validation'; grep -Fq 'capture' "$wrapper" || fail 'owner capture interruption marker'
grep -Fq 'adopt' "$primitive" || fail 'explicit legacy adoption action'; installer=$root/deploy/vn/host/install.sh
awk '
  /crowdsec-privacy-notice-host.sh/ && !helper {helper=NR}; /workload-lock.sh/ && !workload {workload=NR}
  /fence.sh/ && !fence {fence=NR}; /restore-drill.sh/ && !controller {controller=NR}
  /if \[ "\$only" = postgres \]/ {postgres=NR}; END {exit !(helper && workload && fence && controller && postgres && helper<postgres && workload<postgres && fence<postgres && controller<postgres)}
' "$installer" || fail 'postgres installer skipped shared dependencies'
work=$(mktemp -d)
# Fixtures share an executable header; bodies retain their own failure rules.
stub() { local target=$1; { printf '#!/usr/bin/env bash\n'; cat; } >"$target"; chmod 0755 "$target"; }
loops=()
cleanup() {
  umount /etc/containers/systemd 2>/dev/null || true; umount /etc/aboutme 2>/dev/null || true; umount /usr/local/lib/aboutme 2>/dev/null || true; umount -R /var/lib/aboutme 2>/dev/null || true
  umount -R /srv/data 2>/dev/null || true
  for device in "${loops[@]}"; do losetup -d "$device"; done
  rm -rf -- "$work"
}
trap cleanup EXIT; mkdir -p "$work/bin" "$work/lib" /srv/data /var/lib/aboutme /usr/local/lib/aboutme; truncate -s 64M "$work/disk"
mkfs.ext4 -q -L aboutme-data "$work/disk"; device=$(losetup --find --show "$work/disk")
loops+=("$device"); mount "$device" /srv/data; mkdir /srv/data/aboutme
install -d -o root -g root -m 0700 /srv/data/host-operations; mount --bind /srv/data/aboutme /var/lib/aboutme
cp "$primitive" "$work/lib/crowdsec-privacy-notice-host.sh"; cp "$controller_source" "$work/lib/restore-drill.sh"
cp "$root/deploy/vn/scripts/fence.sh" "$root/deploy/vn/scripts/deploy-safety.sh" \
  "$root/deploy/vn/host/workload-lock.sh" "$work/lib/"
mount --bind "$work/lib" /usr/local/lib/aboutme; mkdir -p /run/aboutme; export NOTICE_FIXTURE=$work NOTICE_DEVICE=$device
for command in sync mv rm; do
  stub "$work/bin/$command" <<'STUB'
set -euo pipefail; name=${0##*/}
printf '%s\n' "$name" >>"$NOTICE_FIXTURE/effects"
if [[ $name == mv && ${*: -1} == /var/lib/aboutme/fence.json && ${NOTICE_RELEASE_FAULT:-none} != none ]]; then
  replacement=${@: -2:1}
  if jq -e 'has("notice_authority") and (.operation_id // null)==null' "$replacement" >/dev/null 2>&1; then
    grep -qxF helper-joined "$NOTICE_FIXTURE/order" || exit 1; sha256sum /var/lib/aboutme/privacy-notice/epoch | cut -d' ' -f1 >"$NOTICE_FIXTURE/release-epoch-hash"
    printf 'release-attempt\n' >>"$NOTICE_FIXTURE/release-attempts"
    case $NOTICE_RELEASE_FAULT in
      before-store) exit 1 ;;
      after-rename) /usr/bin/mv "$@"; exit 1 ;;
      after-sync | lost-response) touch "$NOTICE_FIXTURE/release-renamed" ;;
      *) exit 1 ;;
    esac
  fi
fi
case ${*: -1} in
  */privacy-notice) printf '%s directory\n' "$name" >>"$NOTICE_FIXTURE/order" ;;
  */epoch | */send.json | */activation.json | */restore-reconciliation)
    printf '%s %s\n' "$name" "${*: -1}" | sed 's|/var/lib/aboutme/privacy-notice/||' >>"$NOTICE_FIXTURE/order" ;;
  *) printf '%s temporary\n' "$name" >>"$NOTICE_FIXTURE/order" ;;
esac
if [[ $name == sync ]]; then
  if [[ ${*: -1} == /var/lib/aboutme && -f $NOTICE_FIXTURE/release-renamed ]]; then
    /usr/bin/sync "$@"
    case ${NOTICE_RELEASE_FAULT:-none} in
      after-sync) exit 1 ;;
      lost-response) kill -TERM "$PPID"; exit 1 ;;
    esac
  fi
  n=$(cat "$NOTICE_FIXTURE/sync-count" 2>/dev/null || printf 0); n=$((n + 1))
  printf '%s\n' "$n" >"$NOTICE_FIXTURE/sync-count"
  if [[ ${*: -1} == /var/lib/aboutme/privacy-notice* ]]; then
    [[ -e $NOTICE_FIXTURE/first-authority-sync ]] || printf '%s\n' "$n" >"$NOTICE_FIXTURE/first-authority-sync"; printf '%s\n' "$n" >"$NOTICE_FIXTURE/last-authority-sync"
  fi
  [[ $n != "${NOTICE_FAIL_SYNC:-0}" ]] || { printf 'fixture_failure\n' >&2; exit 1; }
  if [[ $n == "${NOTICE_CRASH_SYNC:-0}" ]]; then kill -KILL "$PPID"; exit 137; fi
  if [[ ${*: -1} == /var/lib/aboutme/privacy-notice ]]; then
    /usr/bin/rm -f "$NOTICE_FIXTURE/await-directory-sync"
  elif [[ -f ${*: -1} && ! -L ${*: -1} ]]; then
    printf '%s\n' "${*: -1}" >"$NOTICE_FIXTURE/last-file-sync"
  fi
fi
if [[ $name == mv && ${*: -1} == /var/lib/aboutme/privacy-notice/* ]]; then
  source_file=${@: -2:1}; [[ $(dirname "$source_file") == /var/lib/aboutme/privacy-notice ]] || exit 1
  [[ $(stat -c '%u:%g:%a:%h' "$source_file") == 0:0:444:1 ]] || exit 1; [[ -f $NOTICE_FIXTURE/last-file-sync && $(cat "$NOTICE_FIXTURE/last-file-sync") == "$source_file" ]] || exit 1
  [[ ! -e $NOTICE_FIXTURE/await-directory-sync ]] || exit 1; [[ ${NOTICE_FAIL_MOVE:-0} == 0 ]] || exit 1
  touch "$NOTICE_FIXTURE/await-directory-sync"
fi
if [[ $name == rm && ${NOTICE_CLAIMED:-0} == 1 && $* == *send.json* ]]; then
  [[ -f $NOTICE_FIXTURE/joined ]] || exit 1
fi
exec "/usr/bin/$name" "$@"
STUB
done
stub "$work/bin/systemctl" <<'STUB'
set -euo pipefail
case $1 in
  is-active) [[ ${NOTICE_APP_STATE:-absent} == active || ${*: -1} == aboutme-maintenance* ]] ;;
  show)
    [[ ${NOTICE_APP_STATE:-absent} != inspection-failure ]] || exit 1
    if [[ ${NOTICE_CLAIMED:-0} == 1 && -f $NOTICE_FIXTURE/stopped && ${NOTICE_JOB:-0} == 0 && ${NOTICE_UNIT_STATE:-inactive} == inactive ]]; then
      touch "$NOTICE_FIXTURE/joined"; printf 'join\n' >>"$NOTICE_FIXTURE/order"
    fi
    case $* in
      *ActiveState*Job*) printf 'ActiveState=%s\nJob=%s\n' "${NOTICE_UNIT_STATE:-inactive}" "${NOTICE_JOB:-0}" ;;
      *ActiveState*) printf '%s\n' "${NOTICE_UNIT_STATE:-inactive}" ;;
      *Job*) printf '%s\n' "${NOTICE_JOB:-0}" ;;
      *) exit 1 ;;
    esac ;;
  list-jobs | list-units) : ;;
  stop)
    if [[ ${NOTICE_CAPTURE:-0} == 1 && $* == *aboutme-server* ]]; then
      [[ -f /var/lib/aboutme/privacy-notice/capture-in-progress ]] || exit 1
    fi
    printf 'stop\n' >>"$NOTICE_FIXTURE/order"
    if [[ $* == *aboutme-server* ]]; then touch "$NOTICE_FIXTURE/stopped"; fi
    ;;
  start) printf 'start\n' >>"$NOTICE_FIXTURE/order" ;;
  *) exit 1 ;;
esac
STUB
stub "$work/bin/pgrep" <<'STUB'
[[ ${NOTICE_APP_STATE:-absent} != process ]] || exit 0; exit 1
STUB
stub "$work/bin/flock" <<'STUB'
set -euo pipefail; [[ ${NOTICE_LOCK:-valid} != contention ]] || exit 1
if [[ ${NOTICE_LOCK:-valid} == changed && ! -e $NOTICE_FIXTURE/changed ]]; then
  touch "$NOTICE_FIXTURE/changed"; mount --bind /srv/data/other /var/lib/aboutme
fi
exec /usr/bin/flock "$@"
STUB
stub "$work/bin/blkid" <<'STUB'
case ${NOTICE_LABEL:-valid} in
  absent) exit 2 ;;
  duplicate) printf '%s\n%s\n' "$NOTICE_DEVICE" "$NOTICE_DEVICE" ;;
  failure) exit 1 ;;
  *) exec /usr/sbin/blkid "$@" ;;
esac
STUB
for command in podman psql pgbackrest; do
  stub "$work/bin/$command" <<'STUB'
printf 'forbidden\n' >>"$NOTICE_FIXTURE/forbidden"; exit 1
STUB
done
chmod 0755 "$work/bin/"* "$work/lib/"*; export PATH=$work/bin:$PATH
host=/usr/local/lib/aboutme/crowdsec-privacy-notice-host.sh; fence=/usr/local/lib/aboutme/fence.sh
authority=/var/lib/aboutme/privacy-notice; controller=/usr/local/lib/aboutme/restore-drill.sh
# shellcheck disable=SC2120 # quiet/refuse forward optional pass arguments.
restore_pass() {
  local result=0 mode=${1:-latest}
  if [[ $mode == time ]]; then
    "$controller" host-pass time 2026-10-19T00:00:00Z >"$work/controller-out" 2>"$work/controller-err" || result=$?
  else
    "$controller" host-pass latest "${@:2}" >"$work/controller-out" 2>"$work/controller-err" || result=$?
  fi
  if grep -Eq 'operation_id|mount_identity|"epoch"|"version"|fixture_failure' "$work/controller-out" "$work/controller-err"; then
    fail 'private controller state escaped'
  fi
  if ((result)); then
    if grep -Eq '^PASS (latest|point-in-time) notice restore fence$' "$work/controller-out"; then fail 'failed controller reported success'; fi
    cat "$work/controller-err" >&2; return "$result"
  fi
  [[ ! -s $work/controller-err ]] || fail 'successful controller diagnostic'
}
refuse() {
  local result=0
  "$@" >"$work/out" 2>"$work/err" || result=$?
  [[ $result == 1 && ! -s $work/out ]] || fail 'refusal exit or stdout'; printf 'invalid_state\n' >"$work/expected"
  cmp -s "$work/expected" "$work/err" || fail 'refusal stderr'
}
quiet() {
  "$@" >"$work/out" 2>"$work/err" || fail 'denial operation refused'
  [[ ! -s $work/out && ! -s $work/err ]] || fail 'success output'
}
reset_owner() { cp "$work/adopted-fence" /var/lib/aboutme/fence.json; }
blocked_pass() { refuse restore_pass; retained; reset_owner; }
retained() {
  jq -e '.operation_kind=="notice_restore" and (.operation_id|type=="string")' /var/lib/aboutme/fence.json >/dev/null || fail 'operation not retained'
  denied
}
denied() {
  local unit
  for unit in aboutme-server aboutme-web aboutme-caddy aboutme-migrate \
    aboutme-db-setup aboutme-job@media-deletion-sweep crowdsec crowdsec-firewall-bouncer; do
    if "$fence" check 4007 "$unit" >"$work/out" 2>"$work/err"; then
      fail 'guarded start admitted'
    fi
  done
}
for action in unknown --force --resume authorize-send pre-restore postcheck finish; do refuse "$host" "$action"; done
refuse "$host" initialize extra; refuse "$host" pre-restore extra
for action in pre-restore postcheck finish resume-owned-operation; do
  refuse bash "$wrapper" "$action"; refuse "$controller" "$action"
done
refuse "$controller" host-pass latest extra; refuse "$controller" host-pass latest --resume
refuse "$controller" host-pass latest callback; refuse "$controller" host-pass latest AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA
for target in '' 2026-02-30T00:00:00Z 2026-10-19 2026-10-19T00:00:00+00:00 2026-10-19T00:00:00.000000Z; do
  refuse "$controller" host-pass time "$target"
done
refuse "$controller" _host-pass extra; refuse "$controller" _host-pass; refuse /usr/bin/setpriv --reuid=65534 --regid=65534 --clear-groups "$controller" host-pass latest
refuse /usr/bin/setpriv --reuid=65534 --regid=65534 --clear-groups "$controller" _host-pass; refuse bash -c '"$1" _host-pass 3<"$2" 4>"$3"' bash "$controller" /dev/null "$work/completion-file"
for function in notice_restore_begin notice_restore_pre_restore notice_restore_postcheck notice_restore_finish; do
  grep -Eq "^$function\(\)[[:space:]]*\\{" "$primitive" || fail 'missing source-only boundary'; refuse bash -c 'source "$1"; "$2" extra' bash "$host" "$function"
done
for record in '' '{}' '{"version":1,"version":1}' '{"version":1,"extra":true}' \
  '{"version":"1"}' '{} {}' $'{}\n{}' '{"version":'; do
  refuse bash -c '"$1" _host-pass 3< <(printf "%s\n" "$2") 4> >(cat >/dev/null)' \
    bash "$controller" "$record"
done
refuse bash -c '"$1" _host-pass 3< <(head -c 1025 /dev/zero) 4> >(cat >/dev/null)' bash "$controller"
for label in absent duplicate failure; do
  NOTICE_LABEL=$label refuse "$host" initialize; [[ ! -e /var/lib/aboutme/fence.json && ! -e $authority ]] || fail 'label failure touched state'
done
umount /var/lib/aboutme; umount /srv/data; refuse "$host" initialize; mount "$device" /srv/data
mount --bind /srv/data/aboutme /var/lib/aboutme
# Missing bind with plausible root shadow state must not initialize authority.
umount /var/lib/aboutme; refuse "$host" initialize; [[ ! -e /var/lib/aboutme/fence.json ]] || fail 'root shadow initialized'
mount --bind /srv/data/aboutme /var/lib/aboutme; mkdir /srv/data/other; mount --bind /srv/data/other /var/lib/aboutme; refuse "$host" initialize
[[ ! -e /srv/data/other/fence.json ]] || fail 'wrong bind initialized'; umount /var/lib/aboutme; mount -t tmpfs tmpfs /srv/data
refuse "$host" initialize; umount /srv/data; mount --bind /srv/data/aboutme /var/lib/aboutme; refuse "$host" initialize
umount /var/lib/aboutme; NOTICE_LOCK=changed refuse "$host" initialize; [[ ! -e /srv/data/other/fence.json ]] || fail 'identity change initialized'
umount /var/lib/aboutme; NOTICE_LOCK=contention refuse "$host" initialize; quiet "$host" initialize
[[ $(jq -r '.operation_kind // "none"' /var/lib/aboutme/fence.json) == none ]] || fail 'successful bootstrap retained operation'
[[ $(stat -c '%u:%g:%a' "$authority") == 0:0:755 ]] || fail 'directory metadata'
for name in epoch restore-reconciliation; do
  [[ $(stat -c '%u:%g:%a:%h' "$authority/$name") == 0:0:444:1 ]] || fail 'file metadata'
done
[[ ! -s $authority/restore-reconciliation ]] || fail 'marker contents'; [[ $(wc -c <"$authority/epoch") == 37 ]] || fail 'epoch length'
grep -qxE '[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}' "$authority/epoch" || fail 'epoch grammar'
jq -e '.notice_authority | keys==["operation_id","state","version"] and .version==1 and .state=="adopted" and (.operation_id|test("^[A-Za-z0-9_-]{43}$"))' \
  /var/lib/aboutme/fence.json >/dev/null || fail 'permanent evidence'
cp "$authority/epoch" "$work/initial-epoch"; cp /var/lib/aboutme/fence.json "$work/adopted-fence"; quiet "$host" initialize
cmp -s "$authority/epoch" "$work/initial-epoch" || fail 'reinstall rotated epoch'; [[ ! -e /var/lib/aboutme/release.json ]] || fail 'bootstrap synthesized release'
# Neither ordinary reinstall nor replacement-host bootstrap may derive receipt authority from a minimum release or a restored database.
[[ ! -e $authority/send.json && ! -e $authority/activation.json ]] || fail 'bootstrap issued receipt'
for command in prepare enqueue status activate authorize-send; do
  refuse bash "$wrapper" "$command" extra
done
refuse bash "$wrapper" --force; refuse bash "$wrapper" --resume
for override in FENCE_DIR ABOUTME_WORKLOAD_LOCK NOTICE_AUTHORITY_DIR NOTICE_OPERATION_ID \
  RECOVERY_WAIT_SECONDS DB_NAME SMOKE_TABLE PG_TARGET NOTICE_COMMAND NOTICE_CONTROLLER; do refuse env "$override=invalid" "$controller" host-pass latest
done
# Controlled database commands retain the inherited workload descriptor while proving contention in a separate process. They never read restored rows.
stub "$work/bin/db-probe" <<'STUB'
set -euo pipefail; name=${0##*/}
if /usr/bin/flock -n /run/aboutme/workload.lock true; then exit 1; fi
[[ ! -e /proc/self/fd/3 && ! -e /proc/self/fd/4 ]] || exit 1
jq -e '.operation_kind=="notice_restore" and (.operation_tag|type=="string")' /var/lib/aboutme/fence.json >/dev/null || exit 1
[[ -f /var/lib/aboutme/privacy-notice/restore-reconciliation && ! -e /var/lib/aboutme/privacy-notice/send.json && ! -e /var/lib/aboutme/privacy-notice/activation.json ]] || exit 1
if [[ $name == pgbackrest ]]; then
  [[ $* == *'--stanza=aboutme'* && $* == *'--archive-mode=off'* && ${*: -1} == restore ]] || exit 1; stage=restore
  answer=
else
  query=
  while (($#)); do
    if [[ $1 == -c ]]; then query=$2; break; fi
    shift
  done
  [[ -n $query ]] || query=$(cat)
  case $query in
    *pg_is_in_recovery*) stage=recovery; answer=f ;;
    *'max(version_id)'*) stage=version; answer=13 ;;
    *query_to_xml*) stage=counts; answer='auth_email_jobs|1' ;;
    *aclexplode*) stage=grants; answer=0 ;;
    *has_schema_privilege*) stage=schema; answer=f ;;
    *string_agg*) stage=roles; answer=aboutme,aboutme_app,aboutme_migrator ;;
    *'SET ROLE aboutme_app'*) stage=smoke; answer=0 ;;
    *) exit 1 ;;
  esac
fi
printf '%s\n' "$stage" >>"$NOTICE_FIXTURE/db-stages"; [[ ${NOTICE_DB_FAIL:-none} != "$stage" ]] || { printf 'fixture_failure\n' >&2; exit 1; }
if [[ ${NOTICE_DB_SIGNAL:-none} == "$stage" ]]; then kill -"${NOTICE_SIGNAL:-TERM}" "$PPID"; exit 1; fi
if [[ ${NOTICE_DB_DRIFT:-none} == "$stage" ]]; then
  printf 'invalid\n' > /var/lib/aboutme/privacy-notice/epoch
fi
if [[ ${NOTICE_DB_HOLD:-none} == "$stage" ]]; then
  printf '%s\n' "$BASHPID" >"$NOTICE_FIXTURE/held-child"; printf '%s\n' "$PPID" >"$NOTICE_FIXTURE/held-controller"; read -r -t 10 _ <"$NOTICE_FIXTURE/continue" || exit 1
fi
[[ -z $answer ]] || printf '%s\n' "$answer"
STUB
cp "$work/bin/db-probe" "$work/bin/pgbackrest"; cp "$work/bin/db-probe" "$work/bin/psql"
stub "$work/bin/setpriv" <<'STUB'
set -euo pipefail
while (($#)); do
  case $1 in
    --reuid=* | --regid=* | --groups=* | --init-groups | --clear-groups) shift ;;
    --reuid | --regid | --groups) shift 2 ;;
    --) shift; break ;;
    pgbackrest | psql | /usr/bin/pgbackrest | /usr/bin/psql) break ;;
    *) exit 1 ;;
  esac
done
[[ ${1##*/} == pgbackrest || ${1##*/} == psql ]] || exit 1; exec "${1##*/}" "${@:2}"
STUB
stub "$work/bin/sudo" <<'STUB'
printf 'forbidden_privilege_drop\n' >>"$NOTICE_FIXTURE/forbidden"; exit 1
STUB
stub "$work/bin/sleep" <<'STUB'
if [[ ${NOTICE_DB_FAIL:-none} == recovery ]]; then kill -TERM "$PPID"; exit 1; fi
exec /usr/bin/sleep "$@"
STUB
# The installed helper source stays unchanged. This private fixture surrounds it to inject completion-channel corruption and nonzero helper termination.
cp "$work/lib/workload-lock.sh" "$work/lib/workload-lock-real.sh"
stub "$work/lib/workload-lock.sh" <<'STUB'
set -euo pipefail; [[ $# == 3 && $1 == 300 && $2 == /usr/local/lib/aboutme/restore-drill.sh && $3 == _host-pass ]] || exit 1
[[ $(stat -Lc %F /proc/self/fd/3) == fifo && $(stat -Lc %F /proc/self/fd/4) == fifo ]] || exit 1
jq -e '.operation_kind=="notice_restore" and (.operation_tag|type=="string")' /var/lib/aboutme/fence.json >/dev/null || exit 1
printf 'helper-enter\n' >>"$NOTICE_FIXTURE/order"
mutate_record() {
  local mode=$1 record=$2 bound=$3 filter
  case $mode in
    empty) return ;; truncated) printf '{"version":'; return ;;
    extra) printf '%s\n{}\n' "$record"; return ;;
    duplicate) printf '%s\n' "$record" | sed 's/"version":1/"version":1,"version":1/'; return ;;
    oversized) printf '%0*d\n' "$bound" 0; return ;;
    unknown) filter='.extra=true' ;; type) filter='.version="1"' ;; version) filter='.version=2' ;;
    owner) filter='.operation_id=("A"*43)' ;; epoch) filter='.epoch="00000000-0000-4000-8000-000000000000"' ;;
    tag) filter='.admission_tag="v0.1.0"' ;; mode) filter='.mode="other"' ;;
    target) filter='.target="2026-02-30T00:00:00Z"' ;; mount) filter='.mount_identity.data_mount_id=-1' ;;
    mount-extra) filter='.mount_identity.extra=0' ;; mount-type) filter='.mount_identity.aboutme_inode="1"' ;;
    mount-drift) filter='.mount_identity.aboutme_inode+=1' ;; *) return 1 ;;
  esac
  printf '%s\n' "$record" | jq -c "$filter"
}
if [[ ${NOTICE_CONTEXT:-valid} != valid ]]; then
  IFS= read -r context <&3 || exit 1
  case $NOTICE_CONTEXT in
    missing) exec 3<&- ;;
    regular) : >"$NOTICE_FIXTURE/regular-context"; exec 3<"$NOTICE_FIXTURE/regular-context" ;;
    *)
      exec 3< <(
        mutate_record "$NOTICE_CONTEXT" "$context" 1025
      )
      ;;
  esac
fi
if [[ ${NOTICE_COMPLETION:-valid} == missing || ${NOTICE_COMPLETION:-valid} == regular ]]; then
  if [[ $NOTICE_COMPLETION == missing ]]; then exec 4>&-
  else : >"$NOTICE_FIXTURE/regular-completion"; exec 4>"$NOTICE_FIXTURE/regular-completion"; fi
  /usr/local/lib/aboutme/workload-lock-real.sh "$@"
elif [[ ${NOTICE_COMPLETION:-valid} != valid ]]; then
  exec {saved_completion}>&4; /usr/local/lib/aboutme/workload-lock-real.sh "$@" 4> >(
    IFS= read -r record || exit 1; mutate_record "$NOTICE_COMPLETION" "$record" 257 >&"$saved_completion"
  )
else
  /usr/local/lib/aboutme/workload-lock-real.sh "$@"
fi
printf 'helper-joined\n' >>"$NOTICE_FIXTURE/order"; [[ ${NOTICE_HELPER_EXIT:-0} == 0 ]] || exit 1
STUB
# Instrument only the copied source boundary, retaining its real checks.
sed -i '/^notice_restore_postcheck() {/a\
  if /usr/bin/flock -n /run/aboutme/workload.lock true; then return 1; fi\
  printf "postcheck\\n" >>"$NOTICE_FIXTURE/order"\
  if [[ ${NOTICE_POSTCHECK_FAIL:-0} == 1 ]]; then printf "invalid_state\\n" >&2; return 1; fi' "$host"
for fault in active process inspection-failure; do
  NOTICE_APP_STATE=$fault refuse restore_pass
done
NOTICE_UNIT_STATE=activating refuse restore_pass; NOTICE_UNIT_STATE=deactivating refuse restore_pass; NOTICE_JOB=1 refuse restore_pass; mount -t tmpfs tmpfs "$authority"
refuse restore_pass; umount "$authority"; mount --bind "$authority" "$authority"; refuse restore_pass
umount "$authority"; mkdir "$authority/nested"; mount -t tmpfs tmpfs "$authority/nested"; refuse restore_pass
umount "$authority/nested"; rmdir "$authority/nested"
for pass in latest time repeated; do
  # Safe malformed files must be revoked without parsing their contents.
  printf '{}\n' >"$authority/send.json"; printf '{}\n' >"$authority/activation.json"; chmod 0444 "$authority/send.json" "$authority/activation.json"; cp "$authority/epoch" "$work/previous-epoch"
  : >"$work/order"; quiet restore_pass "$pass"
  ! cmp -s "$authority/epoch" "$work/previous-epoch" || fail 'restore reused epoch'; cp "$authority/epoch" "$work/$pass-epoch"
  [[ -f $authority/restore-reconciliation && ! -e $authority/send.json && ! -e $authority/activation.json ]] || fail 'restore retained authority'
  [[ ! -e /var/lib/aboutme/release.json ]] || fail 'minimum tag became image authority'
  awk '
    $0=="mv restore-reconciliation" {marker=NR}; $0=="mv epoch" {epoch=NR}
    $0=="rm send.json" {send=NR}; $0=="rm activation.json" {activation=NR}
    $0=="sync directory" {if(marker && !epoch) marker_sync=NR; if(send && !activation) send_sync=NR; if(activation) activation_sync=NR}
    END {exit !(marker && marker_sync>marker && epoch>marker_sync && send>epoch && send_sync>send && activation>send_sync && activation_sync>activation)}
  ' "$work/order" || fail 'restore persistence order'
done
! cmp -s "$work/latest-epoch" "$work/time-epoch" || fail 'PITR reused latest epoch'
for context in missing regular empty truncated extra duplicate unknown type version owner tag mode target mount mount-extra mount-type mount-drift oversized; do
  : >"$work/db-stages"; NOTICE_CONTEXT=$context refuse restore_pass
  [[ ! -s $work/db-stages ]] || fail 'invalid context reached database effect'; retained; reset_owner
done
for completion in missing regular empty truncated extra duplicate unknown type version epoch owner oversized; do
  NOTICE_COMPLETION=$completion blocked_pass
done
for flag in NOTICE_HELPER_EXIT NOTICE_POSTCHECK_FAIL; do
  export "$flag=1"; blocked_pass; unset "$flag"
done
for fault in before-store after-rename after-sync lost-response; do
  rm -f "$work/release-renamed" "$work/release-attempts"; : >"$work/order"
  jq -c '{minimum_release,minimum_tag,notice_authority}' /var/lib/aboutme/fence.json >"$work/release-invariants"
  NOTICE_RELEASE_FAULT=$fault refuse restore_pass; [[ $(wc -l <"$work/release-attempts") == 1 ]] || fail 'ambiguous release retried'
  [[ -f $authority/restore-reconciliation && ! -e $authority/send.json && ! -e $authority/activation.json ]] || fail 'ambiguous release lost denial'
  [[ $(sha256sum "$authority/epoch" | cut -d' ' -f1) == "$(cat "$work/release-epoch-hash")" ]] || fail 'ambiguous release changed pass epoch'
  [[ $(jq -c '{minimum_release,minimum_tag,notice_authority}' /var/lib/aboutme/fence.json) == "$(cat "$work/release-invariants")" ]] || fail 'ambiguous release changed permanent evidence'
  if [[ $fault == before-store ]]; then
    jq -e '.operation_kind=="notice_restore"' /var/lib/aboutme/fence.json >/dev/null || fail 'failed store lost owner'
    denied
  else
    jq -e '(.operation_id // null)==null' /var/lib/aboutme/fence.json >/dev/null || fail 'release recreated old ownership'
  fi
  refuse "$controller" finish; refuse "$controller" host-pass latest --resume
  if [[ $fault == lost-response ]]; then
    rm -f "$work/release-renamed"; cp "$authority/epoch" "$work/unknown-pass-epoch"; quiet restore_pass
    ! cmp -s "$authority/epoch" "$work/unknown-pass-epoch" || fail 'explicit new pass certified old epoch'
  fi
  reset_owner
done
rm -f "$work/release-renamed"; printf 'restore\nrecovery\nversion\ncounts\ngrants\nschema\nroles\nsmoke\n' >"$work/expected-stages"; tail -n 8 "$work/db-stages" >"$work/latest-stages"
cmp -s "$work/expected-stages" "$work/latest-stages" || fail 'pass omitted fixed database checks'
for stage in restore recovery version counts grants schema roles smoke; do
  : >"$work/db-stages"; NOTICE_DB_FAIL=$stage refuse restore_pass
  jq -e '.operation_kind=="notice_restore"' /var/lib/aboutme/fence.json >/dev/null || fail 'database failure released owner'
  [[ -f $authority/restore-reconciliation && ! -e $authority/send.json && ! -e $authority/activation.json ]] || fail 'database failure lost denial'
  denied; refuse "$controller" host-pass latest --resume; reset_owner
done
for signal in HUP TERM INT; do
  NOTICE_SIGNAL=$signal NOTICE_DB_SIGNAL=restore blocked_pass
done
# A surviving restore descendant must keep the kernel lock after either controller dies. The latch is a fixture FIFO, never a protocol/FD token.
mkfifo "$work/continue"; exec {continue_fd}<>"$work/continue"
for victim in outer child; do
  rm -f "$work/held-child" "$work/held-controller"
  NOTICE_DB_HOLD=restore "$controller" host-pass latest >"$work/death-out" 2>"$work/death-err" &
  outer_pid=$!
  for ((attempt = 0; attempt < 100; attempt++)); do
    [[ ! -f $work/held-child ]] || break; /usr/bin/sleep 0.02
  done
  [[ -f $work/held-child ]] || fail 'restore descendant never reached latch'
  if [[ $victim == outer ]]; then kill -KILL "$outer_pid"
  else kill -KILL "$(cat "$work/held-controller")"; fi
  if /usr/bin/flock -n /run/aboutme/workload.lock true; then fail 'controller death lost descendant workload ownership'; fi
  [[ -f $authority/restore-reconciliation ]] || fail 'controller death lost reconciliation'
  jq -e '.operation_kind=="notice_restore"' /var/lib/aboutme/fence.json >/dev/null || fail 'controller death released operation'
  printf 'continue\n' >&"$continue_fd"; wait "$outer_pid" 2>/dev/null || true
  for ((attempt = 0; attempt < 100; attempt++)); do
    if /usr/bin/flock -n /run/aboutme/workload.lock true; then break; fi
    /usr/bin/sleep 0.02
  done
  /usr/bin/flock -n /run/aboutme/workload.lock true || fail 'fixture left a surviving workload holder'; denied; refuse "$controller" host-pass latest --resume
  reset_owner
done
exec {continue_fd}>&-; cp "$authority/epoch" "$work/pre-drift-epoch"; NOTICE_DB_DRIFT=smoke refuse restore_pass; denied
cp "$work/pre-drift-epoch" "$authority/epoch"; reset_owner; quiet restore_pass; cp "$authority/epoch" "$work/previous-epoch"
quiet restore_pass; ! cmp -s "$authority/epoch" "$work/previous-epoch" || fail 'absent receipts skipped rotation'
# Every observed durable write boundary must fail closed and keep its owner.
rm -f "$work/sync-count" "$work/first-authority-sync" "$work/last-authority-sync"; quiet restore_pass; first_boundary=$(cat "$work/first-authority-sync")
boundaries=$(cat "$work/last-authority-sync"); ((boundaries - first_boundary >= 3)) || fail 'missing durable boundaries'
for ((boundary = first_boundary; boundary <= boundaries; boundary++)); do
  rm -f "$work/sync-count"; NOTICE_FAIL_SYNC=$boundary refuse restore_pass; [[ -f $authority/restore-reconciliation ]] || fail 'failure lost reconciliation'
  retained; refuse "$host" pre-restore --resume
  # A fixture restores the known baseline only after the failed process exits.
  reset_owner; /usr/bin/rm -f "$work/await-directory-sync"
done
NOTICE_FAIL_MOVE=1 refuse restore_pass; [[ -f $authority/restore-reconciliation ]] || fail 'rename failure lost marker'
denied; reset_owner
for ((boundary = first_boundary; boundary <= boundaries; boundary++)); do
  rm -f "$work/sync-count" "$work/await-directory-sync"; result=0
  NOTICE_CRASH_SYNC=$boundary restore_pass >"$work/crash-out" 2>"$work/crash-err" || result=$?; ((result != 0)) || fail 'crash injection did not interrupt operation'
  [[ -f $authority/restore-reconciliation && ! -s $work/crash-out ]] || fail 'crash lost denial or exposed output'; retained; refuse "$host" pre-restore --resume
  reset_owner
done
/usr/bin/rm -f "$work/await-directory-sync"; cp "$authority/epoch" "$work/saved-epoch"; rm "$authority/epoch"
refuse "$host" initialize; [[ ! -e $authority/epoch ]] || fail 'lost adopted state reinitialized'
cp "$work/saved-epoch" "$authority/epoch"; chmod 0444 "$authority/epoch"
jq 'del(.notice_authority)' "$work/adopted-fence" > /var/lib/aboutme/fence.json
refuse "$host" initialize; reset_owner
for state in 'null' '{}' '{"version":2,"state":"adopted"}' '{"version":1,"state":"other"}'; do
  jq --argjson state "$state" '.notice_authority=$state' "$work/adopted-fence" > /var/lib/aboutme/fence.json
  refuse "$host" initialize
done
reset_owner; initializing_id=$(jq -r .notice_authority.operation_id "$work/adopted-fence")
jq '.notice_authority.state="initializing" | .operation_id=("B"*43) | .operation_kind="notice_restore"
  | .operation_started_at="2026-10-19T00:00:00Z" | .operation_checked_at=.operation_started_at' \
  "$work/adopted-fence" > /var/lib/aboutme/fence.json
refuse "$host" initialize
jq --arg id "$initializing_id" '.operation_id=$id' /var/lib/aboutme/fence.json >"$work/resume-fence"
cp "$work/resume-fence" /var/lib/aboutme/fence.json; rm "$authority/epoch"; quiet "$host" initialize
jq -e --arg id "$initializing_id" '.notice_authority.state=="adopted" and .notice_authority.operation_id==$id' \
  /var/lib/aboutme/fence.json >/dev/null || fail 'initialization changed recorded owner'
cp /var/lib/aboutme/fence.json "$work/adopted-fence"
for object in symlink hardlink fifo writable; do
  rm -f "$authority/send.json"
  case $object in
    symlink) ln -s "$authority/epoch" "$authority/send.json" ;;
    hardlink) ln "$authority/epoch" "$authority/send.json" ;;
    fifo) mkfifo "$authority/send.json" ;;
    writable) printf '{}\n' >"$authority/send.json"; chmod 0666 "$authority/send.json" ;;
  esac
  refuse restore_pass; reset_owner
done
rm -f "$authority/send.json"; chmod 0777 "$authority"; refuse restore_pass; chmod 0755 "$authority"
reset_owner; refuse "$host" adopt extra; refuse "$host" adopt --force; refuse "$host" adopt
[[ ! -e $work/forbidden ]] || fail 'host primitive called application dependency'

# Synthetic adoption uses a copied reviewed program with literal substitutions. The real rollout needs the primary's verified legacy pins before shipping.
mkdir "$work/legacy" "$work/reviewed"
for name in fence deploy-host deploy-safety; do
  git -c safe.directory="$root" -C "$root" show "ae548c5bfbcc506bd12cd45305c1f21445882e9c:deploy/vn/scripts/$name.sh" >"$work/legacy/$name.sh"
  cp "$root/deploy/vn/scripts/$name.sh" "$work/reviewed/$name.sh"
done
cp "$root/deploy/vn/host/workload-lock.sh" "$work/reviewed/"; chmod 0755 "$work/legacy/"*.sh "$work/reviewed/"*.sh; legacy_tag=v0.7.9
legacy_server=ghcr.io/dannyota/aboutme-server@sha256:$(printf '%064d' 1); legacy_web=ghcr.io/dannyota/aboutme-web@sha256:$(printf '%064d' 2)
jq -n --arg server "$legacy_server" --arg web "$legacy_web" \
  '{release_tag:"v0.7.9",release_number:7009,maintenance_tag:"v0.7.9",maintenance_number:7009,
    server_image:$server,web_image:$web,caddy_image:("ghcr.io/dannyota/aboutme-caddy@sha256:"+("0"*64)),
    maintenance_image:("ghcr.io/dannyota/aboutme-caddy@sha256:"+("0"*64))}' >"$work/legacy/release.json"
legacy_values=("$legacy_tag" "$(sha256sum "$work/legacy/release.json" | cut -d' ' -f1)"
  "$legacy_server" "$legacy_web"
  "$(sha256sum "$work/legacy/fence.sh" | cut -d' ' -f1)"
  "$(sha256sum "$work/legacy/deploy-host.sh" | cut -d' ' -f1)"
  "$(sha256sum "$work/legacy/deploy-safety.sh" | cut -d' ' -f1)")
cp "$primitive" "$work/reviewed/crowdsec-privacy-notice-host.sh"
for index in "${!legacy_fields[@]}"; do
  sed -i "s|^readonly ${legacy_fields[$index]}='[^']*'$|readonly ${legacy_fields[$index]}='${legacy_values[$index]}'|" \
    "$work/reviewed/crowdsec-privacy-notice-host.sh"
done
adopt=$work/reviewed/crowdsec-privacy-notice-host.sh
for command in cp install; do
  stub "$work/bin/$command" <<'STUB'
set -euo pipefail
if [[ ${NOTICE_ADOPTION:-0} == 1 ]]; then
  case ${*: -1} in
    /usr/local/lib/aboutme/fence.sh | /usr/local/lib/aboutme/deploy-host.sh | /usr/local/lib/aboutme/deploy-safety.sh)
      jq -e '.notice_authority.state=="initializing" and .operation_kind=="cutover" and .notice_authority.operation_id==.operation_id' \
        /var/lib/aboutme/fence.json >/dev/null || exit 1
      jq -e '.mutation_kind=="quiesce" and .state=="active"' /var/lib/aboutme/cutover-mutation.json >/dev/null || exit 1
      printf 'helper-replaced\n' >>"$NOTICE_FIXTURE/order"
      ;;
  esac
fi
exec "/usr/bin/${0##*/}" "$@"
STUB
done
# Remove only the synthetic test state after all its owning processes ended.
rm -rf -- "$authority"; rm -f /var/lib/aboutme/fence.json /var/lib/aboutme/fence.lock; cp "$work/legacy/"*.sh "$work/lib/"; cp "$work/legacy/release.json" /var/lib/aboutme/release.json
"$fence" init >"$work/out" 2>"$work/err"
for fault in unknown-helper unknown-release; do
  if [[ $fault == unknown-helper ]]; then
    printf '\n# synthetic mismatch\n' >>"$work/lib/deploy-host.sh"
  else
    jq '.release_tag="v0.7.10" | .release_number=7010' "$work/legacy/release.json" > /var/lib/aboutme/release.json
  fi
  refuse bash "$adopt" adopt; [[ ! -e $authority ]] || fail 'ineligible adoption touched authority'
  cp "$work/legacy/"*.sh "$work/lib/"; cp "$work/legacy/release.json" /var/lib/aboutme/release.json
done
NOTICE_ADOPTION=1 quiet bash "$adopt" adopt; [[ -f $authority/restore-reconciliation && -f $authority/epoch ]] || fail 'eligible adoption did not establish denial'
jq -e '.notice_authority.state=="adopted" and .minimum_release==4007' /var/lib/aboutme/fence.json >/dev/null || fail 'adoption evidence or floor'
refuse bash "$adopt" adopt
for field in "${legacy_fields[@]}"; do
  export "$field=caller_override"
done
refuse bash "$adopt" adopt
for field in "${legacy_fields[@]}"; do unset "$field"; done

# The app fixture emits only closed aggregates. It never simulates delivery.
cp "$root/deploy/vn/scripts/fence.sh" "$root/deploy/vn/scripts/deploy-safety.sh" "$work/lib/"; mkdir -p "$work/units" "$work/etc" /etc/containers/systemd /etc/aboutme
mount --bind "$work/units" /etc/containers/systemd; mount --bind "$work/etc" /etc/aboutme; : >"$work/etc/server.env"
: >"$work/etc/flags.env"
jq '.release_tag="v0.999.999" | .release_number=999999' "$work/legacy/release.json" > /var/lib/aboutme/release.json
sed -e 's/@RELEASE_TAG@/v0.999.999/g' -e 's/@RELEASE_NUMBER@/999999/g' \
  -e "s|@SERVER_IMAGE@|$legacy_server|g" -e "s|@WEB_IMAGE@|$legacy_web|g" \
  -e '/@SERVER_SECRETS@/d' "$root/deploy/vn/host/quadlet/aboutme-server.container.in" >"$work/units/aboutme-server.container"
printf '[Container]\nImage=%s\n' "$legacy_web" >"$work/units/aboutme-web.container"; : >"$work/db-stages"
refuse "$controller" host-pass latest; [[ ! -s $work/db-stages ]] || fail 'drill controller restored a full application host'
stub "$work/bin/podman" <<'STUB'
set -euo pipefail
if [[ $1 == secret && $2 == exists && $# == 3 ]]; then exit 0; fi
[[ $1 == run ]] || exit 1; printf '%s\n' "$@" >"$NOTICE_FIXTURE/container-args"
[[ $* == *'--network=none'* && $* == *'--memory=256m'* ]] || exit 1
case $* in
  *SMTP* | *MEDIA* | *smtp.env* | *--privileged* | *--network=host*) exit 1 ;;
esac
[[ $* == *'privacy-notice:/var/lib/aboutme/privacy-notice:ro'* ]] || exit 1; [[ $* == *'server crowdsec-privacy-notice '* ]] || exit 1
if [[ $* != *' crowdsec-privacy-notice enqueue'* ]]; then
  case $* in *auth-email-active-key* | *auth-email-previous-key*) exit 1 ;; esac
fi
[[ ${NOTICE_APP_ERROR:-0} == 0 ]] || { printf 'fixture_failure\n' >&2; exit 1; }; cat "$NOTICE_FIXTURE/aggregate"
exit "$(cat "$NOTICE_FIXTURE/app-exit")"
STUB
stub "$work/bin/date" <<'STUB'
case " $* " in
  *' -d '* | *' --date='*) exec /usr/bin/date "$@" ;;
  *) exec /usr/bin/date --date=2026-11-01T00:00:00Z "$@" ;;
esac
STUB
digest=30746a25e9d1e8c8e638d22383f1b0acf1b0848f4eb2c283dd74f459db0ba619
literal() {
  sed -nE "s/^(const )?[[:space:]]*$1[[:space:]]*=[[:space:]]*\"([^\"]*)\"$/\\2/p" "$2"
}
manifest_source=$root/apps/server/internal/privacynotice/manifest.go; copy_source=$root/apps/server/internal/authmail/privacy_notice_copy.go
{
  printf '%s\n' aboutme-crowdsec-privacy-content-v1 campaign_id=crowdsec-postgres-paths-v1; printf 'publication_date=%s\n' "$(literal PublicationDate "$manifest_source")"
  printf 'canonical_link=%s\n' "$(literal PrivacyNoticeURL "$copy_source")"; printf 'public_en=%s\n' "$(literal publicEN "$manifest_source")"
  printf 'public_vi=%s\n' "$(literal publicVI "$manifest_source")"
  for pair in email_subject:PrivacyNoticeSubject email_button:PrivacyNoticeAction \
    email_heading_vi:PrivacyNoticeHeadingVI email_heading_en:PrivacyNoticeHeadingEN \
    email_preheader:PrivacyNoticePreheader email_body_vi:PrivacyNoticeBodyVI email_body_en:PrivacyNoticeBodyEN; do
    printf '%s=%s\n' "${pair%%:*}" "$(literal "${pair#*:}" "$copy_source")"
  done
} >"$work/manifest"; [[ $(wc -c <"$work/manifest") == 4683 && $(wc -l <"$work/manifest") == 13 ]] || fail 'manifest bytes'
[[ $(sha256sum "$work/manifest" | cut -d' ' -f1) == "$digest" ]] || fail 'independent manifest digest'
jq -n --arg digest "$digest" '{schema_version:1,result:"pending",campaign_id:"crowdsec-postgres-paths-v1",
  content_sha256:$digest,cutoff_at:"2026-10-19T01:02:03.000000Z",expected_count:1,state:"sealed",actual_count:1,
  counts:{unqueued:1,queued:0,accepted:0,deleted:0,failed:0,cancelled:0},
  failure_counts:{expired:0,attempts_exhausted:0,provider_rejected:0,payload_invalid:0,job_missing:0,delivery_failed:0},
  recovery_count:0,recovery_counts:{expired:0,attempts_exhausted:0,provider_rejected:0,payload_invalid:0,job_missing:0,delivery_failed:0},
  activation_count:0,sealed_at:"2026-10-19T01:02:04.000000Z",first_activated_at:null,activated_at:null,
  aborted_at:null,last_accepted_at:null}' >"$work/sealed"
cp "$work/sealed" "$work/aggregate"; printf '2\n' >"$work/app-exit"
app() {
  local expected=$1 result=0; shift
  bash "$wrapper" "$@" >"$work/out" 2>"$work/err" || result=$?; [[ $result == "$expected" && ! -s $work/err ]] || fail 'owner command result'
  if [[ $1 == authorize-send ]]; then
    [[ ! -s $work/out ]] || fail 'host authorization exposed output'
  else
    [[ $(wc -c <"$work/out") -le 4096 && $(tail -c 1 "$work/out") == '' ]] || fail 'aggregate output framing'
    [[ $(jq -Sc . "$work/out") == "$(jq -Sc . "$work/aggregate")" ]] || fail 'aggregate output changed'
  fi
}
# Compatible ordinary mail may restart with reconciliation retained; only notice claim/send and changed ingestion authority remain denied. Actual SMTP
# claim/send gates are exercised by the backend worker tests in full CI.
"$fence" check 999999 aboutme-server >"$work/out" 2>"$work/err" || fail 'notice marker blocked compatible ordinary mail'
refuse bash "$wrapper" enqueue; refuse bash "$wrapper" authorize-send
# Independent reconciliation is represented only by fixture setup, never by a host operation deriving success from a restored queue.
rm "$authority/restore-reconciliation"
# Capture must persist before writer shutdown, remain on a pending batch, and clear only after the sealed postcondition. The fixture has no member data.
jq '.state="capturing" | .actual_count=0 | .counts.unqueued=0 | .sealed_at=null' "$work/sealed" >"$work/aggregate"
NOTICE_CAPTURE=1 app 2 prepare; [[ -f $authority/capture-in-progress && ! -s $authority/capture-in-progress ]] || fail 'pending capture lost marker'
[[ $(stat -c '%u:%g:%a:%h' "$authority/capture-in-progress") == 0:0:444:1 ]] || fail 'capture marker metadata'; NOTICE_APP_STATE=inspection-failure refuse bash "$wrapper" prepare
[[ -f $authority/capture-in-progress ]] || fail 'capture refusal lost marker'; cp "$work/sealed" "$work/aggregate"
jq '.result="sealed"' "$work/sealed" >"$work/aggregate"
printf '0\n' >"$work/app-exit"; NOTICE_CAPTURE=1 app 0 prepare; [[ ! -e $authority/capture-in-progress ]] || fail 'sealed capture retained marker'
jq '.result="sealed" | .expected_count=0 | .actual_count=0 | .counts.unqueued=0' "$work/sealed" >"$work/aggregate"
app 0 prepare; [[ ! -e $authority/capture-in-progress ]] || fail 'zero cohort did not seal'
cp "$work/sealed" "$work/aggregate"; printf '2\n' >"$work/app-exit"; app 0 authorize-send
[[ -f $authority/send.json ]] || fail 'send authorization missing'; cp "$authority/send.json" "$work/current-send"; app 2 enqueue
jq '.counts={unqueued:0,queued:1,accepted:0,deleted:0,failed:0,cancelled:0}' "$work/sealed" >"$work/aggregate"
app 2 enqueue
jq '.result="queue_full"' "$work/aggregate" >"$work/full"
cp "$work/full" "$work/aggregate"; app 2 enqueue; [[ ! -e $authority/activation.json ]] || fail 'queue wait activated ingestion'
cp "$work/sealed" "$work/aggregate"; app 2 activate; [[ ! -e $authority/activation.json ]] || fail 'unresolved ledger activated ingestion'
app 0 authorize-send; cp "$authority/send.json" "$work/current-send"
for mutation in '.extra=true' '.schema_version=2' '.expected_count=2' \
  '.content_sha256=("0"*64)' '.state="aborted"' '.state="retired"' \
  '.cutoff_at="2026-10-19T01:02:03Z"' '.counts.failed=1' \
  '.recovery_count=9223372036854775808' '.actual_count=0'; do
  jq "$mutation" "$work/sealed" >"$work/aggregate"
  refuse bash "$wrapper" authorize-send; cmp -s "$authority/send.json" "$work/current-send" || fail 'invalid aggregate changed authority'
done
for contents in '{}' '{} {}' '{"schema_version":1,"schema_version":1}' '{bad}'; do
  printf '%s\n' "$contents" >"$work/aggregate"; refuse bash "$wrapper" authorize-send
done
printf '\377\n' >"$work/aggregate"; refuse bash "$wrapper" authorize-send; head -c 4097 /dev/zero >"$work/aggregate"; refuse bash "$wrapper" authorize-send
cp "$work/sealed" "$work/aggregate"; NOTICE_APP_ERROR=1 refuse bash "$wrapper" authorize-send; printf '0\n' >"$work/app-exit"; refuse bash "$wrapper" authorize-send
printf '2\n' >"$work/app-exit"
# A valid aggregate cannot authorize a different deployed release or image.
cp /var/lib/aboutme/release.json "$work/current-release"
jq '.release_number=1' "$work/current-release" > /var/lib/aboutme/release.json
refuse bash "$wrapper" authorize-send; cp "$work/current-release" /var/lib/aboutme/release.json; cp "$work/units/aboutme-server.container" "$work/current-unit"
sed -i "s|$legacy_server|ghcr.io/dannyota/aboutme-server@sha256:$(printf '%064d' 9)|" "$work/units/aboutme-server.container"
refuse bash "$wrapper" authorize-send; cp "$work/current-unit" "$work/units/aboutme-server.container"
for mutation in '.epoch="00000000-0000-4000-8000-000000000000"' '.expected_count=2' \
  '.content_sha256=("0"*64)' '.issued_at="9999-01-01T00:00:00.000000Z"' '.extra=true'; do
  jq "$mutation" "$work/current-send" >"$authority/send.json"
  chmod 0444 "$authority/send.json"; refuse bash "$wrapper" enqueue
done
printf '{}\n' >"$authority/send.json"; refuse bash "$wrapper" enqueue; cp "$work/current-send" "$authority/send.json"
jq '.result="activated" | .state="activated" | .counts={unqueued:0,queued:0,accepted:1,deleted:0,failed:0,cancelled:0}
  | .activation_count=1 | .first_activated_at="2026-10-19T01:02:05.000000Z"
  | .activated_at=.first_activated_at | .last_accepted_at=.first_activated_at' "$work/sealed" >"$work/aggregate"
printf '0\n' >"$work/app-exit"; rm -f "$work/joined" "$work/stopped"; : >"$work/order"
NOTICE_CLAIMED=1 app 0 activate; [[ -f $authority/activation.json && ! -e $authority/send.json ]] || fail 'activation authority transition'
awk '$0=="stop" {stopped=NR} $0=="join" {joined=NR} $0=="rm send.json" {revoked=NR} END {exit !(stopped && joined>stopped && revoked>joined)}' \
  "$work/order" || fail 'claimed worker not joined before revocation'
cp "$authority/activation.json" "$work/current-activation"; app 0 activate; cmp -s "$authority/activation.json" "$work/current-activation" || fail 'reconstruction changed activation'
# Acceptance remains valid after outbox cleanup. The host receives no outbox rows, and the pinned clock is more than seven days after acceptance.
[[ $(date -u +%Y-%m-%d) == 2026-11-01 ]] || fail 'delayed activation clock'; rm "$authority/activation.json"; app 0 activate
cmp -s "$authority/activation.json" "$work/current-activation" || fail 'delayed reconstruction changed evidence'
touch "$authority/restore-reconciliation"; chmod 0444 "$authority/restore-reconciliation"; refuse bash "$wrapper" authorize-send; refuse bash "$wrapper" activate
rm "$authority/restore-reconciliation"
# Known late failure blocks reactivation even with a past accepted result.
rm "$authority/activation.json"
jq '.result="blocked" | .state="sealed" | .activated_at=null | .last_accepted_at=null
  | .counts.accepted=0 | .counts.failed=1 | .failure_counts.delivery_failed=1' "$work/aggregate" >"$work/late-failure"
cp "$work/late-failure" "$work/aggregate"; printf '1\n' >"$work/app-exit"; app 1 activate
[[ ! -e $authority/activation.json && ! -e $authority/send.json ]] || fail 'late failure reopened authority'
for state in aborted retired; do
  jq --arg state "$state" '.result="blocked" | .state=$state | .activated_at=null
    | .aborted_at="2026-10-19T01:02:06.000000Z"' "$work/aggregate" >"$work/blocked"
  cp "$work/blocked" "$work/aggregate"; printf '1\n' >"$work/app-exit"; app 1 status; refuse bash "$wrapper" authorize-send
  [[ ! -e $authority/send.json ]] || fail 'terminal campaign received send authority'
done
printf 'PASS notice guest filesystem protocol\n'; printf 'notice reconciliation: not performed\n'
