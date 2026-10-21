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
# Redirect the fixed host path without adding a production override.
sed "s|/srv/data/host-operations|$work/operations|g" "$fence" >"$work/fence.sh"
chmod +x "$work/fence.sh"
fence=$work/fence.sh

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
# Record real flock calls and prove bundle admission precedes the fence lock.
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
bundle_op=$("$fence" lock deploy v0.4.7)
printf '%s\n' "$BUNDLE_PATH" "$work/fence/fence.lock" >"$work/expected-locks"
cmp "$work/expected-locks" "$FLOCK_CALLS"
"$REAL_FLOCK" -x -n "$BUNDLE_PATH" true
[[ $(jq -r .operation_id "$work/fence/fence.json") == "$bundle_op" ]]
if "$fence" lock deploy v0.4.7 >/dev/null 2>&1; then exit 1; fi
"$REAL_FLOCK" -x -n "$BUNDLE_PATH" true
"$fence" release "$bundle_op"

exec {bundle_fd}>>"$BUNDLE_PATH"
"$REAL_FLOCK" -x "$bundle_fd"
rm -f "$FLOCK_CALLS.bundle"
started=$SECONDS
(
  exec {bundle_fd}>&-
  timeout 40 "$fence" lock deploy v0.4.7
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
# An installer or notice adopter passes its exclusive open description to the
# child. Acquiring ownership must neither deadlock nor unlock the caller.
bundle_op=$(timeout 5 "$fence" lock deploy v0.4.7)
if "$REAL_FLOCK" -x -n "$BUNDLE_PATH" true; then exit 1; fi
"$fence" release "$bundle_op"
exec {bundle_fd}>&-
rm "$work/bin/flock"
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
echo "cutover fence tests passed"
