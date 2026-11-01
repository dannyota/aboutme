# Backlog

Open items that outlived their shipped plans. One line each, with the evidence that the item is still open. Delete a line when its work ships.

## Code

- The account-deletion wrapper supports only release v0.7.9 (`deploy/vn/scripts/operations.sh:18`) and gates on `release-eligibility.sh --deploy-checkout v0.7.9` (line 39), so it refuses on hosts running v0.8.x. After the 2026-10-31 history rewrite, v0.7.9 also has no matching main push CI run. Decide the supported-release policy and gate on the deployed release. Evidence: `deploy/vn/scripts/operations.sh:18,39,64`.
- Some docs cite commit IDs from before the 2026-10-31 history rewrite that no longer resolve: the source links in `docs/design/vietnam-host-operations-delivery.md:425-430` and `docs/design/vietnam-host-operations.md:94`. Traceability rows and signed fixtures keep their original IDs as historical evidence. Evidence: those lines.
- Pixel baselines add about 3 MB of PNGs to history on every renderer change (601 versions reached 203 MB before the 2026-10-31 rewrite). Design a storage or lossless-compression change so history stops growing. Evidence: `apps/web/e2e/baselines/`.
- Diagnose any recurrence of `startup-empty-postgresql` failing once in VN run 39557891732. Later runs passed; the original cause remains unknown. Separate controlled measurements proved excessive JIT compilation for the retention candidate query and justified session-scoped JIT disablement, but did not attribute the original startup failure. Evidence: `deploy/vn/tests/crowdsec-retention-test.sh` and runs 39560225083, 39565347582, and 39573660507.
- Verify the notice fixture execution fix twice on the exact head in `vn-infra.yml` job `crowdsec-privacy-notice`. Run 41265028902 attempt 1 failed on the first copied host-helper invocation with exit 126, zero stdout bytes, and 133 stderr bytes. That size matches Bash's `Permission denied` diagnostic; a `noexec` mount under `/run` is inferred, since the log withheld stderr and mount flags. The fixture gives its private bind explicit `exec` permission and exercises a controlled `noexec` case; refusal diagnostics retain all fields and add fixed stderr categories. Main push runs 41195923984 and 41204396744 also failed; the earlier initial-child latch failure remains unattributed. Evidence: `deploy/vn/tests/crowdsec-privacy-notice-test.sh` (fixture mount, `refusal_details`, and initial child latch).
- The test S3 images are Chainguard's MinIO rebuild pinned by digest; the free tier does not keep old digests forever, so a pin will stop pulling. Add a scheduled job that refreshes both digests and opens a change. Evidence: `scripts/test-s3.sh` (`MINIO_IMAGE`, `MC_IMAGE`), `deploy/compose.yml` (`media`, `media-init`).
- The browser failure artifact uploads expected and actual images for every comparison when any test fails (197 PNGs, 45 MB for one timed-out test in run 38134235232); upload only the failing comparisons. Evidence: `scripts/collect-browser-diagnostics.py`.
- `contracts` is cancelled at its 20-minute job timeout when the npm registry is degraded: each `npm ci` took 300 s during the npm incident of 2026-10-22 (run 39711009477, attempt 1 and its rerun). Give `npm ci` a step-level timeout and one retry instead of letting the job budget absorb it. Evidence: `.github/workflows/ci.yml` (`contracts`) and status.npmjs.org for 2026-10-22.
- Driving the public join-invite proof with Playwright's clock (PR #112, branch `test/https-invite-clock`) failed `public-stage:fail-view-join-invite-assertion` in runs 41217154918 and 41219548484, before and after holding the view-start request until hydration. The runner reports only the stage, so add a stage name per assertion (mount, hidden before the floor, visible after it, placement, link) before the next attempt. Main keeps the real-time version. Evidence: those runs' `dev-https-proofs (passkey-recovery)` logs and `deploy/dev-https-browser/public.spec.ts`.
- The LinkedIn import proof failed once in WebKit at `linkedin-import-stage:webkit-failure-at-line-715` in run 41219548484, on a branch that changed only `public.spec.ts`; other runs on the same main passed. If it recurs, troubleshoot with both logs. Evidence: run 41219548484, job `dev-https-proofs (entry)`.
- Delete the `semgrep` environment, repository and Dependabot secrets, and the Semgrep project.

- Complete exact-candidate migration/reset CI and production database, retention, backup, and resource acceptance. Evidence: [remaining release work](crowdsec-postgresql.md) and the CrowdSec acceptance rows.

## Production acceptance

- Renew the free vMonitor Basic Synthetic Test quota each month and automate renewal once the `vngcloud` CLI supports it. The current quota is active from 2026-11-01T06:54:54Z to 2026-12-01T06:54:54Z; the next renewal window is 2026-11-24T06:54:54Z to 2026-12-01T06:54:54Z. The email quota was deleted. Evidence: [production runbook](../runbooks/production.md#alarms).
- Investigate recurring responses of about 5 seconds from HCM01. Evidence: vMonitor results for `aboutme-readyz` on 2026-11-01.

## Vietnam production

- Complete anonymous approval discovery with a throwaway environment before building: prove the remaining review cases and complete-cycle rate budget. One approval was visible anonymously on 2026-11-01. Evidence: [production pull deployment](../design/production-pull-deploy/README.md#approval-api-proof).

- Give the restore-drill server outbound networking without adding a Public NAT to the production VPC. Public NAT adds a `0.0.0.0/0` route for the whole VPC and sends every VM's outbound traffic through it.
- Then run the restore drill end to end and record it: the deletion contract and the state-bucket object proof are settled, and the remaining steps are in [restore drill delivery](./restore-drill.md). No successful drill is recorded. Evidence: [restore prerequisites](./traceability/vietnam-operations.md#restore-prerequisites) and [restore drill](../../deploy/vn/README.md#restore-drill).
- Diagnose readiness after a hard reset: the retained 503 timings are consistent with a cached failed browser probe. Failure recovery and closed initialization diagnostics await review and exact-head CI; the original Chromium failure reason remains unknown. Evidence: [historical readiness failure](./traceability/vietnam-operations.md#historical-readiness-failure) and CI run 38520217491, account proofs, privacy restart.
- Add a static-asset flood scenario with a measured threshold.
- Add and review a host procedure for TOTP key re-encryption.
- Add and review a host procedure for rotating the sign-in-to-view key.
- The deploy smoke bans `${SSH_CLIENT%% *}` and expects the HTTPS probe to answer 403, so it assumes one egress address for SSH and HTTPS. On 2026-10-22 the v0.8.11 deploy ran behind Cloudflare WARP, the two flows left through different addresses, the probe never saw 403, and the deployer left maintenance up with the app stopped although no migration had run. Ban the address the edge sees for the probe, or refuse to start when the two differ. Evidence: `deploy/vn/scripts/deploy.sh` (step 10) and the host journal for 2026-10-22 05:17 to 05:19 UTC.
- On 2026-10-22 `app-down` sent SIGTERM to the v0.8.10 Caddy, which waited on its eternal grace period until podman sent SIGKILL after 10 s and the unit's OnFailure alert fired; the v0.8.11 Caddy stopped in under a second at 05:18:42 UTC. Find what holds the grace period open and bound it below the stop timeout. Evidence: host journal for `aboutme-caddy.service`, 2026-10-22 05:16:59 to 05:17:12 UTC.

## Acceptance evidence gaps

- frontend: web test that an absent or malformed `totpEnabled` counts as false (AC-AUTH-029).
- qa: retained production proof of fence state and TOTP with `scripts/totp-production-proof.sh totp-prod-enabled` (AC-SEC-007, AC-SEC-009); retain a Vietnam direct-edge probe that reaches Caddy at the host IP with the apex hostname and confirms Caddy uses the socket client address and strips spoofed forwarding headers. Evidence: `deploy/caddy/production/edges/direct.sites`. Open: AC-INF-001 and AC-OPS-002 still describe AWS CloudFront origin restrictions and a rejected direct Elastic IP request.
