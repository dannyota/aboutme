# Display assertions

Status: Accepted by the owner, 2026-11-01; implementation pending. Owner
direction: test display with Playwright without pixel comparisons. Accepted: the
assertion contract and ADR 0019, 0020, and 0024 amendments; delete the 107
test-only PNGs after replacement proofs; keep 26 product images and eight
optional 14-day review screenshots; keep Git history.
[Merged owner choices and delivery jobs](local-tooling-simplification.md#owner-choices-and-combined-jobs)
cover both proposals; this document owns the display replacement contract.

## Decision and authority

Replace pixel equality with explicit layout, style, content, accessibility, and
PDF assertions. Keep the `web-e2e` gate, both build surfaces, and existing
non-pixel checks. No product layout, copy, API, schema, or runtime browser
change is proposed. Older clients and saved resumes need no migration or
fallback.

[ADR 0019](../adr/0019-application-ui-toolkit.md) requires unchanged renderer
screenshot suites for chrome changes;
[ADR 0020](../adr/0020-application-visual-identity.md) also names baselines.
[ADR 0024](../adr/0024-delivery-gates.md), `instructions/workflow.md` and
`instructions/resources.md` restrict local browser execution. The combined
authority job amends all three ADRs with History entries and updates those
instructions before activation. Preserve renderer isolation, visual values and
exact-commit hosted release CI. These proposals do not themselves change the
accepted records or authorize execution.

Keep [template contracts](templates/contract.md), [tokens](templates/tokens.md),
[colors](templates/colors.md), [print rules](templates/print.md),
[public themes](public-page-theme.md), and
[UI specifications](ui/typography-and-tokens.md) as the homes of expected
values. Tests encode those values independently of the production resolver.
Reading a CSS variable and comparing it only to its own consumer is
insufficient: a wrong variable would pass both sides.

## Complete comparison inventory

Replace 103 screen, four print and 26 product-page comparisons: 133 total.
Remove `verifyScreenshot` in `support.ts`, both print/sample `compareRaster`
helpers and unused `toHaveScreenshot` configuration after acceptance. Paths in
the complete inventory below are relative to `apps/web/e2e/`.

| Spec and cells                                                                                                                                                                                                                                             | Visual properties guarded                                                                           | Replacement                                                                                                                                             |
| ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `screenshot.spec.ts`: nine renderer cells: `vn-full` paged classic-serif, classic-serif justify, engineer-compact, modern-sidebar, executive-band, consulting-formal A4 and Letter, academic-dense; `full` continuous modern-sidebar                       | Paper, margins, columns, section order, wrapping, headings, photo crop, surface tint, justification | Paper and column geometry; ordered visible content; text containment; computed typography, alignment and region colors; crop ratios; page membership    |
| Same spec: nine public cells: classic-serif and modern-sidebar at 2560; those two plus editorial-wide at 390; dark modern-sidebar at 390, creative-accent and executive-band at 1440; system classic-serif at 1440 with dark preference                    | Centered reading measure, phone gutters, toolbar, column stacking, owner dark theme                 | Center and padding equations; toolbar alignment and print hiding; column order and no overlap; explicit owner/preference color matrix                   |
| Same spec: eight guide cells: `vi` and `en`, light and dark, 390 and 1280                                                                                                                                                                                  | Guide hierarchy, instructions, code wrapping, CTA and footer                                        | Scoped ARIA, exact localized text/order, code-block scrolling exceptions, box and token checks                                                          |
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

Renderer fixtures run with both resume languages independently of interface
locale. The nine renderer cells retain their A4 794 × 1123 or Letter 816 × 1056
viewport, modes and customization, under both viewer schemes. Cards retain 1200
× 630, the seven cases including CJK, and both viewer schemes; add `vi`/`en`
variants for the missing-name and missing-photo states. PDFs run both resume
languages, both A4/Letter for overflow fixtures, and every listed sample in its
prescribed format. Assert print stays light under dark preference.

Public pages cross owner `light`, `dark`, `system` with both viewer preferences
for the five public templates in the inventory, both languages, 390 and 1440.
Assert resolved colors against independent expected values, not just inequality.
Print, cards, gates, thumbnails, template pages and landing samples stay light.
Application chrome still follows its own theme.

Add focused boundary probes in both locales/themes, without repeating PDFs:
640/641 (Library 2/3 columns, showcase 1/2), 899/900 (Library 3/4 and template
detail stacking), 1023/1024 (filter sheet/desktop rows or rail), 1179/1180
(Library 4/5). Test showcase results-container widths 859/860 at viewport ≥1024
for 2/3 columns. Preserve existing 360/375/390/414/430 hero checks and
704/768/1024/1152 header checks. Public columns and header restack below a 36em
container width: probe `36 × container fontSize` minus 1 px and exactly that
width, using the fixture's independently specified font size.

Use the locked Playwright 1.64.0 and its bundled browsers on both platforms,
UTC, device scale 1, reduced motion, explicit locale (`vi-VN` or `en-US`), and
fixed clocks/data. Wait for fonts, decoded images, hydration and pagination
settled markers. Verify requested bundled font faces loaded without errors;
retain offline font/fallback tests. `document.fonts.ready` alone cannot prove
the requested face was used. Keep renderer synthesis disabled.

## Assertion contract

Use role/name locators for UI, stable data attributes for geometry. Gather
related rectangles in one `evaluate` call. Use `expect.poll` for settled
geometry, not fixed sleeps or test retries. Playwright documents
[retrying assertions](https://playwright.dev/docs/test-assertions) and
[viewport-relative bounding boxes](https://playwright.dev/docs/api/class-locator#locator-bounding-box).

| Property          | Required assertion and proposed tolerance                                                                                                                                                                                                                                                                                                        |
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

| Mapping        | Required evidence                                                                                                                                                                                                                                                                                      |
| -------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Bar            | Visible fill width = track width × level / 5 within 1 CSS px; correct start edge, height and colors. Zero has no painted fill.                                                                                                                                                                         |
| Dots           | Five equal visible dots; exactly `level` leading dots filled. Check actual colors, diameter and gaps, not classes.                                                                                                                                                                                     |
| Tag/absence    | Literal `level/5`, independent colors/padding/border and text containment. Absent level and text mode have no widget or stray paint.                                                                                                                                                                   |
| PDF widgets    | Locate beside entry text; transform/clip painted paths. Fill length within 1 point; five dot shapes with exact filled count/order; tag text/background bounds. Unsupported paint evidence fails.                                                                                                       |
| Other mappings | Paper size/margins, column ratios/gaps, section/entry spacing, font multipliers, photo crop/position, card fitted-name sizes and thumbnail scale use fixture inputs and independent tokens. Include grid counts, image ratios, page membership and color schemes in per-cell evidence before deletion. |

## Print and PDF

CSS print emulation does not expose actual page fragments. Keep it for computed
`break-*`, `orphans: 2`, `widows: 2`, print colors and hidden toolbar checks;
use headless Chromium `page.pdf()` for actual output on both platforms.
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

## Platforms, limits and artifacts

Linux CI keeps its pinned browser image initially. Add a native macOS arm64
runner path for the same specs, inputs and expectations, without Podman or
`uname -m = x86_64`. Use Playwright's installed executable rather than a Linux
path. Both native runners follow
[native admission and account](local-tooling-simplification.md#native-admission-and-account):
owner-written/reviewed commits, disposable builds/profiles, explicit environment
and test credentials. Contributor heads use hosted CI. Resource changes follow
the combined authority job; until activation, existing lock/cap rules apply.
[Playwright supports macOS and Linux](https://playwright.dev/docs/intro#system-requirements);
the repository's native run and these tolerances are not yet proven.

`make web-e2e` and `make web-e2e-fast` cover the display HTTP harnesses on
20090/20092, including direct Chromium PDFs on Mac. Functional HTTPS is a
separate requirement: the combined native HTTPS runner job owns `make dev-https`
and `make dev-https-*-check`, strict profile CA trust, exact platform
exclusions, evidence and cleanup. Its server PDF/PNG skips do not apply to
display harness PDFs. Native HTTP WebKit needs no local CA; HTTPS WebKit remains
explicitly skipped as specified there.

Validate all cells on both platforms before deleting comparisons. Record maximum
geometry deltas, browser revision and font hashes. Never use OS-specific
expected layouts, skip Mac PDFs, widen tolerance to a line/page, or accept
missing fonts. Resolve reproducible failures before cutover. Keep production
browser pins and same-environment output determinism rules unchanged.

These assertions miss subtle kerning, glyph raster defects, optical balance,
fine SVG errors, and unasserted shadows or decorations. PDF text may exist while
paint is wrong. Narrow paint checks reduce that gap but cannot eliminate it.
[Playwright warns that screenshots vary with the environment](https://playwright.dev/docs/test-snapshots).
Non-pixel checks remove that source of comparison noise, not all layout drift.

Recommend eight optional, non-blocking manual-review images per run: Vietnamese
public modern-sidebar dark 390; English public executive-band light 1440;
Vietnamese home light 390; English showcase dark 1440; English Library filter
sheet light 390; Vietnamese sign-in error 390; page two of each overflow PDF.
Store only sanitized artifacts for 14 days, no committed diagnostic PNGs or
expected/actual/diff triplets. Failure capture is limited to failed cases and
uses the existing collector bounds; raw traces/HTML/console/PDF stay unuploaded.
Capture failure cannot turn a failed assertion green. Optional capture failure
reports unavailable evidence without failing an otherwise passing display gate.
Use a closed fixture-ID allowlist independent of deleted PNG directories. Public
failure summaries retain only spec, line and fixed status; local reports may
carry measured boxes. Keep commit, source-manifest/archive hashes and browser
identity in sanitized metadata. Do not upload arbitrary attachments by glob.

## Migration

First approve authority changes, then add replacement assertions alongside the
old gate. Prove every inventory cell and mutation below; validate native and
Linux runs. Split product image generation before removing `web-e2e-update`.
Then delete test baselines, comparison helpers and Baselines workflow together;
retain `web-e2e` and `web-e2e-fast` names and both shard selections. No baseline
update mode remains in either test target. Release only after exact-commit CI.

Product PNGs in `public/templates/pages/` are served by Library and PDF tabs.
Recommend keeping those 26 committed assets, outside baseline testing, with a
separate explicit generator. Record renderer/sample/font/browser source hashes
in the manifest and fail stale inputs without comparing pixels. Generation uses
pinned Linux for consistent delivery and requires manual inspection. Keep the
manifest's array shape and existing fields; add source and output SHA-256 fields
per entry. Hash sorted paths and bytes for the generator, renderer, render
utilities, schema/template/sample inputs, bundled fonts, Nuxt configuration and
lockfile, plus browser image digest and PDF flags. Exclude output PNGs and the
manifest from the source hash. Validate each output file's hash, dimensions and
page order without regenerating images in test mode. `make web-template-pages`
builds the harness from the clean source archive in the pinned Linux image, then
runs `node scripts/generate-template-pages.mts` from `apps/web`. Output goes
only to an explicit candidate directory. `template-pages.yml` is
manual-dispatch, read-only, with no commit/push step. On success it uploads only
the validated 26 PNGs, manifest and source metadata for 14 days. Inspect that
exact-commit artifact, copy only product assets and manifest into their owned
paths, then rerun CI; failed generation uploads none. Existing URLs and manifest
consumers remain compatible. Eliminating these PNGs from Git too requires a
separate asset delivery decision; never delete them with the test fixtures.
Showcase test card images become ephemeral fixtures.

## Proof and cost

For each cell, record removed filename, property, replacement, mutation and
observed failure. Temporary mutations must fail the intended assertion on Linux
and Mac; a setup crash does not count. Before deletion, also confirm the old
Linux comparison caught the mutation, then restore a passing clean fixture.
Replay available historical defects. Mutation examples alone do not establish
equivalent coverage.

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

PDF generation, two builds and expanded coverage may offset removed PNG work.
Measure wall/test time, artifact bytes and case counts before changing shards;
no speedup is claimed. Deletion preserves old Git objects; product images still
add history bytes. Geometry-only checks miss paint/pagination; blocking PNGs
conflict with owner direction. The merged choices cover assets and history.

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
