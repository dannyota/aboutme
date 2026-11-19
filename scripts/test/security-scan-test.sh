#!/usr/bin/env bash

set -Eeuo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-security-scan.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT
mkdir -p "$WORK/bin"
ln -s "$(command -v bash)" "$WORK/bin/bash"
ln -s "$(command -v jq)" "$WORK/bin/jq"
ln -s "$(command -v sha256sum)" "$WORK/bin/sha256sum"
ln -s "$(command -v tar)" "$WORK/bin/tar"
export FIXTURE_DIR="$WORK" CALL_LOG="$WORK/calls" API_LOG="$WORK/api"
export PATH="$WORK/bin:/usr/bin:/bin"

fail() { printf 'not ok - %s\n' "$*" >&2; exit 1; }

cat >"$WORK/bin/gh" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$API_LOG"
[ "$1" = api ] || exit 64
repo=repos/${EXPECT_REPOSITORY:-dannyota/aboutme}/releases
case $2 in
"$repo/latest" | "$repo/tags/v9.9.9")
  cat "$FIXTURE_DIR/release.json" ;;
"$repo/tags/v8.8.8")
  cat "$FIXTURE_DIR/retained.json" ;;
"$repo/assets/99")
  [ "$*" = "api $repo/assets/99 -H Accept:application/octet-stream" ] || exit 64
  [ "${ASSET_ERROR:-0}" = 0 ] || exit 70
  cat "$FIXTURE_DIR/digests.txt" ;;
"$repo/assets/88")
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

cat >"$WORK/bin/fetch-trivy" <<'STUB'
#!/usr/bin/env bash
set -eu
printf '%s\n' "$*" >>"$FETCH_CALL_LOG"
case "$*" in
  *public.ecr.aws*) exit 1 ;;
esac
[ "${FAIL_ALL_FETCHES:-0}" = 0 ] || exit 1
mkdir -p "$TRIVY_CACHE_DIR/db"
printf fixture >"$TRIVY_CACHE_DIR/db/trivy.db"
printf fixture >"$TRIVY_CACHE_DIR/db/metadata.json"
STUB
chmod +x "$WORK/bin/fetch-trivy"
export TRIVY_CACHE_DIR="$WORK/fetch-cache" FETCH_CALL_LOG="$WORK/fetch-calls"
: >"$FETCH_CALL_LOG"
bash "$ROOT/scripts/security-scan.sh" fetch-db "$WORK/bin/fetch-trivy" "$TRIVY_CACHE_DIR"
[ "$(wc -l <"$FETCH_CALL_LOG")" -eq 2 ] || fail 'fetch must retry GHCR after a registry error'
grep -Fq -- '--db-repository public.ecr.aws/aquasecurity/trivy-db:2 --db-repository ghcr.io/aquasecurity/trivy-db:2' "$FETCH_CALL_LOG" || fail 'fetch must order the repositories with GHCR last'
tail -n 1 "$FETCH_CALL_LOG" | grep -Fq -- '--db-repository ghcr.io/aquasecurity/trivy-db:2' || fail 'fallback must use GHCR directly'

cat >"$WORK/bin/sleep" <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
chmod +x "$WORK/bin/sleep"
: >"$FETCH_CALL_LOG"
if FAIL_ALL_FETCHES=1 bash "$ROOT/scripts/security-scan.sh" fetch-db "$WORK/bin/fetch-trivy" "$TRIVY_CACHE_DIR"; then
  fail 'exhausted database retries must fail'
fi
[ "$(wc -l <"$FETCH_CALL_LOG")" -eq 3 ] || fail 'database retries must be bounded'
rm "$WORK/bin/sleep"

export TRIVY_CACHE_DIR="$WORK/trivy-cache"
: >"$CALL_LOG"
if bash "$ROOT/scripts/security-scan.sh" scan-image "$WORK/bin/trivy" fixture "" "$WORK/report.json" >"$WORK/output" 2>&1; then
  fail 'missing database must fail the scan'
fi
[ ! -s "$CALL_LOG" ] || fail 'missing database must not invoke Trivy'
mkdir -p "$TRIVY_CACHE_DIR/db"
printf fixture >"$TRIVY_CACHE_DIR/db/trivy.db"
printf fixture >"$TRIVY_CACHE_DIR/db/metadata.json"
bash "$ROOT/scripts/security-scan.sh" scan-image "$WORK/bin/trivy" fixture "" "$WORK/report.json"
grep -Fq -- "--cache-dir $TRIVY_CACHE_DIR" "$CALL_LOG" || fail 'scan must use the handed-off cache'
grep -Fq -- '--skip-db-update' "$CALL_LOG" || fail 'scan must skip database updates'

fixture() {
  local owner=${1:-dannyota} retained_owner=${2:-${1:-dannyota}}
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
      printf 'ghcr.io/%s/aboutme-%s@sha256:%s linux/%s\n' "$owner" "$name" "$digest" "$arch" >>"$WORK/digests.txt"
    done
    if [ "$name" != observer ]; then
      printf 'ghcr.io/%s/aboutme-%s@sha256:%064d linux/arm64,linux/amd64\n' "$owner" "$name" 99 >>"$WORK/digests.txt"
    fi
  done
  sed -e 's/sha256:0/sha256:a/' -e "s|^ghcr.io/$owner/|ghcr.io/$retained_owner/|" "$WORK/digests.txt" >"$WORK/retained-digests.txt"
}
run_scan() {
  : >"$CALL_LOG"
  : >"$API_LOG"
  status=0
  bash "${SCAN_ROOT:-$ROOT}/scripts/security-scan.sh" weekly-scan "$WORK/bin/trivy" "$WORK/reports" "$@" >"$WORK/output" 2>&1 || status=$?
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
  local owner=${1:-dannyota} name arch digest ref i=0
  for name in server web caddy observer; do
    for arch in arm64 amd64; do
      i=$((i + 1))
      printf -v digest '%064d' "$i"
      ref="ghcr.io/$owner/aboutme-$name@sha256:$digest"
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

TREE=$WORK/dntmakers-tree
mkdir -p "$TREE/scripts/lib"
cp "$ROOT/scripts/security-scan.sh" "$ROOT/scripts/release-eligibility.sh" "$TREE/scripts/"
printf '# shellcheck shell=bash disable=SC2034\nowner=dntmakers\nname=aboutme\n' >"$TREE/scripts/lib/repository.sh"
fixture dntmakers
SCAN_ROOT=$TREE EXPECT_REPOSITORY=dntmakers/aboutme run_scan
expect_status success
expect_count 8
expect_all_targets dntmakers
! grep -Fq 'ghcr.io/dannyota/' "$CALL_LOG" || fail 'a dntmakers release scanned a dannyota image'
fixture dntmakers dannyota
SCAN_ROOT=$TREE EXPECT_REPOSITORY=dntmakers/aboutme run_scan v9.9.9 v8.8.8
expect_status success
expect_count 8
grep -Fq -- '--platform linux/arm64 ghcr.io/dannyota/aboutme-observer@sha256:a' "$CALL_LOG" ||
  fail 'a retained dannyota observer was not scanned in its own namespace'
grep -Fq -- '--platform linux/arm64 ghcr.io/dntmakers/aboutme-server@sha256:0' "$CALL_LOG" ||
  fail 'a dntmakers app release was not scanned in its own namespace'
fixture dannyota
SCAN_ROOT=$TREE run_scan
expect_status failure
expect_count 0
grep -q '^api repos/dntmakers/aboutme/releases/latest$' "$API_LOG" ||
  fail 'the release repository does not come from scripts/lib/repository.sh'
printf 'owner=dntmakers\n' >"$TREE/scripts/lib/repository.sh"
fixture dntmakers
SCAN_ROOT=$TREE EXPECT_REPOSITORY=dntmakers/aboutme run_scan
expect_status failure
expect_count 0
! grep -q '^api ' "$API_LOG" || fail 'a malformed repository file reached the release API'
printf 'ok - each release names its own namespace and the repository file names the API repository\n'

for mutation in mixed unknown uppercase extra; do
  fixture
  case $mutation in
  mixed) sed 's|^ghcr.io/dannyota/aboutme-web@|ghcr.io/dntmakers/aboutme-web@|' "$WORK/digests.txt" >"$WORK/changed-digests" ;;
  unknown) sed 's|^ghcr.io/dannyota/|ghcr.io/evil/|' "$WORK/digests.txt" >"$WORK/changed-digests" ;;
  uppercase) sed 's|^ghcr.io/dannyota/|ghcr.io/Dannyota/|' "$WORK/digests.txt" >"$WORK/changed-digests" ;;
  extra) { cat "$WORK/digests.txt"; printf 'ghcr.io/evil/aboutme-extra@sha256:%064d linux/arm64\n' 7; } >"$WORK/changed-digests" ;;
  esac
  mv "$WORK/changed-digests" "$WORK/digests.txt"
  run_scan
  expect_status failure
  expect_count 0
  [ "$(grep -c 'missing or invalid release digest' "$WORK/reports/failed.txt")" -eq 8 ] ||
    fail "$mutation namespace did not fail every target"
done
fixture
sed -e 's|^ghcr.io/dannyota/|ghcr.io/dntmakers/|' \
  -e 's|^ghcr.io/dntmakers/aboutme-observer@\(.*\) linux/amd64$|ghcr.io/dannyota/aboutme-observer@\1 linux/amd64|' \
  "$WORK/retained-digests.txt" >"$WORK/changed-digests"
mv "$WORK/changed-digests" "$WORK/retained-digests.txt"
run_scan v9.9.9 v8.8.8
expect_status failure
expect_count 6
printf 'ok - a release that mixes or names an unknown owner fails closed\n'

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

for name in curl wget; do
  cat >"$WORK/bin/$name" <<'STUB'
#!/usr/bin/env bash
printf '%s %s\n' "${0##*/}" "$*" >>"$FETCH_LOG"
STUB
done
cat >"$WORK/bin/uname" <<'STUB'
#!/usr/bin/env bash
[ "$1" = -m ] || exit 64
printf '%s\n' "$FIXTURE_MACHINE"
STUB
chmod +x "$WORK/bin/curl" "$WORK/bin/wget" "$WORK/bin/uname"
export FETCH_LOG="$WORK/fetch.log"
version=$(sed -n 's/^TRIVY_VERSION=//p' "$ROOT/scripts/security-scan.sh")
archives=$WORK/trivy-archives
mkdir -p "$archives"
cp "$ROOT/scripts/security-scan.sh" "$WORK/security-scan.sh"
for arch in amd64 arm64; do
  case $arch in
  amd64) asset="trivy_${version}_Linux-64bit.tar.gz" pin=TRIVY_SHA256_LINUX_AMD64 ;;
  arm64) asset="trivy_${version}_Linux-ARM64.tar.gz" pin=TRIVY_SHA256_LINUX_ARM64 ;;
  esac
  mkdir -p "$WORK/trivy-src-$arch"
  printf '#!/bin/sh\nprintf "Version: %s\\n"\n' "$arch" >"$WORK/trivy-src-$arch/trivy"
  chmod +x "$WORK/trivy-src-$arch/trivy"
  printf 'not extracted\n' >"$WORK/trivy-src-$arch/README.md"
  tar -czf "$archives/$asset" -C "$WORK/trivy-src-$arch" trivy README.md
  hash=$(sha256sum "$archives/$asset")
  sed "s/^$pin=.*/$pin=${hash%% *}/" "$WORK/security-scan.sh" >"$WORK/security-scan.next"
  mv "$WORK/security-scan.next" "$WORK/security-scan.sh"
done
install_trivy() {
  rm -rf "$WORK/trivy-bin" "$FETCH_LOG"
  status=0
  FIXTURE_MACHINE=$1 bash "$WORK/security-scan.sh" install-trivy "$WORK/trivy-bin" "${@:2}" >"$WORK/output" 2>&1 || status=$?
  [ ! -e "$FETCH_LOG" ] || fail 'install-trivy fell back to a download'
}
for machine in x86_64 aarch64; do
  install_trivy "$machine" "$archives"
  expect_status success
  case $machine in x86_64) arch=amd64 ;; aarch64) arch=arm64 ;; esac
  grep -Fxq "Version: $arch" "$WORK/output" || fail "install-trivy chose the wrong archive on $machine"
  [ "$(ls "$WORK/trivy-bin")" = trivy ] || fail 'install-trivy extracted more than the binary'
done
install_trivy x86_64
expect_status failure
grep -Fq 'usage' "$WORK/output" || fail 'install-trivy without an archive directory must print usage'
install_trivy x86_64 "$WORK/empty"
expect_status failure
grep -Fq "trivy_${version}_Linux-64bit.tar.gz" "$WORK/output" || fail 'install-trivy must name the missing archive'
[ ! -e "$WORK/trivy-bin/trivy" ] || fail 'install-trivy installed without an archive'
cp -R "$archives" "$WORK/corrupt-archives"
printf 'corrupt\n' >"$WORK/corrupt-archives/trivy_${version}_Linux-64bit.tar.gz"
install_trivy x86_64 "$WORK/corrupt-archives"
expect_status failure
[ ! -e "$WORK/trivy-bin/trivy" ] || fail 'install-trivy extracted a corrupt archive'
install_trivy riscv64 "$archives"
expect_status failure
grep -Fq 'unsupported architecture riscv64' "$WORK/output" || fail 'install-trivy must reject an unsupported architecture'
FIXTURE_MACHINE=x86_64 bash "$ROOT/scripts/security-scan.sh" install-trivy "$WORK/trivy-bin" "$archives" >"$WORK/output" 2>&1 &&
  fail 'install-trivy accepted an archive that does not match the pinned sha256'
printf 'ok - install-trivy installs only the pinned archive from the ci-tools directory\n'
