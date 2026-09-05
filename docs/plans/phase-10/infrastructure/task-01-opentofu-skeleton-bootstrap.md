# Task 10.1: OpenTofu skeleton, exact pinning, bootstrap stack (incl. shared ECR)

AC-INF-003 (parity substrate). Produces the layout every later task fills in.

**Task gate:** One author writes the failing checks first and runs the affected
checks. The fresh Phase 10 review covers federated identities, permissions
boundaries, state encryption, and persistent secret-encryption keys.

**Files:** `deploy/aws/{versions.tf,README.md}` and the integration-owned root
`.tool-versions`, `deploy/aws/bootstrap/**` (incl. ECR — D11),
`deploy/aws/shared-email/**` (persistent ownership root excluded from UAT
destruction), `deploy/aws/envs/{staging,production}/**` (empty roots that only
pin providers/backends at this point), `.gitignore` additions for `.terraform/`,
`*.tfstate*`, `crash.log` (report the `.gitignore` diff to the integration owner
— additive only, never touching the global excludes).

**Steps:**

- [ ] Resolve and pin latest stable OpenTofu + AWS provider (record the exact
      versions and the resolution date in `deploy/aws/README.md`); commit
      `.terraform.lock.hcl` with linux_amd64 + linux_arm64 hashes. Verify the
      selected S3 locking setting and the ephemeral/write-only mechanisms
      against current official OpenTofu docs for the pinned versions (guardrail
      above); report incompatibilities and revise the task contract, do not
      improvise.
- [ ] Failing check first: add `deploy/aws/scripts/parity-check.sh`
      (shellcheck-clean) that (a) diffs `envs/staging` vs `envs/production`
      excluding `backend.hcl` + `*.auto.tfvars` and fails on any difference,
      **and (b) extracts the sorted top-level variable key set from each root's
      `*.auto.tfvars` and fails on any asymmetric key** (a production root
      silently omitting a variable staging sets is a parity failure, not a
      silent default). Run it against intentionally divergent stub roots and
      against stub tfvars with a missing key → observe both FAIL modes, then fix
      → PASS.
- [ ] Write `bootstrap/`: state bucket (versioned, SSE-KMS,
      public-access-blocked, `use_lockfile`-ready, **lifecycle rule expiring
      noncurrent versions after 90 d** — old state versions can embed
      rotated-out secret values, D2/D9); one persistent secrets KMS key per
      environment, retained outside disposable environment state; GitHub OIDC
      provider; **the four ECR repos `aboutme/{server,web,caddy,ops}`** with
      scan-on-push, tag immutability, and lifecycle keep-last-20 for disposable
      images (D11). Retain every UAT/promotion/rollback reference for task
      10.8's release-record window; test that cleanup excludes those images.
      Failing-first `tofu test` (mock provider) asserts the ECR properties and
      bucket lifecycle rule, distinct state/secrets keys, key rotation, and
      deletion protection; then `tofu fmt -check`, `tofu init -backend=false`,
      `tofu validate` all green.
- [ ] Define three staging GitHub roles: `ci-build-staging` may authenticate to
      and push only the four staging ECR repositories; `ci-plan-staging` may
      read only the staging backend/KMS data and describe or read staging-tagged
      resources needed for refresh/plan; `ci-deploy-staging` may use the staging
      backend, apply only staging-tagged resources, and pass only the exact
      staging ECS task/execution roles. Every trust policy requires
      `aud=sts.amazonaws.com` and
      `sub=repo:dannyota/aboutme-infra:environment:staging` for the planned
      private repository. Assert the public `aboutme` subject is rejected. If
      the resolved private repository owner/name changes, update the exact
      subject and tests together before activation. The protected GitHub
      `staging` environment admits only the approved branch and requires the
      recorded human reviewer for deploy/build jobs. Attach one explicit CI
      permissions boundary. Deny production and bootstrap mutation, IAM
      policy/role creation outside the declared module resources, unbounded
      `iam:PassRole`, and state-policy/KMS-policy mutation. Failing mocked tests
      compare the complete trust principal/conditions, action/resource sets, tag
      conditions, boundary ARN, and exact `PassRole` resources; labels such as
      “read-only” are not evidence.
- [ ] **Real AWS activation (explicit, operator-run, deferred):** only after the
      Phase 9 cost decision and infrastructure local checkpoint are `PASS` at
      the candidate commit, apply `bootstrap/` once with local state. The
      existing authorization record covers the future UAT scope and Cloudflare
      DNS. Resolve the real AWS AMI only during Task 10.15 activation. Record
      redacted outputs (bucket name, role ARNs, persistent secrets-key ARNs,
      repo URIs) in the phase ledger, and the import-recovery commands in the
      module README. No CI job ever applies bootstrap.
- [ ] Env roots: identical `main.tf` (module calls arrive in later tasks),
      `variables.tf`, `backend.hcl` per env, `staging.auto.tfvars` /
      `production.auto.tfvars` skeletons with identical key sets.
      `tofu init -backend=false && tofu validate` green in both; parity check
      green.

**Verification:** `tofu fmt -check -recursive deploy/aws`; per-root
`init -backend=false` + `validate`; `tofu test` in `bootstrap/`;
`bash deploy/aws/scripts/parity-check.sh`. After the activation gate, the
real-AWS portion records bootstrap-apply evidence in the ledger (this is the
only path to a remote state backend); a second `tofu plan` reports zero changes.
