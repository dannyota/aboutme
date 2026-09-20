# Passkey browser proof brief

Role: qa. Model: `gpt-5.6-terra`.

## Objective and authority

Prove the complete passkey and recovery journey in the scripted local HTTPS
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
- Save bounded ignored evidence under `.dev/native-https/evidence/`.

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
- Use fictional local accounts only. No production account or real credential
  may enter evidence.

## Exact checks

Announce any native-stack restart. Never stop the shared database. The top
manager grants and schedules each browser-heavy command one at a time:

```bash
bash scripts/dev-https-test.sh --static
bash deploy/dev-https-browser/static-test.sh
make dev-https-auth-check
make dev-https-password-check
make dev-https-passkey-check
make dev-https-mcp-check
make dev-https-entry-check
make dev-https-editor-check
make dev-https-public-check
make native-http-check
```

Finish every HTTPS command and stop its stack before starting the native HTTP
stack. The two stacks do not run together.

Definition of done: scripted evidence covers every accepted auth, recovery,
revocation, flag, locale, and viewport case without changing public output.
Report exact changed specs, commands and results, evidence paths, defects,
skipped checks with exact reason, and open items. Do not perform Git operations.
Use short plain text with no em dash.
