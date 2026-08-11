# Task 6a: Preserve a cleared contact value through validation and live writes (AC-DOC-009)

The traceability row still records this specific draft-permissive case as
missing. Close it before v1's released artifacts are declared complete.

**Files:** create `packages/schema/fixtures/draft-cleared-contact-value.json`;
modify the explicit Go/TS schema/store-validation tests and
`apps/server/internal/resume/{codec_test.go,store_test.go}`.

- [ ] **Step 1: failing shared-contract tests.** The fixture contains one valid
      contact/detail entry whose `value` is `""`; JSON Schema and both aggregate
      validators accept it, and Go/TS decoding preserves the entry rather than
      treating it as absent.
- [ ] **Step 2: failing codec/live-store tests.** Parts→document→parts preserves
      the present cleared contact exactly. Create, Get, SaveDocument, and Get
      against live Postgres preserve the item and empty value without
      fabricating a sentinel or dropping the array element.
- [ ] **Step 3: implement the smallest fixture/test wiring; green.** No schema
      rule changes are expected; a red production path is routed back to its
      owning implementation author.
- [ ] **Step 4: gate.** `make schema-check`; focused live resume tests with
      `-race -count=1`; `make server-build server-vet server-test`.
- [ ] **Step 5: independent defect review, then commit** the fixture and its
      explicit conformance/live-write tests.
