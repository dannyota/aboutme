# Task 4: Secrets & IAM module — SSM contract, scoped roles, bootstrap script

AC-INF-004.

**Files:** `deploy/aws/modules/secrets/**` (+ tests),
`deploy/aws/scripts/secrets-bootstrap.sh`, `docs/runbooks/secret-rotation.md`
seed, `.env.example` diff (names only) for the integration owner
(owner-serialized — see "Shared-file serialization").

Task 3 supplies the private media bucket ARN and the fixed `resumes/` prefix.
Per [ADR 0019](../../adr/0019-private-media-delivery.md), object existence never
grants read access and CloudFront has no media permission.

**SSM name contract (module-exported as outputs, single source):**

```text
/aboutme/<env>/db/master-password         (bootstrap-written; ephemeral TF read feeds password_wo only)
/aboutme/<env>/db/app-password            (bootstrap-written; server task secret)
/aboutme/<env>/db/migrator-password       (bootstrap-written; migrate container only)
/aboutme/<env>/ops/restore-password       (bootstrap-written; read-only restore-verification role)
/aboutme/<env>/edge/origin-secret-current
/aboutme/<env>/edge/origin-secret-next
/aboutme/<env>/edge/cloudflare-api-token  (caddy task secret, DNS-01 — Zone:DNS:Edit on aboutme.vn ONLY, D13)
/aboutme/<env>/edge/staging-gate-sha256   (SHA-256 of the exact Basic Authorization value; unused when disabled)
/aboutme/<env>/edge/cloudfront-origin-cidrs (PLAIN String, Terraform-written — drift baseline, D6; not a secret)
```

**Steps:**

- [ ] Failing `terraform test` (mocked) first: SSM/KMS permissions used by ECS
      `secrets`/`valueFrom` exist only on task **execution roles**. The server
      execution role reads exactly `/aboutme/<env>/db/app-password` and
      `/aboutme/<env>/db/migrator-password`, because the task contains separate
      app and migrate containers; only the migrate container references the
      latter. Caddy's reads the two origin-secret parameters and Cloudflare
      token; the ops execution role reads only the drift baseline and
      `/aboutme/<env>/ops/restore-password`. A dedicated one-shot DB-bootstrap
      execution role reads exactly the master, app, migrator, and restore
      passwords needed to create or rotate the three database roles. Each
      execution role also has only the matching KMS decrypt scope plus ECR pull
      and CloudWatch Logs. Task roles have no SSM secret-read grant, no
      long-running service can read another service's path, and no role has
      `ssm:GetParametersByPath` on `/aboutme` root.
- [ ] In the same failing test, pin media access to two server task-role
      statements:
  - `s3:ListBucket` applies only to the media bucket ARN and has a `StringLike`
    `s3:prefix` condition limited to `resumes/` and `resumes/*`.
  - `s3:GetObject`, `s3:PutObject`, and `s3:DeleteObject` apply only to
    `<media-bucket-arn>/resumes/*`.

  Reject `s3:*`, wildcard resources, other S3 actions, another bucket or prefix,
  and media permissions on the caddy, web, ops, execution, CI, or CloudFront
  principals. The scheduled orphan sweep uses the server image and server task
  role, so it needs no broader role.

- [ ] The server task role may call `cloudfront:CreateInvalidation` only on the
      Task 6 distribution ARN. No other task or CI role receives that action.
      The exact-policy tests reject wildcard distributions and neighbouring
      principals. Task 5 injects the matching distribution ID, and the P5A
      publish owner supplies bounded coalescing/retry behavior.

- [ ] Compare every role trust policy, not only attached permissions. ECS
      task/execution roles trust only `ecs-tasks.amazonaws.com` with exact
      `aws:SourceAccount` and the documented regional/account ECS ARN wildcard;
      the scheduler role and GitHub roles use their own exact principals and
      conditions. Do not invent a cluster-specific ECS source ARN that AWS does
      not support.

- [ ] Freeze the database-principal contract consumed by Task 8's bootstrap
      script: `aboutme_migrator` owns the application schema and may run goose
      DDL; `aboutme_app` receives only the exact runtime connect/schema/DML/
      sequence privileges, including default privileges for future migrator-
      owned objects, and cannot create, alter, or drop schema objects;
      `aboutme_restore_verify` is read-only. A live bootstrap test proves the
      app role cannot execute DDL and that the Go container neither receives the
      migrator environment variable nor can read its SSM name.

- [ ] Implement roles and consume the persistent per-environment secrets-key ARN
      from Task 1. Secret-valued parameters are **not** Terraform resources and
      are **not** data-sourced at plan time for mere existence (a plan-time read
      would pull values into state — the contradiction the round-1 review
      flagged): existence is verified exclusively by
      `secrets-bootstrap.sh --check`, and a value is read by Terraform only
      where it is actually consumed — ephemerally for `password_wo` (D10), and
      as a regular `sensitive` data source solely for the CloudFront custom
      header (D9's documented exception). The drift-baseline parameter is
      non-secret and IS a Terraform resource.
- [ ] `secrets-bootstrap.sh`: prompts/reads values (never echoes), writes
      SecureStrings idempotently (`--overwrite` only with an explicit flag),
      generates high-entropy origin secrets (`openssl rand -base64 32`). It
      writes every SecureString under the persistent key and excludes the
      staging Basic-auth plaintext, which exists only in the protected GitHub
      environment/operator secret source. Shellcheck-clean; a `--check` mode
      performs a decrypting read of every expected name without printing values
      and fails on a missing, undecryptable, or wrong-key parameter.
      Failing-first: run `--check` against a fake env name → nonzero exit.
- [ ] Seed `docs/runbooks/secret-rotation.md` with the D9 rotation sequence
      (origin secret current/next promotion; DB master rotation =
      bootstrap-write + bump `db_master_password_version`; app-password
      rotation; **note that the state bucket's noncurrent-version expiration
      (D2) is what ages rotated values out of old state versions**; the P9A
      drill hook — AC-OPS-016). Docs gates green.

**Verification:** `terraform test`, `validate`, shellcheck, parity, docs gates.
The IAM tests compare the media action, resource, principal, and prefix sets
exactly. After authorization and Task 1's bootstrap apply, the real-AWS
bootstrap-write and `--check` complete before Task 14 starts.
