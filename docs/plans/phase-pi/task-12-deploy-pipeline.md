# Task 12: Deploy pipeline — pre-migration snapshot, drain→readiness, rollback

**Files:** `.github/workflows/deploy-staging.yml` authored as a diff for the
integration owner; `docs/runbooks/deploy-rollback.md` seed.

**Steps:**

- [ ] Author the workflow (manual `workflow_dispatch` with an input naming the
      image-digest manifest from Task 8's build): OIDC → `ci-deploy-staging`;
      **pre-migration backup per spec §3: create an RDS snapshot
      (`aws rds create-db-snapshot`), wait for `available`, and verify its
      status/size before any apply — the workflow hard-fails without a verified
      snapshot (review blocking 9)**; `terraform plan` with the digest vars and
      **post the plan as the run summary**; a required manual environment
      approval gate; `terraform apply` (updates task definitions + services —
      the D16 init container makes migration-before-server intrinsic, the
      min-healthy-0 % single-node deploy stops the old task first so "stop
      writes → backup → lock → goose up" holds, and the advisory lock makes a
      concurrent second deploy safe, AC-OPS-001);
      `aws ecs wait services-stable`; then a synthetic smoke: `GET /healthz` +
      `/readyz` **through the staging CloudFront URL** (the
      CloudFront→Caddy→app→DB chain), **sending the D25 staging-gate
      credentials**, failing the run on non-200. Drain semantics: ECS SIGTERM →
      the P0 server's graceful shutdown; `stopTimeout` set ≥ the SSE heartbeat
      interval.
- [ ] Rollback semantics — **code-back / schema-forward (D16, spec §3)**:
      re-dispatch with the previous digest manifest (digests are immutable)
      rolls back **code only**; the schema is never downgraded — released
      migrations are append-only and an older image's migrate container finds
      nothing new to apply against a forward schema; a _bad migration_ is
      repaired by a **forward corrective migration**, and the pre-deploy
      snapshot + PITR are the data-recovery path of last resort. Document
      exactly this in `docs/runbooks/deploy-rollback.md`, plus the automatic
      circuit-breaker rollback from D16 and the **documented maintenance
      window** language from spec §6 (single node, min-healthy 0 %).
- [ ] `actionlint` the workflow; hand the diff to the integration owner. Note in
      the workflow header: **P10 promotes by running the same workflow shape
      against `envs/production` with the staging-proven digest manifest** — the
      interface P10 consumes; PI does not create a production workflow run.

**Verification:** `actionlint`; the workflow's first real execution is Task 14's
staging bring-up after the activation gate. Docs gates on the runbook.
