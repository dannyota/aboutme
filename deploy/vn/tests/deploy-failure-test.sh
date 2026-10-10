#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
export DEPLOY_TEST_DIR=$work
mkdir -p "$work/bin"

cat >"$work/bin/git" <<'STUB'
#!/usr/bin/env bash
case " $* " in
  *' rev-list '*) printf '%040d\n' 0 | tr 0 a ;;
  *) exit 0 ;;
esac
STUB

cat >"$work/bin/gh" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
if [[ $1 == run ]]; then
  printf '[{"conclusion":"success","status":"completed","event":"push","headBranch":"main","headSha":"%040d"}]\n' 0 | tr 0 a
  exit 0
fi
uri=$3
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
args=" $* "
[[ $args == *" -p $EXPECTED_SSH_PORT "* ]] || { echo "SSH port missing" >&2; exit 1; }
case $args in
  *' -O exit '*) exit 0 ;;
  *' sha256sum '*) printf '%064d  remote\n' 0 | tr 0 c; exit 0 ;;
  *'SSH_CLIENT'*) printf '198.51.100.10'; exit 0 ;;
esac
if [[ $args == *' /usr/local/lib/aboutme/fence.sh lock '* ]]; then
  printf 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n'
  exit 0
fi
printf '%s\n' "$args" >>"$DEPLOY_TEST_DIR/ssh.log"
if [[ $args == *' /usr/local/lib/aboutme/deploy-host.sh '* ]]; then
  printf '%s\n' "$args" >>"$DEPLOY_TEST_DIR/host.log"
  if [[ ${FAIL_CLOSED:-0} == 1 && $args == *' fail-closed '* ]]; then exit 1; fi
  exit 0
fi
exit 0
STUB

cat >"$work/bin/curl" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
args=" $* "
digest=$(printf '%064d' 0 | tr 0 b)
if [[ $args == *'ghcr.io/token'* ]]; then
  printf '{"token":"test"}\n'
elif [[ $args == *'ghcr.io/v2/'* && $args == *'fsSI'* ]]; then
  printf 'Docker-Content-Digest: sha256:%s\r\n' "$digest"
elif [[ $args == *'ghcr.io/v2/'* ]]; then
  printf '{"manifests":[{"platform":{"os":"linux","architecture":"amd64"}}]}\n'
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

echo "deployment failure test passed"
