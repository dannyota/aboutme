# PV T03 — landing page

## Contract

Rebuild `apps/web/app/pages/index.vue` to the FIRST VIEWPORT block of the
direction contract. Static server render, no data fetch, base CSP unchanged.

### Sample document

Create `apps/web/app/landing/sampleResume.ts` exporting
`const sampleResume: Resume` (type from `@aboutme/schema`) as a literal copy of
`packages/schema/fixtures/full.json` with `personalDetails.photo` removed, and
`const sampleLink = '/ada-lovelace'`. Create
`apps/web/app/landing/sampleContext.ts` exporting
`const sampleContext: RenderContext = { lng: 'en', mode: 'continuous' }` (type
from `components/resume/resolveRenderModel.ts`).

### Page

```vue
<main class="…" data-testid="landing">
  <section aria-labelledby="landing-title">   <!-- two columns ≥ 42rem -->
    <div>
      <h1 id="landing-title">The resume is public. You are not.</h1>
      <p class="landing-lead">aboutme is an open-source resume builder. Write up to three resumes, preview the exact page, and publish each one at its own link. Search and AI discovery stay off until you turn them on.</p>
      <div>  <!-- signed out -->
        <NuxtLink to="/register" :class="buttonVariants({ variant: 'default' })">Create account</NuxtLink>
        <NuxtLink to="/login" class="…">Sign in</NuxtLink>
      </div>
      <div>  <!-- signed in: v-else -->
        <NuxtLink to="/app/resumes" :class="buttonVariants({ variant: 'default' })">Open your resumes</NuxtLink>
      </div>
    </div>
    <figure data-testid="landing-sample" aria-label="Sample resume published at aboutme.vn/ada-lovelace">
      <div class="landing-sheet">        <!-- white, radius-sheet, shadow-paper, scale 0.6 via zoom -->
        <ResumeDocument :document="sampleResume" :context="sampleContext" />
      </div>
      <AppSeal :link="sampleLink" size="stamp" class="landing-seal" />  <!-- absolute, lower right -->
    </figure>
  </section>
  <ul class="landing-points">…the three existing points, unchanged text, data-testid="landing-point"…</ul>
  <section aria-labelledby="landing-publish-title">
    <h2 id="landing-publish-title">Publishing is three choices</h2>
    <dl>  <!-- three ruled columns -->
      <div><dt>Public resume</dt><dd>Whether any public page exists.</dd></div>
      <div><dt>PDF download</dt><dd>Whether visitors can download the PDF. You can always export your own.</dd></div>
      <div><dt>SEO and GEO</dt><dd>Whether search engines and AI answer engines may index it. Off by default.</dd></div>
    </dl>
  </section>
  <p data-testid="landing-license">Open source under <a …>AGPL-3.0</a>.</p>
</main>
```

Signed-in state comes from the existing session composable the shell already
uses (`useAuth`), read only after hydration; the server render is the signed-out
variant, as the shell does today. The sheet is `zoom: 0.6` at 1440 and
`zoom: 0.5` below 42 rem, where the section stacks to one column with the sheet
under the headline. The seal overlaps the sheet's lower-right corner by a third
of its diameter. Headline is `text-3xl` at 1440 and `text-2xl` below 42 rem,
`text-wrap: balance`. `useHead` keeps the title and description meta from the
current page.

Strings held for the entry proof: the three point titles and texts, the license
sentence, `data-testid` values `landing-point` and `landing-license`. Hook
changes: the hero buttons' text order changes from "Sign in, Create account" to
"Create account, Sign in"; update `entry.spec.ts` in T10 if it asserts order.

## TDD cases

Write `test/landing-sample.test.ts` first: `sampleResume` validates with the
generated validator from `@aboutme/schema` at the current `schemaVersion`, has
no `photo`, and `resolveRenderModel(sampleResume, sampleContext)` returns
without throwing. Write `test/landing.test.ts` (update the existing file): SSR
render contains the headline, the lead, "Create account" before "Sign in", the
rendered name "Ada Lovelace" inside `[data-testid="landing-sample"]`, the seal
`aria-label`, the three points with held text, the three publish choices, the
license; a signed-in render shows "Open your resumes" and neither entry button;
the page module imports no `useFetch`, `useAsyncData`, or `$fetch` (assert on
the source text); the response headers from a Nuxt test-utils `fetch('/')` carry
the base CSP unchanged.

## Ownership and checks

Owned paths:

- `apps/web/app/pages/index.vue`
- `apps/web/app/landing/sampleResume.ts`, `sampleContext.ts`
- `apps/web/test/landing.test.ts`, `test/landing-sample.test.ts`

Acceptance: `AC-UI-008`.

Run:

```sh
cd apps/web
npx vitest run test/landing.test.ts test/landing-sample.test.ts
npx eslint app/pages/index.vue app/landing test/landing.test.ts test/landing-sample.test.ts
npx vue-tsc --noEmit
```

Do not edit the renderer, the shell, the CSP, or Git state. Report the first
failing test, exact commands, the rendered HTML size of `/`, and any schema
field the fixture copy needed to change.
