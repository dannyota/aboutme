# Migration connection retirement

Status: Accepted correction to the [dedicated migrator](migrator.md) under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). The
[scaling index](README.md) records whether it is built.

## Physical closure

Capture an opaque, close-only capability before any SQL on each admitted
connection. It must close that exact data socket even after database/sql
discards its driver handle. Physical proof is either a closed pgx CleanupDone
channel or confirmed closure of the captured TCP socket. This fences the local
transport; it does not prove PostgreSQL has already reaped the backend PID or
released its locks.

Pinned pgx v5.10.0 marks a connection closed before its asynchronous cleanup
finishes. Conn.Close returns nil when IsClosed is already true, while a separate
CancelRequest can delay the original socket's closure for up to 15 seconds.
CleanupDone distinguishes completed resource cleanup from that logical state.
Also, database/sql handles driver.ErrBadConn by marking sql.Conn done and
clearing its driver reference. A later Raw callback cannot recover the socket.
IsClosed, ErrConnDone and a failed Raw call therefore cannot prove retirement.

## Capture and ownership

Capture inside sql.Conn.Raw before identity queries, catalog reads, locks or
transactions on the pinned connection. Require the exact pgx stdlib.Conn and
accept only an exact net.TCPConn or tls.Conn wrapping an exact net.TCPConn.
Reject other drivers, Unix sockets and custom transport wrappers before SQL.
Hosted RDS uses TLS over TCP; local PostgreSQL uses TCP.

The private capability retains only the original TCP pointer, receive-only
CleanupDone channel and local ownership state. This is the narrow exception to
the rule against retaining Raw-derived values. No stdlib, pgx, PgConn, protocol,
configuration or address handle escapes Raw. The capability exposes no socket
getter, Read or Write operation. Retain it by unique pointer and resolve it
once; repeated cleanup must never touch another connection.

The operation records the first observed PID and checks it during later SQL
cleanup. A failure before that query still retires the captured socket. A PID is
an identity check, not the physical closure mechanism.

## Retirement sequence

Use one five-second cleanup context derived from context.WithoutCancel:

1. If Raw is available, verify the same driver, CleanupDone channel and TCP
   pointer, then call pgx.Conn.Close. Preserve real close or binding errors.
   Return nil from the callback after successful closure.
2. If Raw returns ErrConnDone, continue with the saved capability. It remains
   responsible for closure. Do not add that expected logical-discard result as a
   cleanup failure once independent physical proof succeeds.
3. Check CleanupDone without blocking. If closed, resource cleanup is proved.
4. Otherwise, call SetLinger(0), set an immediate deadline and synchronously
   close the exact TCP socket. A nil result or net.ErrClosed proves local
   closure. Supporting linger/deadline errors do not fail a confirmed close.
5. Preserve any other TCP close error. Recheck CleanupDone; if it remains open,
   report unproved retirement and never reconnect or retry.
6. Consume the capability and clear its socket reference. The physical
   retirement primitive never calls sql.Conn.Close or closes the caller's DB.

Keep the original operation error first, followed by real cleanup errors.
CleanupDone may remain open while pgx's separate CancelRequest finishes; this
does not fail a confirmed data-socket close. No application cleanup may keep
using a returned lease. A successful operation with normal retirement remains
successful; the primitive must not manufacture driver.ErrBadConn.

## Goose and manual operations

Goose constructs the connection before calling SessionLock. Capture at the start
of SessionLock. Every failing return after capture retires that socket before
returning. Successful SessionLock transfers ownership to SessionUnlock, which
performs ordered lock and identity cleanup, then retires unconditionally. Clean
bootstrap handoff requires confirmed retirement.

Goose owns the following logical sql.Conn.Close. The physical primitive must
leave it to Goose, so a clean migration does not acquire an artificial
ErrConnDone. If an earlier driver error already discarded the logical
connection, preserve Goose's resulting error tree. Do not broadly filter its
ErrConnDone or other cleanup errors to force a clean handoff.

Status, provisioning, adoption and reconciliation capture immediately after
db.Conn and before SQL. One owner handles every exit, including failed initial
catalog reads, identity changes, BEGIN, queries, rollback, COMMIT, reset and
unlock. A poisonSQLConn or logical Close alone is insufficient after admission.
Provisioning and adoption retire even after clean success. Their owner then
performs the one logical Close and treats ErrConnDone as expected only after
independent physical proof.

Status may reuse a connection only after confirmed read-only COMMIT or ROLLBACK,
unchanged PID, exact identity reset and clean driver/socket binding. Keep its
capability until logical Close returns nil, then disarm it. If clean release
fails, the still-live capability must retire the socket. Reconciliation uses the
same read-only cleanup rules and retires on any ambiguity.

The sole reconnect exception remains
[adoption reconciliation](migration-provisioning.md#adoption-result-and-ambiguous-outcomes).
It may start after local socket retirement without a remote PID-absence check.
Only the full atomic, monotonic convergence proof permits success. An old
snapshot preserves the original error and never causes mutation replay.

## Required proof

Use a real loopback TCP proxy whose DialFunc returns actual TCP connections.
Keep the original data socket open while inducing a read/protocol failure and
gate the separate CancelRequest connection. The fixture must not close or
half-close the original socket to simulate retirement.

Prove that COMMIT can reach PostgreSQL before a lost or corrupt response yields
driver.ErrBadConn, that later Raw returns ErrConnDone, and that the saved
capability closes the original socket before the operation returns. The proxy
must observe EOF or reset before releasing its cancellation gate. Check the
five-second bound, original error, no reconnect, and continued caller DB use.

Also cover normal closure, canceled parents, failure before PID capture,
CleanupDone already closed, pending async cleanup, TLS over TCP, unsupported
transports, binding mismatch, repeated consumption and all admitted early
returns. A later explicit DB operation may prove replacement with a new PID.
That observation is separate from the local transport proof.

The store write runner has the same pgx async-close risk. Its separately owned
correction reuses the physical-close primitive while preserving its exclusive
lease, Hijack and ambiguity rules. Hijack().Close alone is insufficient.
