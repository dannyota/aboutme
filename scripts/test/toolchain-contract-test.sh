#!/usr/bin/env bash

# Contract tests for the local/hosted CI tool-version boundary.
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-toolchain-test.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT

fail() {
  printf 'toolchain-contract-test: %s\n' "$*" >&2
  exit 1
}

CHECKER=$ROOT/scripts/check-tool-versions.sh
[ -x "$CHECKER" ] || fail "$CHECKER is missing or not executable"

BIN=$WORK/bin
mkdir -p "$BIN"

cat >"$BIN/node" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' v24.19.0
EOF

cat >"$BIN/go" <<'EOF'
#!/usr/bin/env bash
if [ "${1:-}" = env ] && [ "${2:-}" = GOVERSION ]; then
  printf '%s\n' go1.26.6
  exit 0
fi
if [ "${1:-}" = version ] && [ "${2:-}" = -m ]; then
  case ${3##*/} in
  golangci-lint) module=github.com/golangci/golangci-lint/v2 version=v2.12.2 ;;
  govulncheck) module=golang.org/x/vuln version=v1.6.0 ;;
  gitleaks) module=github.com/zricethezav/gitleaks/v8 version=v8.30.1 ;;
  *) exit 2 ;;
  esac
  printf '%s\n' "$3: go1.26.6" "path fixture" "mod $module $version fixture"
  exit 0
fi
exit 2
EOF

cat >"$BIN/sqlc" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' v1.31.1
EOF

cat >"$BIN/semgrep" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' 1.172.0
EOF

cat >"$BIN/caddy" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' 'v2.11.4 fixture-checksum'
EOF

for name in golangci-lint govulncheck gitleaks; do
  cat >"$BIN/$name" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
done
chmod +x "$BIN"/*

PATH="$BIN:/usr/bin:/bin" "$CHECKER" ci >/dev/null
PATH="$BIN:/usr/bin:/bin" "$CHECKER" scan >/dev/null

cat >"$BIN/sqlc" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' v9.9.9
EOF
chmod +x "$BIN/sqlc"

if PATH="$BIN:/usr/bin:/bin" "$CHECKER" ci >"$WORK/out" 2>&1; then
  fail "CI tool check accepted the wrong sqlc version"
fi
grep -Fq 'sqlc: want v1.31.1, got v9.9.9' "$WORK/out" ||
  fail "CI tool check did not report the exact sqlc mismatch"

grep -Fq 'node-version: 24.19.0' "$ROOT/.github/workflows/ci.yml" ||
  fail "GitHub CI does not pin Node 24.19.0"
grep -Fq 'go-version: "1.26.6"' "$ROOT/.github/workflows/ci.yml" ||
  fail "GitHub CI does not pin Go 1.26.6"
grep -Fq 'version: v2.12.2' "$ROOT/.github/workflows/ci.yml" ||
  fail "GitHub CI does not pin golangci-lint v2.12.2"
grep -Fq 'govulncheck@v1.6.0' "$ROOT/.github/workflows/ci.yml" ||
  fail "GitHub CI does not pin govulncheck v1.6.0"
grep -Fq 'semgrep==1.172.0' "$ROOT/.github/workflows/ci.yml" ||
  fail "GitHub CI does not pin Semgrep 1.172.0"
grep -Fq 'sqlc@v1.31.1' "$ROOT/.github/workflows/ci.yml" ||
  fail "GitHub CI does not pin sqlc v1.31.1"
grep -Fq 'caddy@v2.11.4' "$ROOT/.github/workflows/ci.yml" ||
  fail "GitHub CI does not pin Caddy 2.11.4"
grep -Fq 'gitleaks/v8@v8.30.1' "$ROOT/.github/workflows/ci.yml" ||
  fail "GitHub CI does not pin gitleaks 8.30.1"
grep -Fq 'gitleaks detect --redact --no-banner' "$ROOT/.github/workflows/ci.yml" ||
  fail "GitHub CI does not run the full-history gitleaks gate"

cat >"$BIN/sqlc" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' v1.31.1
EOF
chmod +x "$BIN/sqlc"

CONTRACT=$WORK/contract
mkdir -p "$CONTRACT/scripts" "$CONTRACT/.github/workflows" \
  "$CONTRACT/apps/web" "$CONTRACT/apps/server" \
  "$CONTRACT/packages/schema/gen/go" "$CONTRACT/deploy"
cp "$ROOT/.tool-versions" "$CONTRACT/.tool-versions"
cp "$ROOT/scripts/check-tool-versions.sh" "$CONTRACT/scripts/"
cp "$ROOT/.github/workflows/ci.yml" "$CONTRACT/.github/workflows/"
cp "$ROOT/apps/web/.nvmrc" "$CONTRACT/apps/web/"
cp "$ROOT/apps/server/go.mod" "$CONTRACT/apps/server/"
cp "$ROOT/packages/schema/gen/go/go.mod" "$CONTRACT/packages/schema/gen/go/"
cp "$ROOT/go.work" "$CONTRACT/"
cp "$ROOT/deploy/web.Dockerfile" "$ROOT/deploy/server.Dockerfile" \
  "$ROOT/deploy/compose.yml" "$CONTRACT/deploy/"

awk '
  $1 == "node-version:" {
    count++
    if (count == 2) sub("24.19.0", "99.0.0")
  }
  { print }
' "$CONTRACT/.github/workflows/ci.yml" >"$CONTRACT/.github/workflows/ci.yml.next"
mv "$CONTRACT/.github/workflows/ci.yml.next" "$CONTRACT/.github/workflows/ci.yml"

if PATH="$BIN:/usr/bin:/bin" "$CONTRACT/scripts/check-tool-versions.sh" ci \
  >"$WORK/partial-drift.out" 2>&1; then
  fail "repository contract accepted one drifted Node job"
fi
grep -Fq 'node: .github/workflows/ci.yml has 99.0.0, want 24.19.0' \
  "$WORK/partial-drift.out" ||
  fail "repository contract did not identify the drifted Node job"

printf 'toolchain contract tests passed\n'
