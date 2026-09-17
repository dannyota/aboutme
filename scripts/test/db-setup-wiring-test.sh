#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-db-setup-wiring.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT
mkdir -p "$WORK/repo/apps/server" "$WORK/bin"
# Only the host-port readiness probe is replaced; all setup ordering runs
# through the real Make recipes with process fakes and no database connection.
sed 's@(echo > /dev/tcp/127.0.0.1/20432)@true@' "$ROOT/Makefile" >"$WORK/repo/Makefile"
cat >"$WORK/bin/podman" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "podman $*" >>"$DB_SETUP_TEST_CALLS"
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
if [[ "$*" == 'run ./cmd/migrate' || "$*" == 'run ./cmd/migrate -check' ]]; then
  printf 'migrate:%s\n' "$*" >>"$DB_SETUP_TEST_CALLS"
  exit 0
fi
[[ "$*" == 'run ./cmd/db-setup' ]] || exit 91
case "${DATABASE_URL:-}" in
*/aboutme?sslmode=disable)
  printf 'db-setup:aboutme\n' >>"$DB_SETUP_TEST_CALLS"
  exit "${DB_SETUP_TEST_RESULT_ABOUTME:-0}"
  ;;
*/aboutme_dev?sslmode=disable)
  printf 'db-setup:aboutme_dev\n' >>"$DB_SETUP_TEST_CALLS"
  exit "${DB_SETUP_TEST_RESULT_ABOUTME_DEV:-0}"
  ;;
*) exit 92 ;;
esac
SH
chmod +x "$WORK/bin/podman" "$WORK/bin/go"

for result in 23 0; do
  calls="$WORK/calls-$result"
  : >"$calls"
  status=0
  (cd "$WORK/repo" && env -u SHELLOPTS PATH="$WORK/bin:/usr/bin:/bin" \
    DB_SETUP_TEST_CALLS="$calls" DB_SETUP_TEST_RESULT_ABOUTME="$result" \
    /usr/bin/make --no-print-directory test-db-up) >"$WORK/output-$result" 2>&1 || status=$?
  grep -q 'psql' "$calls" || { echo 'db-setup-wiring: aboutme_dev database check was omitted' >&2; exit 1; }
  grep -qx 'db-setup:aboutme' "$calls" || { echo 'db-setup-wiring: aboutme setup was omitted' >&2; exit 1; }
  if [ "$result" -ne 0 ]; then
    [ "$status" -ne 0 ] || { echo 'db-setup-wiring: aboutme setup failure was ignored' >&2; exit 1; }
    if grep -qx 'db-setup:aboutme_dev' "$calls"; then
      echo 'db-setup-wiring: aboutme_dev setup ran after aboutme setup failed' >&2
      exit 1
    fi
  else
    [ "$status" -eq 0 ] || { echo 'db-setup-wiring: valid setup failed' >&2; exit 1; }
    awk '/^db-setup:aboutme$/ { ready=1 } /^db-setup:aboutme_dev$/ { if (!ready) exit 1; seen=1 } END { if (!seen) exit 1 }' "$calls" || {
      echo 'db-setup-wiring: aboutme setup must precede aboutme_dev setup' >&2
      exit 1
    }
  fi
done

for target in migrate migrate-check; do
  calls="$WORK/calls-$target"
  : >"$calls"
  (cd "$WORK/repo" && env -u SHELLOPTS -u MIGRATION_IDENTITY \
    PATH="$WORK/bin:/usr/bin:/bin" DB_SETUP_TEST_CALLS="$calls" \
    /usr/bin/make --no-print-directory "$target") >"$WORK/output-$target" 2>&1 || {
    echo "db-setup-wiring: $target failed" >&2
    exit 1
  }
  grep -q '^migrate:' "$calls" || { echo "db-setup-wiring: $target did not run migrate" >&2; exit 1; }
  if grep -q 'db-setup' "$calls"; then
    echo "db-setup-wiring: $target must not run db-setup itself" >&2
    exit 1
  fi
done

echo 'Database role and grant setup ordering and failure propagation passed'
