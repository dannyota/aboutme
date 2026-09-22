# Pending login UI brief

Role: frontend. Model: `gpt-5.6-terra`.

## Objective and authority

Add the bilingual pending second-factor page for password and provider login,
with passkey assertion and recovery fallback. Read `AGENTS.md`,
`docs/design/second-factor-authentication.md`,
`docs/design/passkey-second-factor-contract.md`, web design, ADR 0048, the
accepted OpenAPI and generated types, and the integrated v0.4.1 localization
sources before editing.

## Owned paths

- Modify `apps/web/app/pages/login.vue`.
- Create `apps/web/app/pages/login/second-factor.vue`.
- Modify `apps/web/app/composables/usePasswordAuth.ts`.
- Create `apps/web/app/composables/secondFactorPending.ts`.
- Create `apps/web/app/utils/webauthn.ts`.
- Create `apps/web/app/i18n/second-factor.ts`.
- Modify `apps/web/app/i18n/meta.ts`.
- Modify `apps/web/test/login.test.ts`.
- Create `apps/web/test/second-factor-login.test.ts`.
- Create `apps/web/test/webauthn.test.ts`.

Do not edit settings files, capability composables, generated API types, source
manifest, browser specs, backend, design, or infrastructure. Do not add TOTP UI.

## Required behavior

- Password login distinguishes existing full success from the accepted pending
  result and navigates only to a validated pending path.
- Provider pending callbacks land on the same page without turning query text
  into copy, markup, or a request target.
- The pending client uses only the pending cookie and pending CSRF token. It
  does not reuse the normal session CSRF token or call `/me`.
- Convert only accepted canonical WebAuthn option and credential fields between
  unpadded base64url and browser byte arrays.
- Support passkey assertion, recovery fallback, expiry, attempt exhaustion,
  unsupported WebAuthn, user cancellation, rate limits, unavailable methods,
  safe retry, and validated return navigation.
- Treat any `methods` value this page does not know as unsupported. Show a
  localized refresh prompt beside the supported methods, or alone when none
  remain. Never call a route for an unknown method, call `/me`, or treat the
  pending cookie as a session. Test an unknown value alone and beside
  `recovery`.
- Render every label, error, loading state, warning, title, accessible name, and
  live announcement in Vietnamese and English.
- Keep recovery input and credential response data out of browser storage,
  analytics, URLs, and logs. Clear them when the view exits.

## Test-first cycle and checks

Start with `git status --short`. Add failing tests for 204 versus 202, password
and provider landings, invalid return paths, no normal session access, browser
cancel, malformed options, recovery cleanup, both locales, and locale changes
during a pending attempt. Do not run local tests, builds, lint, installs,
database writes, browsers, or development stacks. Report these checks as unrun
pending GitHub CI on the exact candidate:

```bash
(cd apps/web && npx vitest run test/login.test.ts test/second-factor-login.test.ts test/webauthn.test.ts)
(cd apps/web && npx eslint app/pages/login.vue app/pages/login/second-factor.vue app/composables/usePasswordAuth.ts app/composables/secondFactorPending.ts app/utils/webauthn.ts app/i18n/second-factor.ts app/i18n/meta.ts test/login.test.ts test/second-factor-login.test.ts test/webauthn.test.ts)
```

Definition of done: an enrolled user can finish password or provider login with
a passkey or one recovery code, while an unenrolled user sees unchanged login.
Report exact files, checks and results, skipped checks with command and reason,
and open items. Do not perform Git operations. Use short plain text with no em
dash.
