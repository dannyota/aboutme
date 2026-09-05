# Task 10.12: Deploy pipeline — pre-migration snapshot, drain→readiness, rollback

**Files:** `.github/workflows/deploy-staging.yml` authored as a diff for the
integration owner in private `aboutme-infra`; `docs/runbooks/deploy-rollback.md`
seed. Consumes task 10.8's versioned digest manifest and successful build run.

**Steps:**

- [ ] Author the workflow (manual `workflow_dispatch` with an input naming the
      image-digest manifest and run from task 10.8's build, or its reviewed
      private release record after artifact expiry). Before AWS access, verify
      the checksum and manifest schema, successful build run/attempt in the
      expected private repository/workflow, and app/infrastructure source
      provenance under task 10.8's protected candidate-branch and check-identity
      rules. An archive must come from a reviewed protected private-repository
      commit and preserve the original manifest and successful-run evidence;
      reject a missing or tampered archive. Check the approved commits,
      `linux/arm64`, and all four expected ECR repository/digest pairs. Reject
      missing, substituted, or mismatched metadata; never resolve a mutable tag
      or rebuild an image during deploy or promotion. The previous-image
      compatibility step uses `ubuntu-24.04-arm` to execute the ARM64 image
      natively after authorized registry access. Then OIDC →
      `ci-deploy-staging`; acquire a per-environment concurrency lock with
      `cancel-in-progress: false` so a later request cannot interrupt an active
      snapshot/migration/rollback. Run a speculative OpenTofu plan with the
      digest variables, publish the redacted summary, and require the
      protected-environment approval. After approval, scale the server service
      to zero, wait for service stability, and assert zero running/pending
      server tasks before taking the snapshot. Create the RDS snapshot, wait for
      `available`, and verify its identifier, source, status, encrypted flag,
      and nonzero allocated size. This exact drain → verified backup order
      implements the
      [database release sequence](../../../design/deployment.md#database-and-releases).
      A failure before the candidate apply restores the previous service to
      desired count one, waits for readiness, and reports failure.
- [ ] After the snapshot, create a **fresh** saved OpenTofu plan from the
      drained state. It must restore `services_enabled=true`, carry the approved
      digest manifest, and differ from the approved speculative plan only by the
      expected out-of-band service-count restoration. Verify that comparison,
      then apply the fresh plan; never reuse a plan created before drain. The
      migrate init runs with the dedicated migrator credential and must succeed
      before Go starts. The advisory lock makes a concurrent second migration
      safe (AC-OPS-001). Wait with `aws ecs wait services-stable`, then send
      `/healthz` and `/readyz` through the UAT CloudFront distribution using the
      access mechanism resolved by the Phase 10 refresh (the old baseline uses
      Basic credentials read only from protected GitHub environment secrets).
      Resolve the Basic Authorization conflict before dispatch. Fail on any
      non-200; never print or retain the Authorization value. ECS `SIGTERM`
      invokes graceful shutdown; `stopTimeout` is at least the SSE heartbeat
      interval.
- [ ] First activation is an explicit workflow mode, not an exception hidden in
      shell conditionals. It requires services already at desired zero and
      proves no prior server task or writer exists, takes and verifies the empty
      data-plane snapshot, then runs the same fresh-plan/migrate/readiness path.
      A failure leaves services at zero because there is no prior revision to
      restore. Subsequent deploys use the drain/restore path above.
- [ ] Rollback semantics — **code-back / schema-forward (D16 and the
      [database release sequence](../../../design/deployment.md#database-and-releases))**:
      before any apply, create a disposable seeded database at the prior schema,
      apply the candidate migrations, start the exact previous server digest
      against it, and require readiness plus its supported read/write smoke.
      This compatibility test is mandatory even when the candidate's own tests
      pass. A breaking change uses expand, backfill, and contract across
      releases; a contract migration cannot land while the prior digest remains
      the rollback target. Then re-dispatch with the previous digest manifest
      (digests are immutable) rolls back **code only**; the schema is never
      downgraded — released migrations are append-only and an older image's
      migrate container finds nothing new to apply against a forward schema; a
      _bad migration_ is repaired by a **forward corrective migration**, and the
      pre-deploy snapshot + PITR are the data-recovery path of last resort. If
      the previous digest fails the migrated-schema test, stop the deployment;
      do not claim that the circuit breaker can restore service. Document
      exactly this in `docs/runbooks/deploy-rollback.md`, plus the automatic
      circuit-breaker rollback from D16 and the **documented maintenance
      window** from the
      [production topology](../../../design/deployment.md#production-topology)
      (single node, min-healthy 0 %).
- [ ] `actionlint` the workflow; hand the diff to the integration owner. Note in
      the workflow header: **Phase 11 promotes by running the same workflow
      shape against `envs/production` with the staging-proven digest manifest**
      — the interface Phase 11 consumes; Phase 10 infrastructure does not create
      a production workflow run.

**Verification:** `actionlint`; local manifest rejection and compatibility
harness tests on the laptop architecture; native ARM64 execution of the exact
previous-digest/migrated-schema check before deployment. The workflow's first
real execution is Task 10.15's hosted-UAT activation after the local checkpoint.
Docs gates on the runbook.
