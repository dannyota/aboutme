# TOTP pending login UI brief

Role: frontend. Model: `gpt-5.6-terra`.

## Objective and authority

Add authenticator-code completion to the existing bilingual pending page. Read
`AGENTS.md`, the accepted TOTP and passkey contracts, the key-management design,
web design, accepted ADR 0049, the generated OpenAPI types, and the verified API
report.

## Owned paths

- Modify `apps/web/app/pages/login/second-factor.vue`.
- Modify `apps/web/app/composables/secondFactorPending.ts`.
- Modify `apps/web/app/i18n/second-factor.ts` and `apps/web/app/i18n/meta.ts`
  only for pending TOTP copy and title.
- Modify `apps/web/test/second-factor-login.test.ts`.

Do not edit login primary-auth files, settings files, capability composables,
generated types, package files, manifests, browser specs, backend,
infrastructure, or design.

## Required behavior

- Render methods in server order and permit passkey, TOTP, or recovery without
  treating the pending cookie as a session.
- Accept exactly six ASCII digits. Keep the code in component memory only and
  clear it after every submit outcome, navigation, expiry, exhaustion, logout,
  and unmount.
- Handle invalid and replayed code, rate limits including a TOTP cool-down with
  `Retry-After` up to 24 hours, `503` from a TOTP key failure,
  `404 factor_not_found`, expired pending state, exhausted attempts, method
  changes, locale changes, and safe retry through closed localized copy. Offer
  the other listed methods during a TOTP cool-down or key failure.
- An older page that sees unknown `totp` and no method it supports must request
  refresh and must not call `/me`, invent a route, or issue authority.
- Render phishing guidance, labels, errors, loading state, accessible name,
  focus, and live announcements in Vietnamese and English.

## Hosted checks and report

Write failing code-shape, method-order, cleanup, error, older-client, locale,
focus, and accessibility tests first. Do not run local tests, builds, lint,
installs, database writes, browsers, or stacks. Report these as unrun pending
exact-candidate GitHub CI:

```bash
(cd apps/web && npx vitest run test/second-factor-login.test.ts)
(cd apps/web && npx eslint app/pages/login/second-factor.vue app/composables/secondFactorPending.ts app/i18n/second-factor.ts app/i18n/meta.ts test/second-factor-login.test.ts)
```

Definition of done: pending password and provider flows can complete with TOTP
without changing passkey or recovery behavior or retaining code material. Report
exact files and hunks, checks and results, skipped commands and reason, and open
items. Do not perform Git operations. Use short plain text with no em dash.
Code, tests, comments, and living docs never cite plans, tasks, phases, or
review findings; cite the design, ADR, or `AC-*` ID instead.
