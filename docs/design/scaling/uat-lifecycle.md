# UAT maintenance and database stop

Status: Accepted under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). This is
the target contract. Local implementation and hosted proof remain Phase 10
gates.

## Authority and scope

This contract preserves docs/design/operations.md, docs/design/budgets.md, ADR
0016, ADR 0019, ADR 0034, and Task 10.18. The hourly managed-controller
heartbeat still runs. It may avoid starting RDS and an EC2 maintenance replica
only when the last immutable receipt proves that no work can become due before
the next planned wake. Missing proof starts maintenance or keeps RDS running.

## Maintenance placement and identity

- All server-image maintenance commands run on one temporary complete EC2
  replica. They do not run on Fargate. The replica registers an incarnation,
  joins runtime coordination, and remains private with no ALB admission.
- Mail sends, media object operations, and database claims bind to that replica
  ID. A graceful `left` receipt releases joined claims. Crash, suspension, or
  ambiguous leave retains claims until exact EC2 termination proof fences that
  incarnation. ECS STOPPED, task health, lease expiry, and advisory-lock loss do
  not prove the process or external work stopped.
- Lifecycle command and fence proof have no replica claims and may run as
  independent bounded Fargate tasks. Infrastructure apply has no database login.

## Maintenance database grants

`aboutme_maintenance` is a distinct login used only by the existing one-shot
server commands. It does not receive application-wide ownership or lifecycle
authority. Preserve existing SQL instead of wrapping every job in new definer
functions.

- Idempotency expiry needs SELECT on users/idempotency_records, DELETE on
  idempotency_records, and UPDATE on idempotency_usage. Privacy retention needs
  SELECT/UPDATE on sessions; SELECT/DELETE on lifecycle_audit_events and
  completed media_deletion_jobs; SELECT/DELETE on expired oauth transactions,
  authorization codes, terminal tokens, and eligible idle clients; and SELECT on
  grants/tokens only for the existing client-eligibility predicate.
- Media cleanup needs SELECT/UPDATE on media_deletion_jobs, INSERT for a proved
  orphan job, SELECT on resumes, INSERT on lifecycle_audit_events, and
  SELECT/UPDATE on the media-orphan privacy_sweep_state row. Auth-mail needs
  SELECT/UPDATE on auth_email_jobs and bounded DELETE on finished jobs, expired
  password registrations, and expired reset tokens. No job path gets INSERT or
  DELETE on users, sessions, resumes, identities, credentials, grants, or live
  tokens. Schema integration narrows these to exact columns used by final
  generated queries and rejects any extra table, column, or sequence privilege.
- Grant SELECT on resumes only for media live-reference checks. No resume,
  identity, credential, public-state, transition, capacity, rate, or fence DML.
- Existing session advisory-lock calls remain available. Grant EXECUTE only on
  new SECURITY DEFINER functions for maintenance incarnation registration,
  admission claims, finish_graceful_leave, begin_quiescence,
  record_job_run_result, and create_quiescence_snapshot. Final stop receipt and
  write-gate functions belong only to `aboutme_lifecycle_command`.
- Default privileges and PUBLIC remain revoked. Tests run every existing command
  under this real role and separately prove forbidden DML and lifecycle/proof
  execution fail.

## Durable scheduling schema

## runtime_write_state

- singleton boolean primary key fixed true; generation bigint positive;
  write_gate text check in ('open','closing','closed'); last_write_at and
  last_accepted_writer_at timestamptz not null; last_writer_kind bounded enum;
  last_writer_operation_id text bounded; updated_at timestamptz.
- migrator_enforcement_version smallint in (0,1); migration_history_owner text
  bounded to 1–63 octets. Foundation version 0 records the original Goose table
  owner. Migrator enforcement installs the owner trigger and records version 1
  plus runtime_owner atomically. This does not prove legacy write coverage.
- Every transaction that can create, reschedule, lease, complete, expire, or
  delete application/job state participates in the write barrier below.
  runtime_finish_write runs as runtime_owner and advances generation once per
  dirty transaction and sets last_write_at from clock_timestamp immediately
  before commit. The deferred constraint trigger only asserts finish.
  Transactions that accept an application mutation also advance
  last_accepted_writer_at; maintenance and controller bookkeeping do not extend
  the idempotency tail. Trigger DML on runtime_write_state does not recurse.

## runtime_job_schedule

- category primary key from the fixed category table below; cadence interval;
  last_started_at, last_completed_at, next_due_at timestamptz; run_id uuid;
  state in ('idle','running','failed'); backlog bigint nonnegative;
  oldest_due_at nullable; source_generation bigint positive.
- start and result functions require the category advisory lock, registered
  maintenance replica, exact run UUID, and expected write generation. A success
  records database clock, backlog, oldest due, and computed next due atomically.
  Failure or ambiguity never advances last_completed_at or next_due_at.

## runtime_quiescence_snapshots

- snapshot_id uuid primary key; controller_generation and operation_id unique;
  write_generation bigint; observed_at, last_write_at, last_accepted_writer_at,
  earliest_wake_at timestamptz; replica_id references terminal left;
  category_digest bytea length 32; result fixed 'maintenance_quiescent'.
- Immutable child rows record every category, source table/index identity,
  backlog, oldest_due_at, next_due_at, hard_deadline_at, and last successful
  run. The digest covers ordered child rows and all parent fields.
- A snapshot cannot be issued until observed_at >= last_accepted_writer_at + 24
  hours. It is preparation evidence, not StopDBInstance authority. Termination
  proof and capacity finalization necessarily invalidate its write generation.

## runtime_stop_receipts

- receipt_id uuid primary key; snapshot_id references the immutable snapshot;
  controller_generation and operation_id unique; final_write_generation bigint;
  issued_at, earliest_wake_at, expires_at timestamptz; category_digest bytea
  length 32; result fixed 'safe_to_stop'. Immutable parent and category children
  are writable only by finalize_stop_receipt.
- The S3 controller object stores receipt ID, digest, controller generation, and
  wake time. A receipt is authority only while runtime_write_state.write_gate is
  closed and its final generation still matches.

## Fixed category calculations

1. `idempotency_expiry`: idempotency_records.expires_at. Any expires_at <= now
   is due and blocks stop until the bounded hourly sweep reports zero backlog.
   Next due is min(expires_at). The controller still evaluates this each hour.
2. `media_deletion`: incomplete media_deletion_jobs. Due is min(next_attempt_at,
   enqueued_at + 24 hours). Any due/overdue row or live lease blocks receipt.
   Wake early enough to finish before enqueued_at + 24 hours.
3. `auth_mail`: pending auth_email_jobs use min(next_attempt_at, expires_at);
   leased rows always block. Sent/terminal cleanup is due seven days after
   COALESCE(sent_at,terminal_at). A pending mail expiry is a hard deadline and
   cannot be skipped.
4. `password_expiry`: password_registrations.expires_at and
   password_reset_tokens.expires_at. The earliest is next due; expired backlog
   blocks receipt until cleanup reports zero.
5. `oauth_expiry`: oauth_transactions.expires_at,
   oauth_authorization_codes.expires_at, terminal oauth_tokens cleanup time, and
   oauth_clients last_used_at + 24 hours subject to the existing no-live-
   grant/token predicates. Due candidates block; next due is their minimum.
6. `session_metadata`: sessions.created_at + 90 days for rows retaining UA/IP.
   It retains daily cadence and the existing 10,000-row run cap.
7. `lifecycle_audit`: lifecycle_audit_events.occurred_at + 180 days.
8. `completed_media_retention`: media_deletion_jobs.completed_at + 180 days.
9. `media_orphan`: a full successful reconciliation establishes
   last_completed_at and next_due_at = last_completed_at + 7 days. An incomplete
   cursor, failure, deletion retry, or run-cap hit is backlog and blocks
   receipt. Because object candidates become eligible at 48 hours, next due is
   also no later than the earliest known candidate creation + 48 hours. If
   current storage metadata cannot provide that timestamp and complete-list
   proof, this category cannot authorize suppression; run the weekly command
   before stop.
10. `rds_restart_guard`: external controller time, no database table. Wake is
    strictly earlier than the AWS seven-day forced-start boundary. It is folded
    into earliest_wake_at but never represented as completed database work.

## Code-verified category sources

- `apps/server/sql/privacy_retention.sql` supplies GetIdempotencyExpiryBacklog,
  GetSessionMetadataBacklog, GetLifecycleAuditBacklog, and
  GetCompletedMediaJobsBacklog. Its worker source is
  `internal/privacyretention/worker.go`; its fixed caps and OAuth cleanup are
  defined there.
- `apps/server/sql/media_cleanup.sql` supplies GetMediaDeletionQueueState,
  GetMediaOrphanSweepCursor, live-reference classification, lease completion,
  retry, and cursor writes. `internal/mediacleanup/worker.go` and reconcile.go
  own the 30-second lease, five-second I/O, 24-hour target, 48-hour object age,
  and weekly reconciliation behavior.
- `apps/server/sql/queries.sql` supplies ClaimAuthEmailJobs,
  RequeueExpiredAuthEmailLeases, the three bounded password/mail cleanup
  queries, OAuth expiry deletes, and idle-client eligibility. The mail timing
  source is `internal/authmail/worker.go`.
- New aggregation queries may combine these indexed predicates to compute exact
  next due. They may not change a predicate or infer completion from a command
  heartbeat. Schema/query authors must map every category field to these names
  in tests and stop if final code adds another due-work source.

## Atomic no-writer proof and lock order

Use one fixed 64-bit advisory key `aboutme.runtime-write-barrier.v1`.

1. Every application, mail, maintenance, migration, lifecycle-command, and proof
   transaction that mutates database state enters through the central WriteTx
   API, which first takes the shared transaction advisory lock and checks the
   gate before calling application code with transaction-bound Queries. This
   happens before public_state, user, resume, slug, job, transition, admission,
   or category locks. BEFORE STATEMENT triggers only assert that entry already
   occurred and immediately reject an uninstrumented writer; they never acquire
   or wait for the barrier. This new outer lock changes none of the existing
   inner order: ADR 0022 keeps public_state, discovery, then ascending resume
   UUID; ADR 0016 keeps user, idempotency record, then usage. Normal writers
   never lock runtime_write_state before their existing rows; the private
   runner's finish update is last.
2. The runner calls runtime_finish_write and immediately commits. The deferred
   constraint trigger asserts completion without changing state. Forced early
   constraint checks fail before finish, including when recovered through a
   savepoint. A transaction rollback advances nothing. TRUNCATE is revoked from
   app and maintenance roles. Migration/bootstrap records a generation before
   its commit and cannot overlap receipt issue. Owner tables have explicit
   nonrecursive handling. The protected Goose version INSERT is the sole
   post-finish bookkeeping statement, as specified in
   [transaction entry](transaction-entry.md#migration-policy).
3. create_quiescence_snapshot takes the exclusive transaction advisory lock,
   then runtime_capacity FOR UPDATE, runtime_write_state FOR UPDATE, replica
   rows, public transitions, claims/leases, runtime_job_schedule rows in
   category order, and each source query in the fixed order above.
4. It requires public admission closed; no joining/active/draining/terminating
   replica except the calling drained maintenance replica; no closing or
   unresolved transition; no running shared claim; no leased mail/media row; all
   callbacks joined locally; zero due/backlog category; and the 24-hour
   accepted-writer tail. It commits terminal left plus the snapshot in the same
   transaction. The irreversible local admission latch is set before the call.
5. The controller terminates the exact EC2 instance, records its audit proof,
   and finishes desired capacity/partition state. Each is an ordinary tracked
   write and invalidates the snapshot generation. No stop receipt exists yet.
6. Independent Fargate lifecycle-command calls finalize_stop_receipt. It takes
   the exclusive barrier first, then the same row/source order. It requires the
   exact left snapshot, EC2 proof, runtime capacity in stopping with admission
   disabled and all rate allocation disabled, no nonterminal replica,
   transition, claim, lease, due row, backlog, or changed category, and the
   accepted-writer tail. It recomputes category children rather than trusting
   the old generation.
7. As final DML, finalize_stop_receipt changes write_gate closing-to-closed,
   advances generation exactly once, and inserts the receipt with that resulting
   generation. Deferred tracking coalesces with the explicit advance. Receipt
   tables are immutable/exempt, so creation does not self-invalidate.
8. Once closed, every normal mutation trigger rejects before existing row locks.
   No database metadata write is allowed between receipt commit and
   StopDBInstance. The controller may read the receipt, update its S3 phase
   without changing controller generation, and emit external metrics. It rereads
   gate plus generation immediately before StopDBInstance.
9. On the next RDS start, lifecycle-command first calls begin_wake under the
   exclusive barrier. It advances generation, invalidates the old receipt, and
   changes closed-to-closing. It opens the gate only after migrations,
   reconciliation, due-work planning, and replica admission prerequisites are
   ready. No app or maintenance writer can bypass closing.

## Write paths that invalidate a receipt

The assertion-trigger set covers all INSERT/UPDATE/DELETE/TRUNCATE on users,
identities, sessions, oauth_transactions, oauth_clients,
oauth_authorization_codes, oauth_grants, oauth_tokens, password_credentials,
password_registrations, password_reset_tokens, auth_email_jobs, resumes,
slug_tombstones, idempotency_records, idempotency_usage, public_state,
media_deletion_jobs, lifecycle_audit_events, privacy_sweep_state, public
transition/ack/result tables, runtime replica/capacity/leave/termination/fence
tables, runtime_job_schedule, distributed rate rows, and shared heavy-work
claims. runtime_write_state is the nonrecursive target. Snapshot and
stop-receipt parent/children are immutable products written only by their
exclusive functions and are explicit trigger exemptions. Schema tests compare
this allowlist with every mutable public table and fail when a new table lacks a
trigger or documented owner-table exemption.

External object creation/deletion is covered through its database candidate,
reference, queue, or run result. Unknown object-write outcome prevents receipt
until compensation or a complete orphan reconciliation proves the category. S3
control-object writes change controller generation and invalidate the prior
receipt independently.

## Wake lead and stop decision

- Define measured_start_bound as the hosted p99 bound for RDS available plus EC2
  registration, daemon pairing, runtime replay, and maintenance readiness. Phase
  activation must measure and accept this bound; cost sensitivity is not a
  timing guarantee.
- `wake_lead = measured_start_bound + 30 minutes maximum command duration + 5 minutes reconciliation margin`.
  Schedule start at or before `earliest_hard_deadline - wake_lead`. Also run the
  hourly controller heartbeat and the pre-seven-day guard. A missing measurement
  or a nonpositive margin disables suppression.
- If earliest_wake_at occurs before the next controller heartbeat, schedule a
  one-time generation-bound wake. Duplicate delivery is safe; the controller
  rereads receipt and generation. It never rounds a deadline to daily/weekly.
- Startup overrun, job failure, backlog at a run cap, changed generation,
  ambiguous receipt, or insufficient time to finish keeps RDS running and
  alerts. Optional UAT time yields first. No privacy, deletion, mail, 24-hour,
  daily, weekly, or seven-day obligation is skipped.
- A suppressed hourly/daily evaluation emits its normal generation-bound
  heartbeat with result `proved_empty`; it does not falsely advance a job's
  successful-run watermark. Weekly orphan due always starts RDS and a replica
  because database state alone cannot prove the bucket listing is empty.

## Failing-first proof cases and commands not run

- Writer enters before receipt but commits late; writer attempts entry after
  exclusive lock; SELECT FOR UPDATE after entry; direct SELECT FOR UPDATE before
  entry then DML rejection; deferred-trigger failure; uninstrumented new table;
  rolled-back writer; exactly 24-hour boundary; backward/forward database-clock
  anomaly.
- One adversarial case for every category: due exactly now, next due just inside
  wake lead, backlog/run cap, active lease, failed/ambiguous run, orphan cursor,
  unknown object outcome, and seven-day boundary.
- Paused maintenance process after local work, after left receipt, and before
  EC2 termination; ECS STOPPED without EC2 proof; proof writer concurrent with
  charged mail/media claim; stale S3/DB generation and duplicate wake.

Commands NOT RUN: make sqlc-check server-test-db server-test-integration
server-migration-test; under apps/server, targeted go test -race -count=1 for
store, privacyretention, mediacleanup, authmail, lifecycle, and controller
packages. Root alone runs make ci and connected make scan.

## Unresolved implementation evidence

- Hosted measurements must supply measured_start_bound. Until then no-op
  suppression is disabled and RDS remains available.
- The orphan source must expose trustworthy object creation time during a full
  list. If it cannot, suppression requires a full successful orphan run before
  every stop. This is conservative and does not weaken the weekly obligation.
