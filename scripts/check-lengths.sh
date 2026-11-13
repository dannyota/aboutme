#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"

MD_MAX=400
CODE_MAX=700
SERVER_CODE_MAX=400

generated='^(apps/web/app/api/generated/|packages/schema/gen/)|generated\.[a-z]+$'
tests='(_test\.(go|sh)|[-.]test\.sh|\.test\.[cm]?[jt]s|\.spec\.[cm]?[jt]s)$|(^|/)(test|tests|testdata)/'
server_go='^apps/server/.*\.go$'

violations=0
check() {
  local max=$1 file lines
  while IFS= read -r file; do
    [[ -f "$file" ]] || continue
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
