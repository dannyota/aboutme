# Phase 10 infrastructure — proposed IaC and hosted UAT baseline

## Repository boundary

OpenTofu, environment configuration, and AWS deployment workflows will live in
the planned private `aboutme-infra` repository. It has not been created. The
public app retains source, local tools, and a versioned deployment contract;
normal app checks must not require private infrastructure tooling or access. The
private deployment consumes an explicit app commit and immutable image digests.
It does not silently deploy the latest branch.

The owner selected GitHub Actions `ubuntu-24.04-arm` for native deployment image
builds. The [build contract](contracts.md#build-and-runner-contract) records the
runner split, ARM64 smoke evidence, digest handoff, and private Actions billing.
Development remains on the laptop, and AMD64 browser baselines keep their pinned
architecture.

The detailed file paths below predate this split and are not dispatchable.
Before implementation, move deployment-only paths/workflows into the private
repo, keep shared application changes in the public repo, and define each repo's
independent checks and handoff. Complete Phases 6–8 first; this document records
future work, not a reason to begin deployment now.

> **Proposed baseline:** This material moved from the former infrastructure
> plan. It remains a proposed technical baseline pending Phase 9 AWS cost
> research and the final Phase 6/7/8 runtime contracts. It is not an approval of
> this topology, sizing, production resources, or secret runtime contract. Phase
> 9 must compare managed AWS services, including ECS/Fargate, RDS, S3, and SES,
> for cost and suitability. The existing ECS-on-EC2 shape is a baseline
> candidate only; rewrite any superseded task before dispatch.
>
> **Private-media correction (2026-08-12):** ADR 0019 and Draft v4 accept this
> security invariant. They supersede the earlier direct-media edge design. The
> S3 media bucket has no CloudFront origin, Origin Access Control (OAC), or
> `/assets/*` behavior. Only the Go server task can list the `resumes/` prefix
> and get, put, or delete its objects. The bucket is unversioned so deletion
> removes the bytes rather than leaving noncurrent versions outside the orphan
> sweep. Public and owner reads stay behind Go's authorization checks. Phase 9
> may revise topology and sizing, but it does not waive this private-media
> invariant.
>
> **Baseline Rev 4 (2026-08-04):** Rev 4 separates code-only/local IaC work from
> real-cloud activation. Local preparation follows Phase 7.2 and Phase 9;
> activation happens inside Phase 10 after the infrastructure local checkpoint.
>
> **Baseline Rev 3 (2026-08-02):** The earlier review amendments remain useful
> technical context. Refresh every contract against the current design and
> implementation before dispatch; no old review artifact is a dispatch
> authority.
>
> **Changelog Rev 3:** final-round amendments — custom `orp-auth-api` excluding
> viewer-supplied `Host`/`X-Real-IP`/`Forwarded` on `/api/v1/*`, while the
> custom origin header overwrites a viewer `X-Origin-Secret` (replaces
> AllViewerExceptHostHeader; pinned by a `tofu test` assertion); Task 10.15
> SSR-path end-to-end check + bridge-gateway address verification at caddy
> start; exit-map row qualified for D24 with master-plan patch Edit 3; D24
> records the design owner's SSR rate-limit keying ruling; AC-INF-002 split (UAT
> gate → AC-INF-007, noindex → AC-INF-008, both attributed "Phase 10
> infrastructure D25"); `orp-no-cookie` header-drop inheritance flagged for P5A
> in the companion notes.
>
> **Changelog Rev 2:** records the earlier boundary, media, workflow, and
> traceability amendments; its details remain proposed until the refresh below.
> The baseline retains the fail-closed origin-secret + CIDR contract with new
> test rows 11–20 (Task 10.7); corrected D6 rationale + alarmed prefix-list
> drift detector; explicit `header_up` XFF suppression; repeated-header rows;
> **D24 redesigned** (web tier leaves the host network namespace); CloudFront
> origin-request-policy column + HSTS placement + UAT access gate; origin-secret
> via regular sensitive data source; ECR moved to bootstrap; pre-migration
> snapshot + N-1-compatible code-back/schema-forward rollback; traceability rows
> ratified in the standing matrix (AC-INF-001…006, AC-OPS-015…019);
> caddy.Dockerfile moved into Task 10.7; escalations split into a dedicated
> human-owner section; shared-file serialization note.
>
> **Delivery gate:** ADR 0024 applies. Each task has one author who writes the
> failing test first and runs its affected checks. Before integration, one fresh
> phase reviewer reads the integrated diff. The integration owner runs `make ci`
> and `make scan` once at the unchanged candidate commit.

**Goal:** An OpenTofu baseline for a proposed AWS hosted UAT environment at
`uat.aboutme.vn` in `ap-southeast-1` (Singapore), with Cloudflare DNS and SES
configuration handed off by the user. The modules are intended to apply cleanly
to a **UAT** environment mirroring the production topology (VPC, ECS on EC2
Graviton with host networking for the edge/API tier and fixed ports, RDS
Postgres gp3, private S3 media reached only by Go, CloudFront + ACM us-east-1
with the
[CloudFront behavior contract](../../../design/deployment.md#cloudfront-behavior),
Caddy origin with EIP + auto-reassociation, origin-secret + prefix-list ingress,
SSM secrets with IAM scoping and rotation, CloudWatch alarms/dashboards/SNS,
scheduled retention + restore-verification jobs, arm64 image build + ECS deploy
pipeline with drain→readiness and the migration advisory-lock sequence), with
`tofu validate`/`plan` wired into CI and UAT/production differing **only by
variables**. Plus the **BLOCKING** Phase 0 security-review item: a production
Caddy configuration that derives the viewer address from CloudFront's validated
inbound chain after verifying `X-Origin-Secret`, emits exactly one canonical
client-IP header, **refuses service on empty/unset boundary configuration**, and
is proven by an end-to-end CI test with two viewers through one simulated edge
plus forged and duplicated forwarding headers.

**Base:** the then-current integrated `main` descendant after Phase 7.2 and
Phase 9. Commit `9382c86` is a historical minimum ancestry point, not an
executable base. Before dispatch, refresh this baseline against the final Phase
6.1/6.2, Phase 7.1/7.2, Phase 8, and Phase 9 cost result. The refresh must
resolve `PUBLIC_RENDER_ORIGIN` versus stale `NUXT_RENDER_ORIGIN`, password/MCP
settings, mail runtime and the SES handoff, the provider-login-disabled startup
credentials bug, MCP routes, and the UAT Basic Authorization conflict. It must
also reconcile secret handling with the mandatory current secret-skill
instructions. Use fake AMIs and local mocks during local-only work; resolve the
real AWS AMI only during authorized activation.

## Authority map

| Phase 10 infrastructure concern       | Authority                                                                                                                                                                                                                         |
| ------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Components, routes, health, failures  | [System architecture](../../../design/system.md), including [route ownership](../../../design/system.md#route-ownership), plus [ADR 0007](../../../adr/0007-unversioned-health-endpoints.md)                                      |
| Relational schema and migrations      | [Schema and migrations](../../../design/data.md#schema-and-migrations), [database and releases](../../../design/deployment.md#database-and-releases), and [ADR 0010](../../../adr/0010-goose-only-migrations.md)                  |
| Production topology, edge, trust, DNS | [Production topology](../../../design/deployment.md#production-topology), [client-IP boundary](../../../design/deployment.md#client-ip-boundary), and [CloudFront behavior](../../../design/deployment.md#cloudfront-behavior)    |
| Private media                         | [Media deployment](../../../design/deployment.md#media), [photo intake](../../../design/api.md#photo-intake), [repository boundaries](../../../design/repository.md), and [ADR 0019](../../../adr/0019-private-media-delivery.md) |
| Privacy, retention, monitoring        | [Privacy lifecycle](../../../design/operations.md#privacy-lifecycle), [resource budgets](../../../design/operations.md#resource-and-performance-budgets), and [monitoring](../../../design/operations.md#monitoring)              |
| Approval gates and deferred scope     | [Decision status](../../../design/decisions.md), including [remaining gates](../../../design/decisions.md#remaining-gates)                                                                                                        |
| Delivery order and review gates       | [Master plan](../../implementation-plan.md), [ADR 0024](../../../adr/0024-single-pass-delivery-gates.md), and [engineering standard](../../../standards/engineering.md)                                                           |
| Numeric limits and acceptance owners  | [Budgets](../../../design/budgets.md) and [traceability](../../traceability/README.md)                                                                                                                                            |

AC-INF-001…008 are Phase 10 infrastructure-owned. AC-OPS-015…019 are Phase 10
operational rehearsal-owned. Phase 10 sequencing and status live in the
[master plan](../../implementation-plan.md). These authority documents, not
companion patches or compatibility stubs, are the dispatch inputs.

**Scope boundaries (mandatory):** Local-only preparation begins after Phase 7.2
and Phase 9 research; it produces passing affected local checks and the
infrastructure local checkpoint. The user has recorded authorization for the
future UAT hostname, Singapore region, UAT resources, and Cloudflare DNS. Phase
10 infrastructure then activates UAT at `uat.aboutme.vn`; Phase 10 operational
rehearsal **exercises** it; Phase 11 **promotes**. Phase 9 must quantify budget
and choose sizing first. That result does not authorize production. The plan
performs **no production apply, no production DNS cutover, no ops drills**
(those are Phase 10 operational rehearsal/Phase 11). Deep Vietnam data-residency
work and Flutter are out of scope. The
[production topology](../../../design/deployment.md#production-topology) has no
application load balancer. The rootless-podman dev stack (`deploy/compose.yml`,
dev `CADDY_HTTP_PORT` gotcha) is untouched except the explicitly flagged shared
route-snippet extraction in Task 10.7 (which uses env-placeholder defaults so
`compose.yml` itself needs no edit). Root `Makefile`, `.github/workflows/*`, and
`.env.example` are integration-owned: Phase 10 infrastructure workers author
exact diffs and hand them to the integration owner rather than editing (Tasks
10.7, 10.8, 10.12, 10.13).

**Shared-file serialization:** `.env.example`, `docs/plans/traceability/`,
`docs/architecture.md`, the root `Makefile`, and `.github/workflows/*` are
**owner-only integration points for this phase**. Phase 10 infrastructure
workers never edit them directly, even in Phase 10 infrastructure's own
worktree; they hand the integration owner an exact diff. The refresh pass must
re-audit every consumer that landed between this plan's 2026-08-02 snapshot and
Phase 7.2.

## Plan files

This plan is split by contract and task so each worker has a bounded file set.

| File                                                                               | Contents                                                                                                               |
| ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| [`constraints.md`](constraints.md)                                                 | Environment facts (verified 2026-08-02 at `9382c86`); global constraints                                               |
| [`decisions.md`](decisions.md)                                                     | Infrastructure decisions D1–D25 that refine the design                                                                 |
| [`contracts.md`](contracts.md)                                                     | Traceability position; budget wiring; module/file structure; master-plan exit bullet → task map                        |
| [`task-01-opentofu-skeleton-bootstrap.md`](task-01-opentofu-skeleton-bootstrap.md) | Task 10.1: OpenTofu skeleton, exact pinning, bootstrap stack (incl. shared ECR)                                        |
| [`task-02-network-module.md`](task-02-network-module.md)                           | Task 10.2: Network module — VPC, prefix-list ingress, private DB subnets, EIP auto-reassociation                       |
| [`task-03-database-storage-modules.md`](task-03-database-storage-modules.md)       | Task 10.3: Database + storage modules — RDS gp3 and private S3 media                                                   |
| [`task-04-secrets-iam-module.md`](task-04-secrets-iam-module.md)                   | Task 10.4: Secrets & IAM module — SSM contract, server-only media access, scoped roles                                 |
| [`task-05-compute-module.md`](task-05-compute-module.md)                           | Task 10.5: Compute module — ECS cluster, arm64 capacity, task definitions (D24 topology)                               |
| [`task-06-edge-module.md`](task-06-edge-module.md)                                 | Task 10.6: Edge module — Caddy-only CloudFront behaviors, ORPs, HSTS, UAT gate, ACM                                    |
| [`task-07-caddy-client-ip-boundary.md`](task-07-caddy-client-ip-boundary.md)       | Task 10.7: **BLOCKING** — production Caddy client-IP boundary, fail-closed config, prod image, simulated-edge e2e test |
| [`task-08-ops-image-arm64-build.md`](task-08-ops-image-arm64-build.md)             | Task 10.8: Ops image + arm64 build workflow (registry consumed from bootstrap)                                         |
| [`task-09-observability-module.md`](task-09-observability-module.md)               | Task 10.9: Observability module — alarms (with default thresholds), dashboards, SNS                                    |
| [`task-10-jobs-module.md`](task-10-jobs-module.md)                                 | Task 10.10: Jobs module — retention interface, restore-verification, drift check                                       |
| [`task-11-dns-certificate-glue.md`](task-11-dns-certificate-glue.md)               | Task 10.11: DNS + certificate glue — `cf` apply script from OpenTofu outputs                                           |
| [`task-12-deploy-pipeline.md`](task-12-deploy-pipeline.md)                         | Task 10.12: Deploy pipeline — pre-migration snapshot, drain→readiness, rollback                                        |
| [`task-13-ci-integration.md`](task-13-ci-integration.md)                           | Task 10.13: CI integration — `tofu validate`/`plan`/test, parity, boundary job                                         |
| [`task-15-uat-activation.md`](task-15-uat-activation.md)                           | Task 10.15: Authorized hosted UAT activation (real AWS), runbooks complete, evidence                                   |
| [`exit-criteria.md`](exit-criteria.md)                                             | Distinct local code checkpoint, activation handoff, and hosted-UAT handoff criteria                                    |
| [`gates.md`](gates.md)                                                             | Escalations, interfaces, and links to the local checkpoint and hosted UAT exit criteria                                |
