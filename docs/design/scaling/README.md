# Replica scaling

Status: Accepted under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md), narrowed
by [ADR 0036](../../adr/0036-single-replica-launch-and-pipeline-migrations.md),
[ADR 0037](../../adr/0037-single-host-production-without-hosted-uat.md) and
[ADR 0038](../../adr/0038-single-baseline-and-plain-migrator.md). These pages
hold the accepted design that a second serving replica needs. Only the
[policy catalog](policy-catalog.md) describes built behavior.

## Current state

Production runs one serving replica on one host under ADR 0037. Deployment
configuration, not the database, holds the service at one replica. With one
replica these process-local mechanisms are correct:

- publication fences and revocation leases in `internal/publicstate`;
- render jobs and one-use print capabilities in `internal/renderjob`;
- SSE hubs and subscriber queues in `internal/realtime`;
- rate limiters in `internal/api` under
  [ADR 0018](../../adr/0018-bounded-rate-limiter.md), and the concurrency caps
  in the policy catalog.

The schema holds no coordination tables. Migrations run as a deploy step through
plain goose under a session advisory lock (ADR 0036, ADR 0038). The design below
returns as new migrations with explicit grants.

## Pages

| Page                                | Scope                                                                     |
| ----------------------------------- | ------------------------------------------------------------------------- |
| [Membership](membership.md)         | Topology, replica identity, lifecycle, termination proof, recovery, pools |
| [Transitions](transitions.md)       | Durable publication transitions, commit fence, recovery, reconciliation   |
| [Admission](admission.md)           | Fleet rate policies, shared claims, render affinity, realtime             |
| [Policy catalog](policy-catalog.md) | Current limiters and concurrency caps, and their fleet scope              |

## Out of scope

This design has no database write barrier, scheduled UAT stop receipt, wake
operation or protected migrator. Those served scheduled UAT shutdown and
migration inside a running fleet. ADR 0036 and ADR 0037 retired both uses, and
ADR 0038 removed the code. Hosted UAT returns at about 500 users under a new
decision.

## Shared rules

These rules apply to every page:

- A non-login `aboutme_runtime_owner` owns every coordination table, trigger and
  function. Functions are `SECURITY DEFINER`, set `search_path = pg_catalog`,
  qualify every object and use no dynamic SQL. `PUBLIC` has nothing. Each login
  gets `EXECUTE` on its named functions only and no direct DML.
- Fleet logins beside ADR 0038's `aboutme_migrator` and `aboutme_app` are
  `aboutme_maintenance`, `aboutme_lifecycle_command` and
  `aboutme_fencing_proof`. `db-setup` creates cluster roles; goose never creates
  or drops a role.
- Serving replicas share the app login. A private server adapter binds each call
  to the process's immutable replica tuple, and SQL compares it with stored
  membership. That comparison is consistency checking, not authentication.
- Callers never supply time. A function samples `clock_timestamp()` once after
  its locks and clamps it to stored high-water values. Generation
  compare-and-set orders events, not wall-clock time.
- SQLSTATE `55000` means unavailable: stale generation, closed state or missing
  predecessor. `AM002` means an exact-replay conflict. `AM001` means stored
  corruption or connection contamination and retires the physical connection.
  `22023` is invalid input; `42501` is the wrong login.
- Every mutation is replayable by a caller-supplied UUID. Nothing retries
  automatically. An ambiguous commit resolves only from durable evidence read on
  a fresh connection.
- Logs and metrics carry only UUIDs, enums, digests and states. They never carry
  a raw IP, email, account, token, capability, secret or resume content.
- Public HTTP, SSE, MCP and OpenAPI contracts do not change.

## Before a second replica

Raise the service maximum above one only after these steps, in order:

1. Reinstall the coordination schema and store operations as new migrations.
2. Build the public transition operations and move publication callers onto
   them.
3. Move render, realtime and rate-limit callers onto shared admission.
4. Compose membership, readiness and the lifecycle controller.
5. Prove locally a 1 → 2 → 1 transition during writes, revocation, render and
   SSE traffic, plus crash and graceful-drain cases, without weakening any limit
   (ADR 0034).
6. Decide the fleet edge and load balancer. ADR 0037 retired the CloudFront and
   ALB edge that ADR 0034 chose.
