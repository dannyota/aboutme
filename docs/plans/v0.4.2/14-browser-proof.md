# Passkey browser proof brief

Role: qa. Model: `gpt-5.6-terra`.

## Objective and authority

Author the complete passkey and recovery journey for the scripted hosted HTTPS
harness at phone and desktop widths. Read `AGENTS.md`,
`docs/design/second-factor-authentication.md`,
`docs/design/passkey-second-factor-contract.md`, ADR 0048, the accepted API
contract, local UAT runbook, accepted traceability rows, and every verified
author report before editing.

## Owned paths

- Modify `deploy/dev-https-browser/auth.spec.ts`.
- Modify `deploy/dev-https-browser/password-auth.spec.ts`.
- Modify `deploy/dev-https-browser/privacy.spec.ts`.
- Modify `deploy/dev-https-browser/mcp.spec.ts`.
- Create `deploy/dev-https-browser/second-factor.spec.ts`.
- Modify `deploy/dev-https-browser/run.sh`.
- Modify `deploy/dev-https-browser/static-test.sh`.
- Modify `scripts/lib/dev-https-lifecycle.sh`.
- Modify `scripts/dev-https-test.sh`.
- Modify `scripts/dev-https-check.sh`.
- Modify the root `Makefile` only to add the dedicated `dev-https-passkey-check`
  target and its phony entry.
- Produce bounded hosted evidence under `.dev/native-https/evidence/` for CI to
  upload.

Do not edit product source, package files, images, design, generated files,
manifests, or baselines. Limit lifecycle and harness edits to passing the
explicit enrollment flag and staging, validating, and running the new bounded
passkey spec. Do not fix defects. Report each defect with steps, evidence,
severity, and owning role.

## Required proof

- Enroll the first passkey through a virtual authenticator, record the one-time
  recovery display, close it, and prove the plaintext leaves the page state.
- Prove password and every enabled provider enter pending login without a
  session. Complete one with passkey and one with recovery.
- Prove wrong origin, missing user verification, replay, wrong binding, stale
  epoch, exhausted attempts, used recovery, and concurrent completion fail
  closed. Use HTTP fixtures for browser states a real authenticator cannot
  produce.
- Prove factor addition, recovery regeneration, one removal, final removal,
  session rotation, other-session revocation, connected-agent revocation, and
  password reset preservation.
- Prove enrollment stays hidden and returns the accepted disabled response when
  the flag is off, while verification, removal, state, and recovery still work
  for existing synthetic rows.
- Run Vietnamese and English at `390x844` and `1440x900`. Check text fit,
  diacritics, focus, keyboard access, accessible names, live announcements,
  cancellation, retry, and safe return paths.
- Use fictional hosted-runner accounts only. No production account or real
  credential may enter evidence.
- Give each run a unique fictional account marker. Use a test-level `finally`
  path to remove the run's factors, recovery plaintext, sessions, and account on
  success and failure. The hosted job also tears down the stack and its
  runner-local database with `if: always()` so cancellation leaves no state.
- Keep uploaded evidence bounded and secret-free. Do not retain cookies, CSRF
  values, recovery codes, credential material, email access, or authenticator
  private state.

## Hosted checks

Start with `git status --short`. Add the failing scripted proof and its static
mode-selection contract before the harness changes. Do not run local tests,
builds, lint, installs, database writes, browsers, development stacks, or the
shared database. Report these checks as unrun pending GitHub CI on the exact
candidate.

The docs job runs `make operational-test`, including
`scripts/dev-https-test.sh --static` and
`deploy/dev-https-browser/static-test.sh`. The static path must compile and list
the passkey mode and prove mode isolation. It is not acceptance evidence.

The separate `passkey-browser-proof` job starts the repository HTTPS harness,
builds the pinned browser image, executes `make dev-https-passkey-check` against
the exact candidate, uploads the bounded fictional evidence, and always stops
the stack and runner-local database. It uploads only
`.dev/native-https/evidence/passkey-*`. The manager verifies the job SHA and
artifact path. No local or production browser run substitutes for this job.

Definition of done: scripted evidence covers every accepted auth, recovery,
revocation, flag, locale, and viewport case without changing public output, and
the exact-candidate hosted job executes the proof with cleanup on every exit.
Report exact changed specs and harness files, hosted checks and results or
awaiting CI, artifact paths or `none`, defects, local checks not run with exact
reason, cleanup evidence, and open items. Do not perform Git operations. Use
short plain text with no em dash.
