# Task 14: Authorized staging activation and evidence

This task closes “modules apply cleanly to staging.” It mutates AWS and
Cloudflare only after the recorded human authorization below.

## Preconditions

- [ ] P9 local UAT and its fresh evidence review are `PASS` at the exact
      candidate commit.
- [ ] The human owner records the AWS/Cloudflare resource scope, staging spend,
      and authorization. No bootstrap apply, image push, DNS change, staging
      apply, or workflow dispatch occurs before it.
- [ ] AWS access, the zone-scoped Cloudflare token, on-call destination, and
      protected GitHub `staging` environment are ready. Plaintext staging Basic
      credentials exist only in that protected environment/operator store.
- [ ] Every PI high-risk task has independent frozen tests and a fresh defect
      review. All local Terraform, policy, script, image, docs, and parity gates
      pass at the candidate commit.

## Staged first activation

Each transition is fail-closed and records a redacted saved-plan hash, command,
time, actor, and result. Production promotion uses the same stages.

1. Apply Task 1's bootstrap root once from local state. It creates the state
   backend, persistent state/secrets KMS keys, GitHub roles, and ECR. Record
   non-secret outputs and a zero-drift second plan.
2. Run `secrets-bootstrap.sh`, then its decrypting `--check`, against the
   persistent staging key. Build all four images through Task 8's protected
   workflow and record immutable digests.
3. Create and approve a foundation saved plan with `services_enabled=false` and
   `distribution_enabled=false`. Apply it to create network/host, private RDS
   and S3, ECS definitions, ACM certificate, and alarms without a long-running
   writer or CloudFront distribution.
4. Run `dns-apply.sh --apply-foundation` for the DNS-only origin A record and
   ACM validation CNAMEs. Wait with a bounded command until the certificate is
   `ISSUED`; a timeout leaves services and distribution disabled.
5. Run the named one-shot DB-bootstrap task. It creates the `aboutme` database
   and the migrator, app, and restore roles idempotently. Verify grants, default
   privileges, app DDL denial, and that only the bootstrap/migrate containers
   receive the migrator secret.
6. Dispatch Task 12 in first-activation mode. It proves zero writers, takes and
   verifies the initial snapshot, creates a fresh full saved plan with the
   issued certificate, distribution, exact invalidation policy/environment,
   enabled retention schedules, and services at one, then applies. Migrate must
   finish before Go starts. Wait for ECS stability and `/healthz` plus `/readyz`
   through CloudFront.
7. Run `dns-apply.sh --apply-aliases`, then verify apex service and the
   permanent `www` redirect. Finish with a zero-drift full plan. No single apply
   is claimed to cross the KMS, certificate, database-role, or first-service
   bootstrap boundaries.

## Live boundary checks

- [ ] Record EIP replacement within five minutes of the instance entering
      running, bootstrap AWS CLI version, bridge-gateway existence, encrypted
      gp3 root-volume settings, Caddy UID 10001, its sole
      `CAP_NET_BIND_SERVICE`, writable-directory owner/mode, and a live listener
      on 443.
- [ ] From web and every host-mode application container, IMDS at
      `169.254.169.254` is unreachable. A one-shot task under the server role
      can still obtain its ECS task credentials at `169.254.170.2` and call
      `sts:GetCallerIdentity`. Record no credentials or tokens.
- [ ] Exercise Nuxt SSR through the internal Caddy listener. Credential-free
      structured logs show the internal marker, path-only URI, request ID, and
      canonical bridge address. Sentinel OAuth query values, cookies, CSRF,
      Basic Authorization, origin secrets, and arbitrary query values are absent
      from captured Caddy and Go logs.
- [ ] Direct EIP traffic without the origin secret is refused or gets 403. A
      forged XFF through CloudFront cannot change the canonical rate-limit key.
      A viewer `X-Origin-Secret` is overwritten once. Origin logs show the
      origin FQDN, and CloudFront accepted the custom `allExcept` policy. Both
      origins use HTTPS/443/TLS 1.2 with hostname validation.
- [ ] Missing/wrong/repeated staging Basic credentials return the exact no-store
      401 challenge. Correct credentials reach the app, but `Authorization` is
      absent at Caddy. Every `www.aboutme.vn` path redirects to the apex with
      308 before auth/origin work and preserves semantic query parameters. HSTS
      and staging noindex headers are present.
- [ ] Authenticated privileged OAuth-start POST reaches Go with cookie, Origin,
      and CSRF intact; the matching GET reaches Go and returns 405. The edge
      introduces no Referer dependency.
- [ ] Inspect the private bucket: all public-access-block flags true,
      `IsPublic=false`, versioning disabled, and no website/ACL. CloudFront has
      no S3 origin, OAC, or `/assets/*`. IAM simulation proves only the server
      role can list `resumes/` and act on `resumes/*`; neighbouring prefixes and
      every other role are denied. The server invalidation action is scoped to
      this one distribution.
- [ ] All six Task 10 schedules are enabled, their image commands and roles
      resolve, and every heartbeat/failure alarm has a source. Run the safe TLS
      and CIDR checks once. Restore timing and notification receipt remain P9A
      criteria.

## Disposable staging rehearsal

Stop writers and record the approved data-loss scope. Empty only the named
staging media bucket, then destroy the disposable environment; bootstrap state,
persistent secrets KMS key, SSM parameters, and ECR remain. Verify RDS followed
the staging no-final-snapshot policy and no production resource was addressable.
Re-run all seven activation stages from empty environment state, including the
decrypting secret check, certificate validation, DB bootstrap, and full smoke.
Record whether staging is left up or down for cost control. Production keeps
deletion protection, a final snapshot, and a non-force-destroy bucket.

## Handoff and verification

Hand the integration owner exact diffs for `docs/architecture.md`, all runbook
seeds, AC-INF-001…008 evidence, and the master phase status. Run every prior
task's local checks plus docs gates and record the final candidate. PI does not
claim the P9A live CloudFront matrix, rotation, two-runner migration, timed
restore, alarm delivery, or SSE soak.
