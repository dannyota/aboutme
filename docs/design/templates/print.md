# Template print behavior

Status: **Approved v2** (2026-08-12).

How a template behaves under Chromium's print engine, which is the authoritative
rendering. Everything here applies to every template, because print geometry is
renderer-owned (`tokens.md` §1).

## 1. Three targets, one renderer

| Target          | Route         | Pagination model                                                                               | Authority               |
| --------------- | ------------- | ---------------------------------------------------------------------------------------------- | ----------------------- |
| Editor preview  | `/app/**`     | JS measure-and-break at entry boundaries, selected A4/Letter page, `transform: scale()` to fit | approximate             |
| Public SSR page | `/[slug]`     | continuous flow, no pagination                                                                 | not paginated at all    |
| PDF             | `/print/[id]` | CSS `@page` plus fragmentation properties                                                      | **authoritative**       |
| Share image     | `/print/[id]` | Fixed 1200 by 630 pixel viewport; top crop, no page fragmentation                              | One fixed image variant |

The [renderer boundary](../system.md#renderer-boundary) makes editor pagination
an approximation and Chromium print pagination authoritative. JavaScript
measurement and the print engine are different algorithms by design.

They are allowed to disagree because they must. The editor measures laid-out DOM
boxes in a scaled viewport and cuts between entries; Chromium fragments the flow
honoring `orphans`, `widows`, `break-inside`, and `break-after`, and will split
inside an entry where the editor would not. Font metric rounding at print
resolution, hyphenation, and `@page` margin geometry differ as well. A one-line
overflow therefore lands on page 2 in the PDF while the preview shows it on
page 1.

Two obligations follow:

- The editor's page count is **advisory** and must be labelled as an estimate in
  the UI. No product rule — publish policy, quota, pricing, validation — may
  depend on it.
- The server never accepts a client-supplied page count. A client measurement
  cannot become load-bearing input to a server render.

The [FlowCV rendering research](../../research/flowcv/README.md) is evidence for
the same preview-and-print split. The project contract remains the renderer
boundary linked above.

## 2. Page geometry

The renderer resolves one typed page-geometry value from `pageFormat` and
`spacing.pageMargin`. Its pure `renderPageRule` function emits one of these
exact print-only shapes, substituting the validated `y` and `x` margins:

```css
@page {
  size: 210mm 297mm;
  margin: <y>mm <x>mm;
}
@page {
  size: 8.5in 11in;
  margin: <y>mm <x>mm;
}
```

- `pageFormat: "a4"` selects `210mm × 297mm` and the 794 × 1123 px editor box.
  `pageFormat: "letter"` selects `8.5in × 11in` and the 816 × 1056 px editor
  box. The editor paginator and print rule consume the same resolved value;
  neither defaults every document to A4.
- Margins default to **15 mm on all four sides for both formats**.
  `spacing.pageMargin` (`{x, y}` in mm, 0–40 per axis) overrides them; when
  absent, `useResumeStyles` applies the 15 mm fallback at the point of use and
  never writes it back. Consumer printers cannot print closer than about 5 mm to
  the paper edge, so margins below that produce clipped hardware output even
  though the PDF looks correct.
- `@page` margin boxes are empty. No running headers, no page numbers, no "page
  1 of 2". Chromium's own header/footer is disabled
  (`displayHeaderFooter: false`), and `preferCSSPageSize: true` makes the CSS
  rule win over the print job's defaults.
- Orientation is always portrait. There is no landscape token.
- `renderPageRule` returns the complete rule rather than a CSS variable used
  inside `@page`. The print path inserts that output. The public SSR page and
  editor preview do not insert it.

## 3. Break rules

Applied by the renderer to the classes it emits. The intent is stated first, the
declaration second, because the declaration alone reads as arbitrary.

| Element                                            | Intent                                                         | Declaration                               |
| -------------------------------------------------- | -------------------------------------------------------------- | ----------------------------------------- |
| `.resume-section`                                  | a section longer than a page must split, not push a blank page | `break-inside: auto`                      |
| `.section-heading`                                 | a heading must never be the last thing on a page               | `break-after: avoid`                      |
| `.entry`                                           | a long entry must be splittable                                | `break-inside: auto`                      |
| `.entry-header` (title, subtitle, meta)            | an entry must never orphan its own heading from its body       | `break-inside: avoid; break-after: avoid` |
| `.entry-body` paragraphs and list items            | no single stranded line                                        | `orphans: 2; widows: 2`                   |
| `li`                                               | a bullet is an atom                                            | `break-inside: avoid`                     |
| `ul`, `ol`                                         | a list longer than a page must split between its items         | `break-inside: auto`                      |
| `.resume-header` (photo, name, headline, contacts) | the identity block is never divided                            | `break-inside: avoid`                     |
| level widgets (`bar`, `dots`, `tag`)               | a widget is never cut mid-glyph                                | `break-inside: avoid`                     |

Why `.entry` is `auto` rather than `avoid`: a work entry with a long description
can exceed a page on its own. `break-inside: avoid` on such an element makes
Chromium push it to a fresh page and overflow it anyway, costing a page and
gaining nothing. Pinning `.entry-header` instead achieves the real requirement —
the heading of an entry never appears alone at the foot of a page — without the
blank-page failure mode.

The section heading and first entry stay sibling blocks. Chained
`break-after: avoid` on `.section-heading` and `.entry-header` keeps the
heading, entry header, and the start of the body together without an overlapping
wrapper. The print path may split a long `.entry` body; the editor paginator
instead treats each whole entry as one measured block and pulls an orphan
heading to the same page as that block.

## 4. Widows and orphans

`orphans: 2; widows: 2` on every block container that holds running text — rich
text paragraphs and list items. Chromium honors both properties in the print
path, so no template needs its own rule.

Two known limits, stated rather than papered over:

- Chromium does not apply `orphans`/`widows` inside a fragmented flex or grid
  item in all cases. In two-column mode the guarantee is therefore best-effort
  for column content, and the golden PDFs are the check.
- A paragraph of exactly two lines cannot satisfy both constraints and will move
  whole to the next page. That is the correct outcome and not a defect.

## 5. Two columns across a page break

Two columns are a **CSS grid with two independent flows**, not CSS multi-column.
`column-count` balances content and reflows it unpredictably across pages, which
would make the sidebar's contents migrate between pages on unrelated edits.

Behavior required across a page break:

- Each flow fragments in place. Sidebar content that overflows page 1 continues
  in the sidebar position on page 2; it never collapses into `main`, and `main`
  never wraps under the sidebar.
- The columns are independent, so they end at different heights. A short sidebar
  leaves white space beside a long `main`. That is expected; the renderer must
  not stretch, balance, or reflow to hide it.
- Column width and gutter are the renderer-fixed `--sidebar-ratio` and
  `--column-gutter` and do not change between pages.
- Any surface tint on a column must be painted by the fragmenting element
  itself, so it repeats on every page rather than ending where the first
  fragment ends. `--color-surface-sidebar` and `--color-surface-header` are
  **not** unconditional aliases of `--color-surface`: each resolves to
  `colors.surface` when `effectiveSurfaceTarget` (`colors.md` §4.1) names that
  region, and falls back to `--color-surface` otherwise (`colors.md` §4). The
  tint is live today — `modern-sidebar.json` sets `surfaceTarget: "sidebar"`,
  `executive-band.json` sets `"header"` — so a fragmenting sidebar column with
  an active tint must repaint it on every page, not only the first.
- In one-column mode there is no fragmentation question: `main` sections are
  emitted in order, then `sidebar` sections, in one flow.

**This is the highest-risk area of the print path.** Chromium's fragmentation of
grid containers across print pages is its least reliable behavior. The golden
set must therefore include a two-column fixture whose sidebar alone overflows
one page, and one whose `main` alone does. If Chromium's behavior diverges from
the rules above, preserve the failing output as review evidence and block P3. Do
not replace the accepted baseline with the divergent output. The correction is a
shared print layout or a reviewed design change, never a per-template
workaround.

## 6. Photo and images

The [pure-renderer contract](../web.md#pure-renderer) gives the print browser no
general outbound network access. Everything the page needs must arrive with it.

- The photo is delivered to `/print/[id]` inlined as a `data:` URI, or from a
  same-origin path the print context can reach. A remote S3 URL yields a blank
  photo box in the PDF, silently.
- `/print` awaits `document.fonts.ready` **and** decode of every image before
  signalling readiness to chromedp. Fonts alone are not enough; an undecoded
  image prints as empty space.
- `photo.crop` is applied in CSS — `object-fit: cover` with `object-position`
  computed from the crop rectangle — so the same source image crops identically
  in all three targets. The renderer never rasterizes or re-crops.
- Supply the photo at no less than twice `--photo-size` (192 px for a 96 px
  box). The PDF embeds the raster at its natural resolution, so a 96 px source
  prints visibly soft.
- The sanitizer forbids external images inside rich text, so a photo is the only
  image on the page besides inline lucide SVGs, which carry no network cost.
- `print-color-adjust: exact` (with the `-webkit-` prefix) on the resume root,
  and `printBackground: true` in the print call. Without both, Chromium drops
  background fills and the level bars, tag chips, and any surface color vanish
  from the PDF while remaining in the preview.
- Links survive as PDF link annotations. The renderer must not append a visible
  URL in parentheses; that would inject content the document does not contain.

## 7. Determinism

Golden HTML snapshots and Playwright screenshot diffs ([web design](../web.md))
are only meaningful if the same document renders to the same bytes. Every input
that can vary is pinned.

| Input              | Pin                                                                                                                            |
| ------------------ | ------------------------------------------------------------------------------------------------------------------------------ |
| Chromium           | image digest, per the [engineering standard](../../standards/engineering.md); a browser upgrade is a reviewed snapshot change  |
| Fonts              | self-hosted catalog; 400/700 requested with synthesis disabled and a bundled fallback for missing faces (`tokens.md` §3.2)     |
| Font loading       | `await document.fonts.ready` before print                                                                                      |
| Timezone           | `TZ=UTC` in the render container                                                                                               |
| Locale             | explicit `LANG`/`LC_ALL`; root `lang` is canonical `resumes.lng` or `und` for null, empty, or invalid legacy data              |
| Clock              | the [pure renderer](../web.md#pure-renderer) makes no `Date.now()` or locale calls; `present: true` renders a fixed label      |
| Color              | `--force-color-profile=srgb`                                                                                                   |
| Text rasterization | `--font-render-hinting=none`, `--disable-lcd-text`                                                                             |
| Compositing        | `--disable-gpu`, `--hide-scrollbars`, fixed device scale factor                                                                |
| Animation          | `@media print` disables transitions and animations                                                                             |
| Print parameters   | `preferCSSPageSize: true`, `printBackground: true`, `displayHeaderFooter: false`, `scale: 1`, zero job margins so `@page` wins |
| Contrast clamp     | pure function, fixed step size (`colors.md` §5); its outputs are pinned by the snapshots                                       |

PDF metadata dates are fixed to `D:19700101000000+00'00'`. After bounded
capture, the controller resolves the pinned PDF 1.4 classic xref table and its
trailer Info object, then replaces the two UTC date values without changing byte
lengths or offsets. Unsupported or ambiguous PDF structures fail the job.
Repeated real PDF and PNG captures must match byte for byte.

The og-image render uses the same pipeline and the same pinned environment, at
its own viewport rather than `@page`; it inherits every determinism rule here.
The exact image path, live-state gate, and crop are defined by
[ADR 0032](../../adr/0032-public-share-image.md).

Operationally, prints run one at a time inside the Go task's 512 MiB whole-task
budget, with a configured 20-second cancellation deadline from admission and
process-group kill. Every result waits for joined teardown. The
[numeric budgets](../budgets.md) own those values. A template that exceeds them
is defective; every golden fixture must print within the limits.
