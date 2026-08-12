# Geometry and preset boundaries

Status: **Draft v2** (2026-08-12). Not approved.

Defines spacing, fixed renderer geometry, link treatment, and the limits on what
a preset may change.

## 6. Spacing, rules, and geometry

Spacing derived from `customization.spacing` — pure multiples, so a user value
of `0` really is `0`:

| Property          | Value                                  | Applies to                              |
| ----------------- | -------------------------------------- | --------------------------------------- |
| `--gap-section`   | `spacing.sectionGap`                   | between sections; resume header to body |
| `--gap-entry`     | `spacing.entryGap`                     | between entries in a section            |
| `--gap-heading`   | `0.4 × sectionGap`                     | heading to first entry                  |
| `--gap-block`     | `0.4 × entryGap`                       | between slots inside one entry          |
| `--gap-inline`    | `0.5em` (renderer)                     | between meta items on the same line     |
| `--page-margin-x` | `spacing.pageMargin.x` mm, else `15mm` | left and right page margin              |
| `--page-margin-y` | `spacing.pageMargin.y` mm, else `15mm` | top and bottom page margin              |

The two page-margin properties are the one place a token reaches `@page`
geometry: `useResumeStyles` emits
`margin: var(--page-margin-y) var(--page-margin-x)` into the `@page` rule
(`print.md` §2), and the editor preview and public page apply the same values as
ordinary padding on the resume root. When `spacing.pageMargin` is absent both
fall back to `15mm`, which is what `print.md` §2 fixed before the token existed,
so an untouched document's geometry is unchanged. The fallback is applied here,
at the point of use; it is never written into `customization`. The
[resume aggregate](../data.md#resume-aggregate) preserves absence as "never
entered." Margins are the primary lever for fitting a resume onto one page, so
this is the first token a page-count problem should reach for, ahead of
`baseSizePx`.

Renderer-fixed geometry and inline-link styling. None of it varies by template
in v1; changing a value here changes every template at once.

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
| `text-decoration` | `underline` on inline links  | a link stays identifiable without color — see below                      |

Rule visibility follows `heading.showRule`: false removes the rule and its
`--rule-gap` together, so no empty band is left behind.

**Inline links are underlined in every template.** `text-decoration: underline`
is renderer-fixed on every inline link the renderer emits: the header's contact
anchors (`contract.md` §5.1), the entry link slots (`employerLink`,
`schoolLink`, `titleLink`, project `link` — `contract.md` §5.2), and anchors
inside sanitized rich text. It is not a token, no preset can remove it, and it
applies identically in preview, SSR, and print. `--color-link` stays the link's
color role and nothing else.

This resolves the link constraint in `limitations.md` §9.8. With the underline
present, a link no longer needs a 3:1 color separation from body text to satisfy
WCAG G183, so a preset may target AAA text contrast and still distinguish its
links, and a monochrome print keeps them identifiable. The cost is that every
template now looks slightly more "web": accepted deliberately, because the
alternative was a constraint no preset could satisfy.

## 7. What a preset may not do

- Introduce a token. `customization` is `additionalProperties: false`.
- Remove the inline-link underline. It is renderer-fixed (§6).
- Ship CSS, a component, or a class hook. Nothing records which preset produced
  a document's values, so nothing can key styling off it.
- Set `layout.sections`. ADR 0008 computes it.
- Reference a color by hex anywhere in the renderer. Roles only.
- Depend on a color surviving the clamp unchanged. Design against the roles, and
  verify the preset's own palette passes `colors.md` §5 before clamping.
- Depend on its tint actually rendering. `layout.surfaceTarget` degrades to
  `none` whenever the region does not exist (`colors.md` §4.1), so a preset that
  sets `sidebar` must still read correctly at `columns: 1`, where the tint is
  gone and the sidebar sections run full width. A preset whose only
  distinguishing feature is the band is not a template; it is one visual state
  of a template.
