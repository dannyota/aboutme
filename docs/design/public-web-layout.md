# Public resume web layout

Status: Accepted (2026-11-18). The owner chose the "separate sheets" direction
from mockups on 2026-11-18, with the defaults under
[Owner decisions](#owner-decisions). It amends
[ADR 0020](../adr/0020-application-visual-identity.md) for the public page and
the editor's Web preview on screen. The document contract
([ADR 0004](../adr/0004-resume-document-contract.md)), the render envelope and
the PDF are unchanged.

## What a viewer sees

The public resume at `/{slug}` reads like pages of a book on a phone: the resume
is set on a few sheets of paper with softly rounded corners and a quiet shadow,
on a slightly darker desk. Type stays at resume scale. Each sheet ends with a
small folio: the owner's name and the sheet number, such as "1 / 5". The page
scrolls vertically, so the corners and edges come back every screen or so.

## Owner decisions

On 2026-11-18 the owner chose:

1. Separate sheets, not one long sheet and not sideways page turning.
2. Warm paper by default on screen.
3. In Dark, one charcoal paper for every template, keeping each template's
   accent.
4. One column on screen; the PDF keeps the template's columns.
5. Contacts as labelled rows with dotted leaders, not large Call and Email
   buttons.
6. Browser print of the public page prints the one-column resume flat; the PDF
   remains the faithful print.

## Where it applies

| Surface                                         | Layout                                |
| ----------------------------------------------- | ------------------------------------- |
| Public page `/{slug}` on screen                 | Sheets                                |
| Editor Web preview                              | Sheets, sized by the preview pane     |
| Browser print of the public page                | Sheets flattened, see [Print](#print) |
| PDF, print route, editor paged preview          | Unchanged                             |
| Link-preview card, sign-in gate, thumbnails     | Unchanged                             |
| Template pages, template detail, landing sample | Unchanged                             |
| Project pages under `/{slug}/projects`          | Unchanged; their own design owns them |

The render envelope from Go keeps `mode: "continuous"`. The public app and the
editor's Web preview select the renderer's internal render-context mode `sheets`
for the screen; nothing crosses the process boundary. Print, the PDF, the print
route and every other surface keep their mode.

## Sheets

**Grouping.** Sections are taken in the order the one-column layout reads them:
the main column, then the side column. A section with no visible entry is not
rendered and does not count.

1. Sheet 1 always renders and holds the header, even an empty one. When the
   first visible section in that order has `sectionType` `profile`, it joins
   sheet 1 as the summary.
2. Every other `profile`, `work`, `education` and `project` section, and a
   `custom` section with three or more visible entries, starts a new sheet and
   holds it alone.
3. A run of consecutive small sections, meaning `skill`, `language`,
   `certificate`, and `custom` with fewer than three visible entries, shares one
   sheet, at most three sections per sheet; a fourth starts the next sheet. A
   run continues across the boundary from the main to the side column.
4. A sheet never splits a section. A long section makes a long sheet.
5. The last section in a sheet has no bottom margin, and the last sheet is
   followed by the same gap as the others.

**Folio.** Each sheet ends with a folio element that carries two attributes:
`data-folio-name`, exactly `fullName`, absent when `fullName` is `""`; and
`data-folio-page`, exactly `{n} / {N}` with single spaces around the slash. CSS
generated content draws them, the name from `::before` and the page from
`::after`, in a row with the space between them, so the name sits at the start
and the number at the end. Each declares `content: attr(...)` first and then
`content: attr(...) / ""`, so browsers that know alternative text keep it out of
speech, copying, find in page and indexing, and older ones still show it. The
folio sits at the sheet's foot in letterspaced capitals at 0.7 em in the meta
color.

**Geometry.** There is one resume article. In sheets mode it is the page root's
child at full width with no padding, it is the inline-size container, and the
sheets are its children, so their container units measure the page width (the
preview pane in the editor). Each sheet's content box is a container too, and
the 480 px rules for the side photo and justified text measure it, so both
follow the text width.

| Property    | Value                                                                                                            |
| ----------- | ---------------------------------------------------------------------------------------------------------------- |
| Sheet width | `min(100% - 20px, 46rem)`, centred                                                                               |
| Padding     | top `clamp(28px, 6cqi, 60px)`, inline `clamp(22px, 9cqi, 84px)`, bottom `clamp(20px, 3cqi, 30px)`                |
| Radius      | `clamp(14px, 1.5cqi, 18px)`                                                                                      |
| Gap         | `clamp(14px, 2.5cqi, 26px)`                                                                                      |
| Folio space | `clamp(30px, 4cqi, 44px)` above the folio                                                                        |
| Shadow      | `0 0 0 0.5px rgb(S / .14), 0 1px 2px rgb(S / .08), 0 10px 26px -8px rgb(S / .2), 0 32px 60px -28px rgb(S / .22)` |

Sheets are plain `div` elements with no role or name, have no
`overflow: hidden`, and in `forced-colors` mode draw a 1 px `CanvasText` border.

## Paper and desk

| Scheme | Paper                                                                                            | Desk      | Shadow `S` |
| ------ | ------------------------------------------------------------------------------------------------ | --------- | ---------- |
| Light  | `#faf6ec` when `colors.background` is `#ffffff` in any letter case; else the authored background | `#e6dfd0` | `74 54 22` |
| Dark   | `#2a2723`; when the authored background has OKLCH lightness below 0.5, that background           | `#141210` | `0 0 0`    |

- The desk is fixed per scheme, so a root rule sets it and overscroll shows it.
- The paper replaces the ground in the
  [color roles](templates/colors.md#4-color-roles): roles derive against it with
  the same contrast floors. In Dark the paper replaces step 3 of the
  [dark palette rule](public-page-dark-palette.md), and step 6 places a light
  surface at the paper's lightness plus 0.07 and lifts a dark surface to at
  least that lightness.
- Tinted header bands keep their surfaces. In Light they stay faint against the
  warm paper (1.03 to 1.11:1), which is accepted: bands are tints, and their
  text roles hold the floors. Rules, tracks and tints mix the accent into the
  paper.
- `customization.colorScheme` keeps its meaning: Light, Dark, or Match device
  through `prefers-color-scheme`. No new field.

## Inside a sheet

**Honored from the customization:** fonts, the five colors through the roles,
section heading style, rule and icons, level widget style, date format, section
order, header alignment for the name and headline, photo position and
`iconStyle`. A side photo stays beside the text at every width and never
restacks in sheets: below a 480 px content box it is 66 px wide with a 14 px
gap, and from 480 px it keeps its normal size. `top` stays on top. The
continuous restack below 36em still applies outside sheets, and printed and
paged output keep the side layout as today.

**Overridden on screen in sheets, and only there:**

| Leaf                          | On screen                                                           |
| ----------------------------- | ------------------------------------------------------------------- |
| `layout.columns`, side tint   | One column; the side column's tint is not drawn                     |
| `header.detailsLayout`        | Contact rows, start-aligned whatever `header.align` says            |
| Label omitted beside icons    | Rows always show the label                                          |
| `font.baseSizePx`             | `max(baseSizePx, 16px)`; every size keeps its multiple of that base |
| `spacing.lineHeight`          | At least 1.5                                                        |
| `font.textAlign` `justify`    | Start-aligned when the container is narrower than 480 px            |
| `spacing.pageMargin`          | Replaced by the sheet padding                                       |
| `colors.background` `#ffffff` | Warm paper, as above                                                |

Entry bodies, profile text and the headline are at most 62ch wide. Floored type
sizes are emitted only in sheets mode. The PDF keeps every value exactly.

**Contacts.** One `div` row per contact, keeping the `contact-chip` and
`contact-label` classes with a sheets modifier, at least 44 px high on screen:

- The icon, when `iconStyle` is not `none`.
- The label, with no colon: `detail.label`, else the type's default label.
- A dotted leader drawn as a CSS border, ending where the value starts.
- The value.

The value is the link under today's rules (ADR 0013), and its anchor text is
still the value. A link with a `display` keeps the label column and the leader,
and its display text is the anchor text. A pseudo-element of the anchor
stretches its hit area over the whole row, and `:focus-visible` outlines the row
in `--color-link`. A long value wraps below the label with
`overflow-wrap: anywhere`, and a wrapped value has no leader. Label columns are
not aligned across rows. Location is a row without a link. Rows stay inside
`header.resume-header`.

## Page bar and invite

The [page bar](public-page-bar.md) has no ground of its own and no bottom rule
and sits on the desk inside `main`, outside the article. Its inner row spans the
sheet's outer width with inline padding `clamp(6px, 2cqi, 26px)`, so on a phone
the row starts 16 px from the edge (10 px of desk plus 6 px) and keeps one row.
Its scoped tokens measure, on the light and dark desks: credit text 5.54:1 and
6.55:1, mark 3.59:1 and 6.51:1, button label 12.42:1 and 11.68:1. The sign-in
join invite floats on the desk with its own palette and is placed against the
first sheet with today's breakpoints. Project pages keep the bar with its ground
and rule.

## Print

`@media print` on the public page drops the desk, sheet shadows, radius, gaps
and folios, and prints on the authored background with the roles derived against
it; the paper-ground roles apply only under a screen rule. It prints at the full
printable width with no forced breaks between sheets. The template's page
margins become inline padding on every page and block padding only at the
article's start and end. It keeps the one column, the contact rows, the labels,
the line height of at least 1.5 and the justify rule, and drops the type floor,
the 62ch measure and the 44 px row height. The PDF, with the template's columns
and exact sizes, is the faithful print, offered by the bar's download.

## Editor Web preview

Web mode renders sheets on the desk and drops the chrome `.preview-sheet` frame,
as the paged preview already does: no pane padding, no canvas and no frame, and
the renderer paints the desk. Its container is the preview pane, so its geometry
follows the pane's width.

## Validator, goldens and assertions

- **Public HTML validator.** A page has at least one folio. Folios'
  `data-folio-page` values are exactly `1 / N` to `N / N` in order, N equal to
  the folio count. Each folio carries `data-folio-name` exactly when the
  envelope's `fullName` is not `""`, and its value equals that name; Go builds
  the expected strings and never parses a name. Contact rows stay inside
  `header.resume-header`, so the `contact_link` check holds. Nothing else
  changes.
- **Goldens.** Every preset in sheets mode for the one- and two-column starting
  states, owner Light, Dark and Match device. The existing continuous and paged
  goldens stay byte-identical.
- **Grouping tests.** No summary; a `profile` section later in the order; every
  entry of a section hidden; many small sections; the column boundary.
- **Display assertions,** each with a clean, mutated and restored proof:
  - paper and desk per scheme, including designer-tag (an authored paper) and an
    already-dark template;
  - Match device with both viewer preferences;
  - sheet width, padding, radius and gap at 320, 390, 768 and 1440 px;
  - folio name and page through both attributes and non-empty `::before` and
    `::after` boxes, and their absence from the text layer;
  - one column at every width and no horizontal overflow at 320 px;
  - contact rows at least 44 px high with the hit area covering the row;
  - body at least 16 px and text blocks at most 62ch;
  - print media: authored ground, no desk, no folio, one column;
  - forced colors: sheet borders present;
  - the editor's Web preview: sheets on the desk, no `.preview-sheet` frame, and
    sheet shadows not clipped. The render harness's `public` mode renders
    sheets.

## Accessibility

Landmarks stay as today: the bar inside `main` and outside the article. In
sheets mode the side column's sections render as ordinary sections, so there is
no `aside`. Headings keep their levels across sheets. Folios are not in the
accessibility tree. Contact rows keep the value as the accessible name.

## Revocation and privacy

The page is one HTML response through the live-state gate with the same cache
key inputs; the renderer digest changes with the release. No new representation,
request or script; fonts stay bundled.
[ADR 0010](../adr/0010-public-artifact-revocation.md) and
[ADR 0022](../adr/0022-viewer-privacy-and-counting.md) hold unchanged.

## Release

One release: the sheets layout, the validator rule, the editor preview, the
goldens and the assertions. It also refreshes the template pages manifest from a
hosted `template-pages.yml` run at landing, updates the public corpus HTML in
`apps/server/internal/publicapi/testdata/resume-links-render.json` by hand from
the renderer's exact output, adds an already-dark test fixture, and regenerates
the web source manifest. No document version, migration or OpenAPI change, so a
rollback across it is safe.
