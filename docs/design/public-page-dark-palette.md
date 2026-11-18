# Public page dark palette rule

This page holds the dark palette rule of the
[public page bar and color scheme](public-page-theme.md) design, which owns the
owner decisions, the color scheme setting, and the display checks.

One pure function maps the resume's five authored colors to five dark source
colors. The unchanged role derivation of
[Color roles](templates/colors.md#4-color-roles) then runs on those sources,
once per surface, exactly as for light. No template has hand-picked dark colors.

All steps use OKLCH. A color outside sRGB is clipped by lowering its chroma in
0.002 steps at the same lightness and hue. Outputs are `#rrggbb`.

1. **Already dark.** If `colors.background` has lightness below 0.5, the dark
   scheme equals the light one and the steps below do not run.
2. **Tone.** The tone is the hue and chroma of `colors.background` when its
   chroma is at least 0.01 (a tinted paper), otherwise of `colors.primary`.
3. **Ground.** The dark `background` is
   `oklch(0.20, min(0.25 × toneC, 0.015), toneH)`: a near-black carrying a trace
   of the template's own hue. On the screen sheets of the
   [web layout](public-web-layout.md#paper-and-desk) the charcoal paper
   `#2a2723` replaces it.
4. **Text.** The dark `text` is `oklch(0.90, min(textC, 0.015), textH)`.
5. **Primary and accent.** Each keeps its hue and chroma and takes lightness
   `min(max(L, 1.10 − L), 0.94)`. A dark navy becomes a light steel blue; a
   mid-tone orange keeps its lightness and the clamp lifts it.
6. **Tinted surface.** A light `colors.surface` (lightness 0.5 or more) becomes
   `oklch(0.27, min(2 × C, 0.035), H)`, one step above the ground; on the screen
   sheets, `oklch(paper L + 0.07, min(2 × C, 0.035), H)`. A dark surface keeps
   its hue and chroma and takes lightness `max(L, 0.30)`, on the screen sheets
   `max(L, paper L + 0.07)`, so an authored dark band stays a band above the
   ground.

The role derivation then applies its floors against the dark surfaces: 4.5:1 for
body, headings, meta, and links; 3:1 for the name and for level fills and tag
fills. In the dark scheme every clamp is the hue-preserving search; the light
scheme's shortcut that sends failing text on a dark band straight to white does
not apply, so headings keep their hue.

Other rules:

- Photos, and any image, render unchanged: no filter, no dimming, no border.
- Contact icons and brand marks take the dark `--color-meta`, as in light.
- The link underline stays, as every inline link's does.
- `customization.colors` is never rewritten. The dark sources are derived
  values, like every other role.

## How it reaches the page

- `RenderContext` gains an optional `colorScheme` of `dark` or `system`. Only
  the public page and the editor's Web preview pass it; paged mode ignores it.
  `resolveRenderModel` then computes the dark role set and writes each dark role
  next to its light role at every scope that carries roles (the article, a
  tinted header band, a tinted sidebar), as `--dark-color-<role>`.
- `.public-resume-page` and the sheets article (on the public page and in the
  editor's Web preview) carry `data-color-scheme="dark"` or `"system"`; light
  carries no attribute.
- A screen-only rule in `resumeColorScheme.css`, which `ResumeDocument.vue`
  imports, points each `--color-<role>` at its `--dark-color-<role>` under
  `dark`, and under `system` inside `@media (prefers-color-scheme: dark)`. On
  the public page the rule also sets `color-scheme: dark` and
  `background: Canvas` on the root through
  `:root:has(.public-resume-page[data-color-scheme])`, so scrollbars and
  overscroll turn dark. A root rule cannot read the page element's inline
  colors, so the root takes the browser's dark Canvas, not the template's
  ground. The page fills the viewport with its own ground (`min-height: 100vh`),
  so Canvas shows only in overscroll. The rule needs no script, so the first
  paint is already correct. On the screen sheets of the
  [web layout](public-web-layout.md#paper-and-desk) the root takes the fixed
  desk color instead, which a root rule can set.
- `light-dark()` is not used: Safari before 17.5 drops it, which would leave
  text black on the dark ground.
- The page adds no `theme-color` meta; the HTML validator rejects it.

## Template mapping

The rule gives these dark values for each preset's own colors on the charcoal
paper of the screen sheets of the
[web layout](public-web-layout.md#paper-and-desk), which every dark public
surface uses. The unit test and the sheets goldens pin them, and a user's own
colors go through the same rule. The near-black ground of step 3 remains only
for continuous mode, which no public screen uses, and the unit test keeps its
own pins.

| Template           | Ground    | Body      | Heading   | Link      | Tinted region     |
| ------------------ | --------- | --------- | --------- | --------- | ----------------- |
| academic-dense     | `#2a2723` | `#d9dfe4` | `#aec9e6` | `#aec9e6` | none              |
| ats-plain          | `#2a2723` | `#dedede` | `#ebebeb` | `#ebebeb` | none              |
| classic-serif      | `#2a2723` | `#dadee6` | `#dbe0e7` | `#92b0e5` | none              |
| consulting-formal  | `#2a2723` | `#d8dfe5` | `#9fbfe6` | `#6a9dd5` | none              |
| creative-accent    | `#2a2723` | `#e4dcd8` | `#ec623f` | `#ec623f` | `#49332b` header  |
| designer-tag       | `#2a2723` | `#e0dddb` | `#acba99` | `#809657` | none              |
| editorial-wide     | `#2a2723` | `#e1ddd7` | `#cbb9a7` | `#b7835d` | none              |
| elegant-serif-two  | `#2a2723` | `#e3ddd7` | `#db8b99` | `#db8b99` | `#423724` sidebar |
| engineer-compact   | `#2a2723` | `#d8dfe6` | `#92bcd4` | `#5a9db9` | none              |
| executive-band     | `#2a2723` | `#d8dfe8` | `#b4c9e6` | `#b6844a` | `#283a51` header  |
| government-formal  | `#2a2723` | `#dedede` | `#ebebeb` | `#ebebeb` | none              |
| graduate-friendly  | `#2a2723` | `#e3ddd7` | `#c88262` | `#cd7844` | `#453625` header  |
| high-contrast      | `#2a2723` | `#dedede` | `#ebebeb` | `#548aff` | none              |
| international-lang | `#2a2723` | `#e1ddd9` | `#e0dcd9` | `#a79483` | `#3d392f` header  |
| minimal-air        | `#2a2723` | `#dadee5` | `#dbdee4` | `#dbdee4` | none              |
| modern-sidebar     | `#2a2723` | `#d5e0e6` | `#88b7ce` | `#4a99ac` | `#2d3c42` sidebar |
| mono-print         | `#2a2723` | `#dedede` | `#ebebeb` | `#ebebeb` | none              |
| nordic-muted       | `#2a2723` | `#d7dfe8` | `#8fa8bd` | `#7093a9` | `#36393d` sidebar |
| one-page-tight     | `#2a2723` | `#d8dfe6` | `#aec0cf` | `#8ba6b9` | none              |
| startup-bold       | `#2a2723` | `#d8dee8` | `#e8ebf0` | `#e8ebf0` | none              |

Measured on the charcoal paper, across all twenty, on every surface: body at
least 8.5:1, meta 5.5:1, headings, links and accent text 4.5:1, level and tag
fills 3.4:1, a fill on its track 3.0:1, tag labels 5.0:1 and rules 1.5:1. Link
values are those on the paper; a tinted region clamps its own. Sidebar regions
are computed but not drawn, because sheets have no side tint.

No template needs a hand-made exception. Two rule branches cover the special
cases:

- **executive-band** is the one preset with a dark authored band (`#16273d`).
  Step 6 keeps it navy and lifts it to `#283a51`, the paper's lightness plus
  0.07, so the band still reads above the charcoal paper, and its heading turns
  light blue rather than white.
- **Already dark** (step 1) covers a user who picked a dark background in the
  light scheme. No preset does.
