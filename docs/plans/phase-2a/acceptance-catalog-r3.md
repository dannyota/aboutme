# Phase 2A automated acceptance catalog — revision 3

Status: **Frozen** (2026-08-12). Corrections require revision 4. This file does
not change during a run.

This revision replaces [`acceptance-catalog-r2.md`](acceptance-catalog-r2.md)
for future runs. Revision 2 remains immutable. Revision 3 reaches the corrected
pre-UAT migration `00005`, proves the no-op owner-assignment case beside insert
and real-move cap enforcement, and verifies the marker-based migration
immutability policy.

## Run authority and failure rules

The integration owner pins one clean candidate commit, supplies the completed
phase-defect review, and runs `make ci` and connected `make scan` once at that
commit with no other heavy work. A fresh read-only acceptance worker runs the
remaining commands and adjudicates this catalog. The acceptance worker must not
run `make ci`, stop the shared database, or edit the repository.

The worker may run checks, query the shared test database, and write transcripts
only outside the worktree. It must not fetch, pull, push, contact GitHub, or
edit code, tests, fixtures, generated output, seeds, criteria, or this catalog.

Use `make test-db-up` idempotently and leave `aboutme-test-db` running. Every
live Go test uses `-count=1`, except the frozen Suite A and B stress commands,
which use `-count=20`. Required live suites must fail rather than skip when the
database is absent.

The run fails closed:

- a changed `HEAD`, non-clean worktree, missing transcript, missing hash,
  unexplained output, skipped required test, or evidence from another commit
  makes the affected row `FAIL`;
- a missing service, connected scan, review, provenance record, or owner gate
  makes the affected row `BLOCKED`, which fails the catalog;
- every rerun is a retry and must retain the first result, reason, timestamps,
  and second transcript. A retry cannot erase a product or test failure;
- manual database mutations and container restarts are forbidden. If either
  occurs, record it and fail every database-backed row;
- criteria and expected results remain fixed for the whole run.

## Evidence and report contract

Create one evidence directory outside the repository. Record its absolute path,
but never configuration values. Capture each command's exact text, combined
output, exit status, start and end time, and SHA-256 digest in a separate file.
The report names only these configuration variables: `TEST_DATABASE_URL`,
`REQUIRE_TEST_DB`, and `SEMGREP_APP_TOKEN`.

Use `00-metadata.log`, `01-database.log`, `02-migration-cap.log`,
`03-live-suites.log`, `04-suite-a.log`, `05-suite-b.log`,
`06-schema-bounds.log`, `07-closure.log`, `08-provenance.log`, `09-ci.log`,
`10-scan.log`, `11-defect-review.md`, and `12-final-state.log` under that
directory. A row may cite more than one file, but every cited file needs its
SHA-256.

The report records:

- candidate commit, branch, clean status, catalog path and SHA-256;
- `.tool-versions`, verified tool versions, Podman version, PostgreSQL image
  name, image ID and digest, memory limit, host port, and initial/final
  container state;
- migration head before and after the database checks;
- phase-defect review path and hash, Suite A/B/C provenance path and hash, and
  owner `make ci`/`make scan` transcript paths and hashes;
- run start/end times, every state change, retry count, seed or manual mutation
  count, and one completed row from the template below per criterion.

The worker returns the completed report to the integration owner without
changing the worktree. Before P2A closes, the owner persists it verbatim as
`docs/plans/phase-2a/acceptance-report-r3.md`. A chat-only result does not
satisfy the persisted-report requirement. The report commit is evidence about
the recorded candidate; it must not claim that its own later SHA was tested.

## Required command sequence

Capture these blocks in order. Set `P2A_R3_EVIDENCE` to the external evidence
directory and `P2A_TEST_DSN` to the documented shared test DSN. Do not print the
DSN.

### 1. Candidate and tool metadata

```sh
date --iso-8601=seconds
git rev-parse HEAD
git branch --show-current
git status --short --branch
test -z "$(git status --porcelain=v1 --untracked-files=all)"
sha256sum docs/plans/phase-2a/acceptance-catalog-r3.md
cat .tool-versions
make tools-check ARGS=ci
make tools-check ARGS=scan
podman version
printf '%s\n' TEST_DATABASE_URL REQUIRE_TEST_DB SEMGREP_APP_TOKEN
```

Expected: branch `main`; one unchanged commit; empty porcelain status; pinned
tools pass. Record configuration names only.

### 2. Shared database identity and initial state

```sh
make test-db-up
podman inspect aboutme-test-db --format '{{.Name}}|{{.State.Status}}|{{.Config.Image}}|{{.Image}}|{{.HostConfig.Memory}}|{{json .NetworkSettings.Ports}}'
P2A_DB_IMAGE=$(podman inspect aboutme-test-db --format '{{.Config.Image}}')
podman image inspect "$P2A_DB_IMAGE" --format '{{.Id}}|{{.Digest}}|{{json .RepoDigests}}'
podman exec aboutme-test-db pg_isready -h 127.0.0.1 -p 5432 -U aboutme -d aboutme
podman exec aboutme-test-db psql -X -U aboutme -d aboutme -Atc \
  "SELECT to_regclass('public.goose_db_version');"
```

Expected: the one shared container is running on host port `20432`, has a
`536870912` byte memory limit, is ready, and uses the recorded pinned PostgreSQL
image. A missing migration table before the checks is allowed and must be
recorded, not repaired manually.

### 3. Migration, sqlc, and corrected cap behavior

```sh
make server-migration-test
make sqlc-check
cd apps/server && REQUIRE_TEST_DB=1 TEST_DATABASE_URL="$P2A_TEST_DSN" \
  go test ./migrations -run '^TestResumeCapTrigger_(ExistsOnResumesTable|EnforcesThreePerUser|AllowsNoOpUpdateOfUserIDAtCap|EnforcesCapOnUpdateOfUserID)$' -count=1 -v
cd ../..
podman exec aboutme-test-db psql -X -U aboutme -d aboutme -Atc \
  "SELECT version_id::text || '|' || is_applied::text FROM goose_db_version ORDER BY id DESC LIMIT 1;"
podman exec aboutme-test-db psql -X -U aboutme -d aboutme -Atc \
  "SELECT pg_get_functiondef('enforce_resume_cap()'::regprocedure);"
```

Expected: empty-to-head, previous-to-head, concurrent-runner, partial-failure
recovery, migrate CLI, and sqlc drift checks pass. The head is `5|true`. The
function includes the equality return before the target-owner lock. At a count
of three, assigning a row to its existing owner succeeds and leaves owner/count
unchanged; a fourth insert and a real move to that owner both fail with SQLSTATE
`23514` and `resumes_user_cap_exceeded`.

### 4. Required live suites and absent-database failure

```sh
make server-test-db
bash -c 'if (echo > /dev/tcp/127.0.0.1/21000) 2>/dev/null; then exit 1; fi'
if TEST_DATABASE_URL='postgres://invalid:invalid@127.0.0.1:21000/invalid?sslmode=disable&connect_timeout=1' \
  make server-test-db; then
  echo 'server-test-db passed without its required database' >&2
  exit 1
else
  echo 'server-test-db failed closed with its required database unavailable'
fi
```

Expected: the positive target runs auth, store, user, and resume packages with
`-race -count=1`; no required test is skipped. The isolated unbound-port probe
fails closed. This negative probe does not stop or change the shared container.

### 5. Independent Suite A and B stress gates

```sh
(cd apps/server && REQUIRE_TEST_DB=1 TEST_DATABASE_URL="$P2A_TEST_DSN" \
  go test ./internal/resume -race \
    -run '^(TestCreate_Concurrent_ExactlyThreeSucceed|TestCreate_ConcurrentRawSQLBypass_StillCapped|TestSaveDocument_ConcurrentSameRevision_OneWinner|TestIdempotency_ConcurrentSameKey_OneMutationCommits)$' \
    -count=20 -v)
(cd apps/server && REQUIRE_TEST_DB=1 TEST_DATABASE_URL="$P2A_TEST_DSN" \
  go test ./internal/resume -race \
    -run '^TestSuiteB_(Get_NeverWrites|Backfill_LosesToConcurrentAutosave|Autosave_AfterBackfill_NoSpurious412|Backfill_ConcurrentWithItself_AppliesOnce|Backfill_TitleOnlyWriteCausesRetryableLostRace)$' \
    -count=20 -v)
(cd apps/server && go test ./internal/resume/docmigrate \
  -run '^TestSuiteB_' -count=1 -v)
```

Expected: all iterations pass. Suite A observes exactly three successful
creates, trigger blocking, one CAS winner, and one committed idempotent
mutation. Suite B keeps reads pure and makes every backfill race resolve without
lost user data or a spurious revision failure. Its wire suite validates both
conversion directions and every missing-path failure.

### 6. Schema, bounds, and version gates

```sh
make schema-check
cd apps/server && go test ./internal/resume \
  -run '^(TestBoundsMatrix_LimitAndLimitPlusOne|TestBoundsCompletenessGuard|TestBoundsCorpus_MatchesCommitted|TestBoundsAdversarial)' \
  -count=1 -v
cd ../..
```

Expected: JavaScript/TypeScript and Go schema verdicts agree; generated output
is clean; every schema and aggregate bound has limit/limit+1 coverage; cleared
values remain distinct; released v1 schema/types and version declarations are
immutable and fail closed for unknown or lossy conversion paths. Suite C's
independent inventory matches the author matrix exactly.

### 7. Pre-UAT marker, future guard, and closure inspection

```sh
test ! -e apps/server/migrations/.uat-baseline
if git cat-file -e HEAD:apps/server/migrations/.uat-baseline 2>/dev/null; then exit 1; fi
bash scripts/test/migration-append-only-test.sh
bash scripts/test/workflow-safety-test.sh
rg -n 'uat-baseline|pre-UAT|UAT-baselined' \
  scripts/ci.sh scripts/test/migration-append-only-test.sh .github/workflows/ci.yml \
  docs/design/data.md docs/standards/engineering.md
rg -n '\.(LockUserForResumeWrite|CreateResume|DeleteResumeForUser|UpdateResumeDocumentCAS|UpdateResumeTitleCAS|BackfillResumeDocumentCAS)\(' \
  apps/server --glob '*.go' --glob '!**/*_test.go' --glob '!internal/store/queries.sql.go'
rg -n 'AC-DOC-00(1|2|3|4|7|8|9)|AC-DOC-01(0|1|2)|AC-SAVE-003' \
  docs/plans/traceability docs/plans/phase-2a
git ls-files --error-unmatch .env && exit 1 || true
```

Expected: the candidate and `HEAD` have no baseline marker because this is
before the first UAT candidate. The local regression accepts pre-UAT correction,
then proves that a present marker cannot be deleted or changed, existing SQL
cannot be modified in staged, unstaged, or hidden-index state, and a new forward
migration remains allowed. The hosted and local guards express the same marker
policy and fail closed on diff errors. Every production generated resume-write
call is under `internal/resume`; traceability and handoffs have exact evidence
and named downstream gates; `.env` is untracked.

### 8. Owner gates and independent records

Do not rerun these. Verify supplied transcripts and records:

```sh
make ci
make scan
```

Expected: the integration owner ran both commands once, in this order, at the
same clean candidate with no concurrent heavy work. `make ci` reports every
group passed, including operational marker-guard tests, schema/sqlc drift,
released-schema immutability, live database suites, migration harness, web, and
route table. `make scan` proves connected Semgrep SAST, Supply Chain SCA,
secrets analysis, and full-history gitleaks. Offline Semgrep is not evidence.

The phase-defect review must name the same candidate and report no blocker after
independent re-review of fixes. The Suite A/B/C provenance record must identify
three different fresh authors, their frozen inputs and withheld paths, the
pre-implementation derivation time, their delivered suite hashes, and every
later editor/review. A missing field, shared author, implementation-first
derivation, or unexplained suite edit makes P2A-R3-10 `BLOCKED`.

Record the current suite identities and compare them with that provenance:

```sh
sha256sum \
  apps/server/internal/resume/writesafety_adversarial_test.go \
  apps/server/internal/resume/docmigrate_adversarial_suiteb_test.go \
  apps/server/internal/resume/docmigrate/suiteb_wire_adversarial_test.go \
  apps/server/internal/resume/bounds_adversarial_test.go
git log --format='%H%x09%ad%x09%s' --date=iso-strict -- \
  apps/server/internal/resume/writesafety_adversarial_test.go \
  apps/server/internal/resume/docmigrate_adversarial_suiteb_test.go \
  apps/server/internal/resume/docmigrate/suiteb_wire_adversarial_test.go \
  apps/server/internal/resume/bounds_adversarial_test.go
```

### 9. Final identity and state

```sh
date --iso-8601=seconds
git rev-parse HEAD
git status --short --branch
test -z "$(git status --porcelain=v1 --untracked-files=all)"
sha256sum docs/plans/phase-2a/acceptance-catalog-r3.md
podman inspect aboutme-test-db --format '{{.Name}}|{{.State.Status}}|{{.Image}}|{{.HostConfig.Memory}}|{{json .NetworkSettings.Ports}}'
podman exec aboutme-test-db psql -X -U aboutme -d aboutme -Atc \
  "SELECT version_id::text || '|' || is_applied::text FROM goose_db_version ORDER BY id DESC LIMIT 1;"
```

Expected: commit and catalog hash match the start, the worktree remains clean,
the shared database remains running and unchanged except for test transactions
and migration-to-head, and the head remains `5|true`.

## Required report rows

Copy this table to the report and fill every placeholder. Evidence cells contain
the external transcript or record path and SHA-256, not only a command name.

| ID        | Expected result                                                                                                                        | Observed result | Evidence path and SHA-256 | Verdict                   |
| --------- | -------------------------------------------------------------------------------------------------------------------------------------- | --------------- | ------------------------- | ------------------------- |
| P2A-R3-01 | Required live suites run with `-race -count=1`, no skips, and fail closed against the isolated unavailable database                    | _fill_          | _fill_                    | `PASS \| FAIL \| BLOCKED` |
| P2A-R3-02 | Migration and sqlc checks reach corrected `00005`; pre-UAT marker absence and the future marker guard match policy                     | _fill_          | _fill_                    | `PASS \| FAIL \| BLOCKED` |
| P2A-R3-03 | Direct SQL and concurrency allow exactly three resumes; no-op owner assignment succeeds while a fourth insert and real owner move fail | _fill_          | _fill_                    | `PASS \| FAIL \| BLOCKED` |
| P2A-R3-04 | Title limits and wrong-owner/unknown probes reject without changing row count or row state                                             | _fill_          | _fill_                    | `PASS \| FAIL \| BLOCKED` |
| P2A-R3-05 | Go and TypeScript enforce every document and aggregate bound, including cleared-value persistence and no-write rejection               | _fill_          | _fill_                    | `PASS \| FAIL \| BLOCKED` |
| P2A-R3-06 | Concurrent document and title CAS have one winner; every loser receives current truth without an existence oracle                      | _fill_          | _fill_                    | `PASS \| FAIL \| BLOCKED` |
| P2A-R3-07 | Idempotency replay, reuse rejection, concurrency, rollback, cap composition, and active-user expiry cleanup are transactional          | _fill_          | _fill_                    | `PASS \| FAIL \| BLOCKED` |
| P2A-R3-08 | Projection reads are pure; backfill preserves revision and loses safely to document, title, and concurrent backfill writes             | _fill_          | _fill_                    | `PASS \| FAIL \| BLOCKED` |
| P2A-R3-09 | Released v1 schema/types, registries, converters, production wire persistence, and unknown/lossy-version rejection pass                | _fill_          | _fill_                    | `PASS \| FAIL \| BLOCKED` |
| P2A-R3-10 | Suites A, B, and C retain three-author blind provenance, current hashes, complete matrices, and their specified count/race gates       | _fill_          | _fill_                    | `PASS \| FAIL \| BLOCKED` |
| P2A-R3-11 | Owner `make ci` and connected `make scan` pass once at the exact clean candidate with no concurrent heavy work                         | _fill_          | _fill_                    | `PASS \| FAIL \| BLOCKED` |
| P2A-R3-12 | Write-path review, traceability evidence/state, phase-defect review, and named P2B/P8 handoffs are complete                            | _fill_          | _fill_                    | `PASS \| FAIL \| BLOCKED` |
| P2A-R3-13 | Pre-UAT migration policy, future migration/schema immutability guards, and secret-free source/log/evidence checks pass                 | _fill_          | _fill_                    | `PASS \| FAIL \| BLOCKED` |

Acceptance ownership is unchanged from revision 2: P2A-R3-01/11/12 cover all P2A
rows; P2A-R3-02/03/04 cover AC-DOC-001; P2A-R3-05 covers
AC-DOC-002/003/004/007/008/009/011; P2A-R3-06 is store evidence supporting
P2B-owned AC-SAVE-001; P2A-R3-07 covers AC-SAVE-003; P2A-R3-08 covers
AC-DOC-010; P2A-R3-09 covers AC-DOC-012; P2A-R3-10 covers the independent
portions of AC-DOC-001/004/010/011 and AC-SAVE-003; P2A-R3-13 covers migration,
released-schema, supply-chain, and privacy gates.
