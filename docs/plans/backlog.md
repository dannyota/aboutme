# Backlog

Open items that outlived their shipped plans. One line each, with the evidence that the item is still open. Delete a line when its work ships.

## Code

- Diagnose any recurrence of `startup-empty-postgresql` failing once in VN run 39557891732. Later runs passed; the original cause remains unknown. Separate controlled measurements proved excessive JIT compilation for the retention candidate query and justified session-scoped JIT disablement, but did not attribute the original startup failure. Evidence: `deploy/vn/tests/crowdsec-retention-test.sh` and runs 39560225083, 39565347582, and 39573660507.
- Two npm audit exceptions expire on 2026-11-01 (node-forge, braces); check for fixed releases before then. Evidence: `.github/npm-audit-exceptions.json`.
- The test S3 images are Chainguard's MinIO rebuild pinned by digest; the free tier does not keep old digests forever, so a pin will stop pulling. Add a scheduled job that refreshes both digests and opens a change. Evidence: `scripts/test-s3.sh` (`MINIO_IMAGE`, `MC_IMAGE`), `deploy/compose.yml` (`media`, `media-init`).
- The browser failure artifact uploads expected and actual images for every comparison when any test fails (197 PNGs, 45 MB for one timed-out test in run 38134235232); upload only the failing comparisons. Evidence: `scripts/collect-browser-diagnostics.py`.
- `e2e/chrome.spec.ts:188` (hero sheet centered on phones, WebKit light vi) timed out once at 20 s in run 38134235232 and passed on the next run; five widths, each launching WebKit, navigating, and awaiting fonts, share one 20 s budget. If it recurs, troubleshoot with both runs' logs. Evidence: `apps/web/e2e/chrome.spec.ts`.

- Complete exact-candidate migration/reset CI and production database, retention, backup, and resource acceptance. Evidence: [remaining release work](crowdsec-postgresql.md) and the CrowdSec acceptance rows.

## Production acceptance

- Renew the free vMonitor Synthetic Test and email quotas each month, and automate renewal once the `vngcloud` CLI supports it; current expiries are Synthetic 2026-10-25 21:43 UTC and email 2026-11-13 03:10 UTC. Evidence: [external readiness monitoring](./traceability/vietnam-operations.md#external-readiness-monitoring).

## Vietnam production

- Give the restore-drill server outbound networking without adding a Public NAT to the production VPC. Public NAT adds a `0.0.0.0/0` route for the whole VPC and sends every VM's outbound traffic through it.
- Then run the restore drill end to end and record it: the deletion contract and the state-bucket object proof are settled, and the remaining steps are in [restore drill delivery](./restore-drill.md). No successful drill is recorded. Evidence: [restore prerequisites](./traceability/vietnam-operations.md#restore-prerequisites) and [restore drill](../../deploy/vn/README.md#restore-drill).
- Diagnose readiness after a hard reset: the retained 503 timings are consistent with a cached failed browser probe. Failure recovery and closed initialization diagnostics await review and exact-head CI; the original Chromium failure reason remains unknown. Evidence: [historical readiness failure](./traceability/vietnam-operations.md#historical-readiness-failure) and CI run 38520217491, account proofs, privacy restart.
- Add a static-asset flood scenario with a measured threshold.
- Add and review a host procedure for TOTP key re-encryption.
- Add and review a host procedure for rotating the sign-in-to-view key.

## Acceptance evidence gaps

- frontend: web test that an absent or malformed `totpEnabled` counts as false (AC-AUTH-029).
- qa: retained production proof of fence state and TOTP with `scripts/totp-production-proof.sh totp-prod-enabled` (AC-SEC-007, AC-SEC-009); retain a Vietnam direct-edge probe that reaches Caddy at the host IP with the apex hostname and confirms Caddy uses the socket client address and strips spoofed forwarding headers. Evidence: `deploy/caddy/production/edges/direct.sites`. Open: AC-INF-001 and AC-OPS-002 still describe AWS CloudFront origin restrictions and a rejected direct Elastic IP request.
