# Task 5: Compute module — ECS cluster, arm64 capacity, task definitions (D24 topology)

**Files:** `deploy/aws/modules/compute/**` (+ tests), env-root wiring.

**Steps:**

- [ ] Failing `terraform test` (mocked, with `override_data` for AMI/SSM
      lookups) first, pinning the P0 contract into the task definitions: **caddy
      and server tasks use `network_mode = "host"`; the web task uses
      `network_mode = "bridge"` with a port mapping container 3000 → host 3000
      (D24)**; desired count 1 each; server container env has `PORT=8080`,
      `LISTEN_HOST=127.0.0.1`, `TRUSTED_PROXY_CIDRS=127.0.0.1/32`,
      `ENV=var.environment` (`staging`/`prod` — the values AC-OPS-009/014 fail
      closed on), `PGPASSWORD` arrives via `secrets`/`valueFrom` (never
      `environment`), and `DATABASE_URL` contains **no** credential; server
      task-definition **task-level** `memory = 512` (the task cgroup per
      budgets.md's whole-task semantics; container-level limits unset or equal —
      never a looser task bound); caddy + server containers set
      `ulimits nofile 65536/65536`; web container env has `HOST=0.0.0.0`
      (bridge-internal), `PORT=3000`, `NUXT_PUBLIC_API_BASE=/api/v1`,
      `NUXT_INTERNAL_API_BASE=http://${var.bridge_gateway_ip}:8081` (D24); caddy
      task env includes `GO_UPSTREAM=127.0.0.1:8080`,
      `WEB_UPSTREAM=127.0.0.1:3000`, `INTERNAL_API_LISTEN` derived from
      `var.bridge_gateway_ip` (D7/D24); the server task definition contains the
      non-essential `migrate` container with `dependsOn SUCCESS` from the server
      container (D16); every container's image input is a **digest** reference
      (`@sha256:`) — the test rejects a tag.
- [ ] Implement: cluster, capacity provider on Task 2's ASG, three services
      (caddy, server, web) with deployment `minimum_healthy_percent = 0`,
      `maximum_percent = 100` (fixed host ports forbid overlap — this _is_ the
      documented maintenance window), **deployment circuit breaker with rollback
      enabled**, container health checks mapped to `/healthz` (liveness —
      readiness must not restart-loop on DB outage, spec §6's liveness/readiness
      split); **the caddy container's health check targets Caddy's loopback
      health vhost `http://127.0.0.1:2020/caddy-health` (Task 7) — a plain
      `wget` against the named production site block would 404 on Host mismatch,
      so the dedicated vhost is the named mechanism**; log groups per service
      with `var.log_retention_days`.
- [ ] Caddy task env additionally includes `CLOUDFRONT_ORIGIN_CIDRS` rendered
      from the D6 prefix-list data source, plus the ECS secrets for origin
      secrets and the Cloudflare token; bind mount `/var/lib/caddy` host volume
      (D13).

**Verification:** `terraform test`, `validate`, parity. Live scheduling
correctness is Task 14 (real AWS, stated).
