# Replica lifecycle operations

Status: Accepted under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). Local
implementation and hosted proof remain Phase 10 gates.

This contract defines desired serving capacity, logical rate partitions, exact
activation and scale changes, and the private maintenance wake path.
[Lifecycle replay](lifecycle-replay.md) defines operation identity, action
order, and retry results. [Wake operations](wake-operations.md) fixes fresh wake
predicates and historical results; [wake migrations](wake-migrations.md) fixes
the protected closing-gate migration path.

## Capacity model

runtime_capacity has exactly singleton=true. desired_replicas stays 1..2 even
while external UAT ASG is zero. generation and controller_generation are
positive. controller_operation_id is 1..128 printable ASCII.

Every fresh successful lifecycle action increments capacity generation and
controller generation exactly once in its transaction and stores both resulting
values in its immutable step result. This includes prepare-scale-out,
activation, prepare-scale-in, finish-scale-in, prepare-maintenance-drain,
begin-replica-termination, begin-wake, and complete-wake. Exact replay, rejected
calls, and rollback increment neither. Lifecycle-command changes capacity state
only through these definer functions and has no direct table DML. Each fresh
action writes operation_id to both updated_by and controller_operation_id;
replay preserves them. The owner-only runtime_sample_lifecycle_time() samples
clock_timestamp once after locks, with the clamp and isolated test seam fixed by
the [ordinary operation contract](lifecycle-controller.md).

Fresh begin-wake and complete-wake also each increment
runtime_write_state.generation exactly once. Each requires the exact expected
write generation and returns the resulting write generation and gate. The
write-state update is the transaction's last state mutation, samples database
time after its locks, and clamps updated_at against its prior value. Neither
wake action changes last_accepted_writer_at. Replay returns the historical
stored gate and generation and changes no row.

Rate partitions are logical fleet capacity and never belong to a physical
replica. Flags are not continuously derived from active count. Offline,
stopping, and maintenance-only wake keep both disabled. First serving activation
enables partition 1; second enables partition 2. Failure and proof preserve the
flags, including when both serving nodes are fenced. Replacement changes no
flag. Proved scale-in disables partition 2, and final shutdown disables both.
Debt is retained.

## Installation seed

The migration inserts exactly one runtime_capacity row for staged local use:

| Field                          | Seed                      |
| ------------------------------ | ------------------------- |
| singleton                      | true                      |
| desired_replicas               | 1                         |
| generation                     | 1                         |
| controller_generation          | 1                         |
| controller_operation_id        | `bootstrap-uncomposed-v1` |
| admission_enabled              | true                      |
| lifecycle_phase                | `online`                  |
| partition 1 allocation enabled | false                     |
| partition 2 allocation enabled | false                     |
| updated_by                     | `bootstrap-uncomposed-v1` |

The migration samples database time once for updated_at. The bootstrap evidence
is a seed label, not a lifecycle operation, so it creates no operation parent,
action, replica, intent, receipt, or proof. Existing native callers remain
usable because shared membership/admission composition is still absent. New
shared claim and rate paths remain unusable without an active replica, even
though the staged capacity row is online. Production initial-serving may later
activate from this online zero-replica state and enable partition 1.

A clean closed UAT state is not a different migration seed. It is reached only
by the accepted final-shutdown transaction and receipt: write gate closed,
lifecycle offline, admission false, and both partitions disabled. A later UAT
cycle must use begin-wake and complete-wake; migration must not fabricate that
history.

## Lifecycle SQL

runtime_prepare_scale_out(expected_controller_generation, operation_id) ->
runtime_capacity_result

Lock public_state then capacity. Require online, admission enabled, desired=1,
exactly one active serving replica, logical partition 2 disabled, no unfenced
terminating replica, and no closing/unresolved transition. Set desired=2 but do
not enable partition 2 or change active membership.

runtime_activate_replica_capacity( expected_controller_generation, operation_id,
replica_id, instance_id, container_instance_arn, caddy_task_arn, go_task_arn,
nuxt_task_arn, release_digest, replaced_replica_id uuid) ->
runtime_replica_result

Lock public_state, capacity, target replica, then shared_rate_partitions in
numeric order. Require exact joining+join_ready tuple, lifecycle online, no
closing/ unresolved transition or unfenced terminating replica. For serving
kind, require active serving count below desired; activation of the first
serving replica under initial_serving or uat_serving_wake requires both
partitions disabled, then atomically enables partition 1. Retained enabled
partition 1 after node loss requires exact fenced-predecessor replacement.
Activation of the second serving replica enables partition 2. A replacement
requires an exact fenced, previously unused replaced_replica_id and changes
neither logical flag. This permits one replacement after one-node loss and two
separately bound replacements after both nodes are fenced, while active serving
count must remain below unchanged desired. Initial and scale-out activation
require replaced_replica_id null. For maintenance kind, require no other
maintenance replica in joining, active, draining or terminating state. Retained
left/fenced maintenance rows do not block a later campaign. Require
replaced_replica_id null; do not count maintenance against desired serving
capacity or change rate partitions. Change joining->active. Only a serving
result may later contribute to Caddy readiness; commit observation, not this
return alone, permits that readiness.

## Private maintenance wake

Maintenance does not bypass closed write admission and does not self-activate:

runtime_begin_wake(expected_controller_generation bigint, operation_id text,
wake_mode text, expected_write_generation bigint) -> runtime_capacity_result

`wake_mode` is exactly `serving` or `maintenance`, is covered by the argument
digest, and fixes the operation parent's workflow kind. It cannot change on
replay.

1. Lifecycle-command begin_wake supplies fixed mode `maintenance`, holds the
   exclusive barrier, invalidates the stop receipt and moves closed->closing.
   Protected migrations use ApplyWake. Reconciliation and due-work planning
   before complete are read-only; due-work writes wait for the open gate.
2. The lifecycle-command wake-completion step opens the write gate and sets
   lifecycle online/admission enabled only after those prerequisites pass. No
   ALB exists and no serving replica is active, so public traffic remains
   impossible.
3. Start one complete no-ALB EC2 node. Maintenance calls its registration and
   maintenance-ready wrappers after local probes. Lifecycle-command activates
   that exact maintenance tuple without changing desired serving count or rate
   partitions.
4. Shared-claim acquisition accepts that active maintenance replica only for C03
   mail.send. It rejects C01, C02, C04, C05 and public-transition initiation.
   Existing auth_email_jobs leases, media_deletion_jobs leases and maintenance
   advisory locks remain their separate accepted protocols; they are not shared
   claim policies. Normal write entry and gate checks apply.
5. After due work, lifecycle drains the exact maintenance tuple without changing
   desired serving count. The node joins/releases claims and records leave, then
   termination gains EC2 audit proof. Ambiguous leave retains claims until
   proof.
6. Only after no serving/maintenance replica or work remains may the separate
   UAT finalizer close the gate. R1 neither defines nor grants finalize-stop.

When due-work recomputation after complete-wake proves no node work, steps 3-5
are omitted. The existing finalizer may close the gate with zero replicas. This
branch does not start an unnecessary maintenance node.

Booked UAT serving uses the same wake functions with mode `serving`: begin-wake,
complete-wake, then one complete serving replica registers, marks ready, and is
activated while the ALB remains absent or unready. Target registration and Caddy
readiness occur only after activation. It never activates a maintenance replica.
Production initial-serving remains separate and may activate from an already
online lifecycle without begin-wake.

runtime_complete_wake( expected_controller_generation bigint, operation_id text,
expected_write_generation bigint) -> runtime_capacity_result

This lifecycle-only dependency locks public_state then capacity and the
write-state row under the exclusive wake barrier, checks the begin_wake step and
prerequisites, then opens the gate and sets lifecycle online/admission enabled
atomically. It does not register a replica, create a claim, attach an ALB or
make public readiness true.

runtime_prepare_maintenance_drain( expected_controller_generation bigint,
operation_id text, replica_id uuid, instance_id text, release_digest text) ->
runtime_replica_result

Require exact active maintenance kind and no other maintenance drain; change
only it to draining. It does not require desired=2 and does not change desired
or rate partitions. After leave and exact physical termination, proof is the
terminal membership evidence; no scale-in capacity finish is needed because
maintenance never consumed serving desired capacity.

runtime_prepare_scale_in( expected_controller_generation, operation_id,
replica_id, instance_id, release_digest) -> runtime_replica_result

Lock public_state, capacity and exact active serving target. Require desired=2,
exactly two active serving replicas, both logical partitions enabled, no
transition closing/unresolved, no prior intent, and no other drain. Either
physical replica may be the exact target. Change only it active->draining and
set draining_at. Admission adapters reject that replica after the row lock
commits. Do not reduce desired or disable partition 2 yet.

runtime_begin_replica_termination( expected_controller_generation, operation_id,
request_id, replica_id, instance_id, release_digest, reason) ->
runtime_replica_result

Lock public_state, capacity, target replica and intent. Accept joining, active
or draining; exact replay only. Insert immutable intent and change to
terminating. It never cancels, reopens, releases claims, changes desired,
enables a partition, or creates leave evidence. `reason` is exactly one of:

- `startup_failed`: the joining incarnation failed bootstrap, trio validation,
  join probes, or bounded readiness before activation. It is valid only from
  joining.
- `readiness_failed`: an active incarnation failed the accepted bounded
  node-specific readiness path. It is valid only from active and only after the
  controller's fleet-wide RDS-unavailable suppression has ruled out shared
  database outage.
- `drain_failed`: the exact prepared draining incarnation could not produce or
  resolve its graceful leave receipt. It is valid only from draining and
  requires the matching prepare-scale-in or prepare-maintenance-drain step.

The reason is immutable audit classification and is covered by the operation
argument digest. It grants no termination or fencing authority. ASG hook
timeout, controller outage, or already-terminated discovery may produce exact
EC2 proof without an intent; they do not invent another reason. No caller text
is stored.

runtime_finish_scale_in( expected_controller_generation, operation_id,
replica_id, instance_id, release_digest, leave_operation_id,
fencing_evidence_id) -> runtime_replica_result

Lock public_state, capacity, target replica, leave receipt, fencing proof, then
rate partitions. Require the exact prepared target and desired two. The graceful
branch requires target left, nonnull exact leave_operation_id, and exact EC2
audit proof. The abrupt branch requires target fenced, leave_operation_id null,
exact termination intent, and exact fencing proof. After accounting for every
other incarnation, permit zero or one active serving survivor and require all
others terminal. Both branches set desired one and disable partition 2
atomically while retaining debt. This capacity reconciliation creates no leave,
ack, business result, or claim release; abrupt proof already performed its
atomic exact-replica claim cleanup under its bounded caller context. Exact
replay returns the stored historical result. A zero-survivor result permits one
later replacement activation under desired one.

## Required lifecycle cases

- Initial serving activation has no predecessor and requires desired one, zero
  active serving replicas, and no replacement identity.
- Scale-out prepares desired two before activating the second serving replica.
- Replacement activation names an exact fenced predecessor and leaves desired
  capacity and logical partition state unchanged.
- Either serving node may be the exact scale-in target. Generic desired
  reduction cannot select it.
- Maintenance activation requires completed wake, stays outside serving desired
  capacity and public readiness, and permits only C03 `mail.send` shared claims.
  Existing mail/media leases remain separate protocols.
- Lifecycle functions use the real lifecycle-command grants. R8 composes callers
  and readiness; it adds no database privilege gate.
- Booked UAT serving wake and maintenance wake use distinct stored modes. The
  maintenance no-work branch launches no replica and adds no task or capacity.
- Every fresh lifecycle action increments both generations once. Exact replay,
  rejection, rollback, and the maintenance no-node branch after complete-wake
  add no further increment.
- Scale-in converges for a left target or a fenced target, with zero or one
  survivor. Target-dead, survivor-dead, and both-dead races terminalize the
  exact prepared operation without fabricating work evidence.
- Termination intent rejects a reason/state mismatch, shared-RDS failure labeled
  as node readiness failure, drain failure without the exact prepare step, and
  changed-reason replay.
