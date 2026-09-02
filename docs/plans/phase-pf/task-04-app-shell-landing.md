# Task 04 — App shell by authentication state and the landing page

**Acceptance:** AC-OPS-022 (web clauses); spec D4 and D5.

**Depends on:** T00. Independent of T01–T03.

**Owned paths:** `apps/web/app/app.vue`,
`apps/web/app/components/ui/AppChrome.vue`, `apps/web/app/pages/index.vue`,
`apps/web/app/assets/css/landing.css`, `apps/web/test/app-chrome.test.ts`,
`apps/web/test/landing.test.ts`. Deletes
`apps/web/app/components/PlaceholderHero.vue` and
`apps/web/test/placeholder-hero.test.ts`.

## Interfaces

- Consumes: `useAuth().authState`
  (`'loading' | 'authenticated' | 'anonymous' | 'error'`), `AccountControl.vue`,
  `ThemeToggle.vue`, the `app-chrome`, `app-brand`, `app-nav`, and
  `app-account-actions` classes in `app/assets/css/app.css`.
- Produces: `AppChrome.vue` (no props; reads auth state itself) rendered by
  `app.vue` on every surface that showed the header before. T05's login and
  settings tests mount pages whose parent shell is this component; T06 asserts
  the shell's link texts.

## Contract

Signed out or loading: brand link to `/`, `Sign in` link to `/login`,
`Create account` link to `/register`, theme toggle. Authenticated: brand,
`Resumes` link to `/app/resumes`, `Settings` link to `/app/settings/sessions`,
`AccountControl`, theme toggle. The editor route (`/app/resumes/{id}`) keeps its
own top bar and renders no chrome, as today. The landing page is static SSR with
the D5 copy and no data fetch.

## Steps

- [ ] **Step 1: Write the failing shell test**

Create `apps/web/test/app-chrome.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { mountSuspended, registerEndpoint } from "@nuxt/test-utils/runtime";
import { flushPromises } from "@vue/test-utils";
import { setResponseStatus } from "h3";
import AppChrome from "../app/components/ui/AppChrome.vue";

const me = {
  data: {
    user: {
      id: "user-1",
      email: "dev@aboutme.invalid",
      name: "Dev User",
      avatarKey: null,
      hasPassword: true,
    },
    csrfToken: "csrf",
    identities: [],
  },
};

let meStatus = 401;
registerEndpoint("/api/v1/me", (event) => {
  if (meStatus !== 200) {
    setResponseStatus(event, meStatus);
    return { error: { code: "session_required", message: "Sign in." } };
  }
  return me;
});

function links(
  wrapper: Awaited<ReturnType<typeof mountSuspended>>,
): Record<string, string> {
  const out: Record<string, string> = {};
  for (const a of wrapper.findAll("a"))
    out[a.text().trim()] = a.attributes("href") ?? "";
  return out;
}

describe("AppChrome", () => {
  it("shows sign-in and registration while signed out", async () => {
    meStatus = 401;
    const wrapper = await mountSuspended(AppChrome);
    await flushPromises();
    const found = links(wrapper);
    expect(found["Sign in"]).toBe("/login");
    expect(found["Create account"]).toBe("/register");
    expect(found["Resumes"]).toBeUndefined();
    expect(found["Settings"]).toBeUndefined();
    expect(wrapper.find(".account-control").exists()).toBe(false);
  });

  it("shows app navigation and the account control when authenticated", async () => {
    meStatus = 200;
    const wrapper = await mountSuspended(AppChrome);
    await flushPromises();
    const found = links(wrapper);
    expect(found["Resumes"]).toBe("/app/resumes");
    expect(found["Settings"]).toBe("/app/settings/sessions");
    expect(found["Sign in"]).toBeUndefined();
    expect(found["Create account"]).toBeUndefined();
    expect(wrapper.get(".account-control").text()).toContain("Dev User");
  });

  it("keeps the brand link and theme toggle in both states", async () => {
    meStatus = 401;
    const wrapper = await mountSuspended(AppChrome);
    await flushPromises();
    expect(links(wrapper)["aboutme"]).toBe("/");
    expect(wrapper.find('button[aria-label^="Switch to"]').exists()).toBe(true);
  });
});
```

- [ ] **Step 2: Run it and watch it fail**

```sh
cd apps/web && npx vitest run test/app-chrome.test.ts
```

Expected: cannot resolve `../app/components/ui/AppChrome.vue`.

- [ ] **Step 3: Create `AppChrome.vue`**

```vue
<script setup lang="ts">
import AccountControl from "./AccountControl.vue";
import ThemeToggle from "./ThemeToggle.vue";

// Until /me resolves the shell is the signed-out variant (design: web,
// "Application surfaces"); an app route redirects on its own if the session
// is absent, so this never shows a signed-in link to an anonymous visitor.
const { authState } = useAuth();
const signedIn = computed(() => authState.value === "authenticated");
</script>

<template>
  <header class="app-chrome">
    <NuxtLink class="app-brand" to="/"> aboutme </NuxtLink>
    <nav v-if="signedIn" class="app-nav" aria-label="Primary navigation">
      <NuxtLink to="/app/resumes"> Resumes </NuxtLink>
      <NuxtLink to="/app/settings/sessions"> Settings </NuxtLink>
    </nav>
    <div class="app-account-actions">
      <template v-if="signedIn">
        <AccountControl />
      </template>
      <template v-else>
        <NuxtLink class="app-entry-link" to="/login"> Sign in </NuxtLink>
        <NuxtLink class="app-entry-link app-entry-link--primary" to="/register">
          Create account
        </NuxtLink>
      </template>
      <ThemeToggle />
    </div>
  </header>
</template>
```

Add to `app/assets/css/app.css`, beside the existing `.app-account-actions`
rule, styles for `.app-entry-link` and `.app-entry-link--primary` that reuse the
link and button tokens the header already uses (same font size and gap as
`.app-nav a`; the primary variant uses the existing accent background and
contrast text tokens). No new color values.

- [ ] **Step 4: Use it in `app.vue`**

Replace the `<header ...>…</header>` block with
`<AppChrome v-if="showAppChrome" />`, import it
(`import AppChrome from './components/ui/AppChrome.vue';`), and delete the
now-unused `AccountControl` and `ThemeToggle` imports and the
`isAuthenticatedSurface` computed. Keep `isAppSurface` and `showAppChrome` as
they are.

- [ ] **Step 5: Run the shell test to GREEN**

```sh
cd apps/web && npx vitest run test/app-chrome.test.ts
```

Expected: three tests pass.

- [ ] **Step 6: Write the failing landing test**

Create `apps/web/test/landing.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { mountSuspended, registerEndpoint } from "@nuxt/test-utils/runtime";
import { setResponseStatus } from "h3";
import LandingPage from "../app/pages/index.vue";

registerEndpoint("/api/v1/me", (event) => {
  setResponseStatus(event, 401);
  return { error: { code: "session_required", message: "Sign in." } };
});

describe("index.vue", () => {
  it("renders the approved copy", async () => {
    const wrapper = await mountSuspended(LandingPage);
    expect(wrapper.get("h1").text()).toBe(
      "Build your resume. Publish it at its own link.",
    );
    expect(wrapper.text()).toContain(
      "aboutme is an open-source resume builder.",
    );
    const points = wrapper
      .findAll('[data-testid="landing-point"]')
      .map((p) => p.get("strong").text());
    expect(points).toEqual([
      "Yours to keep.",
      "One link per resume.",
      "Bring your own agent.",
    ]);
  });

  it("offers sign-in and registration and nothing into the app", async () => {
    const wrapper = await mountSuspended(LandingPage);
    const hrefs = wrapper.findAll("a").map((a) => a.attributes("href"));
    expect(hrefs).toContain("/login");
    expect(hrefs).toContain("/register");
    expect(hrefs.some((h) => h?.startsWith("/app"))).toBe(false);
  });

  it("names no unshipped feature", async () => {
    const wrapper = await mountSuspended(LandingPage);
    expect(wrapper.text().toLowerCase()).not.toMatch(/pdf|realtime|real-time/);
  });

  it("links the license line to the repository", async () => {
    const wrapper = await mountSuspended(LandingPage);
    const license = wrapper.get('[data-testid="landing-license"] a');
    expect(license.text()).toContain("AGPL-3.0");
    expect(license.attributes("href")).toBe(
      "https://github.com/dannyota/aboutme",
    );
    expect(license.attributes("rel")).toBe("noopener noreferrer");
  });
});
```

- [ ] **Step 7: Run it and watch it fail**

```sh
cd apps/web && npx vitest run test/landing.test.ts
```

Expected: the `h1` text is `aboutme`.

- [ ] **Step 8: Rewrite `app/pages/index.vue`**

```vue
<script setup lang="ts">
import "~/assets/css/landing.css";

useHead({
  title: "aboutme",
  meta: [
    {
      name: "description",
      content:
        "Open-source resume builder. Write once, preview the exact page " +
        "layout, and publish each resume at a clean URL you control.",
    },
  ],
});

const points = [
  {
    title: "Yours to keep.",
    text: "Up to three resumes per account, private until you publish.",
  },
  {
    title: "One link per resume.",
    text: "Publish, unpublish, and control search indexing for each resume on its own.",
  },
  {
    title: "Bring your own agent.",
    text: "Connect an MCP-capable assistant with scopes you grant and can revoke.",
  },
] as const;
</script>

<template>
  <main class="app-page landing">
    <section class="landing-hero" aria-labelledby="landing-title">
      <h1 id="landing-title">Build your resume. Publish it at its own link.</h1>
      <p class="landing-lead">
        aboutme is an open-source resume builder. Write once, preview the exact
        page layout, and publish each resume at a clean URL you control.
      </p>
      <div class="landing-actions">
        <NuxtLink class="landing-button landing-button--primary" to="/login">
          Sign in
        </NuxtLink>
        <NuxtLink class="landing-button" to="/register">
          Create account
        </NuxtLink>
      </div>
    </section>
    <ul class="landing-points">
      <li
        v-for="point in points"
        :key="point.title"
        data-testid="landing-point"
      >
        <strong>{{ point.title }}</strong>
        {{ point.text }}
      </li>
    </ul>
    <p class="landing-license" data-testid="landing-license">
      Open source under
      <a href="https://github.com/dannyota/aboutme" rel="noopener noreferrer"
        >AGPL-3.0</a
      >.
    </p>
  </main>
</template>
```

Create `app/assets/css/landing.css` with `.landing`, `.landing-hero`,
`.landing-lead`, `.landing-actions`, `.landing-button`,
`.landing-button--primary`, `.landing-points`, and `.landing-license` rules. Use
only the existing design tokens from `app.css` (the surface, text, muted text,
accent, and border custom properties the auth pages use); the hero is a single
centered column with a maximum width of 40rem, the buttons match the auth submit
button's height and radius, and the points are a plain list with no icons. Light
and dark themes must both read; the theme tokens already switch, so no
theme-specific rules are needed.

- [ ] **Step 9: Delete the placeholder**

```sh
git rm apps/web/app/components/PlaceholderHero.vue apps/web/test/placeholder-hero.test.ts
```

(Workers report this deletion to the owner instead of running Git.) Grep for
`PlaceholderHero` afterward; no reference may remain.

- [ ] **Step 10: Run the landing test and the whole web gate**

```sh
cd apps/web && npx vitest run test/landing.test.ts test/app-chrome.test.ts
make web-lint web-typecheck web-test
```

Expected: both new files pass; lint, typecheck, and the full suite pass. If an
existing test asserted the placeholder text or the old header on `/`, it is
updated to the new copy in this task.

- [ ] **Step 11: Look at it**

Open `http://localhost:20080/` signed out and signed in (T03's seed or any
account), in light and dark themes. The signed-out shell shows Sign in and
Create account; the signed-in shell shows Resumes, Settings, and the account
name. The landing copy matches the spec.

## Adversarial checklist

- A stale `/me` error (500) renders the signed-out shell, never a broken
  signed-in one.
- The landing page issues no fetch: assert in the landing test that
  `registerEndpoint` for `/api/v1/me` is hit at most once (the shell's call) and
  that no other endpoint is requested.
- No link on `/` points under `/app`.

## Handoff

Report RED and GREEN outputs for steps 2, 7, and 10, the CSS rules you added,
and the deletion for the owner. Suggested commit:
`feat(web): add the landing page and state-aware shell`.
