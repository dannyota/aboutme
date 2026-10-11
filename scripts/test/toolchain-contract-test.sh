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

declare -A expected=()
while read -r name version; do
  [[ -z $name || $name == \#* ]] && continue
  expected[$name]=$version
done <"$ROOT/.tool-versions"
export FIXTURE_NODE=${expected[nodejs]} FIXTURE_GO=${expected[golang]}
export FIXTURE_SQLC=${expected[sqlc]} FIXTURE_SEMGREP=${expected[semgrep]}
export FIXTURE_CADDY=${expected[caddy]} FIXTURE_GITLEAKS=${expected[gitleaks]}
export FIXTURE_GOLANGCI=${expected[golangci-lint]} FIXTURE_GOVULNCHECK=${expected[govulncheck]}

BIN=$WORK/bin
mkdir -p "$BIN"

cat >"$BIN/node" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "v$FIXTURE_NODE"
EOF

cat >"$BIN/go" <<'EOF'
#!/usr/bin/env bash
if [ "${1:-}" = env ] && [ "${2:-}" = GOVERSION ]; then
  printf '%s\n' "go$FIXTURE_GO"
  exit 0
fi
if [ "${1:-}" = version ] && [ "${2:-}" = -m ]; then
  case ${3##*/} in
  golangci-lint) module=github.com/golangci/golangci-lint/v2 version=v$FIXTURE_GOLANGCI ;;
  govulncheck) module=golang.org/x/vuln version=v$FIXTURE_GOVULNCHECK ;;
  gitleaks) module=github.com/zricethezav/gitleaks/v8 version=v$FIXTURE_GITLEAKS ;;
  *) exit 2 ;;
  esac
  printf '%s\n' "$3: go$FIXTURE_GO" "path fixture" "mod $module $version fixture"
  exit 0
fi
exit 2
EOF

cat >"$BIN/sqlc" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "v$FIXTURE_SQLC"
EOF

cat >"$BIN/semgrep" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$FIXTURE_SEMGREP"
EOF

cat >"$BIN/caddy" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "v$FIXTURE_CADDY fixture-checksum"
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
grep -Fq "sqlc: want v$FIXTURE_SQLC, got v9.9.9" "$WORK/out" ||
  fail "CI tool check did not report the exact sqlc mismatch"

grep -Fq 'gitleaks detect --redact --no-banner' "$ROOT/.github/workflows/ci.yml" \
  "$ROOT"/.github/actions/*/action.yml ||
  fail "GitHub CI does not run the full-history gitleaks gate"
[ -x "$ROOT/scripts/generate-public-roots.mjs" ] ||
  fail "public-root generator is missing or not executable"
head -n 1 "$ROOT/scripts/generate-public-roots.mjs" | grep -Fqx '#!/usr/bin/env node' ||
  fail "public-root generator does not use the pinned Node runtime"
node --check "$ROOT/scripts/generate-public-roots.mjs" ||
  fail "public-root generator does not parse under the pinned Node runtime"

cat >"$BIN/sqlc" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "v$FIXTURE_SQLC"
EOF
chmod +x "$BIN/sqlc"

CONTRACT=$WORK/contract
mkdir -p "$CONTRACT/scripts" "$CONTRACT/.github/workflows" \
  "$CONTRACT/apps/web" "$CONTRACT/apps/server" \
  "$CONTRACT/packages/schema/gen/go" "$CONTRACT/deploy/caddy/production"
cp "$ROOT/.tool-versions" "$CONTRACT/.tool-versions"
cp "$ROOT/scripts/check-tool-versions.sh" "$CONTRACT/scripts/"
cp "$ROOT/.github/workflows/ci.yml" "$CONTRACT/.github/workflows/"
cp "$ROOT/.github/workflows/security-scan.yml" "$CONTRACT/.github/workflows/"
cp -R "$ROOT/.github/actions" "$CONTRACT/.github/"
cp "$ROOT/apps/web/.nvmrc" "$CONTRACT/apps/web/"
cp "$ROOT/apps/server/go.mod" "$CONTRACT/apps/server/"
cp "$ROOT/packages/schema/gen/go/go.mod" "$CONTRACT/packages/schema/gen/go/"
cp "$ROOT/go.work" "$CONTRACT/"
cp "$ROOT/deploy/web.Dockerfile" "$ROOT/deploy/server.Dockerfile" \
  "$ROOT/deploy/compose.yml" "$CONTRACT/deploy/"
cp "$ROOT/deploy/caddy/production/Dockerfile" "$CONTRACT/deploy/caddy/production/"

awk '
  $1 == "node-version:" {
    count++
    if (count == 2) $2 = "99.0.0"
  }
  { print }
' "$CONTRACT/.github/workflows/ci.yml" >"$CONTRACT/.github/workflows/ci.yml.next"
mv "$CONTRACT/.github/workflows/ci.yml.next" "$CONTRACT/.github/workflows/ci.yml"

if PATH="$BIN:/usr/bin:/bin" "$CONTRACT/scripts/check-tool-versions.sh" ci \
  >"$WORK/partial-drift.out" 2>&1; then
  fail "repository contract accepted one drifted Node job"
fi
grep -Fq "node: .github/workflows/ci.yml has 99.0.0, want $FIXTURE_NODE" \
  "$WORK/partial-drift.out" ||
  fail "repository contract did not identify the drifted Node job"

# A drifted pin in a composite action the workflow uses is drift too.
cp "$ROOT/.github/workflows/ci.yml" "$CONTRACT/.github/workflows/"
sed -i 's/go-version:.*/go-version: "1.99.0"/' \
  "$CONTRACT/.github/actions/setup-go/action.yml"
if PATH="$BIN:/usr/bin:/bin" "$CONTRACT/scripts/check-tool-versions.sh" ci \
  >"$WORK/action-drift.out" 2>&1; then
  fail "repository contract accepted a drifted Go pin in a composite action"
fi
grep -Fq "go: .github/actions/setup-go/action.yml has 1.99.0, want $FIXTURE_GO" \
  "$WORK/action-drift.out" ||
  fail "repository contract did not identify the drifted composite action"

node --test "$ROOT/scripts/test/workflow-contract.test.mjs"

printf 'toolchain contract tests passed\n'
