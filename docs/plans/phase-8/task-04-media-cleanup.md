# Task 8.4 — Media deletion and orphan reconciliation

**Owner:** Sol author. **Acceptance:** AC-MEDIA-003/006/007, AC-PRIV-004/005.
**Predecessor:** 8.1 schema. Read the phase index, media backend contract,
operations/budgets and ADR 0019.

**Owned paths:** `apps/server/internal/mediacleanup/**` and
`apps/server/sql/media_cleanup.sql`. Root owns migration, sqlc output, commands,
build files and Git. Report missing shared edits.

Implement the Worker contract in the phase index. Hold a dedicated advisory lock
for a whole run, fail closed on overlap, and join all workers on cancel. Use
distinct locks for deletion and orphan runs; row leases and conditional
completion prevent one runner from completing another runner's claim.

Deletion is hourly: 200/page, 2,000/run, four concurrent, one attempt per due
job per run. Claim with a 30-second lease, use a 5-second object deadline, and
double retry delay from one minute to at most six hours. Validate canonical key
and resume ownership encoded in it before I/O. Recheck the current exact
document reference before deletion. A live reference blocks deletion and raises
a fixed failure signal. Already-absent objects succeed.

Mark a 24-hour target breach once in the durable audit ledger and emit an
overdue alert every run while overdue. Record completion and audit atomically;
retain completed jobs and audit for 180 days. Failures remain queued with no
finite terminal-failure cutoff. Never print keys or backend errors.

Reconciliation is weekly: 48-hour minimum age, 1,000/page, 10,000/run, four
concurrent, at most three attempts per object, 1/2-second backoff and 5-second
I/O deadlines. Compare each validated object with live references and
outstanding deletion jobs. Do not directly delete queued or live objects.
Persist the next cursor at the run ceiling; reset at exhaustion. Invalid listing
order, cursor, oversized page or key fails closed. Dry run performs the reads
and reports counts but never deletes, enqueues, audits, or saves the cursor.
Repair unreferenced crash candidates and retain failed cleanup as exact durable
jobs rather than silently losing them behind the cursor.

- [ ] Write fake-backend bounds, live-reference and failure tests; observe red.
- [ ] Implement bounded selection, claims, retries and completion.
- [ ] Test stale leases, concurrent runners, clock boundaries, invalid objects,
      foreign resume objects, late results, ambiguous outcomes, cancellation,
      oldest-first pagination, run ceilings, cursor resume/reset, dry-run
      immutability, 24-hour once-only audit, and recovery after audit/DB
      failure.
- [ ] Run `go test -race -count=1 ./internal/mediacleanup` from `apps/server`,
      including real PostgreSQL and filesystem cases.

Report evidence, query generation needs and exact numeric results.
