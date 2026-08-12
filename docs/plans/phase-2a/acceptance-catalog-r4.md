# Phase 2A automated acceptance catalog — revision 4

Status: **Frozen** (2026-08-12). Corrections require revision 5. This file does
not change during a run.

This revision replaces [`acceptance-catalog-r3.md`](acceptance-catalog-r3.md).
Revision 3 remains immutable. Revision 4 keeps its corrected migration and
stress gates, fixes the scanner-input and evidence-capture contracts, and
separates the tested candidate from its later documentation-only closure commit.

## Actors and immutable candidate

The integration owner creates one clean candidate commit containing this
catalog, the current implementation, and
[`suite-provenance-r3.md`](suite-provenance-r3.md). AC-DOC-001 may remain
`PENDING` at that commit because acceptance has not run yet. The owner records a
fresh phase-defect review of that exact commit, then runs `make ci` and
connected `make scan` once, in that order, with no concurrent heavy work.

A fresh read-only acceptance worker then runs the remaining commands. The worker
may write only to the external evidence directory. It must not edit the
repository, stop or recreate the shared database, contact GitHub, or run the
owner gates.

The candidate, catalog, tests, review, provenance record, and owner transcripts
must stay unchanged for the run. A failed or interrupted command is final for
that candidate: do not retry it. Record the failure and fail the affected rows.
A service or credential unavailable before its command starts is `BLOCKED` and
also fails. A changed `HEAD`, dirty worktree, missing evidence, skipped required
test, unrecorded state change, or evidence from another commit is `FAIL`.

## Safe evidence capture

The owner starts with one empty canonical absolute directory outside the
worktree. The acceptance worker continues in that same supplied directory; it
does not clear or replace the owner records. Set `P2A_R4_EVIDENCE` to that
directory. Both actors must retain `TEST_DATABASE_URL` and `SEMGREP_APP_TOKEN`
through the final exact-value audit. Put neither value in a command or file.
Disable shell tracing.

Both actors use this shell function. It records the literal command, combined
output, literal exit status, timestamps, and a separate SHA-256 manifest. It
never expands a command while recording it.

```bash
set +x
set -euo pipefail
: "${P2A_R4_EVIDENCE:?P2A_R4_EVIDENCE must be set}"
: "${TEST_DATABASE_URL:?TEST_DATABASE_URL must be set}"
: "${SEMGREP_APP_TOKEN:?SEMGREP_APP_TOKEN must be set}"
case "$P2A_R4_EVIDENCE" in
  /*) ;;
  *) printf 'P2A_R4_EVIDENCE must be absolute\n' >&2; exit 1 ;;
esac
test -d "$P2A_R4_EVIDENCE"
test "$(realpath -e "$P2A_R4_EVIDENCE")" = "$P2A_R4_EVIDENCE"
P2A_R4_ROOT=$(git rev-parse --show-toplevel)
case "$P2A_R4_EVIDENCE/" in
  "$P2A_R4_ROOT/"*) printf 'evidence directory must be outside the worktree\n' >&2; exit 1 ;;
esac
export P2A_R4_EVIDENCE TEST_DATABASE_URL SEMGREP_APP_TOKEN
P2A_R4_FAILURES=0
P2A_R4_CANDIDATE=$(git rev-parse HEAD)
export P2A_R4_CANDIDATE

verify_manifest() {
  local manifest="$P2A_R4_EVIDENCE/SHA256SUMS" expected=$# relative absolute count
  test -f "$manifest"
  test "$(wc -l <"$manifest")" -eq "$expected"
  for relative in "$@"; do
    absolute="$P2A_R4_EVIDENCE/$relative"
    test -f "$absolute"
    count=$(awk -v path="$absolute" '$2 == path { count++ } END { print count + 0 }' "$manifest")
    test "$count" -eq 1
  done
  sha256sum --check "$manifest"
}
export -f verify_manifest

capture() {
  local name=$1 command=$2 log status started ended
  log="$P2A_R4_EVIDENCE/$name.log"
  test ! -e "$log"
  started=$(date --iso-8601=seconds)
  {
    printf 'command: %s\n' "$command"
    printf 'started: %s\n' "$started"
  } >"$log"
  if /bin/bash -Eeuo pipefail -c "$command" >>"$log" 2>&1; then
    status=0
  else
    status=$?
    P2A_R4_FAILURES=$((P2A_R4_FAILURES + 1))
  fi
  ended=$(date --iso-8601=seconds)
  {
    printf 'ended: %s\n' "$ended"
    printf 'exit_status: %s\n' "$status"
  } >>"$log"
  sha256sum "$log" >>"$P2A_R4_EVIDENCE/SHA256SUMS"
  return "$status"
}
```

Before the review, the owner proves the directory is empty with
`test -z "$(find "$P2A_R4_EVIDENCE" -mindepth 1 -maxdepth 1 -print -quit)"`. The
owner reserves `00`–`02`; the acceptance worker uses `03`–`12`. A capture
failure stops the sequence after preserving that log. Never record `env`, `set`,
`.env`, a connection string, or a token. Evidence may name `TEST_DATABASE_URL`,
`REQUIRE_TEST_DB`, and `SEMGREP_APP_TOKEN` and state only whether each is set.

## Owner gate

The fresh defect reviewer writes `00-defect-review.md` outside the repository.
It must name the candidate, reviewed diff, design and ADR authorities, P2A-owned
traceability rows, assumptions, and every finding and re-review. It passes only
with no unresolved blocker. The owner adds its hash exactly once to
`SHA256SUMS`, then captures:

```bash
capture 01-ci 'before=$(git rev-parse HEAD); dirty_before=$(git status --porcelain=v1 --untracked-files=all); printf "owner_candidate_before: %s\n" "$before"; if test -n "$dirty_before"; then printf "owner_clean_before: no\n"; exit 1; fi; printf "owner_clean_before: yes\n"; test "$before" = "$P2A_R4_CANDIDATE"; make_status=0; make ci || make_status=$?; after=$(git rev-parse HEAD); dirty_after=$(git status --porcelain=v1 --untracked-files=all); printf "make_status: %s\n" "$make_status"; printf "owner_candidate_after: %s\n" "$after"; if test -n "$dirty_after"; then printf "owner_clean_after: no\n"; else printf "owner_clean_after: yes\n"; fi; test "$after" = "$P2A_R4_CANDIDATE"; test -z "$dirty_after"; test "$make_status" -eq 0'
capture 02-scan 'before=$(git rev-parse HEAD); dirty_before=$(git status --porcelain=v1 --untracked-files=all); printf "owner_candidate_before: %s\n" "$before"; if test -n "$dirty_before"; then printf "owner_clean_before: no\n"; exit 1; fi; printf "owner_clean_before: yes\n"; test "$before" = "$P2A_R4_CANDIDATE"; make_status=0; make scan || make_status=$?; after=$(git rev-parse HEAD); dirty_after=$(git status --porcelain=v1 --untracked-files=all); printf "make_status: %s\n" "$make_status"; printf "owner_candidate_after: %s\n" "$after"; if test -n "$dirty_after"; then printf "owner_clean_after: no\n"; else printf "owner_clean_after: yes\n"; fi; test "$after" = "$P2A_R4_CANDIDATE"; test -z "$dirty_after"; test "$make_status" -eq 0'
```

Each log must record the candidate and clean state immediately before and after
its one `make` command, plus that command's literal exit status. All recorded
commits must equal `P2A_R4_CANDIDATE`, both clean states must be `yes`, and both
make and capture exit statuses must be zero. `02-scan.log` must show that the
connected gate passed, not the offline fallback. At the candidate bytes,
`make scan` first verifies the effective dependency inputs, then runs exactly
one `semgrep ci --code --supply-chain --secrets --no-suppress-errors`, followed
by full-history `gitleaks detect --redact --no-banner`. Do not rerun either
owner command after a failure, even if the cause appears environmental; create a
new candidate and a new evidence directory instead.

## Acceptance-worker sequence

### 1. Identity and database

```bash
capture 03-identity 'verify_manifest 00-defect-review.md 01-ci.log 02-scan.log; date --iso-8601=seconds; printf "candidate: %s\n" "$(git rev-parse HEAD)"; git branch --show-current; git status --short --branch; test -z "$(git status --porcelain=v1 --untracked-files=all)"; sha256sum docs/plans/phase-2a/acceptance-catalog-r4.md docs/plans/phase-2a/suite-provenance-r3.md; for name in TEST_DATABASE_URL SEMGREP_APP_TOKEN; do test -n "$(printenv "$name")"; printf "%s=set\n" "$name"; done; if test -n "${REQUIRE_TEST_DB:-}"; then printf "REQUIRE_TEST_DB=set\n"; else printf "REQUIRE_TEST_DB=unset\n"; fi; cat .tool-versions; make tools-check ARGS=ci; make tools-check ARGS=scan'
capture 04-database 'make test-db-up; podman inspect aboutme-test-db --format "{{.Name}}|{{.State.Status}}|{{.Config.Image}}|{{.Image}}|{{.HostConfig.Memory}}|{{json .NetworkSettings.Ports}}"; image=$(podman inspect aboutme-test-db --format "{{.Config.Image}}"); podman image inspect "$image" --format "{{.Id}}|{{.Digest}}|{{json .RepoDigests}}"; podman exec aboutme-test-db pg_isready -h 127.0.0.1 -p 5432 -U aboutme -d aboutme; podman exec aboutme-test-db psql -X -U aboutme -d aboutme -Atc "SELECT version_id::text || '\''|'\'' || is_applied::text FROM goose_db_version ORDER BY id DESC LIMIT 1"'
```

Expected: `main` is clean and matches every supplied record. The one shared
container is running, ready on host port `20432`, limited to `536870912` bytes,
and remains at migration head `5|true`. The image identity and digest are
recorded. `TEST_DATABASE_URL` must be set for the focused live tests; its value
must not appear.

### 2. Corrected migration and focused concurrency

```bash
capture 05-cap 'cd apps/server && REQUIRE_TEST_DB=1 go test ./migrations -run "^TestResumeCapTrigger_(ExistsOnResumesTable|EnforcesThreePerUser|AllowsNoOpUpdateOfUserIDAtCap|EnforcesCapOnUpdateOfUserID)$" -count=1 -v'
capture 06-suite-a 'cd apps/server && REQUIRE_TEST_DB=1 go test ./internal/resume -race -run "^(TestCreate_Concurrent_ExactlyThreeSucceed|TestCreate_ConcurrentRawSQLBypass_StillCapped|TestSaveDocument_ConcurrentSameRevision_OneWinner|TestIdempotency_ConcurrentSameKey_OneMutationCommits|TestIdempotency_DifferentBodyNeverExecutes)$" -count=20 -v'
capture 07-suite-b 'cd apps/server && REQUIRE_TEST_DB=1 go test ./internal/resume -race -run "^TestSuiteB_(Get_NeverWrites|Backfill_LosesToConcurrentAutosave|Autosave_AfterBackfill_NoSpurious412|Backfill_ConcurrentWithItself_AppliesOnce|Backfill_TitleOnlyWriteCausesRetryableLostRace)$" -count=20 -v; go test ./internal/resume/docmigrate -run "^TestSuiteB_" -count=1 -v'
capture 08-cas-bounds 'cd apps/server && REQUIRE_TEST_DB=1 go test ./internal/resume -run "^(TestStore_Integration_SaveTitle_CAS|TestBoundsMatrix_LimitAndLimitPlusOne|TestBoundsCompletenessGuard|TestBoundsCorpus_MatchesCommitted|TestBoundsAdversarial.*)$" -count=1 -v'
```

Expected: the migration permits an owner-field no-op at the three-resume cap but
rejects a fourth insert and a real owner move with SQLSTATE `23514` and
`resumes_user_cap_exceeded`. Suite A's concurrent document save has one winner
and current truth for every loser. `TestStore_Integration_SaveTitle_CAS`
separately proves a sequential stale title save returns the current row; this
catalog does not claim a concurrent title race. All Suite A/B stress iterations
and Suite C bounds cases pass under their stated race and count settings.

### 3. Scanner inputs, provenance, and closure contract

```bash
capture 09-security-contract 'inputs="package.json package-lock.json apps/web/package.json apps/web/package-lock.json packages/schema/package.json packages/schema/package-lock.json apps/server/go.mod apps/server/go.sum packages/schema/gen/go/go.mod go.work go.work.sum"; selected=$(mktemp); trap '\''rm -f -- "$selected"'\'' EXIT; SEMGREP_SEND_METRICS=off semgrep scan --config .semgrep.yml --x-ls . >"$selected"; sed -i "s#^\\./##" "$selected"; for path in $inputs; do git ls-files --error-unmatch -- "$path" >/dev/null; grep -Fqx -- "$path" "$selected"; printf "selected dependency input: %s\n" "$path"; done; scripts/test/semgrep-sca-inputs-test.sh; scripts/test/scan-products-contract-test.sh; rg -n -- "--code|--supply-chain|--secrets|--no-suppress-errors|gitleaks detect --redact --no-banner" Makefile scripts/scan.sh'
capture 10-closure 'test ! -e apps/server/migrations/.uat-baseline; bash scripts/test/migration-append-only-test.sh; bash scripts/test/workflow-safety-test.sh; rg -n "\\.(LockUserForResumeWrite|CreateResume|DeleteResumeForUser|UpdateResumeDocumentCAS|UpdateResumeTitleCAS|BackfillResumeDocumentCAS)\\(" apps/server --glob "*.go" --glob "!**/*_test.go" --glob "!internal/store/queries.sql.go"; rg -n "AC-DOC-00(1|2|3|4|7|8|9)|AC-DOC-01(0|1|2)|AC-SAVE-003" docs/plans/traceability docs/plans/phase-2a; test -z "$(git ls-files -- .env)"; verify_current() { path=$1 expected=$2 actual=$(sha256sum "$path" | cut -d " " -f 1); printf "current_sha256: %s|%s\n" "$path" "$actual"; grep -Fq -- "$expected" docs/plans/phase-2a/suite-provenance-r3.md; test "$actual" = "$expected"; }; verify_current apps/server/internal/resume/writesafety_adversarial_test.go 60ddd06a5c827a39fbbaaef669d5ea502a39dbbe5ff87374afb66c1e83be637a; verify_current apps/server/internal/resume/docmigrate_adversarial_suiteb_test.go de1459a85f8f7269e0fe9522cb0df196a59542ab0b66843fd079dadd8c1acba3; verify_current apps/server/internal/resume/docmigrate/suiteb_wire_adversarial_test.go ccb25182f4276f5c81a20737e9fa7b18220a9bf9628171f976ef2710b210ab6c; verify_current apps/server/internal/resume/bounds_adversarial_test.go 800ea28ba769e4d32b8297469cc83f3d658e8aee8f7798941d8e423132005125; git log --format="%H%x09%ad%x09%s" --date=iso-strict -- apps/server/internal/resume/writesafety_adversarial_test.go apps/server/internal/resume/docmigrate_adversarial_suiteb_test.go apps/server/internal/resume/docmigrate/suiteb_wire_adversarial_test.go apps/server/internal/resume/bounds_adversarial_test.go; git log --all --follow --format="%H%x09%ad%x09%s" --date=iso-strict -- apps/server/internal/resume/writesafety_adversarial_test.go; git log --all --follow --format="%H%x09%ad%x09%s" --date=iso-strict -- apps/server/internal/resume/docmigrate_adversarial_test.go; git log --all --follow --format="%H%x09%ad%x09%s" --date=iso-strict -- apps/server/internal/resume/bounds_adversarial_test.go; verify_delivered() { spec=$1 expected=$2 actual=$(git show "$spec" | sha256sum | cut -d " " -f 1); printf "delivered_sha256: %s|%s\n" "$spec" "$actual"; grep -Fq -- "$expected" docs/plans/phase-2a/suite-provenance-r3.md; test "$actual" = "$expected"; }; verify_delivered a3deaa8c36700127a67a9950cdb9b77a8926bab8:apps/server/internal/resume/writesafety_adversarial_test.go 8bbc5726f63a0e4b6dc410fa01619a73459f9b3577dec143159f75fedacec035; verify_delivered b94f3e6493943a616c6263db77363770f134f342:apps/server/internal/resume/docmigrate_adversarial_test.go e28971b2dd70dd03e8d545071efccab187f89e26565932c921ce2d14afc137ee; verify_delivered b6f1ee0d1f15da7133ff887ba8ecec8559b78fd4:apps/server/internal/resume/bounds_adversarial_test.go c3d473e903ff9a9666018d051d4b8b6d4c49dbf52ca371dff4f40f8e9cb30829; for commit in a3deaa8c36700127a67a9950cdb9b77a8926bab8 b94f3e6493943a616c6263db77363770f134f342 b6f1ee0d1f15da7133ff887ba8ecec8559b78fd4; do git merge-base --is-ancestor "$commit" ba03f64402123c57bdf389aeb685788f5cc67d36; done'
```

Expected: the log names every root, web, schema, server-Go, generated-Go, and
Go-workspace dependency manifest or lock input and proves Semgrep selects each
one. The scan contract selects connected Code, Supply Chain, and Secrets and
fails on any product failure. The owner scan and these candidate script bytes
together prove the connected products and their effective dependency inputs.

The pre-UAT marker is absent, while the regression proves its future
immutability. Generated resume writes remain inside `internal/resume`.
Traceability may still say AC-DOC-001 `PENDING`; all other owned rows and
handoffs must contain exact evidence. The suite hashes and Git history must
match `suite-provenance-r3.md`. That record must distinguish verifiable file and
commit facts from historical task claims. Acceptance does not require anonymous
worker identities, private dispatch timestamps, or undocumented authorship.

### 4. Explicit secret audit and final identity

Run this only after the owner transcripts and all acceptance logs exist. It
checks source history, the evidence directory, and exact configured values
without printing those values.

```bash
capture 11-secret-final 'verify_manifest 00-defect-review.md 01-ci.log 02-scan.log 03-identity.log 04-database.log 05-cap.log 06-suite-a.log 07-suite-b.log 08-cas-bounds.log 09-security-contract.log 10-closure.log; gitleaks detect --redact --no-banner; gitleaks dir --redact --no-banner "$P2A_R4_EVIDENCE"; for name in TEST_DATABASE_URL SEMGREP_APP_TOKEN; do value=$(printenv "$name"); test -n "$value"; if grep -RFl --exclude=11-secret-final.log -- "$value" "$P2A_R4_EVIDENCE" >/dev/null; then printf "%s value appears in evidence\n" "$name" >&2; exit 1; fi; done; recorded_candidate=$(sed -n "s/^candidate: //p" "$P2A_R4_EVIDENCE/03-identity.log"); test -n "$recorded_candidate"; test "$(git rev-parse HEAD)" = "$recorded_candidate"; test "$recorded_candidate" = "$P2A_R4_CANDIDATE"; for log in 01-ci 02-scan; do for edge in before after; do owner_candidate=$(sed -n "s/^owner_candidate_${edge}: //p" "$P2A_R4_EVIDENCE/$log.log"); test -n "$owner_candidate"; test "$owner_candidate" = "$recorded_candidate"; grep -Fqx -- "owner_clean_${edge}: yes" "$P2A_R4_EVIDENCE/$log.log"; done; grep -Fqx -- "make_status: 0" "$P2A_R4_EVIDENCE/$log.log"; grep -Fqx -- "exit_status: 0" "$P2A_R4_EVIDENCE/$log.log"; done; test -z "$(git status --porcelain=v1 --untracked-files=all)"; sha256sum docs/plans/phase-2a/acceptance-catalog-r4.md docs/plans/phase-2a/suite-provenance-r3.md; podman inspect aboutme-test-db --format "{{.Name}}|{{.State.Status}}|{{.Image}}|{{.HostConfig.Memory}}|{{json .NetworkSettings.Ports}}"; podman exec aboutme-test-db psql -X -U aboutme -d aboutme -Atc "SELECT version_id::text || '\''|'\'' || is_applied::text FROM goose_db_version ORDER BY id DESC LIMIT 1"'
```

Expected: the preliminary gitleaks audits pass; neither configured value occurs
in records `00`–`10`; the candidate, catalog, provenance hash, clean state,
container, and migration head match the start. After capture 11, the worker
writes `acceptance-report-r4.md` in the evidence directory. Without changing it,
the worker adds its hash to the manifest and captures the final audit:

```bash
P2A_R4_REPORT="$P2A_R4_EVIDENCE/acceptance-report-r4.md"
test -f "$P2A_R4_REPORT"
sha256sum "$P2A_R4_REPORT" >>"$P2A_R4_EVIDENCE/SHA256SUMS"
capture 12-handoff 'verify_manifest 00-defect-review.md 01-ci.log 02-scan.log 03-identity.log 04-database.log 05-cap.log 06-suite-a.log 07-suite-b.log 08-cas-bounds.log 09-security-contract.log 10-closure.log 11-secret-final.log acceptance-report-r4.md; for name in TEST_DATABASE_URL SEMGREP_APP_TOKEN; do value=$(printenv "$name"); test -n "$value"; if grep -RFl -- "$value" "$P2A_R4_EVIDENCE" >/dev/null; then printf "%s value appears in final evidence\n" "$name" >&2; exit 1; fi; done; gitleaks dir --redact --no-banner "$P2A_R4_EVIDENCE"'
verify_manifest 00-defect-review.md 01-ci.log 02-scan.log 03-identity.log \
  04-database.log 05-cap.log 06-suite-a.log 07-suite-b.log \
  08-cas-bounds.log 09-security-contract.log 10-closure.log \
  11-secret-final.log acceptance-report-r4.md 12-handoff.log
test "$P2A_R4_FAILURES" -eq 0
```

The recipient repeats the final `verify_manifest` command before accepting the
handoff. The returned report, handoff log, and manifest are immutable records.
Any missing, duplicate, changed, or unlisted record fails.

## Required report rows

The external report records candidate and catalog hashes, branch and clean
state, start/end times, tool and database identity, every capture through 11's
literal status, state changes, manual mutation count, retry count `0`, and one
filled row below. Evidence cells name files and SHA-256 digests. The report
cannot contain its later handoff-log digest; `12-handoff.log` and the final
manifest record that post-report audit.

| ID        | Expected result                                                                                                                       | Observed | Evidence | Verdict                   |
| --------- | ------------------------------------------------------------------------------------------------------------------------------------- | -------- | -------- | ------------------------- |
| P2A-R4-01 | Full live suites pass under `make ci` with required DB, `-race -count=1`, and no skip                                                 | _fill_   | _fill_   | `PASS \| FAIL \| BLOCKED` |
| P2A-R4-02 | Migration/sqlc gates reach corrected `00005`; pre-UAT and future-marker policies pass                                                 | _fill_   | _fill_   | `PASS \| FAIL \| BLOCKED` |
| P2A-R4-03 | Exactly three creates succeed; owner no-op succeeds; fourth insert and real owner move fail                                           | _fill_   | _fill_   | `PASS \| FAIL \| BLOCKED` |
| P2A-R4-04 | Title bounds and wrong-owner or unknown probes reject without a write                                                                 | _fill_   | _fill_   | `PASS \| FAIL \| BLOCKED` |
| P2A-R4-05 | Go and TypeScript enforce document, aggregate, cleared-value, and no-write bounds                                                     | _fill_   | _fill_   | `PASS \| FAIL \| BLOCKED` |
| P2A-R4-06 | Concurrent document CAS has one winner and current-truth losers; sequential stale title CAS returns current truth                     | _fill_   | _fill_   | `PASS \| FAIL \| BLOCKED` |
| P2A-R4-07 | Idempotency replay, reuse, concurrency, rollback, cap composition, and active-user expiry cleanup are transactional                   | _fill_   | _fill_   | `PASS \| FAIL \| BLOCKED` |
| P2A-R4-08 | Projection reads are pure; backfill loses safely to document, title, and concurrent backfill writes                                   | _fill_   | _fill_   | `PASS \| FAIL \| BLOCKED` |
| P2A-R4-09 | Released v1 schemas/types, registries, converters, wire persistence, and unknown or lossy rejection pass                              | _fill_   | _fill_   | `PASS \| FAIL \| BLOCKED` |
| P2A-R4-10 | Suite hashes/history match honest provenance; Suite A/B stress and Suite C count gates pass                                           | _fill_   | _fill_   | `PASS \| FAIL \| BLOCKED` |
| P2A-R4-11 | Fresh defect review, one `make ci`, and one connected Code/Supply Chain/Secrets scan pass at the exact candidate without retry        | _fill_   | _fill_   | `PASS \| FAIL \| BLOCKED` |
| P2A-R4-12 | Write-path review, evidence-rich traceability, and named P2B/P8 handoffs are complete; AC-DOC-001 awaits only this run                | _fill_   | _fill_   | `PASS \| FAIL \| BLOCKED` |
| P2A-R4-13 | Exact dependency inputs are scanned; source history and external evidence contain no secret; migration/schema immutability gates pass | _fill_   | _fill_   | `PASS \| FAIL \| BLOCKED` |

Ownership is unchanged from revision 3. R4-01/11/12 cover all P2A rows; R4-02,
R4-03, and R4-04 cover AC-DOC-001; R4-05 covers
AC-DOC-002/003/004/007/008/009/011; R4-06 supports P2B-owned AC-SAVE-001; R4-07
covers AC-SAVE-003; R4-08 covers AC-DOC-010; R4-09 covers AC-DOC-012; R4-10
covers the independent Suite A/B and retained Suite C evidence; R4-13 covers
migration, released-schema, supply-chain, and privacy gates.

## Documentation-only closure commit

The acceptance worker returns the formatted report and evidence hashes without
changing the repository. If every row passes, the integration owner makes a
second, documentation-only commit that:

1. adds the report verbatim as `acceptance-report-r4.md`;
2. changes AC-DOC-001 from `PENDING` to `PROVEN` and cites R4-02/03/11/12;
3. marks the P2A phase review, acceptance, exit criteria, handoff, architecture,
   and master-plan status complete; and
4. while both configured values remain set, proves neither exact value appears
   in the report or other closure paths, stages only those explicit paths, and
   runs formatting checks plus `gitleaks git --staged --redact --no-banner`.

The closure commit records evidence about the named candidate. It must not claim
that its later commit hash ran the product, CI, scan, or acceptance gates. Any
product, test, migration, scanner, workflow, catalog, provenance, or review
change requires a new candidate and a new acceptance run.
