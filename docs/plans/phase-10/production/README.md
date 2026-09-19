# Single-host production implementation plan

Status: production serves v0.3.29. Task 16 acceptance and closure remain open.

> **For agentic workers:** use superpowers:executing-plans within the ownership
> rules in `AGENTS.md`. ADR 0024 keeps one author per task and one fresh phase
> review. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deploy web v1 to `https://aboutme.vn` on one AWS Singapore host behind
Cloudflare, and make it ready for the owner to test in production.

**Architecture:** One `t4g.small` Bottlerocket ECS host runs Caddy and Go in a
host-network task and Nuxt in a bridge-network task. RDS PostgreSQL and a
private S3 bucket sit behind them. OpenTofu in `deploy/aws/` builds the
infrastructure; a laptop script deploys image digests built by a public GitHub
workflow.

**Tech stack:** Go 1.27.1, Nuxt, Caddy 2.11.4, OpenTofu 1.12.6, AWS provider v6,
Cloudflare MCP for edge settings, Bottlerocket `aws-ecs-2`, PostgreSQL 18, Bash.

**Spec:**
[single-host production design](../../../design/single-host-production.md) and
[ADR 0037](../../../adr/0037-single-host-production-without-hosted-uat.md). Read
both before any task.

## Global constraints

- Region `ap-southeast-1`. Resource name prefix `aboutme-prod`. No em dashes in
  AWS names or descriptions.
- `ENV=prod`. `PUBLIC_ORIGIN=https://aboutme.vn`. `MCP_ENABLED=true`.
  `PROVIDER_LOGIN_ENABLED` unset.
- Go public listener `127.0.0.1:8080`; print listener `172.17.0.1:8081`; Nuxt
  host port `127.0.0.1:3000` reached as `http://127.0.0.1:3000`.
- `TRUSTED_PROXY_CIDRS=127.0.0.1/32`.
- Database `aboutme`, master user `aboutme`, login roles `aboutme_migrator` and
  `aboutme_app`. `sslmode=verify-full` with
  `sslrootcert=/etc/ssl/rds/global-bundle.pem`.
- SSM parameter prefix `/aboutme/prod/`. No secret value in Git, OpenTofu state,
  command arguments, logs, or this plan's reports.
- The repository is public. Never commit an AWS account ID, a Cloudflare zone or
  account ID, `backend.hcl`, or `prod.tfvars`.
- Resource rules from `AGENTS.md` apply: one database container, heavy checks
  one at a time, live DB tests with `-count=1`.
- Code snippets use spaces for Markdown lint; `gofmt` restores Go tabs.
- Workers never run Git. The integration owner commits each task.
- Any AWS or Cloudflare write needs the owner's go-ahead in the session where it
  happens.

## File map

| Path                                                         | Responsibility                                  |
| ------------------------------------------------------------ | ----------------------------------------------- |
| `apps/server/internal/dbroles/dbroles.go`                    | Fixed role creation and grant authority         |
| `apps/server/internal/dbroles/login.go`                      | SCRAM verifier and fixed login-role writes      |
| `apps/server/cmd/db-setup/main.go`                           | `db-setup` one-shot command                     |
| `apps/server/internal/config/{print,config}.go`              | Print listener and provider credential rules    |
| `apps/web/server/utils/print/redemption.ts`                  | Print origin allowlist                          |
| `deploy/server.Dockerfile`                                   | RDS CA bundle and the new binary                |
| `deploy/caddy/production/{Dockerfile,Caddyfile,render.sh}`   | Production Caddy image                          |
| `.github/workflows/release-images.yml`                       | Tag-triggered ARM64 build, smoke, GHCR publish  |
| `deploy/aws/probe/`                                          | Throwaway Bottlerocket probe                    |
| `deploy/aws/bootstrap/`                                      | State bucket and KMS key                        |
| `deploy/aws/modules/{network,data,identity,tasks,host,ops}/` | Production modules                              |
| `deploy/aws/prod/`                                           | Production root                                 |
| `deploy/aws/scripts/{secrets,tls,deploy}.sh`                 | Secret generation, TLS keys, deploy             |
| `docs/runbooks/production.md`                                | Operator steps for deploy, rollback and restore |

## Tasks

| Task | File                               | Deliverable                                         | Depends on |
| ---- | ---------------------------------- | --------------------------------------------------- | ---------- |
| 1    | [Host probe](host-probe.md)        | Four Bottlerocket facts proved or design sent back  | none       |
| 2    | [Code](code-tasks.md#task-2)       | Provisioning accepts the non-superuser owner        | none       |
| 3    | [Code](code-tasks.md#task-3)       | `db-set-login` sends SCRAM verifiers                | none       |
| 4    | [Code](code-tasks.md#task-4)       | Print listener and provider credential rules        | none       |
| 5    | [Code](code-tasks.md#task-5)       | Server image verifies RDS TLS and ships the command | 3          |
| 6    | [Release](release-tasks.md#task-6) | Production Caddy image                              | none       |
| 7    | [Release](release-tasks.md#task-7) | Tag workflow builds, smokes and publishes images    | 5, 6       |
| 8    | [Infra](infra-tasks.md#task-8)     | State bootstrap and production root skeleton        | 1          |
| 9    | [Infra](infra-tasks.md#task-9)     | Network, RDS and S3                                 | 8          |
| 10   | [Infra](infra-tasks.md#task-10)    | Secrets, IAM roles and ECS task definitions         | 9          |
| 11   | [Infra](infra-tasks.md#task-11)    | Host, ECS services and Cloudflare edge (MCP)        | 10         |
| 12   | [Infra](infra-tasks.md#task-12)    | Jobs and alarms                                     | 11         |
| 13   | [Deploy](deploy-tasks.md#task-13)  | `deploy.sh` with first-deploy and rollback modes    | 10         |
| 14   | [Deploy](deploy-tasks.md#task-14)  | Phase review, candidate gates, baseline marker      | 2–13       |
| 15   | [Deploy](deploy-tasks.md#task-15)  | First production deploy and smoke                   | 14         |
| 16   | [Deploy](deploy-tasks.md#task-16)  | Production checks, runbook, traceability, closure   | 15         |

Tasks 2, 3, 4 and 6 share no files and may run in parallel. Tasks 8 through 12
touch AWS and run in order. The phase review in Task 14 happens once, after the
last code task, and confirms client-IP trust, origin lockdown, secret handling,
IAM scope and migration order by name.
