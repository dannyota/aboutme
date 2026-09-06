# Task 8.5 — Retention sweeps

**Owner:** Sol author. **Acceptance:** AC-PRIV-003/004/005. **Predecessor:** 8.1
schema. Read the phase index, operations/budgets, ADR 0016 and current
idempotency/session/auth cleanup code.

**Owned paths:** `apps/server/internal/privacyretention/**`,
`apps/server/sql/privacy_retention.sql`. Root owns generated files, migration,
command wiring and Git.

Implement the Worker contract with one advisory lock per command. Use a bounded
run context and injected clock. Emit fixed numeric success, backlog, oldest-age
and failure signals without identifiers or raw errors.

Idempotency expiry runs hourly with 1,000 records per page and 10,000 per run.
Preserve the write lock order: acquire user locks before deleting their replay
records and changing usage. The existing global delete query alone does not
establish that order. Never deadlock a writer that already holds its user lock.
Delete and release exact stored body/header byte counters in one transaction.
Skip busy accounts without starving inactive accounts; signal remaining backlog
and oldest expired age. Preserve unexpired records and exact replay behavior.

Daily privacy retention redacts session IP/UA at 90 days from created_at. Delete
lifecycle audit and completed media jobs at 180 days from event or completion,
respectively. Pending/overdue media jobs never expire. Each category is
oldest-first, 1,000/page and 10,000/run. Use bounded expired OAuth/password
cleanup where needed without duplicating existing mail-worker ownership;
document any separately owned cleanup path.

Security diagnostics remain secret-free structured logs. Their 180-day hosted
retention is Phase 10 configuration; do not silently claim local files prove
hosted retention.

- [ ] Write real-DB boundary and accounting tests; observe red.
- [ ] Implement bounded pages and command results.
- [ ] Test exact 24h/90d/180d boundaries, inactive accounts, multiple pages/run
      cap, counters with JSON null/body/headers, rollback, repeated execution,
      concurrent writer/expiry/deletion, locked accounts, audit survival after
      account deletion and pending media preservation.
- [ ] Run `go test -race -count=1 ./internal/privacyretention` from
      `apps/server`.

Report the observed failures and passes, shared edits and remaining uncertainty.
