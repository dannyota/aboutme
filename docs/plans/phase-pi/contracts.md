# Contracts, budgets, and structure

## Traceability position

The committed authority is direct: **AC-INF-001…008** are PI-owned rows in
[`traceability/`](../traceability/), and **AC-OPS-015…019** are the P9A-owned
rows for the live CloudFront matrix, origin-secret rotation drill, live
two-runner migration, real restore, and alarm receipt. The current sequencing
and arm64 build wording live in
[`implementation-plan.md`](../implementation-plan.md); no companion patch file
is required or present.

| Row                | Owner              | This plan's obligation                                                                                                                              |
| ------------------ | ------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| AC-INF-001         | PI Task 7          | Fail-closed boundary + validated-chain viewer derivation + single canonical header, proven by the CI e2e matrix (rows 1–20)                         |
| AC-INF-002         | PI Task 6          | Behavior matrix (cache + origin-request policies incl. viewer-header exclusions, SSE origins, HSTS) encoded and asserted by mocked `terraform test` |
| AC-INF-003         | PI Task 1/13       | Env parity: byte-identical roots + tfvars key-set equality, CI-enforced                                                                             |
| AC-INF-007         | PI Task 6 (PI D25) | Staging viewer gate (basic-auth CloudFront Function, env-varying, disabled in production) — PI-originated control, no spec-clause attribution       |
| AC-INF-008         | PI Task 6 (PI D25) | Blanket `X-Robots-Tag: noindex, nofollow` on staging via the response-headers policy — PI-originated control, no spec-clause attribution            |
| AC-INF-004         | PI Task 4          | Secrets absent from repo/state (documented D9 exception); per-service IAM SSM paths disjoint                                                        |
| AC-INF-005         | PI Task 9          | Alarm inventory + dashboards + SNS provisioned, incl. drift + heartbeat alarms                                                                      |
| AC-INF-006         | PI Task 10         | Scheduled restore-verification + retention skeleton, overlap-locked, alarmed                                                                        |
| AC-OPS-001         | P0B (done)         | Reused, not re-implemented (D16); live two-runner staging drill is AC-OPS-017                                                                       |
| AC-OPS-002         | P9A                | PI builds the mechanism (D8/D9, Tasks 6–7) and proves it in CI simulation; live bypass rejection stays P9A                                          |
| AC-OPS-015…019     | P9A                | PI leaves the interfaces (alarm-trigger table, rotation runbook, restore job, deploy workflow); the drills are out of PI scope                      |
| AC-OPS-008/009/014 | P0 (done)          | PI's task env (`TRUSTED_PROXY_CIDRS=127.0.0.1/32`, `LISTEN_HOST=127.0.0.1`, `ENV=staging`/`prod`) is the deployed instantiation                     |

## Budget wiring (numeric budgets → infrastructure parameters)

| Budget (budgets.md)                   | Infrastructure parameter (task)                                                                                                                                                                      |
| ------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Server task ≤ 512 MiB (Go + Chromium) | ECS **task-definition-level** `memory = 512` (the task cgroup — budgets.md's whole-task semantics); container-level limits left unset or equal so a container can never out-budget the task (Task 5) |
| pgx pool ≤ 20 < `max_connections`     | RDS parameter group asserts `max_connections` ≥ 100 on the chosen class (Task 3, `terraform test` assertion)                                                                                         |
| SSE ≤ 2000 conns, ≥ 25 % fd headroom  | `ulimits { nofile soft/hard = 65536 }` on caddy + server containers (Task 5)                                                                                                                         |
| SSE heartbeat 25 s < CF idle timeout  | `caddy-sse` origin read timeout 60 s; default origin 30 s (Task 6, D22)                                                                                                                              |
| API/SSR p95 SLOs (P9A-measured)       | Instance classes are tfvars (D21) so P9A benchmark evidence can change them without module edits                                                                                                     |
| Request body ≤ 256 KB                 | App-enforced (P0 middleware); no CloudFront/Caddy override introduced                                                                                                                                |

## Module/file structure produced by this phase

| Path                                                                            | Responsibility                                                                                                                                                                               |
| ------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `deploy/aws/bootstrap/`                                                         | One-time: state bucket (+ noncurrent-version expiration) + KMS, GitHub OIDC provider, `ci-plan`/`ci-deploy-staging` roles, **shared ECR repos + lifecycle/immutability (D11)** (local state) |
| `deploy/aws/modules/network/`                                                   | VPC, public + **private (DB)** subnets, SG (443 from CloudFront origin-facing prefix list), EIP + ASG(1) auto-reassociation                                                                  |
| `deploy/aws/modules/database/`                                                  | RDS Postgres (gp3, PITR, write-only master password + version), parameter group, **private** subnet group                                                                                    |
| `deploy/aws/modules/storage/`                                                   | S3 media bucket (private, OAC-only policy), lifecycle rules                                                                                                                                  |
| `deploy/aws/modules/secrets/`                                                   | SSM parameter _names_ contract, KMS, per-service IAM task/execution roles with scoped read paths                                                                                             |
| `deploy/aws/modules/compute/`                                                   | ECS cluster, capacity provider (ASG, ECS-optimized arm64 AMI), task definitions (host-mode caddy/server, **bridge-mode web — D24**), log groups                                              |
| `deploy/aws/modules/edge/`                                                      | CloudFront distribution (behavior matrix incl. origin-request + response-headers policies, dual Caddy origins, S3 OAC `/assets`, **staging viewer gate — D25**), ACM us-east-1 cert          |
| `deploy/aws/modules/observability/`                                             | CloudWatch alarms (inventory below, with default thresholds), dashboards, SNS topic + subscription, EventBridge failure rules                                                                |
| `deploy/aws/modules/jobs/`                                                      | EventBridge Scheduler → RunTask wiring for retention (interface), restore-verification, TLS-expiry, **prefix-list drift check (D6)**                                                         |
| `deploy/aws/envs/staging/`, `deploy/aws/envs/production/`                       | Thin, byte-identical roots (D4): `main.tf`, `variables.tf`, `outputs.tf`, `backend.hcl`, `<env>.auto.tfvars`                                                                                 |
| `deploy/aws/scripts/secrets-bootstrap.sh`                                       | Writes SSM SecureStrings from operator input before first apply (D10/D9)                                                                                                                     |
| `deploy/aws/scripts/dns-apply.sh`                                               | Applies/diffs Cloudflare records from Terraform outputs via `cf` CLI (D19)                                                                                                                   |
| `deploy/aws/scripts/restore-verify.sh`                                          | RDS snapshot → isolated instance → verification query → teardown; overlap-guarded (D20)                                                                                                      |
| `deploy/aws/scripts/cidr-drift-check.sh`                                        | Compares deployed CloudFront origin-facing CIDR set vs the live managed prefix list; nonzero exit on drift (D6)                                                                              |
| `deploy/caddy/routes.caddy`                                                     | Shared route table imported by dev + prod Caddyfiles; env-placeholder upstreams with dev defaults (D7)                                                                                       |
| `deploy/caddy/boundary.caddy`                                                   | Edge boundary: fail-closed origin-secret gate, trusted-chain client-IP, header strip/emit (D5/D8)                                                                                            |
| `deploy/caddy/Caddyfile.prod`                                                   | Production Caddyfile: `admin off`, DNS-01 TLS, internal SSR listener (D24), loopback health vhost, imports of the two snippets                                                               |
| `deploy/caddy/Caddyfile.boundary-test`                                          | TLS-free wrapper importing the same snippets, driven by the CI e2e test (env-substituted listen port)                                                                                        |
| `deploy/caddy/Caddyfile` (modify)                                               | Dev file now imports `routes.caddy` (behavior unchanged; existing route-table test must stay green)                                                                                          |
| `deploy/caddy.Dockerfile` + `deploy/caddy/caddy-entrypoint.sh`                  | **Task 7:** xcaddy build (Caddy 2.11.4 + caddy-dns/cloudflare, pinned, arm64) + fail-closed env entrypoint guard                                                                             |
| `deploy/ops.Dockerfile`                                                         | Minimal arm64 image: aws-cli + postgresql-client + the ops scripts                                                                                                                           |
| `apps/server/internal/routetable/prod_boundary_test.go`                         | The BLOCKING e2e test (simulated edge, two viewers, forged + duplicated headers, fail-closed rows)                                                                                           |
| `.github/workflows/{iac.yml,images.yml,deploy-staging.yml}`                     | Authored by Tasks 8/12/13 as exact diffs, **applied by the integration owner**                                                                                                               |
| Root `Makefile` (diff for owner)                                                | `iac-fmt`, `iac-validate`, `iac-test`, `route-table-test-prod`, `staging-plan` targets                                                                                                       |
| `.env.example` (diff for owner — owner-serialized)                              | `CLOUDFLARE_API_TOKEN=` (Zone:DNS:Edit, `aboutme.vn` only), `AWS_PROFILE=` names-only additions                                                                                              |
| `docs/runbooks/{deploy-rollback,eip-recovery,secret-rotation,restore-drill}.md` | Seeded runbooks (drilled in P9A)                                                                                                                                                             |
| `docs/architecture.md` (update — **owner-serialized diff**)                     | Gains the deployed-staging current-state section (Task 14, diff handed to owner)                                                                                                             |

Module dependency graph:

```mermaid
graph TD
    B[bootstrap - one-time, local state: state bucket, OIDC roles, ECR] -.-> R[envs/staging - envs/production roots]
    R --> N[network]
    R --> SE[secrets]
    N --> D[database]
    R --> ST[storage]
    N --> C[compute]
    SE --> C
    D --> C
    ST --> E[edge]
    N --> E
    SE --> E
    C --> O[observability]
    D --> O
    E --> O
    C --> J[jobs]
    SE --> J
```

## Master-plan exit bullet → task map

| Master-plan PI exit bullet                                                                                              | Task(s)  |
| ----------------------------------------------------------------------------------------------------------------------- | -------- |
| VPC                                                                                                                     | 2        |
| ECS on EC2 Graviton — host networking for the edge/API tier (web tier bridge-mode per D24), fixed ports per P0 contract | 5 (D24)  |
| RDS Postgres (gp3)                                                                                                      | 3        |
| S3 (media)                                                                                                              | 3        |
| CloudFront + ACM (us-east-1) with the §6 behavior matrix                                                                | 6, 11    |
| Caddy origin `origin.aboutme.vn` (DNS-01 via Cloudflare)                                                                | 7, 11    |
| EIP + auto-reassociation                                                                                                | 2        |
| Origin-secret + prefix-list ingress                                                                                     | 2, 6, 7  |
| SSM secrets (IAM scoping + rotation)                                                                                    | 4        |
| CloudWatch alarms + dashboards + SNS/on-call                                                                            | 9        |
| Scheduled retention + RDS restore-verification (overlap, alarmed)                                                       | 10       |
| arm64 (Graviton) image build + ECS deploy pipeline drain→readiness                                                      | 7, 8, 12 |
| Migration advisory-lock sequence (+ pre-migration backup, §3)                                                           | 5, 12    |
| `terraform validate`/`plan` in CI                                                                                       | 13       |
| Env-parameterized modules (staging/prod differ only by variables)                                                       | 1, 13    |
| **BLOCKING**: production Caddy client-IP boundary + e2e test                                                            | 7        |
| Modules apply cleanly to a staging environment                                                                          | 14       |

---
