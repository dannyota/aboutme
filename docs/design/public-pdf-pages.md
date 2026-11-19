# Public resume as PDF pages

Status: Accepted (2026-11-19), with the owner's decisions below. It replaces the
[public web layout](public-web-layout.md) and the
[dark palette rule](public-page-dark-palette.md) when its release lands, amends
[ADR 0020](../adr/0020-application-visual-identity.md) and
[ADR 0013](../adr/0013-resume-header-and-contacts.md), and changes the
[print pagination table](templates/print.md). The document contract, the render
envelope and the PDF do not change.

## Owner decisions

On 2026-11-19 the owner reviewed the sheets layout on his own resume and chose,
from a mockup built from his real PDF:

1. The public page looks like the PDF: the template's layout, each page a sheet
   of paper.
2. Pages on every screen. A phone swipes from page to page, right to left, like
   Apple Books, with each page scaled to fit.
3. In Dark, the pages stay light like the PDF; only the desk and the page bar
   turn dark.

## What a viewer sees

- **Layout.** The template's own layout, exactly as the editor's PDF preview
  draws it: columns, sizes, fonts, line height, inline contacts, photo position,
  and A4 or Letter page boxes with the template's margins. There is no screen
  type floor, one-column override, contact rows, measure or warm paper.
- **Sheets.** Each page is a separate sheet on the desk, with a 4 px radius and
  the shadow below. The page's position `n / N` sits on the desk under the
  sheet, never on the page.
- **Wide screens, 600 px and wider.** The sheets run down the screen, centered
  with a 24 px gap, at scale `min(1, (content width - 32 px) / page width)`.
- **Phones, under 600 px.** The scaled sheets sit side by side in a horizontal
  scroller, one page per screen. Swiping right to left moves to the next page.
  Under the scroller sit the position `n / N`, one decorative dot per page, and
  previous and next buttons with 44 px targets. A one-page resume shows no
  pager.
- **Colors.** Pages use the PDF's colors. `customization.colorScheme` keeps its
  values and sets only the desk and the page bar: Light, Dark, or Match device
  through `prefers-color-scheme`.

| Scheme | Desk      | Shadow `S` |
| ------ | --------- | ---------- |
| Light  | `#e6dfd0` | `74 54 22` |
| Dark   | `#141210` | `0 0 0`    |

The sheet shadow is
`0 0 0 0.5px rgb(S / .14), 0 1px 2px rgb(S / .08), 0 10px 26px -8px rgb(S / .2), 0 32px 60px -28px rgb(S / .22)`,
a renderer value: chrome tokens such as `--shadow-paper` stay off the public
page. A root rule paints the desk color for the scheme, so overscroll shows it.
The page bar's inner row is as wide as the page on a wide screen and spans the
viewport minus 16 px gutters on a phone.

## How the page is built

### Server HTML

- The public app renders `ResumeDocument` in `continuous` mode, with no color
  scheme, inside `main`. That is the HTML the server validates and caches, and
  the format the continuous goldens pin. It is complete without script for
  crawlers, readers without script and the live-state gate
  ([ADR 0010](../adr/0010-public-artifact-revocation.md)).
- The public HTML validator keeps its continuous rules and has no folio rule.
  The HTML format version rises with this change.

### Pagination in the reader's browser

The hydration script `public-resume.mjs` paginates after hydration with
`PagedResume`, the editor's renderer, using `measure.ts` and `paginate.ts`.

1. **Hydration.** The client's first render equals the server's: continuous
   only. `PagedResume` mounts after `onMounted`, through a flag, on both the
   hydrate path and the replace path.
2. **Measuring.** `PagedResume` lays out its hidden measuring copy at the page
   width, outside every transformed ancestor and outside the scroller, so the
   breaks do not depend on the screen. Pages and the measuring copy set
   `text-size-adjust: 100%`. Pagination waits for the template's own font faces
   to load; `fontsReady` loads only those faces, not the fallback stack.
3. **Staging.** The paged tree renders with `visibility: hidden`, `inert` and
   `aria-hidden` while it measures, never `display: none`.
4. **Signals.** `PagedResume` emits `settled` and `failed`. Its run loop catches
   errors and reports them as `failed`.
5. **Swap.** One frame after `settled`, the script shows the pages and hides the
   continuous article on screen. It swaps only while the reader is still within
   the first page's height; a reader who has scrolled further keeps the
   continuous article for this load.
6. **Timeout.** A 3 s timer starts after mount and font loading. On `failed` or
   timeout the script unmounts `PagedResume` and keeps the continuous article,
   with no late swap. A later live update tries pagination again.
7. **Live updates.** An SSE update re-renders the continuous article with the
   new revision and re-paginates. The last settled pages stay visible while it
   runs, for at most the timeout. On `failed` or timeout the script shows the
   new continuous article. A phone keeps its page index, clamped to the new
   count, and focus returns to the scroller.

`PagedResume` gains a per-page wrapper slot for the public layout: the `section`
named for the page, the position on the desk, the snap item and the scaled
sheet. On a phone its untransformed `.paged-resume` root is the scroller,
without its grid styles; the fixed-position measuring copy inside it adds no
scrollable overflow. The paged goldens stay byte-identical. Its overflow
outlines become editor-only (`.preview-sheet`); a block taller than a page still
grows its page and is never clipped.

### Scale and scroller

- The script computes the scale from `documentElement.clientWidth`, which
  excludes the scrollbar gutter, and the page format: 794 px for A4, 816 px for
  Letter. Each sheet uses `transform: scale()` inside a box sized to the scaled
  page, the `ScaledSheet.vue` technique; CSS `zoom` stays rejected because
  WebKit clamps small text.
- The 600 px breakpoint is a viewport media query, so the orientation needs no
  script and cannot flip back and forth. The root sets
  `scrollbar-gutter: stable`.
- On a phone, `scroll-snap-type: x mandatory` sits on the scroller, and
  `scroll-snap-align: center` and `scroll-snap-stop: always` on the
  untransformed wrapper, which also draws the radius and shadow. The scroller
  sets `overscroll-behavior-x: contain`. While `visualViewport.scale` is above 1
  the script turns snapping off, so a zoomed reader can pan.
- An IntersectionObserver tracks the current page. Left and Right move one page,
  with `preventDefault` on the browser's own arrow scrolling. Under
  `prefers-reduced-motion` page moves jump.

### Join invite

The invite anchors to the first page wrapper. It shows once the reader reaches
60% of the pages: the page index on a phone, the scroll position on a wide
screen. On a phone it reserves its height above the pager.

### Styles

`PagedResume`'s styles and the desk, sheet, pager and scroller rules reach
`print.css` through an import in `ResumeDocument`'s style block. Every public
rule is scoped under `.public-resume-page`. The print route stays script-free
and its CSP is unchanged; the PDF and the print-PDF checks do not change.

### Browser print

The continuous article stays in the DOM after the swap, hidden on screen. In
print the pages, desk and pager are hidden and the continuous article prints
with its padding set to 0, as the print route does, so Chromium paginates it
with the PDF's own engine, margins and break rules. After hydration the script
inserts the `@page` rule from `renderPageRule` as a `<style>` element, which the
CSP's `style-src 'unsafe-inline'` allows, and replaces it when a live update
changes the page format or margins. The page bar's Download PDF stays the
faithful print.

### Editor

The editor's Web preview mode, its toggle and the `aboutme.editorPreviewMode`
key are removed. Its PDF preview is what the public page shows.

## Accessibility

- Each sheet is a `section` named "Trang {n}/{N}" / "Page {n} of {N}". The
  visible position under it is `aria-hidden`.
- The phone scroller is focusable and named "Các trang CV" / "Resume pages". The
  buttons are "Trang trước" / "Previous page" and "Trang sau" / "Next page",
  disabled at the ends. The position updates a polite live region once a move
  settles.
- Reading order follows the pages, so a two-column template reads its main and
  side column page by page. That is accepted as the PDF's order.
- Scaled text and links are small on a phone. Links inside text meet WCAG 2.5.8
  through its inline exception. The header's scaled links fall short of it; that
  is an accepted shortfall of the owner's choice, mitigated by pinch zoom and by
  the full-size continuous article without script.

## Page positions

`n / N` is the sheet's position on this page, not a page count, and no rule
depends on it. That is the one exception to
[print pagination](templates/print.md)'s rule that the editor's count is
labelled an estimate: the PDF stays the authority and may break differently.

## Release

One release. It changes no document version, migration or OpenAPI, so a rollback
is safe. The validator change and the renderer change ship together in the same
images.

## Changes

| Area                             | Change                                                                                                                                                                                                                                                                                                                                         |
| -------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `PublicResumeApp.vue`, hydration | Server and first client render are continuous; the client adds `PagedResume`, stages, swaps, and handles failure, timeout and live updates                                                                                                                                                                                                     |
| `PagedResume.vue`                | Per-page wrapper slot, `settled` and `failed` events, a caught run loop, editor-only overflow outlines                                                                                                                                                                                                                                         |
| Renderer                         | Remove `ResumeSheets.vue`, `resumeSheets.css`, `sheetGroups.ts`, the `sheets` mode, `darkPalette.ts`, the `--dark-color-*` emission in `useResumeStyles.ts`, the role overrides in `resumeColorScheme.css` (keeping its bar rules; a root rule paints the desk), and `RenderContext.colorScheme`; `fontsReady` loads only the template's faces |
| Join invite                      | Anchor and trigger as above                                                                                                                                                                                                                                                                                                                    |
| Editor                           | Remove the Web preview mode, its toggle, the storage key and the `previewModeWeb` copy in `i18n/editor-shell.ts`; reword the color scheme hint in `i18n/editor-controls.ts` for the desk                                                                                                                                                       |
| Go `publicapi`                   | Remove `html_folio.go`, its tests and its hooks in `html_validation.go`; bump `htmlFormatVersion`                                                                                                                                                                                                                                              |
| Goldens and manifests            | Remove the 120 sheets goldens and the harness `ground=dark` fixture; regenerate `testdata/resume-links-render.json`, the template pages manifest and `scripts/web-e2e-source.manifest`                                                                                                                                                         |
| Tests                            | Replace the sheets, dark palette and Web preview tests (`test/editor/{editor-preview,color-scheme,editor-shell}.test.ts`, `e2e/chrome.spec.ts`, `e2e/editor-web-sheets.ts`) with the cases below                                                                                                                                               |

## Doc changes in the same release

ADR 0020 and ADR 0013 are amended, and the two superseded designs are marked,
with this design. The release changes the rest:

| Document                                                                                 | Change                                                                                                                         |
| ---------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| `public-page-theme.md`                                                                   | Owner decisions, the hint copy, the editor preview section, the dark rule section and the light-surfaces table, for decision 3 |
| `public-page-bar.md`                                                                     | The bar row width                                                                                                              |
| `web.md`, `display-assertions.md`                                                        | Sheets mode, golden coverage, screen overrides and the public display cells                                                    |
| `templates/print.md`                                                                     | The public row of the pagination table, the page-position exception, and browser print                                         |
| Template docs (`contract.md`, `tokens.md`, `geometry.md`, `colors.md`, `limitations.md`) | Remove the screen type floor and the sheets overrides                                                                          |

## Tests

- **Server HTML.** The article inside `main` equals the continuous golden for
  each preset, for fixtures without project links, and the validator accepts the
  page.
- **Page breaks.** For the 26 preview-gap samples in Chromium and WebKit, the
  public page shows the same page count and first block per page as the editor's
  PDF preview, at 390, 768 and 1440 px. One Letter sample is added.
- **Swap.** On a wide screen page 1's blocks do not move at the swap. A reader
  scrolled past the first page keeps the continuous article.
- **Wide screens.** Sheets stack with 24 px gaps and the position outside each
  sheet; at 700 px they are scaled and vertical.
- **Phones** (390 px): one sheet per screen, snap stops on each page, the next
  button and the Right arrow reach page 2 and update the position, and there is
  no horizontal page scroll outside the scroller.
- **Fallback.** With script disabled the continuous article renders. A forced
  failure on first load, and on an SSE update, shows the newest continuous
  article.
- **Live update.** An SSE update re-paginates and keeps the page index.
- **Colors.** Pages are light under Light, Dark and Match device, the last under
  both viewer preferences; the desk and bar follow the scheme.
- **Print.** Emulated print media prints the continuous article with no desk,
  and for one sample Chromium's printed page count equals the PDF's.
- **Mutation proofs.** One for each: missing snap, broken pager, clipped
  overflow, scale above 1, dark pages, a stale page after a failed update, and a
  late swap after a timeout.

## Risks

- **Page breaks.** The breaks are the editor's estimate in the reader's engine.
  The preview-gap suite holds them to the PDF's for its samples in Chromium and
  WebKit. Firefox, iOS text settings and unusual content can differ by a block.
- **One visible change after load.** The continuous article becomes pages once
  fonts load. On a wide screen page 1 does not move; on a phone the restacked
  text becomes scaled pages.
- **Page weight.** Measuring adds a hidden copy of the resume. Block counts are
  small, so this is accepted.

## Alternatives rejected

- **Store the PDF's breaks at publish and render pages on the server.**
  `PagedResume` already accepts an injected measurer on the server
  (`PaginationMeasureKey`), and the public PDF job and its revocation-gated
  cache exist, so this is the upgrade path. It needs stored measurements under
  [ADR 0010](../adr/0010-public-artifact-revocation.md) and a fallback before
  they exist. Switch if the fallback rate or the change after load becomes a
  complaint.
- **Print the PDF from the paged layout.** One break model for both, but it
  loses Chromium's widow, orphan and in-paragraph splitting.
- **Images of the real PDF pages.** Exact, but the text is not selectable,
  searchable or accessible, and it exposes the PDF when downloads are off.
- **CSS multi-column pages.** The browser's own fragmentation, but no paper box
  per page and weak grid fragmentation outside Chromium.
- **The sheets layout on phones, or dark pages.** Shown to the owner on
  2026-11-19 and not chosen.
