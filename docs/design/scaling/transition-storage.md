# Public transition storage

Status: Accepted under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). This is
the four-table storage contract. Callable operations, local composition and
hosted proof remain Phase 10 gates.

[Public transitions](public-transitions.md) fixes target encoding and caller
semantics. [Transition commit](transition-commit.md) fixes the execution fence
and lock graph. [Runtime schema](runtime-schema.md) fixes ownership and write
entry. This schema adds no callable transition operation or commit capability.

## Parent identity and state

`public_transitions` stores these columns:

| Column                       | Type        | Rule                                                 |
| ---------------------------- | ----------- | ---------------------------------------------------- |
| transition_id                | uuid        | Primary key, non-nil                                 |
| initiator_replica_id         | uuid        | Required immutable membership identity               |
| initiator_instance_id        | text        | Required immutable membership identity               |
| initiator_release_digest     | text        | Required immutable membership identity               |
| operation                    | text        | Required closed operation enum                       |
| state                        | text        | Required closing, committed, rolled_back, unresolved |
| created_at                   | timestamptz | Required                                             |
| deadline_at                  | timestamptz | Required, exactly created_at plus five seconds       |
| terminal_at                  | timestamptz | Null exactly while closing                           |
| target_digest                | bytea       | Required, exactly 32 octets                          |
| terminal_error_code          | text        | Closed values in the matrix below                    |
| recovery_fencing_evidence_id | text        | Null except exact initiator-fenced rollback          |

The operation enum is `resume_update`, `resume_publication`, `resume_retire`, or
`account_retire`. A composite foreign key binds the three initiator fields to
the immutable `(replica_id, instance_id, release_digest)` membership key.
`UNIQUE(transition_id, target_digest)` supports child digest references. The
fencing evidence reference points to `runtime_fencing_proofs.evidence_id`.
Foreign keys use `ON UPDATE RESTRICT ON DELETE RESTRICT` and are not deferred.

The parent state matrix is exact:

| State       | terminal_at | terminal_error_code                                                    | Fencing evidence |
| ----------- | ----------- | ---------------------------------------------------------------------- | ---------------- |
| closing     | null        | null                                                                   | null             |
| committed   | required    | null                                                                   | null             |
| rolled_back | required    | canceled, deadline, drain_failed, ack_missing, or business_not_started | null             |
| rolled_back | required    | initiator_fenced                                                       | required         |
| unresolved  | required    | recovery_evidence_conflict                                             | null             |

Only closing may change to committed, rolled_back or unresolved. Every terminal
row is immutable. Identity, operation, timestamps and target digest cannot
change while closing. No parent delete or reopen path exists.

The fencing foreign key proves evidence exists. The later fenced-recovery
function must also prove its exact initiator tuple and fenced membership. The
schema does not infer death from a timeout or database state.

## Targets and results

`public_transition_targets` stores:

- `transition_id uuid` and `ordinal integer`, required composite primary key;
- `kind text`, required discovery or resume;
- `resume_id uuid`, null only for discovery and non-nil for resume;
- `expected_generation bigint`, required and positive;
- `class text`, required non_draining or revoking;
- nullable `result_kind text`, generation or retired;
- nullable `result_generation bigint` and `result_recorded_at timestamptz`.

The transition foreign key is immediate and restrictive. One partial unique
index permits at most one discovery target per transition. Another unique key on
`(transition_id, resume_id)` permits each resume once, with nulls distinct. No
resume foreign key is added: retirement results must survive resume deletion.

Each parent has one through four targets. The maximum follows the approved
[three-resume account cap](../product.md) plus discovery. Ordinals are
contiguous from zero. Discovery, when present, is ordinal zero and revoking.
Resume targets follow in ascending raw UUID byte order, or start at zero without
discovery.

A target result has one of these exact shapes:

| result_kind | result_generation | result_recorded_at | Allowed target |
| ----------- | ----------------- | ------------------ | -------------- |
| null        | null              | null               | Either         |
| generation  | Positive          | Required           | Either         |
| retired     | null              | Required           | Resume only    |

Closing, rolled_back and unresolved require all target results null. Committed
requires one complete result on every target. A target update may only change
null result fields to one complete result while its parent is closing. Target
identity, ordering, expected generation and class never change. No target delete
or terminal result rewrite is allowed.

Targets do not duplicate the digest. An owner-only fixed helper reads them in
ordinal order, emits the exact
[binary encoding](public-transitions.md#target-digest) and applies
`pg_catalog.sha256(bytea)`. The whole-parent assertion requires this digest to
match the parent. Tests use the published literal vector. Later begin also
validates its supplied digest; schema correctness does not depend on that
callable function.

## Required replicas and acknowledgements

`public_transition_replicas` stores:

- required `transition_id uuid` and non-nil `replica_id uuid`, composite primary
  key;
- required `snapshot_state text`, fixed to required;
- required `instance_id text` and `release_digest text`;
- required `target_digest bytea`, exactly 32 octets.

Its composite membership foreign key binds replica, instance and release. Its
`(transition_id, target_digest)` foreign key binds the parent digest.
`UNIQUE(transition_id, replica_id, target_digest)` supports ack references. All
foreign keys are immediate and restrictive. Required rows are immutable. Every
parent must have a nonempty required set at commit.

These constraints prove an exact stored incarnation. The later begin function
still snapshots active/draining membership and rejects an unfenced terminating
incarnation. Schema constraints do not reconstruct historical liveness.

`public_transition_acks` stores:

- required `transition_id uuid` and non-nil `replica_id uuid`, composite primary
  key;
- required `target_digest bytea`, exactly 32 octets;
- required `acked_at timestamptz`;
- required `local_result text`, fixed to closed;
- required nonnegative integer `revoking_count` and `non_draining_count`.

An immediate restrictive composite foreign key binds transition, replica and
digest to the required row. Identity is anchored through that immutable row;
acks do not duplicate instance and release. The later ack function compares the
private adapter tuple against the required row and current membership before
insertion. Schema constraints do not prove local close/join or deadline checks.

Ack rows cannot be updated or deleted. Exact insert replay belongs to the later
ack function; a key conflict alone does not prove matching replay.

Closing, rolled_back and unresolved may retain any observed ack subset,
including the full required set. Only committed requires full exact coverage.
Extra acks fail the foreign key. A full ack set alone grants no business
authority, including for unresolved parents with null target results.

## Owner assertions and locking

An owner deferred whole-parent assertion validates target count, order, digest,
result shape, nonempty required set and committed ack coverage. Constraint
triggers schedule it on:

- parent insertion and terminal update;
- target insertion and result update;
- required-replica insertion;
- ack insertion.

It reads final transaction state, including when forced with `SET CONSTRAINTS`.
Initial closing creation cannot commit with missing targets, missing required
replicas or a false digest. A later change schedules a new check after any
earlier forced check.

Owner BEFORE ROW triggers enforce parent, target, required-row and ack
immutability. Every child INSERT locks its parent `FOR UPDATE` and requires
closing before proceeding. Supported callers already hold the parent first, so
this lock is reentrant. A concurrent terminal update cannot admit a late child
insertion. A defective child-first owner function gets no alternative lock
order; it may wait or deadlock and must roll back.

Every table has a `runtime_assert_write_entry` BEFORE
INSERT/UPDATE/DELETE/TRUNCATE statement trigger. Each also has a separate owner
BEFORE TRUNCATE trigger that always raises `AM001`, even with valid write entry.
Bookkeeping does not extend `last_accepted_writer_at`.

These assertions contain invalid state changes. The later commit operation must
prove the private transition capability and parent lock before business SQL and
target results. The schema alone grants no execution authority.

## Unresolved evidence boundary

The only unresolved error code is `recovery_evidence_conflict`. No schema-only
function can prove it. Under valid constraints, parent and target shapes are
consistent. A caller digest mismatch, missing caller data, timeout or
unavailable database must return an error without changing durable state.

This schema grants no unresolved mutator. A later fixed resolver must identify
the exact durable mutation/idempotency evidence and prove its conflict in the
same parent-locked transaction. It may only change closing to unresolved with
the literal code; it accepts no reason string or caller assertion. Until that
resolver is defined, contradictory recovery retains closing and keeps readiness
unavailable. Committed and rolled-back results remain immutable.

## Ownership, grants and indexes

`aboutme_runtime_owner` owns all objects. Functions use
`search_path=pg_catalog`, qualified references and fixed SQL. Revoke PUBLIC
privileges and direct DML from every login role. Assertion and digest helpers
remain owner-only. This slice grants no callable transition operation and
creates no sequence or new composite result type.

Supporting indexes cover nonnull target resume IDs for overlap reads,
closing/unresolved parents for readiness, and required replica IDs for owned
transition reads. Keys provide the parent, required-row and ack lookups.
Constraint and index names are stable implementation choices tested by catalog
and exact error assertions. Runtime methods and their role grants remain those
in [public transitions](public-transitions.md) and
[transition commit](transition-commit.md).
