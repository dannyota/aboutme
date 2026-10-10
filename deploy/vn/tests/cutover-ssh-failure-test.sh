#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin" "$work/runtime"

cat >"$work/bin/ssh" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$CUTOVER_TEST_LOG"
args=" $* "
if [[ $args == *' /usr/local/lib/aboutme/deploy-host.sh '*' release-json '* ]]; then
  printf '{"release_tag":"v0.4.7"}\n'
elif [[ $args == *' /usr/local/lib/aboutme/fence.sh lock cutover v0.4.7 '* ]]; then
  printf 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n'
elif [[ $args == *' /usr/local/lib/aboutme/fence.sh mutation-start '* ]]; then
  exit 0
elif [[ ${CUTOVER_TEST_MODE:-} == uncertain && $args == *' /usr/local/lib/aboutme/deploy-host.sh '*' cutover-quiesce '* ]]; then
  exit 255
elif [[ ${CUTOVER_TEST_MODE:-} == cleanup-failure && $args == *' /usr/local/lib/aboutme/deploy-host.sh '*' cutover-fail-closed '* ]]; then
  exit 1
else
  exit 0
fi
STUB
cat >"$work/bin/aws" <<'STUB'
#!/usr/bin/env bash
case " $* " in
  *' describe-services '*'aboutme-prod-app'*) printf '0\t0\n' ;;
  *' describe-services '*'aboutme-prod-maintenance'*) printf '1\n' ;;
  *' list-schedules '*) printf '0\n' ;;
  *) exit 1 ;;
esac
STUB
chmod +x "$work/bin/"*
export PATH=$work/bin:$PATH XDG_RUNTIME_DIR=$work/runtime CUTOVER_TEST_LOG=$work/ssh.log
export DEPLOY_HOST=vn.example.test
export CUTOVER_TEST_MODE=uncertain

if "$repo_root/deploy/vn/scripts/cutover.sh" preflight >"$work/out" 2>&1; then
  echo "cutover passed after an uncertain SSH failure" >&2
  exit 1
fi
grep -q '/usr/local/lib/aboutme/fence.sh lock cutover v0.4.7' "$work/ssh.log" || { echo "cutover did not take the fence" >&2; exit 1; }
grep -q '/usr/local/lib/aboutme/fence.sh mutation-start .* quiesce' "$work/ssh.log" || { echo "cutover did not mark its mutation" >&2; exit 1; }
if grep -q '/usr/local/lib/aboutme/fence.sh release' "$work/ssh.log"; then
  echo "cutover released its lock after an uncertain SSH failure" >&2
  exit 1
fi
grep -q 'marker and operation lock remain' "$work/out" || { echo "cutover did not report manual recovery" >&2; exit 1; }

: >"$work/ssh.log"
export CUTOVER_TEST_MODE=cleanup-failure
if "$repo_root/deploy/vn/scripts/cutover.sh" preflight >"$work/cleanup.out" 2>&1; then
  echo "cutover passed after final fail-closed cleanup failed" >&2
  exit 1
fi
grep -q '/usr/local/lib/aboutme/deploy-host.sh .* cutover-fail-closed' "$work/ssh.log" || {
  echo "cutover did not attempt final fail-closed cleanup" >&2
  exit 1
}
if grep -q '/usr/local/lib/aboutme/fence.sh release' "$work/ssh.log"; then
  echo "cutover released its lock after final cleanup failed" >&2
  exit 1
fi
grep -q 'operation lock remains for manual recovery' "$work/cleanup.out" || {
  echo "cutover did not report retained-lock cleanup failure" >&2
  exit 1
}

echo "cutover SSH failure test passed"
