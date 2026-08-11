# Task 11: Playwright harness — visual regression, offline fonts, browser corpus + CSP

Satisfies **AC-SEC-001**'s real-browser + CSP legs, AC-REN-003's offline proof,
and the master plan's "Visual regression" row.

**Files:** create
`apps/web/e2e/{playwright.config.ts,screenshot.spec.ts, fonts-offline.spec.ts,corpus.spec.ts,baselines/*.png}`,
`apps/web/app/pages/_harness/render.vue` (D17 gating in `nuxt.config.ts`),
`apps/web/app/utils/csp.ts`, `apps/web/test/harness-absent.test.ts`.
`@playwright/test` is already installed by Task 0 (B8); this task does not touch
`package.json`.

**Requested from the integration owner (Makefile + CI are owner-owned; exact
text to hand over):**

```make
web-e2e: ## Renderer visual regression + browser corpus (pinned container, ARM64 baseline)
 podman run --rm --platform linux/arm64 --network=host \
   -v $(PWD):/work -w /work/apps/web \
   -e UPDATE_GOLDEN= -e PLAYWRIGHT_UPDATE_SNAPSHOTS= \
   mcr.microsoft.com/playwright:<pinned-version>@sha256:<digest> \
   npx playwright test --config e2e/playwright.config.ts

web-e2e-update: ## Regenerate committed screenshot baselines (same container)
 # identical, plus --update-snapshots
```

plus a CI `web-e2e` job running `make web-e2e` after `web`, with the job
definition itself asserting `UPDATE_GOLDEN` and
`--update-snapshots`/`PLAYWRIGHT_UPDATE_SNAPSHOTS` are unset before the step
runs (B6 — a stray CI env var left set would silently rewrite baselines instead
of comparing against them). The `<pinned version>@sha256:<digest>` is resolved
at implementation time from the pinned `@playwright/test` version, is an **ARM64
manifest-list digest** (this project's canonical baseline platform — D16/B6),
and is recorded in `playwright.config.ts`'s header comment; local runs use the
identical `--platform linux/arm64` container so baselines never diverge by
architecture.

`playwright.config.ts` (D16/B6) defines: a `webServer` block that runs
`nuxt build` followed by `nuxt preview` — never `nuxt dev` — inside the pinned
container before tests start; a fixed `use:` context (`timezoneId: 'UTC'` — the
same value `print.md` §7 pins as `TZ=UTC` for the print container, so the two
rendering paths cannot disagree about which is authoritative (D16);
`locale: 'en-US'`, `viewport` per D7 page geometry, `deviceScaleFactor: 1`,
`colorScheme: 'light'`, `reducedMotion: 'reduce'`); and Chromium launch args
`--force-color-profile=srgb --font-render-hinting=none --disable-lcd-text`. A
global setup step asserts `process.env.UPDATE_GOLDEN` and
`process.env.PLAYWRIGHT_UPDATE_SNAPSHOTS` are both unset and throws if either is
set, so a local `web-e2e` run gets the same protection as the requested CI job
even before that job exists (B6).

## Screenshot subset — 7 baselines, named here (owner ruling 2026-08-11)

Pixel baselines are the scarce artifact: string goldens cover all 20 presets
(Task 9), screenshots cover a named six-preset subset plus the continuous-mode
case (`tokens.md` §4.2). The subset is **named, not derived at run time**, and
each entry earns its place by a property no other entry covers:

| Preset              | Cell                    | Property it pins                                                                                                                 |
| ------------------- | ----------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| `classic-serif`     | `vn-full` × paged       | one column, `placement: "keep"`, untinted, serif — the plain baseline                                                            |
| `engineer-compact`  | `vn-full` × paged       | two columns, `placement: "byType"`, untinted — a column regression cannot hide behind a fill                                     |
| `modern-sidebar`    | `vn-full` × paged       | two columns with `surfaceTarget: "sidebar"` — per-surface clamp, and a tint that must repaint across the flow (`print.md` §5)    |
| `executive-band`    | `vn-full` × paged       | `surfaceTarget: "header"` at `#16273d`, the darkest surface in the set — the clamp's direction flip to light text on a dark band |
| `consulting-formal` | `vn-full` × paged       | `pageFormat: "letter"` — the only page geometry A4 cannot show                                                                   |
| `academic-dense`    | `vn-full` × paged       | base 13 px, `lineHeight` 1.3, `entryGap` 6 — dense small type, where line-height and gap rounding shows first                    |
| `modern-sidebar`    | `full` × **continuous** | the continuous-mode case; the other six run paged                                                                                |

The other fourteen presets get no pixel baseline; Task 9's string goldens cover
them. P9's manual browser UAT exercises this same six-preset subset
(`../phase-9-local-uat.md`), so the three coverage surfaces cannot drift apart —
changing the subset changes all three at once.

Harness page contract: `/_harness/render?fixture=<id>&template=<id>&mode=<m>`
renders `ResumeDocument` from the named fixture (served from
`packages/schema/fixtures`), plus `?payload=<corpus-id>` mode rendering a single
`RichText` with that payload, and `?raw=1` rendering the payload **unsanitized**
(CSP-backstop probe only — the page carries a visible warning comment and exists
only under the build flag). All `_harness` responses carry `HTML_CSP` via route
rules.

- [ ] **Step 1: Failing gating test.** `harness-absent.test.ts`: a normal
      `nuxt build` output contains no `_harness` route; a `NUXT_HARNESS=1` build
      does. Implement the page + gating; pass.
- [ ] **Step 2: Offline fonts.** `fonts-offline.spec.ts`: route-block every
      non-`localhost` request and **fail the test if any is attempted**; load
      the vn-full harness page; await `fontsReady()`; assert
      `document.fonts.check()` true for all five families; assert zero blocked
      external requests — the self-hosted/offline proof (AC-REN-003).
- [ ] **Step 3: Screenshot baselines.** `screenshot.spec.ts` renders the seven
      cells of the named subset above, full-page after `fontsReady()`, compared
      with **zero** tolerance against committed baselines. First run generates
      via the update target; committed; CI compares. Vietnamese diacritic
      fidelity is judged in baseline review (tofu/misplaced marks in a baseline
      = task failure, not a later discovery).
- [ ] **Step 4: Browser corpus + CSP conformance.** `corpus.spec.ts`, per corpus
      payload: (a) sanitized page (`?payload=`) — collect `page.on('dialog')`,
      `pageerror`, console errors, and `securitypolicyviolation` events; assert
      **zero** of each; assert the neutralization predicate via `page.evaluate`
      over the live DOM (the real-browser leg of AC-SEC-001); (b) raw backstop
      (`?raw=1`) — the CSP alone must still prevent script execution: zero
      dialogs/pageerrors even though markup is unsanitized (violation events are
      _expected_ here and recorded, not asserted zero). Assert the response
      actually carried `HTML_CSP` byte-exact (a silently missing header must
      fail, not vacuously pass). (c) **CSP baseline compatibility on a normal
      build (B9).** Using this same `webServer`'s **normal**
      (non-`NUXT_HARNESS`) build, navigate to the already-existing `/login` page
      and inject the `HTML_CSP` header via `page.route()` response fulfillment —
      real route-rule wiring for production pages is P5A/P8-sec's job (D5), this
      step only proves the exported **value** is compatible with genuinely
      Nuxt-emitted output, not just the harness's purpose-built markup. Collect
      `securitypolicyviolation` events across the page's full load and hydration
      and assert **zero** — the regression this catches is a future Nuxt-emitted
      resource (e.g. a payload script needing a nonce) that `script-src 'self'`
      would silently break. `csp.ts`'s doc comment states this constant is the
      renderer-surface baseline and names P5A/P8-sec as its
      production-enforcement owner.
- [ ] **Step 5: Gate + commit.**
      `make web-lint web-typecheck web-test     web-build` plus the harness
      invocation (via the granted target, or the documented `podman run` line
      with the result recorded verbatim if the target is not yet granted — never
      claim the gate ran without output).
