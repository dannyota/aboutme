# Backlog

Open items that outlived their shipped plans. One line each, with the evidence that the item is still open. Delete a line when its work ships.

## Code

- `TestRealtimeRoutesDeliverCommittedDatabaseChangesThroughRealSessionMiddleware` failed once with "public stream remained open after revocation drain" (`apps/server/internal/realtime/service_test.go:539`); find the race. Evidence: deploy-fixes branch CI, 2026-09-24.
- The deploy warm-up requests `/danny`; if that resume is unpublished, every deploy fails. Evidence: `deploy/aws/scripts/deploy.sh` (`DEPLOY_WARM_PAGE`).
- The test S3 images are Chainguard's MinIO rebuild pinned by digest; the free tier does not keep old digests forever, so a pin will stop pulling. Add a scheduled job that refreshes both digests and opens a change. Evidence: `scripts/test-s3.sh` (`MINIO_IMAGE`, `MC_IMAGE`), `deploy/compose.yml` (`media`, `media-init`).

## Production acceptance

- Trigger every alarm once and confirm its email arrives (AC-OPS-019, `PLANNED`).
- Record one successful run of each of the five scheduled jobs. Evidence: `docs/architecture.md` production section.
- Restore a snapshot to a temporary instance, verify the data, delete the instance (AC-OPS-018, `PLANNED`).

## Before the public announcement

- Privacy, terms, and disclosure review (qualified privacy counsel and owner).

## Acceptance evidence gaps

- backend: TOTP code-step reuse across flows (AC-AUTH-026); TOTP replace or remove racing a code or recovery use, and a TOTP change racing session rotation (AC-AUTH-027).
- frontend: web test that an absent or malformed `totpEnabled` counts as false (AC-AUTH-029).
- devops: run `make caddy-prod-test` in CI (AC-INF-001, AC-OPS-002); exact IAM policy tests or a live IAM simulation (AC-INF-004, AC-SEC-008).
- qa: retained production proof of fence state and TOTP with `scripts/totp-production-proof.sh totp-prod-enabled` (AC-SEC-007, AC-SEC-009); Cloudflare edge probe (AC-OPS-015, AC-INF-002); direct-IP and spoofed-header probe (AC-INF-001, AC-OPS-002).
