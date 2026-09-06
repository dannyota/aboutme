# 0034 — Scheduled UAT and production application autoscaling

Status: Accepted (2026-09-06), by the human owner's direction.

## Context

The Phase 9 comparison baseline used one ECS on EC2 Graviton host, a stable
elastic IP, fixed host ports, and no application load balancer or horizontal
scaling. It also treated UAT as a disposable environment rather than one that is
normally stopped between test windows.

The owner selected ECS on EC2 Graviton with RDS PostgreSQL in Singapore. The
production application tier must increase and decrease capacity automatically,
starting with a minimum of one and an initial maximum of two replicas. RDS
compute is sized independently and remains fixed at launch. UAT must stay within
an approved monthly ceiling by stopping between scheduled tests.

The current Go publication coordinator, render queue and redeemed capability
state, subscriber queues, and admission limits are process-local. PostgreSQL
already carries revision notifications to each server's SSE hub. Cross-replica
authorization, revocation, admission, and draining remain unproved. Running two
copies requires a contract that preserves one-use capabilities and resource
bounds as well as those invariants.

## Decision

- The target production origin is CloudFront to an internet-facing application
  load balancer (ALB) across two Availability Zones, then to ECS services on EC2
  Graviton nodes in `ap-southeast-1`. One application replica is placed per
  node. Each replica retains separate Caddy, Go, and Nuxt tasks and their cgroup
  limits.
- The production application service automatically scales from one through an
  initial maximum of two replicas and back to one. RDS PostgreSQL remains fixed,
  private, and single-AZ at launch. Automatic or persistent production shutdown
  is not authorized. The operator-approved serialized snapshot and migration
  workflow may drain application capacity to zero and must restore at least one
  healthy replica afterward.
- Nodes use public IPv4 for outbound access to avoid a NAT gateway. Node ingress
  is restricted to the ALB security group and the origin-secret boundary.
  CloudFront reaches the ALB with HTTPS. Phase 10 must resolve and prove the
  ALB-to-Caddy TLS and authentication model, source-specific forwarding chain,
  and absence of an ALB or node bypass before implementation. The design must
  account for the ALB target TLS behavior instead of assuming native target
  certificate validation.
- Phase 10 begins with a bounded distributed-runtime design and implementation
  contract. It owns shared publication-generation fences and admitted-request
  drains; account deletion, private-media deletion, and artifact-revocation
  ordering; replica-safe render jobs, one-use print capability redemption, and
  origin-replica routing; SSE fanout, rechecks, reconnect repair, and scale-in
  draining; fleet-wide rate and heavy-work limits; per-task 512 MiB bounds and
  the RDS/pgx connection budget; colocated network trust; startup readiness; and
  fail-closed handling of transient failures and ambiguous commits.
- Compute, edge, deployment, scheduled jobs, hosted harness, and activation work
  cannot assume a second replica until that contract is implemented and its
  local checks pass. A production-shaped UAT gate must prove a real 1 → 2 → 1
  transition during writes, revocation, render, and SSE traffic, plus crash and
  graceful-drain cases, without weakening global limits.
- UAT normally retains its private single-AZ RDS data plane while application
  nodes and the temporary ALB are absent. Every booked UAT running window
  creates the ALB, starts RDS and ECS, runs readiness and overdue-job checks,
  then drains ECS, terminates application nodes, removes the ALB, and stops RDS.
  Automation restarts RDS before AWS's seven-day forced restart, completes
  privacy and retention jobs before their deadlines, and stops it again. Failed
  checks or a high forecast shorten optional testing; they do not relax
  retention obligations.
- The approved planning envelope before tax is USD 20–30 per month for UAT, USD
  140–170 for production, and USD 160–200 combined. UAT uses a conservative USD
  30 monthly operational ceiling, with alerts at USD 20 and USD 25 and a USD 30
  forecast stop. Paid GitHub Actions usage remains disabled. These amounts do
  not authorize production activation or a plan purchase.

## Supersession

This decision supersedes single-host-only placement and routing, the stable-EIP
origin, and the no-load-balancer clauses in the deployment design and Phase 10
infrastructure baseline. Fixed ports may remain inside one colocated replica if
Task 10.18 proves its placement and isolation. It does not claim that the
current application or infrastructure code already implements replica safety,
autoscaling, or scheduled UAT.

ADR 0031 still owns the Singapore region, UAT and Cloudflare authorization, and
the separate Phase 11 production approval. ADR 0033 still owns public image
builds and private publication and deployment.

## Consequences

Phase 10 must update its infrastructure contracts before dispatch. The temporary
UAT ALB cannot accrue a full idle month. RDS storage, keys, state, ECR images,
and required logs remain after application nodes terminate. The cost model may
keep a full-month 30 GB root-disk and public-IPv4 reserve as conservative
headroom; it does not claim those resources persist after normal termination.
Any orphaned volume or address requires cleanup. Schedules reconcile ECS desired
capacity and Auto Scaling group capacity so host replacement cannot turn a
stopped environment back on.

The one-host deployment remains useful only as historical comparison context and
an interim UAT implementation if it is never represented as the production
target or as scaling evidence. Production launch remains Phase 11 and requires
separate owner approval.
