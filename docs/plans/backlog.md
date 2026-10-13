# Backlog

Open items that outlived their shipped plans. One line each, with the evidence that the item is still open. Delete a line when its work ships.

## Code

- Decide whether the Library (`/templates`) should get the showcase's filter sheet and rail. Evidence: [showcase layout](../design/ui/showcase.md).
- A showcase filter change reverted before its load finishes can announce the count once more on the next load. Evidence: `apps/web/app/pages/showcase.vue` count line.
- Two npm audit exceptions expire on 2026-11-01 (node-forge, braces); check for fixed releases before then. Evidence: `.github/npm-audit-exceptions.json`.
- Add Visual Studio Code to the MCP guide only after its client proof, using redirect `http://127.0.0.1:33418`. Evidence: [MCP client compatibility](../design/mcp-client-compatibility.md).
- Implement Client ID Metadata Documents once the owner settles the open decisions in the design. Evidence: [MCP Client ID Metadata Documents](../design/mcp-cimd.md).
- The Vietnam tech style guide conflicts with the samples on two points (Zalo line in English resumes; fresher awards as their own section). The designer decides and updates the guide or the samples. Evidence: `docs/design/vietnam-tech-resumes.md`.
- The test S3 images are Chainguard's MinIO rebuild pinned by digest; the free tier does not keep old digests forever, so a pin will stop pulling. Add a scheduled job that refreshes both digests and opens a change. Evidence: `scripts/test-s3.sh` (`MINIO_IMAGE`, `MC_IMAGE`), `deploy/compose.yml` (`media`, `media-init`).
- The browser failure artifact uploads expected and actual images for every comparison when any test fails (197 PNGs, 45 MB for one timed-out test in run 38134235232); upload only the failing comparisons. Evidence: `scripts/collect-browser-diagnostics.py`.
- `e2e/chrome.spec.ts:188` (hero sheet centered on phones, WebKit light vi) timed out once at 20 s in run 38134235232 and passed on the next run; five widths, each launching WebKit, navigating, and awaiting fonts, share one 20 s budget. If it recurs, troubleshoot with both runs' logs. Evidence: `apps/web/e2e/chrome.spec.ts`.
- An MCP fixture register failure (run 38115886452) now leaves a redacted `fixture-failure.json` in the `shard-evidence-mcp` artifact; on the next occurrence, check whether it is a 503 `authentication_unavailable` from the five-second Have I Been Pwned deadline. Evidence: `scripts/lib/mcp-workflow-fixture.sh`.

## Production acceptance

- Document safe trigger methods for the accepted Vietnam alarm inventory, exercise each detector or failure action, and record a matching mailbox receipt timestamp per alarm (AC-OPS-019, `PLANNED`). Per-alarm delivery evidence is missing. Evidence: [Vietnam operations evidence](./traceability/vietnam-operations.md#alarm-acceptance-remains-open).
- Investigate the GitHub uptime workflow cadence gap against its five-minute cron. Successful samples do not prove monitoring at the intended interval. Evidence: [external readiness monitoring collected 2026-10-13](./traceability/vietnam-operations.md#external-readiness-monitoring).

## Vietnam production

- Put the Cloudflare DNS records under OpenTofu. The owner edits them by hand today.
- Give the restore-drill server outbound networking without adding a Public NAT to the production VPC. Public NAT adds a `0.0.0.0/0` route for the whole VPC and sends every VM's outbound traffic through it.
- Then run the restore drill end to end and record it: no successful drill is recorded yet, so the backups are unproven. Evidence: [restore drill](../../deploy/vn/README.md#restore-drill).
- Diagnose readiness after a hard reset. Readiness stayed failed once until `aboutme-server` restarted.
- Add a static-asset flood scenario with a measured threshold.
- Add and review a host procedure for TOTP key re-encryption.
- Add and review a host procedure for abuse account deletion.
- Add and review a host procedure for rotating the sign-in-to-view key.

## Acceptance evidence gaps

- frontend: web test that an absent or malformed `totpEnabled` counts as false (AC-AUTH-029).
- qa: retained production proof of fence state and TOTP with `scripts/totp-production-proof.sh totp-prod-enabled` (AC-SEC-007, AC-SEC-009); retain a Vietnam direct-edge probe that reaches Caddy at the host IP with the apex hostname and confirms Caddy uses the socket client address and strips spoofed forwarding headers. Evidence: `deploy/caddy/production/edges/direct.sites`. Open: AC-INF-001 and AC-OPS-002 still describe AWS CloudFront origin restrictions and a rejected direct Elastic IP request.
