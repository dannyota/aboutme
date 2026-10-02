# Backlog

Open items that outlived their shipped plans. One line each, with the evidence that the item is still open. Delete a line when its work ships.

## Code

- Add Visual Studio Code to the MCP guide only after its client proof, using redirect `http://127.0.0.1:33418`. Evidence: [MCP client compatibility](../design/mcp-client-compatibility.md).
- Implement Client ID Metadata Documents once the owner settles the open decisions in the design. Evidence: [MCP Client ID Metadata Documents](../design/mcp-cimd.md).
- The Vietnam tech style guide conflicts with the samples on two points (Zalo line in English resumes; fresher awards as their own section). The designer decides and updates the guide or the samples. Evidence: `docs/design/vietnam-tech-resumes.md`.
- The test S3 images are Chainguard's MinIO rebuild pinned by digest; the free tier does not keep old digests forever, so a pin will stop pulling. Add a scheduled job that refreshes both digests and opens a change. Evidence: `scripts/test-s3.sh` (`MINIO_IMAGE`, `MC_IMAGE`), `deploy/compose.yml` (`media`, `media-init`).

## Production acceptance

- Run `scripts/deployment-document-check.sh` once on production and record a denied-call test for the observer role (it cannot describe another cluster). Evidence: [deployment transparency](../design/deployment-transparency/README.md), `docs/runbooks/observer.md`.
- Trigger every alarm once and confirm its email arrives (AC-OPS-019, `PLANNED`).
- Record one successful run of each of the five scheduled jobs. Evidence: `docs/architecture.md` production section.
- Restore a snapshot to a temporary instance, verify the data, delete the instance (AC-OPS-018, `PLANNED`).
- Record the open CloudFront facts on the live distribution. Evidence: [CloudFront runbook](../runbooks/cloudfront.md#facts-to-verify).

## Before the public announcement

- Move production to Vietnam ([ADR 0027](../adr/0027-vietnam-hosted-production.md)). Announce only after the move.
- Owner approves the agent-reviewed privacy, terms, and disclosure text.
- After the move and before real users: prepare and file the data protection impact assessment and the cross-border transfer dossier with the Ministry of Public Security. They are not filed now because there are no real users yet.

## Acceptance evidence gaps

- frontend: web test that an absent or malformed `totpEnabled` counts as false (AC-AUTH-029).
- devops: exact IAM policy tests or a live IAM simulation (AC-INF-004, AC-SEC-008).
- qa: retained production proof of fence state and TOTP with `scripts/totp-production-proof.sh totp-prod-enabled` (AC-SEC-007, AC-SEC-009); CloudFront edge probe (AC-OPS-015, AC-INF-002); direct-IP and spoofed-header probe (AC-INF-001, AC-OPS-002).
