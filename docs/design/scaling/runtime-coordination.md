# Replica coordination and recovery

Status: Accepted under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). This is
the target contract. Local implementation and hosted proof remain Phase 10
gates.

## Replica registration and joining

1. Composition obtains replica ID and exact EC2/container/task/release identity
   from trusted deployment inputs. Partial or mismatched trio fails startup.
2. In one transaction lock public_state then runtime capacity, insert joining
   replica and its exact three task rows, and read closing/unresolved
   transitions without locking them. Any such row or unapproved capacity keeps
   it unready.
3. Start the transition listener and revision LISTEN connection. Read durable
   discovery generation and current affected resume revisions on demand.
4. Exercise one normal query connection, both listener/coordinator connections,
   local Nuxt render probe, Caddy pairing probe, shared admission read/write
   rollback probe, and absence of unresolved transitions.
5. App marks the exact joined replica ready but stays unready. The lifecycle
   controller calls activate_replica_capacity. That one transaction locks the
   singleton, validates desired capacity 1..2, exact instance/release/trio,
   current active count below desired, no transition/unresolved state, changes
   joining to active, and enables logical partition one or two for first or
   second serving activation. Replacement preserves the existing flags. Only
   after observing commit may Caddy readiness open.

## Capacity/topology handshake

- Scale out: lifecycle role calls prepare_scale_out to move database desired
  one-to-two under the singleton lock, without enabling partition two. It then
  asks ASG for two. The new node joins and marks ready. Lifecycle calls
  activate_replica_capacity for that exact identity; commit activates it and
  enables partition two. Caddy readiness then opens. Failed ASG creation leaves
  desired two but only one active/one partition enabled and raises
  reconciliation; it does not weaken limits.
- Graceful scale in: lifecycle prepare_scale_in checks the expected controller
  generation, names the exact active target, and makes only it draining. The
  process closes admission, joins all work, then calls finish_graceful_leave as
  its final database action. That function matches the stored replica identity,
  requires the same generation and proves no owned transition or claim. The
  caller's local invariant requires every callback/process joined before this
  call. It changes the row to terminal left and returns an immutable receipt.
  Local admission stays irreversibly closed; a restart creates a new replica
  UUID. The controller calls TerminateInstanceInAutoScalingGroup for that exact
  instance with desired-capacity decrement, while every other instance remains
  scale-in protected. The lifecycle-hook instance must match the receipt.
  Verified termination may add audit proof without changing left to fenced.
  Lifecycle finish_scale_in then checks receipt, proof, and generation and
  atomically changes desired to one and disables new partition-two ownership.
  Existing partition-two debt remains readable and enforceable.
- If drain, receipt, target selection, or termination is missing or ambiguous,
  do not infer graceful leave. Begin termination for the exact incarnation,
  terminate it without activating replacement capacity, and retain membership
  and claims until verified EC2 termination changes it to fenced. A committed
  leave receipt is rediscovered idempotently by replica ID, generation, and
  operation ID.
- Crash replacement: while old node is unfenced, capacity reconciliation cannot
  activate a replacement beyond desired/max two. Verified termination/fencing
  retires old membership, then one replacement may join and activate.
- Generic desired-capacity reduction is forbidden for scale-in. Database and AWS
  mutations are not one transaction. Every retry first describes AWS and rereads
  capacity, replica, receipt, proof, and controller generation; it then performs
  only the next convergent step. A stale generation cannot activate, terminate,
  finish scale-in, or change a partition.

## Transition begin and acknowledgement

1. Existing caller prepares its plan and idempotency inspection exactly as now.
2. In one short transaction lock public_state singleton; reject if initiator is
   not active, another overlapping closing/unresolved target exists, or any
   expected generation differs. Reject while any unfenced terminating replica
   exists. Insert transition, ordered targets, and all active/draining required
   replicas. Commit emits transition NOTIFY.
3. Each replica agent reads and verifies the target digest. Under its local
   publicstate mutex it closes each target. NonDraining seals old admission but
   does not cancel or wait existing leases. Discovery/Revoking cancels every
   applicable local lease and waits for local release.
4. After local close/drain succeeds, insert the exact ack. New local admission
   remains closed until a terminal row is observed. An agent restart rebuilds
   this state from closing rows before readiness.
5. Initiator polls acks and terminal state until the original deadline. Missing
   ack, context cancellation before business commit, or local drain timeout
   compare-and-set closing to rolled_back. It performs no business SQL.
6. Terminal NOTIFY makes every replica reopen the unchanged generation after
   rollback. On committed, every replica rereads all target result rows,
   verifies the digest, then opens each recorded generation or retires that
   resume. Delayed, duplicate or reordered terminal notices are harmless because
   the durable parent and complete results are authoritative. NonDraining old
   lease sets may finish under the existing ADR 0022 rule.

## Business commit and paused-initiator exclusion

After every required ack, begin the existing business transaction. Its first
operation selects the transition FOR UPDATE and requires state=closing, database
time <= deadline, matching initiator, digest, expected generations, and complete
ack set. The same transaction then runs the existing mutation,
media-reference/deletion-job work, generation updates, and idempotency response,
stores every target result and sets transition committed. A publish/edit writes
the new per-resume generation; rename/unpublish writes its new generation plus
the discovery result; resume deletion writes retired plus discovery generation;
account deletion writes retired for every deleted resume and one discovery
generation. These results and business rows commit atomically.

This row lock and state predicate are the execution fence. If another process
already rolled back the transition, a resumed initiator cannot run business SQL.
If the initiator transaction is open, a recovery writer waits for its row lock;
statement/lock timeout returns unavailable and leaves admission closed. It does
not guess or compensate.

Definite rollback changes closing to rolled_back in its transaction. Ambiguous
commit uses the atomic transition record as its outcome authority. Independent
[recovery](transition-recovery.md) locks the parent, validates exact identity
and, only for closing, checks unchanged expected generations without business
row locks. It then rolls back or remains unavailable. rolled_back proves no
business change from that transition committed. Response receipts govern exact
HTTP replay only. No unresolved writer is installed.

Before changing a local fence, R2 loads the fixed
[reconciliation snapshot](transition-reconciliation.md) while holding its local
apply mutex. Later generations and immutable retirement evidence survive old
notifications and restart; every current closing/unresolved blocker remains
closed. No PostgreSQL business/transition lock is held while waiting for that
mutex.

If the initiator has been fenced by exact EC2 termination proof,
lifecycle-command may roll back its closing transition through the separate
[fenced recovery function](transition-commit.md). It locks the transition parent
and validates the stored proof. It cannot acknowledge, run business SQL, change
committed results or repair unresolved evidence. This permits replacement when
no serving replica survives. Proof recording and recovery use separate
transactions; membership never waits on a transition parent.

Account deletion locks the transition parent, ordered slug advisory keys,
public_state, canonical email, registration, user, ordered resumes and current
session. It preserves the at-most-three plan attempts, reference revocation and
deletion-job atomicity. A failed fleet close consumes one attempt only as
current caller semantics specify; it never commits deletion. Private media
remains unreachable immediately after the successful reference-removal
transaction.

## Replica drain and leave

- SIGTERM or prepare_scale_in changes active to draining before Caddy admission
  closes. New HTTP, SSE, render, shared concurrency, and transition begin are
  rejected.
- An acknowledged closing transition must reach durable terminal state before
  exit. Revoking work uses its five-second deadline; render keeps its original
  20-second deadline. SSE closes within one heartbeat, and public revocation is
  stricter at five seconds. Local callbacks and Chromium are joined.
- Graceful completion records terminal left plus an immutable leave receipt. It
  releases only claims whose joined local completion is proved in the same
  transaction. Exact termination proof may be retained for physical-removal
  audit, but it does not rewrite left. Missing or ambiguous leave requires proof
  before membership or claims can be reclaimed.
- Abrupt loss, suspension, or DB partition leaves the replica required and its
  claims charged. Current mutations fail before SQL. Replacement capacity does
  not erase the old incarnation.

## Verified EC2 termination adapter

The lifecycle controller accepts replica ID plus exact instance ID and release
digest. It requests termination through the approved ASG lifecycle path, waits
for EC2 InstanceState=terminated for that same instance, then invokes the
fencing-role function with a unique evidence ID. It must reject instance reuse,
replacement-node evidence, duplicate mismatched evidence, and any lesser state.
ECS STOPPED, ALB unhealthy/deregistered, lock loss, heartbeat expiry, and ENI
state are diagnostic only. If EC2 or the fencing write is unavailable, proof is
absent and the old replica stays required/charged.

After proof, a reconciler may mark old active/draining membership fenced and
release its concurrency claims. It cannot create an ack for an old transition or
revive a timed-out mutation. A later caller prepares a fresh plan and new
transition. This recovery can take minutes; safety intentionally wins.

## Shared-database outage and same-incarnation recovery

Loss of the transition listener, coordinator session, or ordinary database probe
moves the process from healthy to quarantined. Quarantine synchronously closes
new HTTP mutation/public/SSE/render/shared-claim admission, cancels and joins
public leases, SSE streams, render callbacks and Chromium, and fails Caddy
readiness. Liveness stays healthy while the process can enforce quarantine, so
an ordinary shared RDS outage does not restart all nodes.

The same replica ID may return through replaying to healthy only when all are
true: the database is authoritative again; its replica row is still active;
there is no termination intent or fencing proof; the lifecycle controller has
not begun EC2 termination; every canceled callback/process has joined; both
LISTEN sessions are re-established; all closing and terminal transitions since
the last durable cursor are replayed; local fences are rebuilt from current
durable generations/results; shared admission and paired Caddy/Nuxt probes pass.
Replay runs with admission closed. It writes a replay generation/cursor before
readiness opens. Delayed notifications after replay reread durable state.

If state is draining, recovery may finish drain but cannot reopen admission. If
state is terminating or fenced, or a termination intent exists, recovery closes
the process and never reopens Caddy. `Terminating:Wait` has no rollback edge.
Advisory-session reacquisition is diagnostic only and grants no recovery by
itself.

## Controller suppression contract

- Caddy exposes separate liveness and readiness. Database/coordination failure
  fails readiness but not liveness after quarantine completes.
- A fleet-wide RDS-unavailable signal suppresses readiness-driven node
  replacement and ASG lifecycle termination. Nodes remain quarantined while RDS
  recovers; no restart loop is allowed.
- For an isolated replica while RDS and another replica are healthy, the
  lifecycle controller allows a bounded replay grace. After it durably inserts
  one termination intent, it terminates that exact node once. Health changes
  cannot create another intent or reopen it.
- Replacement starts only after termination is verified and fencing proof is
  recorded. Any fenced initiator's closing transitions are then recovered in
  separate transactions before replacement activation and capacity
  reconciliation. No readiness alarm directly increments desired capacity. This
  preserves maximum two and prevents churn.

## Controller generation and credentials

- One Standard Step Functions execution serializes schedule, autoscale, manual,
  and redrive mutations. Before any AWS or database mutation it acquires a
  no-expiry conditional-write generation object in the approved private state
  bucket. The object binds environment, generation, operation ID, execution ARN,
  desired state, and last completed step. It contains no credential or app data.
- There is no timed takeover. A new owner requires the prior execution to be
  terminal, or reviewed StopExecution followed by terminal observation, then an
  If-Match compare-and-swap. Redrive reuses the operation ID and rereads AWS,
  the S3 object, and runtime_capacity before each convergent step.
- While RDS is stopped, that object is the durable lifecycle authority. Starting
  RDS does not activate service. The lifecycle command first reconciles the
  object's generation with runtime_capacity by expected-generation CAS. A gap,
  stale operation, missing object, or ambiguous write keeps admission closed.
  PostgreSQL remains the authority for replica, partition, transition, claim,
  and graceful-leave state once reachable.
- Lifecycle-command and fencing-proof run as independent Fargate tasks because
  they own no app/external work claim. Maintenance stays on the complete EC2
  replica because its mail/media claims require that incarnation's death proof.
  Separate task definitions resolve separate database secrets at task start.
  Lifecycle command has only `aboutme_lifecycle_command` EXECUTE; proof writer
  has only `aboutme_fencing_proof` EXECUTE. Maintenance runs on a complete EC2
  replica under narrow existing-table grants plus new runtime/quiescence
  functions; no broad job rewrite is required. Workflow state carries receipts,
  never database credentials. Infrastructure apply has no application login.

Readiness is false for joining/draining/left/terminating/fenced state;
quarantine or replay; listener loss; unresolved transition; a closing target not
reflected locally; unavailable shared admission; failed normal query; pool below
required dedicated capacity; or failed paired Nuxt/Caddy probes. The existing
one-second readiness cache may cache failure but cannot convert failure to
success.

## Connection envelope

- Go pgx pool maximum 12 per replica, including one revision LISTEN connection
  and one transition/coordinator connection: 24 at maximum two replicas.
- Independently runnable source-RDS consumers each have max four: migrator;
  serialized EC2 maintenance/job process; lifecycle-command task; proof-writer
  task; restore/source verifier. This is 20. Step Functions serializes lifecycle
  mutations, and PostgreSQL claims serialize jobs, but proof writer remains
  independent so a crashed job claim cannot consume recovery capacity.
- Planned maximum is 24 app + 20 auxiliary + 16 administration/incident reserve
  = 60 of required max_connections at least 100. Remaining 40 is unused
  headroom, not third-node or extra-worker authority. Readiness and both LISTEN
  sessions use the app pool; infrastructure apply has no database connection.
- Restore verifier normally connects to its restored database. Four source
  connections remain budgeted for comparison and recovery overlap.
- Migration remains scale-to-zero. ASG maximum two forbids a third replacement
  node. Every constructor and task definition enforces its cap.

## Observability

Count transition begin/commit/rollback/unresolved, ack latency by replica,
five-second timeout, quarantine/replay state, stale membership, fencing
request/proof latency, proof rejection, claims blocked on fencing, listener
health, and pool use. Log only transition/replica UUID, operation enum, digest,
state, and request ID. No resume content, client IP, email, capability,
controller, or secret is logged.
