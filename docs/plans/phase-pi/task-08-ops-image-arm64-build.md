# Task 8: Ops image + arm64 build workflow (registry consumed from bootstrap)

**Files:** `deploy/ops.Dockerfile`, `deploy/aws/scripts/db-bootstrap.sh` plus
its fake-PostgreSQL tests, and `.github/workflows/images.yml` authored as a diff
for the integration owner. (ECR repositories live in `deploy/aws/bootstrap/` —
Task 1/D11; this task only consumes them. No "implementer's choice" remains —
review blocking 8.)

**Steps:**

- [ ] `deploy/ops.Dockerfile`: pinned alpine + `aws-cli`, `postgresql-client`,
      `openssl`, `bash`, and the ops scripts (`db-bootstrap.sh`,
      `restore-verify.sh`, `cidr-drift-check.sh`, `tls-expiry-check.sh`) baked
      in; UID 10004, read-only root at runtime. `db-bootstrap.sh` idempotently
      creates the `aboutme` database, then creates/rotates `aboutme_migrator`,
      `aboutme_app`, and `aboutme_restore_verify` with Task 4's exact
      grants/default privileges and proves the app role cannot DDL. The TLS
      script connects to `127.0.0.1:443` with SNI/hostname `var.origin_fqdn`,
      bounded connect/read timeouts, verifies the chain and hostname, parses
      remaining lifetime, and emits only the expiry metric or a nonzero failure.
      It never probes the public EIP, whose security group rejects the host.
      Failing-first tests use local fake AWS/Postgres/TLS endpoints, assert
      command arguments and secret-free output, run shellcheck, and verify every
      script is present and executable before the Dockerfile test can pass.
- [ ] Author `images.yml`: manual `workflow_dispatch` only,
      `runs-on: ubuntu-24.04-arm`, builds all four images (server, web, caddy —
      Task 7's Dockerfile — and ops) natively for arm64, pushes to the
      bootstrap-owned ECR **by digest** via Task 1's `ci-build-staging` OIDC
      role, emits a digest manifest artifact (JSON: image → digest) consumed by
      Task 12's deploy workflow and, later, by P10's promotion (same digests,
      per the master plan). The workflow requires the AWS-authorized staging
      environment gate and cannot run on a tag, push, or pull request. No
      QEMU/buildx multi-arch matrix (D12). Hand the workflow diff to the
      integration owner; run `actionlint` on it before handoff.

**Verification:** podman builds succeed locally; `actionlint` clean. The actual
ECR push is exercised only after the activation gate, during the first staging
deploy in Task 14.
