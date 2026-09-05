# Task 10.8: Ops image + arm64 build workflow (registry consumed from bootstrap)

**Files:** `deploy/ops.Dockerfile`, `deploy/aws/scripts/db-bootstrap.sh` plus
its fake-PostgreSQL tests, and `.github/workflows/images.yml` authored as a diff
for the integration owner in private `aboutme-infra`. Shared server/web sources
and Dockerfiles come from the explicit public app commit. Resolve the remaining
deployment-only paths under the
[repository boundary](README.md#repository-boundary) before dispatch. (ECR
repositories live in `deploy/aws/bootstrap/` — Task 10.1/D11; this task consumes
the proposed baseline. The Phase 9 managed-service and runtime refresh may
change this contract before dispatch.)

**Contract:**
[native ARM64 build and runner contract](contracts.md#build-and-runner-contract).
AMD64 browser baselines stay in the public app CI. This task owns deployment
image compatibility; it does not duplicate every app CI job on ARM64.

**Steps:**

- [ ] `deploy/ops.Dockerfile`: pinned alpine + `aws-cli`, `postgresql-client`,
      `openssl`, `bash`, and the ops scripts (`db-bootstrap.sh`,
      `restore-verify.sh`, `cidr-drift-check.sh`, `tls-expiry-check.sh`) baked
      in; UID 10004, read-only root at runtime. `db-bootstrap.sh` idempotently
      creates the `aboutme` database, then creates/rotates `aboutme_migrator`,
      `aboutme_app`, and `aboutme_restore_verify` with Task 10.4's exact
      grants/default privileges and proves the app role cannot DDL. The TLS
      script connects to `127.0.0.1:443` with SNI/hostname `var.origin_fqdn`,
      bounded connect/read timeouts, verifies the chain and hostname, parses
      remaining lifetime, and emits only the expiry metric or a nonzero failure.
      It never probes the public EIP, whose security group rejects the host.
      Failing-first tests use local fake AWS/Postgres/TLS endpoints, assert
      command arguments and secret-free output, run shellcheck, and verify every
      script is present and executable before the Dockerfile test can pass.
- [ ] Author `images.yml`: manual `workflow_dispatch` only, with a full app
      commit SHA and `publish` boolean defaulting to false. Define allowed
      candidate branches and required workflow/check identities in protected
      workflow configuration, not caller-supplied inputs. Require the app SHA to
      be the reviewed candidate in canonical `dannyota/aboutme`, reachable from
      an allowed protected candidate branch, with successful checks from those
      expected identities. The infrastructure workflow SHA must likewise be
      reviewed and reachable from its allowed protected branch. Reject arbitrary
      PR/fork-only commits even if they have green checks. Recheck this
      provenance before publication; check out the exact app commit and record
      both commits. Build all four images (server, web, Caddy from task 10.7,
      and ops) with Podman on `runs-on: ubuntu-24.04-arm`, targeting
      `linux/arm64`. Assert `runner.arch == 'ARM64'`, `uname -m` is `aarch64`,
      and each image reports Linux/arm64. Resolve ARM64-compatible base digests,
      tools, and native packages from the pinned sources. No QEMU or
      multi-architecture matrix.
- [ ] Separate credential-free build/smoke from publication. With
      `publish: false`, the workflow performs no AWS call or registry write.
      Native smoke uses synthetic fixtures and isolated test services: run
      server health/readiness and migrations, Nuxt SSR, Caddy config/boundary
      checks, and ops executable/version checks. Exercise one Phase 7 print
      fixture with the pinned Chromium and fonts, verify the produced PDF and
      image, and enforce the existing render resource limits. Missing ARM64
      Chromium/native packages or a failed smoke blocks publication. Keep exact
      AMD64 visual-baseline comparison in its existing job.
- [ ] Use architecture/tool/lockfile-specific dependency caches and reuse
      unchanged Podman build layers where supported by the pinned toolchain.
      Keep release caches isolated from untrusted PR cache writes. Start with
      sequential image builds within a job; record duration, peak memory, and
      disk use before increasing concurrency. Set timeouts and bounded artifact
      retention; canceled or failed builds cannot produce a deployable manifest.
- [ ] Publication requires `publish: true`, the activation handoff, and the
      protected `staging` environment on its approved branch. Only this job
      receives `id-token: write` and uses task 10.1's `ci-build-staging` OIDC
      role. Transfer the smoke-tested images as OCI archives from the build job,
      validate their artifact checksums and image identities, and push those
      same images to the bootstrap-owned ECR repositories without rebuilding.
      Resolve registry digests after push and verify `linux/arm64` before
      emitting the manifest. Serialize publication with
      `cancel-in-progress: false`; a tag, push, or PR cannot trigger it.
- [ ] Emit versioned JSON with `schema_version: 1`, `app_commit`,
      `infra_commit`, `build_run_id`, `build_run_attempt`, `platform` set to
      `linux/arm64`, and an `images` map with exactly `server`, `web`, `caddy`,
      and `ops`, each carrying its ECR repository and `sha256:` digest. Record
      runner image/version and smoke results alongside it. Task 10.12 consumes
      the manifest from this successful run; Phase 11 reuses the UAT-proven
      digests. Test rejection of missing images, wrong platform, malformed
      digests, failed/incomplete builds, and mismatched commits/run identity.
- [ ] Before Actions artifacts expire, the integration owner archives the exact
      manifest bytes, their SHA-256 checksum, successful build-run/attempt
      metadata, source approval record, and smoke evidence in a reviewed private
      `aboutme-infra` release record. Retain it through UAT, Phase 11 promotion,
      and the rollback window. The workflow remains read-only to GitHub; it does
      not commit this record. Protect every referenced UAT, promotion, and
      rollback image from ECR lifecycle cleanup for the same period under D11's
      retention rule. Task 10.12 accepts this protected archive after artifact
      expiry and never reconstructs the manifest from mutable tags. Test that an
      expired artifact with a valid archive works, while a missing/tampered
      archive fails.
- [ ] Hand the workflow diff and exact check commands to the integration owner;
      run `actionlint`. Task 10.13 incorporates workflow contract tests.

**Verification:** local Podman builds use the laptop architecture; local smoke
and workflow/manifest tests plus `actionlint` must pass. Before publication, the
owner observes the credential-free workflow build and smoke on native ARM64 and
records its runner/image evidence. The actual ECR push is exercised only after
the activation gate, during the first UAT deploy in task 10.15.
