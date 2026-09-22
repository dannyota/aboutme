# TOTP hosted browser proof brief

Role: qa. Model: `gpt-5.6-terra`.

## Objective and authority

Author the complete TOTP journey for the scripted hosted HTTPS harness at phone
and desktop widths. Read `AGENTS.md`, the accepted TOTP and passkey contracts,
accepted ADR 0049, OpenAPI, local UAT runbook, accepted traceability rows, and
every verified author report.

## Owned paths

- Modify `deploy/dev-https-browser/second-factor.spec.ts`.
- Create `deploy/dev-https-browser/totp-fixture.ts`.
- Modify `deploy/dev-https-browser/run.sh` and
  `deploy/dev-https-browser/static-test.sh`.
- Modify `scripts/lib/dev-https-lifecycle.sh`, `scripts/dev-https-test.sh`, and
  `scripts/dev-https-check.sh`.
- Modify the root `Makefile` only to add `dev-https-totp-check` and its phony
  entry.
- Produce bounded hosted evidence under `.dev/native-https/evidence/` for CI.

Do not edit product source, package files, images, design, generated files,
manifests, baselines, workflows, runbooks, or infrastructure. Do not fix product
defects. Report each defect with steps, evidence, severity, and owning role.

## Required proof

- Use a deterministic software TOTP helper with an injected clock. Never retain
  a generated secret, URI, QR payload, submitted code, key, cookie, or CSRF
  value in evidence.
- Prove first enrollment, one-time recovery display cleanup, local QR with no
  external request, pending password and every enabled provider login, TOTP
  completion, one recovery completion, and passkey coexistence.
- Prove previous, current, and next-step acceptance; same-step replay and
  concurrent use rejection; invalid code and Unicode digits; enrollment
  supersession; replacement; removal; final disablement; session and grant
  revocation; and password-reset preservation.
- Prove tampered and swapped ciphertext, unknown key, wrong session, wrong
  epoch, expired enrollment, exhausted attempts, and disabled enrollment fail
  closed. Use HTTP fixtures for states the browser cannot create.
- Prove flag-off hides setup and returns uniform 404 while verification,
  removal, state, passkeys, and recovery keep working.
- Run Vietnamese and English at `390x844` and `1440x900`. Check phishing copy,
  text fit, diacritics, focus, keyboard access, accessible names, announcements,
  cancellation, retry, and secret cleanup.
- Use unique fictional hosted-runner accounts. A test-level `finally` path
  removes factors, recovery plaintext, sessions, and account. The hosted job
  also tears down stack and runner-local database on every exit.

## Hosted checks and report

Write the failing scripted proof and static mode contract before harness
changes. Do not run local tests, builds, lint, installs, database writes,
browsers, stacks, or the shared database.

The docs job runs `make operational-test`, including static harness checks. It
must compile and list TOTP mode and prove isolation. The separate
`totp-browser-proof` job executes `make dev-https-totp-check` on the exact
candidate and uploads only `.dev/native-https/evidence/totp-*`. Static listing
is not acceptance evidence.

Definition of done: hosted evidence covers accepted TOTP, recovery, passkey,
revocation, flag, locale, and viewport cases with cleanup on every exit. Report
exact files, hosted checks and results or awaiting CI, artifact paths or none,
defects, skipped local commands and reason, cleanup evidence, and open items. Do
not perform Git operations. Use short plain text with no em dash.
