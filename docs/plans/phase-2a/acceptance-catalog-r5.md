# Phase 2A automated acceptance catalog — revision 5

Status: **Frozen** (2026-08-12). Corrections require revision 6. This file does
not change during a run.

This revision replaces [`acceptance-catalog-r4.md`](acceptance-catalog-r4.md).
Revision 4 remains immutable. Revision 5 preserves every R4 criterion, command,
expected result, actor boundary, zero-retry rule, and two-commit closure rule.
It changes only the shell contract after the first R4 owner attempt proved that
Zsh reserves the variable name `status` and therefore could not append the
otherwise-passing `make ci` result to its log. That candidate and evidence
directory are failed and must not be reused.

## Revision-wide substitutions

Apply these substitutions to the complete R4 procedure and report:

- Each actor runs its setup and assigned `capture` calls in one Bash 5 or later
  process invoked with `bash --noprofile --norc`. The acceptance worker also
  runs the handoff in its process. Do not source the procedure into Zsh or
  another shell.
- Replace `P2A_R4_` with `P2A_R5_` in environment and shell variable names.
- Use a new empty external evidence directory and a new exact candidate.
- Hash both `acceptance-catalog-r4.md` and this file anywhere R4 hashes its
  catalog. The R5 hash is the controlling catalog identity.
- Replace `P2A-R4-01`…`P2A-R4-13` with `P2A-R5-01`…`P2A-R5-13` in the report and
  closure references. The expected result and ownership of each corresponding
  row are unchanged.
- Replace every shorthand `R4-<row>` reference in R4's ownership paragraph and
  documentation-only closure steps with the corresponding `R5-<row>`.
- Write `acceptance-report-r5.md`; use that name in the manifest, handoff, and
  documentation-only closure commit.
- Prefix owner capture `01-ci` and worker capture `03-identity` with
  `bash --version;` so both actor versions are recorded in their existing logs.

The owner records a fresh defect review of the new exact candidate. The owner
then runs one `make ci` and one connected `make scan`. The prior R4 `make ci`
does not count and is not reused. A failed or interrupted R5 command remains
final for its candidate.

## Replacement Bash setup and capture function

Replace R4's complete **Safe evidence capture** fenced block with this block.
All later R4 commands run unchanged after the revision-wide substitutions.

```bash
set +x
set -euo pipefail
test "${BASH_VERSINFO[0]}" -ge 5
: "${P2A_R5_EVIDENCE:?P2A_R5_EVIDENCE must be set}"
: "${TEST_DATABASE_URL:?TEST_DATABASE_URL must be set}"
: "${SEMGREP_APP_TOKEN:?SEMGREP_APP_TOKEN must be set}"
case "$P2A_R5_EVIDENCE" in
  /*) ;;
  *) printf 'P2A_R5_EVIDENCE must be absolute\n' >&2; exit 1 ;;
esac
test -d "$P2A_R5_EVIDENCE"
test "$(realpath -e "$P2A_R5_EVIDENCE")" = "$P2A_R5_EVIDENCE"
P2A_R5_ROOT=$(git rev-parse --show-toplevel)
case "$P2A_R5_EVIDENCE/" in
  "$P2A_R5_ROOT/"*) printf 'evidence directory must be outside the worktree\n' >&2; exit 1 ;;
esac
export P2A_R5_EVIDENCE TEST_DATABASE_URL SEMGREP_APP_TOKEN
P2A_R5_FAILURES=0
P2A_R5_CANDIDATE=$(git rev-parse HEAD)
export P2A_R5_CANDIDATE

verify_manifest() {
  local manifest="$P2A_R5_EVIDENCE/SHA256SUMS" expected=$# relative absolute count
  test -f "$manifest"
  test "$(wc -l <"$manifest")" -eq "$expected"
  for relative in "$@"; do
    absolute="$P2A_R5_EVIDENCE/$relative"
    test -f "$absolute"
    count=$(awk -v path="$absolute" '$2 == path { count++ } END { print count + 0 }' "$manifest")
    test "$count" -eq 1
  done
  sha256sum --check "$manifest"
}
export -f verify_manifest

capture() {
  local name=$1 command=$2 log command_status started ended
  log="$P2A_R5_EVIDENCE/$name.log"
  test ! -e "$log"
  started=$(date --iso-8601=seconds)
  {
    printf 'command: %s\n' "$command"
    printf 'started: %s\n' "$started"
  } >"$log"
  if /bin/bash -Eeuo pipefail -c "$command" >>"$log" 2>&1; then
    command_status=0
  else
    command_status=$?
    P2A_R5_FAILURES=$((P2A_R5_FAILURES + 1))
  fi
  ended=$(date --iso-8601=seconds)
  {
    printf 'ended: %s\n' "$ended"
    printf 'exit_status: %s\n' "$command_status"
  } >>"$log"
  sha256sum "$log" >>"$P2A_R5_EVIDENCE/SHA256SUMS"
  return "$command_status"
}
```

Before any evidence write, require `BASH_VERSINFO[0]` to be at least `5`. The
owner and acceptance worker each record `bash --version` without recording
environment values. Bash is an execution boundary for this catalog, not a new
repository-wide tool pin.

## Closure

The R5 report and final 14-entry manifest replace their R4 names. The recipient
repeats R4's deterministic final manifest verification with the substituted R5
directory and report name. If every R5 row passes, the integration owner makes
the documentation-only closure commit defined by R4, citing the R5 tested
candidate. No product or gate claim attaches to the later closure commit.
