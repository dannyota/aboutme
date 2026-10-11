# Backlog

Open items that outlived their shipped plans. One line each, with the evidence that the item is still open. Delete a line when its work ships.

## Code

- The editor's sheet close control reads the English word "Close" to screen readers in both languages and is a 16 px target; give it a localized label and a 44 px target, as the showcase filter sheet has. Evidence: `apps/web/app/components/ui/sheet/SheetContent.vue`.
- Decide whether the Library (`/templates`) should get the showcase's filter sheet and rail. Evidence: [showcase layout](../design/ui/showcase.md).
- A showcase filter change reverted before its load finishes can announce the count once more on the next load. Evidence: `apps/web/app/pages/showcase.vue` count line.
- Two npm audit exceptions expire on 2026-11-01 (node-forge, braces); check for fixed releases before then. Evidence: `.github/npm-audit-exceptions.json`.
- Add Visual Studio Code to the MCP guide only after its client proof, using redirect `http://127.0.0.1:33418`. Evidence: [MCP client compatibility](../design/mcp-client-compatibility.md).
- Implement Client ID Metadata Documents once the owner settles the open decisions in the design. Evidence: [MCP Client ID Metadata Documents](../design/mcp-cimd.md).
- The Vietnam tech style guide conflicts with the samples on two points (Zalo line in English resumes; fresher awards as their own section). The designer decides and updates the guide or the samples. Evidence: `docs/design/vietnam-tech-resumes.md`.
- The test S3 images are Chainguard's MinIO rebuild pinned by digest; the free tier does not keep old digests forever, so a pin will stop pulling. Add a scheduled job that refreshes both digests and opens a change. Evidence: `scripts/test-s3.sh` (`MINIO_IMAGE`, `MC_IMAGE`), `deploy/compose.yml` (`media`, `media-init`).

## Production acceptance

- Trigger every alarm once and confirm its email arrives (AC-OPS-019, `PLANNED`).
- Record one successful run of each of the four Vietnam application jobs: `idempotency-expiry-sweep`, `media-deletion-sweep`, `privacy-retention-sweep`, and `media-orphan-sweep`. Evidence: [Vietnam scheduled jobs](../design/vietnam-production.md#scheduled-jobs), `deploy/vn/scripts/deploy-host.sh`. Open: AC-INF-006 still describes five AWS EventBridge jobs, including the retired `release-snapshot-sweep`.

## Vietnam production

- Put the Cloudflare DNS records under OpenTofu. The owner edits them by hand today.
- Give the restore-drill server outbound networking without adding a Public NAT to the production VPC. Public NAT adds a `0.0.0.0/0` route for the whole VPC and sends every VM's outbound traffic through it.
- Then run the restore drill end to end and record it: no successful drill is recorded yet, so the backups are unproven. Evidence: [restore drill](../../deploy/vn/README.md#restore-drill).
- Diagnose readiness after a hard reset. Readiness stayed failed once until `aboutme-server` restarted.
- Add a static-asset flood scenario with a measured threshold.
- Add and review a host procedure for TOTP key re-encryption.
- Add and review a host procedure for abuse account deletion.
- Add and review a host procedure for rotating the sign-in-to-view key.

## Before the public announcement

- Owner approves the agent-reviewed privacy, terms, and disclosure text.

## Acceptance evidence gaps

- frontend: web test that an absent or malformed `totpEnabled` counts as false (AC-AUTH-029).
- qa: retained production proof of fence state and TOTP with `scripts/totp-production-proof.sh totp-prod-enabled` (AC-SEC-007, AC-SEC-009); retain a Vietnam direct-edge probe that reaches Caddy at the host IP with the apex hostname and confirms Caddy uses the socket client address and strips spoofed forwarding headers. Evidence: `deploy/caddy/production/edges/direct.sites`. Open: AC-INF-001 and AC-OPS-002 still describe AWS CloudFront origin restrictions and a rejected direct Elastic IP request.
