#!/usr/bin/env bash

# Published-release fixtures exercise selection, coverage, and error collection.
set -Eeuo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-security-scan.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT
mkdir -p "$WORK/bin"
ln -s "$(command -v bash)" "$WORK/bin/bash"
ln -s "$(command -v jq)" "$WORK/bin/jq"
export FIXTURE_DIR="$WORK" CALL_LOG="$WORK/calls" API_LOG="$WORK/api"
export PATH="$WORK/bin:/usr/bin:/bin"

fail() { printf 'not ok - %s\n' "$*" >&2; exit 1; }

cat >"$WORK/bin/gh" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$API_LOG"
[ "$1" = api ] || exit 64
case $2 in
repos/dannyota/aboutme/releases/latest | repos/dannyota/aboutme/releases/tags/v9.9.9)
  cat "$FIXTURE_DIR/release.json" ;;
repos/dannyota/aboutme/releases/tags/v8.8.8)
  cat "$FIXTURE_DIR/retained.json" ;;
repos/dannyota/aboutme/releases/assets/99)
  [ "$*" = 'api repos/dannyota/aboutme/releases/assets/99 -H Accept:application/octet-stream' ] || exit 64
  [ "${ASSET_ERROR:-0}" = 0 ] || exit 70
  cat "$FIXTURE_DIR/digests.txt" ;;
repos/dannyota/aboutme/releases/assets/88)
  cat "$FIXTURE_DIR/retained-digests.txt" ;;
*) exit 44 ;;
esac
STUB
cat >"$WORK/bin/git" <<'STUB'
#!/usr/bin/env bash
# A reachable but unpublished tag must never select the scan targets.
printf 'git %s\n' "$*" >>"$API_LOG"
printf '%s\n' v7.7.7
STUB
cat >"$WORK/bin/trivy" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$CALL_LOG"
args=$*
report=
while [ "$#" -gt 0 ]; do
  case $1 in
  --output) report=$2; shift 2 ;;
  *) shift ;;
  esac
done
if [[ $args == *"${FAIL_TARGET:-no-match}"* ]]; then
  case ${FAIL_MODE:-vulnerability} in
  engine) exit 70 ;;
  no-report) exit 0 ;;
  engine-clean)
    printf '%s\n' '{"ArtifactName":"fixture","Results":[]}' >"$report"
    exit 70 ;;
  malformed) printf '%s\n' '{"ArtifactName":"fixture","Results":null}' >"$report" ;;
  vulnerability)
    printf '%s\n' '{"ArtifactName":"fixture","Results":[{"Vulnerabilities":[{"VulnerabilityID":"CVE-fixture","PkgName":"fixture","InstalledVersion":"1","FixedVersion":"2"}]}]}' >"$report" ;;
  esac
else
  printf '%s\n' '{"ArtifactName":"fixture","Results":[]}' >"$report"
fi
STUB
chmod +x "$WORK/bin/gh" "$WORK/bin/git" "$WORK/bin/trivy"

fixture() {
  unset FAIL_TARGET FAIL_MODE ASSET_ERROR
  cat >"$WORK/release.json" <<'JSON'
{"tag_name":"v9.9.9","draft":false,"prerelease":false,"published_at":"2026-10-11T00:00:00Z","assets":[{"id":99,"name":"digests.txt","state":"uploaded"}]}
JSON
  cat >"$WORK/retained.json" <<'JSON'
{"tag_name":"v8.8.8","draft":false,"prerelease":false,"published_at":"2026-10-10T00:00:00Z","assets":[{"id":88,"name":"digests.txt","state":"uploaded"}]}
JSON
  : >"$WORK/digests.txt"
  local name arch i=0 digest
  for name in server web caddy observer; do
    for arch in arm64 amd64; do
      i=$((i + 1))
      printf -v digest '%064d' "$i"
      printf 'ghcr.io/dannyota/aboutme-%s@sha256:%s linux/%s\n' "$name" "$digest" "$arch" >>"$WORK/digests.txt"
    done
    if [ "$name" != observer ]; then
      printf 'ghcr.io/dannyota/aboutme-%s@sha256:%064d linux/arm64,linux/amd64\n' "$name" 99 >>"$WORK/digests.txt"
    fi
  done
  sed 's/sha256:0/sha256:a/' "$WORK/digests.txt" >"$WORK/retained-digests.txt"
}
run_scan() {
  : >"$CALL_LOG"
  : >"$API_LOG"
  status=0
  bash "$ROOT/scripts/security-scan.sh" weekly-scan "$WORK/bin/trivy" "$WORK/reports" "$@" >"$WORK/output" 2>&1 || status=$?
}
expect_status() {
  if [ "$1" = success ]; then
    [ "$status" -eq 0 ] || { cat "$WORK/output" >&2; fail 'expected successful scan'; }
  else
    [ "$status" -ne 0 ] || fail 'expected failed scan'
  fi
}
expect_count() {
  [ "$(wc -l <"$CALL_LOG")" -eq "$1" ] || fail "expected $1 image scans"
}
expect_all_targets() {
  local name arch digest ref i=0
  for name in server web caddy observer; do
    for arch in arm64 amd64; do
      i=$((i + 1))
      printf -v digest '%064d' "$i"
      ref="ghcr.io/dannyota/aboutme-$name@sha256:$digest"
      grep -Fq -- "--platform linux/$arch $ref" "$CALL_LOG" || fail "missing immutable target $name $arch"
      grep -Fq "$ref" "$WORK/reports/summary.txt" || fail "missing summary digest $name $arch"
      [ -s "$WORK/reports/released-$name-$arch.json" ] || fail "missing report $name $arch"
    done
  done
  ! grep -q '^git ' "$API_LOG" || fail 'selected a reachable Git tag'
}

fixture
run_scan
expect_status success
expect_count 8
expect_all_targets
grep -Fq 'v9.9.9' "$WORK/reports/summary.txt" || fail 'missing selected release'
printf 'ok - published release scans eight immutable platform digests\n'

fixture
run_scan v8.8.8
expect_status success
expect_count 8
! grep -q '/latest' "$API_LOG" || fail 'rollback selection queried latest release'
grep -Fq 'aboutme-server@sha256:a' "$CALL_LOG" || fail 'rollback ignored selected assets'

fixture
run_scan v9.9.9 v8.8.8
expect_status success
expect_count 8
grep -Fq 'aboutme-observer@sha256:a' "$CALL_LOG" || fail 'retained observer assets not used'
! grep -Fq 'aboutme-server@sha256:a' "$CALL_LOG" || fail 'observer override changed app selection'
grep -Fq 'v8.8.8' "$WORK/reports/summary.txt" || fail 'missing observer release'
printf 'ok - rollback and retained observer select independent published assets\n'

fixture
run_scan v7.7.7 v8.8.8
expect_status failure
expect_count 2
fixture
run_scan v9.9.9 v7.7.7
expect_status failure
expect_count 6
fixture
run_scan '../../tags/unsafe'
expect_status failure
expect_count 0
for mutation in '.draft = true' '.prerelease = true' '.published_at = null' '.tag_name = "v0.0.1"' '.assets = []' '.assets += .assets' '.assets[0].state = "new"'; do
  fixture
  jq "$mutation" "$WORK/release.json" >"$WORK/changed.json"
  mv "$WORK/changed.json" "$WORK/release.json"
  run_scan v9.9.9
  expect_status failure
  expect_count 0
done
fixture
export ASSET_ERROR=1
run_scan
expect_status failure
expect_count 0
printf 'ok - unpublished releases and missing or invalid assets fail closed\n'

for mutation in missing duplicate malformed; do
  fixture
  case $mutation in
  missing) sed '/aboutme-observer@.*linux\/arm64$/d' "$WORK/digests.txt" >"$WORK/changed-digests"; mv "$WORK/changed-digests" "$WORK/digests.txt" ;;
  duplicate) grep 'aboutme-observer@.*linux/arm64$' "$WORK/digests.txt" >>"$WORK/extra"; cat "$WORK/extra" >>"$WORK/digests.txt" ;;
  malformed) sed 's/aboutme-observer@sha256:/aboutme-observer@invalid:/' "$WORK/digests.txt" >"$WORK/changed-digests"; mv "$WORK/changed-digests" "$WORK/digests.txt" ;;
  esac
  run_scan
  expect_status failure
  if [ "$mutation" = malformed ]; then expect_count 6; else expect_count 7; fi
  grep -Fq 'observer' "$WORK/reports/failed.txt" || fail 'missing failed digest target'
done
printf 'ok - missing, duplicate, and malformed digests preserve other scans\n'

for mode in vulnerability engine engine-clean malformed no-report; do
  fixture
  export FAIL_TARGET='--platform linux/arm64 ghcr.io/dannyota/aboutme-observer@' FAIL_MODE="$mode"
  run_scan
  expect_status failure
  expect_count 8
  grep -Fq 'aboutme-observer@' "$WORK/reports/failed.txt" || fail "missing observer $mode failure"
  grep -Fq 'released-observer-amd64.json' "$CALL_LOG" || fail 'observer failure stopped next platform'
  if [ "$mode" = engine ] || [ "$mode" = no-report ]; then
    [ ! -e "$WORK/reports/released-observer-arm64.json" ] || fail 'scan reused a stale report'
  fi
done
fixture
export FAIL_TARGET='--platform linux/arm64 ghcr.io/dannyota/aboutme-server@' FAIL_MODE=engine
run_scan
expect_status failure
expect_count 8
fixture
run_scan
expect_status success
[ ! -s "$WORK/reports/failed.txt" ] || fail 'stale failure survived a clean run'
printf 'ok - observer findings and engine errors fail while collecting other targets\n'
