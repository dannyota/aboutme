# Factor settings UI brief

Role: frontend. Model: `gpt-5.6-terra`.

## Objective and authority

Add localized passkey and recovery controls to the integrated v0.4.1 account
security page. Read `AGENTS.md`, `docs/design/second-factor-authentication.md`,
`docs/design/passkey-second-factor-contract.md`, web design, ADR 0048, the
accepted OpenAPI and generated types, and the final v0.4.1 settings and locale
sources before editing.

## Owned paths

- Modify `apps/web/app/pages/app/settings/sessions.vue`.
- Create `apps/web/app/components/settings/SecondFactorSettings.vue`.
- Create `apps/web/app/composables/secondFactorSettings.ts`.
- Modify `apps/web/app/composables/useCapabilities.ts`.
- Modify `apps/web/test/support/capabilities.ts`.
- Create `apps/web/app/i18n/second-factor-settings.ts`.
- Modify `apps/web/app/i18n/settings.ts`.
- Modify `apps/web/test/useCapabilities.test.ts`.
- Create `apps/web/test/second-factor-settings.test.ts`.
- Modify `apps/web/test/sessions-settings.test.ts`.
- Modify `apps/web/test/sessions-csrf-gating.test.ts`.
- Modify `apps/web/test/sessions-privileged-start-adversarial.test.ts`.

Do not edit login files, generated API types, source manifest, browser specs,
backend, design, or infrastructure. Do not add TOTP controls or QR code code.

## Required behavior

- Read factor state regardless of the enrollment capability. Hide only new
  enrollment starts when `passkeyEnrollment` is false or malformed.
- List passkeys in stable creation order with accepted safe timestamps and
  internal management IDs. Never expose credential IDs, keys, transports, or
  assertion material.
- Support options and completion, browser cancellation, passkey removal,
  deliberate final-factor removal, recovery count, recovery regeneration, and
  accepted recent reauthentication flow.
- Show first-enrollment and regenerated recovery codes once with accepted copy
  and download behavior. Remove plaintext from component state on close,
  navigation, unmount, logout, and failed continuation.
- Preserve settings drafts, focus, dialogs, and factor state during interface
  locale changes. Render all copy and accessible states in Vietnamese and
  English.
- Refresh session and factor state after epoch-changing success. Do not treat a
  rotated session as failure.

## Test-first cycle and checks

Start with `git status --short`. Add failing capability, state, enrollment,
removal, recovery, cleanup, locale, accessibility, and session-rotation tests.
Do not run local tests, builds, lint, installs, database writes, browsers, or
development stacks. Report these checks as unrun pending GitHub CI on the exact
candidate:

```bash
(cd apps/web && npx vitest run test/useCapabilities.test.ts test/second-factor-settings.test.ts test/sessions-settings.test.ts test/sessions-csrf-gating.test.ts test/sessions-privileged-start-adversarial.test.ts)
(cd apps/web && npx eslint app/pages/app/settings/sessions.vue app/components/settings/SecondFactorSettings.vue app/composables/secondFactorSettings.ts app/composables/useCapabilities.ts app/i18n/second-factor-settings.ts app/i18n/settings.ts test/support/capabilities.ts test/useCapabilities.test.ts test/second-factor-settings.test.ts)
```

Definition of done: a user can add, list, remove, recover, regenerate, copy, and
download through localized accessible controls without retaining plaintext.
Report exact files, including every affected session test, checks and results,
skipped checks with command and reason, and open items. Do not perform Git
operations. Use short plain text with no em dash.
