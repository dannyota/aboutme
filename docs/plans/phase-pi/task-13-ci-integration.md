# Task 13: CI integration — `terraform validate`/`plan`/test, parity, boundary job

AC-INF-003 closure. **Split per review non-blocking 12:** the worker authors and
locally verifies; the integration owner applies and observes — the worker cannot
execute the applied-workflow steps and must not claim them.

**Files:** `.github/workflows/iac.yml` + additions to `ci.yml` authored as diffs
for the integration owner; Makefile diff (`iac-fmt`, `iac-validate`, `iac-test`,
`staging-plan`). Per ADR 0011, `iac-fmt`, `iac-validate`, and `iac-test` are
fork-safe and credential-free, so `make ci` composes all three — they run at the
gate of record, not only in CI. `staging-plan` is credentialed and stays out of
`make ci`; it runs only via the manual `workflow_dispatch` path below.

**Worker steps:**

- [ ] Author the PR-gate job set (fork-safe, zero credentials — D17):
      `terraform fmt -check -recursive`; per-root `init -backend=false` +
      `validate`; `terraform test` across bootstrap + all modules (mock
      providers); `tflint` with the pinned AWS ruleset; `parity-check.sh`
      (diff + tfvars key-set); shellcheck on `deploy/aws/scripts`;
      `route-table-test-prod` (Task 7's e2e job with the pinned caddy binary,
      alongside the existing `route-table` job).
- [ ] Author the credentialed `staging-plan` job: manual `workflow_dispatch`
      only, after the AWS-authorization gate; OIDC → `ci-plan-staging`,
      `terraform     plan -lock=false` against the staging backend, plan summary
      posted; **never** on `push` or `pull_request` (public repo, fork secrets).
      Author and lint the workflow before AWS authorization, but do not dispatch
      it or make any AWS API call until the activation gate is recorded.
- [ ] Locally verify everything the worker _can_ verify: `actionlint` on both
      workflow files; run each PR-gate command directly in the worktree and
      record green output; deliberately mis-format one `.tf` file, run
      `terraform fmt -check`, observe red, revert — the failing-first
      observation at command level.
- [ ] Hand all diffs to the integration owner with the exact expected job-name
      list and trigger table.

**Integration-owner steps (not the worker's):** apply the diffs; observe the PR
gate green on a no-op PR and red on a seeded violation; confirm no AWS
credentials are reachable from any `pull_request` trigger by reading the applied
workflow triggers.

**Verification:** worker: local command runs + `actionlint` output recorded.
Owner: the applied workflows' red-then-green observation.
