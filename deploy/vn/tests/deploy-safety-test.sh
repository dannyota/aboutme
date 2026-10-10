#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
export ABOUTME_TEST_STATE=$work/state
mkdir -p "$ABOUTME_TEST_STATE/active" "$ABOUTME_TEST_STATE/enabled"

cat >"$work/systemctl" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
state=${ABOUTME_TEST_STATE:?}
case $1 in
  is-active) [[ -e $state/active/${3} ]] ;;
  is-enabled) [[ -e $state/enabled/${3} ]] ;;
  stop)
    shift
    for unit in "$@"; do rm -f "$state/active/$unit"; done
    ;;
  start)
    shift
    for unit in "$@"; do : >"$state/active/$unit"; done
    ;;
  enable)
    shift
    [[ ${1:-} == --now ]] && shift
    for unit in "$@"; do
      : >"$state/enabled/$unit"
      : >"$state/active/$unit"
    done
    ;;
  *) echo "unexpected systemctl call: $*" >&2; exit 2 ;;
esac
STUB
chmod +x "$work/systemctl"
export PATH=$work:$PATH

# shellcheck source=../scripts/deploy-safety.sh
source "$root/deploy/vn/scripts/deploy-safety.sh"

for unit in aboutme-caddy aboutme-server aboutme-web \
  aboutme-job-idempotency-expiry-sweep.timer \
  aboutme-job-media-deletion-sweep.timer \
  aboutme-job-privacy-retention-sweep.timer \
  aboutme-job-media-orphan-sweep.timer \
  aboutme-job@media-deletion-sweep.service; do
  : >"$ABOUTME_TEST_STATE/active/$unit"
done

aboutme_fail_closed 1
aboutme_assert_fail_closed

for unit in aboutme-caddy aboutme-server aboutme-web \
  aboutme-job-idempotency-expiry-sweep.timer \
  aboutme-job-media-deletion-sweep.timer \
  aboutme-job-privacy-retention-sweep.timer \
  aboutme-job-media-orphan-sweep.timer \
  aboutme-job@media-deletion-sweep.service; do
  [[ ! -e $ABOUTME_TEST_STATE/active/$unit ]] || {
    echo "$unit still active after fail closed" >&2
    exit 1
  }
done
[[ -e $ABOUTME_TEST_STATE/active/aboutme-maintenance ]] || {
  echo "maintenance is not active after fail closed" >&2
  exit 1
}

# A timer restarted by another process must make the cutover guard fail.
: >"$ABOUTME_TEST_STATE/active/aboutme-job-media-orphan-sweep.timer"
if aboutme_assert_cutover_quiescent; then
  echo "cutover guard accepted a restarted job timer" >&2
  exit 1
fi
rm -f "$ABOUTME_TEST_STATE/active/aboutme-job-media-orphan-sweep.timer"
aboutme_assert_cutover_quiescent

echo "deployment safety tests passed"
