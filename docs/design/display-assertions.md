# Display assertions

Status: Accepted by the owner, 2026-11-01. Revised with the owner's approval on
2026-11-10: hosted Linux CI alone gates the cutover, and mutation tests stay in
CI permanently. The [cutover](#cutover) landed on 2026-11-10: the assertions,
mutation tests and review images are the display gate, and the pixel comparisons
and their baselines are deleted.

## Decision and authority

Test display with rules, not pictures. A pixel comparison can fail on harmless
rendering differences between machines and never names the rule that broke. A
display assertion states one rule the product must keep, such as "columns do not
overlap" or "page two repeats the sidebar tint", and names the rule that failed.

Replace every pixel comparison with explicit layout, style, content,
accessibility, and PDF assertions. Keep the `web-e2e` gate, both build surfaces,
and existing non-pixel checks. No product layout, copy, API, schema, or runtime
browser change is proposed. Older clients and saved resumes need no migration or
fallback.

[ADR 0019](../adr/0019-application-ui-toolkit.md),
[ADR 0020](../adr/0020-application-visual-identity.md), and
[ADR 0024](../adr/0024-delivery-gates.md) record the decision. Preserve renderer
isolation, visual values, and exact-commit hosted release CI.

Keep [template contracts](templates/contract.md), [tokens](templates/tokens.md),
[colors](templates/colors.md), [print rules](templates/print.md),
[public themes](public-page-theme.md), and
[UI specifications](ui/typography-and-tokens.md) as the homes of expected
values. Tests encode those values independently of the production resolver.
Reading a CSS variable and comparing it only to its own consumer is
insufficient: a wrong variable would pass both sides.

## How the tests protect the product

Every cell of the [matrix](#matrix-and-stable-inputs) passes through these
layers. Together they replace what a pixel comparison caught.

| Layer             | What it checks                                                                                                                                          | Catches                                                           |
| ----------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------- |
| Shared invariants | On every tested page: no overflow or clipping, no overlap, text inside its container, requested fonts loaded, images decoded, colors equal token values | A broken page of any kind, on surfaces nobody thought to describe |
| Surface checks    | What a user would see break on that surface: paper and columns, grid counts, content order, toolbar and sheet placement, crop, fitted names             | A layout that is intact but wrong                                 |
| PDF checks        | Real Chromium output: page count and size, which entry lands on which page, text with accents, fonts, painted regions and images                        | Print defects that screen checks cannot see                       |
| Mutation tests    | Seeded defects that each assertion must detect, on every commit                                                                                         | An assertion that silently stopped working                        |
| Review images     | Eight optional screenshots per run for a person to look at                                                                                              | Kerning, raster and optical defects that no rule describes        |

The [assertion contract](#assertion-contract) defines the first two layers,
[print and PDF](#print-and-pdf) the third, [proof](#proof-mutation-tests) the
fourth, and [limits](#platforms-limits-and-artifacts) the fifth.

## Complete comparison inventory

The assertions replaced 103 screen, four print and 26 product-page comparisons:
133 total. The 103 screen and four print PNGs were test baselines, deleted at
cutover. The 26 product-page PNGs are [product images](#product-images): they
stay, and only their comparison is removed. `verifyScreenshot`, both
print/sample `compareRaster` helpers and the `toHaveScreenshot` configuration
are deleted. Paths in the complete inventory below are relative to
`apps/web/e2e/`.

| Spec and cells                                                                                                                                                                                                                                             | Visual properties guarded                                                                           | Replacement                                                                                                                                             |
| ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `screenshot.spec.ts`: nine renderer cells: `vn-full` paged classic-serif, classic-serif justify, engineer-compact, modern-sidebar, executive-band, consulting-formal A4 and Letter, academic-dense; `full` continuous modern-sidebar                       | Paper, margins, columns, section order, wrapping, headings, photo crop, surface tint, justification | Paper and column geometry; ordered visible content; text containment; computed typography, alignment and region colors; crop ratios; page membership    |
| Same spec: nine public cells: classic-serif and modern-sidebar at 2560; those two plus editorial-wide at 390; dark modern-sidebar at 390, creative-accent and executive-band at 1440; system classic-serif at 1440 with dark preference                    | Centered reading measure, phone gutters, toolbar, column stacking, owner dark theme                 | Center and padding equations; toolbar alignment and print hiding; column order and no overlap; explicit owner/preference color matrix                   |
| `guide-screenshot.spec.ts`: eight guide cells: `vi` and `en`, light and dark, 390 and 1280                                                                                                                                                                 | Guide hierarchy, instructions, code wrapping and CTA                                                | Scoped ARIA, exact localized text/order, code-block scrolling exceptions, box and token checks                                                          |
| `chrome.spec.ts`: home, login, templates, and home-template region, each light/dark at 390/1440, `vi`: 16 images                                                                                                                                           | Header, form, hero sheet and ghost, cards, spacing, glows and colors                                | Header/form structure, sheet alignment, grid dimensions, image readiness, token and pseudo-element styles; retain Chromium/WebKit phone centering tests |
| `gallery.spec.ts`: creative-accent Page light 390; executive-band PDF light/dark 390/1440; ATS dark 390/1440, all `vi`: seven                                                                                                                              | Page/info stacking, PDF image sequence and fit, ATS readability, tab chrome                         | Tab roles and selected state; document/info boxes; decoded ordered page images and aspect ratio; ATS text/order, wrap and colors                        |
| `gallery-baselines.spec.ts`: sheet `vi` dark 390 / `en` light 390; filtered same phone cell / `en` light 1440; rows `vi` light 1024: five                                                                                                                  | Filter sheet, chips, desktop rows, result grid                                                      | Modal bounds, focus, scrolling; chip wrapping; row spacing; correct grid count, gaps and order                                                          |
| `verify.spec.ts`: verified/unavailable, light/dark, 390/1280, `vi`: eight                                                                                                                                                                                  | Status, component cards, provenance chain, long digests, footer                                     | Exact state and bilingual structure; digest wrapping; sibling separation; status colors/icons and spacing                                               |
| `card.spec.ts`: long-name-vi, short-name-en, no-photo-en, no-name, accent-light, accent-dark, cjk-name: seven                                                                                                                                              | Safe square, fitted names, fallback identity, photo, accent, logo                                   | Retain 1200 × 630 and safe-square checks; fitted text line bounds, expected font sizes, crop, accent and logo geometry; absent contact sentinels        |
| `sign-in-gate.spec.ts`: Google/no-error `en`/`vi` at 390/1280; Google+LinkedIn/no-error `en` 390; Google/failed `en`/`vi` 390: seven                                                                                                                       | Gate card, provider buttons, error copy, privacy boundary                                           | Card containment, button alignment, ARIA and literal copy; expected provider/error visibility; no resume content                                        |
| Same spec: invite bar `en`/`vi` 390/1280 and card `en`/`vi` 1920: six                                                                                                                                                                                      | Fixed invite placement, button, clearance and bilingual fit                                         | Retain 56 px bar/body clearance; 320 px card, 16 px right/bottom inset, 20-second delay; text and control containment                                   |
| `showcase-baselines.spec.ts`: empty/filled × `en`/`vi` × light/dark × 390/1440; filled `vi` light 768; filtered `en` light 1440 / `vi` dark 390; sheet `en` light 390 / `vi` dark 390: 21                                                                  | Empty state, twelve tiles, invite, pager, filters, sheet and responsive grid                        | State/list counts, ordered links, decoded cards, aspect ratios, grid/gap equations, sheet focus and fit, tokens                                         |
| `print.spec.ts`: main-overflow and sidebar-overflow, modern-sidebar, pages 1 and 2: four                                                                                                                                                                   | Independent column continuation, break positions, repeated backgrounds, photo, no dropped text      | PDF page count, per-page markers and column coordinates; PDF paint geometry; print CSS and image checks                                                 |
| `sample-pages.spec.ts`: `en`/`vi` for ats-plain, engineer-compact, consulting-formal, creative-accent, elegant-serif-two, international-lang, minimal-air, mono-print, nordic-muted, one-page-tight, executive-band, graduate-friendly, modern-sidebar: 26 | Every sample's printed layout, one-page fit, typography, image served by Library                    | PDF text/geometry/style contract, one page and 0.90–0.98 fill; independent product-image generation and freshness metadata                              |

`card.spec.ts` also captures PNG bytes to enforce the 524,288-byte output cap.
That is a product size check, not a pixel comparison; keep it. Keep HTML
goldens, corpus, offline fonts, sample content, CSP, and preview-gap assertions.
The latter already uses PDF text and geometry. No PNG comparison exists there.

## Matrix and stable inputs

Run applicable screen states in both interface locales (`vi`, `en`) and both
viewer color schemes (`light`, `dark`) at 390 × 900 and 1440 × 900. Below 1024,
test open sheets and hidden desktop controls; from 1024, require closed/absent
sheets, hidden Filters buttons and visible desktop controls. Retain the listed
extra widths 768, 1024, 1280, 1920 and 2560 on their owning surfaces. Gate tests
retain height 800; fixed invite tests retain height 900. Do not enlarge the
viewport to load lazy content: scroll each item into view, wait for
decode/mount, then return to the required position before measuring.

Factors that do not interact are covered by a rotation, not a cross product
(owner, 2026-11-10, to keep the gate fast). Every value of every factor runs,
and factors that interact are crossed in full: owner theme with viewer
preference on public pages, and language, message and providers on gates. The
rotation is a fixed list or index rule in the spec, never random.

Renderer fixtures run with both resume languages. The nine renderer cells retain
their A4 794 × 1123 or Letter 816 × 1056 viewport, modes and customization. Each
cell runs once per resume language, with viewer scheme and interface locale
rotated so every cell sees both schemes; one continuous cell keeps the full
product. Cards retain 1200 × 630, the seven cases including CJK, and both viewer
schemes; add `vi`/`en` variants for the missing-name and missing-photo states.
PDFs run both resume languages, both A4/Letter for overflow fixtures, and every
listed sample in its prescribed format. Assert print stays light under dark
preference.

Public pages cross owner `light`, `dark`, `system` with both viewer preferences
for the five public templates in the inventory. Language and width (390, 1440)
rotate across those six cells so each template sees all four pairs. Gates cover
every language, message and provider combination and every width and viewer
pair; invites cover every width and language with the viewer alternating. Assert
resolved colors against independent expected values, not just inequality. Print,
cards, gates, thumbnails, template pages and landing samples stay light.
Application chrome still follows its own theme.

Add focused boundary probes in both locales/themes, without repeating PDFs:
640/641 (Library 2/3 columns, showcase 1/2), 899/900 (Library 3/4 and template
detail stacking), 1023/1024 (filter sheet/desktop rows or rail), 1179/1180
(Library 4/5). Test showcase results-container widths 859/860 at viewport ≥1024
for 2/3 columns. Preserve existing 360/375/390/414/430 hero checks and
704/768/1024/1152 header checks. Public columns and header restack below a 36em
container width: probe `36 × container fontSize` minus 1 px and exactly that
width, using the fixture's independently specified font size.

Use the locked Playwright 1.64.0 and its bundled browsers, UTC, device scale 1,
reduced motion, explicit locale (`vi-VN` or `en-US`), and fixed clocks/data.
Wait for fonts, decoded images, hydration and pagination settled markers. Verify
requested bundled font faces loaded without errors; retain offline font/fallback
tests. `document.fonts.ready` alone cannot prove the requested face was used.
Keep renderer synthesis disabled.

## Assertion contract

Use role/name locators for UI, stable data attributes for geometry. Gather
related rectangles in one `evaluate` call. Use `expect.poll` for settled
geometry, not fixed sleeps or test retries. Playwright documents
[retrying assertions](https://playwright.dev/docs/test-assertions) and
[viewport-relative bounding boxes](https://playwright.dev/docs/api/class-locator#locator-bounding-box).

| Property          | Required assertion and accepted tolerance                                                                                                                                                                                                                                                                                                        |
| ----------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Geometry          | Positive visible boxes; edges, widths, fixed spacing and alignment within 1 CSS px; centering within 2 px. Measure transformed thumbnails in viewport coordinates and convert back by their explicit scale for paper checks. No whole-page coordinate snapshot.                                                                                  |
| Overflow/clipping | Root and named content containers: `scrollWidth <= clientWidth + 1`; constrained vertical boxes: `scrollHeight <= clientHeight + 1`. Text `Range.getClientRects()` must remain inside the nearest content/clip rectangle within 1 px. Assert visible display, opacity and nonzero text bounds too. Root overflow alone misses `overflow:hidden`. |
| Overlap           | For adjacent blocks in each flow, `next.top >= previous.bottom - 1`; columns and grid siblings must have no intersection exceeding 1 px on both axes. Check line rectangles when block boxes conceal clipped text. Exempt only named intentional overlaps: hero ghost, seal, cropped photo, fixed overlay.                                       |
| Typography        | Independently specified family stack, actual loaded face/fallback, weight, synthesis, transform, alignment and letter spacing. Font size and line height within 0.1 CSS px; heading/body ratios from tokens. Exact wrap counts only for deliberate fit fixtures; normal text may wrap differently while staying ordered and contained.           |
| Color/decoration  | Normalize resolved CSS colors to sRGB channels, tolerance 1/255 per channel and 0.01 alpha; assert expected token values and applied foreground/background/border/fill/stroke, including relevant pseudo-elements. Check radius, shadow and gradient declarations; retain contrast floors.                                                       |
| Content/structure | Expected visible text, occurrence counts and order per column/section, links and selected/disabled states. Literal bilingual expectations come from approved copy, not imports of the same runtime translation object. Normalize whitespace and NFC only; retain Vietnamese accents.                                                             |
| Accessibility     | Scoped inline `toMatchAriaSnapshot` for headings, navigation, form, tabs, lists and modal. Include names/states plus explicit counts and absence assertions, since partial snapshots allow extra nodes. Keep focus and keyboard tests separately.                                                                                                |
| Images            | Decode, natural dimensions, expected source identity and aspect ratio; crop ratios 1.25, −0.125, −0.0625 within 0.001 for the existing fixture, `object-fit: cover`, clipping frame. Check SVG identity/geometry and styles without snapshotting arbitrary serialized SVG.                                                                       |

[ARIA snapshots](https://playwright.dev/docs/aria-snapshots) prove structure,
not paint or geometry. Limit scrolling exceptions to code blocks/sheet bodies
with reachable controls. Approved ellipses need full accessible names; never
hide resume body text.

### Data mapped to geometry

Check independent inputs in every applicable screen/PDF cell, including
engineer-compact and modern-sidebar. For `primitives/LevelWidget.vue`, add skill
and language levels 0–5, absent level and all four modes in both languages.

| Mapping        | Required evidence                                                                                                                                                                                                                                                                         |
| -------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Bar            | Visible fill width = track width × level / 5 within 1 CSS px; correct start edge, height and colors. Zero has no painted fill.                                                                                                                                                            |
| Dots           | Five equal visible dots; exactly `level` leading dots filled. Check actual colors, diameter and gaps, not classes.                                                                                                                                                                        |
| Tag/absence    | Literal `level/5`, independent colors/padding/border and text containment. Absent level and text mode have no widget or stray paint.                                                                                                                                                      |
| PDF widgets    | Locate beside entry text; transform/clip painted paths. Fill length within 1 point; five dot shapes with exact filled count/order; tag text/background bounds. Unsupported paint evidence fails.                                                                                          |
| Other mappings | Paper size/margins, column ratios/gaps, section/entry spacing, font multipliers, photo crop/position, card fitted-name sizes and thumbnail scale use fixture inputs and independent tokens. Assert grid counts, image ratios, page membership and color schemes in every applicable cell. |

## Print and PDF

CSS print emulation does not expose actual page fragments. Keep it for computed
`break-*`, `orphans: 2`, `widows: 2`, print colors and hidden toolbar checks;
use headless Chromium `page.pdf()` for actual output.
[Playwright PDF options](https://playwright.dev/docs/api/class-page#page-pdf)
support print media, CSS page size and backgrounds. Preserve production flags:
zero job margins, `preferCSSPageSize: true`, `printBackground: true`,
`displayHeaderFooter: false`, `scale: 1`.

Reuse pinned `pdfjs-dist` 6.3.289. Its
[page API](https://mozilla.github.io/pdf.js/api/draft/module-pdfjsLib-PDFPageProxy.html)
provides text, viewports and drawing operators. Store small reviewed semantic
expectations in TypeScript, never extracted output auto-approved as truth:

- Exact page count, nonempty pages, A4/Letter dimensions within 1 PDF point.
  Keep the existing two-page overflow and one-page sample requirements.
- Every numbered entry exactly once in column order; start/end/opposite markers
  on their expected pages; explicit first/last entry IDs per page. Approve
  membership from existing accepted output before removing PNGs. Both columns
  must begin on page one and continue at the same x position within 1 point.
- Normalize PDF text whitespace and NFC, preserving accents. Group text by
  expected column and y position, not raw extractor stream order. Check glyph
  bounds against margins and adjacent lines within 1 point; retain sample fill
  0.90–0.98. A page-count-only test cannot detect a misplaced break.
- Check heading plus first body fragment stay together, header-only entries
  remain breakable, and explicit two-line paragraph fixtures satisfy applicable
  widow/orphan rules. Keep the documented two-column engine limits.
- Check PDF drawing operators for the fixture's photo image and the expected
  colored rectangular sidebar/header regions on each applicable page, including
  page two. Apply graphics transforms and clipping before checking bounds and
  fill. Unsupported operator forms fail with a fixed diagnostic; do not skip.
  This is a narrow fixture assertion, not a general PDF renderer.

Keep preview/PDF count agreement for the existing long sample fixtures and the
preview-gap gate. Do not promote that agreement into a product guarantee: editor
pagination remains advisory and Chromium print remains authoritative.

## Proof: mutation tests

A mutation test seeds one defect into a passing page and requires one named
assertion to fail. Mutation tests run in `web-e2e` on every commit, so an
assertion that stops detecting its defect turns CI red. `style-mutation.ts`
applies screen mutations; `print-mutations.spec.ts` and `print-pdf.spec.ts` hold
the PDF mutations.

An assertion class is one row of the [assertion contract](#assertion-contract)
(geometry, overflow and clipping, overlap, typography, color and decoration,
content and structure, accessibility, images) or one PDF rule under
[print and PDF](#print-and-pdf) (page count and size, page membership, text,
glyph bounds, paint and images, widgets).

- Every spec in the inventory has at least one mutation test for each assertion
  class it relies on. A mutation for a spec's fixture may live in a sibling
  spec. A new display assertion lands with its mutation test.
- Each mutation test shows three things: the clean page passes; the defect took
  effect, as changed DOM content, order, attributes or accessibility state,
  changed computed style or geometry, or changed PDF output; and the named
  assertion fails. A setup crash or an unspecific thrown error does not count.
- No per-image record, second platform, or replay against the old comparison is
  required.

| Mutation                                                               | Required failure                                    |
| ---------------------------------------------------------------------- | --------------------------------------------------- |
| Shift column or toolbar by 4 px; change grid count                     | Alignment, column/grid geometry                     |
| Disable wrapping; give body fixed height and hidden overflow           | Range containment and scroll bounds                 |
| Negative margin causing 4 px overlap                                   | Sibling separation                                  |
| Swap font, increase base by 2 px, drop bold or justification           | Font readiness, computed type/alignment             |
| Remove dark selector or change foreground/surface token                | Independent computed color expectations             |
| Hide, duplicate or reorder an entry; remove Vietnamese accents         | Visible content/count/order and PDF text            |
| Add forced break; make every entry unbreakable                         | Per-page membership/count; no blank page            |
| Move second-page sidebar into main; drop continuation tint/photo       | PDF column bounds and paint/image assertions        |
| Hide filter control; remove heading role or provider error             | ARIA plus explicit visibility/state/copy            |
| Force `.level-fill { width: 20% !important }` for level 4              | Expected 80% screen/PDF fill length                 |
| Paint all dots filled; change a level-4 tag to `3/5`                   | Exact filled count/order and tag text in screen/PDF |
| Ignore crop offsets or change thumbnail scale without changing its box | Input-derived crop/scale equations                  |

Mutation tests show that each rule detects its defect. They do not show that the
rules cover every defect; the review images and the limits below cover that gap.
PDF generation, two builds and the added checks may offset the removed PNG work,
so no speedup is claimed. Measure time and artifact bytes before changing
shards.

## Platforms, limits and artifacts

Hosted Linux CI in the pinned browser image is the display gate and the only
gate for cutover. It is the environment that produces production PDFs. Keep
production browser pins and same-environment output determinism rules unchanged.

A native macOS run of the same specs is an optional follow-up under
[local tooling](local-tooling-simplification.md#native-admission-and-account).
It never blocks cutover or a release. If it is added, it uses the same inputs
and expectations: no OS-specific expected layouts, skipped PDFs, tolerance
widened to a line or page, or accepted missing fonts.

Chromium may embed variable fonts under the `-Regular` instance name, so PDF
weight evidence is advisory and rejects only contradictions supported by at
least three shared glyphs. Print-emulated DOM computed weight stays strict in
every cell, alongside strict PDF family, size and color checks.

These assertions miss subtle kerning, glyph raster defects, optical balance,
fine SVG errors, and unasserted shadows or decorations. PDF text may exist while
paint is wrong. Narrow paint checks reduce that gap but cannot eliminate it.
[Playwright warns that screenshots vary with the environment](https://playwright.dev/docs/test-snapshots).
Non-pixel checks remove that source of comparison noise, not all layout drift.

Text containment compares range rectangles with clipping ancestors, with a
half-leading allowance, and checks line separation. It does not detect a few
pixels of clipped accent or descender ink, or a heading displaced without
leaving a clipping ancestor. The owner accepted this limit on 2026-11-10; the
review images cover it.

The owner accepted eight optional, non-blocking manual-review images per run:
Vietnamese public modern-sidebar dark 390; English public executive-band light
1440; Vietnamese home light 390; English showcase dark 1440; English Library
filter sheet light 390; Vietnamese sign-in error 390; page two of each overflow
PDF. Store only sanitized artifacts for 14 days, no committed diagnostic PNGs or
expected/actual/diff triplets. Failure capture is limited to failed cases and
uses the existing collector bounds; raw traces/HTML/console/PDF stay unuploaded.
Capture failure cannot turn a failed assertion green. Optional capture failure
reports unavailable evidence without failing an otherwise passing display gate.
Use a closed fixture-ID allowlist independent of deleted PNG directories. Public
failure summaries retain only spec, line and fixed status; local reports may
carry measured boxes. Keep commit, source-manifest/archive hashes and browser
identity in sanitized metadata. Do not upload arbitrary attachments by glob.

## Product images

Product PNGs in `public/templates/pages/` are served by Library and PDF tabs.
Keep those 26 committed assets, outside baseline testing, with a separate
explicit generator, as accepted by the owner. The generator, the validator and
the manifest hashes have landed. Record renderer, sample, font, and browser
source hashes in the manifest and fail stale inputs without comparing pixels.
Generation uses pinned Linux for consistent delivery and requires manual
inspection. Keep the manifest's array shape and existing fields; add source and
output SHA-256 fields per entry. Hash sorted paths and bytes for the generator,
renderer, render utilities, schema/template/sample inputs, bundled fonts, and
Nuxt configuration. Include sorted paths and versions from the resolved
dependency closure of the render packages in the web lockfile, browser image
digest, browser and PDF flags, and raster DPI. Exclude output PNGs and the
manifest from the source hash. Validate each file's hash, dimensions and page
order without regenerating images in test mode. `make web-template-pages` builds
the harness from the clean source archive in the pinned Linux image, then runs
`node scripts/generate-template-pages.mts` from `apps/web`. Output goes only to
an explicit candidate directory. `template-pages.yml` is manual-dispatch,
read-only, with no commit/push step. On success it uploads only the validated 26
PNGs, manifest and source metadata for 14 days. Inspect that exact-commit
artifact, copy only product assets and manifest into their owned paths, then
rerun CI; failed generation uploads none. Existing URLs and manifest consumers
remain compatible. Eliminating these PNGs from Git too requires a separate asset
delivery decision; never delete them with the test fixtures. Showcase test card
images become ephemeral fixtures.

## Cutover

Landed on 2026-11-10 in three steps, gated by hosted Linux CI alone:

1. Coverage: every inventory row has its shared invariants, its surface or PDF
   checks, and one named mutation test per assertion class.
2. Diagnostics and review: a failed case uploads only that case through the
   bounded collector, and a passing shard uploads its review images.
3. Deletion: `apps/web/e2e/baselines/`, `apps/web/e2e/print-baselines/`,
   `verifyScreenshot`, both `compareRaster` helpers, the `toHaveScreenshot`
   configuration, `web-e2e-update`, and `.github/workflows/baselines.yml`.

`web-e2e`, `web-e2e-fast`, every shard selection, the product images, and Git
history remain. No baseline update mode exists in either test target.

A deliberate display change updates the expected values of the affected
assertions in the same pull request, and its author looks at the review images.
Reverting the deletion commit restores the comparisons from Git.

## Security and failure boundaries

Use only synthetic content, mocked identity/provider responses and local assets.
Anonymous and signed-in cross-owner attacks remain covered by existing
authorization proofs; display checks cannot replace them. Hostile HTML, links
and images retain sanitizer, CSP and denied-network checks through rendering and
PDF extraction. Keep output size/time limits and parser dependencies pinned.
Stolen session/MCP token, network interception, compromised host container,
database-writer and leaked-backup protections are unchanged: these tests create
no production credential, signing key, capability, session or stored resume. Key
custody, revocation and recovery remain with existing controllers/ADRs.

Workflow tokens stay read-only. Native candidate admission follows the shared
trust contract; artifact bytes remain untrusted. Keep source allowlists, no
`.env`/Git/local state in source archives, symlink and special-file rejection,
bounded PNG decode/re-encode, and fixed diagnostic fields. Do not upload
arbitrary page text, PDF text, paths or browser errors. Missing fonts/assets,
unsupported PDF evidence or failed required assertions fail the gate; recovery
is a reviewed fix, never an automatic expectation update.
