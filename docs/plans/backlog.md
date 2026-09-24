# Backlog

Open items that outlived their shipped plans. One line each, with the evidence that the item is still open. Delete a line when its work ships.

## Code

- App HTML pages (`/`, `/login`, `/app/**`) send no Content-Security-Policy or `frame-ancestors`; only `/_harness/**` gets `HTML_CSP`. Public resume pages and Go routes already send one. Evidence: `apps/web/nuxt.config.ts` `routeRules`, `apps/web/app/utils/csp.ts`.

## Production acceptance

- Trigger every alarm once and confirm its email arrives (AC-OPS-019, `PLANNED`).
- Record one successful run of each of the five scheduled jobs. Evidence: `docs/architecture.md` production section.
- Restore a snapshot to a temporary instance, verify the data, delete the instance (AC-OPS-018, `PLANNED`).

## Before the public announcement

- SES production access. Evidence: `docs/runbooks/email.md` (account still in the SES sandbox).
- Product name and trademark review (owner).
- Privacy, terms, and disclosure review (qualified privacy counsel and owner).

## Acceptance evidence gaps

- backend: TOTP code-step reuse across flows (AC-AUTH-026); TOTP replace or remove racing a code or recovery use, and a TOTP change racing session rotation (AC-AUTH-027).
- frontend: web test that an absent or malformed `totpEnabled` counts as false (AC-AUTH-029).
- devops: run `make caddy-prod-test` in CI (AC-INF-001, AC-OPS-002); exact IAM policy tests or a live IAM simulation (AC-INF-004, AC-SEC-008); make `deploy.sh` accept only a green push run on `main` for the tag's commit, not a `workflow_dispatch` run at the same SHA.
- qa: retained production proof of fence state and TOTP with `scripts/totp-production-proof.sh totp-prod-enabled` (AC-SEC-007, AC-SEC-009); Cloudflare edge probe (AC-OPS-015, AC-INF-002); direct-IP and spoofed-header probe (AC-INF-001, AC-OPS-002).
