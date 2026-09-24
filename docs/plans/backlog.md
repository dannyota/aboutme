# Backlog

Open items that outlived their shipped plans. One line each, with the evidence that the item is still open. Delete a line when its work ships.

## Code

- App HTML pages (`/`, `/login`, `/app/**`) send no Content-Security-Policy or `frame-ancestors`; only `/_harness/**` gets `HTML_CSP`. Public resume pages and Go routes already send one. Evidence: `apps/web/nuxt.config.ts` `routeRules`, `apps/web/app/utils/csp.ts`.

## Traceability

- AC-AUTH-020 to AC-AUTH-030 and AC-SEC-007 to AC-SEC-009 are still `PLANNED`, although passkeys shipped in v0.4.2 and TOTP in v0.4.7. Adjudicate each row against its tests and production proof. Evidence: `traceability/ac-auth.md`, `traceability/ac-sec.md`.
- AC-INF-001 to AC-INF-010 and AC-OPS-002, AC-OPS-015 to AC-OPS-017 still describe the superseded fleet and CloudFront target. Remap each to single-host production or mark it deferred with the reason; configuration alone proves no row. Evidence: `traceability/ac-inf.md`, `traceability/ac-ops.md`.

## Production acceptance

- Trigger every alarm once and confirm its email arrives (AC-OPS-019, `PLANNED`).
- Record one successful run of each of the five scheduled jobs. Evidence: `docs/architecture.md` production section.
- Restore a snapshot to a temporary instance, verify the data, delete the instance (AC-OPS-018, `PLANNED`).

## Before the public announcement

- SES production access. Evidence: `docs/runbooks/email.md` (account still in the SES sandbox).
- Product name and trademark review (owner).
- Privacy, terms, and disclosure review (qualified privacy counsel and owner).
