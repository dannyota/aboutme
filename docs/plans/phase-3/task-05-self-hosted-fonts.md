# Task 5: Self-hosted Vietnamese-diacritic fonts

Satisfies **AC-REN-003**.

**Owner ruling 2026-08-11 — language scope, cost, and why not the CDN.** The
product targets **Vietnamese and English only**. The subset ranges below are
therefore already correct and are not widened: Latin Extended-A (Polish,
Turkish, Czech) stays out, and the earlier open question about it is closed.

All five families are Google Fonts under the **SIL Open Font License 1.1** —
free for commercial use, self-hosting, modification, subsetting, and embedding
in a PDF, with no fee and no usage reporting, in perpetuity. The only OFL
obligation is to ship each family's licence text, which `LICENSES/` does. The
owner cannot be billed for these fonts by any route.

Self-hosting is **not** a cost decision — it is free, exactly like the CDN — so
the CDN's permission does not change the plan, for three independent reasons:

1. **The print path has no outbound network** (design spec §2). Chromium renders
   the PDF with egress denied, so a CDN `@font-face` cannot resolve there; every
   PDF would silently fall back to a container font.
2. **Determinism.** Goldens and pinned-tolerance screenshots require the exact
   same bytes on every run; a CDN can reship a family at any time. The committed
   woff2 files plus their `manifest.json` sha256 entries are what make the
   visual gates meaningful.
3. **Privacy.** A Google Fonts CDN request discloses each visitor's IP to a
   third party, which conflicts with this project's privacy posture and has been
   held to breach the GDPR in at least one EU ruling.

Google remains the **source**: fetch from the `google/fonts` repository at the
recorded commit (Step 2), then subset and commit the result.

**Files:** create `apps/web/app/assets/fonts/**` (20 woff2, `fonts.css`,
`manifest.json`, `LICENSES/` with each family's OFL),
`apps/web/app/utils/ fontsReady.ts`, `apps/web/test/fonts.test.ts`,
`apps/web/scripts/subset-fonts.md` (documented regeneration procedure —
`pyftsubset` invocation, pinned fonttools version, unicode ranges; the committed
binaries are the authority, the script is provenance). `fontkit` is already
installed by Task 0 (B8); this task does not touch `package.json`. This task
also modifies `apps/web/nuxt.config.ts` (`css:` registration of `fonts.css` — B8
ownership: T5 lands first, T11 reads the landed file before adding its own
block) and reports its `eslint.config.mjs` ignore need (`app/assets/fonts/**`)
to Task 10 rather than editing it directly (B8).

Families (must equal the schema enum, mechanically tested): Be Vietnam Pro,
Inter, Source Sans 3, Alegreya, Roboto Serif. Subset ranges (recorded in the
manifest and used by the coverage test): Basic Latin U+0020–007E; Latin-1
letters U+00C0–00FF; U+0102–0103, U+0110–0111, U+0128–0129, U+0168–0169
(Ă/Đ/Ĩ/Ũ); U+01A0–01B0 (Ơ/Ư); **U+1EA0–1EF9 complete** (Vietnamese precomposed
additions); general punctuation subset U+2013–2014, U+2018–201D, U+2026. (U+2013
EN DASH added 2026-08-11: it is the date-range separator the renderer emits per
the template contract (`docs/specs/templates/contract.md` §5.4), so omitting it
would make every date range fall back to a container font and break screenshot
determinism; U+2014 rides along for rich-text prose.)

- [ ] **Step 1: Failing coverage test.** `fonts.test.ts`: read `manifest.json`;
      for each entry assert the file exists, sha256 matches, and (via `fontkit`)
      the cmap contains **every** codepoint in the pinned Vietnamese list above
      (exported as a constant in the test file, derived from the ranges — write
      it out, don't compute it from the manifest, so the manifest can't
      self-certify). Also assert the set of `font-family` names in `fonts.css`
      equals the schema's `customization.font.family` enum exactly, and that
      **every family ships both weight 400 and weight 700** — `tokens.md` §3.2
      makes this a hard build requirement, because a missing cut makes Chromium
      synthesize the bold and destroys golden determinism. A family missing
      either weight fails the build, it does not warn. Run → **FAIL** (nothing
      exists).
- [ ] **Step 2: Vendor + subset.** Fetch each family from its upstream source
      (google/fonts repo at a recorded commit), subset with pinned fonttools to
      the ranges above, four instances per family (400/700 × roman/italic —
      static instances even for variable-font upstreams), emit woff2, write
      `manifest.json` (upstream repo+commit, license, tool+version, ranges,
      sha256 per file). Write `fonts.css` (`@font-face`, `font-display: block` —
      not `swap`, since `swap` can repaint after `fonts.ready` resolves and
      destabilize pinned-tolerance screenshots, D16/B6 — `unicode-range`
      matching the subset) and register it globally in `nuxt.config.ts` `css:`.
      Run Step 1 → **PASS**.
- [ ] **Step 3: `fontsReady`.** `app/utils/fontsReady.ts`:
      `export async function fontsReady(doc: Document = document):     Promise<void>`
      — awaits `doc.fonts.ready` **and** explicit `doc.fonts.load()` for the
      five families at the sizes the renderer uses (fonts.ready alone resolves
      early if nothing requested the face yet). Unit test with a stubbed
      FontFaceSet. The offline/real-browser proof is Task 11's.
- [ ] **Step 4: Gate + commit.** Full web gate. If binary files or `fonts.css`
      need an `eslint.config.mjs` ignore, state the exact glob
      (`app/assets/fonts/**`) in the task report for Task 10 to land (B8 — this
      task does not edit `eslint.config.mjs`). Commit fonts + test +
      `nuxt.config.ts` paths only.
