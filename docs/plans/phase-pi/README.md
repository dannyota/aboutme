# Phase PI — Infrastructure as code (implementation plan)

> **Private-media correction (2026-08-12):** Draft v4 and proposed ADR 0019
> supersede PI's earlier direct-media edge design. The S3 media bucket has no
> CloudFront origin, Origin Access Control (OAC), or `/assets/*` behavior. Only
> the Go server task can list the `resumes/` prefix and get, put, or delete its
> objects. The bucket is unversioned so deletion removes the bytes rather than
> leaving noncurrent versions outside the orphan sweep. Public and owner reads
> stay behind Go's authorization checks. This correction preserves the adopted
> revision history below.
>
> **Adopted Rev 4 (2026-08-04)** by the integration owner. Rev 4 separates
> code-only/local IaC work from real-cloud activation: no AWS or Cloudflare
> mutation may occur until P9 local UAT and its independent evidence review pass
> and the human owner records AWS resource-creation authorization.
>
> **Adopted Rev 3 (2026-08-02)** after two independent Opus plan-review rounds
> plus a scoped verification pass (workspace record: `phase-pi-review-r1.md` and
> the Rev 2/Rev 3 disposition maps). Traceability rows AC-INF-001…008 and
> AC-OPS-015…019 were ratified into `../traceability/` at adoption. **Code-only
> execution is intentionally deferred until after P7B.** Real-cloud work remains
> hard-blocked on the P9 and human-authorization gate above plus the inputs
> under "Escalations pending human owner."
>
> **Changelog Rev 3:** final-round amendments — custom `orp-auth-api` excluding
> viewer-supplied `Host`/`X-Real-IP`/`Forwarded` on `/api/v1/*`, while the
> custom origin header overwrites a viewer `X-Origin-Secret` (replaces
> AllViewerExceptHostHeader; pinned by a `terraform test` assertion); Task 14
> SSR-path end-to-end check + bridge-gateway address verification at caddy
> start; exit-map row qualified for D24 with master-plan patch Edit 3; D24
> records the design owner's SSR rate-limit keying ruling; AC-INF-002 split
> (staging gate → AC-INF-007, noindex → AC-INF-008, both attributed "PI D25");
> `orp-no-cookie` header-drop inheritance flagged for P5A in the companion
> notes.
>
> **Changelog Rev 2:** addresses all 12 blocking and all 15 non-blocking
> findings of the Opus round-1 plan review (`phase-pi-review-r1.md`):
> fail-closed origin-secret + CIDR contract with new test rows 11–20 (Task 7);
> corrected D6 rationale + alarmed prefix-list drift detector; explicit
> `header_up` XFF suppression; repeated-header rows; **D24 redesigned** (web
> tier leaves the host network namespace — no risk acceptance); CloudFront
> origin-request-policy column + HSTS placement + staging access gate;
> origin-secret via regular sensitive data source; ECR moved to bootstrap;
> pre-migration snapshot + N-1-compatible code-back/schema-forward rollback;
> traceability rows ratified in the standing matrix (AC-INF-001…006,
> AC-OPS-015…019); caddy.Dockerfile moved into Task 7; escalations split into a
> dedicated human-owner section; shared-file serialization note.
>
> **For agentic workers:** execute with superpowers:subagent-driven-development,
> one task per fresh subagent, each task delivered per its ADR 0011 risk tier —
> high-risk: author TDD, then a fresh worker deriving tests from the design and
> acceptance IDs before reading the diff, then a fresh reviewer; normal: author
> TDD plus `make ci`. Tasks 1 (OIDC/IAM), 2 (instance profile/IMDS), 4
> (secrets/IAM), 6 (viewer authentication/cache invalidation), 7 (client-IP
> boundary), 10 (retention/IAM/restore concurrency), and 12 (deploy/migration
> sequence) are high-risk. Tasks 3, 5, 8, 9, 11, 13, and 14 are normal except
> when they change a named high-risk interface. Steps are `- [ ]`. Every task's
> tests are written **before** its implementation (TDD): write the failing
> check, run it and see it fail, implement, and see it pass. Then report the
> owned diff and exact check output to the integration owner; do not stage or
> commit.

**Goal:** Terraform modules that apply cleanly to a **staging** environment
mirroring the production topology (VPC, ECS on EC2 Graviton with host networking
for the edge/API tier and fixed ports, RDS Postgres gp3, private S3 media
reached only by Go, CloudFront + ACM us-east-1 with the
[CloudFront behavior contract](../../design/deployment.md#cloudfront-behavior),
Caddy origin with EIP + auto-reassociation, origin-secret + prefix-list ingress,
SSM secrets with IAM scoping and rotation, CloudWatch alarms/dashboards/SNS,
scheduled retention + restore-verification jobs, arm64 image build + ECS deploy
pipeline with drain→readiness and the migration advisory-lock sequence), with
`terraform validate`/`plan` wired into CI and staging/production differing
**only by variables**. Plus the **BLOCKING** Phase 0 security-review item: a
production Caddy configuration that derives the viewer address from CloudFront's
validated inbound chain after verifying `X-Origin-Secret`, emits exactly one
canonical client-IP header, **refuses service on empty/unset boundary
configuration**, and is proven by an end-to-end CI test with two viewers through
one simulated edge plus forged and duplicated forwarding headers.

**Base:** the then-current integrated `main` descendant after P7B. Commit
`9382c86` is the historical minimum ancestry point, not an executable base: PI
must refresh this adopted plan against the final P2B/P6A/P7A/P7B runtime shape
before dispatch, and the refresh must record that it checked ADR 0010
(goose-only migrations — no consequence expected for PI; no operative references
to the retired migration workflow remain in `phase-pi/`) and ADR 0011
(risk-tiered gates — real consequences, applied above). PI executes in its **own
isolated worktree** branched from `main`. Workers must run `git rev-parse HEAD`
and `git merge-base --is-ancestor 9382c86 HEAD` and confirm both before starting
(worktree-isolated agents have checked out stale bases before; this check is
mandatory, not advisory).

## Authority map

| PI concern                            | Authority                                                                                                                                                                                                             |
| ------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Components, routes, health, failures  | [System architecture](../../design/system.md), including [route ownership](../../design/system.md#route-ownership), plus [ADR 0007](../../adr/0007-unversioned-health-endpoints.md)                                   |
| Relational schema and migrations      | [Schema and migrations](../../design/data.md#schema-and-migrations), [database and releases](../../design/deployment.md#database-and-releases), and [ADR 0010](../../adr/0010-goose-only-migrations.md)               |
| Production topology, edge, trust, DNS | [Production topology](../../design/deployment.md#production-topology), [client-IP boundary](../../design/deployment.md#client-ip-boundary), and [CloudFront behavior](../../design/deployment.md#cloudfront-behavior) |
| Private media                         | [Media deployment](../../design/deployment.md#media), [photo intake](../../design/api.md#photo-intake), [repository boundaries](../../design/repository.md), and [ADR 0019](../../adr/0019-private-media-delivery.md) |
| Privacy, retention, monitoring        | [Privacy lifecycle](../../design/operations.md#privacy-lifecycle), [resource budgets](../../design/operations.md#resource-and-performance-budgets), and [monitoring](../../design/operations.md#monitoring)           |
| Approval gates and deferred scope     | [Decision status](../../design/decisions.md), including [open approval gates](../../design/decisions.md#open-approval-gates)                                                                                          |
| Delivery order and review gates       | [Master plan](../implementation-plan.md), [ADR 0011](../../adr/0011-risk-tiered-delivery-gates.md), and [engineering standard](../../standards/engineering.md)                                                        |
| Numeric limits and acceptance owners  | [Budgets](../budgets.md) and [traceability](../traceability/README.md)                                                                                                                                                |

AC-INF-001…008 are PI-owned. AC-OPS-015…019 are P9A-owned. PI sequencing and
status live in the [master plan](../implementation-plan.md). These authority
documents, not companion patches or compatibility stubs, are the dispatch
inputs.

**Scope boundaries (mandatory):** PI authors and locally validates IaC before
AWS authorization, then **builds** staging after authorization; P9A
**exercises** it; P10 **promotes**. Before P9 local UAT and evidence review pass
and human AWS authorization is recorded, PI performs no AWS API call, Cloudflare
mutation, bootstrap apply, ECR push, DNS mutation, staging apply, or
deployment-workflow dispatch. The plan performs **no production apply, no public
DNS cutover, no ops drills** (those are P9A/P10). Deep Vietnam data-residency
work and Flutter are out of scope. The
[production topology](../../design/deployment.md#production-topology) has no
application load balancer. The rootless-podman dev stack (`deploy/compose.yml`,
dev `CADDY_HTTP_PORT` gotcha) is untouched except the explicitly flagged shared
route-snippet extraction in Task 7 (which uses env-placeholder defaults so
`compose.yml` itself needs no edit). Root `Makefile`, `.github/workflows/*`, and
`.env.example` are integration-owned: PI workers author exact diffs and hand
them to the integration owner rather than editing (Tasks 7, 8, 12, 13).

**Shared-file serialization:** `.env.example`, `docs/plans/traceability/`,
`docs/architecture.md`, the root `Makefile`, and `.github/workflows/*` are
**owner-only integration points for this phase**. PI workers never edit them
directly, even in PI's own worktree; they hand the integration owner an exact
diff. The refresh pass must re-audit every consumer that landed between this
plan's 2026-08-02 snapshot and P7B.

## Plan files

This plan is split by contract and task so each worker has a bounded file set.

| File                                                                                 | Contents                                                                                                            |
| ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------- |
| [`constraints.md`](constraints.md)                                                   | Environment facts (verified 2026-08-02 at `9382c86`); global constraints                                            |
| [`decisions.md`](decisions.md)                                                       | Infrastructure decisions D1–D25 that refine the design                                                              |
| [`contracts.md`](contracts.md)                                                       | Traceability position; budget wiring; module/file structure; master-plan exit bullet → task map                     |
| [`task-01-terraform-skeleton-bootstrap.md`](task-01-terraform-skeleton-bootstrap.md) | Task 1: Terraform skeleton, exact pinning, bootstrap stack (incl. shared ECR)                                       |
| [`task-02-network-module.md`](task-02-network-module.md)                             | Task 2: Network module — VPC, prefix-list ingress, private DB subnets, EIP auto-reassociation                       |
| [`task-03-database-storage-modules.md`](task-03-database-storage-modules.md)         | Task 3: Database + storage modules — RDS gp3 and private S3 media                                                   |
| [`task-04-secrets-iam-module.md`](task-04-secrets-iam-module.md)                     | Task 4: Secrets & IAM module — SSM contract, server-only media access, scoped roles                                 |
| [`task-05-compute-module.md`](task-05-compute-module.md)                             | Task 5: Compute module — ECS cluster, arm64 capacity, task definitions (D24 topology)                               |
| [`task-06-edge-module.md`](task-06-edge-module.md)                                   | Task 6: Edge module — Caddy-only CloudFront behaviors, ORPs, HSTS, staging gate, ACM                                |
| [`task-07-caddy-client-ip-boundary.md`](task-07-caddy-client-ip-boundary.md)         | Task 7: **BLOCKING** — production Caddy client-IP boundary, fail-closed config, prod image, simulated-edge e2e test |
| [`task-08-ops-image-arm64-build.md`](task-08-ops-image-arm64-build.md)               | Task 8: Ops image + arm64 build workflow (registry consumed from bootstrap)                                         |
| [`task-09-observability-module.md`](task-09-observability-module.md)                 | Task 9: Observability module — alarms (with default thresholds), dashboards, SNS                                    |
| [`task-10-jobs-module.md`](task-10-jobs-module.md)                                   | Task 10: Jobs module — retention interface, restore-verification, drift check                                       |
| [`task-11-dns-certificate-glue.md`](task-11-dns-certificate-glue.md)                 | Task 11: DNS + certificate glue — `cf` apply script from Terraform outputs                                          |
| [`task-12-deploy-pipeline.md`](task-12-deploy-pipeline.md)                           | Task 12: Deploy pipeline — pre-migration snapshot, drain→readiness, rollback                                        |
| [`task-13-ci-integration.md`](task-13-ci-integration.md)                             | Task 13: CI integration — `terraform validate`/`plan`/test, parity, boundary job                                    |
| [`task-14-staging-bring-up.md`](task-14-staging-bring-up.md)                         | Task 14: Authorized staging bring-up (real AWS), runbooks complete, evidence                                        |
| [`gates.md`](gates.md)                                                               | Escalations pending human owner; interfaces PI leaves behind; phase exit criteria                                   |
