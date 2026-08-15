# Task 10 — Add login, registration, verification, and recovery pages

**Acceptance:** AC-AUTH-009, AC-AUTH-011, AC-AUTH-013, AC-AUTH-016.

**Depends on:** T02 generated client; T08 routes; integrated P4 Nova theme and
P5A Nuxt build.

**Owned paths:** T10 paths in `file-structure.md`. Preserve the integrated P4
login wrapper, account control, theme switch, and global CSS.

## Contract

`usePasswordAuth` wraps only T02 generated operations and exposes closed UI
outcomes:

```ts
type PasswordIssue = "length" | "common" | "breached";
type PasswordAuthError =
  | "invalid-request"
  | "invalid-token"
  | "authentication-failed"
  | "password-invalid"
  | "rate-limited"
  | "unavailable";

interface UsePasswordAuth {
  register(input: {
    name: string;
    email: string;
    password: string;
  }): Promise<void>;
  verify(token: string): Promise<void>;
  login(input: { email: string; password: string }): Promise<void>;
  forgot(email: string): Promise<void>;
  reset(input: { token: string; password: string }): Promise<void>;
}
```

The composable maps only exact status/code pairs and falls back to unavailable.
It never stores a password/token in Nuxt state, route query, local/session
storage, error object, logger, or analytics. All requests include credentials;
the browser supplies exact same Origin.

`PasswordField` has an explicit label, `autocomplete` supplied by caller,
show/hide button with accessible state, paste/autofill support, no composition
rewrite, no strength score, and no character-class hint. Registration/reset
confirmation is component-local and never submitted.

## TDD cycle

- [ ] Extend `login.test.ts` first. Require email/current-password fields,
      provider links unchanged, forgot/register links, submit pending state,
      closed failure copy, successful `/app/resumes` navigation, and no password
      retention in rendered attributes/state after completion.
- [ ] Add page/composable REDs for register fixed 202 copy, local confirmation,
      all closed policy issues, disabled duplicate submit, server-authoritative
      validation, and keyboard/error-summary focus.
- [ ] Add fragment REDs before implementation. On verify/reset initial browser
      setup, require exactly one `token`, immediate `history.replaceState`
      before fetch, no query conversion, no-referrer meta, no third-party
      resource, malformed token local failure, one POST, token memory clear,
      refresh/replay behavior, and uniform success copy.
- [ ] Add forgot REDs for exact generic copy across all mock states and no
      account/provider-specific wording.
- [ ] Add light/dark and Nova/Zinc/Emerald token assertions without
      pixel-copying FlowCV or adding custom theme variants.
- [ ] Run expected RED:

  ```sh
  cd apps/web && npx vitest run \
    test/login.test.ts test/register.test.ts test/verify-email.test.ts \
    test/forgot-password.test.ts test/reset-password.test.ts
  ```

- [ ] Implement the composable, shared field, pages, and smallest auth CSS. Page
      code may read `window.location.hash` only under `import.meta.client` and
      must strip it synchronously before awaiting or registering telemetry.
- [ ] Keep provider anchors as top-level navigation and preserve the closed
      OAuth callback error map.
- [ ] Run the minimal GREEN focused Vitest, then:

  ```sh
  cd apps/web && npx eslint \
    app/pages/login.vue app/pages/register.vue app/pages/verify-email.vue \
    app/pages/forgot-password.vue app/pages/reset-password.vue \
    app/components/auth/PasswordField.vue \
    app/composables/usePasswordAuth.ts \
    test/login.test.ts test/register.test.ts test/verify-email.test.ts \
    test/forgot-password.test.ts test/reset-password.test.ts
  npx vue-tsc --build --noEmit
  ```

## Adversarial checklist

- Password manager/autocomplete values are `email`, `current-password`, and
  `new-password` as appropriate; confirmation uses `new-password`.
- Paste, Unicode, spaces, Caps Lock, IME composition, browser autofill, Enter,
  duplicate click, back/forward, refresh, and reduced motion remain safe.
- Prototype-pollution error codes, hostile server messages, and unknown issue
  paths never become HTML or user-visible raw text.
- Fragment token disappears before request and from DOM/state after completion;
  request traces contain it only in the exact POST body.
- No provider link, theme toggle, app shell, public renderer, or SSR contract
  regresses.

## Handoff

Report component/composable interfaces, fragment ordering proof, exact UI copy,
focused checks, and T11 locators. Suggested commit:
`feat(web): add password authentication pages`.
