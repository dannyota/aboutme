# Lifecycle operation replay

Status: Accepted under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). Local
implementation and hosted proof remain Phase 10 gates.

This contract serializes lifecycle workflows and makes every controller retry
resolve to one immutable action result.

The [fixed operation error contract](claim-operations.md#roles-and-errors) uses
AM002 for supplied identity/replay conflict and AM001 for corrupted stored
argument/result digests or impossible ledger rows. Missing/out-of-order
predecessors and stale generations use 55000. Existing transition mismatch codes
remain unchanged.

## Operation schema and digest

`runtime_lifecycle_operations` has operation_id text primary key, workflow_kind
in the closed enum below, created_at database time, and an owner immutability
trigger with no delete path. `runtime_lifecycle_operation_steps` has primary key
(operation_id, action), with each text 1..128 printable ASCII; workflow_kind
fixed enum; action is one of prepare_scale_out, activate_replica_capacity,
prepare_scale_in, finish_scale_in, prepare_maintenance_drain,
begin_replica_termination, begin_wake, or complete_wake; workflow_kind is one of
initial_serving, scale_out, replacement_serving, scale_in, uat_serving_wake,
maintenance_wake, or replica_termination; expected_generation and
result_generation bigint >0; argument_digest and result_digest bytea length 32;
recorded_at database time; and the immutable result columns below. Owner
triggers make every row immutable and there is no delete path. A workflow/action
compatibility check rejects actions outside its closed sequence. operation_id
has a foreign key to the immutable parent, and each step's workflow_kind must
equal its parent.

Each step stores result_kind as capacity or replica and these canonical result
columns: nullable replica_id, replica_kind and replica_state; desired, active
serving and active maintenance counts; partition 1 and partition 2 enabled;
capacity_generation; controller_generation; controller_operation_id;
admission_enabled; and lifecycle_phase. Text fields retain their source bounds.
Wake results additionally store write_gate and write_generation; both are null
for every non-wake action. write_gate is closing for begin-wake and open for
complete-wake, and write_generation is the historical post-action value. Counts
are nonnegative smallint and generations are positive bigint. A shape constraint
keyed by action and result_kind requires every applicable field and requires
every inapplicable field null. These typed columns are the historical result
payload. No opaque JSON or owner composite datum is stored.

The nullable result matrix is exact:

| Action                    | Kind     | Replica ID/kind/state | Counts and partition flags | Write gate/generation |
| ------------------------- | -------- | --------------------- | -------------------------- | --------------------- |
| prepare_scale_out         | capacity | null                  | required                   | null                  |
| begin_wake                | capacity | null                  | required                   | required              |
| complete_wake             | capacity | null                  | required                   | required              |
| activate_replica_capacity | replica  | required              | null                       | null                  |
| prepare_scale_in          | replica  | required              | null                       | null                  |
| prepare_maintenance_drain | replica  | required              | null                       | null                  |
| begin_replica_termination | replica  | required              | null                       | null                  |
| finish_scale_in           | replica  | required              | required                   | null                  |

Every row requires capacity_generation, controller_generation,
controller_operation_id, admission_enabled, and lifecycle_phase. For
finish_scale_in, counts and flags capture desired one, zero or one active
serving survivor, the maintenance count, partition 1's preserved value, and
partition 2 disabled. The replica fields capture the exact terminal target. The
typed runtime_replica_result returns this same action-shaped subset.

The step also has nullable typed argument_replaced_replica_id. It is required
only for replacement_serving activation and null for initial, scale-out, UAT
serving, maintenance activation, and every non-activation action. A unique
partial index on nonnull argument_replaced_replica_id prevents consuming one
fenced predecessor twice. No argument or result uses JSON.

The retained ledger keys each controller step by (operation_id, action), because
one lifecycle workflow intentionally uses the same operation ID across several
controller steps. App leave receipts and fencing proofs remain their separate
immutable tables and are referenced by later controller steps; they are not
misrepresented as controller-ledger actions. The ledger stores an immutable
argument/result digest per action indefinitely; the small control plane volume
avoids a reuse hole from garbage collection. Digests are SHA-256 over one
canonical byte stream computed by SQL. The stream is the ASCII domain
`aboutme.runtime-lifecycle-step.v1`, one NUL byte, a one-byte kind (`0x01`
arguments or `0x02` result), then fields in the named function signature/result
order. Every field is encoded as one-byte type (`0x01` UUID, `0x02` int8, `0x03`
text, `0x04` boolean), one-byte presence (`0x00` null or `0x01` present),
four-byte unsigned big-endian payload length, then payload. Present UUID uses
uuid_send, bigint uses int8send, boolean is `0x00`/`0x01`, and text is exact
UTF-8. Arguments begin with operation_id, action and expected controller
generation. Results begin with operation_id, action and result controller
generation, then every stored result column in the fixed order above, including
explicit null presence. `replayed` is deliberately excluded: a fresh step stores
and returns replayed=false, while an exact retry returns the same stored result
columns and digest with replayed=true. Replay never reads current replica,
capacity, or partition rows to reconstruct an old result. The migration fixes
the [literal SHA-256 vectors](lifecycle-vectors.md), including each action and
its nullable variants; Go repeats the same bytes and vectors. SQL is
authoritative. Fresh mutation records result_generation exactly one above
expected_generation. Replay returns the stored result without mutation or
generation increment. admission_enabled is true only for lifecycle_phase=online.
updated_at and updated_by are database evidence.

## Serialization and action graph

Every function takes `expected_controller_generation bigint` and
`operation_id text`. It first locks public_state, then runtime_capacity. Only
after holding the capacity singleton does it read operation identity: it inserts
and locks the operation parent for a fresh ID, or locks the existing parent and
requires exact workflow_kind. It then reads/inserts the action row. A
nonexistent action row is never described as locked. The capacity singleton
serializes two different first actions for the same fresh ID; the loser rechecks
the operation parent after acquiring capacity and conflicts on any
workflow/action mismatch. The exact same action/arguments/result is replay. The
same workflow ID may advance through its fixed distinct actions; a repeated
action with different arguments or result conflicts. The first action for a new
workflow requires a never-used ID. Each fresh action requires exact current
generation, increments controller_generation once, and records its result. A
stale workflow cannot skip or reorder actions because each action checks its
required predecessor row and current capacity/replica state.

Intervening controller actions may advance generation between workflow steps.
The predecessor's result generation must be at most the next action's expected
generation; it need not equal it. For example, abrupt scale-in records a
separate termination action between prepare and finish. The next action still
requires the exact current controller generation and every state prerequisite.

The workflow/action map is closed:

- initial_serving: activate_replica_capacity is the sole and first action; it
  requires no active serving replica, desired one, and a null replaced replica.
- scale_out: prepare_scale_out first, then activate_replica_capacity for
  serving; activation requires that exact predecessor and a null replaced
  replica.
- replacement_serving: activate_replica_capacity is the sole and first action;
  it requires a nonnull replaced_replica_id naming an exact fenced incarnation,
  active serving count below unchanged desired, no unfinished scale-in, and no
  prior successful replacement step using that predecessor. The step stores the
  typed replaced_replica_id argument and a unique partial constraint makes each
  fenced predecessor consumable once. Separate operations may replace both
  fenced nodes while active count remains below desired; a third activation
  fails.
- scale_in: prepare_scale_in first, then finish_scale_in. The graceful result
  requires target left plus the exact nonnull leave receipt and EC2 audit proof.
  The abrupt result requires target fenced plus exact termination intent/proof
  and a null leave_operation_id. Either result stores zero or one active
  survivor after every other incarnation is terminal.
- uat_serving_wake: begin_wake with mode `serving`, complete_wake, then
  activate_replica_capacity for the one booked serving replica.
- maintenance_wake: begin_wake with mode `maintenance`, then complete_wake. When
  recomputed due work requires a node, activate_replica_capacity for maintenance
  and then prepare_maintenance_drain. When recomputation proves no node work,
  the workflow ends after complete_wake and the existing final-stop protocol may
  close the database without launching an EC2 replica.
- replica_termination: begin_replica_termination is the sole and first action.

No other first action, repeated action position, cross-kind action, skipped
predecessor or action after a terminal step is valid. Each predecessor is the
immutable step for the same operation parent. Its result generation cannot
exceed the next action's expected generation, which must match current
controller generation.

## Required replay cases

- Concurrent different first actions with one fresh operation ID serialize on
  `public_state`, then `runtime_capacity`. Exactly one creates the parent.
- A replay with the same arguments returns the stored fields with
  `replayed=true` and does not increment generation. Changed arguments or
  results conflict.
- Replay after later successful lifecycle or membership mutations returns the
  original typed stored result rather than current row values.
- Missing, skipped, repeated out-of-position, cross-workflow, and post-terminal
  actions fail without mutation.
- SQL and Go match the fixed digest vector for every action, including null
  replacement identity and nonnull replacement identity.
- Action rows and operation parents are immutable and have no delete path.
- Bootstrap creates no lifecycle operation or step. The first real action
  expects controller generation 1 and records generation 2.
- Serving and maintenance wake modes conflict on operation-ID reuse. Serving
  wake cannot activate maintenance, and maintenance wake cannot activate
  serving. A no-work maintenance branch launches no replica.
- Wake vectors include historical write gate and write generation. Each fresh
  wake action advances write generation once; replay after later writes returns
  the stored value.
- Scale-in replay covers graceful target leave, abrupt target fence, survivor
  fence, and both fenced. Every branch returns the original zero/one-survivor
  result after later recovery actions.
