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
esac
printf '%s\n' "$args" >>"$DEPLOY_TEST_DIR/order.log"
if [[ $args == *' /usr/local/lib/aboutme/fence.sh lock '* ]]; then
  printf 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n'
  exit 0
fi
printf '%s\n' "$args" >>"$DEPLOY_TEST_DIR/ssh.log"
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
  printf 'HTTP/2 200\r\nx-content-type-options: nosniff\r\n\r\n'
elif grep -q ' maintenance-up ' "$DEPLOY_TEST_DIR/host.log"; then
  printf '<!-- aboutme:maintenance -->\n503'
else
  printf '200'
fi
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
"$repo_root/deploy/vn/scripts/deploy.sh" --activate v0.1.1 >"$work/activate.out" 2>&1
lock=$(grep -n 'fence.sh lock activate' "$work/order.log" | cut -d: -f1)
read_line=$(grep -n ' running-release ' "$work/order.log" | cut -d: -f1)
[[ $lock -lt $read_line ]]

for OBSERVER_CASE in good arm64 index bad-provenance transport-error; do
  export OBSERVER_CASE
  : >"$work/host.log"
  : >"$work/observer-verify.log"
  if "$repo_root/deploy/vn/scripts/deploy.sh" v0.7.5 >"$work/observer-$OBSERVER_CASE.out" 2>&1; then exit 1; fi
  if [[ $OBSERVER_CASE == good ]]; then
    grep -q -- 'pull .*ghcr.io/dannyota/aboutme-observer@sha256:' "$work/host.log"
    grep -q -- 'render .*ghcr.io/dannyota/aboutme-observer@sha256:' "$work/host.log"
    grep -q -- '--source-ref refs/tags/v0.7.5' "$work/observer-verify.log"
    # App images pin the linux/amd64 digest inside the index, not the index.
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
echo "deployment failure test passed"
