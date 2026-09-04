# PV T04 — auth and consent pages

## Contract

Restyle `login.vue`, `register.vue`, `forgot-password.vue`,
`reset-password.vue`, `verify-email.vue`, and `authorize.vue` on PU's composites
to the design's auth rule: the form sits on the desk with no card.

Shared structure for every page:

```vue
<main class="mx-auto w-full max-w-[26rem] px-6 py-16" data-testid="<page>-page">
  <h1 class="text-xl font-semibold">Sign in</h1>          <!-- left-aligned -->
  <StatusBanner v-if="formError" tone="error" role="alert">…</StatusBanner>
  <form class="mt-8 grid gap-6" novalidate @submit.prevent="onSubmit">
    <TextField … />            <!-- controlled inputs; these forms submit -->
    <PasswordField … />        <!-- toggle is an IconButton inside the field, h-9 -->
    <Button type="submit" variant="default" class="h-9">Sign in</Button>
  </form>
  <nav class="mt-6 flex justify-between text-sm">…secondary links…</nav>
</main>
```

Rules:

- No `Card`, no shadow, no border around the form. One `--border` rule under the
  title (`border-b pb-4`).
- `PasswordField` keeps its `Show`/`Hide` behavior but renders the toggle as an
  `IconButton` (eye icon from `@lucide/vue`) absolutely positioned inside the
  input, `aria-label="Show password"` / `"Hide password"`; the visible text
  "Show" is replaced by the label. Hook change: tests that click by text "Show"
  now click by `aria-label`.
- Success states (`auth-success`) become `StatusBanner tone="success"` in
  `--foreground` with a pencil tick, not green.
- The provider block on `login.vue` keeps its `v-if="providerLogin"` gate and
  renders providers as `Button variant="outline"` links.
- `authorize.vue` renders the client name as text in the title ("Allow {client}
  to edit your resumes?"), the scopes as a ruled `<dl>` with the two scope
  explanations, "Approve" (`default`) and "Deny" (`ghost`), and keeps every
  existing `data-testid`.
- Every page keeps its `useHead` title and the `#token=` fragment handling.

Strings held: every string asserted in `test/login.test.ts`,
`test/register.test.ts`, `test/forgot-password.test.ts`,
`test/reset-password.test.ts`, `test/verify-email.test.ts`,
`test/authorize.test.ts`, `password-auth.spec.ts`, and `entry.spec.ts`, except
the "Show" button text listed above.

## TDD cases

Update each page's test first: assert the absence of a card wrapper (no element
with `data-slot="card"` inside `main`), the title precedes the form, the
password toggle by `aria-label`, error banners with `role="alert"`, and the
consent page's client-name rendering of `<img src=x onerror=alert(1)>` as text
with no `img` element. Keep all behavioral cases.

## Ownership and checks

Owned paths:

- `apps/web/app/pages/login.vue`, `register.vue`, `forgot-password.vue`,
  `reset-password.vue`, `verify-email.vue`, `authorize.vue`
- `apps/web/app/components/auth/PasswordField.vue`
- `apps/web/test/login.test.ts`, `register.test.ts`, `forgot-password.test.ts`,
  `reset-password.test.ts`, `verify-email.test.ts`, `authorize.test.ts`

Acceptance: `AC-UI-002`, `AC-UI-005`, `AC-UI-006` (re-proof on these pages).

Run:

```sh
cd apps/web
npx vitest run test/login.test.ts test/register.test.ts test/forgot-password.test.ts \
  test/reset-password.test.ts test/verify-email.test.ts test/authorize.test.ts
npx eslint app/pages app/components/auth test
npx vue-tsc --noEmit
```

Do not edit composables, the shell, or Git state. Report the first failing test,
exact commands, and every held string you had to keep verbatim.
