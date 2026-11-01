#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-media-s3-test.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT
REPO=$WORK/repo
BIN=$WORK/bin
mkdir -p "$REPO/scripts/lib" "$BIN" "$WORK/state"
cp "$ROOT/scripts/test-media-s3.sh" "$REPO/scripts/"
cp "$ROOT/scripts/lib/platform.sh" "$REPO/scripts/lib/"
export FIXTURE_STATE=$WORK/state CALL_LOG=$WORK/calls
: >"$CALL_LOG"

cat >"$BIN/podman" <<'STUB'
#!/usr/bin/env bash
set -eu
printf '%s\n' "$*" >>"$CALL_LOG"
case "$1 $2" in
  'ps -a') [ ! -f "$FIXTURE_STATE/container" ] || printf 'aboutme-test-media-s3|running\n' ;;
  'network exists') [ -f "$FIXTURE_STATE/network" ] ;;
  'network create') : >"$FIXTURE_STATE/network" ;;
  'network inspect') printf '%s\n' "${NETWORK_OWNER:-aboutme-test-media-s3}" ;;
  'network rm') rm -f "$FIXTURE_STATE/network" ;;
  'inspect aboutme-test-media-s3')
    case "$*" in
      *Config.Image*) printf '%s\n' 'cgr.dev/chainguard/minio@sha256:bd014394a80898e68c149f2311fdf8d5a2c2f3bb2c33b9327ae6d02b4b065ae1' ;;
      *NetworkSettings.Networks*) printf '%s\n' "${CONTAINER_ALIAS:-minio}" ;;
      *) exit 1 ;;
    esac ;;
  'port aboutme-test-media-s3') printf '%s\n' '127.0.0.1:20091' ;;
  'run -d') : >"$FIXTURE_STATE/container" ;;
  'run --rm') [ "${MC_HOST_test:-}" = 'http://0123456789abcdef:0123456789abcdef@minio:9000' ] ;;
  'rm -f'|'rm aboutme-test-media-s3') rm -f "$FIXTURE_STATE/container" ;;
  *) exit 1 ;;
esac
STUB
cat >"$BIN/git" <<'STUB'
#!/usr/bin/env bash
[ "$1" = check-ignore ]
STUB
cat >"$BIN/openssl" <<'STUB'
#!/usr/bin/env bash
printf '0123456789abcdef\n'
STUB
cat >"$BIN/curl" <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
cat >"$BIN/stat" <<'STUB'
#!/usr/bin/env bash
# Model BSD stat, including rejection of GNU options.
[ "$1" = -f ] && [ "$2" = '%Lp' ] || exit 1
printf '%s\n' "${FIXTURE_MODE:-600}"
STUB
chmod +x "$BIN"/*
export PATH="$BIN:$PATH"

run_s3() { bash "$REPO/scripts/test-media-s3.sh" "$@" >"$WORK/output" 2>&1; }
assert_call() { grep -Fxq -- "$1" "$CALL_LOG" || { printf 'missing call: %s\n' "$1" >&2; exit 1; }; }
assert_fails() { if run_s3 "$@"; then printf 'unexpected success: %s\n' "$*" >&2; exit 1; fi; }

run_s3 up
assert_call 'network create --label aboutme.test.owner=aboutme-test-media-s3 aboutme-test-media-s3'
assert_call 'run -d --rm --name aboutme-test-media-s3 --memory 512m --network aboutme-test-media-s3 --network-alias minio -p 127.0.0.1:20091:9000 -e MINIO_ROOT_USER -e MINIO_ROOT_PASSWORD cgr.dev/chainguard/minio@sha256:bd014394a80898e68c149f2311fdf8d5a2c2f3bb2c33b9327ae6d02b4b065ae1 server /data'
assert_call 'run --rm --network aboutme-test-media-s3 -e MC_HOST_test -e MC_CONFIG_DIR=/tmp/.mc cgr.dev/chainguard/minio-client@sha256:b8b144ab34694ecea25aa352c4be9de4c26ee2a02701521dce02ee5593c57338 mb --ignore-existing test/aboutme-test'
assert_call 'run --rm --network aboutme-test-media-s3 -e MC_HOST_test -e MC_CONFIG_DIR=/tmp/.mc cgr.dev/chainguard/minio-client@sha256:b8b144ab34694ecea25aa352c4be9de4c26ee2a02701521dce02ee5593c57338 anonymous set none test/aboutme-test'
run_s3 up
run_s3 run true
CONTAINER_ALIAS=wrong assert_fails up
FIXTURE_MODE=644 assert_fails run true
NETWORK_OWNER=foreign assert_fails up
mv "$REPO/.dev/test-s3.env" "$REPO/.dev/saved.env"
ln -s saved.env "$REPO/.dev/test-s3.env"
assert_fails run true
rm "$REPO/.dev/test-s3.env"
mv "$REPO/.dev/saved.env" "$REPO/.dev/test-s3.env"
run_s3 down
assert_call 'network rm aboutme-test-media-s3'
[ ! -e "$FIXTURE_STATE/container" ] && [ ! -e "$FIXTURE_STATE/network" ] && [ ! -e "$REPO/.dev/test-s3.env" ]
! grep -Fq '0123456789abcdef' "$CALL_LOG" "$WORK/output"
printf 'media S3 contract tests passed\n'
