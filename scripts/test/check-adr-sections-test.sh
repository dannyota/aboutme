#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aboutme-adr-sections.XXXXXX")
trap 'rm -rf -- "$WORK"' EXIT

fail() { printf 'check-adr-sections-test: %s\n' "$*" >&2; exit 1; }

git init -q "$WORK/repo"
cd "$WORK/repo"
mkdir -p docs/adr/history

cat >docs/adr/0001-complete.md <<'EOF'
# Complete record

## Context

A choice needs a record.

## Decision

Keep the record complete.

## Alternatives rejected

- An incomplete record hides the trade-off.

## Consequences

Reviewers can see the trade-off.

## History

Initial decision.
EOF
printf '# Index\n' >docs/adr/README.md
printf '# Historical record\n' >docs/adr/history/0002-archived.md
printf '# Other document\n' >docs/adr/123-not-record.md
printf '# Untracked record\n' >docs/adr/0003-untracked.md
git add -- docs/adr/0001-complete.md docs/adr/README.md \
  docs/adr/history/0002-archived.md docs/adr/123-not-record.md

bash "$ROOT/scripts/check-adr-sections.sh" >"$WORK/output" 2>&1
[[ ! -s $WORK/output ]] || fail 'complete record or ignored paths produced output'

assert_missing() {
  local section=$1 status=0
  bash "$ROOT/scripts/check-adr-sections.sh" >"$WORK/output" 2>&1 || status=$?
  [[ $status == 1 ]] || fail "expected missing-section exit 1, got $status"
  grep -Fqx -- "docs/adr/0002-incomplete.md: missing ## $section" "$WORK/output" ||
    fail "missing diagnostic for $section"
}

sed '/^## Alternatives rejected$/d' docs/adr/0001-complete.md >docs/adr/0002-incomplete.md
git add -- docs/adr/0002-incomplete.md
assert_missing 'Alternatives rejected'

sed '/^## History$/d' docs/adr/0001-complete.md >docs/adr/0002-incomplete.md
assert_missing History

printf '# Incomplete record\n' >docs/adr/0002-incomplete.md
assert_missing 'Alternatives rejected'
assert_missing History

sed 's/^## Alternatives rejected$/### Alternatives rejected/' \
  docs/adr/0001-complete.md >docs/adr/0002-incomplete.md
assert_missing 'Alternatives rejected'

cp docs/adr/0001-complete.md docs/adr/0002-incomplete.md
bash "$ROOT/scripts/check-adr-sections.sh" >"$WORK/output" 2>&1
[[ ! -s $WORK/output ]] || fail 'repaired working-tree record produced output'

mkdir "$WORK/bin"
cat >"$WORK/bin/git" <<'EOF'
#!/usr/bin/env bash
if [[ ${1:-} == ls-files ]]; then
  exit 1
fi
exec "$ADR_SECTIONS_TEST_GIT" "$@"
EOF
chmod +x "$WORK/bin/git"
if ADR_SECTIONS_TEST_GIT=$(command -v git) PATH="$WORK/bin:$PATH" \
  bash "$ROOT/scripts/check-adr-sections.sh" >"$WORK/output" 2>&1; then
  fail 'failed Git read passed'
fi

printf 'check-adr-sections-test: passed\n'
