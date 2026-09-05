# Task 10.15: Authorized hosted UAT activation and evidence

This task closes “modules apply cleanly to hosted UAT.” It mutates AWS and
Cloudflare only after the Phase 9 cost decision and the local checkpoint below.

## Preconditions

- [ ] Phase 9 records quantified cost, the budget decision, and selected UAT
      sizing at the exact candidate commit.
- [ ] The infrastructure local checkpoint is `PASS`: affected local checks, fake
      AMI and OpenTofu-mock checks, one author per task, one fresh Phase 10
      review, and the owner's single `make ci` plus `make scan` run.
- [ ] The existing authorization record names `uat.aboutme.vn`,
      `ap-southeast-1`, UAT resources, and Cloudflare DNS. It does not authorize
      production. No bootstrap apply, image push, DNS change, UAT apply, or
      workflow dispatch occurs before the cost result and local checkpoint.
- [ ] The refresh has resolved the UAT access mechanism, secret runtime
      contract, final Phase 6/7/8 settings, mail runtime and SES handoff, and
      the exact protected operator environment. Plaintext credentials remain
      outside this plan and follow the resolved contract.
- [ ] Before OpenTofu applies any overlapping SES, SNS, SQS, or CloudWatch
      resource, it adopts/imports the existing `aboutme-email` CloudFormation
      stack under the
      [single-owner transfer contract](contracts.md#existing-email-ownership),
      in persistent state excluded from UAT destruction. Preserve Google
      Workspace root MX/SPF and the separate `bounce.aboutme.vn` MAIL FROM
      records. No mail is sent during this documentation or local-checkpoint
      work.

## Staged first activation

Each transition is fail-closed and records a redacted saved-plan hash, command,
time, actor, and result. Production reuses the applicable stage shapes and
references the already-managed shared email root. It does not repeat one-time
bootstrap or email adoption.

1. Apply Task 10.1's bootstrap root once from local state. It creates the state
   backend, persistent state/secrets KMS keys, GitHub roles, and ECR. Record
   non-secret outputs and a zero-drift second plan. Resolve and record the real
   AWS AMI here; local tasks use fake AMI data only.
2. Run `secrets-bootstrap.sh`, then its decrypting `--check`, against the
   persistent UAT key. Build all four images through Task 10.8's protected
   workflow and record immutable digests.
3. Create and approve a foundation saved plan with `services_enabled=false` and
   `distribution_enabled=false`. Apply it to create network/host, private RDS
   and S3, ECS definitions, ACM certificate, and application alarms without a
   long-running writer or CloudFront distribution. Exclude existing mail-owned
   resources until the step 6 ownership handoff.
4. Run `dns-apply.sh --apply-foundation` for the DNS-only origin A record and
   ACM validation CNAMEs. Wait with a bounded command until the certificate is
   `ISSUED`; a timeout leaves services and distribution disabled.
5. Run the named one-shot DB-bootstrap task. It creates the `aboutme` database
   and the migrator, app, and restore roles idempotently. Verify grants, default
   privileges, app DDL denial, and that only the bootstrap/migrate containers
   receive the migrator secret.
6. Complete the controlled adoption/import of the existing `aboutme-email` stack
   before applying any overlapping SES, SNS, SQS, or CloudWatch resource.
   Execute the selected ownership-transfer runbook, verify the single manager
   for each resource, and require a no-change post-import plan before updates.
   Do not duplicate the stack. Keep the Google root MX/SPF and separate
   `bounce.aboutme.vn` MAIL FROM records.
7. Dispatch Task 10.12 in first-activation mode. It proves zero writers, takes
   and verifies the initial snapshot, creates a fresh full saved plan with the
   issued certificate, distribution, exact invalidation policy/environment,
   enabled retention schedules, and services at one, then applies. Migrate must
   finish before Go starts. Wait for ECS stability and `/healthz` plus `/readyz`
   through CloudFront.
8. Run `dns-apply.sh --apply-aliases`, then verify the `uat.aboutme.vn` service
   and configured canonical redirect. Finish with a zero-drift full plan. No
   single apply is claimed to cross the KMS, certificate, database-role, or
   first-service bootstrap boundaries.

At the hosted-UAT smoke stage, use the SES mailbox simulator. Send to a real
recipient only when the owner has approved the verified address and explicit
test-mail allowance. The simulator does not prove end-user links. Do not request
unrestricted production SES access until public HTTPS signup and contact flows
are live.

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
- [ ] The resolved UAT access policy rejects missing/invalid access and passes
      browser workflows without caching authentication responses. MCP receives
      exactly the intended Bearer header; no blanket Basic gate strips or
      replaces it. HSTS and UAT noindex are present. Canonical-host checks use
      only the recorded UAT host inventory; production host redirects remain
      local policy tests until Phase 11.
- [ ] Password reauthentication and MCP consent reach Go with cookie, Origin,
      and CSRF intact. Disabled provider routes remain unavailable for v1 and
      unused provider credentials are not needed at startup. The edge introduces
      no Referer dependency.
- [ ] Inspect the private bucket: all public-access-block flags true,
      `IsPublic=false`, versioning disabled, and no website/ACL. CloudFront has
      no S3 origin, OAC, or `/assets/*`. IAM simulation proves only the server
      role can list `resumes/` and act on `resumes/*`; neighbouring prefixes and
      every other role are denied. The server invalidation action is scoped to
      this one distribution.
- [ ] All six Task 10.10 schedules are enabled, their image commands and roles
      resolve, and every heartbeat/failure alarm has a source. Run the safe TLS
      and CIDR checks once. Restore timing and notification receipt remain Phase
      10 operational rehearsal criteria.

## Disposable UAT rehearsal

Stop writers and record the approved data-loss scope. Empty only the named UAT
media bucket, then destroy the disposable environment; bootstrap state,
persistent secrets KMS key, SSM parameters, and ECR remain. Verify RDS followed
the UAT no-final-snapshot policy and no production resource was addressable.
Re-run all eight activation stages from empty environment state, including the
decrypting secret check, certificate validation, DB bootstrap, and full smoke.
Leave the recreated UAT environment healthy for tasks 10.16–10.17 and run the
task 10.14 harness's live preflight. Cost-control shutdown or final teardown
occurs only after task 10.17 records hosted acceptance and the retention plan.
Production keeps deletion protection, a final snapshot, and a non-force-destroy
bucket.

## Handoff and verification

Hand the integration owner exact diffs for `docs/architecture.md`, all runbook
seeds, AC-INF-001…008 evidence, and the master phase status. Attach the existing
candidate's local-check results and live evidence; rerun checks only when a
change or unresolved failure invalidates them. Phase 10 infrastructure does not
claim the Phase 10 operational rehearsal live CloudFront matrix, rotation,
two-runner migration, timed restore, alarm delivery, or SSE soak.
