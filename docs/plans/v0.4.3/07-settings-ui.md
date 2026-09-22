# TOTP settings UI brief

Role: frontend. Model: `gpt-5.6-terra`.

## Objective and authority

Add localized TOTP enrollment, replacement, and removal to the existing factor
settings surface. Read `AGENTS.md`, the accepted TOTP and passkey contracts, web
design, accepted ADR 0049, the generated OpenAPI types, and the verified API
report.

## Owned paths

- Modify `apps/web/package.json` and `apps/web/package-lock.json` only to pin
  reviewed `uqr` 0.1.3 as a direct dependency.
- Modify `apps/web/app/components/settings/SecondFactorSettings.vue`.
- Modify `apps/web/app/composables/secondFactorSettings.ts`.
- Modify `apps/web/app/composables/useCapabilities.ts` and
  `apps/web/test/support/capabilities.ts`.
- Modify `apps/web/app/i18n/second-factor-settings.ts` and
  `apps/web/app/i18n/settings.ts` only for TOTP settings copy.
- Modify `apps/web/test/useCapabilities.test.ts`,
  `apps/web/test/second-factor-settings.test.ts`,
  `apps/web/test/sessions-settings.test.ts`, and
  `apps/web/test/sessions-csrf-gating.test.ts`.

Do not edit pending login files, generated API types, source manifest, browser
specs, backend, infrastructure, or design. Do not add a QR network request.

## Required behavior

- Read `totpEnabled` regardless of enrollment capability. Treat absent or
  malformed new capability and state fields as false during mixed versions.
- Start setup only when capability is true. Render the exact provisioning URI
  locally through pinned `uqr` and show the grouped secret as a copyable
  fallback. Never fetch, upload, log, or persist either value.
- Require code proof before first install or replacement. Make replacement
  clearly preserve the current credential until success.
- Support cancellation, expiry, invalid code, replay, rate limit, unavailable
  key service, session rotation, add, replace, mixed passkey state, removal, and
  final disablement.
- Clear secret, URI, QR data, and code on close, navigation, locale change that
  rebuilds the dialog, completion, failure that ends setup, logout, and unmount.
- Preserve recovery-code display and download behavior. Refresh `/me` and factor
  state after epoch-changing success before another mutation.
- Render phishing guidance, labels, warnings, errors, titles, focus, keyboard
  operation, accessible names, and live announcements in both locales and at
  phone and desktop widths.

## Hosted checks and report

Write failing dependency, capability, setup, local-QR, replacement, removal,
cleanup, recovery, locale, session-rotation, and accessibility tests first. Do
not run local installs, tests, builds, lint, database writes, browsers, or
stacks. Report these as unrun pending exact-candidate GitHub CI:

```bash
(cd apps/web && npm ci)
(cd apps/web && npx vitest run test/useCapabilities.test.ts test/second-factor-settings.test.ts test/sessions-settings.test.ts test/sessions-csrf-gating.test.ts)
(cd apps/web && npx eslint app/components/settings/SecondFactorSettings.vue app/composables/secondFactorSettings.ts app/composables/useCapabilities.ts app/i18n/second-factor-settings.ts app/i18n/settings.ts test/support/capabilities.ts test/useCapabilities.test.ts test/second-factor-settings.test.ts)
```

Definition of done: a user can add, replace, and remove TOTP through local-only
QR setup without retaining secret material or changing passkey and recovery
behavior. Report exact files and lockfile lines, dependency source and license,
checks and results, skipped commands and reason, and open items. Do not perform
Git operations. Use short plain text with no em dash.
