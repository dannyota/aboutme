# Task 8: Ops image + arm64 build workflow (registry consumed from bootstrap)

**Files:** `deploy/ops.Dockerfile`, `.github/workflows/images.yml` authored as a
diff for the integration owner. (ECR repositories live in
`deploy/aws/bootstrap/` — Task 1/D11; this task only consumes them. No
"implementer's choice" remains — review blocking 8.)

**Steps:**

- [ ] `deploy/ops.Dockerfile`: pinned alpine + `aws-cli`, `postgresql-client`,
      `bash`, and the ops scripts (`restore-verify.sh`, `cidr-drift-check.sh`)
      baked in; non-root. Failing-first: a trivial structure check (scripts
      present + executable + shellcheck run during build) that fails before the
      Dockerfile exists.
- [ ] Author `images.yml`: manual `workflow_dispatch` only,
      `runs-on: ubuntu-24.04-arm`, builds all four images (server, web, caddy —
      Task 7's Dockerfile — and ops) natively for arm64, pushes to the
      bootstrap-owned ECR **by digest** via the `ci-deploy-staging` OIDC role,
      emits a digest manifest artifact (JSON: image → digest) consumed by Task
      12's deploy workflow and, later, by P10's promotion (same digests, per the
      master plan). The workflow requires the AWS-authorized staging environment
      gate and cannot run on a tag, push, or pull request. No QEMU/buildx
      multi-arch matrix (D12). Hand the workflow diff to the integration owner;
      run `actionlint` on it before handoff.

**Verification:** podman builds succeed locally; `actionlint` clean. The actual
ECR push is exercised only after the activation gate, during the first staging
deploy in Task 14.
