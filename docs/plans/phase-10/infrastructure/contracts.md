# Contracts, budgets, and structure

## Required pre-dispatch refresh

This baseline is proposed until Phase 9 cost research and the final Phase 6.1,
6.2, 7.1, 7.2, and 8 contracts are available. Before any task dispatch, record
the refresh outputs in the phase plan:

- quantified AWS cost, selected UAT sizing, and the resulting budget decision;
- a managed-service cost and suitability comparison, including ECS/Fargate, RDS,
  S3, and SES. The existing ECS-on-EC2 topology is only a proposed baseline; any
  superseded task is rewritten from the Phase 9 result before dispatch;
- `PUBLIC_RENDER_ORIGIN` versus stale `NUXT_RENDER_ORIGIN` usage, password and
  MCP settings, mail runtime, SES handoff, and the provider-login-disabled
  startup credentials fix;
- final MCP routes and the UAT Basic Authorization interaction; and
- the resolved UAT access mechanism and secret runtime contract. Neither is
  chosen silently here. Reconcile secret details with the current mandatory
  secret-skill instructions before implementation; do not handle secret values.

The user-owned [email runbook](../../../runbooks/email.md) is the SES handoff:
SES is configured in the Singapore sandbox with `aboutme-auth`,
`danny@aboutme.vn`, and the existing `aboutme-email` CloudFormation stack.
Before OpenTofu creates overlapping SES, SNS, SQS, or CloudWatch resources, the
refresh must define controlled adoption/import blocks for that stack and verify
them against the pinned OpenTofu import documentation. Preserve the Google
Workspace root MX/SPF and the separate `bounce.aboutme.vn` MAIL FROM records.
Runtime IAM is still absent and must be designed with least privilege; the
encrypted feedback queue has no consumer and must not be described as
application processing. UAT smoke uses the SES simulator; real signup/reset mail
requires owner-approved verified recipients, and production SES access waits for
public HTTPS signup/contact flows.

### Existing email ownership

The local checkpoint must choose and document one management strategy before
activation. Import alone does not transfer ownership away from CloudFormation:

1. Adopt the existing stack itself into OpenTofu while CloudFormation remains
   the sole manager of its child resources. OpenTofu must not also declare or
   import those children as individually managed resources.
2. If individual resources need OpenTofu ownership, prepare a
   resource-by-resource transfer: inventory physical IDs and dependencies,
   record the original stack template, apply and verify retention protection,
   remove the retained resources from CloudFormation management, verify they
   still exist, then import them into OpenTofu. Require a no-change post-import
   plan and a rollback procedure before any follow-on resource update. Never
   delete the email stack as a shortcut.

Use the official
[CloudFormation retention rules](https://docs.aws.amazon.com/AWSCloudFormation/latest/TemplateReference/aws-attribute-deletionpolicy.html)
and [OpenTofu import contract](https://opentofu.org/docs/language/import/) when
writing the exact transfer runbook. Only one controller may manage each
resource. The selected strategy and its non-destructive checks are required
inputs to task 10.15 and must preserve sending, feedback, and Google Workspace
DNS.

Adopt shared mail infrastructure into a separate persistent root/state, planned
as `deploy/aws/shared-email/`, outside the disposable UAT environment. Its
resource protections and UAT teardown tests must prove that the email stack,
identities, feedback queue, and associated DNS cannot be destroyed by a UAT
reset or cleanup.

Local-only tasks use fake AMI data and OpenTofu mocks. Task 10.15 resolves the
real AWS AMI only during authorized UAT activation.

The task contracts retain the exact boundary, private-media and IAM, retention
and heartbeat, image, deploy-pipeline, parity, and operational handoff checks.
The exit criteria gate their affected checks; it does not replace those task
contracts with an abbreviated checklist.

## Build and runner contract

The owner selected GitHub-hosted `ubuntu-24.04-arm` on 2026-09-05 for native
`linux/arm64` deployment images. This runner choice is settled; AWS topology,
instance sizes, and the spending limit still come from Phase 9.

| Work                                         | Execution location                                      | Owner                                     |
| -------------------------------------------- | ------------------------------------------------------- | ----------------------------------------- |
| Development and affected feature checks      | Laptop, native stack and one shared database container  | Public app repository                     |
| Existing app CI and pinned browser baselines | Existing GitHub runners; AMD64 for baseline comparison  | Public app repository                     |
| Deployment image build and runtime smoke     | Native `ubuntu-24.04-arm`, Podman, `linux/arm64`        | Private `aboutme-infra`, task 10.8        |
| Image publication and AWS UAT deployment     | Protected manual workflows after the activation handoff | Private `aboutme-infra`, tasks 10.8/10.12 |

Task 10.8 records the tested app commit, infrastructure commit, runner image
version, target platform, and the four immutable image digests. Native smoke
tests cover server/migrate, Nuxt, Caddy, and ops, including the Phase 7
Chromium, font, and export path. The browser baseline remains AMD64. Neither
QEMU nor a multi-architecture release build is required; laptop image checks use
the laptop architecture. ARM64 runtime smoke must pass on the native runner
before publication and deployment, beyond the existing local checkpoint.

Task 10.8 checks reviewed canonical candidates against protected branch and
workflow/check identities before publication. Task 10.12 verifies that
provenance and the manifest against the selected successful build run, then
deploys those digests. The integration owner archives the manifest, checksum,
source approval and successful-run/smoke evidence in a reviewed private release
record before Actions artifacts expire. Retain that record and referenced ECR
images through UAT, Phase 11 promotion, and the rollback window. Task 10.12 can
validate the protected archive after artifact expiry; it cannot recreate the
manifest from tags. Phase 11 promotes the UAT-proven images without rebuilding.
Task 10.13 checks the runner labels, event/permission boundaries, and manifest
rejection paths. These are task obligations, not new acceptance IDs or evidence
of completion.

Use caches keyed by OS, architecture, exact tool versions, and dependency
lockfile hashes. Keep ARM64 native outputs separate from AMD64 outputs and
release caches separate from untrusted pull-request caches. Cancel superseded
read-only PR checks; serialize image publication and deploys with
`cancel-in-progress: false`, so cancellation cannot interrupt a migration. Set
explicit job timeouts and artifact retention. Measure build duration and
disk/memory use before adding larger runners or parallel image builds.

GitHub's
[runner reference](https://docs.github.com/en/actions/reference/runners/github-hosted-runners)
and
[billing rules](https://docs.github.com/en/billing/concepts/product-billing/github-actions)
(checked 2026-09-05) distinguish free standard public-repository jobs from
included/paid private-repository minutes. Task 9.1 prices the private workflows
and storage. Runner labels select architecture/OS, not an immutable machine
image or AWS region; pin tools and container digests and record the actual
runner image. Follow GitHub's
[concurrency rules](https://docs.github.com/en/actions/concepts/workflows-and-actions/concurrency)
when implementing cancellation.

## Traceability position

The committed authority is direct: **AC-INF-001…008** are Phase 10
infrastructure-owned rows in [`traceability/`](../../traceability/), and
**AC-OPS-015…019** are the Phase 10 operational rehearsal-owned rows for the
live CloudFront matrix, origin-secret rotation drill, live two-runner migration,
real restore, and alarm receipt. The current sequencing and arm64 build wording
live in [`implementation-plan.md`](../../implementation-plan.md); no companion
patch file is required or present.

| Row                | Owner                                                           | This plan's obligation                                                                                                                                                   |
| ------------------ | --------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| AC-INF-001         | Phase 10 infrastructure Task 10.7                               | Fail-closed boundary + validated-chain viewer derivation + single canonical header and secret-free access logs, proven by CI e2e rows 1–22                               |
| AC-INF-002         | Phase 10 infrastructure Task 10.6                               | Caddy-only behavior matrix, with no S3 origin or `/assets` behavior, encoded and asserted by mocked `tofu test`                                                          |
| AC-INF-003         | Phase 10 infrastructure Task 10.1/10.13                         | Env parity: byte-identical roots + tfvars key-set equality, CI-enforced                                                                                                  |
| AC-INF-007         | Phase 10 infrastructure Task 10.6 (Phase 10 infrastructure D25) | Proposed UAT viewer gate (access mechanism remains a refresh output, disabled in production) — Phase 10 infrastructure-local control, no design-clause attribution       |
| AC-INF-008         | Phase 10 infrastructure Task 10.6 (Phase 10 infrastructure D25) | Blanket `X-Robots-Tag: noindex, nofollow` on UAT via the response-headers policy — Phase 10 infrastructure-local control, no design-clause attribution                   |
| AC-INF-004         | Phase 10 infrastructure Task 10.4                               | Secrets absent from repo/state; SSM paths disjoint; only the server role gets prefix-scoped media operations                                                             |
| AC-INF-005         | Phase 10 infrastructure Task 10.9                               | Alarm inventory + dashboards + SNS provisioned, incl. drift + heartbeat alarms                                                                                           |
| AC-INF-006         | Phase 10 infrastructure Task 10.10                              | Enabled privacy, media, restore, TLS, and drift schedules with exact roles, overlap locks, heartbeats, and failure alarms                                                |
| AC-OPS-001         | P0B (done)                                                      | Reused, not re-implemented (D16); live two-runner staging drill is AC-OPS-017                                                                                            |
| AC-OPS-002         | Phase 10 operational rehearsal                                  | Phase 10 infrastructure builds the mechanism (D8/D9, Tasks 10.6–10.7) and proves it in CI simulation; live bypass rejection stays Phase 10 operational rehearsal         |
| AC-OPS-015…019     | Phase 10 operational rehearsal                                  | Phase 10 infrastructure leaves the interfaces (alarm-trigger table, rotation runbook, restore job, deploy workflow); the drills are out of Phase 10 infrastructure scope |
| AC-OPS-008/009/014 | P0 (done)                                                       | Phase 10 infrastructure's task env (`TRUSTED_PROXY_CIDRS=127.0.0.1/32`, `LISTEN_HOST=127.0.0.1`, `ENV=staging`/`prod`) is the deployed instantiation                     |

## Budget wiring (numeric budgets → infrastructure parameters)

| Budget (budgets.md)                                        | Infrastructure parameter (task)                                                                                                                                                                         |
| ---------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Server task ≤ 512 MiB (Go + Chromium)                      | ECS **task-definition-level** `memory = 512` (the task cgroup — budgets.md's whole-task semantics); container-level limits left unset or equal so a container can never out-budget the task (Task 10.5) |
| pgx pool ≤ 20 < `max_connections`                          | RDS parameter group asserts `max_connections` ≥ 100 on the chosen class (Task 10.3, `tofu test` assertion)                                                                                              |
| SSE ≤ 2000 conns, ≥ 25 % fd headroom                       | `ulimits { nofile soft/hard = 65536 }` on caddy + server containers (Task 10.5)                                                                                                                         |
| SSE heartbeat 25 s < CF idle timeout                       | `caddy-sse` origin read timeout 60 s; default origin 30 s (Task 10.6, D22)                                                                                                                              |
| API/SSR p95 SLOs (Phase 10 operational rehearsal-measured) | Instance classes are tfvars (D21) so Phase 10 operational rehearsal benchmark evidence can change them without module edits                                                                             |
| Request body ≤ 256 KB                                      | App-enforced (P0 middleware); no CloudFront/Caddy override introduced                                                                                                                                   |

## Module/file structure produced by this phase

| Path                                                                            | Responsibility                                                                                                                                                   |
| ------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `deploy/aws/bootstrap/`                                                         | One-time retained state/secrets KMS roots, state bucket, exact GitHub OIDC build/plan/deploy roles + boundary, and shared ECR repos (local state)                |
| `deploy/aws/modules/network/`                                                   | VPC, public + **private (DB)** subnets, SG (443 from CloudFront origin-facing prefix list), EIP + ASG(1) auto-reassociation                                      |
| `deploy/aws/modules/database/`                                                  | RDS Postgres (gp3, PITR, write-only master password + version), parameter group, **private** subnet group                                                        |
| `deploy/aws/modules/storage/`                                                   | Private unversioned S3 media bucket, public-access block, encryption, bucket and prefix outputs                                                                  |
| `deploy/aws/modules/secrets/`                                                   | SSM name contract, retained-key input, scoped task/execution roles, server-only media and one-distribution invalidation permissions                              |
| `deploy/aws/modules/compute/`                                                   | ECS cluster, capacity provider (ASG, ECS-optimized arm64 AMI), task definitions (host-mode caddy/server, **bridge-mode web — D24**), log groups                  |
| `deploy/aws/modules/edge/`                                                      | CloudFront distribution with dual Caddy origins only, origin-request and response-header policies, **proposed UAT viewer gate — D25**, ACM us-east-1 certificate |
| `deploy/aws/modules/observability/`                                             | CloudWatch alarms (inventory below, with default thresholds), dashboards, SNS topic + subscription, EventBridge failure rules                                    |
| `deploy/aws/modules/jobs/`                                                      | Enabled bounded privacy/media jobs plus restore, TLS-expiry, and prefix-list drift; exact RunTask/PassRole/task-role boundaries                                  |
| `deploy/aws/envs/staging/`, `deploy/aws/envs/production/`                       | Thin, byte-identical roots (D4): `main.tf`, `variables.tf`, `outputs.tf`, `backend.hcl`, `<env>.auto.tfvars`                                                     |
| `deploy/aws/scripts/secrets-bootstrap.sh`                                       | After bootstrap, writes and decrypt-checks SSM SecureStrings under the retained environment key before the environment foundation apply                          |
| `deploy/aws/scripts/dns-apply.sh`                                               | Applies/diffs Cloudflare records from OpenTofu outputs via `cf` CLI (D19)                                                                                        |
| `deploy/aws/scripts/restore-verify.sh`                                          | RDS snapshot → isolated instance → verification query → teardown; overlap-guarded (D20)                                                                          |
| `deploy/aws/scripts/cidr-drift-check.sh`                                        | Compares deployed CloudFront origin-facing CIDR set vs the live managed prefix list; nonzero exit on drift (D6)                                                  |
| `deploy/aws/scripts/db-bootstrap.sh`                                            | Creates the database and exact migrator/app/restore roles and grants; idempotent and app-DDL-negative tested                                                     |
| `deploy/aws/scripts/tls-expiry-check.sh`                                        | Checks the local Caddy listener with origin SNI/hostname and emits bounded expiry/failure metrics                                                                |
| `deploy/caddy/routes.caddy`                                                     | Shared route table imported by dev + prod Caddyfiles; env-placeholder upstreams with dev defaults (D7)                                                           |
| `deploy/caddy/boundary.caddy`                                                   | Edge boundary: fail-closed origin-secret gate, trusted-chain client-IP, header strip/emit (D5/D8)                                                                |
| `deploy/caddy/Caddyfile.prod`                                                   | Production Caddyfile: `admin off`, DNS-01 TLS, internal SSR listener (D24), loopback health vhost, imports of the two snippets                                   |
| `deploy/caddy/Caddyfile.boundary-test`                                          | TLS-free wrapper importing the same snippets, driven by the CI e2e test (env-substituted listen port)                                                            |
| `deploy/caddy/Caddyfile` (modify)                                               | Dev file now imports `routes.caddy` (behavior unchanged; existing route-table test must stay green)                                                              |
| `deploy/caddy.Dockerfile` + `deploy/caddy/caddy-entrypoint.sh`                  | **Task 10.7:** xcaddy build (Caddy 2.11.4 + caddy-dns/cloudflare, pinned, arm64) + fail-closed env entrypoint guard                                              |
| `deploy/ops.Dockerfile`                                                         | Minimal arm64 image: aws-cli + postgresql-client + the ops scripts                                                                                               |
| `apps/server/internal/routetable/prod_boundary_test.go`                         | The BLOCKING e2e test (simulated edge, two viewers, forged + duplicated headers, fail-closed rows)                                                               |
| `.github/workflows/{iac.yml,images.yml,deploy-staging.yml}`                     | Authored by Tasks 10.8/10.12/10.13 as exact diffs, **applied by the integration owner**                                                                          |
| Root `Makefile` (diff for owner)                                                | `iac-fmt`, `iac-validate`, `iac-test`, `route-table-test-prod`, `staging-plan` targets                                                                           |
| `.env.example` (diff for owner — owner-serialized)                              | `CLOUDFLARE_API_TOKEN=` (Zone:DNS:Edit, `aboutme.vn` only), `AWS_PROFILE=` names-only additions                                                                  |
| `docs/runbooks/{deploy-rollback,eip-recovery,secret-rotation,restore-drill}.md` | Seeded runbooks (drilled in Phase 10 operational rehearsal)                                                                                                      |
| `docs/architecture.md` (update — **owner-serialized diff**)                     | Gains the deployed-UAT current-state section (Task 10.15, diff handed to owner)                                                                                  |

Module dependency graph:

```mermaid
graph TD
    B[bootstrap: retained state/secrets keys, OIDC roles, ECR] -.-> R[environment roots]
    R --> N[network]
    N --> D[database]
    R --> ST[storage]
    R --> E[edge and certificate]
    ST --> SE
    N --> C[compute]
    ST --> C
    SE --> C
    D --> C
    N --> E
    E --> SE[secrets and IAM]
    C --> O[observability]
    D --> O
    E --> O
    C --> J[jobs]
    SE --> J
```

## Master-plan exit bullet → task map

| Master-plan Phase 10 infrastructure exit bullet                                                                         | Task(s)           |
| ----------------------------------------------------------------------------------------------------------------------- | ----------------- |
| VPC                                                                                                                     | 10.2              |
| ECS on EC2 Graviton — host networking for the edge/API tier (web tier bridge-mode per D24), fixed ports per P0 contract | 10.5 (D24)        |
| RDS Postgres (gp3)                                                                                                      | 10.3              |
| Private S3 media, with access limited to the Go server role                                                             | 10.3, 10.4        |
| CloudFront + ACM (us-east-1) with the [CloudFront behavior contract](../../../design/deployment.md#cloudfront-behavior) | 10.6, 10.11       |
| Caddy origin `origin.aboutme.vn` (DNS-01 via Cloudflare)                                                                | 10.7, 10.11       |
| EIP + auto-reassociation                                                                                                | 10.2              |
| Origin-secret + prefix-list ingress                                                                                     | 10.2, 10.6, 10.7  |
| SSM secrets (IAM scoping + rotation)                                                                                    | 10.4              |
| CloudWatch alarms + dashboards + SNS/on-call                                                                            | 10.9              |
| Scheduled retention + RDS restore-verification (overlap, alarmed)                                                       | 10.10             |
| arm64 (Graviton) image build + ECS deploy pipeline drain→readiness                                                      | 10.7, 10.8, 10.12 |
| [Database release sequence](../../../design/deployment.md#database-and-releases), including backup and migration lock   | 10.5, 10.12       |
| `tofu validate`/`plan` in CI                                                                                            | 10.13             |
| Env-parameterized modules (staging/prod differ only by variables)                                                       | 10.1, 10.13       |
| **BLOCKING**: production Caddy client-IP boundary + e2e test                                                            | 10.7              |
| Modules apply cleanly to a UAT environment                                                                              | 10.15             |

---
