# Phase exit criteria

The master plan's phase acceptance for P2B is: create/edit/delete via API; 4th
resume rejected; concurrent-write `412`; idempotent replay; old-client
write/emit; oversized payload rejected. Every one of those appears below as a
checkable row, plus the phase-specific gates.

- [ ] `make ci` green at the candidate commit — the ADR 0011 gate of record,
      including the DB-backed suites.
      `cd apps/server && go build ./... &&     go vet ./... && go test ./... -race`
      clean hermetically (DB- and object-storage-backed cases self-skip without
      their DSN/endpoint).
- [ ] `make server-test-db` green **with `./internal/resumeapi/...` and
      `./internal/media/...` in its package list** and `REQUIRE_TEST_DB=1` set,
      so the gate cannot pass vacuously by skipping. A local ad-hoc invocation
      is dev-loop evidence only, never phase-exit evidence.
- [ ] `REQUIRE_TEST_S3=1 make server-test-db` green against the pinned local
      S3-compatible service: the media conformance suite runs for real on the
      **S3 backend**, not only the filesystem backend (AC-MEDIA-004).
- [ ] `make api-check` green: the OpenAPI document lints, every P2B example
      validates against its schema, and the generated TypeScript client shows
      **no drift** without the check modifying the worktree (AC-API-001 stays
      green over the extended surface).
- [ ] No migration was added: `apps/server/migrations/` is unchanged from the
      P2A head, `make sqlc-check` is green with no regeneration, and
      `apps/server/sql/queries.sql` is unchanged.
- [ ] Every P2B route is registered, reachable, and covered: the route-table
      test asserts the complete set, and no route returns the Task 4
      `501 not_implemented` stub at the phase commit.
- [ ] **Create/edit/delete through the API** round-trips: a created resume is
      readable, every granular endpoint changes exactly what it claims, and a
      deleted resume returns `404 resume_not_found` afterwards.
- [ ] **The 4th resume is rejected** through the API with
      `409 resume_cap_exceeded`, and concurrent creates over HTTP still yield
      exactly 3 rows (the DB trigger remains the enforcement).
- [ ] **Concurrent write returns `412`**: two writers at the same revision
      produce exactly one winner; the loser's body carries `details.revision`
      and `details.document` byte-matching a fresh `GET` (AC-SAVE-001).
- [ ] **Idempotent replay**: the same key with the same body returns the stored
      response without re-executing; the same key with a different body returns
      `409 idempotency_key_reuse` with zero database deltas; a `csrf_rejected`
      retry reusing the same key cannot double-mutate (AC-SAVE-002).
- [ ] **Old-client write and emit** over the real HTTP/OpenAPI path: a document
      declared at an older version is accepted, projected, target-validated,
      persisted as the **complete current-version** document, and emitted at a
      declared supported version; an undeclared version fails closed
      (AC-SAVE-004).
- [ ] **Oversized and hostile payloads are rejected before any write**: the 256
      KB request-body limit, the 512 KB document limit, and every schema bound
      reject at limit+1 through the real handler, and the row is byte-identical
      before and after each rejection.
- [ ] Every rich-text field on every write path passes through
      `sanitize.RichText`; the shared hostile corpus is neutralized through the
      HTTP boundary, and removing the sanitizer call makes a test fail
      (AC-SEC-003's P2B half).
- [ ] Cross-user probes on every route return the same response as a wholly
      nonexistent id — no existence oracle (P2A D17), including the media
      routes.
- [ ] The CSRF matrix holds fail-closed on every mutating route (missing token,
      wrong token, wrong `Origin`, absent `Origin` with unusable `Referer`,
      wrong `Content-Type` on JSON routes) and `multipart/form-data` is
      permitted **only** on the photo upload route.
- [ ] Media: an upload is owner-only, bounded, content-sniffed, stored under a
      server-derived key, and referenced by the document; replace and delete
      remove the previous object; deleting a resume sweeps its prefix
      (AC-MEDIA-001…003).
- [ ] Blind suites D, E, and F were authored by three **separate fresh workers**
      from the written contracts before any implementation diff or author test
      was read (attested in their reports), were not edited by any
      implementation author, and are green.
- [ ] Every high-risk task diff got an independent defect review by a worker
      that did not author it; blocking findings were fixed and re-reviewed by a
      worker that did not write the fix. No author signed off its own work.
- [ ] Fresh-cache `golangci-lint run ./...`, `govulncheck ./...`, and
      `make scan` (Semgrep + full-history gitleaks) are green at the phase gate;
      no credential appears in any committed file or test fixture.
- [ ] Traceability: AC-SAVE-001/002/004 and AC-SEC-003's P2B half carry real
      test references; AC-MEDIA-001…005 and AC-SAVE-005 exist with references;
      `../traceability/README.md`'s prefix index and totals are corrected; the
      master plan's media/avatar ownership gap is struck.
- [ ] Integration handoffs are applied or explicitly assigned with an owner and
      a downstream gate ([integration-handoffs.md](integration-handoffs.md)).
- [ ] **Gate 1 — phase defect review (ADR 0011).** A fresh reviewer that
      authored none of the phase reads the phase diff for defects, spec
      consistency (§3 aggregate invariant and doc-shape rules, §4 endpoint and
      write-safety rows, §5 autosave/sanitizer constraints), interface
      stability, traceability closure, and adversarial challenge of this plan's
      assumptions and tradeoffs — at minimum D1 (contract-first), D4 (the
      down-emit/apply/up-accept model), D7 (the `Error.details` amendment to a
      P0-frozen shape), D10 (two media backends), D12 (owner-only photo read),
      and D13 (no media table).
- [ ] **Gate 2 — phase acceptance (ADR 0011).** A fresh worker that cannot edit
      product code, tests, snapshots, seeds, or criteria runs an immutable
      `../uat-phase-2b.md` catalog authored by the integration owner **before**
      the run, and emits the machine-readable fail-closed report: commit, exact
      commands, timestamps, state changes, retry count, one
      expected/observed/`PASS | FAIL | BLOCKED` row per criterion. `BLOCKED`
      counts as failure; missing evidence or undisclosed retries fails the gate.
- [ ] Evidence is pinned to the exact shipping commit. Any later product-code
      commit makes every row probing a changed path stale and forces a re-run.
- [ ] `make docs-fmt && make docs-lint` green for every `.md` touched.
