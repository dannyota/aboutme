#!/usr/bin/env bash
set -euo pipefail
umask 077
here=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=deploy/vn/host/operation-state.sh
source "$here/host/operation-state.sh"
# shellcheck source=deploy/vn/host/operation-account.sh
source "$here/host/operation-account.sh"
fail() { echo "host-operations-failure-test: $1" >&2; exit 1; }
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
id=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA
user=018f5b6a-9a3e-7c21-8b1e-000000000030
for rc in 0 3 4 5 125; do
  printf 'deleted user=%s slugs=valid-slug\naccount-delete: end\n' "$user" >"$work/output"
  result=$(account_parse_write "$rc" "$work/output" "$user" valid-slug)
  if [[ $rc == 0 ]]; then [[ $(jq -r .writeOutcome <<<"$result") == committed ]] || fail committed
  else [[ $(jq -r .writeOutcome <<<"$result") == unknown ]] || fail 'exit mismatch authorized result'; fi
done
printf 'deleted user=%s slugs=valid-slug\n' "$user" >"$work/output"
[[ $(account_parse_write 0 "$work/output" "$user" valid-slug | jq -r .writeOutcome) == unknown ]] || fail 'missing marker'
printf 'not found: valid-slug\naccount-delete: end\n' >"$work/output"
[[ $(account_parse_write 4 "$work/output" "$user" valid-slug | jq -r .writeOutcome) == not_found ]] || fail 'not found marker'
printf 'mismatch: valid-slug is not owned by %s; nothing deleted\naccount-delete: end\n' "$user" >"$work/output"
[[ $(account_parse_write 5 "$work/output" "$user" valid-slug | jq -r .writeOutcome) == mismatch ]] || fail mismatch
printf 'deleted user=%s slugs=valid-slug\naccount-delete: end\nextra\n' "$user" >"$work/output"
[[ $(account_parse_write 0 "$work/output" "$user" valid-slug | jq -r .writeOutcome) == unknown ]] || fail 'extra output'
for output in '{"success":true,"state":"absent"}' '{"success":true,"state":"present"}'; do
  account_parse_status "$output" >/dev/null || fail 'status snapshot'
done
for output in '{"success":false,"state":"absent"}' '{"success":true,"state":"absent","extra":0}' '{}'; do
  if account_parse_status "$output" >/dev/null 2>&1; then fail 'unknown status accepted'; fi
done
for output in '{"success":true,"pending":-1}' '{"success":false,"pending":0}' '{"success":true,"pending":0,"extra":1}'; do
  if account_parse_queue "$output" >/dev/null 2>&1; then fail 'false global zero'; fi
done
[[ $(account_parse_queue '{"success":true,"pending":0}') == 0 ]] || fail zero
[[ $(account_parse_queue '{"success":true,"pending":2}') == 2 ]] || fail nonzero
# Resume must contain only the read command, never the destructive command.
body=$(sed -n '/^account_reconcile()/,/^}/p' "$here/host/operation-account.sh")
[[ $body == *'account-delete status'* && $body != *'account-delete confirm'* ]] || fail 'unknown replay'
grep -qF '180' "$here/host/operation-state.sh" || fail 'account evidence retention'
grep -qF '30' "$here/host/operation-state.sh" || fail 'attempt metadata retention'
echo 'host operation result reconciliation passed'

# Exercise the durable state implementation under the CI user's UID. Only the
# root ownership expectation changes; modes, links, bounds and IO stay real.
python3 - "$here/host/operation-state.sh" "$work/state.py" <<'PY'
import pathlib, sys
source = pathlib.Path(sys.argv[1]).read_text().split("<<'PY'\n", 1)[1].split('\nPY\n', 1)[0]
pathlib.Path(sys.argv[2]).write_text(source.replace('s.st_uid != 0', 's.st_uid != os.getuid()'))
PY
operations_dir=$work/records
mkdir -m 0700 "$operations_dir"
# Redirect installed paths and the root UID expectation only in this private
# test copy; modes, links, state IO and request dispatch stay real.
sed -e "s|^lib=/usr/local/lib/aboutme$|lib=$here/host|" \
  -e "s|EUID == 0|EUID == $(id -u)|" "$here/host/operation-run.sh" >"$work/runner.sh"
# shellcheck source=deploy/vn/host/operation-run.sh
source "$work/runner.sh"
# Use the fixture state driver after loading installed helper definitions.
operation_state() { python3 "$work/state.py" "$1" "${2:-}" "$operations_dir"; }
new_state() {
  local request=$1
  jq -cn --arg id "$request" --arg user "$user" --arg at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" '
    {schemaVersion:1,requestId:$id,kind:"account-delete",phase:"planned",outcome:"pending",createdAt:$at,
    expiresAt:"2099-01-01T00:00:00Z",planningDeadline:$at,evidenceAt:$at,
    confirmation:("delete-account:"+$id+":"+$user+":valid-slug"),flagsHash:("a"*64),dbSecretId:"synthetic",
    release:{release_tag:"v0.7.9",release_number:7009,maintenance_tag:"v0.7.9",maintenance_number:7009,
    server_image:("ghcr.io/dannyota/aboutme-server@sha256:"+("a"*64)),web_image:("ghcr.io/dannyota/aboutme-web@sha256:"+("b"*64)),
    caddy_image:("ghcr.io/dannyota/aboutme-caddy@sha256:"+("c"*64)),maintenance_image:("ghcr.io/dannyota/aboutme-caddy@sha256:"+("d"*64))},
    action:{userId:$user,reportedSlug:"valid-slug",shownSlugs:["valid-slug"],returnedSlugs:null,writeOutcome:"not_started",publicVerification:"pending",physicalCleanup:"pending"},
    challenge:null,recoveryCopy:null,proofReceipt:null,attemptCount:0,attempts:[],intent:"none",pendingVerb:"none",fenceHeld:false,reserved:true,closureQuiescent:true}'
}
request_id() { printf '%042dA' "$1"; }
# Unknown requests have no state, lock, fence or unit to change. Missing or
# unsafe files inside an existing request are protocol failures, not absence.
(
  operations_dir=$work/lookup-records
  mkdir -m 0700 "$operations_dir"
  operation_initialize() { :; }
  request_start() { touch "$work/unknown-dispatch"; }
  request_fence() { touch "$work/unknown-fence"; }
  expect_error() {
    local expected=$1 rc=0
    shift
    ("$@") >"$work/lookup-output" 2>"$work/lookup-error" || rc=$?
    [[ $rc == 1 && ! -s $work/lookup-output ]] || fail "lookup exit or stdout: $expected"
    printf '%s\n' "$expected" >"$work/expected-error"
    cmp -s "$work/expected-error" "$work/lookup-error" || fail "lookup diagnostic: $expected"
  }
  for mode in read load; do expect_error not_found operation_state "$mode" "$id"; done
  for verb in status resume verify abort apply proof; do
    envelope=$(jq -cn --arg verb "$verb" --arg id "$id" --arg user "$user" '
      {verb:$verb,requestId:$id} +
      (if $verb=="apply" then {confirmation:("delete-account:"+$id+":"+$user+":valid-slug")}
       elif $verb=="proof" then {phase:"serving",nonce:$id,result:"pass"} else {} end)')
    expect_error not_found operation_main request <<<"$envelope"
  done
  expect_error not_found operation_main run "$id"
  [[ ! -e $operations_dir/$id && ! -e $work/unknown-dispatch && ! -e $work/unknown-fence ]] || fail 'unknown lookup created work'
  # Inject EIO at the request-directory lookup. Absence must come from errno,
  # and failed metadata reads must stop before opening a lock or dispatching.
  (
    operation_state() {
      python3 - "$work/state.py" "$1" "${2:-}" "$operations_dir" <<'PY'
import errno, os, runpy, sys
driver = sys.argv.pop(1)
request_path = sys.argv[3]+'/'+sys.argv[2]
real_lstat = os.lstat
def lookup(path, *args, **kwargs):
    if path == request_path: raise OSError(errno.EIO, 'synthetic lookup failure')
    return real_lstat(path, *args, **kwargs)
os.lstat = lookup
runpy.run_path(driver, run_name='__main__')
PY
    }
    for presence in missing existing; do
      [[ $presence != existing ]] || mkdir -m 0700 "$operations_dir/$id"
      for mode in read load; do expect_error protocol_failed operation_state "$mode" "$id"; done
      for verb in status resume verify abort apply proof; do
        envelope=$(jq -cn --arg verb "$verb" --arg id "$id" --arg user "$user" '
          {verb:$verb,requestId:$id} +
          (if $verb=="apply" then {confirmation:("delete-account:"+$id+":"+$user+":valid-slug")}
           elif $verb=="proof" then {phase:"serving",nonce:$id,result:"pass"} else {} end)')
        expect_error protocol_failed operation_main request <<<"$envelope"
      done
      expect_error protocol_failed operation_main run "$id"
      [[ ! -e $operations_dir/$id/.request.lock && ! -e $work/unknown-dispatch && ! -e $work/unknown-fence ]] || fail 'failed lookup created work'
    done
    rmdir "$operations_dir/$id"
  )
  mkdir -m 0700 "$operations_dir/$id"
  for mode in read load; do
    expect_error protocol_failed operation_state "$mode" "$id"
    printf '{}\n' >"$operations_dir/$id/state.json"
    expect_error protocol_failed operation_state "$mode" "$id"
    rm "$operations_dir/$id/state.json"
    ln -s "$work/absent-state" "$operations_dir/$id/state.json"
    expect_error protocol_failed operation_state "$mode" "$id"
    rm "$operations_dir/$id/state.json"
  done
  envelope=$(jq -cn --arg id "$id" '{verb:"status",requestId:$id}')
  expect_error protocol_failed operation_main request <<<"$envelope"
  rmdir "$operations_dir/$id"
  ln -s "$work/absent-request" "$operations_dir/$id"
  expect_error protocol_failed operation_state read "$id"
  expect_error protocol_failed request_existing resume
  rm "$operations_dir/$id"
  chmod 0755 "$operations_dir"
  expect_error protocol_failed operation_state read "$id"
  chmod 0700 "$operations_dir"
  rmdir "$operations_dir"
  expect_error protocol_failed operation_state read "$id"
  mkdir -m 0700 "$operations_dir"
  operation_state new '' 3<<<"$(new_state "$id")" >/dev/null
  cp "$operations_dir/$id/state.json" "$work/known-state"
  operation_main request <<<"$envelope" >"$work/known-output" 2>"$work/known-error" || fail 'known status refused'
  [[ ! -s $work/known-error ]] || fail 'known status emitted an error'
  operation_state wire '' 3<"$work/known-output" >/dev/null || fail 'known status changed its wire schema'
  cmp -s "$work/known-state" "$operations_dir/$id/state.json" || fail 'status changed known state'
)
echo 'host operation unknown requests and damaged state passed'

# Concurrent admission may reserve exactly 32 slots, even across restarts.
for n in $(seq 1 36); do
  (request=$(request_id "$n"); operation_state new '' 3<<<"$(new_state "$request")" >"$work/admit-$n" 2>/dev/null || true) &
done
wait
count=$(find "$operations_dir" -mindepth 1 -maxdepth 1 -type d | wc -l)
[[ $count == 32 ]] || fail 'concurrent admission overbooked or lost capacity'
if operation_state new '' 3<<<"$(new_state "$id")" >/dev/null 2>&1; then fail 'restart lost reservation accounting'; fi
# Finalizing successful shows releases fences while retaining plan capacity.
(
  lib=$work/plan-lib
  mkdir -p "$lib"
  printf 'operation_state() { cat "$RELEASE_STATE"; }\n' >"$lib/release-state.sh"
  sed "s|/usr/local/lib/aboutme/operation-state.sh|$lib/release-state.sh|g" "$here/scripts/deploy-safety.sh" >"$lib/release-safety.sh"
  export PLAN_SAFETY=$lib/release-safety.sh
  cat >"$lib/fence.sh" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=deploy/vn/scripts/deploy-safety.sh
source "$PLAN_SAFETY"
mountpoint() { return 0; }
systemctl() { [[ " $* " == *' --all '* ]] || return 1; printf 'ActiveState=inactive\nJob=\n'; }
podman() { :; }
timeout() { shift; "$@"; }
runuser() { cat >/dev/null; printf '0\n'; }
case $1 in
  read) cat "$PLAN_FENCE" ;;
  release)
    aboutme_assert_operation_release "$2" || exit 1
    printf '{"operation_id":null}\n' >"$PLAN_FENCE"
    ;;
  *) exit 1 ;;
esac
STUB
  chmod +x "$lib/fence.sh"
  export PLAN_FENCE=$work/plan-fence
  systemctl() { printf 'ActiveState=inactive\nJob=0\n'; }
  for path in "$operations_dir"/*; do
    [[ -d $path ]] || continue
    id=${path##*/}
    operation_load "$id"
    state=$(jq '.fenceHeld=true | .pendingVerb="show"' <<<"$state")
    operation_save
    jq -cn --arg id "$id" '{operation_id:$id}' >"$PLAN_FENCE"
    export RELEASE_STATE=$operations_dir/$id/state.json
    request_finish || fail 'successful show finalization refused'
    [[ $(jq -r .fenceHeld <<<"$state") == false && $(jq -r .operation_id "$PLAN_FENCE") == null ]] || fail 'planning fence retained'
    [[ $(jq -r .reserved <<<"$state") == true && $(jq -r .phase <<<"$state") == planned ]] || fail 'applicable plan lost reservation'
  done
  id=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA
  if operation_state new '' 3<<<"$(new_state "$id")" >/dev/null 2>&1; then fail '33rd applicable plan admitted'; fi
  # Aborted plans close before returning their reservations.
  for path in "$operations_dir"/*; do
    [[ -d $path ]] || continue
    id=${path##*/}
    operation_load "$id"
    state=$(jq '.pendingVerb="abort"' <<<"$state")
    operation_save
    request_finish || fail 'quiescent plan abort refused'
    [[ $(jq -r .phase <<<"$state") == closed && $(jq -r .reserved <<<"$state") == false ]] || fail 'closed plan retained capacity'
  done
)
# Compact, closed records retain audit but return unused reservation bytes.
audit_count=$(find "$operations_dir" -name 'audit-*' | wc -l)
operation_state new '' 3<<<"$(new_state "$id")" >/dev/null || fail '33rd compact record refused'
[[ $(find "$operations_dir" -name 'audit-*' | wc -l) == "$audit_count" ]] || fail 'unexpired audit evicted'
# A held request lock makes retention skip that request, not race its writer.
exec {held}>"$operations_dir/$id/.request.lock"
flock "$held"
state=$(jq '.createdAt="2000-01-01T00:00:00Z" | .evidenceAt=.createdAt | .expiresAt=.createdAt | .closureQuiescent=false' "$operations_dir/$id/state.json")
printf '%s\n' "$state" >"$operations_dir/$id/state.json"
operation_state prune '' >/dev/null
[[ $(jq -r .action.userId "$operations_dir/$id/state.json") == "$user" ]] || fail 'retention raced active owner'
flock -u "$held"
operation_state prune '' >/dev/null
[[ $(jq -r .action.userId "$operations_dir/$id/state.json") == null ]] || fail '180-day target retained'
[[ $(jq -r .reserved "$operations_dir/$id/state.json") == true ]] || fail 'expiry freed uncertain attempt capacity'
# Expired plans become compact only after a controller proves quiescence.
state=$(operation_state load "$id" | jq '.phase="closed" | .outcome="unverified" | .reserved=false | .closureQuiescent=true')
operation_state save "$id" 3<<<"$state" >/dev/null
operation_state prune '' >/dev/null
[[ $(jq -r .reserved "$operations_dir/$id/state.json") == false ]] || fail 'safe closure kept reservation'
# Unsafe files, malformed versions and an unwritable audit path fail closed.
request=$(request_id 99)
new_state "$request" | jq '.schemaVersion=2' >"$work/bad"
if operation_state new '' 3<"$work/bad" >/dev/null 2>&1; then fail 'unknown schema accepted'; fi
ln -s "$work/bad" "$operations_dir/$id/unsafe"
if operation_state prune '' >/dev/null 2>&1; then fail 'symlink accepted'; fi
rm "$operations_dir/$id/unsafe"
chmod 0500 "$operations_dir/$id"
if operation_state save "$id" 3<<<"$state" >/dev/null 2>&1; then fail 'unwritable state admitted write'; fi
chmod 0700 "$operations_dir/$id"
# Expired, quiescent attempt metadata is removed at 30 days without extending
# or evicting personal audit. The attempt counter survives compaction.
request=$(request_id 97)
state=$(new_state "$request" | jq --arg id "$request" '.phase="closed" | .outcome="aborted" | .confirmation=null | .expiresAt=null | .reserved=false | .attemptCount=1 | .attempts=[{number:1,container:("aboutme-operation-"+$id+"-1"),applicationName:("hostop:"+$id+":1"),startedAt:"2000-01-01T00:00:00Z",stopped:true}]')
operation_state new '' 3<<<"$state" >/dev/null
operation_state prune '' >/dev/null
[[ $(jq '.attempts|length' "$operations_dir/$request/state.json") == 0 && $(jq .attemptCount "$operations_dir/$request/state.json") == 1 ]] || fail '30-day metadata retention'
# True byte capacity cannot evict retained records.
request=$(request_id 99)
truncate -s 67108864 "$operations_dir/$id/retained"
chmod 0600 "$operations_dir/$id/retained"
if operation_state new '' 3<<<"$(new_state "$request")" >/dev/null 2>&1; then fail 'full audit store admitted request'; fi
rm "$operations_dir/$id/retained"

id=$(request_id 98)
state=$(new_state "$id" | jq '.phase="accepted" | .confirmation=null | .expiresAt=null | .pendingVerb="apply"')
operation_state new '' 3<<<"$state" >/dev/null
operation_save() { printf '%s\n' "$state" >"$work/saved"; }
account_live() { return 0; }
request_release() { state=$(jq ' .fenceHeld=false | .reserved=(.phase=="planned") | .closureQuiescent=true | .pendingVerb="none"' <<<"$state"); operation_save; }
request_fence() { return 0; }
request_unit_stopped() { [[ ! -e $work/unit-active ]]; }
account_attempts_stopped() { [[ ! -e $work/backend-active ]]; }
request_lock() { exec {request_fd}>"$work/request.lock"; flock -w 2 "$request_fd"; state=$(<"$work/saved"); }
operation_output() { printf '%s\n' "$state"; }
request_start() { printf 'dispatch\n' >>"$work/dispatches"; }
account_container() {
  printf '%s\n' "$*" >>"$work/commands"
  command_rc=${stub_rc:-0} command_output=$work/output
  printf '%s\n' "${stub_output:-}" >"$command_output"
}
# SSH loss before dispatch leaves no effect; abort must close, never dispatch.
operation_save
(request_existing abort >/dev/null) || fail 'before-dispatch abort refused'
[[ ! -e $work/dispatches ]] || fail 'abort dispatched deletion'
# Crash after intent, before receipt: preserve unknown and issue a UUID read.
state=$(new_state "$id" | jq '.phase="accepted" | .confirmation=null | .expiresAt=null | .pendingVerb="apply" | .intent="delete"')
operation_save
(request_existing resume >/dev/null) || fail 'lost dispatch resume refused'
[[ $(jq -r .action.writeOutcome "$work/saved") == unknown ]] || fail 'intent did not become unknown'
[[ $(wc -l <"$work/dispatches") == 1 ]] || fail 'more than one reconciliation dispatch'
# Renamed slug does not authorize absence or a second destructive command.
state=$(<"$work/saved")
stub_output='{"success":true,"state":"present"}'
account_reconcile || fail 'present reconciliation failed'
[[ $(jq -r .action.writeOutcome <<<"$state") == present ]] || fail 'rename treated as absence'
[[ $(<"$work/commands") == *"account-delete status $user"* && $(<"$work/commands") != *confirm* ]] || fail 'destructive replay'
# Lost commit receipt plus absent UUID records only absent_after_unknown.
stub_output='{"success":true,"state":"absent"}'
account_reconcile || fail 'absent reconciliation failed'
[[ $(jq -r .action.writeOutcome <<<"$state") == absent_after_unknown ]] || fail 'claimed committed result'
# Backend/unit liveness and failed reads retain ownership.
touch "$work/backend-active"
operation_save
if (request_existing resume >/dev/null 2>&1); then fail 'live backend released'; fi
rm "$work/backend-active"
touch "$work/unit-active"
if (request_existing abort >/dev/null 2>&1); then fail 'live unit released'; fi
rm "$work/unit-active"
stub_rc=1
if account_reconcile; then fail 'failed status proved absence'; fi
stub_rc=0 stub_output='{}'
if account_reconcile; then fail 'unknown status proved absence'; fi
# Expired evidence may only abort after quiescence, without target lookup.
state=$(new_state "$id" | jq '.phase="accepted" | .confirmation=null | .expiresAt=null | .action.userId=null | .action.reportedSlug=null | .action.shownSlugs=[] | .action.returnedSlugs=[] | .action.writeOutcome="evidence_expired"')
operation_save
rm "$work/commands"
if (request_existing verify >/dev/null 2>&1); then fail 'expired verify accepted'; fi
request_release() { state=$(jq '.fenceHeld=false | .reserved=(.phase=="planned") | .closureQuiescent=true' <<<"$state"); operation_save; }
(request_existing abort >/dev/null) || fail 'expired abort refused'
[[ ! -e $work/commands && $(jq -r .outcome "$work/saved") == unverified ]] || fail 'expired abort queried target'

# Concurrent apply/resume/abort share the request flock. Only one apply wins.
state=$(new_state "$id")
operation_save
rm -f "$work/dispatches"
confirmation=$(jq -r .confirmation <<<"$state")
for verb in apply apply resume abort; do
  (
    request_start() { touch "$work/unit-active"; printf 'dispatch\n' >>"$work/dispatches"; }
    request_existing "$verb" "$confirmation" >/dev/null 2>&1 || true
  ) &
done
wait
[[ ! -e $work/dispatches || $(wc -l <"$work/dispatches") -le 1 ]] || fail 'concurrent duplicate dispatch'
rm -f "$work/unit-active"
echo 'host operation durable failure cases passed'

# A stale plan, confirmation drift or live-release drift cannot dispatch.
for reason in expired confirmation release; do
  state=$(new_state "$id")
  [[ $reason != expired ]] || state=$(jq '.expiresAt="2000-01-01T00:00:00Z"' <<<"$state")
  operation_save
  confirmation=$(jq -r .confirmation <<<"$state")
  [[ $reason != confirmation ]] || confirmation=${confirmation/valid-slug/other-slug}
  rm -f "$work/dispatches"
  (
    [[ $reason != release ]] || account_live() { return 1; }
    request_existing apply "$confirmation" >/dev/null 2>&1
  ) && fail 'stale or changed plan dispatched'
  [[ ! -e $work/dispatches ]] || fail 'refusal created an effect'
done
# A duplicate accepted apply must still bind the exact UUID and reported slug.
state=$(new_state "$id" | jq '.phase="accepted" | .confirmation=null | .expiresAt=null | .pendingVerb="apply"')
operation_save
confirmation="delete-account:$id:$user:other-slug"
if (request_existing apply "$confirmation" >/dev/null 2>&1); then fail 'duplicate rebound confirmation'; fi

# Queue reads prove physical completion only after UUID absence. Cleanup runs
# under a read attempt, then finalization releases the fence before sweep start.
account_container() {
  command_rc=0 command_output=$work/output
  case $1 in
    show-verify) command_rc=4; printf 'not found: %s\naccount-delete: end\n' "$4" >"$command_output" ;;
    status) printf '{"success":true,"state":"%s"}\n' "${presence:-absent}" >"$command_output" ;;
    queue) command_rc=${queue_rc:-0}; printf '%s\n' "$queue_output" >"$command_output" ;;
    *) fail 'cleanup attempted a write' ;;
  esac
}
for queue_output in '{"success":true,"pending":2}' '{"success":true,"pending":0}' '{}'; do
  state=$(new_state "$id" | jq '.phase="serving" | .confirmation=null | .expiresAt=null | .action.writeOutcome="committed"')
  queue_rc=0
  if account_cleanup; then
    expected=pending
    [[ $queue_output != '{"success":true,"pending":0}' ]] || expected=complete
    [[ $(jq -r .action.physicalCleanup <<<"$state") == "$expected" ]] || fail 'queue completion mismatch'
  else
    [[ $queue_output == '{}' ]] || fail 'valid queue snapshot refused'
    [[ $(jq -r .action.physicalCleanup <<<"$state") == pending ]] || fail 'failed queue emitted zero'
  fi
done
queue_rc=1 queue_output='{"success":true,"pending":0}'
if account_cleanup; then fail 'nonzero queue exit completed cleanup'; fi
queue_rc=0 presence=present
if account_cleanup; then fail 'present account completed cleanup'; fi
presence=absent
request_release() { printf 'release\n' >>"$work/order"; state=$(jq '.fenceHeld=false | .reserved=(.phase=="planned") | .closureQuiescent=true | .pendingVerb="none"' <<<"$state"); operation_save; }
systemctl() { printf 'sweep\n' >>"$work/order"; }
state=$(jq '.pendingVerb="resume"' <<<"$state")
request_finish || fail 'known write could not release'
[[ $(head -n1 "$work/order") == release && $(tail -n1 "$work/order") == sweep ]] || fail 'cleanup started before release'

# Backend liveness is metadata-based, and a failed metadata query is not zero.
(
  # shellcheck source=deploy/vn/host/operation-account.sh
  source "$here/host/operation-account.sh"
  operation_save() { :; }
  state=$(new_state "$id" | jq --arg id "$id" '.attemptCount=1 | .attempts=[{number:1,container:("aboutme-operation-"+$id+"-1"),applicationName:("hostop:"+$id+":1"),startedAt:.createdAt,stopped:false}]')
  podman() {
    case "$1 $2" in
      'container exists') return 1 ;;
      *) fail 'unexpected container call' ;;
    esac
  }
  timeout() { shift; "$@"; }
  runuser() { cat >/dev/null; printf '%s\n' "$backend_count"; return "${backend_rc:-0}"; }
  for backend_count in 1 0; do
    backend_rc=0
    if account_attempts_stopped; then [[ $backend_count == 0 ]] || fail 'live backend ignored'
    else [[ $backend_count != 0 ]] || fail 'absent backend retained'; fi
  done
  backend_count=0 backend_rc=1
  if account_attempts_stopped; then fail 'failed metadata read proved absence'; fi
)

# Delayed proof and cleanup may bind a later capable live read release without
# repeating confirm. Unknown mutation resumes still require the saved release.
state=$(new_state "$id" | jq '.phase="serving" | .confirmation=null | .expiresAt=null | .action.writeOutcome="committed"')
operation_save
account_live() {
  [[ -z $state ]] || return 1
  live_release=$(jq '.release | .release_tag="v0.7.10" | .release_number=7010' "$work/saved")
  flags_hash=$(jq -r .flagsHash "$work/saved") db_secret_id=synthetic
}
(request_existing verify >/dev/null) || fail 'later capable verification refused'
[[ $(jq -r .release.release_tag "$work/saved") == v0.7.10 ]] || fail 'read release not rebound'
[[ $(jq -r .pendingVerb "$work/saved") == verify ]] || fail 'verification became deletion'
echo 'host operation stale plan, cleanup and backend cases passed'

# Interrupt intent, creation, effect receipt and result persistence separately.
# The same container implementation runs with synthetic Podman and fence calls.
for crash in intent create receipt result; do
  (
    # shellcheck source=deploy/vn/host/operation-account.sh
    source "$here/host/operation-account.sh"
    lib=$work/crash-lib
    mkdir -p "$lib"
    printf '#!/usr/bin/env bash\nexit 0\n' >"$lib/fence.sh"
    chmod +x "$lib/fence.sh"
    operations_dir=$work/crash-records
    mkdir -p "$operations_dir/$id"
    state=$(new_state "$id" | jq '.phase="accepted" | .confirmation=null | .expiresAt=null | .pendingVerb="apply"')
    printf '%s\n' "$state" >"$work/crash-state"
    rm -f "$work/effects"
    account_live() { return 0; }
    account_attempts_stopped() { return 0; }
    operation_save() {
      if [[ $crash == intent && $(jq -r .intent <<<"$state") == delete ]]; then return 1; fi
      if [[ $crash == result && $(jq -r .action.writeOutcome <<<"$state") == committed ]]; then return 1; fi
      printf '%s\n' "$state" >"$work/crash-state"
    }
    timeout() { shift; "$@"; }
    podman() {
      case $1 in
        create)
          printf 'create\n' >>"$work/effects"
          [[ $crash != create ]]
          ;;
        start)
          printf 'effect\n' >>"$work/effects"
          [[ $crash != receipt ]] || return 125
          printf 'deleted user=%s slugs=valid-slug\naccount-delete: end\n' "$user"
          ;;
        *) fail 'unexpected Podman call' ;;
      esac
    }
    account_delete || true
    case $crash in
      intent) [[ ! -e $work/effects ]] || fail 'effect without durable intent' ;;
      create | receipt) [[ $(jq -r .action.writeOutcome "$work/crash-state") == unknown ]] || fail 'lost receipt claimed success' ;;
      result)
        [[ $(jq -r .intent "$work/crash-state") == delete && $(jq -r .action.writeOutcome "$work/crash-state") == not_started ]] || fail 'lost result lost its intent'
        [[ $(grep -c '^effect$' "$work/effects") == 1 ]] || fail 'effect replayed'
        ;;
    esac
  ) || fail "crash boundary: $crash"
done
echo 'host operation crash boundaries passed'

# Retention cannot release a bound fence, and a released expired plan must
# prove fresh quiescence before dropping its reservation.
(
  operation_state() { :; }
  operation_load() { state=$(<"$work/prune-state"); }
  operation_save() { printf '%s\n' "$state" >"$work/prune-state"; }
  operations_dir=$work/prune-records
  id=$(request_id 96)
  mkdir -p "$operations_dir/$id"
  request_release() { fail 'prune released a fence'; }
  account_attempts_stopped() { [[ ! -e $work/prune-backend ]]; }
  request_unit_stopped() { [[ ! -e $work/prune-unit ]]; }
  state=$(new_state "$id" | jq '.expiresAt="2000-01-01T00:00:00Z" | .fenceHeld=true')
  operation_save
  operation_prune || fail 'bound retention failed'
  [[ $(jq -r .reserved "$work/prune-state") == true ]] || fail 'bound plan capacity released'
  state=$(jq '.fenceHeld=false' "$work/prune-state")
  operation_save
  touch "$work/prune-backend"
  operation_prune || fail 'live retention failed'
  [[ $(jq -r .reserved "$work/prune-state") == true ]] || fail 'live backend capacity released'
  rm "$work/prune-backend"
  touch "$work/prune-unit"
  operation_prune || fail 'live unit retention failed'
  [[ $(jq -r .reserved "$work/prune-state") == true ]] || fail 'live unit capacity released'
  rm "$work/prune-unit"
  operation_prune || fail 'quiescent plan retention failed'
  [[ $(jq -r .reserved "$work/prune-state") == false ]] || fail 'expired plan capacity retained'
)
echo 'host operation expired plan admission passed'

# Proof receipts bind request, live digest and phase. Duplicate receipts return
# the same result and contradictory or expired receipts cannot advance state.
(
  account_live() { return 0; }
  id=$(request_id 95)
  nonce=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA
  state=$(new_state "$id" | jq --arg nonce "$nonce" '.phase="serving" | .confirmation=null | .expiresAt=null | .reserved=false | .action.writeOutcome="committed" | .challenge={requestId:.requestId,digest:(.release.server_image|split("@")[1]),phase:"serving",nonce:$nonce,expiresAt:"2099-01-01T00:00:00Z"}')
  operation_save
  envelope=$(jq -cn --arg nonce "$nonce" '{phase:"serving",nonce:$nonce,result:"pass"}')
  (request_proof >/dev/null) || fail 'valid proof receipt refused'
  [[ $(jq -r .action.publicVerification "$work/saved") == limited && $(jq -r .reserved "$work/saved") == false ]] || fail 'proof claimed full card coverage'
  (request_proof >/dev/null) || fail 'identical receipt refused'
  envelope=$(jq '.result="fail"' <<<"$envelope")
  if (request_proof >/dev/null 2>&1); then fail 'contradictory receipt accepted'; fi
  envelope=$(jq '.nonce="BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBA"' <<<"$envelope")
  if (request_proof >/dev/null 2>&1); then fail 'different nonce accepted'; fi
  state=$(jq '.challenge.expiresAt="2000-01-01T00:00:00Z"' "$work/saved")
  operation_save
  if (request_proof >/dev/null 2>&1); then fail 'expired receipt accepted'; fi
)
echo 'host operation proof receipts passed'

# Accepted work lost before the first intent may continue on explicit resume.
# Once intent exists, the earlier crash case requires a separate UUID read.
(
  account_live() { return 0; }
  id=$(request_id 94)
  state=$(new_state "$id" | jq '.phase="accepted" | .confirmation=null | .expiresAt=null | .pendingVerb="apply"')
  operation_save
  rm -f "$work/dispatches"
  (request_existing resume >/dev/null) || fail 'accepted pre-intent resume refused'
  [[ $(jq -r .pendingVerb "$work/saved") == apply && $(wc -l <"$work/dispatches") == 1 ]] || fail 'pre-intent resume did not continue exactly once'
)
echo 'host operation pre-intent resume passed'

# Inactive without a completed systemd job is not quiescence.
(
  # shellcheck disable=SC1090
  source <(sed -n '/^request_unit_stopped()/,/^}/p' "$here/host/operation-run.sh")
  systemctl() { printf '%s\n' "$unit_metadata"; return "${unit_rc:-0}"; }
  for unit_metadata in $'ActiveState=inactive\nJob=1' $'ActiveState=active\nJob=0' 'ActiveState=inactive'; do
    if request_unit_stopped; then fail 'queued or malformed unit proved quiescence'; fi
  done
  for unit_metadata in $'ActiveState=inactive\nJob=0' $'ActiveState=failed\nJob=0'; do
    request_unit_stopped || fail 'quiescent unit refused'
  done
  unit_rc=1
  if request_unit_stopped; then fail 'failed unit read proved quiescence'; fi
)
echo 'host operation queued unit checks passed'
