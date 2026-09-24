# Geometry and preset boundaries

Defines spacing, fixed renderer geometry, link treatment, and the limits on what
a preset may change.

## 6. Spacing, rules, and geometry

Spacing derived from `customization.spacing` — pure multiples, so a user value
of `0` really is `0`:

| Property          | Value                                  | Applies to                          |
| ----------------- | -------------------------------------- | ----------------------------------- |
| `--gap-section`   | `spacing.sectionGap`                   | between sections                    |
| `--gap-header`    | `1.5 × sectionGap`                     | resume header to body               |
| `--gap-entry`     | `spacing.entryGap`                     | between entries in a section        |
| `--gap-heading`   | `0.4 × sectionGap`                     | heading to first entry              |
| `--gap-block`     | `0.4 × entryGap`                       | between slots inside one entry      |
| `--gap-inline`    | `0.5em` (renderer)                     | between meta items on the same line |
| `--page-margin-x` | `spacing.pageMargin.x` mm, else `15mm` | left and right page margin          |
| `--page-margin-y` | `spacing.pageMargin.y` mm, else `15mm` | top and bottom page margin          |

The page margins are the one place a token reaches `@page` geometry (`print.md`
§2); the editor preview and public page apply the same values as padding on the
resume root. The `15mm` fallback applies at the point of use and is never
written into `customization`. Margins are the first lever for fitting a resume
onto one page, ahead of `baseSizePx`.

The renderer fixes the geometry below for every template.

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
| `text-decoration` | `underline` on inline links  | a link stays identifiable without color                                  |

Rule visibility follows `heading.showRule`: false removes the rule and its
`--rule-gap` together, so no empty band is left behind.

**Inline links are underlined in every template**: the header's contact anchors
(`contract.md` §5.1), the entry link slots (`contract.md` §5.2), and anchors in
rich text, in preview, SSR, and print alike. No preset can remove it
(`limitations.md` §9.8). `--color-link` is only the link's color.

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
