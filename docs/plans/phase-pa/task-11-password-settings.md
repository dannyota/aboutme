# Task 11 — Add password credential controls to account settings

**Acceptance:** AC-AUTH-012, AC-AUTH-016.

**Depends on:** T02, T05 `/me.hasPassword`, T08 routes, T10 shared password
field/composable, and integrated P4 app shell.

**Owned paths:** T11 paths in `file-structure.md`.

## Contract

`AuthUser` adds required `hasPassword: boolean`. `MutateOptions.method` adds
`PUT`; CSRF rotation retry remains one retry only. `PasswordSettings` receives
current status and exact actions rather than reading ambient auth state:

```ts
interface PasswordSettingsProps {
  hasPassword: boolean;
}
interface PasswordSettingsActions {
  reauthenticate(password: string): Promise<void>;
  setPassword(password: string): Promise<void>;
  startProviderReauth(provider: AuthProvider): Promise<void>;
}
```

Provider-only users select an existing linked provider for reauth, then add a
password. Password users may reauth with current password and change it. The
existing provider-link UI remains independent. No provider email is shown. After
successful add/change, refresh `/me`, show `hasPassword = true`, and rely on the
server-set fresh cookie; every other session is gone.

## TDD cycle

- [ ] Add `useAuth` REDs for required `hasPassword`, authenticated PUT with
      JSON+CSRF, one refresh/retry on `csrf_rejected`, and no retry for other
      failures.
- [ ] Add settings REDs for provider-only false, password true, linked provider
      selection, provider callback reauth, password reauth, 403 reauth-required
      recovery, add/change policy issues, pending/duplicate guards, exact
      success status, `/me` refresh, and session-list refresh.
- [ ] Preserve existing session revoke/logout/provider-link tests and add an
      assertion that provider-reported email never renders.
- [ ] Add focus/error-summary and theme/accessibility REDs: labels,
      autocomplete, keyboard, dialog return focus, busy announcement, and safe
      server issue mapping.
- [ ] Run expected RED:

  ```sh
  cd apps/web && npx vitest run \
    test/useAuth.test.ts test/sessions.test.ts \
    test/password-settings.test.ts
  ```

- [ ] Implement the smallest additions to `useAuth`, sessions page, and
      `PasswordSettings`. Reuse T10 field/styles; do not refactor app shell,
      theme, editor, or provider authorization URL validation.
- [ ] Run the minimal GREEN focused tests and:

  ```sh
  cd apps/web && npx eslint \
    app/composables/useAuth.ts app/pages/app/settings/sessions.vue \
    app/components/auth/PasswordSettings.vue \
    test/useAuth.test.ts test/sessions.test.ts test/password-settings.test.ts
  npx vue-tsc --build --noEmit
  ```

## Adversarial checklist

- Stale `hasPassword`, stale CSRF after cookie replacement, expired reauth,
  missing provider, provider callback cancellation, and a second CSRF rejection
  fail closed without losing form text.
- Add/change sends one password, never confirmation/current password to the
  wrong route, and clears password fields after settle.
- Provider-only and password states reveal no linked provider email or account
  merge suggestion.
- A successful change causes all prior browser contexts to become anonymous;
  component tests model refresh while T12 proves it live.

## Handoff

Report `useAuth` type change, component/action contract, exact reauth branches,
focused checks, and T12 stable locators. Suggested commit:
`feat(web): add password security settings`.
