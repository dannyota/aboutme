# Template design tokens

The complete token set a template may control, who owns each token, and the
accessibility floor every template satisfies regardless of what the user picks.

## 1. How a token reaches the page

`resolveRenderModel(currentDocument, renderContext)` is the renderer's single
pure input boundary, as required by the
[pure renderer](../web.md#pure-renderer). It resolves structural customization
into component props and passes a CSS-valued token projection to
`useResumeStyles`. The latter returns the scoped CSS custom properties used by
editor preview, server-rendered public pages, and Chromium print.

Layout arrays and columns, header behavior, `sectionDisplay`, `dateFormat`, and
`pageFormat` remain typed fields in the resolved model; they are not encoded as
CSS to hide structural decisions. Font, colors, spacing, heading treatment, page
geometry, and surface treatment form the CSS-valued projection. After
resolution, no component reads raw `customization`, and no component invents a
color, size, order, or fallback of its own.

Ownership has exactly three values in the tables below.

| Owner            | Meaning                                                                       |
| ---------------- | ----------------------------------------------------------------------------- |
| **user, preset** | a leaf in `customization`; the user edits it, and a preset sets it on apply   |
| **derived**      | computed from user, preset values by a pure function; not directly settable   |
| **renderer**     | fixed in the codebase, identical in every template; see `limitations.md` §9.1 |

There is no fourth column for "the template fixes it". Because the document
stores no template identity (`contract.md` §1), any value not in `customization`
is the same in every template by construction.

## 2. Customization leaves

The complete document `customization` has 27 leaves: 25 author-controlled and 2
derived placement arrays. Ten leaves are optional. Ranges and enums are the
schema's, not this document's.

| Token                           | Type                                            | Baseline (`fixtures/minimal.json`) | Owner        |
| ------------------------------- | ----------------------------------------------- | ---------------------------------- | ------------ |
| `font.family`                   | stable ID from the released font catalog        | `inter`                            | user, preset |
| `font.baseSizePx`               | integer 10–20                                   | `14`                               | user, preset |
| `font.textAlign`                | enum: `left`, `justify`, **optional**           | absent (renders `left`)            | user         |
| `colors.primary`                | `#rrggbb`                                       | `#1a1a1a`                          | user, preset |
| `colors.text`                   | `#rrggbb`                                       | `#1a1a1a`                          | user, preset |
| `colors.background`             | `#rrggbb`                                       | `#ffffff`                          | user, preset |
| `colors.accent`                 | `#rrggbb`, **optional**                         | absent                             | user, preset |
| `colors.surface`                | `#rrggbb`, **optional**                         | absent                             | user, preset |
| `spacing.sectionGap`            | number 0–64 (px)                                | `16`                               | user, preset |
| `spacing.entryGap`              | number 0–64 (px)                                | `8`                                | user, preset |
| `spacing.lineHeight`            | number 1–2.5 (unitless)                         | `1.4`                              | user, preset |
| `spacing.pageMargin.x`          | number 0–40 (mm), **optional pair**             | absent (renderer 15 mm)            | user, preset |
| `spacing.pageMargin.y`          | number 0–40 (mm), **optional pair**             | absent (renderer 15 mm)            | user, preset |
| `heading.style`                 | enum: `uppercase`, `titlecase`, `normal`        | `normal`                           | user, preset |
| `heading.showRule`              | boolean                                         | `false`                            | user, preset |
| `header.align`                  | enum: `left`, `center`, **optional object**     | absent (renderer `left`)           | user, preset |
| `header.detailsLayout`          | enum: `inline`, `stacked`, **optional object**  | absent (renderer `inline`)         | user, preset |
| `header.iconStyle`              | enum: `none`, `outline`, **optional object**    | absent (renderer `outline`)        | user, preset |
| `header.photoPosition`          | enum: `top`, `left`, `right`, **optional**      | absent (renders `top`)             | user         |
| `layout.columns`                | enum: 1, 2                                      | `1`                                | user, preset |
| `layout.surfaceTarget`          | enum: `none`, `header`, `sidebar`, **optional** | absent (renders as `none`)         | user, preset |
| `layout.sections.main/.sidebar` | arrays of section keys                          | `[]`, `[]`                         | derived      |
| `sectionDisplay.skill.style`    | enum: `text`, `tag`, `bar`, `dots`              | `text`                             | user, preset |
| `sectionDisplay.language.style` | enum: `text`, `tag`, `bar`, `dots`              | `text`                             | user, preset |
| `pageFormat`                    | enum: `a4`, `letter`                            | `a4`                               | user, preset |
| `dateFormat`                    | enum: `MM/YYYY`, `Mon YYYY`, `YYYY`             | `MM/YYYY`                          | user, preset |

`minimal.json` is a baseline document, not a declared default. A preset states
the 15 required author-controlled leaves and its `layout.placement` rule, and
never contains the two derived `layout.sections` arrays, which `applyTemplate`
computes.

New fields start optional under the
[document-version rule](../data.md#document-versions), so `customization` keeps
exactly the eight required keys of `contract.md` §1. A preset may omit any
optional leaf. The renderer applies the "Baseline" fallback at the point of use
and never writes it back, so absence still means "never entered" (`contract.md`
§6).

Two structures have grouped requirements. `spacing.pageMargin` requires both `x`
and `y` once present. `customization.header` requires `align`, `detailsLayout`,
and `iconStyle` together, while `photoPosition` remains optional. A margin or a
header missing any required child is invalid rather than partially defaulted.

`customization.header` is the **resume header**: name, headline, photo, and
contact details (`contract.md` §5.1). `customization.heading` is the **section
heading**: a section's `displayName` and its rule (`contract.md` §5.3).

`layout.sections` is marked derived because `applyTemplate` computes it from the
document's content keys (ADR 0008) and only `PATCH /resumes/{id}/structure` may
rewrite it (ADR 0009). A preset never contains section keys.

## 3. Typography

### 3.1 Scale

Every size is a renderer-fixed multiple of `--fs-base`, which is
`font.baseSizePx`. A template varies size only by moving the base.

| Property        | Multiple of base | Weight | Line height    | Used by                    |
| --------------- | ---------------- | ------ | -------------- | -------------------------- |
| `--fs-name`     | 2.00             | 700    | `--lh-heading` | `personalDetails.fullName` |
| `--fs-headline` | 1.15             | 400    | `--lh-heading` | `personalDetails.headline` |
| `--fs-heading`  | 1.10             | 700    | `--lh-heading` | section `displayName`      |
| `--fs-title`    | 1.00             | 700    | `--lh-heading` | entry `title` slot         |
| `--fs-subtitle` | 1.00             | 400    | `--lh-body`    | entry `subtitle` slot      |
| `--fs-body`     | 1.00             | 400    | `--lh-body`    | rich text, contact values  |
| `--fs-meta`     | 0.90, min 9 px   | 400    | `--lh-body`    | dates, place, level labels |

`--lh-body` is `spacing.lineHeight`. `--lh-heading` is renderer-fixed at `1.2`.

`font.textAlign` sets `--body-align`. `justify` justifies entry and summary body
paragraphs and list items and sets `hyphens: auto`, which hyphenates by the
resume's `lang`. Headings, the header, dates and meta lines, contact rows, and
skill or language tags never justify. A template switch keeps the user's choice,
so presets do not set it (ADR 0041).

### 3.2 Weights

The renderer requests weights 400 and 700 and sets `font-synthesis: none`. The
catalog reports which faces a family provides. A missing requested face uses the
bundled category fallback; it does not exclude the family or let Chromium
synthesize metrics. Preset defaults may choose families that provide both
weights, but catalog admission remains license-only.

### 3.3 Section heading treatment

`heading.style`, the per-**section** heading, maps to:

| Value       | Transform                    | Letter spacing |
| ----------- | ---------------------------- | -------------- |
| `uppercase` | `text-transform: uppercase`  | `0.06em`       |
| `titlecase` | `text-transform: capitalize` | `0`            |
| `normal`    | none                         | `0`            |

Title case is an English convention. A resume whose language is `vi` keeps
`titlecase` headings as typed (`text-transform: none`), since Vietnamese
capitalizes only the first word.

`text-transform` is locale-sensitive in Chromium, so the resume root carries the
server-normalized render language. A valid `resumes.lng` becomes its canonical
BCP 47 form; null, empty, or invalid legacy data becomes `und`. Without this
total mapping, the print container's locale could change casing and break
snapshot determinism (`print.md` §7).

### 3.4 Resume header treatment

`customization.header` governs the top block only: photo, `fullName`,
`headline`, then `personalDetails.details` in array order (`contract.md` §5.1).
It is presentation, never content: no value adds, removes, reorders, or reveals
a detail, and `isHidden` always wins.

| Token                  | Value     | Effect                                                                                 |
| ---------------------- | --------- | -------------------------------------------------------------------------------------- |
| `header.align`         | `left`    | the resolved header block sits on the content measure's left edge                      |
|                        | `center`  | photo, name, headline, and details all center together; a side photo stays at its edge |
| `header.detailsLayout` | `inline`  | details flow on wrapping rows (header intervals below)                                 |
|                        | `stacked` | each detail takes its own line, at the same row gap                                    |
| `header.iconStyle`     | `none`    | no icon before a contact detail; the label or value stands alone                       |
|                        | `outline` | the contact glyph at `--icon-size`; replaces default label                             |
| `header.photoPosition` | `top`     | the photo sits above the name; absent means `top`                                      |
|                        | `left`    | the photo sits left of the text block, which `header.align` aligns                     |
|                        | `right`   | the photo sits right of the text block, which `header.align` aligns                    |

Absent `header` renders `left` / `inline` / `outline` / `top`. A side photo
keeps `--photo-size` and is vertically centered against the text block
(`contract.md` §5.1).

With `outline`, a typed detail's icon stands in for its default label, which is
omitted. A non-empty user `label` and a `custom` detail's label still render,
and no value is ever hidden (ADR 0040).

The enum is `none` | `outline`. Lucide is stroke-only, so a `solid` value would
require a second icon family or a `fill: currentColor` hack that turns many
marks into blobs. The schema and every preset therefore use `outline` for a
visible header icon. The GitHub, LinkedIn, and X contacts are the exception:
they render their filled brand marks (Simple Icons for GitHub and X, Font
Awesome Free for LinkedIn) in the icon colour, because those brands have no
stroked mark (ADR 0041).

Header intervals grow outward, so each icon reads with its own value and the
details read as one group under the headline. The renderer fixes them for every
template:

| Property               | Value               | Between                                      |
| ---------------------- | ------------------- | -------------------------------------------- |
| `--chip-icon-gap`      | `0.3em`             | a contact icon and its value                 |
| `--details-row-gap`    | `0.15 × lineHeight` | detail rows; row pitch equals the headline's |
| `--details-column-gap` | `1em`               | details on one row                           |
| `--header-name-gap`    | `0.2em`             | name and headline                            |
| `--header-details-gap` | `0.5em`             | headline and the first detail row            |
| `--header-photo-gap`   | `0.9em`             | photo and name                               |
| `--gap-header`         | `1.5 × sectionGap`  | header and body (`geometry.md` §6)           |

Contact icons take `--color-meta`, which holds 4.5:1 against the header surface,
so the value leads and the icon still prints.

`header.iconStyle` covers only the header's contact icons and never suppresses a
section's `iconKey` (`limitations.md` §9.5). `align: "center"` centres the
block, not the page, and changes no margin or column ratio.
