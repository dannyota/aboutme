# Privacy and media cleanup

This runbook covers the four one-shot lifecycle commands shipped in the Go
server. They are local or scheduler entry points; they do not start HTTP,
authentication, or Chromium components.

## Commands and schedules

Run commands from `apps/server` with the normal runtime environment. The
commands read `DATABASE_URL`. The two media commands also read the configured
private media backend settings. Do not place secret values in command lines,
logs, tickets, or evidence.

| Command                    | Local use                                                          | Phase 10 schedule |
| -------------------------- | ------------------------------------------------------------------ | ----------------- |
| `idempotency-expiry-sweep` | Remove expired replay records                                      | Hourly            |
| `media-deletion-sweep`     | Drain due exact-key deletion jobs                                  | Hourly            |
| `media-orphan-sweep`       | Reconcile old private objects; add `--dry-run` for inspection      | Weekly            |
| `privacy-retention-sweep`  | Apply session, lifecycle-audit, completed-media, and OAuth cleanup | Daily             |

Each run has a 30-minute deadline. PostgreSQL advisory locks make overlap a
successful skip. Cancellation is propagated and the worker joins its work before
returning. The command emits a fixed, identifier-free result and metrics surface
with success, failure, overlap, page or item counts, backlog, oldest age where
applicable, overdue work, and duration. A command execution failure exits
nonzero. A completed media sweep can report success with failed items queued for
retry; alert on its failure count too. A dry-run orphan sweep changes no object,
job, audit row, or cursor.

## Budgets and behavior

The idempotency sweep deletes at most 10,000 records in 1,000-record pages per
run. Each page verifies that deleted records and released retained bytes match;
the result reports both record and byte totals. It reports the remaining backlog
and oldest expired age. Request-path cleanup remains opportunistic; the hourly
run is the retention authority.

The media deletion sweep reads at most 2,000 jobs in 200-job pages and processes
up to four jobs concurrently. Claims have 30-second leases and each object
operation has a five-second deadline. It rechecks the live reference before
deletion, treats an already absent object as successful, and retries failed work
with a one-minute starting delay capped at six hours. It reports pending
backlog, oldest age, failures, and jobs overdue by 24 hours. Overdue jobs stay
queued and must alert until a terminal outcome is recorded.

The weekly orphan sweep lists at most 10,000 objects in 1,000-object pages and
uses at most four deletion workers. It considers objects only after 48 hours,
continues from the stored key cursor, and reports scanned, candidates, live,
queued, enqueued, deleted, absent, failed, and backlog counts. It rechecks live
references before creating retry work. A failed removal creates durable retry
work before the cursor advances. `--dry-run` performs listing and authority
classification without changing storage or database state.

The daily retention sweep processes up to 10,000 rows per category in 1,000-row
pages. Session metadata is redacted after 90 days. Lifecycle audit records and
completed media jobs are retained for 180 days from occurrence or completion.
Pending and overdue media jobs never expire. Existing OAuth transaction,
authorization-code, token, and idle-client cleanup is limited to 200 rows per
category/pass. The result reports category totals, backlogs, and oldest ages.

## Triage and rollback

First inspect the fixed result and secret-free structured diagnostics. A nonzero
failure, a growing backlog, an overdue count, or a missed heartbeat is an
operational failure. Check database reachability, the relevant advisory lock,
the task's configured media backend for media jobs, and the scheduler's task
exit and alarm events. Do not print database URLs, access keys, object keys, or
token values while collecting evidence.

If a run is cancelled or reaches its deadline, rerun the same command. Claims
and leases make unfinished media work eligible for a later run; retention pages
commit independently. Do not manually delete rows or advance the orphan cursor
to clear an alarm. If a cursor or job state is inconsistent, stop the schedule
and use a reviewed forward database repair with the affected references
rechecked.

Logical revocation is the immediate privacy boundary: deleting a reference,
session, generation, or grant denies access. Physical media deletion removes an
unreferenced object asynchronously and targets completion within 24 hours of
reference revocation. Backup expiry is separate: backups follow the 30-day
retention schedule and can outlive live revocation or physical object removal.
Rollback restores the application and scheduler configuration to a reviewed
previous version; it does not restore revoked references, sessions, or deleted
media. Phase 10 supplies the hosted schedule, alarm, log-retention, and backup
enforcement evidence.
