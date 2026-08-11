# Task 6b: Mechanically restrict generated resume writes to the domain store

Closes owner correction 5's unenforced choke-point convention before phase exit.
This is an integration-owner task because it touches root policy/gates.

**Files:** modify `.semgrep.yml`, the root `Makefile`, and
`.github/workflows/ci.yml`; create `scripts/test-resume-write-chokepoint.sh`. Do
not edit generated sqlc code.

- [ ] **Step 1: failing policy regression.** The script creates temporary Go
      files (never tracked): an outside `apps/server/internal/api` caller of
      each forbidden generated method must initially pass, proving the gap; the
      same calls under `internal/resume` are the allowed control.
- [ ] **Step 2: add a project Semgrep rule** covering `CreateResume`,
      `DeleteResumeForUser`, `UpdateResumeDocumentCAS`, `UpdateResumeTitleCAS`,
      `BackfillResumeDocumentCAS`, `CreateIdempotencyRecord`,
      `DeleteIdempotencyRecordIfExpired`, and
      `DeleteExpiredIdempotencyRecordsForUser`, plus the lock-bearing
      `LockUserForResumeWrite`. Include `apps/server/**/*.go`; exclude only
      generated definitions in `internal/store/**` and authorized calls in
      `internal/resume/**`. Method-name additions in `queries.sql` must extend
      this list in the same reviewed change.
- [ ] **Step 3: make the regression executable.** Add an owner-applied
      `semgrep-policy-test` target that asserts the temporary outside fixture
      fails with the new rule and the inside fixture passes; leave no temporary
      file behind. The script also parses named blocks in `sql/queries.sql` and
      fails if any `INSERT`/`UPDATE`/`DELETE` targeting `resumes` or
      `idempotency_records`, or the resume-user `FOR UPDATE` lock, is absent
      from the rule's covered-method manifest. Run it in CI beside the offline
      Semgrep gate.
- [ ] **Step 4: gate.** `make semgrep-policy-test semgrep`; fresh repository
      scan proves no outside production/test caller; `make docs-lint` if policy
      documentation changes.
- [ ] **Step 5: independent security/defect review, then commit** only the
      policy, regression script, Makefile, and CI wiring.
