# 0026: One serving replica now; a designed path to a second

Status: Accepted (2026-09-06, 2026-09-08, 2026-09-17).

## Context

The publication coordinator, render authority, SSE hubs, and admission limits
hold process-local state. A second serving replica without shared coordination
could bypass revocation or multiply a limit. A coordination runtime for two
replicas was designed and partly built, but the rest of the work was larger than
the part completed, and almost all of it existed to make a second replica safe.
The product has no need for that capacity yet. A second replica would add
survival of a node failure and deploys without downtime, which are worth having
but not at the cost of blocking a launch.

## Decision

Production runs one serving replica. The growth path is a larger instance before
a second replica. Deployment configuration, not the database, holds the service
at one replica; raising the maximum above one requires the deferred work below
first.

With one replica these process-local mechanisms are correct: publication fences
and revocation leases (ADR 0010), render jobs and one-use print capabilities
(ADR 0011), SSE hubs and subscriber queues, rate limiters (ADR 0007), and the
view-count dedupe and token keys (ADR 0022).

New exclusive queued work uses `SELECT ... FOR UPDATE SKIP LOCKED`, which is
sufficient when a worker may die, because an aborted transaction releases the
row without any fencing step.

A second replica needs the accepted
[scaling design](../design/scaling/README.md) to return as new migrations with
explicit grants (ADR 0005), in this order: the runtime schema, membership, and
termination proof; the public transition operations; live entry; caller
integration for publication, render, realtime, and rate-limit callers; then
composition and readiness. Its core rules:

- PostgreSQL is the shared application coordinator. Each node runs one complete
  Caddy, Go with Chromium, and Nuxt replica in separate task cgroups.
- Publication transitions record ordered targets, required replicas,
  acknowledgements, and committed outcomes. Non-draining transitions close new
  admission while earlier work may finish; revoking and discovery transitions
  cancel and drain. A missing acknowledgement fails the mutation before business
  SQL within the five-second bound, and business changes and terminal results
  commit together.
- A graceful replica records an irreversible `left` receipt after its work
  joins. An unresolved replica keeps its membership and claims until an
  independent proof verifies its exact instance is terminated. That proof
  permits reclamation for a fresh attempt; it cannot acknowledge an earlier
  transition or revive a timed-out mutation.
- Render snapshots, capabilities, and completion authority stay in the
  initiating Go process. Shared claims enforce one running and eight waiting
  render jobs across the fleet, and paired node-local routing keeps one-use
  print redemption and the 20-second job deadline.
- Rate policies use shared PostgreSQL time and state with the same algorithms,
  overflow, debt, and scopes. Both outer API route chains share one 300 per
  minute client-IP budget.
- SSE keeps PostgreSQL revision notifications, local queues, and
  reconnect-and-refetch repair without a new wire event.
- View counting needs a shared dedupe and nonce store.
- The database connection envelope is 60 of at least 100 connections: two
  12-connection application pools, five four-connection auxiliary allowances,
  and 16 reserved for administration. Spare capacity grants no extra node or
  concurrent worker.
- Public HTTP and SSE schemas, mutation ordering, CSRF and session rules,
  privacy deadlines, and print capability authority do not change.

## Consequences

- Deploys briefly interrupt service, because one replica cannot hand over. That
  is the main thing a second replica would buy.
- The schema holds no coordination tables; migrations run as a deploy step (ADR
  0005), so no in-fleet migration protocol exists.
- The scaling design's pages state which parts describe built behavior (the
  policy catalog) and which wait for a second replica.

## History

- Former ADR 0034 (2026-09-06): an ALB and one to two autoscaled replicas, with
  scheduled UAT between test windows. Replaced by 0036 and 0037.
- Former ADR 0035 (2026-09-06): shared replica coordination and a recoverable
  UAT lifecycle (EventBridge, Step Functions, S3 lock, Fargate proof tasks). Its
  coordination contracts are the design above; its UAT lifecycle and installed
  runtime were removed by 0037 and 0038.
- Former ADR 0036 (2026-09-08): one serving replica, migrations as a deploy
  step, wake and protected-migrator work retired, the rest deferred. Its "keep
  the runtime installed" clauses were replaced by 0038.
- Former ADR 0038 (2026-09-17), in part: removed the coordination runtime
  (migrations 00013 to 00023), so rate limits returned to process memory until a
  second replica.
