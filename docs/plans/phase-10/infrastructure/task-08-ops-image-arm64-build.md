# Task 10.8: Public ARM64 builds and private ECR publication

**Public files:** `deploy/ops.Dockerfile`, generic
`deploy/aws/scripts/db-bootstrap.sh` and fake-PostgreSQL tests, build/smoke and
bundle-validation harnesses, and `.github/workflows/images-arm64.yml`. Task 10.7
supplies public Caddy inputs; Task 10.10 supplies the other three public ops
scripts before the complete image build. All four images consume only the exact
public app commit.

**Private files:** `aboutme-infra` `.github/workflows/publish-images.yml`,
release-manifest validation, and private release-record format. Workflow edits
are authored as diffs for the integration owner. ECR repositories remain in
private bootstrap under Task 10.1/D11. Apply the
[repository boundary](README.md#repository-boundary) before dispatch.

**Contract:**
[native ARM64 build and runner contract](contracts.md#build-and-runner-contract).
AMD64 browser baselines stay in public app CI. This task proves deployment image
compatibility; it does not duplicate every app CI job on ARM64.

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
      It never probes a public node address, whose security group rejects the
      host. Failing-first tests use local fake AWS/Postgres/TLS endpoints,
      assert command arguments and secret-free output, run shellcheck, and
      verify every script is present and executable before the Dockerfile test
      can pass.
- [ ] Author public `images-arm64.yml`: manual `workflow_dispatch` only, with a
      full app commit SHA and no publication input. Define allowed candidate
      branches and required workflow/check identities in protected
      configuration, not caller inputs. Require a reviewed candidate in
      canonical `dannyota/aboutme`, reachable from an allowed protected branch,
      with successful checks from expected identities. Apply the same provenance
      rule to the public workflow commit. Reject PR/fork-only commits even with
      green checks. Check out the exact app commit and record source and
      workflow SHAs.
- [ ] Use one public build-and-smoke job. Build all four images sequentially
      with Podman on `ubuntu-24.04-arm`, targeting `linux/arm64`. Assert
      `runner.arch == 'ARM64'`, `uname -m` is `aarch64`, and every image reports
      Linux/arm64. Resolve compatible base digests, Chromium paths, tools,
      fonts, and native packages from pinned sources. No QEMU or
      multi-architecture matrix. The public workflow has no private checkout,
      AWS access, `id-token: write`, environment secrets, or registry write.
- [ ] In the same job, smoke the locally built images with synthetic fixtures
      and isolated services: server health, readiness and migrations, Nuxt SSR,
      Caddy config/boundary checks, and ops executable/version checks. Exercise
      one Phase 7 print fixture with pinned Chromium/fonts, verify PDF and image
      output, and enforce render resource limits. Failed smoke blocks
      publication. Keep exact AMD64 visual-baseline comparison in its existing
      job. Upload the final bundle/evidence only after all smoke succeeds; no
      intermediate OCI artifact transfer is needed.
- [ ] Use architecture/tool/lockfile-specific caches isolated from untrusted PR
      writes; cap public caches at 10 GiB. Private publication starts without
      caches. Record build duration, peak memory and disk before changing
      sequential builds or standard runners. Set job timeouts. Canceled or
      failed builds cannot authorize publication.
- [ ] After smoke succeeds, emit the public OCI bundle in the build contract.
      Version its JSON schema and set bounds on archive count, paths,
      compressed/unpacked bytes, and metadata before writing validators. Record
      exactly four archives and checksums, `app_commit`, source repository,
      workflow commit/path/ref, run ID/attempt, runner image/version,
      `linux/arm64`, OCI manifest/config digests, and smoke evidence. Upload
      with 14-day retention (7 days for the low scenario). Capture the returned
      artifact ID/digest separately; neither can be embedded inside its own
      upload. Include no AWS account, ECR endpoint, environment value, or
      private evidence.
- [ ] Private `publish-images.yml` accepts the approved app SHA and canonical
      public run/attempt/artifact IDs, not arbitrary URLs. A credential-free job
      fetches through GitHub's API and verifies successful completion, expected
      workflow/source provenance, service-reported artifact digest, internal
      checksums, safe archive contents, all four image identities, and smoke
      evidence. Its GitHub access is read-only; verify the public-artifact
      download method before introducing any additional credential.
- [ ] A separate publication job requires that validation, the activation
      handoff, reviewed private workflow commit, and protected `staging`
      approval. Only it receives `id-token: write` for `ci-publish-staging`.
      Recheck the approved bundle identity at the handoff; never execute bundle
      scripts or images with publication credentials. Push the validated OCI
      images to the four bootstrap ECR repositories without rebuilding. Verify
      remote platform, manifest/config and layers against the tested OCI content
      and record both source and ECR digests. Serialize with
      `cancel-in-progress: false`; a tag, push, PR, or public workflow event
      cannot trigger publication. Do not upload OCI copies to private Actions
      artifacts.
- [ ] Emit a versioned private ECR release manifest binding the public bundle
      checksum, artifact ID/digest, build repo/workflow/run/attempt and
      `app_commit` to `infra_commit`, private publication run/attempt, and an
      `images` map with exactly `server`, `web`, `caddy`, and `ops`, each
      carrying its ECR repository and `sha256:` digest for `linux/arm64`. Task
      10.12 consumes this successful private publication; Phase 11 reuses the
      UAT-proven digests.
- [ ] Test failed/canceled/incomplete runs, wrong repo/workflow/ref/attempt,
      green fork-only commits, missing/extra images, malformed digests, artifact
      substitution, unsafe paths and size limits. A valid bundle's checksums
      cannot rescue a failed or unapproved source run.
- [ ] Before artifacts expire, the owner archives the exact public bundle
      manifest, private release manifest, checksums, artifact/API identities,
      successful build/publication metadata, approvals, and smoke evidence in a
      reviewed private `aboutme-infra` release record. The workflows remain
      read-only to GitHub source; they never commit this record. Keep referenced
      ECR images through UAT, Phase 11 and rollback under D11. Task 10.12
      accepts this protected record after artifact expiry and never recreates it
      from tags. Test expired artifacts with valid records, and missing/tampered
      records. If an unpublished public bundle expires, rebuild publicly and
      repeat approval; no record can publish missing image bytes.
- [ ] Hand both workflow diffs and exact checks to the integration owner. Run
      `actionlint`; Task 10.13 incorporates the workflow contract tests.

**Verification:** local Podman builds/smoke use the laptop architecture.
Workflow, bundle, and manifest rejection tests plus `actionlint` must pass. The
owner observes the public credential-free native ARM64 build/smoke and records
its evidence before publication. ECR push runs only after the activation handoff
in Task 10.15. AWS OIDC policy tests reject every public build identity.
