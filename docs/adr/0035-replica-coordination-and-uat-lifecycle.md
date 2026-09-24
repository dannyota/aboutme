# 0035: Shared replica coordination and a recoverable UAT lifecycle

Status: Accepted (2026-09-06), following the owner's scaling and cost approval
and delegated design review. Superseded in part by
[ADR 0036](0036-single-replica-launch-and-pipeline-migrations.md) and
[ADR 0038](0038-single-baseline-and-plain-migrator.md).

## Context

ADR 0034 permits one to two production application replicas and scheduled UAT
shutdown. The current public-state coordinator, render authority, and admission
limits contain process-local state. Adding another replica without shared
coordination could bypass revocation or multiply a limit.

Stopping UAT also requires a controller that survives its application nodes and
RDS being off. Due privacy work must still run. Neither a lost database session
nor an unhealthy load-balancer target proves that an old process has stopped.

## Decision

PostgreSQL remains the shared application coordinator. Each EC2 node runs one
complete Caddy, Go with Chromium, and Nuxt replica in separate task cgroups.
Production capacity remains one to two nodes, without a third-node deployment
surge. RDS compute remains fixed.

Publication transitions record ordered targets, required replicas,
acknowledgements, and committed target outcomes. `NonDraining` closes new
admission while earlier work may finish. Revoking and discovery transitions
cancel and drain the applicable work. A missing acknowledgement fails the
current mutation before business SQL within the existing five-second bound.
Business changes and terminal transition results commit together.

A graceful replica records an irreversible `left` receipt after its work joins.
An unresolved replica retains its membership and claims until an independent
proof task verifies that its exact EC2 instance is `terminated`. That proof
permits reclamation for a fresh attempt. It cannot acknowledge an earlier
transition or revive a timed-out mutation.

After that proof commits, lifecycle-command may separately roll back a closing
transition whose initiator was fenced. It must obtain the transition's execution
lock and validate the exact stored proof. It cannot change committed results,
repair unresolved evidence or perform business work. This closes the recovery
gap when no serving replica survives; the proof role itself cannot mutate
transitions. See
[transition recovery](../design/scaling/transitions.md#fenced-initiator-recovery).

Render snapshots, capabilities, controllers, and completion authority stay in
the initiating Go process. Shared claims enforce one running job and eight
waiting jobs across the fleet. Paired node-local routing preserves one-use print
redemption and the original 20-second job deadline.

Rate policies use shared PostgreSQL time and state. They retain their existing
algorithms, overflow behavior, debt, response mapping, and declared scopes.
Explicit per-task limits remain local. Server-sent events retain PostgreSQL
revision notifications, local queues, and reconnect-and-refetch repair without a
new wire event.

The two outer API route chains share one 300/client-IP/minute policy. Their
current independent counters can double cross-chain allowance; this corrects
that gap against the global budget. Existing bypass routes stay unchanged.
Provider starts retain one policy and their existing key shapes.

The source-RDS envelope is 60 of at least 100 connections: two 12-connection
application pools, five distinct four-connection auxiliary allowances, and 16
reserved for administration. Spare capacity grants no extra application node or
concurrent worker.

EventBridge Scheduler and Step Functions Standard run the lifecycle controller.
A conditional-write object in the existing private S3 state bucket serializes
environment changes while RDS is stopped. Ownership has no timeout takeover.
Recovery joins the prior workflow and its child tasks before transferring
ownership. Database generation checks provide a second boundary while RDS is
available.

Independent Fargate tasks perform lifecycle commands and termination proof.
Maintenance remains on an EC2 replica so its claims retain the same process
fencing rule. Runtime secrets remain Systems Manager Parameter Store
`SecureString` values under the retained KMS key.

The existing public ops image may carry pinned OpenTofu and a generic plan
validator. Private configuration, state, credentials, and deployment policy stay
in `aboutme-infra`. Routine UAT cycles generate a fresh plan, validate its
allowed resource changes, and apply that plan under the environment lock. Saved
plans are not reused against a later state. All four image builds and their
native smoke checks remain public under ADR 0033.

RDS stays available through a booked campaign and the final 24-hour application
writer tail. A final stop receipt requires drained and terminated replicas,
completed due maintenance, valid category deadlines, and a closed database write
gate. The controller may skip an empty database invocation only while that
receipt proves no work can become due before the next wake. Missing proof keeps
RDS available. Hourly controller checks and privacy deadlines remain required.

## Cost and authority

The initial planning envelope is two calendar days with 16 test hours. The
[reproducible model](../research/aws-cost/uat-lifecycle.md) forecasts USD
27.636091 before tax without free-tier allowances. It includes maintenance, a
four-hour node and RDS recovery reserve, and gross controller costs. Its task
counts, startup times, cleanup, storage, and log assumptions require local and
hosted proof before a booking is enabled.

The existing USD 30 UAT ceiling includes retained and allocated shared costs.
The controller stops optional work at USD 25 actual or USD 30 forecast. These
controls do not impose an instantaneous billing cap. Production activation
remains a separate decision requiring owner approval.

## Compatibility and review

This decision changes internal coordination, database privileges, time
authority, and UAT scheduling. It preserves public HTTP and SSE schemas,
mutation ordering, CSRF and session rules, privacy deadlines, and print
capability authority.

The [scaling contract](../design/scaling/README.md), deployment design, budgets,
operational contracts and traceability record this decision together. This
record supersedes these earlier mechanisms:

- ADR 0018: distributed time and durable ownership for fleet rate policies.
- ADR 0022: distributed publication fences and replica recovery.
- ADR 0034: executable lifecycle ownership and proved-empty UAT scheduling.

This work follows a bounded implementation plan and its local checks. Hosted
scaling and shutdown proofs remain required before deployment.

The transaction mechanism was corrected during local implementation on
2026-09-06. PostgreSQL can force deferred constraint triggers before commit. The
private write runner therefore calls an explicit finish function before commit;
the deferred trigger only asserts completion. Owner-only temporary markers,
contaminated-backend removal and framed migrator bookkeeping preserve the write
barrier. This correction changes neither deployment scope nor the approved
budget.

Local hostile-SQL probes also showed that DISCARD TEMP can remove a finished
marker after forced constraint checks. A reserved per-backend transaction
advisory guard prevents that removal from permitting another finish. Completion
locks the state row before sampling database time and preserves timestamp
high-water values. These corrections retain the same write and privacy contract.
