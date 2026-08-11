#!/usr/bin/env bash
# Resume/idempotency write choke-point policy regression (Phase 2A, task 6b).
#
# The resume domain store (apps/server/internal/resume) owns every write to
# `resumes` and `idempotency_records`: title and document validation, the D7
# resume cap, the revision CAS, and the D11 idempotency-record lifecycle. The
# sqlc-generated methods that perform those writes are exported Go API, so
# nothing in the compiler stops another package from calling them directly and
# skipping all of it. The semgrep rule `go-resume-write-outside-domain-store`
# (.semgrep.yml) is that missing enforcement; this script is its regression:
#
#   1. the real tree has no caller of a covered method outside
#      internal/resume (and outside the generated internal/store itself);
#   2. a temporary outside caller (internal/api) of EVERY covered method is
#      flagged by the rule -- if the rule is deleted, weakened, or its path
#      filters stop matching, this fails;
#   3. the identical calls under internal/resume are NOT flagged, so the
#      authorized caller keeps working;
#   4. every named query in apps/server/sql/queries.sql that INSERTs,
#      UPDATEs or DELETEs `resumes`/`idempotency_records`, plus the per-user
#      FOR UPDATE lock that serializes those writes, is covered by the rule --
#      so a query added later cannot silently bypass the choke point;
#   5. every covered method still exists in the generated Querier, so a
#      renamed query cannot leave a dead manifest entry enforcing nothing.
#
# The covered-method manifest is deliberately NOT duplicated in this script:
# it is read out of the rule's own `$METHOD` regex in .semgrep.yml. The rule
# is the single source of truth, so what this script checks can never drift
# from what semgrep actually enforces. Adding a method to the rule extends the
# fixtures and the queries.sql check automatically.
#
# The two fixtures are generated, temporary, and deleted by an EXIT trap. Their
# file names start with `_`, which the go tool ignores, so they never enter a
# build, vet, lint, or test run even while they are on disk.
#
# Usage: scripts/test-resume-write-chokepoint.sh   (this is `make semgrep-policy-test`)
set -Eeuo pipefail

cd "$(dirname "$0")/.."

RULE_ID="go-resume-write-outside-domain-store"
CONFIG=".semgrep.yml"
QUERIES="apps/server/sql/queries.sql"
QUERIER="apps/server/internal/store/querier.go"
OUTSIDE_FIXTURE="apps/server/internal/api/_chokepoint_outside_fixture.go"
INSIDE_FIXTURE="apps/server/internal/resume/_chokepoint_inside_fixture.go"

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

for tool in semgrep jq awk; do
  command -v "$tool" >/dev/null 2>&1 || fail "$tool is required but not on PATH."
done
for f in "$CONFIG" "$QUERIES" "$QUERIER"; do
  [ -f "$f" ] || fail "expected $f to exist (run this from anywhere in the repo)."
done

# Never clobber a tracked file: if one of the fixture paths is tracked, this
# script would delete a real source file on cleanup.
for f in "$OUTSIDE_FIXTURE" "$INSIDE_FIXTURE"; do
  if git ls-files --error-unmatch "$f" >/dev/null 2>&1; then
    fail "$f is tracked by git. It is a temporary fixture path and must never be committed."
  fi
done

PROBE_CONFIG=""

cleanup() {
  local status=$?
  rm -f "$OUTSIDE_FIXTURE" "$INSIDE_FIXTURE" ${PROBE_CONFIG:+"$PROBE_CONFIG"}
  if [ -e "$OUTSIDE_FIXTURE" ] || [ -e "$INSIDE_FIXTURE" ]; then
    echo "FAIL: could not remove the temporary fixtures; delete them manually:" >&2
    echo "  $OUTSIDE_FIXTURE" >&2
    echo "  $INSIDE_FIXTURE" >&2
    exit 1
  fi
  exit "$status"
}
trap cleanup EXIT INT TERM

# ---------------------------------------------------------------------------
# The covered-method manifest, read from the rule itself.
# ---------------------------------------------------------------------------

# The rule block runs from its `- id:` line to the next top-level `- id:` line.
# Inside it, exactly one `regex: "^(A|B|C)$"` line carries the manifest; more
# than one (or none) means the rule was restructured and this script must be
# updated with it rather than silently checking the wrong list.
rule_block="$(awk -v id="  - id: ${RULE_ID}" '
  $0 == id { inside = 1; next }
  inside && /^  - id: / { inside = 0 }
  inside { print }
' "$CONFIG")"
[ -n "$rule_block" ] || fail "rule '$RULE_ID' not found in $CONFIG. The write choke point is unenforced."

manifest_line="$(printf '%s\n' "$rule_block" | grep -c -E '^[[:space:]]+regex: "\^\(.*\)\$"$' || true)"
[ "$manifest_line" = "1" ] ||
  fail "expected exactly one covered-method regex line in rule '$RULE_ID', found $manifest_line."

alternation="$(printf '%s\n' "$rule_block" |
  sed -n -E 's/^[[:space:]]+regex: "\^\((.*)\)\$"$/\1/p')"
[ -n "$alternation" ] || fail "could not read the covered-method manifest out of rule '$RULE_ID'."

mapfile -t METHODS < <(printf '%s\n' "$alternation" | tr '|' '\n' | sed '/^$/d')
[ "${#METHODS[@]}" -gt 0 ] || fail "the covered-method manifest in rule '$RULE_ID' is empty."

# Guard the manifest itself: real method names, no duplicates, each one still
# present in the generated Querier interface.
for m in "${METHODS[@]}"; do
  [[ "$m" =~ ^[A-Za-z][A-Za-z0-9]*$ ]] || fail "manifest entry '$m' is not a Go method name."
  grep -q -E "^[[:space:]]+${m}\(ctx context\.Context" "$QUERIER" ||
    fail "manifest entry '$m' is not a method on the generated Querier in $QUERIER. A renamed or removed query leaves the rule enforcing nothing -- update the rule in the same change."
done
dupes="$(printf '%s\n' "${METHODS[@]}" | sort | uniq -d)"
[ -z "$dupes" ] || fail "duplicate entries in the covered-method manifest: $(echo "$dupes" | tr '\n' ' ')"

covered_sorted="$(printf '%s\n' "${METHODS[@]}" | sort)"
covered_pipe="|$(printf '%s|' "${METHODS[@]}")"

echo "=== covered methods (from ${CONFIG}, rule ${RULE_ID})"
printf '  %s\n' "${METHODS[@]}"

# ---------------------------------------------------------------------------
# Shared semgrep helper. Always invoked from the repository root with
# repo-relative targets, so the rule's `**/apps/server/**` path filters see
# exactly the paths `make semgrep` gives them.
# ---------------------------------------------------------------------------

SEMGREP_JSON=""
run_semgrep() {
  local out status=0
  out="$(semgrep --config "$CONFIG" --json --quiet --metrics=off \
    --disable-version-check "$@" 2>/dev/null)" || status=$?
  # semgrep exits 1 only with --error (not passed here); anything non-zero is
  # a real tool failure and must not be read as "no findings".
  [ "$status" -eq 0 ] || fail "semgrep exited $status while scanning: $*"
  [ -n "$out" ] || fail "semgrep produced no JSON output while scanning: $*"
  local errs
  errs="$(jq -r '[.errors[]? | .message] | join("; ")' <<<"$out")"
  [ -z "$errs" ] || fail "semgrep reported errors while scanning $*: $errs"
  SEMGREP_JSON="$out"
}

# Method names the rule flagged in a given file.
flagged_methods() {
  jq -r --arg rule "$RULE_ID" --arg path "$1" '
    .results[]
    | select(.check_id | endswith($rule))
    | select(.path == $path)
    | .extra.metavars["$METHOD"].abstract_content
  ' <<<"$SEMGREP_JSON" | sort -u
}

# Method names ANY rule flagged in a given file (used by the probe below,
# whose config holds exactly one rule).
any_flagged_methods() {
  jq -r --arg path "$1" '
    .results[]
    | select(.path == $path)
    | .extra.metavars["$METHOD"].abstract_content
  ' <<<"$SEMGREP_JSON" | sort -u
}

# ---------------------------------------------------------------------------
# 1. The real tree has no caller outside the domain store.
# ---------------------------------------------------------------------------

echo
echo "=== real-tree scan: no covered call outside apps/server/internal/resume"
# Same target selection as `make semgrep`: semgrep scans git-tracked files, so
# a brand-new UNTRACKED local file is not covered until it is staged. CI always
# scans committed content, which is where this gate is authoritative.
run_semgrep apps/server
real_hits="$(jq -r --arg rule "$RULE_ID" '
  .results[] | select(.check_id | endswith($rule))
  | "  \(.path):\(.start.line): \(.extra.metavars["$METHOD"].abstract_content)"
' <<<"$SEMGREP_JSON")"
if [ -n "$real_hits" ]; then
  echo "$real_hits" >&2
  fail "a caller outside internal/resume reaches a generated resume write. Route it through the resume domain store."
fi
echo "  clean"

# ---------------------------------------------------------------------------
# 2/3. Temporary fixtures: outside must be flagged, inside must not.
# ---------------------------------------------------------------------------

write_fixture() {
  local path="$1" pkg="$2"
  mkdir -p "$(dirname "$path")"
  {
    echo "// Code generated by scripts/test-resume-write-chokepoint.sh. DO NOT EDIT."
    echo "//"
    echo "// Temporary policy fixture for the resume write choke point. It is written"
    echo "// and deleted by that script within a single run and must never be committed."
    echo "// The leading underscore in the file name keeps the go tool from seeing it."
    echo "package ${pkg}"
    echo
    echo "func chokepointFixture(q chokepointQuerier, ctx chokepointCtx, arg chokepointArg) {"
    for m in "${METHODS[@]}"; do
      echo "	q.${m}(ctx, arg)"
    done
    echo "}"
  } >"$path"
}

echo
echo "=== fixture scan: ${OUTSIDE_FIXTURE} (must be flagged)"
echo "===              ${INSIDE_FIXTURE} (must be clean)"
write_fixture "$OUTSIDE_FIXTURE" api
write_fixture "$INSIDE_FIXTURE" resume

# Visibility control first. Both fixtures are scanned with a probe rule built
# from the same manifest but with NO path filters: both must light up
# completely. Without this, an inside fixture that semgrep never opened, could
# not parse, or silently skipped would look exactly like a correctly excluded
# one, and the "inside stays clean" assertion below would pass vacuously.
PROBE_CONFIG="$(mktemp -t chokepoint-probe-XXXXXX.yml)"
probe_config="$PROBE_CONFIG"
{
  echo "rules:"
  echo "  - id: ${RULE_ID}-probe"
  echo "    languages: [go]"
  echo "    severity: ERROR"
  echo "    message: fixture visibility probe (no path filters)"
  echo "    patterns:"
  echo "      - pattern: \$RECV.\$METHOD"
  echo "      - metavariable-regex:"
  echo "          metavariable: \$METHOD"
  echo "          regex: \"^(${alternation})\$\""
} >"$probe_config"
probe_status=0
probe_json="$(semgrep --config "$probe_config" --json --quiet --metrics=off \
  --disable-version-check "$OUTSIDE_FIXTURE" "$INSIDE_FIXTURE" 2>/dev/null)" || probe_status=$?
rm -f "$probe_config"
[ "$probe_status" -eq 0 ] || fail "the fixture visibility probe failed (semgrep exited $probe_status)."
SEMGREP_JSON="$probe_json"
for f in "$OUTSIDE_FIXTURE" "$INSIDE_FIXTURE"; do
  probe_flagged="$(any_flagged_methods "$f")"
  [ "$probe_flagged" = "$covered_sorted" ] ||
    fail "visibility probe: semgrep did not see all ${#METHODS[@]} covered calls in $f, so the checks below would be vacuous. Saw: $(echo "$probe_flagged" | tr '\n' ' ')"
done
echo "  visibility probe: both fixtures parsed, all ${#METHODS[@]} calls visible to semgrep"

run_semgrep "$OUTSIDE_FIXTURE" "$INSIDE_FIXTURE"

outside_flagged="$(flagged_methods "$OUTSIDE_FIXTURE")"
if [ "$outside_flagged" != "$covered_sorted" ]; then
  echo "  flagged:  $(echo "$outside_flagged" | tr '\n' ' ')" >&2
  echo "  expected: $(echo "$covered_sorted" | tr '\n' ' ')" >&2
  fail "the rule does not flag every covered method called from outside internal/resume."
fi
echo "  outside fixture: all ${#METHODS[@]} covered methods flagged"

inside_flagged="$(flagged_methods "$INSIDE_FIXTURE")"
if [ -n "$inside_flagged" ]; then
  echo "  flagged: $(echo "$inside_flagged" | tr '\n' ' ')" >&2
  fail "the rule flags the authorized caller inside internal/resume; its path exclusion is wrong."
fi
echo "  inside control:  clean"

rm -f "$OUTSIDE_FIXTURE" "$INSIDE_FIXTURE"

# ---------------------------------------------------------------------------
# 4. Every write query in queries.sql is covered by the manifest.
# ---------------------------------------------------------------------------
#
# Each `-- name: X :kind` block is collapsed to one line with its SQL comments
# stripped, then matched, case-insensitively, for a write against `resumes` or
# `idempotency_records` and for a row lock (`FOR UPDATE`) on `users`/`resumes`
# -- the lock that serializes a user's resume writes is part of the same choke
# point as the writes themselves. A block that matches and is not in the
# manifest fails the gate: extend the rule in the same reviewed change that
# adds the query.

echo
echo "=== ${QUERIES}: every resume/idempotency write is covered by the rule"
uncovered="$(awk -v covered="$covered_pipe" '
  function flush(   b, is_write) {
    if (name == "") return
    b = toupper(body) " "
    is_write = 0
    if (b ~ /(INSERT[ \t]+INTO|UPDATE|DELETE[ \t]+FROM)[ \t]+(ONLY[ \t]+)?(PUBLIC\.)?(RESUMES|IDEMPOTENCY_RECORDS)[^A-Z0-9_]/) is_write = 1
    if (b ~ /FOR[ \t]+UPDATE[^A-Z0-9_]/ && b ~ /(PUBLIC\.)?(USERS|RESUMES)[^A-Z0-9_]/) is_write = 1
    if (is_write && index(covered, "|" name "|") == 0) print name
    name = ""; body = ""
  }
  /^--[ \t]*name:/ { flush(); name = $3; next }
  { line = $0; sub(/--.*$/, "", line); body = body " " line }
  END { flush() }
' "$QUERIES")"
if [ -n "$uncovered" ]; then
  echo "  uncovered write queries:" >&2
  printf '    %s\n' $uncovered >&2
  fail "these queries write resumes/idempotency_records (or take the resume-write lock) but are not in rule '$RULE_ID'. Add them to its \$METHOD list."
fi
echo "  clean"

echo
echo "Resume write choke point enforced: ${#METHODS[@]} methods covered, fixtures removed."
