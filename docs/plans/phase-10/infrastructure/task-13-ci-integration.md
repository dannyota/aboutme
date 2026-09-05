# Task 10.13: CI integration — `tofu validate`/`plan`/test, parity, boundary job

AC-INF-003 closure. The task author locally verifies the fork-safe jobs; the
integration owner applies and observes credentialed workflow steps.

**Files:** private `aboutme-infra` `.github/workflows/iac.yml` and Makefile diff
(`iac-fmt`, `iac-validate`, `iac-test`, `staging-plan`), plus public app
`ci.yml`/Makefile diffs only for shared application checks. Resolve exact paths
under the [repository boundary](README.md#repository-boundary) before dispatch.
Per ADR 0024, the private repository's local `make ci` composes `iac-fmt`,
`iac-validate`, and `iac-test`, all credential-free. The public app's `make ci`
does not depend on private files/tools or AWS. `staging-plan` is credentialed
and stays out of either local gate; it runs only via manual dispatch below.

**Worker steps:**

- [ ] Author the PR-gate job set (fork-safe, zero credentials — D17):
      `tofu fmt -check -recursive`; per-root `init -backend=false` + `validate`;
      `tofu test` across bootstrap + all modules (mock providers); `tflint` with
      the pinned AWS ruleset; `parity-check.sh` (diff + tfvars key-set);
      shellcheck on `deploy/aws/scripts`; `route-table-test-prod` (Task 10.7's
      e2e job with the pinned caddy binary, alongside the existing `route-table`
      job).
- [ ] Keep existing public app jobs and the pinned AMD64 `web-e2e` comparison on
      their existing architectures. Use `ubuntu-24.04-arm` for task 10.8's
      private image build/smoke and task 10.12's previous-image compatibility
      job. Fail workflow tests if either ARM64 execution job uses an x86 runner
      or QEMU, or if the baseline comparison is moved to ARM64. Test that build
      smoke defaults to no publication, AWS access exists only in protected
      manual jobs, OIDC names private `aboutme-infra`, and deploy rejects a
      failed build, wrong commit/run, missing image, or wrong platform.
- [ ] Exercise source and retention rejection cases: green checks on a
      PR/fork-only SHA, wrong workflow/check identity, or an unapproved
      candidate branch cannot authorize publication/deployment. An expired
      Actions artifact with task 10.8's valid protected release record works;
      missing or tampered archival evidence fails. Registry lifecycle tests keep
      all referenced UAT/promotion/rollback images through their window.
- [ ] Cancel superseded credential-free PR checks using a workflow-and-PR
      concurrency group. Keep publication and deployment in separate groups with
      `cancel-in-progress: false`. Verify architecture-specific cache keys,
      isolation from untrusted PR writes, explicit timeouts, and artifact
      retention against the
      [build contract](contracts.md#build-and-runner-contract). Pin actions to
      reviewed commit SHAs and tools to exact versions; validate their native
      ARM64 support when authoring the affected jobs.
- [ ] Author the credentialed `staging-plan` job: manual `workflow_dispatch`
      only, after the Phase 9 cost/local-checkpoint gate and the existing UAT
      authorization record; OIDC → `ci-plan-staging`, `tofu plan -lock=false`
      against the staging backend, plan summary posted; **never** on `push` or
      `pull_request`. Author and lint the private workflow before activation,
      but do not dispatch it or make any AWS API call until the activation
      handoff is recorded.
- [ ] Locally verify everything the task author can verify: `actionlint` on both
      repositories' affected workflow files; run each PR-gate command in its
      owning worktree and record green output; deliberately mis-format one `.tf`
      file, run `tofu fmt -check`, observe red, revert — the failing-first
      observation at command level.
- [ ] Hand all diffs to the integration owner with the exact expected job-name
      list and trigger table.

**Integration-owner steps (not the worker's):** apply the diffs; observe the PR
gate green on a no-op PR and red on a seeded violation; confirm no AWS
credentials are reachable from any `pull_request` trigger by reading the applied
workflow triggers. Observe a private native ARM64 build/smoke run with
publication disabled and record it before task 10.8's first ECR publication.

**Verification:** worker: local command runs + `actionlint` output recorded.
Owner: the applied workflows' red-then-green observation.
