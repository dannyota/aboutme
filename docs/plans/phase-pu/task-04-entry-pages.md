# Task 04 — Entry pages

**Acceptance:** AC-UI-002, AC-UI-005, AC-UI-006.

**Depends on:** T03.

**Owned paths:** T04 paths in `file-structure.md`.

## Contract

- Every entry page (`/`, `/login`, `/register`, `/forgot-password`,
  `/reset-password`, `/verify-email`, `/authorize`) renders inside the same
  shell and composes only `components/ui` and `components/app`.
- Auth pages share one local layout component,
  `app/components/auth/AuthCard.vue` (T04 creates it): a centered `Card` with
  `max-w-md`, a `CardHeader` holding the `h1` title and one-sentence
  description, `CardContent` for the form, and `CardFooter` for the links.
- `PasswordField.vue` renders `FormField` around `Input` with the show/hide
  `Button` (`variant="ghost" size="sm"`, `aria-pressed`, `aria-label`
  `Show {label}`/`Hide {label}`, text `Show`/`Hide`) and keeps the local
  confirmation field and `Passwords do not match.` copy. Native `v-model`, no
  paste handler.
- The landing page is type-led: the headline at
  `text-4xl font-semibold tracking-tight` (`text-5xl` from `md`), the lead in
  `text-muted-foreground text-lg`, two `buttonVariants` links, the three points
  as a plain `<ul>` with `font-medium` titles, the license line in
  `text-sm text-muted-foreground`. No cards, no eyebrow labels, no icons.
- Error and success messages render through `StatusBanner` with the existing
  `data-testid` values and roles; errors that the page previously focused keep
  `focusOnMount`.
- The consent page lists scopes as a `<ul aria-label="Requested permissions">`
  of `Badge` items and renders the client name as text.
- Logic in every page's `<script setup>` is unchanged: the `next` validator, the
  closed error vocabulary, the token stripping, the double-submit guard.

## Strings held

`Sign in`, `Create account`, `Forgot password?`, `Send reset link`,
`Back to sign in`, `Reset password`, `Approve`, `Deny`, every error string in
the closed vocabularies, `Already have an account?`, `Passwords do not match.`.

## TDD cycle

- [ ] **RED.** In `test/login.test.ts`, `test/register.test.ts`,
      `test/forgot-password.test.ts`, `test/reset-password.test.ts`,
      `test/verify-email.test.ts`, `test/authorize.test.ts`, and
      `test/landing.test.ts`, keep every existing assertion and add one case per
      page that proves the composition:

  ```ts
  it("composes the auth card and shared banner", async () => {
    const wrapper = await mountSuspended(LoginPage, {
      route: "/login?error=cancelled",
    });
    await flushPromises();
    expect(wrapper.find('[data-slot="card"]').exists()).toBe(true);
    expect(wrapper.get('[data-testid="login-error"]').attributes("role")).toBe(
      "alert",
    );
    expect(
      wrapper
        .findAll('[data-slot="input"]')
        .every((input) => input.attributes("id") !== undefined),
    ).toBe(true);
  });
  ```

  (`data-slot` is the attribute the generated shadcn-vue primitives put on their
  root; the last assertion pins "no raw button".) For the landing page assert
  the headline is an `h1` with `id="landing-title"` and that no element has a
  `data-slot="card"` attribute.

- [ ] Run and watch the new cases fail:

  ```sh
  cd apps/web && npx vitest run test/login.test.ts test/register.test.ts test/forgot-password.test.ts test/reset-password.test.ts test/verify-email.test.ts test/authorize.test.ts test/landing.test.ts
  ```

- [ ] **AuthCard.** Create `app/components/auth/AuthCard.vue`:

  ```vue
  <script setup lang="ts">
  import {
    Card,
    CardContent,
    CardDescription,
    CardFooter,
    CardHeader,
    CardTitle,
  } from "@/components/ui/card";

  defineProps<{
    readonly title: string;
    readonly description?: string;
    readonly titleId?: string;
  }>();
  </script>

  <template>
    <main class="mx-auto flex w-full max-w-md flex-col px-5 py-10 sm:py-16">
      <Card>
        <CardHeader>
          <CardTitle
            :id="titleId ?? 'auth-title'"
            as="h1"
            class="text-2xl font-semibold tracking-tight"
          >
            {{ title }}
          </CardTitle>
          <CardDescription v-if="description">
            {{ description }}
          </CardDescription>
        </CardHeader>
        <CardContent class="grid gap-4">
          <slot />
        </CardContent>
        <CardFooter
          v-if="$slots.footer"
          class="flex flex-wrap justify-between gap-3 text-sm"
        >
          <slot name="footer" />
        </CardFooter>
      </Card>
    </main>
  </template>
  ```

  If the generated `CardTitle` has no `as` prop, render `<h1>` with the same
  classes directly inside `CardHeader`.

- [ ] **Login page template** (script unchanged):

  ```vue
  <template>
    <AuthCard
      title="Sign in"
      description="Use the email and password for your account."
    >
      <StatusBanner v-if="errorMessage" kind="error" testid="login-error">
        {{ errorMessage }}
      </StatusBanner>
      <StatusBanner v-if="formError" kind="error" testid="login-form-error">
        {{ formError }}
      </StatusBanner>
      <form class="grid gap-4" novalidate @submit.prevent="onSubmit">
        <FormField
          v-slot="{ id, describedBy, invalid }"
          id="login-email"
          label="Email"
        >
          <Input
            :id="id"
            v-model="email"
            :aria-describedby="describedBy"
            :aria-invalid="invalid"
            autocomplete="email"
            type="email"
          />
        </FormField>
        <PasswordField
          id="login-password"
          v-model="password"
          autocomplete="current-password"
          label="Password"
        />
        <Button class="mt-1 w-full" :disabled="pending" type="submit">
          {{ pending ? "Signing in…" : "Sign in" }}
        </Button>
      </form>
      <template v-if="providerLogin">
        <div class="flex items-center gap-3 text-xs text-muted-foreground">
          <Separator class="flex-1" />
          or
          <Separator class="flex-1" />
        </div>
        <ul class="grid gap-2">
          <li v-for="provider in providers" :key="provider.id">
            <a
              :class="buttonVariants({ variant: 'outline', class: 'w-full' })"
              :href="
                explicitNext
                  ? `/api/v1/auth/${provider.id}/start?next=${encodeURIComponent(explicitNext)}`
                  : `/api/v1/auth/${provider.id}/start`
              "
            >
              {{ provider.label }}
            </a>
          </li>
        </ul>
      </template>
      <template #footer>
        <NuxtLink
          class="text-primary underline-offset-4 hover:underline"
          to="/forgot-password"
        >
          Forgot password?
        </NuxtLink>
        <NuxtLink
          class="text-primary underline-offset-4 hover:underline"
          to="/register"
        >
          Create account
        </NuxtLink>
      </template>
    </AuthCard>
  </template>
  ```

  Import `AuthCard`, `StatusBanner`, `FormField`, `PasswordField`, `Input`,
  `Button`, `buttonVariants`, and `Separator` explicitly. Delete the
  `import '~/assets/css/auth.css'` line.

- [ ] **Register, forgot, reset, verify pages.** Apply the same composition:
      `AuthCard` title `Create account` / `Forgot password` / `Reset password` /
      `Verify email`; every input inside `FormField` with the ids the tests use
      (`forgot-email`, `password-new`); success banners are `StatusBanner`
      `kind="success"` with `testid` `register-success`, `forgot-success`,
      `reset-success`, `verify-success`; error banners `kind="error"` with
      `focusOnMount` where the page called `.focus()` before. Footer links:
      `Already have an account? Sign in`, `Back to sign in`, `Sign in`.

- [ ] **Consent page.** `AuthCard` title `Allow access` with the client name in
      `CardDescription` inside `<strong data-testid="consent-client-name">`, the
      scope list as `Badge variant="secondary"` items, and a footer-free
      `CardContent` holding the two buttons: `Button` `type="submit"`
      `data-decision="approve"` (`Approve`) and `Button variant="outline"`
      `type="button"` `data-decision="deny"` (`Deny`). Error banner `testid`
      `consent-error` with `focusOnMount`.

- [ ] **Landing page.** Replace the template with the type-led layout in the
      contract; the points keep `data-testid="landing-point"` and the license
      line `data-testid="landing-license"`. Delete
      `import '~/assets/css/landing.css'`.

- [ ] Delete every rule from `app/assets/css/auth.css` and `landing.css` (leave
      the files empty; T13 removes them).

- [ ] GREEN:

  ```sh
  cd apps/web && npx vitest run test/login.test.ts test/register.test.ts test/forgot-password.test.ts test/reset-password.test.ts test/verify-email.test.ts test/authorize.test.ts test/landing.test.ts
  make -C ../.. web-lint web-typecheck
  ```

## Adversarial checklist

- Author: the T04 cases in `adversarial-coverage.md`.

## Handoff

Report RED and GREEN outputs and any string that changed. Suggested commit:
`feat(web): rebuild the entry pages on the shared components`.
