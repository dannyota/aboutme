# Maintenance command sessions

Status: Accepted target under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). R7d
implements this contract after the store and database seams exist. R8 owns
command composition and the final operation inventory. Local proof precedes
hosted activation.

## One backend for each command

Each privacy-retention or media-cleanup command leases one backend for its whole
run. It takes the runtime shared session barrier, then its fixed command session
lock. Every locking read and mutation uses a WriteTxRunner bound to that
backend. The session stays leased across pages, waits and external object calls,
so finalization cannot pass during an idle gap.

Auth mail has no command session lock. It uses the ordinary pool-backed
WriteTxRunner and retains its current two concurrent sends.

## Store interface and fixed locks

The store exposes these capabilities:

```go
type MaintenanceCommand uint8

const (
    MaintenanceIdempotencyExpiry MaintenanceCommand = iota + 1
    MaintenancePrivacyRetention
    MaintenanceMediaDeletion
    MaintenanceMediaOrphan
)

type MaintenanceSessionFactory interface {
    TryBeginMaintenance(context.Context, MaintenanceCommand) (MaintenanceSession, bool, error)
}

type MaintenanceSession interface {
    WriteTxRunner
    End(context.Context) error
}

func NewMaintenanceSessionFactory(*pgxpool.Pool) MaintenanceSessionFactory
```

Zero or unknown commands fail before acquiring a connection. The concrete
session, connection, transaction, SQL and cleanup methods stay private. Callers
cannot supply a role, advisory key or SQL, or retrieve Conn, DBTX or Queries.
Callbacks receive only transaction-bound Queries under the existing
[write-entry contract](transaction-entry.md).

The SQL definer functions own this exact map. Go passes the command enum and
does not compute a second set of hashes.

| Command                      | PostgreSQL session lock                                             |
| ---------------------------- | ------------------------------------------------------------------- |
| MaintenanceIdempotencyExpiry | bigint `hashtextextended('aboutme.idempotency-expiry-sweep.v1', 0)` |
| MaintenancePrivacyRetention  | bigint `hashtextextended('aboutme.privacy-retention-sweep.v1', 0)`  |
| MaintenanceMediaDeletion     | two-int `(int32(0x61626d65), int32(1))`                             |
| MaintenanceMediaOrphan       | two-int `(int32(0x61626d65), int32(2))`                             |

These preserve the locks in `apps/server/sql/privacy_retention.sql` and
`apps/server/internal/mediacleanup/worker.go`. For bigint locks, pg_locks uses
the high and low unsigned 32-bit halves as classid and objid, with objsubid 1.
Two-int locks use the first and second unsigned 32-bit values and objsubid 2.
Checks bind database, PID, mode and granted state too. The runtime barrier is
ShareLock; command locks are ExclusiveLock. Tests prove old/new contention in
both directions before removing the old callers.

## Database entry and exit

Owner-controlled SECURITY DEFINER functions provide
`runtime_try_enter_maintenance(command) -> boolean` and
`runtime_exit_maintenance(command) -> void`. They fix search_path to pg_catalog
and qualify persistent objects. Only aboutme_maintenance receives EXECUTE;
PUBLIC and other logins receive no marker or helper privileges.

Entry is one statement on the leased backend:

1. Validate role, command and temporary marker catalog. Require no marker row
   and no current-backend grant or wait for either selected lock. Do not read
   runtime gate or generation yet.
2. Acquire the shared runtime session barrier with the caller's deadline.
3. Read gate and generation. A non-open gate releases the acquired barrier and
   fails unavailable.
4. Try the fixed command lock. Confirmed overlap releases the one acquired
   barrier count and returns false without constructing a session.
5. Record backend PID, session-role OID, fixed command and entered generation in
   the owner-only temporary session marker.

The static marker uses ON COMMIT PRESERVE ROWS. Validate its actual temporary
namespace, runtime_owner ownership, persistence, row security, exact columns,
constraints, indexes, triggers and ACL, including column ACLs. The maintenance
login gets no marker DDL or DML. A lookalike or stale row is never adopted,
repaired or dropped. Entry errors or ambiguous replies retire the backend.

After all worker goroutines and transactions join, exit uses a detached
five-second context. It validates marker, PID, role, command and held locks,
deletes the marker, unlocks command then runtime, and requires each unlock to
return true. It proves no marker row, grant or wait remains. Releasing once and
checking absence detects a stacked lock count. Any mismatch or ambiguous exit
retires the backend with Hijack and bounded Close after exclusive Go ownership
is established. Close failure remains command failure with fixed diagnostics.

## Transaction ownership and shutdown

Use a capacity-one channel permit and a state mutex for open, closing and
retired. A write waits for the permit or context cancellation, then rechecks
open before touching pgx. The bound runner uses the current entry, callback,
finish and commit sequence on the retained backend. No query intervenes between
Begin and runtime_enter_write, or between runtime_finish_write and Commit. Every
path returns the permit after transaction disposition is known.

Entry SQLSTATE 55000 permits reuse only after confirmed rollback and valid
session locks/marker. Ordinary callback errors, cancellation or panic follow the
existing confirmed-rollback rules; panic is rethrown. AM001, ambiguous
begin/finish/rollback or any Commit error retires the whole session without
replay. Goexit runs deferred cleanup and cannot produce command success.

End first closes admission, then waits at most five seconds for the permit. When
it owns the permit it alone performs exit or retirement. It does not return the
permit afterward. If the wait expires, End must not Release, Hijack, Close,
unlock or touch pgx while an active callback may use it. A quarantine owner
retains the lease and barrier, waits for the active owner to join without a TTL,
then retires the backend. End reports an ambiguous failed command. No lease is
returned while cleanup or a callback remains active.

This follows pinned pgx v5.10.0's single-user connection contract. Hijack
transfers pool ownership; it does not join an active operation. Lifecycle may
not record success or graceful leave before all work joins and disposition is
known. A lost DB session never proves process death. Claims remain until the
existing graceful-completion rules or exact EC2 termination proof permit
release; quarantine has no timeout-based claim recovery.

## Allowed pool reads

Only these existing nonlocking SELECT methods may retain pool-backed Queries:

| Method                       | SQL source            | Command              |
| ---------------------------- | --------------------- | -------------------- |
| GetIdempotencyExpiryBacklog  | privacy_retention.sql | ExpireIdempotency    |
| GetSessionMetadataBacklog    | privacy_retention.sql | Retain               |
| GetLifecycleAuditBacklog     | privacy_retention.sql | Retain               |
| GetCompletedMediaJobsBacklog | privacy_retention.sql | Retain               |
| MediaObjectHasLiveReference  | media_cleanup.sql     | DeleteDue, Reconcile |
| GetMediaDeletionQueueState   | media_cleanup.sql     | DeleteDue            |
| GetMediaOrphanSweepCursor    | media_cleanup.sql     | Reconcile            |
| ClassifyMediaObject          | media_cleanup.sql     | Reconcile            |

Sources are under `apps/server/sql/`. No listed query may gain FOR UPDATE, FOR
SHARE or their variants, advisory locks, DML, writable CTEs, mutating functions
or transaction control. ListIdleOAuthClientCandidates uses FOR UPDATE SKIP
LOCKED and therefore runs inside the bound transaction. Every other locking read
and mutation uses that runner too. R8's existing operation inventory rejects
unclassified methods or changes to these classifications. A future locking read
needs a named bound method or WithWriteTx.

## Preserved worker behavior

Privacy commands retain overlapSkipped, page bounds, cutoffs, inner lock order,
backlog reporting and the 30-minute command limit. Pages remain separate
transactions. Media commands retain their separate locks, caps, leases,
deadlines, retries, dry runs and result fields. Four media object operations may
remain concurrent while their short database transactions serialize.

Auth mail retains one-second polling, ten-job claims, two sends, 30-second
leases, ten-second sends, and registration/reset/user scope before the leased
job lock. The sender stays inside the existing transaction. Cancellation and
no-oracle behavior remain unchanged.

An ambiguous database result never justifies repeating external work or SQL.
Workers use only their existing exact job, lease, cursor or object evidence to
resolve outcomes. Missing evidence is a failed or ambiguous run. This contract
adds no receipt, claim-recovery or fencing protocol.

One maintenance process retains one of its existing four pool connections;
allowed reads use at most the other three. Auth mail stays in the application
pool capped at 12. No additional pool or capacity allowance is introduced.

## Ownership and proof

R1/store owns the session primitive, definer functions and query seams in one
root-serialized migration. R7d owns authmail, mediacleanup and privacyretention
callers. R8/root owns cmd/server composition, generated SQL output, shared
fixtures and final operation coverage. Goose retains its separate migration
connection contract.

Live tests must prove exact PID and runtime-before-command-before-row order,
overlap across idle gaps, all four old/new lock pairs, marker contamination and
recursion, cancellation at every stage, panic/Goexit, commit/rollback/exit
ambiguity, no concurrent pgx cleanup, use-after-End and the four-slot pool cap.
Crash tests must retain claims until graceful joined completion or exact EC2
fencing. Existing caps, privacy deadlines, media authority and mail behavior
tests remain required.

Authors run focused package tests with the public TEST_DATABASE_URL and
REQUIRE_TEST_DB=1, then their affected race tests. Root runs
`make sqlc-check server-test-db server-test-integration server-migration-test`
and the Go gate after integration. Implementation checks remain pending.
