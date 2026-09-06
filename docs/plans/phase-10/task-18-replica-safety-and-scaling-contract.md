# Task 10.18 — Replica safety and scaling contract

Complete this task before dispatching any Phase 10 work that wires compute, edge
routing, jobs, deployment, hosted tests, or activation. The task produces an
approved detailed runtime design, converts that design into bounded
implementation tasks, implements them locally, and proves their checks before a
second application replica can receive traffic.

## Authorities and current gaps

[ADR 0034](../../adr/0034-scheduled-uat-and-production-autoscaling.md) and the
[AWS deployment design](../../design/deployment.md#production-topology) own the
target. Current behavior combines shared database state with local coordination:

- `publicstate.Coordinator` owns publication generations and request leases in
  one Go process.
- `renderjob.Queue`, pending jobs, controller handles, and redeemed one-use
  print capabilities live in memory.
- PostgreSQL revision notifications reach each server's owner and public
  server-sent event (SSE) hub. Subscriber queues and admission are local;
  cross-replica authorization, revocation, and draining remain unproved.
- request, route, concurrency, and heavy-work limiters are per process.

Replica count two is therefore a failure condition until this task supplies and
implements the shared contracts. Infrastructure work must not invent the missing
application protocol.

## Design output

Write a focused design and its implementation-task split before changing runtime
code. The design must choose exact state ownership, transaction boundaries,
identifiers, expiry rules, retry behavior, and observability for:

1. Shared publication-generation fences and admitted-request draining. A state
   change must wait for every request admitted under the old generation across
   all replicas. Account deletion, private-media reference removal, public
   artifact revocation, and discovery-generation changes retain their existing
   ordering and five-second bounds.
2. Transient failure and ambiguous commit handling. A replica must fail closed
   when shared coordination is unavailable. Retries cannot turn a possibly
   committed mutation, capability redemption, artifact completion, or object
   write into a duplicate or unsafe compensation.
3. Replica-safe render jobs and one-use print capabilities. Choose either shared
   claims or strict origin affinity. Define terminal digests, redemption
   atomicity, controller authority, crash behavior, retry bounds, and how Go
   reaches the Nuxt origin paired with the job. Preserve the 20-second bound and
   never continue a job automatically past its deadline. No job ID alone grants
   completion authority.
4. Fleet-wide SSE behavior. Preserve the PostgreSQL revision transport and prove
   or complete authorization and generation rechecks, bounded per-client queues,
   reconnect-and-refetch repair, replica crash behavior, fleet admission, and
   graceful scale-in draining within heartbeat and revocation deadlines.
5. Fleet-wide rate, concurrency, and heavy-work limits. Preserve per-account,
   per-client-IP, and globally scoped heavy-work or concurrency bounds across
   the fleet. Each budget row keeps its declared scope: limits explicitly scoped
   per task or limiter instance, including 2,000 SSE connections per task, 512
   MiB per Go task, and 10,000 limiter keys per instance, remain per task or
   instance. Define overload behavior and shared-admission failure modes.
6. Connection and memory budgets. Each Caddy, Go, and Nuxt task keeps its own
   cgroup and each Go task remains at 512 MiB. Derive an explicit per-task pgx
   pool ceiling for two application replicas, migrators, scheduled jobs, restore
   checks, and RDS administrative reserve. Readiness fails before admitting
   traffic when required connections or shared coordination are unavailable.
7. Colocated network trust and routing. One application replica per EC2 node
   retains separate Caddy, Go, and Nuxt tasks. Define service discovery,
   placement constraints, private print redemption, frozen public-render
   routing, startup order, health and readiness, and replacement behavior
   without exposing internal routes.
8. CloudFront, ALB, and Caddy trust. Resolve HTTPS validation from CloudFront to
   the ALB and the ALB-to-Caddy TLS and authentication model, without assuming
   native ALB target-certificate validation. Define ALB health checks,
   source-specific forwarded-header parsing, origin-secret rotation, security
   groups, and tests proving direct ALB and node paths cannot bypass CloudFront
   policy.
9. Autoscaling and draining. Define metrics, cooldowns, target tracking or step
   policies, ECS capacity-provider behavior, one-replica-per-node placement,
   deregistration delay, SIGTERM handling, admitted-work drain, crash behavior,
   and rollback. The initial production capacity is minimum one and maximum two;
   RDS compute does not scale with it.
10. Scheduled UAT lifecycle. Define a state machine that creates the temporary
    ALB for every booked running window and starts RDS and ECS, checks readiness
    and overdue retention work, drains ECS, terminates application nodes,
    removes the ALB, and stops RDS. Reconcile ECS desired counts and Auto
    Scaling group minimum, desired, and maximum capacity so replacement cannot
    wake a stopped environment. Restart RDS before AWS's seven-day guard, run
    privacy and deletion jobs before their deadlines, then stop it again.

The design must name schema and API compatibility effects and generate exact,
bounded implementation tasks with owned paths, failing-first tests, dependency
order, and narrow verification commands. Update design, ADR, OpenAPI, generated
artifacts, traceability, and runbooks together when the chosen protocol changes
their contracts. A fresh phase reviewer checks the invariants above by name.

## Local proof before infrastructure wiring

- [ ] Multi-process tests prove publication and discovery fences block new
      admissions, drain old-generation work fleet-wide, and fail closed on
      coordinator loss, timeout, replica crash, and ambiguous completion.
- [ ] Render tests prove one claim, one successful capability redemption, one
      authorized terminal completion, and origin affinity across two replicas.
      Claimant death either fails the job or retries only within the original
      20-second deadline; no automatic work continues beyond it.
- [ ] SSE tests prove cross-replica delivery, authorization and generation
      rechecks, bounded slow-client behavior, reconnect repair, graceful drain,
      and abrupt replica loss.
- [ ] Rate and heavy-work tests prove each budget retains its declared scope at
      replica counts one and two, including shared-store failure.
- [ ] Resource tests enforce the 512 MiB Go task limit, separate Caddy/Nuxt
      budgets, and the derived pgx/RDS connection ceiling during deploy, jobs,
      restore, and two-replica load.
- [ ] Topology tests prove one complete replica per node, no internal-route
      exposure, correct paired render/redemption routing, startup readiness, and
      CloudFront-to-ALB-to-Caddy client-IP and origin-secret trust.
- [ ] Lifecycle tests prove stopped UAT cannot self-start through ECS or the
      Auto Scaling group, application nodes and the ALB are absent while off,
      RDS restarts before seven days, overdue privacy work blocks restop, and
      cleanup leaves only the specified persistent resources. Full-month disk
      and IPv4 reserves remain cost headroom until live inventory proves a safe
      reduction.

## Hosted gate

Task 10.14 must include a production-shaped test that drives ongoing writes,
publication and artifact revocation, account/private-media deletion, render and
one-use print redemption, and owner/public SSE while actual capacity moves 1 → 2
→ 1. It injects one abrupt replica failure and one graceful scale-in, confirms
no stale public artifact or private media becomes reachable, and proves each
limit retains its declared scope while database connections stay within the
derived fleet budget.

Task 10.15 records schedule transitions, temporary ALB deletion, RDS stop/start
guard behavior, actual persistent-resource cost, orphan cleanup, and
failed/high-forecast shortening of optional work. It never applies the UAT
lifecycle to production and never treats Phase 11 activation as authorized.

**Verification:** exact checks come from the approved implementation-task split;
the integration owner also runs the affected Go, database, web, infrastructure,
and hosted gates before Phase 10 closure.
