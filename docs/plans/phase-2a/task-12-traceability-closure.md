# Task 12: Traceability closure, docs, and integration handoffs

**Files:** modify only `docs/plans/traceability/{ac-doc,ac-save}.md`,
`docs/architecture.md`, and
`docs/plans/phase-2a/{README.md,exit-criteria.md,integration-handoffs.md}`.
Integration-owner-only shared-file edits remain handoffs.

- [ ] **Step 1:** fill test references for AC-DOC-001 (trigger + store + Suite A
      tests), AC-DOC-002 (Task 2 conformance + Task 5 pipeline),
      AC-DOC-003/004/007/008/009 (append live-write, limit+1, and
      cleared-contact references to the existing P0 evidence), AC-DOC-010
      (projection/CAS/backfill + Suite B), AC-DOC-011 (canonical 512 KB
      boundary + Suite C), AC-DOC-012 (immutable v1/types, both converter
      directions, old-client preparation/emission), and AC-SAVE-003 (Task 7 +
      Suite A). Remove every stale "not yet wired" annotation.
- [ ] **Step 2:** hand the integration owner, in one report: (a) any owner-only
      CI/Makefile edit still required for `server-test-db` or immutable released
      schemas; record the generated-write-method restriction as phase-review
      evidence only, not a requested implementation edit; (b) the P2B
      forward-binding notes: D14 customization allowlist, the idempotency retry
      contract, D12(ii) full-document persistence, and the real HTTP/OpenAPI
      AC-SAVE-004 old-client persist/emission test that consumes Task 8 rather
      than reimplementing it; (c) the global P8 retention sweep remains additive
      to Task 7's opportunistic per-user reap.
- [ ] **Step 3: gate.** `make docs-fmt && make docs-lint`.
- [ ] **Step 4: handoff.** Report the exact owned diff and gate output without
      staging or committing.
