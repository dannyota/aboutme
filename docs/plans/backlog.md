# Backlog

Open items that outlived their shipped plans. One line each, with the evidence that the item is still open. Delete a line when its work ships.

## Code

- `/app/settings/sessions` has no redirect of its own for an anonymous visitor; it leaves only because `ConnectedAgents.vue` mounts when agent access is on. Give app routes one shared signed-out redirect. Evidence: `apps/web/app/pages/app/settings/sessions.vue`, `apps/web/app/composables/useResumeList.ts`.
- `hidePhoneAccountLinks` in `apps/web/app/components/app/AppShell.vue` keeps an exception for `/app/settings/sessions` and `/authorize` that can no longer be reached; remove it and update `DESIGN.md` if it mentions it.
- `docs/design/vietnam-production.md` contradicts itself: lines 150-151 say the two Caddy units conflict in systemd, while steps 5 and 8 run them side by side (SO_REUSEPORT). Also cover ACME HTTP-01 with two Caddy processes on port 80 (shared certificate storage), same-user units, and check `/readyz` on the server directly in step 8. Evidence: deploy handoff review, 2026-09-25.
- `docs/design/single-host-production.md:268` and `docs/runbooks/production.md:257` omit the new recovery exceptions: an unconfirmed new app is left running when maintenance cannot be confirmed, and the failed previous-app restart path. Evidence: `deploy/aws/scripts/deploy.sh` restore.
- The paged editor preview holds less than the PDF on some templates (minimal-air spilled to two pages at 65% print fill while the PDF fit one), so a user can see two preview pages for a one-page PDF. Evidence: Vietnam tech samples work, 2026-09-25; `apps/web/e2e/preview-gap-expected.json`.
- Template date formats ignore the resume language: the style guide wants `MM/YYYY` for Vietnamese and `Mon YYYY` for English, and international-lang's `YYYY` drops months. Evidence: `docs/design/vietnam-tech-resumes.md`, `packages/schema/samples/international-lang.*`.
- The Vietnam tech style guide conflicts with the samples on two points (Zalo line in English resumes; fresher awards as their own section). The designer decides and updates the guide or the samples. Evidence: `docs/design/vietnam-tech-resumes.md`.
- A stray dot sits on the stored Executive Band page 1 image (Vietnamese), about 60 px above the bottom edge; check the print path. Evidence: `apps/web/public/templates/pages/executive-band-vi-p1.png`.
- The deploy warm-up requests `/danny`; if that resume is unpublished, every deploy fails. Evidence: `deploy/aws/scripts/deploy.sh` (`DEPLOY_WARM_PAGE`).
- The test S3 images are Chainguard's MinIO rebuild pinned by digest; the free tier does not keep old digests forever, so a pin will stop pulling. Add a scheduled job that refreshes both digests and opens a change. Evidence: `scripts/test-s3.sh` (`MINIO_IMAGE`, `MC_IMAGE`), `deploy/compose.yml` (`media`, `media-init`).

## Production acceptance

- Trigger every alarm once and confirm its email arrives (AC-OPS-019, `PLANNED`).
- Record one successful run of each of the five scheduled jobs. Evidence: `docs/architecture.md` production section.
- Restore a snapshot to a temporary instance, verify the data, delete the instance (AC-OPS-018, `PLANNED`).

## Before the public announcement

- Privacy, terms, and disclosure review (qualified privacy counsel and owner).

## Acceptance evidence gaps

- frontend: web test that an absent or malformed `totpEnabled` counts as false (AC-AUTH-029).
- devops: run `make caddy-prod-test` in CI (AC-INF-001, AC-OPS-002); exact IAM policy tests or a live IAM simulation (AC-INF-004, AC-SEC-008).
- qa: retained production proof of fence state and TOTP with `scripts/totp-production-proof.sh totp-prod-enabled` (AC-SEC-007, AC-SEC-009); Cloudflare edge probe (AC-OPS-015, AC-INF-002); direct-IP and spoofed-header probe (AC-INF-001, AC-OPS-002).
