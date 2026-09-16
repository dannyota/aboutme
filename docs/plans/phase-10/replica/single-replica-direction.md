# Single-replica direction implementation plan

> For workers: use superpowers:executing-plans within AGENTS.md ownership. ADR
> 0024 keeps one author per task and one fresh phase review. Steps use checkbox
> syntax for tracking.

**Goal:** Land the direction set by
[ADR 0036](../../../adr/0036-single-replica-launch-and-pipeline-migrations.md):
finish the half-built slice, move migrations into the deployment, retire the two
wake slices, and leave the tree with exactly one readable account of what is
built and what is deferred.

**Architecture:** No new migration. R1.11's store transport completes
migration 23. A migration step runs the existing protected migrator from a
command that already exists, before the service starts. The retired plans are
deleted rather than annotated, because git keeps them and a stale plan misleads.

**Tech stack:** Go, PostgreSQL 18, Goose, ECS; repository pins apply.

**Spec:**
[ADR 0036](../../../adr/0036-single-replica-launch-and-pipeline-migrations.md),
[runtime tasks](runtime-tasks.md),
[migrator](../../../design/scaling/migrator.md).

## Global constraints

- One database container, `aboutme-test-db`, 512 MB cap. One heavy command at a
  time; two concurrent `go test` runs have been out-of-memory killed.
- Live DB targets use `-count=1`. Public fixture DSN:
  `postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable`. Live
  runs need `-timeout 30m`; the migrations package takes about 320 seconds under
  race.
- Migrations stay editable until `apps/server/migrations/.uat-baseline` lands.
  This plan adds no migration and edits none.
- Root owns the Makefile, workflows, plans, generated files and Git. Workers
  never run a git command.
- `apps/server/.golangci.yml` sets `errcheck.check-type-assertions: true` and
  the govet shadow check.

## Ownership and acceptance

Task 1 goes to one author who did not write migration 23. Tasks 2 through 4 are
the integration owner's: they touch plans, the deployment and the phase record.

Definition of done: migration 23 has a typed store transport with the same proof
standard as its siblings; a documented migration step exists and is proved to
run to completion before the service starts; no plan in the tree describes
retired work as pending; and the phase README states the single replica decision
in one place.

---

### Task 1: Complete the membership evidence transport

**Files:**

- Create: `apps/server/internal/store/runtime_membership_evidence.go`
- Create: `apps/server/internal/store/runtime_membership_evidence_test.go`

**Interfaces:**

- Consumes: the three generated methods in
  `apps/server/internal/store/runtime_membership_evidence.sql.go`
  (`RuntimeFinishServingGracefulLeave`, `RuntimeFinishMaintenanceGracefulLeave`,
  `RuntimeRecordEC2Termination`), `WriteTxRunner`, and the accepted result
  shapes in
  [membership evidence transport](../../../design/scaling/membership-evidence-transport.md).
- Produces: `RuntimeMembershipEvidenceTransport` and its pool constructor, with
  three context-first methods returning owned `RuntimeLeaveResult` and
  `RuntimeFenceResult` values. The name comes from the design document's Go
  block, which outranks this plan, and matches the three sibling transports.

- [x] **Step 1: Read the accepted transport contract and the two siblings**

Read `docs/design/scaling/membership-evidence-transport.md` for the exact method
set and result fields. Then read
`apps/server/internal/store/runtime_lifecycle.go` and `runtime_rates.go`: they
are the accepted shape for this exact kind of transport, including the generic
write-runner helper and decode-inside-the-callback rule.

- [x] **Step 2: Write the failing per-method tests**

One test per method proving one runner call, one query, the right SQL name in
the query text, and a fully decoded result. Reuse `registrationRunnerStub`,
`registrationDBStub`, `claimOrderRunner`, `assertEvents`, `liveWrappedRunner`,
`commitResponseLostTx`, `registrationFinishResponseLostTx` and
`testutil.NewMigratedTestDatabase` rather than writing new harness code.

- [x] **Step 3: Run them and record the failure**

```sh
REQUIRE_TEST_DB=1 TEST_DATABASE_URL='postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable' go test -race -count=1 -timeout 30m ./internal/store -run '^TestRuntimeMembershipEvidence' -v
```

Expected: build failure naming the missing store type and methods.

- [x] **Step 4: Implement the transport**

Each method validates input before touching the runner, runs one generated
query, and validates and copies the result inside the callback so an invalid row
rolls the transaction back. Every returned error yields the exact zero result,
including a finish failure or commit ambiguity after a value decoded. No retry.
No driver value escapes.

- [x] **Step 5: Prove the matrices and the role boundary**

Add: an invalid-row matrix per result type, proving each returns the zero value
and fails before finish; input rejection proving the runner is never called; the
write-barrier order test, which must name its function, not a placeholder; and
live tests for a real graceful leave, a real termination proof that reclaims a
claim, commit ambiguity returning zero while the row committed, finish failure
rolling back, and the role boundary in both directions. Graceful leave belongs
to the application and maintenance roles; termination proof belongs to the
fencing proof role.

- [x] **Step 6: Run the required checks**

```sh
go build ./... && go vet ./internal/store
REQUIRE_TEST_DB=1 TEST_DATABASE_URL='postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable' go test -race -count=1 -timeout 30m ./internal/store
GOGC=50 golangci-lint run ./internal/store --tests
```

The lint run must end with `0 issues.`

- [x] **Step 7: Report**

Write `.dev/phase-10/runtime-membership-evidence-store-author-report.txt` with
base commit, red evidence per method, exact green output, a coverage map, any
deviation from the transport contract, and anything unresolved. Release both
files. Root inspects, reruns and commits.

---

### Task 2: Make the deployment migrate before the service starts

**Files:**

- Modify: `docs/plans/phase-10/infrastructure/` contracts that describe the
  deploy sequence; the exact file is found in Step 1
- Modify: `docs/runbooks/` deploy or UAT runbook, same
- Modify: `docs/design/deployment.md`

- [x] **Step 1: Find every place the deploy sequence is written**

```sh
grep -rln "migrate\|migration" docs/design/deployment.md docs/plans/phase-10/infrastructure docs/runbooks
```

List the files that state when migrations run. The infrastructure tasks are
unimplemented, so this is a contract change, not a code change.

- [x] **Step 2: State the sequence in one place and reference it elsewhere**

The sequence is: start the database; run the migration task to completion; only
then update the service. Stopping reverses it. Record that the migration task
uses the existing `cmd/migrate` binary with `MIGRATION_IDENTITY` set, runs as a
task that exits, and that a nonzero exit blocks the service update.

Record that the application does not migrate at startup, and that the existing
advisory lock makes a second concurrent migrator safe rather than expected.

- [x] **Step 3: Record the single-replica service configuration**

The service maximum is one for this release. Note that raising it requires the
deferred work named in ADR 0036, so a future reader does not raise a number and
assume the coordination is finished.

- [x] **Step 4: Check the docs gate**

```sh
make docs-lint
npx prettier --check --ignore-path /dev/null <each changed file>
```

---

### Task 3: Retire the two wake slices

**Files:**

- Delete: `docs/plans/phase-10/replica/exclusive-wake.md`
- Delete: `docs/plans/phase-10/replica/wake-migrations.md`
- Modify: `docs/plans/phase-10/replica/runtime-tasks.md`
- Modify: `docs/design/scaling/README.md`

- [x] **Step 1: Delete the two retired plans**

Git keeps them and ADR 0036 records why they went. A plan that describes work
nobody will do is worse than no plan.

- [x] **Step 2: Remove their reservations from the task index**

In `runtime-tasks.md`, remove the R1.12 and R1.13 paragraphs and the migration
24 and 25 reservations. Say instead that migrations 24 and upward are
unreserved, and that the wake contracts remain accepted design for a later
second replica.

- [x] **Step 3: Keep the design documents, mark their status**

Do not delete `wake-operations.md` or `wake-migrations.md` under
`docs/design/scaling/`. They are accepted contracts and ADR 0036 does not
withdraw them. Add one line to each saying implementation is deferred under ADR
0036, so a reader does not start on them.

- [x] **Step 4: Check the links and the docs gate**

```sh
grep -rn "exclusive-wake.md\|replica/wake-migrations.md" docs/
make docs-lint
```

Every link to a deleted plan must be gone. Expected: no hits, then a clean gate.

---

### Task 4: Make the phase record match reality

**Files:**

- Modify: `docs/plans/phase-10/replica/lifecycle-operations.md`
- Modify: `docs/plans/phase-10/replica/membership-evidence.md`
- Modify: `docs/plans/phase-10/replica/README.md`
- Modify: `docs/plans/phase-10/README.md`
- Modify: `docs/design/decisions.md`

- [x] **Step 1: Tick the two completed plans and record their evidence**

Both are complete in code but unticked. Follow the pattern the claim and rate
plans already use: mark the checklist, then add an implementation evidence
section with what the checks measured, what the fresh review found, and what
each slice left open. Membership evidence is only complete once Task 1 lands, so
do this after it.

- [x] **Step 2: State the direction once, at the top of the phase**

In `docs/plans/phase-10/README.md`, state that the first release runs one
serving replica, that migrations run from the deployment, and that the
coordination work beyond migration 23 is deferred under ADR 0036. Link the ADR.
Remove or correct any sentence that promises a two-replica proof as a phase
exit.

- [x] **Step 3: Map the new decision in the index**

Add ADR 0036 to `docs/design/decisions.md` in the established format, naming the
rule it establishes.

- [x] **Step 4: Correct the exit criteria**

In `docs/plans/phase-10/exit-criteria.md`, the hosted acceptance item requiring
production-shaped one to two capacity during writes no longer applies to this
release. Rewrite it as a single-replica acceptance, and record the two-replica
proof as a Phase 11 precondition instead of deleting the requirement.

- [x] **Step 5: Check the docs gate**

```sh
make docs-lint
```

---

## Verification and release

- [x] Run the full local gate at one unchanged candidate, one heavy command at a
      time, and record each result:

```sh
make server-build server-vet server-test
make server-test-db
make server-migration-test
make sqlc-check
```

- [x] Confirm the working tree is clean and every changed document passes
      `make docs-lint`.
- [x] A fresh reviewer who authored none of this reads the integrated diff and
      confirms by name: the transport returns zero authority on every error; the
      deleted plans are unreferenced; no retained document still describes wake
      implementation as pending; and the deployment sequence states that
      migrations complete before the service starts.
- [x] Record the outcome in this plan's implementation evidence section, then
      delete this plan at phase exit as the lean-docs rule requires.

## Implementation evidence

- Task 1 landed as `8ad40f0`; tasks 2 to 4 as `ca4bf90`, `60896e0`, `848cf28`
  and `8528006`.
- 2026-09-17 local gate, run in chunks: `make check` (build, vet, tests,
  golangci-lint, sqlc drift), `make server-test-db`,
  `make server-migration-test` and `make docs-lint` passed.
- The fresh Phase 10 reviewer confirmed the four points by name. It found three
  plans that still described wake as pending work (`runtime-tasks.md`,
  `lifecycle-operations.md`, `membership-schema.md`); they now mark it deferred.

## What this plan does not do

It does not remove any migration, schema, function or store transport already
built. It does not change the public API, the privacy deadlines, or any database
invariant already proved. It does not raise the service maximum above one, and
it does not withdraw the accepted contracts for a second replica.
