# Task 5: Self-hosted Vietnamese-diacritic fonts

Satisfies **AC-REN-003**.

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
      equals the schema's `customization.font.family` enum exactly. Run →
      **FAIL** (nothing exists).
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
