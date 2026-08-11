# Phase exit criteria

- [ ] `cd apps/server && go build ./... && go vet ./... && go test ./...     -race`
      clean (hermetic; DB-backed cases self-skip). **Hard exit condition (owner
      ruling B11): the Makefile handoff has landed — `server-test-db`'s package
      list includes `./internal/resume/...` — and `make server-test-db`
      (equivalently, CI's server gate) is green _with that package list_.** A
      local ad-hoc
      `REQUIRE_TEST_DB=1 … go test ./internal/resume/... -race -count=1 -v`
      invocation (the interim tally recorded at each task's Step 5 during Tasks
      6–10, before the Makefile edit landed) is dev-loop evidence only and is
      **never** phase-exit evidence on its own — B11 is explicit that a local
      invocation cannot stand in for CI running the package for real.
- [ ] `make sqlc-check`, `make server-migration-test`, `make schema-check` all
      green at the phase head. (ADR 0010 made migrations goose-only and deleted
      `make data-drift`, `make migrate-gen`, `atlas.sum`, and `sql/schema.sql`
      after Tasks 1–7 executed; `migrations/` is now the single schema source
      for both goose and sqlc, so the Atlas trigger cross-check criterion is
      superseded — sqlc parses the trigger migration directly.)
- [ ] Migrations `00004`/`00005` appended after head `00003`; no existing
      migration `.sql` file modified or deleted (CI append-only check); the
      migration harness's four scenarios green over the new head.
- [ ] 4th-resume insert rejected by the **database** (raw-SQL test) and by the
      store; 20-way concurrent create yields exactly 3, under `-race`.
- [ ] Resume titles accept empty and exactly 160 Unicode code points, reject 161
      before any transaction or write, and leave the row unchanged on rejection.
- [ ] Every size bound in the named-bound matrix has a passing limit and failing
      limit+1 case, and the schema-walk completeness guard passes (no schema
      bound unexercised).
- [ ] Entry-id uniqueness enforced whole-resume in Go and TS against the shared
      fixture; `make schema-check` proves no generated drift.
- [ ] The cleared-contact-value fixture is valid in Go/TS and round-trips
      unchanged through live Create/Get/SaveDocument/Get, closing AC-DOC-009.
- [ ] CAS mismatch returns the current revision + winning doc; concurrent
      same-revision writers produce exactly one winner (Suite A green).
- [ ] Idempotency: replay returns the stored response without re-execution;
      same-key concurrent CAS calls invoke one callback and converge on replay
      or key reuse; different body rejected with zero loser writes; an ordinary
      callback error rolls back its mutation and record (Suite A green).
- [ ] Reads never write (purity test green under concurrency); backfill CAS
      loses cleanly to autosave; autosave after backfill does not 412; a
      title-only write between read and CAS also yields a retryable
      `BackfillLostRace` with `schema_version` still old, and a second
      `BackfillOne` then applies (Suite B green; B6).
- [ ] `resume.v1.schema.json` is immutable and byte-equal to the current v1
      source; generated raw-schema registries cover each released version;
      accepted/emitted declarations and adjacent `Up`/`Down` conversion are
      tested, including synthetic old-client preparation to the current
      canonical shape and supported-version emission. Real HTTP persistence is
      P2B's AC-SAVE-004 gate.
- [ ] The size-bound limit+1 matrix (formerly Suite C / Task 11) is complete in
      the author's own tests, mechanically derived from `../budgets.md`, spec
      §3, and `resume.schema.json`, with the schema-walk completeness guard
      proving no bound is unexercised. (ADR 0011 folded Suite C into author TDD;
      Suites A and B stay blind and independent.)
- [ ] Suites A and B were authored by fresh instances from the written contracts
      before reading implementation diffs, and were not edited by any
      implementation author; every high-risk task diff got an independent defect
      review, blocking findings fixed and re-reviewed.
- [ ] Fresh-cache `golangci-lint run ./...`, `govulncheck ./...`, and offline
      Semgrep are green; direct calls to the generated resume write methods are
      mechanically restricted to `internal/resume`.
- [ ] Traceability rows AC-DOC-001/002/003/004/007/008/009/010/011/012 and
      AC-SAVE-003 are closed at the phase commit; integration handoffs are
      applied or explicitly assigned with an owner and downstream gate.
- [ ] The phase defect review (ADR 0011): a fresh reviewer that authored none of
      the phase reads the phase diff for defects, spec consistency, interface
      stability, traceability closure, and adversarial challenge of assumptions
      and tradeoffs; blocking findings are fixed and re-reviewed.
- [ ] A fresh UAT worker runs `../uat-phase-2a.md` at the exact candidate
      commit, edits no product/test/criteria files, and reports no FAIL or
      BLOCKED rows.
- [ ] A separate evidence reviewer samples artifacts and reruns a deterministic
      subset at the same commit. Any later product-code change invalidates every
      affected UAT row and triggers a rerun.
