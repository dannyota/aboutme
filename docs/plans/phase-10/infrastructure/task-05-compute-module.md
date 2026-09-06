# Task 10.5: Compute module — replica placement, services, and autoscaling

**Dependency:** Task 10.18's distributed runtime implementation and local proof.
This task implements its service-discovery, placement, readiness, drain,
connection, and autoscaling outputs; it does not choose those contracts.

**Files:** `deploy/aws/modules/compute/**` and tests, environment-root wiring.

**Steps:**

- [ ] Write mocked `tofu test` cases that place one complete application replica
      per EC2 node. A replica keeps distinct Caddy, Go, and Nuxt ECS tasks with
      separate cgroups and uses the service discovery and placement constraints
      selected by Task 10.18. Tests reject a layout that collapses the three
      tasks into one task definition or routes Go to another replica's Nuxt.
- [ ] Pin the runtime security and resource baseline in mocked tests and task
      definitions: `privileged=false`, read-only root filesystems,
      no-new-privileges, no host-device mounts, and dedicated numeric non-root
      UIDs. Caddy uses UID/GID 10001, Go and migrate use 10002, web uses 10003,
      and ops/bootstrap use 10004. Drop every Linux capability. Add only
      `CAP_NET_BIND_SERVICE` to Caddy when Task 10.18's selected listener still
      requires a privileged port; no other task or port receives it.
- [ ] Give each task an explicit cgroup and reservation: Caddy 128 MiB/128 CPU
      units, Go plus Chromium 512 MiB/512 units, Nuxt 256 MiB/256 units, and
      ops/bootstrap 256 MiB/256 units. Container limits are unset or no larger
      than their task limit. Caddy and Go use `nofile` soft/hard 65536. Tests
      reject aggregate reservations above the selected node capacity.
- [ ] Use named writable mounts with exact owner and mode prepared by Task 10.2;
      temporary paths use bounded tmpfs mounts. Preserve Caddy's required state
      and log mounts if Task 10.18's TLS design needs them. Tests reject a
      writable root, wrong owner/mode, unbounded temporary storage, or a mount
      visible to an unrelated task.
- [ ] Wire Go, Nuxt, private print redemption, and frozen public rendering with
      the colocated private routes chosen by Task 10.18. Keep external
      `/print/**` and `/internal-render/**` denied. Only the server task
      receives media, database, and CloudFront invalidation permissions. Only
      the migration path receives the migrator credential.
- [ ] Inject `PGPASSWORD` into Go through ECS `secrets`/`valueFrom` and its
      execution role, never through `environment`; `DATABASE_URL` contains no
      credential. Only migrate receives `DB_MIGRATOR_PASSWORD`. The separate
      one-shot bootstrap task receives the master, application, migrator, and
      restore secrets through Task 10.4's dedicated bootstrap execution role; no
      application service receives that combined set.
- [ ] Inject Task 10.6's exact CloudFront distribution ID into Go as
      `CLOUDFRONT_DISTRIBUTION_ID`. It must match the sole distribution ARN in
      Task 10.4's server task-role invalidation policy, and no other container
      receives it.
- [ ] Wire private media only into Go with `MEDIA_BACKEND=s3`, Task 10.3's
      `MEDIA_BUCKET`, and `MEDIA_REGION=var.aws_region`. Leave `MEDIA_ENDPOINT`,
      `MEDIA_ACCESS_KEY_ID`, and `MEDIA_SECRET_ACCESS_KEY` unset and path-style
      addressing disabled. The AWS SDK uses Task 10.4's server task role. Tests
      prove Caddy, Nuxt, ops, execution, and bootstrap roles have no media
      settings, static access keys, or media permissions.
- [ ] Enforce Task 10.18's per-task pgx pool ceiling and RDS administrative
      reserve. Readiness remains closed until required shared coordination,
      database capacity, and colocated render/redemption routes are healthy.
      Liveness does not restart-loop on an ordinary database outage.
- [ ] Implement graceful stop, deregistration delay, request and SSE draining,
      render claim handoff, deployment circuit breaker, and startup order from
      Task 10.18. A task cannot become ready before its publication, limiter,
      render, SSE, and private-route dependencies.
- [ ] Implement production application autoscaling with minimum one and initial
      maximum two replicas. Use the approved metrics, thresholds, cooldowns, ECS
      capacity-provider behavior, and one-replica-per-node placement. RDS
      compute stays fixed. Autoscaling and the UAT lifecycle cannot set
      production desired capacity to zero. Task 10.12's serialized,
      operator-approved snapshot and migration drain is the explicit exception;
      it restores at least one healthy production replica afterward.
- [ ] Implement scheduled UAT active and stopped states. Active UAT can exercise
      one and two replicas. Stopped UAT sets ECS and ASG capacities coherently
      to zero and cannot be awakened by replacement or a scaling policy. The
      lifecycle owner, not a normal service deployment, changes this state.
- [ ] Keep application services disabled through bootstrap, foundation, and DNS
      stages. Only Task 10.15's authorized booked-window transition enables them
      after secrets, migration, ALB, target trust, and readiness inputs are
      complete. A speculative or foundation plan cannot create a running writer.
- [ ] Keep the one-shot database bootstrap and migration task definitions
      separate from application services. Every runtime image uses `@sha256:`;
      tests reject mutable tags and credentials in environment values.

**Verification:** mocked `tofu test`, `tofu validate`, environment parity, task
definition policy tests, and Task 10.18's local topology and drain suites. Task
10.15 proves real placement and a production-shaped 1 → 2 → 1 transition.
