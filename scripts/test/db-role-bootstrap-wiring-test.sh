#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-role-wiring.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT
mkdir -p "$WORK/repo/apps/server" "$WORK/bin"
# Only the host-port readiness probe is replaced; all bootstrap ordering runs
# through the real Make recipe with process fakes and no database connection.
sed 's@(echo > /dev/tcp/127.0.0.1/20432)@true@' "$ROOT/Makefile" >"$WORK/repo/Makefile"
cat >"$WORK/bin/podman" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "podman $*" >>"$ROLE_TEST_CALLS"
case "$1" in
ps) printf '%s\n' 'aboutme-test-db||' ;;
exec)
  if [[ "$*" == *'SELECT 1 FROM pg_database'* ]]; then printf '1\n'; fi
  ;;
*) exit 90 ;;
esac
SH
cat >"$WORK/bin/go" <<'SH'
#!/usr/bin/env bash
[[ "$*" == 'run ./cmd/db-role-bootstrap' ]] || exit 91
[[ ${CLUSTER_BOOTSTRAP_DATABASE_URL:-} == *'/postgres?sslmode=disable' ]] || exit 92
printf 'bootstrap\n' >>"$ROLE_TEST_CALLS"
exit "${ROLE_TEST_RESULT:-0}"
SH
chmod +x "$WORK/bin/podman" "$WORK/bin/go"

for result in 23 0; do
  calls="$WORK/calls-$result"
  : >"$calls"
  status=0
  (cd "$WORK/repo" && env -u SHELLOPTS PATH="$WORK/bin:/usr/bin:/bin" \
    ROLE_TEST_CALLS="$calls" ROLE_TEST_RESULT="$result" \
    /usr/bin/make --no-print-directory test-db-up) >"$WORK/output-$result" 2>&1 || status=$?
  grep -qx bootstrap "$calls" || { echo 'role-wiring: bootstrap was omitted' >&2; exit 1; }
  if [ "$result" -ne 0 ]; then
    [ "$status" -ne 0 ] || { echo 'role-wiring: bootstrap failure was ignored' >&2; exit 1; }
    if grep -q 'psql' "$calls"; then
      echo 'role-wiring: database setup continued after bootstrap failure' >&2
      exit 1
    fi
  else
    [ "$status" -eq 0 ] || { echo 'role-wiring: valid bootstrap failed' >&2; exit 1; }
    awk '/^bootstrap$/ { ready=1 } /psql/ { if (!ready) exit 1; seen=1 } END { if (!seen) exit 1 }' "$calls"
  fi
done
echo 'Database role bootstrap ordering and failure propagation passed'
