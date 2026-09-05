# Environment and constraints

## Environment facts (verified 2026-08-02 at `9382c86`)

- `deploy/` today: `compose.yml` (three disjoint networks — `db`, `edge`
  10.90.0.0/28, `frontend`; one-shot `migrate` service using the server image
  with a `migrate` entrypoint; `PGPASSWORD` never spliced into `DATABASE_URL`),
  `caddy/Caddyfile` (dev), `server.Dockerfile` (builds `server` + `migrate`
  binaries; Chromium seam reserved for Phase 7.1), `web.Dockerfile`.
  **`deploy/aws/` does not exist yet**. The
  [repository boundaries](../../../design/repository.md) assign deployment
  artifacts to `deploy/`, and the
  [production topology](../../../design/deployment.md#production-topology)
  defines the target.
- The dev Caddyfile carries an explicit **"DEV ONLY — do not promote
  unchanged"** marker naming this phase: it sets `X-Real-IP` from the immediate
  peer, which behind CloudFront is the **edge**, recreating the cross-tenant DoS
  the Phase 0 security review flagged. Caddy 2.11.4
  (`docker.io/library/caddy:2.11.4-alpine`) is the pinned dev version.
- A live-Caddy test harness already exists:
  `apps/server/internal/routetable/route_table_test.go` drives the real dev
  Caddyfile through a `caddy` binary (`CADDY_BIN`, `make route-table-test`, CI
  job `route-table` with a pinned caddy) against two stub backends, asserting
  backend fingerprints, **including a listen-port substitution so the test never
  needs a privileged port**. Task 10.7 extends this pattern (including the port
  substitution); do not invent a second harness.
- Go server config contract (P0, tested): `ENV=prod` **and** `ENV=staging` fail
  closed without `TRUSTED_PROXY_CIDRS` (each CIDR ≤ /8 IPv4, ≤ /48 IPv6, no
  IPv4-mapped prefixes) and require a **loopback** `LISTEN_HOST` (AC-OPS-009,
  AC-OPS-014). The canonical client-IP header is `X-Real-IP`, accepted only from
  trusted-proxy peers (AC-OPS-008). Host networking for Caddy + Go satisfies
  this exactly: Go binds `127.0.0.1:8080`, Caddy proxies via loopback,
  `TRUSTED_PROXY_CIDRS=127.0.0.1/32`. **The web tier does NOT share that
  namespace** — see D24.
- Phase 10 infrastructure port map: Caddy `80/443` (the production SG exposes
  only 443; see D14), Go `127.0.0.1:8080`, and Nuxt `127.0.0.1:3000` (satisfied
  through D24's bridge port publication). The
  [production topology](../../../design/deployment.md#production-topology) fixes
  one task per service and no application load balancer; Phase 10 infrastructure
  adds no service discovery.
- CI jobs at base:
  `docs, schema, api, server, web, server-integration, migrations-append-only, released-schema-append-only, gitleaks, semgrep, route-table, sqlc-drift`
  (`.github/workflows/ci.yml`). Makefile targets relevant here: `docs-fmt`,
  `docs-lint`, `route-table-test`, `dev`, `test-db-up/down`,
  `server-migration-test`, plus `ci`, `check`, `scan` (`make ci` is the ADR 0024
  gate of record, run locally by the integration owner before integration).
- Two-runner migration safety is already implemented and tested (AC-OPS-001):
  goose Provider with Postgres session advisory lock,
  `apps/server/migrations/harness_test.go::TestHarness_ConcurrentRunners_ExactlyOneApplies`.
  Phase 10 infrastructure reuses it; Phase 10 infrastructure does not
  re-implement migration locking.
- `.env.example` holds names-only variables; `.env` is git-ignored and never
  committed. The repo is **public**: no secret, account ID, or internal hostname
  may appear in OpenTofu code, tfvars, workflow logs, or docs.
- The repo `go.work` gotcha applies: run Go commands from inside `apps/server`.
- Budgets that bind infrastructure (from `../budgets.md`): whole-server-task
  memory ≤ 512 MiB — a **task-level cgroup** (Go + Chromium together), not a
  container-level limit; pgx pool ≤ 20 < Postgres `max_connections`; SSE ≤ 2000
  conns/task with ≥ 25 % fd headroom; SSE heartbeat 25 s < CloudFront idle
  timeout; API/SSR p95 SLOs measured in Phase 10 operational rehearsal on the
  production instance class.
- GitHub runner decision (2026-09-05): native `ubuntu-24.04-arm` builds target
  `linux/arm64` in the planned private `aboutme-infra` repository. Public app CI
  uses free standard runners; private deployment builds consume included/paid
  Actions minutes and storage. Task 9.1 includes those costs. The current public
  AMD64 browser baseline job retains its architecture. Recheck runner tool
  availability at implementation; the label does not pin installed tools. See
  the [build contract](contracts.md#build-and-runner-contract).
- Fork PR checks have no AWS role or publication access. Build/plan/deploy OIDC
  trust names the private infrastructure repository and its protected
  environment; public app workflow identities cannot assume those roles.

## Global constraints (inherited, plus phase-specific)

- **Latest stable at scaffold time, then pinned exactly.** At scaffold the
  implementer resolves and pins: OpenTofu version (`required_version` exact,
  plus the repo-owned `.tool-versions` OpenTofu pin), AWS provider (exact
  version in `required_providers` + committed `.terraform.lock.hcl` with
  `tofu providers lock -platform=linux_amd64 -platform=linux_arm64`), tflint +
  its AWS ruleset, xcaddy, `caddy-dns/cloudflare` module version, ECS-optimized
  arm64 AMI (pinned via SSM parameter **resolved and recorded**, not floating),
  and all base image digests. Never hand-write a version guessed from memory;
  resolve it, then commit the resolved pin.
- Feature-availability guardrail: several mechanisms this plan may need depend
  on toolchain versions (OpenTofu S3 state locking, `tofu test` mock providers,
  ephemeral values, write-only arguments such as `aws_db_instance.password_wo`,
  and Caddy `trusted_proxies` / `client_ip_headers` / `{client_ip}`). The
  refresh must verify each against current official documentation for the pinned
  release before use. Report any incompatibility and revise the proposed task
  contract; do not assume a tool-specific feature or substitute a mechanism
  silently.
- HCL style: `tofu fmt` mandatory; module inputs/outputs documented with
  `description`; no `provider` blocks inside shared modules (providers are
  passed from the env roots); no `count`/`for_each` churn that would force
  destroy/recreate of stateful resources on a variable flip.
- Shell scripts: `bash`, `set -euo pipefail`, shellcheck-clean.
- **No secrets in repo or state where avoidable:** secret _names_ live in code;
  secret _values_ live in SSM SecureString written by the bootstrap script from
  `.env`/operator input. The single unavoidable exception (CloudFront's
  `X-Origin-Secret` custom origin header value, which AWS stores in the
  distribution config and therefore in OpenTofu state) is documented and
  mitigated in decision D9.
- Determinism: `tofu test` runs use mock providers (no network, no credentials)
  — and wherever a module's behavior is wired through a **data source** (managed
  prefix list, SSM parameters, AMI lookup), the test must supply explicit
  `override_data`/mock-data values so the assertion checks real derived wiring,
  not provider-generated placeholders. The Caddy boundary test injects its
  secrets, CIDRs, and listen ports via env vars and stub backends exactly like
  the existing route-table test — no real CloudFront, no sleeps, no retries.
- Conventional Commits; no AI/agent mentions; `make docs-fmt` before committing
  any touched `.md`.
- Real-AWS work (bootstrap apply, ECR push, UAT apply, ACM issuance, and cf DNS)
  is **explicitly labeled** in its task. It runs only after the Phase 9 cost
  decision and infrastructure local checkpoint, from an operator/maintainer
  context — never from a fork-PR CI job — and records redacted evidence in the
  phase ledger under `.superpowers/`.
