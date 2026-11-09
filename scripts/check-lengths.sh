#!/usr/bin/env bash
# Fails when a tracked Markdown doc passes MD_MAX lines, a non-test Go file
# under apps/server passes SERVER_CODE_MAX lines, or another non-test source
# file passes CODE_MAX lines. Split an oversize file by topic (Go: a sibling
# file in the same package; docs: a focused page) or trim it; git keeps history.
# It reads working-tree files, so the pre-commit hook checks the whole tree,
# not only staged content.
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"

MD_MAX=450
CODE_MAX=700
SERVER_CODE_MAX=400

# Generated paths without a standard header. sqlc output has the header, so no
# .sql.go pattern belongs here.
generated='^(apps/web/app/api/generated/|packages/schema/gen/)|generated\.[a-z]+$'
tests='(_test\.(go|sh)|[-.]test\.sh|\.test\.[cm]?[jt]s|\.spec\.[cm]?[jt]s)$|(^|/)(test|tests|testdata)/'
server_go='^apps/server/.*\.go$'

violations=0
check() {
  local max=$1 file lines
  while IFS= read -r file; do
    [[ -f "$file" ]] || continue
    # Generated files follow their generator, not authored size.
    head -n 5 "$file" | grep -Eq 'Code generated .* DO NOT EDIT|@generated' && continue
    lines=$(wc -l <"$file")
    if ((lines > max)); then
      echo "too long: $file has $lines lines (max $max)"
      violations=$((violations + 1))
    fi
  done
}

code=$(
  git ls-files -- '*.go' '*.ts' '*.vue' '*.mjs' '*.js' '*.sh' |
    grep -Ev "$generated" | grep -Ev "$tests" || true
)

check "$MD_MAX" < <(git ls-files -- '*.md')
check "$SERVER_CODE_MAX" < <(grep -E "$server_go" <<<"$code" || true)
check "$CODE_MAX" < <(grep -Ev "$server_go" <<<"$code" || true)

if ((violations > 0)); then
  echo "check-lengths: $violations file(s) over the limit" >&2
  exit 1
fi
echo "check-lengths: ok (docs <= $MD_MAX, apps/server Go <= $SERVER_CODE_MAX, other code <= $CODE_MAX lines)"
