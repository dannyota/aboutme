# Task 1: Terraform skeleton, exact pinning, bootstrap stack (incl. shared ECR)

AC-INF-003 (parity substrate). Produces the layout every later task fills in.

**Files:** `deploy/aws/{versions.tf conventions,.terraform-version,README.md}`,
`deploy/aws/bootstrap/**` (incl. ECR — D11),
`deploy/aws/envs/{staging,production}/**` (empty roots that only pin
providers/backends at this point), `.gitignore` additions for `.terraform/`,
`*.tfstate*`, `crash.log` (report the `.gitignore` diff to the integration owner
— additive only, never touching the global excludes).

**Steps:**

- [ ] Resolve and pin latest stable Terraform + AWS provider (record the exact
      versions and the resolution date in `deploy/aws/README.md`); commit
      `.terraform.lock.hcl` with linux_amd64 + linux_arm64 hashes. Verify D2's
      `use_lockfile` and the ephemeral/write-only mechanisms against the pinned
      versions' docs (guardrail above); report mismatches, do not improvise.
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
      rotated-out secret values, D2/D9); GitHub OIDC provider; `ci-plan`
      read-only role; `ci-deploy-staging` role trust-scoped to this repo +
      environment; **the four ECR repos `aboutme/{server,web,caddy,ops}`** with
      scan-on-push, tag immutability, lifecycle keep-last-20 (D11).
      Failing-first `terraform test` (mock provider) asserts the ECR properties
      and the bucket lifecycle rule; then `terraform fmt -check`,
      `terraform init -backend=false`, `terraform validate` all green.
- [ ] **Real AWS activation (explicit, operator-run, deferred):** only after the
      P9 local-UAT report and independent evidence verdict are both `PASS` at
      the candidate commit and human AWS authorization is recorded, apply
      `bootstrap/` once with local state. Record redacted outputs (bucket name,
      role ARNs, repo URIs) in the phase ledger, and the import-recovery
      commands in the module README. No CI job ever applies bootstrap.
- [ ] Env roots: identical `main.tf` (module calls arrive in later tasks),
      `variables.tf`, `backend.hcl` per env, `staging.auto.tfvars` /
      `production.auto.tfvars` skeletons with identical key sets.
      `terraform init -backend=false && terraform validate` green in both;
      parity check green.

**Verification:** `terraform fmt -check -recursive deploy/aws`; per-root
`init -backend=false` + `validate`; `terraform test` in `bootstrap/`;
`bash deploy/aws/scripts/parity-check.sh`. After the activation gate, the
real-AWS portion records bootstrap-apply evidence in the ledger (this is the
only path to a remote state backend); a second `terraform plan` reports zero
changes.
