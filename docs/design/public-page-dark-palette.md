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
   of the template's own hue.
4. **Text.** The dark `text` is `oklch(0.90, min(textC, 0.015), textH)`.
5. **Primary and accent.** Each keeps its hue and chroma and takes lightness
   `min(max(L, 1.10 − L), 0.94)`. A dark navy becomes a light steel blue; a
   mid-tone orange keeps its lightness and the clamp lifts it.
6. **Tinted surface.** A light `colors.surface` (lightness 0.5 or more) becomes
   `oklch(0.27, min(2 × C, 0.035), H)`, one step above the ground. A dark
   surface keeps its hue and chroma and takes lightness `max(L, 0.30)`, so an
   authored dark band stays a band above the ground.

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
- `.public-resume-page`, and the editor's Web preview sheet, carry
  `data-color-scheme="dark"` or `"system"`; light carries no attribute.
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
  paint is already correct.
- `light-dark()` is not used: Safari before 17.5 drops it, which would leave
  text black on the dark ground.
- The page adds no `theme-color` meta; the HTML validator rejects it.

## Template mapping

The rule gives these dark values for each preset's own colors, computed by a
reference model of the rule. The unit test pins the implementation's values, and
a user's own colors go through the same rule.

| Template           | Ground    | Body      | Heading   | Link      | Tinted region     |
| ------------------ | --------- | --------- | --------- | --------- | ----------------- |
| academic-dense     | `#12171c` | `#d9dfe4` | `#aec9e6` | `#aec9e6` | none              |
| ats-plain          | `#161616` | `#dedede` | `#ebebeb` | `#ebebeb` | none              |
| classic-serif      | `#151617` | `#dadee6` | `#dbe0e7` | `#92b0e5` | none              |
| consulting-formal  | `#11171d` | `#d8dfe5` | `#9fbfe6` | `#6a9dd5` | none              |
| creative-accent    | `#1c1411` | `#e4dcd8` | `#dc5330` | `#dc5330` | `#352019` header  |
| designer-tag       | `#151711` | `#e0dddb` | `#acba99` | `#778d4e` | none              |
| editorial-wide     | `#191512` | `#e1ddd7` | `#cbb9a7` | `#b4805a` | none              |
| elegant-serif-two  | `#1c1315` | `#e3ddd7` | `#db8b99` | `#db8b99` | `#2f2512` sidebar |
| engineer-compact   | `#10171c` | `#d8dfe6` | `#92bcd4` | `#5a9db9` | none              |
| executive-band     | `#13161b` | `#d8dfe8` | `#b4c9e6` | `#a6753b` | `#1d2f45` header  |
| government-formal  | `#161616` | `#dedede` | `#ebebeb` | `#ebebeb` | none              |
| graduate-friendly  | `#1c1410` | `#e3ddd7` | `#c88262` | `#bc6934` | `#322313` header  |
| high-contrast      | `#161616` | `#dedede` | `#ebebeb` | `#5085ff` | none              |
| international-lang | `#171615` | `#e1ddd9` | `#e0dcd9` | `#a79483` | `#2a261d` header  |
| minimal-air        | `#151617` | `#dadee5` | `#dbdee4` | `#dbdee4` | none              |
| modern-sidebar     | `#0f171b` | `#d5e0e6` | `#88b7ce` | `#4090a3` | `#1b292f` sidebar |
| mono-print         | `#161616` | `#dedede` | `#ebebeb` | `#ebebeb` | none              |
| nordic-muted       | `#12171a` | `#d7dfe8` | `#8fa8bd` | `#63859b` | `#24272a` sidebar |
| one-page-tight     | `#131619` | `#d8dfe6` | `#aec0cf` | `#8ba6b9` | none              |
| startup-bold       | `#151617` | `#d8dee8` | `#e8ebf0` | `#e8ebf0` | none              |

Across all twenty, on every surface: body at least 10.1:1, meta 6.4:1, headings
4.5:1, links 4.5:1, and level and tag fills 3.5:1. Link values are those on the
ground; a tinted region clamps its own.

No template needs a hand-made exception. Two rule branches cover the special
cases:

- **executive-band** is the one preset with a dark authored band (`#16273d`).
  Step 6 keeps it navy and lifts it to `#1d2f45`, so the band still reads above
  the near-black ground, and its heading turns light blue rather than white.
- **Already dark** (step 1) covers a user who picked a dark background in the
  light scheme. No preset does.
