# Task 4: Secrets & IAM module — SSM contract, scoped roles, bootstrap script

AC-INF-004.

**Files:** `deploy/aws/modules/secrets/**` (+ tests),
`deploy/aws/scripts/secrets-bootstrap.sh`, `docs/runbooks/secret-rotation.md`
seed, `.env.example` diff (names only) for the integration owner
(owner-serialized — see "Shared-file serialization").

**SSM name contract (module-exported as outputs, single source):**

```text
/aboutme/<env>/db/master-password         (bootstrap-written; ephemeral TF read feeds password_wo only)
/aboutme/<env>/db/app-password            (bootstrap-written; server task secret)
/aboutme/<env>/edge/origin-secret-current
/aboutme/<env>/edge/origin-secret-next
/aboutme/<env>/edge/cloudflare-api-token  (caddy task secret, DNS-01 — Zone:DNS:Edit on aboutme.vn ONLY, D13)
/aboutme/<env>/edge/staging-gate-htpasswd (viewer-gate credential hash, D25; unused when the gate is disabled)
/aboutme/<env>/edge/cloudfront-origin-cidrs (PLAIN String, Terraform-written — drift baseline, D6; not a secret)
```

**Steps:**

- [ ] Failing `terraform test` (mocked) first: the server task role's SSM policy
      resource list is exactly `/aboutme/<env>/db/app-password`; the caddy task
      role's is exactly the two origin-secret params + the cloudflare token;
      **neither role can read the other's path**; the ops task role reads only
      the drift baseline + what the restore drill needs; the execution roles
      hold ECR pull + CloudWatch Logs + their own `secrets` only; nothing grants
      `ssm:GetParametersByPath` on `/aboutme` root.
- [ ] Implement roles + KMS key. Secret-valued parameters are **not** Terraform
      resources and are **not** data-sourced at plan time for mere existence (a
      plan-time read would pull values into state — the contradiction the
      round-1 review flagged): existence is verified exclusively by
      `secrets-bootstrap.sh --check`, and a value is read by Terraform only
      where it is actually consumed — ephemerally for `password_wo` (D10), and
      as a regular `sensitive` data source solely for the CloudFront custom
      header (D9's documented exception). The drift-baseline parameter is
      non-secret and IS a Terraform resource.
- [ ] `secrets-bootstrap.sh`: prompts/reads values (never echoes), writes
      SecureStrings idempotently (`--overwrite` only with an explicit flag),
      generates high-entropy origin secrets (`openssl rand -base64 32`).
      Shellcheck-clean; a `--check` mode verifies all names exist without
      printing values. Failing-first: run `--check` against a fake env name →
      nonzero exit.
- [ ] Seed `docs/runbooks/secret-rotation.md` with the D9 rotation sequence
      (origin secret current/next promotion; DB master rotation =
      bootstrap-write + bump `db_master_password_version`; app-password
      rotation; **note that the state bucket's noncurrent-version expiration
      (D2) is what ages rotated values out of old state versions**; the P9A
      drill hook — AC-OPS-016). Docs gates green.

**Verification:** `terraform test`, `validate`, shellcheck, parity, docs gates.
After authorization and Task 1's bootstrap apply, the real-AWS bootstrap-write
and `--check` complete before Task 14 starts.
