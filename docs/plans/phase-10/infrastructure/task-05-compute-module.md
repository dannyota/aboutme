# Task 10.5: Compute module — ECS cluster, arm64 capacity, task definitions (D24 topology)

**Files:** `deploy/aws/modules/compute/**` (+ tests), env-root wiring.

**Steps:**

- [ ] Failing `tofu test` (mocked, with `override_data` for AMI/SSM lookups)
      first, pinning the P0 contract into the task definitions: **caddy and
      server tasks use `network_mode = "host"`; the web task uses
      `network_mode = "bridge"` with a port mapping container 3000 → host 3000
      (D24)**; every service uses
      `desired_count = var.services_enabled ? 1 : 0`, where foundation and DNS
      stages keep it false and only final activation sets it true; server
      container env has `PORT=8080`, `LISTEN_HOST=127.0.0.1`,
      `TRUSTED_PROXY_CIDRS=127.0.0.1/32`, `ENV=var.environment`
      (`staging`/`prod` — the values AC-OPS-009/014 fail closed on),
      `PGPASSWORD` arrives via `secrets`/`valueFrom` (never `environment`)
      through the server execution role, and `DATABASE_URL` contains **no**
      credential; server task-definition **task-level** `memory = 512` (the task
      cgroup per budgets.md's whole-task semantics; container-level limits unset
      or equal — never a looser task bound); caddy + server containers set
      `ulimits nofile 65536/65536`; web container env has `HOST=0.0.0.0`
      (bridge-internal), `PORT=3000`, `NUXT_PUBLIC_API_BASE=/api/v1`,
      `NUXT_INTERNAL_API_BASE=http://${var.bridge_gateway_ip}:8081` (D24);
      server env also has the render-origin setting (the old baseline uses
      `NUXT_RENDER_ORIGIN=http://127.0.0.1:3000`; refresh against
      `PUBLIC_RENDER_ORIGIN` before dispatch), the direct origin used only for
      bounded `POST /internal-render/public`; caddy task env includes
      `GO_UPSTREAM=127.0.0.1:8080`, `WEB_UPSTREAM=127.0.0.1:3000`,
      `INTERNAL_API_LISTEN` derived from `var.bridge_gateway_ip` (D7/D24); the
      server task definition contains the non-essential `migrate` container with
      `dependsOn SUCCESS` from the server container (D16). Only `migrate`
      receives `DB_MIGRATOR_PASSWORD`; Go receives only `PGPASSWORD`. The module
      also creates a non-service, one-shot DB-bootstrap task definition whose
      container receives master, app, migrator, and restore secrets through Task
      4's dedicated execution role and runs Task 10.8's exact bootstrap script.
      Every container's image input is a **digest** reference (`@sha256:`) — the
      test rejects a tag.
- [ ] Implement: cluster, capacity provider on Task 10.2's ASG, three services
      (caddy, server, web) with deployment `minimum_healthy_percent = 0`,
      `maximum_percent = 100` (fixed host ports forbid overlap — this _is_ the
      documented maintenance window), **deployment circuit breaker with rollback
      enabled**, container health checks mapped to `/healthz` (liveness —
      readiness must not restart-loop on DB outage, as required by
      [route ownership](../../../design/system.md#route-ownership)); **the caddy
      container's health check targets Caddy's loopback health vhost
      `http://127.0.0.1:2020/caddy-health` (Task 10.7) — a plain `wget` against
      the named production site block would 404 on Host mismatch, so the
      dedicated vhost is the named mechanism**; log groups per service with
      `var.log_retention_days`; Container Insights enabled for the task and
      resource metrics consumed by Task 10.9.
- [ ] Apply one runtime baseline in mocked tests and task definitions:
      `privileged=false`, `readonlyRootFilesystem=true`, `user` set to dedicated
      numeric non-root UIDs, no-new-privileges, and every Linux capability
      dropped. The sole exception is Caddy's `CAP_NET_BIND_SERVICE` so UID 10001
      can bind 443; it receives no other capability. Server and migrate use
      10002, web 10003, and ops/bootstrap 10004. Writable paths are named mounts
      with exact owner and mode prepared by Task 10.2 user data; Caddy gets
      `/var/lib/caddy` and `/var/log/caddy`, while temporary paths use bounded
      tmpfs mounts. Caddy, web, and ops tasks use exact memory/CPU reservations
      of 128 MiB/128 units, 256 MiB/256 units, and 256 MiB/256 units; server
      remains 512 MiB/512 units. Tests reject root, privileged mode, writable
      roots, host-device mounts, added capabilities, or aggregate reservations
      above the selected host capacity.
- [ ] Caddy task env additionally includes `CLOUDFRONT_ORIGIN_CIDRS` rendered
      from the D6 prefix-list data source, plus the ECS `secrets`/`valueFrom`
      entries for origin secrets and the Cloudflare token through Caddy's
      execution role; bind mount Task 10.2's `/var/lib/caddy` and
      `/var/log/caddy` host volumes (D13).
- [ ] Inject Task 10.6's exact CloudFront distribution ID into Go as
      `CLOUDFRONT_DISTRIBUTION_ID`. It must match the one ARN allowed by Task
      4's server task-role policy; no other container receives it.
- [ ] Wire private media into the server task: `MEDIA_BACKEND=s3`,
      `MEDIA_BUCKET` from Task 10.3, and `MEDIA_REGION=var.aws_region`. Leave
      `MEDIA_ENDPOINT`, `MEDIA_ACCESS_KEY_ID`, and `MEDIA_SECRET_ACCESS_KEY`
      unset, and leave path-style addressing off. The AWS SDK obtains
      credentials from the Task 10.4 server task role. `tofu test` proves that
      the server task uses that role and that caddy, web, and execution roles
      receive neither media settings nor static media credentials.

**Verification:** `tofu test`, `validate`, parity. Live scheduling correctness
is Task 10.15 (real AWS, stated).
