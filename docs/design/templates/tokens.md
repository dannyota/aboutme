# Template design tokens

Status: **Approved v2** (2026-08-12).

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

The complete document `customization` has 25 leaves: 23 author-controlled and 2
derived placement arrays. Eight leaves are optional. Ranges and enums are the
schema's, not this document's.

| Token                           | Type                                            | Baseline (`fixtures/minimal.json`) | Owner        |
| ------------------------------- | ----------------------------------------------- | ---------------------------------- | ------------ |
| `font.family`                   | stable ID from the released font catalog        | `inter`                            | user, preset |
| `font.baseSizePx`               | integer 10–20                                   | `14`                               | user, preset |
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
| `layout.columns`                | enum: 1, 2                                      | `1`                                | user, preset |
| `layout.surfaceTarget`          | enum: `none`, `header`, `sidebar`, **optional** | absent (renders as `none`)         | user, preset |
| `layout.sections.main/.sidebar` | arrays of section keys                          | `[]`, `[]`                         | derived      |
| `sectionDisplay.skill.style`    | enum: `text`, `tag`, `bar`, `dots`              | `text`                             | user, preset |
| `sectionDisplay.language.style` | enum: `text`, `tag`, `bar`, `dots`              | `text`                             | user, preset |
| `pageFormat`                    | enum: `a4`, `letter`                            | `a4`                               | user, preset |
| `dateFormat`                    | enum: `MM/YYYY`, `Mon YYYY`, `YYYY`             | `MM/YYYY`                          | user, preset |

`minimal.json` is the repository's baseline document, not a declared default; no
canonical default customization exists in code yet. A preset therefore states
the 15 required author-controlled leaves explicitly, adds its required
`layout.placement` rule, and never contains the 2 derived `layout.sections`
arrays. `applyTemplate` computes those arrays. This accounts for all 17 required
document leaves without inventing preset-owned placement arrays.

The eight optional leaves work differently. The
[document-version rule](../data.md#document-versions) requires new fields to
start optional so adding one does not force an all-document migration.
`customization` therefore still has exactly the eight required keys
`contract.md` §1 names, and documents written before the optional fields existed
stay valid unchanged. A preset may omit any of the eight, and omission is not a
gap to be filled: the renderer applies the fallback in the "Baseline" column
above at the point of use, and nothing writes that fallback back into the
document. Absence means "never entered," so the editor can distinguish a user
who has never opened the header panel from one who chose `align: "left"`, even
though the two render identically. `contract.md` §6 records the same
absent-versus-cleared rule for text fields.

Two of them are **complete-or-absent** rather than per-leaf optional:
`spacing.pageMargin` requires both `x` and `y` once present, and
`customization.header` requires all three of its fields. A margin or a header
treatment has no half-specified form, so the schema offers the whole object or
nothing, matching every other nested object in the document.

`customization.header` is the **resume header** — the top block of name,
headline, photo, and contact details (`contract.md` §5.1).
`customization.heading` is the **section heading** — a section's `displayName`
and its rule (`contract.md` §5.3). The names are one letter apart and mean
different blocks of the page; both schema descriptions say so, and so does §3.4
below.

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

### 3.2 Weights

The renderer requests weights 400 and 700 and sets `font-synthesis: none`. The
catalog reports which faces a family provides. A missing requested face uses the
bundled category fallback; it does not exclude the family or let Chromium
synthesize metrics. Preset defaults may choose families that provide both
weights, but catalog admission remains license-only.

### 3.3 Section heading treatment

`heading.style` — the per-**section** heading, not the resume header — maps to:

| Value       | Transform                    | Letter spacing |
| ----------- | ---------------------------- | -------------- |
| `uppercase` | `text-transform: uppercase`  | `0.06em`       |
| `titlecase` | `text-transform: capitalize` | `0`            |
| `normal`    | none                         | `0`            |

`text-transform` is locale-sensitive in Chromium, so the resume root carries the
server-normalized render language. A valid `resumes.lng` becomes its canonical
BCP 47 form; null, empty, or invalid legacy data becomes `und`. Without this
total mapping, the print container's locale could change casing and break
snapshot determinism (`print.md` §7).

### 3.4 Resume header treatment

`customization.header` governs the top block only: photo, `fullName`,
`headline`, then `personalDetails.details` in array order (`contract.md` §5.1).
It is presentation, never content — no value here adds, removes, reorders, or
reveals a detail, and `isHidden` still wins in every combination.

| Token                  | Value     | Effect                                                            |
| ---------------------- | --------- | ----------------------------------------------------------------- |
| `header.align`         | `left`    | the resolved header block sits on the content measure's left edge |
|                        | `center`  | photo, name, headline, and details all centre together            |
| `header.detailsLayout` | `inline`  | details flow on one wrapping line, separated by `--gap-inline`    |
|                        | `stacked` | each detail takes its own line, separated by `--gap-block`        |
| `header.iconStyle`     | `none`    | no icon before a contact detail; the label or value stands alone  |
|                        | `outline` | the stroked lucide glyph at `--icon-size`                         |

Absent `header` renders `left` / `inline` / `outline`, which is what every
document rendered before the token existed.

The enum is `none` | `outline`. Lucide is stroke-only, so a `solid` value would
require a second icon family or a `fill: currentColor` hack that turns many
marks into blobs. The schema and every preset therefore use `outline` for a
visible header icon.

Two boundaries this token must not cross. `header.iconStyle` covers the header's
contact icons only — it never suppresses a section's `iconKey`, which every
template renders (`limitations.md` §9.5). And `align: "center"` centres the
block, not the page: it changes no margin and no column ratio.
