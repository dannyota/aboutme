#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
fence=$repo_root/deploy/vn/scripts/fence.sh
safety=$repo_root/deploy/vn/scripts/deploy-safety.sh
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin" "$work/fence" "$work/operations"
chmod 0700 "$work/operations"
: >"$work/operations/.bundle.lock"
chmod 0600 "$work/operations/.bundle.lock"
fixture_uid=$(id -u)
sed -e "s|/srv/data/host-operations|$work/operations|g" \
  -e "s/== 0:700/== $fixture_uid:700/; s/== 0:600/== $fixture_uid:600/" \
  "$fence" >"$work/fence.sh"
chmod +x "$work/fence.sh"
fence=$work/fence.sh
floor_tag=$(sed -n 's/^floor_tag=//p' "$fence")
floor_release=$(sed -n 's/^floor_release=//p' "$fence")
[[ $floor_release =~ ^[0-9]+$ && $floor_tag =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]
above_floor=v${BASH_REMATCH[1]}.${BASH_REMATCH[2]}.$((BASH_REMATCH[3] + 1))

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
[[ $(jq -r .minimum_tag "$work/fence/fence.json") == "$floor_tag" ]]
jq '.minimum_release = 4007 | .minimum_tag = "v0.4.7"' "$work/fence/fence.json" >"$work/below-floor.json"
cp "$work/below-floor.json" "$work/fence/fence.json"
raise_op=$("$fence" lock deploy v0.4.7)
cp "$work/fence/fence.json" "$work/before-raise.json"
if "$fence" raise "$raise_op" "$above_floor" 2>"$work/raise.err"; then
  echo "a raise went above the tracked floor" >&2
  exit 1
fi
cmp -s "$work/before-raise.json" "$work/fence/fence.json" || {
  echo "a refused raise changed the fence document" >&2
  exit 1
}
grep -qF "is above the tracked floor $floor_tag" "$work/raise.err"
"$fence" raise "$raise_op" "$floor_tag"
[[ $(jq -r '"\(.minimum_release) \(.minimum_tag)"' "$work/fence/fence.json") == "$floor_release $floor_tag" ]]
"$fence" release "$raise_op"
for invalid in missing-directory directory-mode missing-lock lock-mode symlink-lock; do
  case $invalid in
    missing-directory) mv "$work/operations" "$work/saved-operations" ;;
    directory-mode) chmod 0755 "$work/operations" ;;
    missing-lock) mv "$work/operations/.bundle.lock" "$work/saved-lock" ;;
    lock-mode) chmod 0644 "$work/operations/.bundle.lock" ;;
    symlink-lock)
      mv "$work/operations/.bundle.lock" "$work/saved-lock"
      ln -s "$work/saved-lock" "$work/operations/.bundle.lock" ;;
  esac
  if "$fence" lock deploy "$floor_tag" >"$work/unsafe.out" 2>"$work/unsafe.err"; then
    echo 'FAIL unsafe-bundle-admitted-owner' >&2; exit 1
  fi
  [[ ! -s $work/unsafe.out ]] && grep -qxF 'fence: unsafe bundle lock' "$work/unsafe.err"
  [[ $(jq -r '.operation_id // empty' "$work/fence/fence.json") == '' ]]
  case $invalid in
    missing-directory) mv "$work/saved-operations" "$work/operations" ;;
    directory-mode) chmod 0700 "$work/operations" ;;
    missing-lock) mv "$work/saved-lock" "$work/operations/.bundle.lock" ;;
    lock-mode) chmod 0600 "$work/operations/.bundle.lock" ;;
    symlink-lock) rm "$work/operations/.bundle.lock"; mv "$work/saved-lock" "$work/operations/.bundle.lock" ;;
  esac
done
export FLOCK_CALLS=$work/flock-calls BUNDLE_PATH=$work/operations/.bundle.lock
export REAL_FLOCK
REAL_FLOCK=$(command -v flock)
cat >"$work/bin/flock" <<'STUB'
#!/usr/bin/env bash
descriptor=${!#}
path=$(readlink "/proc/$PPID/fd/$descriptor")
printf '%s\n' "$path" >>"$FLOCK_CALLS"
if [[ $path == "$BUNDLE_PATH" ]]; then touch "$FLOCK_CALLS.bundle"; fi
exec "$REAL_FLOCK" "$@"
STUB
chmod +x "$work/bin/flock"
: >"$FLOCK_CALLS"
bundle_op=$("$fence" lock deploy "$floor_tag")
printf '%s\n' "$BUNDLE_PATH" "$work/fence/fence.lock" >"$work/expected-locks"
cmp "$work/expected-locks" "$FLOCK_CALLS"
"$REAL_FLOCK" -x -n "$BUNDLE_PATH" true
[[ $(jq -r .operation_id "$work/fence/fence.json") == "$bundle_op" ]]
if "$fence" lock deploy "$floor_tag" >/dev/null 2>&1; then exit 1; fi
"$REAL_FLOCK" -x -n "$BUNDLE_PATH" true
"$fence" release "$bundle_op"

exec {bundle_fd}>>"$BUNDLE_PATH"
"$REAL_FLOCK" -x "$bundle_fd"
rm -f "$FLOCK_CALLS.bundle"
started=$SECONDS
(
  exec {bundle_fd}>&-
  timeout 40 "$fence" lock deploy "$floor_tag"
) >"$work/busy.out" 2>"$work/busy.err" &
waiting=$!
for ((attempt=0; attempt<100; attempt++)); do
  [[ ! -e $FLOCK_CALLS.bundle ]] || break
  sleep 0.05
done
[[ -e $FLOCK_CALLS.bundle ]]
kill -0 "$waiting"
"$REAL_FLOCK" -x -n "$work/fence/fence.lock" true
if wait "$waiting"; then echo 'bundle contention allowed ownership' >&2; exit 1; else status=$?; fi
[[ $status == 1 && $((SECONDS-started)) -ge 29 && $((SECONDS-started)) -lt 40 ]]
[[ ! -s $work/busy.out ]]
grep -qxF "fence: could not take $BUNDLE_PATH within 30 s" "$work/busy.err"
[[ $(jq -r '.operation_id // empty' "$work/fence/fence.json") == '' ]]
bundle_op=$(timeout 5 "$fence" lock deploy "$floor_tag")
if "$REAL_FLOCK" -x -n "$BUNDLE_PATH" true; then exit 1; fi
"$fence" release "$bundle_op"
exec {bundle_fd}>&-
rm "$work/bin/flock"
if MOUNT_ABSENT=1 "$fence" check "$floor_release" aboutme-server 2>/dev/null; then
  echo "the server started without its data mount" >&2
  exit 1
fi
request=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA
[[ $("$fence" lock-for account_delete "$floor_tag" "$request") == "$request" ]]
[[ $("$fence" lock-for account_delete "$floor_tag" "$request") == "$request" ]]
if "$fence" lock-for account_delete "$above_floor" "$request" 2>/dev/null; then
  echo "a bound request changed its tag" >&2
  exit 1
fi
if "$fence" checkpoint "$request" "$above_floor" 2>/dev/null; then
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
sed "s|/srv/data/host-operations|$work/operations|g" "$fence" >"$work/marker-fence.sh"
chmod +x "$work/marker-fence.sh"
for marker in malformed symlink; do
  if [[ $marker == malformed ]]; then printf '{"bad":true}\n' >"$work/operations/key-change.json"
  else ln -s "$work/nonexistent" "$work/operations/key-change.json"; fi
  for unit in aboutme-server aboutme-web aboutme-caddy aboutme-migrate aboutme-job@media-deletion-sweep; do
    if "$work/marker-fence.sh" check "$floor_release" "$unit" 2>/dev/null; then
      echo "a retained key marker allowed an image start" >&2
      exit 1
    fi
  done
  rm "$work/operations/key-change.json"
done
op=$("$fence" lock cutover "$floor_tag")
"$fence" mutation-start "$op" "$floor_tag" reset-db
for unit in aboutme-server aboutme-web aboutme-caddy \
  aboutme-job@media-deletion-sweep aboutme-migrate; do
  if "$fence" check "$floor_release" "$unit" 2>/dev/null; then
    echo "$unit started while a cutover mutation was active" >&2
    exit 1
  fi
done
"$fence" check "$floor_release" aboutme-db-setup
cp "$work/fence/cutover-mutation.json" "$work/reset-db-marker.json"
jq '.operation_id = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"' \
  "$work/reset-db-marker.json" >"$work/fence/cutover-mutation.json"
if "$fence" check "$floor_release" aboutme-db-setup 2>/dev/null; then
  echo "db-setup started with a marker owned by another operation" >&2
  exit 1
fi
cp "$work/reset-db-marker.json" "$work/fence/cutover-mutation.json"
if "$fence" lock deploy "$floor_tag" 2>/dev/null; then
  echo "a deploy acquired the cutover lock" >&2
  exit 1
fi
if "$fence" release "$op" 2>/dev/null; then
  echo "the cutover lock released with an active mutation" >&2
  exit 1
fi
"$fence" mutation-clear "$op" reset-db
if "$fence" check "$floor_release" aboutme-server 2>/dev/null; then
  echo "the server started while the cutover lock remained" >&2
  exit 1
fi
if "$fence" check "$floor_release" aboutme-db-setup 2>/dev/null; then
  echo "db-setup started without its reset-db marker" >&2
  exit 1
fi
"$fence" mutation-start "$op" "$floor_tag" media
if "$fence" check "$floor_release" aboutme-db-setup 2>/dev/null; then
  echo "db-setup started for a media mutation" >&2
  exit 1
fi
if "$fence" recover-cutover-media "$op" wrong-phrase inspect 2>/dev/null; then
  echo "media recovery accepted no operator attestation" >&2
  exit 1
fi
"$fence" recover-cutover-media "$op" media-writer-stopped-and-result-inspected inspect
"$fence" release "$op"

deploy_op=$("$fence" lock deploy "$floor_tag")
for unit in aboutme-server aboutme-web aboutme-caddy \
  aboutme-job@media-deletion-sweep aboutme-db-setup aboutme-migrate; do
  "$fence" check "$floor_release" "$unit"
done
"$fence" release "$deploy_op"

printf '{"state":"active"}\n' >"$work/fence/cutover-mutation.json"
if "$fence" check "$floor_release" aboutme-server 2>/dev/null; then
  echo "the server started with a malformed cutover marker" >&2
  exit 1
fi
if "$fence" check "$floor_release" aboutme-db-setup 2>/dev/null; then
  echo "db-setup started with a malformed cutover marker" >&2
  exit 1
fi
rm -f "$work/fence/cutover-mutation.json"
ln -s "$work/reset-db-marker.json" "$work/fence/cutover-mutation.json"
if "$fence" check "$floor_release" aboutme-db-setup 2>/dev/null; then
  echo "db-setup started with a symlinked cutover marker" >&2
  exit 1
fi

rm "$work/fence/cutover-mutation.json"
notice_op=$("$fence" lock notice_restore "$floor_tag")
for unit in aboutme-server aboutme-web aboutme-caddy aboutme-migrate \
  aboutme-db-setup aboutme-job@media-deletion-sweep crowdsec crowdsec-firewall-bouncer; do
  if "$fence" check "$floor_release" "$unit" >"$work/notice.out" 2>&1; then
    echo 'notice restore permitted a guarded start' >&2
    exit 1
  fi
done
"$fence" check "$floor_release" aboutme-maintenance
"$fence" check "$floor_release" postgresql@18-main
if "$fence" checkpoint "$request" "$floor_tag" >/dev/null 2>&1; then
  echo 'notice restore accepted another owner' >&2
  exit 1
fi
"$fence" checkpoint "$notice_op" "$floor_tag"
if "$fence" notice-restore-release "$request" >/dev/null 2>&1; then
  echo 'strict notice release accepted a different owner' >&2
  exit 1
fi
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
      if "$fence" check "$floor_release" "$unit" >/dev/null 2>&1; then
        echo 'initializing evidence permitted a start without ownership' >&2
        exit 1
      fi
    done
    "$fence" check "$floor_release" aboutme-maintenance
    "$fence" check "$floor_release" postgresql@18-main
  fi
done
preserved=$(jq -c .notice_authority "$work/fence/fence.json")
notice_op=$("$fence" lock deploy "$floor_tag")
for verb in checkpoint raise; do
  "$fence" "$verb" "$notice_op" "$floor_tag"
  [[ $(jq -c .notice_authority "$work/fence/fence.json") == "$preserved" ]]
done
"$fence" release "$notice_op"
[[ $(jq -c .notice_authority "$work/fence/fence.json") == "$preserved" ]]
for evidence in 'null' '{}' '{"version":2,"state":"adopted"}' \
  '{"version":1,"state":"adopted","operation_id":"bad"}' \
  '{"version":1,"state":"adopted","operation_id":"bad","extra":true}'; do
  jq --argjson value "$evidence" '.notice_authority=$value' "$work/evidence.json" >"$work/fence/fence.json"
  if "$fence" check "$floor_release" aboutme-server >/dev/null 2>&1; then
    echo 'malformed notice evidence permitted startup' >&2
    exit 1
  fi
done
echo "cutover fence tests passed"
