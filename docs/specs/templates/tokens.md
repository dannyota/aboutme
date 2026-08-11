# Template design tokens

DRAFT v1 (2026-08-11) — not approved.

The complete token set a template may control, who owns each token, and the
accessibility floor every template satisfies regardless of what the user picks.

## 1. How a token reaches the page

`useResumeStyles(customization)` is the single function that turns the
document's `customization` object into CSS custom properties on the resume root
element (design spec §5: "all styling via CSS custom properties computed by
`useResumeStyles`"). Editor preview, SSR public page, and Chromium print call
the same function with the same input and get byte-identical properties. No
component reads `customization` directly, and no component computes a color or a
size of its own.

Ownership has exactly three values in the tables below.

| Owner            | Meaning                                                                     |
| ---------------- | --------------------------------------------------------------------------- |
| **user, preset** | a leaf in `customization`; the user edits it, and a preset sets it on apply |
| **derived**      | computed from user, preset values by a pure function; not directly settable |
| **renderer**     | fixed in the codebase, identical in every template; see `contract.md` §9.1  |

There is no fourth column for "the template fixes it". Because the document
stores no template identity (`contract.md` §1), any value not in `customization`
is the same in every template by construction.

## 2. Author-controlled tokens (the whole of `customization`)

Twenty-four leaves, seven of them optional. Ranges and enums are the schema's,
not this document's.

| Token                           | Type                                                               | Baseline (`fixtures/minimal.json`) | Owner        |
| ------------------------------- | ------------------------------------------------------------------ | ---------------------------------- | ------------ |
| `font.family`                   | enum: Be Vietnam Pro, Inter, Source Sans 3, Alegreya, Roboto Serif | `Inter`                            | user, preset |
| `font.baseSizePx`               | integer 10–20                                                      | `14`                               | user, preset |
| `colors.primary`                | `#rrggbb`                                                          | `#1a1a1a`                          | user, preset |
| `colors.text`                   | `#rrggbb`                                                          | `#1a1a1a`                          | user, preset |
| `colors.background`             | `#rrggbb`                                                          | `#ffffff`                          | user, preset |
| `colors.accent`                 | `#rrggbb`, **optional**                                            | absent                             | user, preset |
| `colors.surface`                | `#rrggbb`, **optional**                                            | absent                             | user, preset |
| `spacing.sectionGap`            | number 0–64 (px)                                                   | `16`                               | user, preset |
| `spacing.entryGap`              | number 0–64 (px)                                                   | `8`                                | user, preset |
| `spacing.lineHeight`            | number 1–2.5 (unitless)                                            | `1.4`                              | user, preset |
| `spacing.pageMargin.x`          | number 0–40 (mm), **optional pair**                                | absent (renderer 15 mm)            | user, preset |
| `spacing.pageMargin.y`          | number 0–40 (mm), **optional pair**                                | absent (renderer 15 mm)            | user, preset |
| `heading.style`                 | enum: `uppercase`, `titlecase`, `normal`                           | `normal`                           | user, preset |
| `heading.showRule`              | boolean                                                            | `false`                            | user, preset |
| `header.align`                  | enum: `left`, `center`, **optional object**                        | absent (renderer `left`)           | user, preset |
| `header.detailsLayout`          | enum: `inline`, `stacked`, **optional object**                     | absent (renderer `inline`)         | user, preset |
| `header.iconStyle`              | enum: `none`, `outline`, `solid`, **optional object**              | absent (renderer `outline`)        | user, preset |
| `layout.columns`                | enum: 1, 2                                                         | `1`                                | user, preset |
| `layout.surfaceTarget`          | enum: `none`, `header`, `sidebar`, **optional**                    | absent (renders as `none`)         | user, preset |
| `layout.sections.main/.sidebar` | arrays of section keys                                             | `[]`, `[]`                         | derived      |
| `sectionDisplay.skill.style`    | enum: `text`, `tag`, `bar`, `dots`                                 | `text`                             | user, preset |
| `sectionDisplay.language.style` | enum: `text`, `tag`, `bar`, `dots`                                 | `text`                             | user, preset |
| `pageFormat`                    | enum: `a4`, `letter`                                               | `a4`                               | user, preset |
| `dateFormat`                    | enum: `MM/YYYY`, `Mon YYYY`, `YYYY`                                | `MM/YYYY`                          | user, preset |

`minimal.json` is the repository's baseline document, not a declared default; no
canonical default customization exists in code yet, so a preset must state all
seventeen **required** values explicitly rather than relying on omission.

The seven optional leaves work differently, and deliberately so. Design spec §3
requires every new field to arrive optional, so that adding one is never an
all-document migration; `customization` therefore still has exactly the eight
required keys `contract.md` §1 names, and every document written before
2026-08-11 stays valid unchanged. A preset may omit any of the seven, and
omission is not a gap to be filled: the renderer applies the fallback in the
"Baseline" column above at the point of use, and nothing writes that fallback
back into the document. Absence means "never entered" (design spec §3), so the
editor can distinguish a user who has never opened the header panel from one who
chose `align: "left"`, even though the two render identically — the same
absent-versus-cleared relationship `contract.md` §6 records for text fields.

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

**400 and 700 only.** These are the two weights guaranteed present in all five
self-hosted subsets; restricting to them keeps rendering identical across
families and prevents Chromium from synthesizing a bold when a cut is missing —
synthetic bolding is a determinism hazard for golden snapshots. The subsetting
build step must fail if any of the five families ships without either weight.

### 3.3 Section heading treatment

`heading.style` — the per-**section** heading, not the resume header — maps to:

| Value       | Transform                    | Letter spacing |
| ----------- | ---------------------------- | -------------- |
| `uppercase` | `text-transform: uppercase`  | `0.06em`       |
| `titlecase` | `text-transform: capitalize` | `0`            |
| `normal`    | none                         | `0`            |

`text-transform` is locale-sensitive in Chromium, so the resume root carries an
explicit `lang` from `resumes.lng`. Without it, the print container's locale
could change casing and break snapshot determinism (`print.md` §7).

### 3.4 Resume header treatment

`customization.header` governs the top block only: photo, `fullName`,
`headline`, then `personalDetails.details` in array order (`contract.md` §5.1).
It is presentation, never content — no value here adds, removes, reorders, or
reveals a detail, and `isHidden` still wins in every combination.

| Token                  | Value     | Effect                                                                           |
| ---------------------- | --------- | -------------------------------------------------------------------------------- |
| `header.align`         | `left`    | `--header-align: left`; the block sits on the content measure's left edge        |
|                        | `center`  | `--header-align: center`; photo, name, headline, and details all centre together |
| `header.detailsLayout` | `inline`  | details flow on one wrapping line, separated by `--gap-inline`                   |
|                        | `stacked` | each detail takes its own line, separated by `--gap-block`                       |
| `header.iconStyle`     | `none`    | no icon before a contact detail; the label or value stands alone                 |
|                        | `outline` | the stroked lucide glyph at `--icon-size`                                        |
|                        | `solid`   | the filled lucide glyph at `--icon-size`                                         |

Absent `header` renders `left` / `inline` / `outline`, which is what every
document rendered before the token existed.

Two boundaries this token must not cross. `header.iconStyle` covers the header's
contact icons only — it never suppresses a section's `iconKey`, which every
template renders (`contract.md` §9.5). And `align: "center"` centres the block,
not the page: it changes no margin and no column ratio.

## 4. Color roles

Templates and components address roles, never the raw hex values. Every role is
derived by `useResumeStyles`; the raw values in `customization.colors` are never
mutated.

| Role                      | Source                                                                         | Contrast target              |
| ------------------------- | ------------------------------------------------------------------------------ | ---------------------------- |
| `--color-surface`         | `colors.background`                                                            | — (it is the surface)        |
| `--color-surface-header`  | `colors.surface` if the effective target is `header`, else `--color-surface`   | — (it is a surface)          |
| `--color-surface-sidebar` | `colors.surface` if the effective target is `sidebar`, else `--color-surface`  | — (it is a surface)          |
| `--color-heading`         | `colors.primary`, clamped                                                      | 4.5:1 on its surface         |
| `--color-body`            | `colors.text`, clamped                                                         | 4.5:1 on its surface         |
| `--color-meta`            | `colors.text` mixed 25% toward the surface, then clamped                       | 4.5:1                        |
| `--color-accent`          | `colors.accent` if present, else `colors.primary`                              | raw, not for direct use      |
| `--color-accent-text`     | `--color-accent`, clamped                                                      | 4.5:1 on its surface         |
| `--color-accent-solid`    | `--color-accent`, clamped                                                      | 3:1 on its surface           |
| `--color-on-accent`       | `#000000` or `#ffffff`, whichever scores higher against `--color-accent-solid` | 4.5:1                        |
| `--color-link`            | alias of `--color-accent-text`                                                 | 4.5:1                        |
| `--color-rule`            | `--color-accent` mixed 60% toward the surface                                  | 1.5:1, decorative only       |
| `--color-track`           | `--color-accent` mixed 80% toward the surface                                  | — none, by design (see note) |

Notes:

- **`--color-track` carries no contrast floor, and cannot.** Its derivation
  (accent mixed 80% toward the surface) caps it at 1.61:1 on a white surface
  even for a pure-black accent — verified independently by two preset designs.
  This is acceptable because the track is not the meaning-carrier: in a level
  widget only the FILLED portion asserts anything, and it meets 3:1 against both
  track and surface. The §5 "meaningful non-text" floor therefore applies to the
  filled bar, filled dots, and chip fill — never to the track. Renderer
  consequence: a level widget must remain correct when the track is invisible,
  which is why an absent `level` renders no widget at all rather than an empty
  track (contract §5.6) — an empty track that might not be visible cannot be the
  difference between "rated 0" and "unrated".
- `colors.accent` and `colors.surface` are the only optional colors and neither
  can be cleared to `""` — `hexColor`'s pattern forbids it. Absent is the only
  unset state; the accent falls back to `colors.primary` and the surface to
  `colors.background`, never to a hard-coded brand color or tint.
- `--color-surface-sidebar` was reserved as a role in the first draft so that
  giving the sidebar an independent tint would be a token change rather than a
  markup change. `colors.surface` plus `layout.surfaceTarget` is that change;
  the role is no longer an unconditional alias, and `--color-surface-header`
  joins it on the same footing. Anything below "its surface" in the table above
  means whichever of the three the element actually paints on.
- `--color-on-accent` always exists: for any accent, the contrast ratios of
  black and white against it multiply to exactly 21, so the larger of the two is
  at least √21 ≈ 4.58 and clears the 4.5:1 floor. (The first draft specified an
  outline-chip fallback for the case where neither reached 4.5:1; that case is
  arithmetically unreachable, so the fallback was removed — a `tag` chip always
  fills.)
- **Mix space (normative):** every "mixed N% toward the surface" step in the
  table above is a component-wise linear interpolation of the **gamma-encoded
  sRGB channels** (the CSS `color-mix(in srgb, …)` behavior), not a mix in
  linear light or OKLab. The spaces disagree enough to change conformance:
  mixing text 25% toward white in linear light darkens the result's computed
  luminance so far that no text color could reach `--color-meta`'s 4.5:1 target
  without clamping, while the sRGB mix leaves typical dark text at 6–7:1
  unclamped. Contrast checks in preset rationale docs and golden tests must use
  this definition.

### 4.1 The effective surface target

`layout.surfaceTarget` says which region `colors.surface` fills. The renderer
resolves it to an **effective** target first, as a total function over the
stored values — it never rejects a document and never writes one:

```text
effectiveSurfaceTarget(customization):
  t = layout.surfaceTarget, or "none" when the key is absent
  if t == "none":                       return "none"
  if colors.surface is absent:          return "none"   # nothing to fill with
  if t == "sidebar" and layout.columns == 1: return "none"   # no sidebar region
  return t
```

The two degradations are the point, not an edge case. `contract.md` §7 already
requires that in one-column mode "any sidebar-specific treatment (tint, narrower
measure) degrades to the main treatment", and a user reaches exactly that state
with one click of the columns toggle. Making it a validation error would make a
document unsaveable that the editor itself produced, so both combinations stay
stored verbatim: toggling back to two columns, or re-picking a color, restores
the tint with no rewrite of either field.

A tinted header band spans the full content measure, inside `--page-margin-x`. A
tinted sidebar fills its own column, `--sidebar-ratio` wide, and continues
across page breaks with the sidebar flow (`print.md` §5).

### 4.2 Text over `colors.surface`

`colors.surface` introduces a second text-on-surface pair, and the guarantee for
it is the same one §5 gives for an arbitrary accent, applied with a different
second argument: **every clamped role is evaluated once per surface, against the
surface the element actually paints on.** Concretely, an element inside the
tinted region resolves `--color-heading`, `--color-body`, `--color-meta`,
`--color-accent-text`, `--color-accent-solid`, `--color-link`, `--color-rule`,
and `--color-track` with
`clamp(color, --color-surface-header-or-sidebar, target)` instead of
`clamp(color, --color-surface, target)`; the mix-toward-the- surface steps mix
toward that same surface. `--color-on-accent` follows, because it is chosen
against `--color-accent-solid`, which is itself per-surface.

Three consequences worth stating outright:

- The floor holds for any of the 16.7 million surfaces a user can pick,
  including a near-black band under `colors.text: #1a1a1a`. The clamp keeps the
  hue and chroma of the text color and moves only its lightness, in the
  direction the surface's own relative luminance selects, until it passes —
  4.5:1 for body, headings, meta, and links, 3:1 for `--fs-name` and for
  non-text marks that carry meaning. It cannot fail to terminate: black or white
  is the floor.
- It is non-destructive. `customization.colors` keeps the user's hexes; only the
  derived roles differ between the tinted region and the rest of the page.
- The same text color can therefore resolve to two different values on one page,
  which is correct and is why §5's "per surface" bullet is a hard rule rather
  than an optimization note. Code must not hoist a clamped role to the document
  root, and golden snapshots must cover a document with a tinted region so the
  second resolution is pinned too — `fixtures/full.json` carries one
  (`surfaceTarget: "sidebar"` over two columns).

## 5. Accessibility floor

Two hard floors, guaranteed by the renderer for every template and every user
color choice.

**Size.** Body text renders at exactly `font.baseSizePx`, whose schema floor is
10 px (7.5 pt at Chromium's 96 CSS px per inch). The smallest text the renderer
can produce is `--fs-meta` at `max(0.9 × base, 9px)`. A base below 13 px (≈ 9.75
pt) is below normal print-legibility practice; the renderer must not silently
override the user's explicit choice, so the containment is an editor warning,
and the residual risk is recorded in `contract.md` §9.7. The same reasoning
applies to `spacing.*` at `0`: legal, explicit, and not overridden. That
includes `spacing.pageMargin` at `0`, which puts content inside the unprintable
edge of most desktop printers — again an editor warning, never a schema
rejection, since a screen-only or professionally trimmed resume is a legitimate
use.

**Contrast.** Measured as the WCAG 2.x relative-luminance ratio.

| Usage                                                        | Minimum |
| ------------------------------------------------------------ | ------- |
| text below 24 px, or below 18.66 px bold                     | 4.5:1   |
| text at or above 24 px, or 18.66 px bold (`--fs-name`)       | 3:1     |
| non-text that carries meaning: level bars, dots, tag borders | 3:1     |
| decorative section rule                                      | 1.5:1   |

The rule is decorative because the heading's size and weight already signal the
section boundary; a rule may therefore be faint, but it may never be the only
boundary signal.

### How the guarantee holds for an arbitrary accent

The user may pick any of 16.7 million colors, including yellow on white. The
renderer clamps at the point of use:

```text
clamp(color, surface, target):
  if contrast(color, surface) >= target: return color
  keep the hue and chroma of color in OKLCH
  direction = darker if surface is light (relative luminance >= 0.5), else lighter
  for L from color.L, stepping 0.005 in that direction, until L leaves [0, 1]:
      candidate = gamut-clipped sRGB of oklch(L, C, H)
      if contrast(candidate, surface) >= target: return candidate
  return #000000 if surface is light, else #ffffff
```

Properties this relies on:

- **Pure and deterministic.** Same inputs, same output, in preview, SSR, and
  print. Golden snapshots pin the computed values, so a change to the clamp is a
  visible diff rather than a silent drift.
- **Hue-preserving.** The user's color identity survives; only its lightness
  moves, and only far enough to pass.
- **Non-destructive.** `customization.colors` keeps the user's hex. The clamp
  produces a different derived role, never a write.
- **Per surface.** Text in the sidebar clamps against `--color-surface-sidebar`,
  text in the header band against `--color-surface-header`, text elsewhere
  against `--color-surface`. They are equal only while `colors.surface` is
  absent or the effective target is `none`; `colors.surface` is exactly what
  makes them diverge, so the code must never assume they are equal (§4.2).
- The user's own `colors.text` on `colors.background` is clamped by the same
  rule. Light grey on white becomes dark enough to read. This is the one place
  the renderer overrides a direct user choice, and it is deliberate: an
  unreadable published resume is worse than an approximated color.

## 6. Spacing, rules, and geometry

Spacing derived from `customization.spacing` — pure multiples, so a user value
of `0` really is `0`:

| Property          | Value                                  | Applies to                          |
| ----------------- | -------------------------------------- | ----------------------------------- |
| `--gap-section`   | `spacing.sectionGap`                   | between sections in a column        |
| `--gap-entry`     | `spacing.entryGap`                     | between entries in a section        |
| `--gap-heading`   | `0.4 × sectionGap`                     | heading to first entry              |
| `--gap-block`     | `0.4 × entryGap`                       | between slots inside one entry      |
| `--gap-inline`    | `0.5em` (renderer)                     | between meta items on the same line |
| `--page-margin-x` | `spacing.pageMargin.x` mm, else `15mm` | left and right page margin          |
| `--page-margin-y` | `spacing.pageMargin.y` mm, else `15mm` | top and bottom page margin          |

The two page-margin properties are the one place a token reaches `@page`
geometry: `useResumeStyles` emits
`margin: var(--page-margin-y) var(--page-margin-x)` into the `@page` rule
(`print.md` §2), and the editor preview and public page apply the same values as
ordinary padding on the resume root. When `spacing.pageMargin` is absent both
fall back to `15mm`, which is what `print.md` §2 fixed before the token existed,
so an untouched document's geometry is unchanged. The fallback is applied here,
at the point of use; it is never written into `customization` (design spec §3:
absence means "never entered"). Margins are the primary lever for fitting a
resume onto one page, so this is the first token a page-count problem should
reach for, ahead of `baseSizePx`.

Renderer-fixed geometry. None of it varies by template in v1; changing a value
here changes every template at once.

| Property          | Value                        | Rationale                                                                |
| ----------------- | ---------------------------- | ------------------------------------------------------------------------ |
| `--rule-width`    | `1px` solid                  | one hairline that survives print rasterization at 96 dpi                 |
| `--rule-gap`      | `0.25em` below heading text  | proportional to the heading, so it tracks `baseSizePx`                   |
| `--sidebar-ratio` | `32%` of content width       | fits a two-word skill name at base 14 without wrapping                   |
| `--column-gutter` | `8mm`                        | print unit, because the gutter is a page-geometry decision               |
| `--photo-size`    | `96px`, `4px` radius, square | honors the user's rectangular crop faithfully; a circle would re-crop it |
| `--bar-height`    | `4px`, `2px` radius          | level widget, 5 steps, track `--color-track`                             |
| `--dot-size`      | `7px`, `4px` gap             | level widget, 5 dots                                                     |
| `--tag-padding`   | `0.15em 0.5em`, `3px` radius | level widget, `tag` style                                                |
| `--icon-size`     | `1em`                        | lucide inline SVG, aligned to heading cap height                         |

Rule visibility follows `heading.showRule`: false removes the rule and its
`--rule-gap` together, so no empty band is left behind.

## 7. What a preset may not do

- Introduce a token. `customization` is `additionalProperties: false`.
- Ship CSS, a component, or a class hook. Nothing records which preset produced
  a document's values, so nothing can key styling off it.
- Set `layout.sections`. ADR 0008 computes it.
- Reference a color by hex anywhere in the renderer. Roles only.
- Depend on a color surviving the clamp unchanged. Design against the roles, and
  verify the preset's own palette passes §5 before clamping.
- Depend on its tint actually rendering. `layout.surfaceTarget` degrades to
  `none` whenever the region does not exist (§4.1), so a preset that sets
  `sidebar` must still read correctly at `columns: 1`, where the tint is gone
  and the sidebar sections run full width. A preset whose only distinguishing
  feature is the band is not a template; it is one visual state of a template.
