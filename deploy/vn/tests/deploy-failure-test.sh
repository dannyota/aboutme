#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
export DEPLOY_TEST_DIR=$work
mkdir -p "$work/bin"

cat >"$work/bin/git" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
[[ $1 != -C ]] || shift 2
case $1 in
  status)
    case ${CHECKOUT_CASE:-} in
      dirty) echo ' M deploy/vn/scripts/deploy.sh' ;;
      staged) echo 'M  deploy/vn/scripts/deploy.sh' ;;
      untracked) echo '?? deploy/vn/scripts/local.sh' ;;
      status-error) exit 1 ;;
    esac
    ;;
  symbolic-ref)
    case ${CHECKOUT_CASE:-} in
      branch) echo feature ;;
      detached) exit 1 ;;
      *) echo main ;;
    esac
    ;;
  rev-parse)
    if [[ $* == *HEAD && ${CHECKOUT_CASE:-} == stale ]]; then
      printf '%040d\n' 0 | tr 0 b
    else
      printf '%040d\n' 0 | tr 0 a
    fi
    ;;
  fetch) [[ ${RELEASE_CASE:-} != fetch-error ]] ;;
  merge-base) [[ ${RELEASE_CASE:-} != off-main ]] ;;
  # The tagged commit's tree: SCHEMA_FLOOR holds the schema-floor file's exact
  # bytes and SCHEMA_ENTRY its mode and type. As in git, a path given from a
  # subdirectory resolves there unless --full-tree is passed.
  ls-tree)
    printf '%s\n' "$*" >>"$DEPLOY_TEST_DIR/git.log"
    [[ ${SCHEMA_TREE:-} != fail ]] || { echo 'fatal: not a tree object' >&2; exit 128; }
    if [[ -n ${SCHEMA_FLOOR+set} && " $* " == *' --full-tree '* ]]; then
      printf '%s %040d\t%s\n' "${SCHEMA_ENTRY:-100644 blob}" 0 apps/server/migrations/schema-floor
    fi
    ;;
  show)
    printf '%s\n' "$*" >>"$DEPLOY_TEST_DIR/git.log"
    [[ ${SCHEMA_SHOW:-} != fail ]] || exit 128
    printf '%s' "$SCHEMA_FLOOR"
    ;;
  *) echo "unexpected git command: $*" >&2; exit 2 ;;
esac
STUB

cat >"$work/bin/gh" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
if [[ $1 == run ]]; then
  [[ ${RELEASE_CASE:-} != api-error ]] || exit 1
  printf '[{"conclusion":"success","status":"completed","event":"push","headBranch":"main","databaseId":1,"headSha":"%040d"}]\n' 0 | tr 0 a |
    jq --arg case "${RELEASE_CASE:-}" '
      if $case == "wrong-sha" then .[0].headSha = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
      elif $case == "branch-only" then .[0].headBranch = "feature"
      elif $case == "dispatch-only" then .[0].event = "workflow_dispatch"
      elif $case == "cancelled" then .[0].conclusion = "cancelled"
      elif $case == "red" then .[0].conclusion = "failure"
      elif $case == "pending" then .[0].status = "in_progress"
      elif $case == "no-run" then []
      else . end'
  exit 0
fi
uri=$3
if [[ $uri == *aboutme-observer@* ]]; then
  printf '%s\n' "$*" >>"$DEPLOY_TEST_DIR/observer-verify.log"
  [[ ${OBSERVER_CASE:-absent} != bad-provenance ]] || exit 1
fi
subject=${uri#oci://}
subject=${subject%@*}
digest=${uri##*@sha256:}
printf '[{"verificationResult":{"statement":{"subject":[{"name":"%s","digest":{"sha256":"%s"}}]}}}]\n' "$subject" "$digest"
STUB

cat >"$work/bin/sha256sum" <<'STUB'
#!/usr/bin/env bash
printf '%064d  %s\n' 0 "$1" | tr 0 c
STUB

cat >"$work/bin/ssh" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$DEPLOY_TEST_DIR/external.log"
args=" $* "
[[ $args == *" -p $EXPECTED_SSH_PORT "* ]] || { echo "SSH port missing" >&2; exit 1; }
case $args in
  *' -O exit '*) exit 0 ;;
  *' sha256sum '*) printf '%064d  remote\n' 0 | tr 0 c; exit 0 ;;
  *'SSH_CLIENT'*) printf '198.51.100.10'; exit 0 ;;
  *' ss -Htn '*)
    # Answer once the held probe runs, as the host sees it only then.
    for ((i = 0; i < 500; i++)); do
      [[ ! -f $DEPLOY_TEST_DIR/probe.pid ]] || break
      /bin/sleep 0.01
    done
    [[ ${EGRESS_CASE:-} != interrupt ]] || kill -TERM "$PPID"
    printf '%s\n' "${EGRESS_COUNT:-1}"
    exit 0
    ;;
  *' podman exec aboutme-caddy '*) printf '10s\n'; exit 0 ;;
esac
printf '%s\n' "$args" >>"$DEPLOY_TEST_DIR/order.log"
if [[ $args == *' /usr/local/lib/aboutme/fence.sh lock '* ]]; then
  printf 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n'
  exit 0
fi
printf '%s\n' "$args" >>"$DEPLOY_TEST_DIR/ssh.log"
if [[ $args == *' /usr/local/lib/aboutme/fence.sh read '* ]]; then
  [[ ${FAIL_FENCE_READ:-0} != 1 ]] || exit 1
  printf '{"minimum_release":%s,"minimum_tag":"v0.1.0","operation_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}\n' "${FENCE_MINIMUM:-1000}"
  exit 0
fi
if [[ $args == *' /usr/local/lib/aboutme/fence.sh raise '* && ${FAIL_RAISE:-0} == 1 ]]; then exit 1; fi
if [[ $args == *' /usr/local/lib/aboutme/deploy-host.sh '* ]]; then
  printf '%s\n' "$args" >>"$DEPLOY_TEST_DIR/host.log"
  if [[ $args == *' release-json '* ]]; then printf '%s\n' '{"release_tag":"v0.1.0","maintenance_image":"previous-caddy"}'; fi
  if [[ $args == *' running-release '* ]]; then printf '1001\n'; fi
  if [[ ${FAIL_RENDER:-0} == 1 && $args == *' render '* ]]; then exit 255; fi
  if [[ ${FAIL_CIMD:-} == render && $args == *' render '* ]] ||
     [[ ${FAIL_CIMD:-} == render && $args == *' restore-previous '* ]] ||
     [[ ${FAIL_CIMD:-} == restore && $args == *' restore-previous '* ]] ||
     [[ ${FAIL_CIMD:-} == app-up && $args == *' app-up '* ]]; then
    echo 'pre-CIMD rollback refused by host guard' >&2
    exit 1
  fi
  if [[ ${FAIL_CLOSED:-0} == 1 && $args == *' fail-closed '* ]]; then exit 1; fi
  exit 0
fi
exit 0
STUB

cat >"$work/bin/curl" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$DEPLOY_TEST_DIR/external.log"
args=" $* "
if [[ $args == *' --limit-rate '* ]]; then
  if [[ ${EGRESS_CASE:-} != interrupt ]]; then
    printf '%s\n' "$$" >"$DEPLOY_TEST_DIR/probe.pid"
    exec /bin/sleep 30
  fi
  trap 'kill "$nap" 2>/dev/null; /bin/sleep 1; exit 0' TERM
  /bin/sleep 30 &
  nap=$!
  printf '%s\n' "$$" >"$DEPLOY_TEST_DIR/probe.pid"
  wait "$nap"
  exit 0
fi
digest=$(printf '%064d' 0 | tr 0 b)
if [[ $args == *'aboutme-observer/manifests/'* ]]; then
  [[ ${OBSERVER_CASE:-absent} != transport-error ]] || exit 7
  if [[ ${OBSERVER_CASE:-absent} == absent ]]; then printf '404'; exit 0; fi
  output= headers=
  while (($#)); do
    case $1 in
      -o) output=$2; shift ;;
      -D) headers=$2; shift ;;
    esac
    shift
  done
  printf 'Docker-Content-Digest: sha256:%s\r\n' "$digest" >"$headers"
  if [[ $OBSERVER_CASE == index ]]; then
    printf '{"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[]}' >"$output"
  else
    if [[ $OBSERVER_CASE == arm64 ]]; then arch=arm64; else arch=amd64; fi
    config_sha=$(printf '{"os":"linux","architecture":"%s"}' "$arch" | sha256sum | cut -d' ' -f1)
    printf '{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"digest":"sha256:%s"}}' "$config_sha" >"$output"
  fi
  printf '200'
elif [[ $args == *'aboutme-observer/blobs/'* ]]; then
  [[ $args == *'-fsSL'* ]] || exit 22
  if [[ $OBSERVER_CASE == arm64 ]]; then arch=arm64; else arch=amd64; fi
  output=/dev/stdout
  while (($#)); do
    [[ $1 == -o ]] && output=$2
    shift
  done
  printf '{"os":"linux","architecture":"%s"}' "$arch" >"$output"
elif [[ $args == *'ghcr.io/token'* ]]; then
  printf '{"token":"test"}\n'
elif [[ $args == *'ghcr.io/v2/'* && $args == *'fsSI'* ]]; then
  printf 'Docker-Content-Digest: sha256:%s\r\n' "$digest"
elif [[ $args == *'ghcr.io/v2/'* ]]; then
  printf '{"manifests":[{"digest":"sha256:%s","platform":{"os":"linux","architecture":"amd64"}}]}\n' "$(printf '%064d' 0 | tr 0 d)"
elif [[ $args == *'/healthz'* || $args == *'/readyz'* ]]; then
  printf '200'
elif [[ $args == *'fsSI'* ]]; then
  if [[ -n ${EDGE_CASE:-} ]]; then
    printf 'HTTP/2 200\r\nstrict-transport-security: max-age=31536000\r\nx-content-type-options: nosniff\r\n\r\n'
  else
    printf 'HTTP/2 200\r\nx-content-type-options: nosniff\r\n\r\n'
  fi
elif [[ -n ${EDGE_CASE:-} ]] && grep -q ' ban-test del ' "$DEPLOY_TEST_DIR/host.log"; then
  count=0
  [[ ! -f $DEPLOY_TEST_DIR/edge-count ]] || read -r count <"$DEPLOY_TEST_DIR/edge-count"
  count=$((count + 1))
  printf '%s\n' "$count" >"$DEPLOY_TEST_DIR/edge-count"
  if [[ $EDGE_CASE == stuck || ( $EDGE_CASE == delayed && $count -le 3 ) ]]; then printf '403'; else printf '200'; fi
elif [[ -n ${EDGE_CASE:-} ]] && grep -q ' ban-test add ' "$DEPLOY_TEST_DIR/host.log"; then
  bans=0
  [[ ! -f $DEPLOY_TEST_DIR/ban-count ]] || read -r bans <"$DEPLOY_TEST_DIR/ban-count"
  bans=$((bans + 1))
  printf '%s\n' "$bans" >"$DEPLOY_TEST_DIR/ban-count"
  case $EDGE_CASE in
    late-ban) if ((bans >= 20)); then printf '403'; else printf '200'; fi ;;
    no-ban) printf '200' ;;
    *) printf '403' ;;
  esac
elif grep -q ' maintenance-up ' "$DEPLOY_TEST_DIR/host.log"; then
  printf '<!-- aboutme:maintenance -->\n503'
elif [[ ${SITE_CASE:-} == down ]]; then
  printf '000'
  exit 7
else
  printf '200'
fi
STUB

cat >"$work/bin/sleep" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$1" >>"$DEPLOY_TEST_DIR/sleep.log"
STUB

chmod +x "$work/bin/"*
export PATH=$work/bin:$PATH
export DEPLOY_HOST=vn.example.test
export DEPLOY_SITE=https://vn.example.test
unset SSH_PORT
export EXPECTED_SSH_PORT=22922

for CHECKOUT_CASE in dirty staged untracked stale branch detached status-error; do
  export CHECKOUT_CASE
  : >"$work/external.log"
  if "$repo_root/deploy/vn/scripts/deploy.sh" v0.1.1 >"$work/checkout.out" 2>&1; then
    echo "$CHECKOUT_CASE checkout was accepted" >&2; exit 1
  fi
  [[ ! -s $work/external.log ]] || {
    echo "$CHECKOUT_CASE checkout reached SSH or the registry" >&2; exit 1
  }
  grep -q 'release-eligibility:.*checkout' "$work/checkout.out"
  echo "rejected $CHECKOUT_CASE checkout before SSH or registry access"
done
unset CHECKOUT_CASE

for RELEASE_CASE in off-main fetch-error api-error wrong-sha branch-only dispatch-only cancelled pending red no-run; do
  export RELEASE_CASE
  : >"$work/external.log"
  if "$repo_root/deploy/vn/scripts/deploy.sh" v0.1.1 >"$work/eligibility.out" 2>&1; then
    echo "$RELEASE_CASE release was accepted" >&2; exit 1
  fi
  [[ ! -s $work/external.log ]] || {
    echo "$RELEASE_CASE reached SSH or the registry" >&2; exit 1
  }
  grep -q 'release-eligibility:' "$work/eligibility.out"
  echo "rejected $RELEASE_CASE before SSH or registry access"
done
unset RELEASE_CASE

if "$repo_root/deploy/vn/scripts/deploy.sh" v0.1.1 >"$work/out" 2>&1; then
  echo "deploy unexpectedly passed without HSTS" >&2
  exit 1
fi

start_line=$(grep -n ' timers-start ' "$work/host.log" | tail -n1 | cut -d: -f1)
closed_line=$(grep -n ' fail-closed ' "$work/host.log" | tail -n1 | cut -d: -f1)
[[ -n $start_line && -n $closed_line && $closed_line -gt $start_line ]] || {
  echo "deploy did not fail closed after restarting timers" >&2
  exit 1
}
grep -q 'smoke: HSTS header missing' "$work/out" || {
  echo "deploy did not report the failing security header" >&2
  exit 1
}
grep -q 'observer image absent; keeping the installed observer unit' "$work/out"
[[ ! -e $work/observer-verify.log ]] || { echo 'verified an absent observer image' >&2; exit 1; }
if grep -q 'recovery did not finish' "$work/out"; then
  echo "deploy reported a failed recovery after enforcing fail closed" >&2
  exit 1
fi

export SSH_PORT=23000 EXPECTED_SSH_PORT=23000
: >"$work/host.log"
: >"$work/ssh.log"
if FAIL_CLOSED=1 "$repo_root/deploy/vn/scripts/deploy.sh" v0.1.1 >"$work/failed-recovery.out" 2>&1; then
  echo "deploy passed after fail-closed recovery failed" >&2
  exit 1
fi
grep -q ' fail-closed ' "$work/host.log" || {
  echo "deploy did not try fail-closed recovery" >&2
  exit 1
}
if grep -q ' /usr/local/lib/aboutme/fence.sh release ' "$work/ssh.log"; then
  echo "deploy released its lock after fail-closed recovery failed" >&2
  exit 1
fi
grep -q 'operation lock remains for manual recovery' "$work/failed-recovery.out" || {
  echo "deploy did not report retained-lock recovery" >&2
  exit 1
}

for mode in uncertain rollback; do
  : >"$work/host.log"
  : >"$work/order.log"
  args=(v0.1.1)
  [[ $mode != rollback ]] || args=(--rollback v0.1.1)
  if FAIL_RENDER=$([[ $mode == uncertain ]] && echo 1 || echo 0) \
    "$repo_root/deploy/vn/scripts/deploy.sh" "${args[@]}" >"$work/$mode.out" 2>&1; then exit 1; fi
  grep -q ' restore-previous .*v0.1.0' "$work/host.log"
  tail -n 6 "$work/host.log" >"$work/recovery"
  maintenance=$(grep -n ' maintenance-up ' "$work/recovery" | cut -d: -f1)
  stop=$(grep -n ' app-down ' "$work/recovery" | cut -d: -f1)
  restore=$(grep -n ' restore-previous ' "$work/recovery" | cut -d: -f1)
  start=$(grep -n ' app-up ' "$work/recovery" | cut -d: -f1)
  [[ $maintenance -lt $stop && $stop -lt $restore && $restore -lt $start ]]
done
: >"$work/order.log"
: >"$work/external.log"
"$repo_root/deploy/vn/scripts/deploy.sh" --activate v0.1.1 >"$work/activate.out" 2>&1
lock=$(grep -n 'fence.sh lock activate' "$work/order.log" | cut -d: -f1)
read_line=$(grep -n ' running-release ' "$work/order.log" | cut -d: -f1)
[[ $lock -lt $read_line ]]
if grep -q -e ' ss -Htn ' -e '--limit-rate' "$work/external.log"; then
  echo 'activation ran the egress check' >&2
  exit 1
fi

for OBSERVER_CASE in good arm64 index bad-provenance transport-error; do
  export OBSERVER_CASE
  : >"$work/host.log"
  : >"$work/observer-verify.log"
  if "$repo_root/deploy/vn/scripts/deploy.sh" v0.7.5 >"$work/observer-$OBSERVER_CASE.out" 2>&1; then exit 1; fi
  if [[ $OBSERVER_CASE == good ]]; then
    grep -q -- 'pull .*ghcr.io/dannyota/aboutme-observer@sha256:' "$work/host.log"
    grep -q -- 'render .*ghcr.io/dannyota/aboutme-observer@sha256:' "$work/host.log"
    grep -q -- '--source-ref refs/tags/v0.7.5' "$work/observer-verify.log"
    grep -q -- "render .*ghcr.io/dannyota/aboutme-server@sha256:$(printf '%064d' 0 | tr 0 d)" "$work/host.log"
    if grep -q -- "aboutme-server@sha256:$(printf '%064d' 0 | tr 0 b)" "$work/host.log"; then exit 1; fi
  else
    [[ ! -s $work/host.log ]] || { echo "changed host on $OBSERVER_CASE observer" >&2; exit 1; }
  fi
done
unset OBSERVER_CASE
for FAIL_CIMD in render restore app-up; do
  export FAIL_CIMD
  : >"$work/host.log"
  : >"$work/ssh.log"
  initial_render_failure=0
  [[ $FAIL_CIMD != restore ]] || initial_render_failure=1
  if FAIL_RENDER=$initial_render_failure "$repo_root/deploy/vn/scripts/deploy.sh" --rollback v0.1.1 >"$work/cimd-$FAIL_CIMD.out" 2>&1; then
    echo 'deployment accepted a refused CIMD rollback' >&2; exit 1
  fi
  if [[ $FAIL_CIMD != app-up ]] && grep -q ' app-up ' "$work/host.log"; then
    echo 'started an app after restoration guard refusal' >&2; exit 1
  fi
  if grep -q ' /usr/local/lib/aboutme/fence.sh release ' "$work/ssh.log"; then
    echo 'released operation lock after CIMD recovery refusal' >&2; exit 1
  fi
  grep -q 'operation lock remains for manual recovery' "$work/cimd-$FAIL_CIMD.out"
done
unset FAIL_CIMD

edge_failures=0
for EDGE_CASE in delayed stuck late-ban no-ban; do
  export EDGE_CASE
  : >"$work/host.log"
  : >"$work/sleep.log"
  rm -f "$work/edge-count" "$work/ban-count"
  if "$repo_root/deploy/vn/scripts/deploy.sh" v0.1.1 >"$work/edge-$EDGE_CASE.out" 2>&1; then
    result=success
  else
    result=failure
  fi
  count=0 bans=0
  [[ ! -f $work/edge-count ]] || read -r count <"$work/edge-count"
  [[ ! -f $work/ban-count ]] || read -r bans <"$work/ban-count"
  if [[ $EDGE_CASE == delayed ]]; then
    if [[ $result != success || $count -lt 4 ]]; then
      echo 'delayed edge recovery did not wait for a 200 before success' >&2
      edge_failures=1
    fi
  elif [[ $EDGE_CASE == late-ban ]]; then
    if [[ $result != success || $bans != 20 ]]; then
      echo 'a 403 on the twentieth poll did not pass the ban check' >&2
      edge_failures=1
    fi
  elif [[ $EDGE_CASE == no-ban ]]; then
    polls=$(grep -cx 5 "$work/sleep.log" || true)
    if [[ $result != failure || $bans != 31 || $polls != 30 ]] ||
       ! grep -q 'smoke: CrowdSec did not answer 403 for a banned address within 150 s' "$work/edge-$EDGE_CASE.out" ||
       ! grep -q ' ban-test del ' "$work/host.log" ||
       ! grep -q ' fail-closed ' "$work/host.log"; then
      echo 'a missing 403 did not end at the attempt bound with the ban deleted' >&2
      edge_failures=1
    fi
  else
    elapsed=$(awk '{ sum += $1 } END { print sum + 0 }' "$work/sleep.log")
    if [[ $result != failure || $elapsed -lt 25 || $count -lt 2 ]] ||
       ! grep -q 'smoke:.*after.*ban' "$work/edge-$EDGE_CASE.out" ||
       ! grep -q ' fail-closed ' "$work/host.log"; then
      echo 'persistent edge denial did not fail closed after the polling bound' >&2
      edge_failures=1
    fi
  fi
done
unset EDGE_CASE

sed -n '/^ban_seen() {/,/^}/p' "$repo_root/deploy/vn/scripts/deploy.sh" >"$work/ban-seen.sh"
deadline=$(
  # shellcheck source=/dev/null
  source "$work/ban-seen.sh"
  unset SECONDS
  SECONDS=0 polls=0
  banned() { polls=$((polls + 1)); SECONDS=$((SECONDS + 10)); return 1; }
  sleep() { SECONDS=$((SECONDS + $1)); }
  if ban_seen 150; then echo accepted; fi
  echo "$polls $SECONDS"
)
read -r polls clock <<<"$deadline"
if [[ $polls != 11 || ! $clock =~ ^[0-9]+$ ]] || ((clock > 160)); then
  echo "the ban poll did not stop at its deadline: $deadline" >&2
  edge_failures=1
fi

egress_failures=0
filter="ss -Htn state established '( sport = :443 ) and ( dst 198.51.100.10 or dst [::ffff:198.51.100.10] )' | wc -l"
site_probe='-s -o /dev/null -m 10 -w %{http_code} https://vn.example.test/'
probe_stopped() {
  local pid
  [[ -f $work/probe.pid ]] || return 1
  pid=$(cat "$work/probe.pid")
  rm -f "$work/probe.pid"
  if kill -0 "$pid" 2>/dev/null; then
    kill "$pid"
    return 1
  fi
}
egress_reset() {
  rm -f "$work/probe.pid"
  : >"$work/external.log"
  : >"$work/order.log"
  : >"$work/host.log"
  : >"$work/sleep.log"
}

egress_reset
"$repo_root/deploy/vn/scripts/deploy.sh" v0.1.1 >"$work/egress-pass.out" 2>&1 || true
check_line=$(grep -nF -- "$filter" "$work/external.log" | head -n1 | cut -d: -f1) || true
lock_line=$(grep -n 'fence.sh lock deploy' "$work/external.log" | head -n1 | cut -d: -f1) || true
if ! probe_stopped || [[ -z $check_line || -z $lock_line || $check_line -gt $lock_line ]] ||
   ! grep -q ' maintenance-up ' "$work/host.log"; then
  echo 'deploy did not count its held HTTPS probe on the host before the lock' >&2
  egress_failures=1
fi

for EGRESS_COUNT in 0 x; do
  export EGRESS_COUNT
  polls=1 message='could not count'
  [[ $EGRESS_COUNT != 0 ]] || polls=5 message='turn off VPNs such as Cloudflare WARP'
  for mode in deploy rollback; do
    egress_reset
    args=(v0.1.1)
    [[ $mode == deploy ]] || args=(--rollback v0.1.1)
    result=failure
    if "$repo_root/deploy/vn/scripts/deploy.sh" "${args[@]}" >"$work/egress-$mode.out" 2>&1; then result=success; fi
    stopped=1
    probe_stopped || stopped=0
    site_line=$(grep -nxF -- "$site_probe" "$work/external.log" | head -n1 | cut -d: -f1) || true
    check_line=$(grep -nF -- "$filter" "$work/external.log" | head -n1 | cut -d: -f1) || true
    if [[ $result != failure || $stopped != 1 || -s $work/order.log || -s $work/host.log ]] ||
       [[ $(grep -cF -- "$filter" "$work/external.log") != "$polls" ]] ||
       [[ $(grep -cxF -- "$site_probe" "$work/external.log") != 1 || -z $check_line || $site_line -gt $check_line ]] ||
       ! grep -q "$message" "$work/egress-$mode.out"; then
      echo "$mode with an egress count of $EGRESS_COUNT did not refuse before the lock" >&2
      egress_failures=1
    fi
  done
done

export EGRESS_COUNT=0
egress_reset
result=failure
if "$repo_root/deploy/vn/scripts/deploy.sh" v0.1.1 --first-deploy >"$work/egress-first.out" 2>&1; then result=success; fi
stopped=1
probe_stopped || stopped=0
confirmed=$(grep -n 'maintenance confirmed from outside' "$work/egress-first.out" | cut -d: -f1) || true
refused=$(grep -n 'turn off VPNs such as Cloudflare WARP' "$work/egress-first.out" | cut -d: -f1) || true
check_line=$(grep -nF -- "$filter" "$work/external.log" | head -n1 | cut -d: -f1) || true
lock_line=$(grep -n 'fence.sh lock deploy' "$work/external.log" | head -n1 | cut -d: -f1) || true
if [[ $result != failure || $stopped != 1 || -z $confirmed || -z $refused || $confirmed -gt $refused ]] ||
   [[ -z $check_line || -z $lock_line || $check_line -lt $lock_line ]] ||
   ! grep -q ' maintenance-up ' "$work/host.log" || grep -qxF -- "$site_probe" "$work/external.log" ||
   grep -q -e ' render ' -e ' db-setup ' -e ' migrate ' "$work/host.log"; then
  echo 'first deploy did not run the egress check after maintenance confirmation' >&2
  egress_failures=1
fi
unset EGRESS_COUNT

export SITE_CASE=down
for mode in deploy rollback; do
  args=(v0.1.1)
  [[ $mode == deploy ]] || args=(--rollback v0.1.1)
  for EGRESS_COUNT in 1 0; do
    export EGRESS_COUNT
    egress_reset
    "$repo_root/deploy/vn/scripts/deploy.sh" "${args[@]}" >"$work/egress-down.out" 2>&1 || true
    stopped=1
    probe_stopped || stopped=0
    lock_line=$(grep -n 'fence.sh lock ' "$work/external.log" | head -n1 | cut -d: -f1) || true
    maintenance_line=$(grep -nF -- '-s -m 10 -w' "$work/external.log" | tail -n1 | cut -d: -f1) || true
    check_line=$(grep -nF -- "$filter" "$work/external.log" | head -n1 | cut -d: -f1) || true
    case_ok=1
    if [[ $stopped != 1 || -z $lock_line || -z $maintenance_line || -z $check_line ]] ||
       ((lock_line > maintenance_line || maintenance_line > check_line)) ||
       ! grep -q 'the egress check waits until maintenance serves' "$work/egress-down.out"; then
      case_ok=0
    elif [[ $EGRESS_COUNT == 1 ]]; then
      grep -q ' render ' "$work/host.log" || case_ok=0
    else
      confirmed=$(grep -n 'maintenance confirmed from outside' "$work/egress-down.out" | cut -d: -f1) || true
      refused=$(grep -n 'turn off VPNs such as Cloudflare WARP' "$work/egress-down.out" | cut -d: -f1) || true
      if [[ -z $confirmed || -z $refused || $confirmed -gt $refused ]] ||
         grep -q -e ' render ' -e ' migrate ' "$work/host.log" ||
         ! grep -q 'restored the previous release' "$work/egress-down.out" ||
         [[ $(tail -n 3 "$work/host.log" | awk '{ print $NF }' | paste -sd ' ' -) != 'app-up maintenance-down timers-start' ]]; then
        case_ok=0
      fi
    fi
    if ((!case_ok)); then
      echo "$mode with no site answer and an egress count of $EGRESS_COUNT did not check once maintenance served" >&2
      egress_failures=1
    fi
  done
done
unset SITE_CASE EGRESS_COUNT

export EGRESS_CASE=interrupt
egress_reset
status=0
"$repo_root/deploy/vn/scripts/deploy.sh" v0.1.1 >"$work/egress-interrupt.out" 2>&1 || status=$?
pid=$(cat "$work/probe.pid" 2>/dev/null) || pid=''
if [[ $status != 143 || -z $pid || -s $work/order.log || -s $work/host.log ]] || kill -0 "$pid" 2>/dev/null; then
  echo "an interrupted deploy (exit $status) left its HTTPS probe running or passed the lock" >&2
  egress_failures=1
fi
[[ -z $pid ]] || kill "$pid" 2>/dev/null || true
rm -f "$work/probe.pid"
unset EGRESS_CASE

floor_failures=0
floor_fail() { echo "$*" >&2; floor_failures=1; }
floor_reset() {
  rm -f "$work/probe.pid" "$work/git.log"
  : >"$work/external.log"
  : >"$work/order.log"
  : >"$work/host.log"
  : >"$work/ssh.log"
}
deploy() { "$repo_root/deploy/vn/scripts/deploy.sh" "$@"; }
at() { grep -nF -- "$1" "$work/order.log" | head -n1 | cut -d: -f1 || true; }
op_id=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
file_read="ls-tree --full-tree $(printf '%040d' 0 | tr 0 a) -- apps/server/migrations/schema-floor"
[[ $(grep '^floor_tag=' "$repo_root/deploy/vn/scripts/fence.sh") =~ ^floor_tag=v([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]
above_floor=v${BASH_REMATCH[1]}.${BASH_REMATCH[2]}.$((BASH_REMATCH[3] + 1))
past_floor=v${BASH_REMATCH[1]}.$((BASH_REMATCH[2] + 1)).0

floor_reset
if deploy --activate "$above_floor" >"$work/floor-activate.out" 2>&1 ||
   [[ -s $work/external.log ]] || ! grep -q 'above the tracked floor' "$work/floor-activate.out"; then
  floor_fail 'an activation above the tracked floor was not refused before SSH'
fi

for schema_case in above-candidate above-tracked no-newline blank-line padded crlf empty two-tags \
  tree-unreadable executable symlink directory submodule file-unreadable; do
  tag=v0.1.1 message='must hold one strict tag and a newline'
  SCHEMA_TREE='' SCHEMA_ENTRY='100644 blob' SCHEMA_SHOW='' SCHEMA_FLOOR=$'v0.1.1\n'
  case $schema_case in
    above-candidate) SCHEMA_FLOOR=$'v0.1.2\n' message='is below its schema floor' ;;
    above-tracked) tag=$past_floor SCHEMA_FLOOR=$above_floor$'\n' message='above the tracked floor' ;;
    no-newline) SCHEMA_FLOOR=v0.1.1 ;;
    blank-line) SCHEMA_FLOOR=$'v0.1.1\n\n' ;;
    padded) SCHEMA_FLOOR=$'v0.01.1\n' ;;
    crlf) SCHEMA_FLOOR=$'v0.1.1\r\n' ;;
    empty) SCHEMA_FLOOR='' ;;
    two-tags) SCHEMA_FLOOR=$'v0.1.1\nv0.1.0\n' ;;
    tree-unreadable) SCHEMA_TREE=fail message='could not inspect' ;;
    executable) SCHEMA_ENTRY='100755 blob' message='not a regular file' ;;
    symlink) SCHEMA_ENTRY='120000 blob' message='not a regular file' ;;
    directory) SCHEMA_ENTRY='040000 tree' message='not a regular file' ;;
    submodule) SCHEMA_ENTRY='160000 commit' message='not a regular file' ;;
    file-unreadable) SCHEMA_SHOW=fail message='could not read' ;;
  esac
  export SCHEMA_FLOOR SCHEMA_TREE SCHEMA_ENTRY SCHEMA_SHOW
  floor_reset
  if deploy "$tag" >"$work/floor-$schema_case.out" 2>&1 || [[ -s $work/external.log ]] ||
     ! grep -q "$message" "$work/floor-$schema_case.out" || ! grep -qsxF -- "$file_read" "$work/git.log"; then
    floor_fail "a schema floor case ($schema_case) was not refused before SSH"
  fi
done
unset SCHEMA_TREE SCHEMA_ENTRY SCHEMA_SHOW

export SCHEMA_FLOOR=$'v0.1.1\n' FENCE_MINIMUM=1000
for mode in deploy first-deploy; do
  args=(v0.1.1)
  [[ $mode == deploy ]] || args+=(--first-deploy)
  floor_reset
  deploy "${args[@]}" >"$work/floor-raise.out" 2>&1 || true
  down=$(at ' app-down ') render=$(at ' render ') fence_read=$(at '/fence.sh read ')
  raise=$(at "/fence.sh raise $op_id v0.1.1 ") migrate=$(at ' migrate ') setup=$migrate
  [[ $mode == deploy ]] || setup=$(at ' db-setup ')
  if [[ -z $down || -z $render || -z $fence_read || -z $raise || -z $setup || -z $migrate ]] ||
     ((down > render || render > fence_read || fence_read > raise || raise > setup || setup > migrate)) ||
     grep -qF "$op_id" "$work/floor-raise.out"; then
    floor_fail "$mode did not raise a schema floor above the fence after app-down and before migrate"
  fi
done

floor_reset
if FAIL_RAISE=1 deploy v0.1.1 >"$work/floor-raise-failed.out" 2>&1 ||
   ! grep -qF "/fence.sh raise $op_id v0.1.1 " "$work/order.log" || ! grep -q ' fail-closed ' "$work/host.log" ||
   grep -q -e ' restore-previous ' -e ' app-up ' -e ' migrate ' "$work/host.log"; then
  floor_fail 'a failed schema-floor raise did not fail closed'
fi

floor_reset
if FAIL_FENCE_READ=1 deploy v0.1.1 >"$work/floor-read-failed.out" 2>&1 ||
   ! grep -qF '/fence.sh read ' "$work/order.log" || grep -qF '/fence.sh raise ' "$work/order.log" ||
   grep -q -e ' fail-closed ' -e ' migrate ' "$work/host.log" || ! grep -q ' restore-previous ' "$work/host.log" ||
   [[ $(tail -n 3 "$work/host.log" | awk '{ print $NF }' | paste -sd ' ' -) != 'app-up maintenance-down timers-start' ]] ||
   ! grep -q 'restored the previous release' "$work/floor-read-failed.out"; then
  floor_fail 'a failed fence read did not restore the previous release'
fi

for FENCE_MINIMUM in 1001 2000; do
  floor_reset
  deploy v0.1.1 >"$work/floor-at-fence.out" 2>&1 || true
  if ! grep -qF '/fence.sh read ' "$work/order.log" || grep -qF '/fence.sh raise ' "$work/order.log" ||
     ! grep -q ' migrate ' "$work/host.log"; then
    floor_fail "a schema floor at or below a fence of $FENCE_MINIMUM did not continue without a raise"
  fi
done
unset SCHEMA_FLOOR FENCE_MINIMUM

floor_reset
deploy v0.1.1 >"$work/floor-absent.out" 2>&1 || true
if ! grep -qsxF -- "$file_read" "$work/git.log" || grep -qF -e '/fence.sh read ' -e '/fence.sh raise ' "$work/order.log" ||
   ! grep -q ' migrate ' "$work/host.log"; then
  floor_fail 'a tree without a schema floor did not deploy without reading the fence'
fi

export SCHEMA_FLOOR=$'v0.1.2\n'
for mode in rollback activate; do
  floor_reset
  deploy "--$mode" v0.1.1 >"$work/floor-$mode.out" 2>&1 || true
  if [[ -e $work/git.log ]] || grep -qF -e '/fence.sh read ' -e "/fence.sh raise $op_id v0.1.2 " "$work/order.log" ||
     ! grep -qF "/fence.sh lock $mode v0.1.1 " "$work/order.log"; then
    floor_fail "a $mode read the schema floor"
  fi
done
unset SCHEMA_FLOOR

((edge_failures == 0 && egress_failures == 0 && floor_failures == 0)) || exit 1
echo "deployment failure test passed"
