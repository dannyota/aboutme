# Public page bar and color scheme

This page specifies two changes to the public resume page at `/{slug}`: a
compact page bar in the Aurora identity, and an owner-chosen color scheme that
can show the resume on a dark ground. The PDF, print, and the link-preview card
stay light in every case.

Status: proposed. The owner approved the direction: light by default, the owner
chooses the scheme per resume, and viewers see the owner's choice. The dark
scheme contradicts the white-sheet rule of
[ADR 0050](../adr/0050-aurora-application-identity.md), so it needs an ADR that
amends that rule for the public page on screen before it is built. The page bar
needs no contract change.

## Owner decisions

- Light is the default for every resume, new and existing.
- The owner picks the scheme for each resume in the editor: Light, Dark, or
  Match device. Match device follows each viewer's `prefers-color-scheme`.
- Viewers see what the owner chose. The page has no viewer toggle (see
  [No viewer toggle](#no-viewer-toggle)).
- The PDF, browser print, and the link-preview card are always light.

## Page bar

The bar is aboutme chrome, not part of the resume. It uses Aurora tokens and the
chrome typeface, and it stays quiet next to the resume: a pale strip, muted
credit text, and one small outlined button.

### Structure

The bar is the first child of `.public-resume-page`, above `.public-measure`, so
it spans the full viewport width. An inner row aligns its content with the
resume text.

```html
<div class="public-toolbar">
  <div class="public-toolbar-inner">
    <span class="public-brand">
      <svg class="public-mark" aria-hidden="true" focusable="false">…</svg>
      <a class="public-credit" href="https://aboutme.vn/"
        >Built with aboutme.vn</a
      >
    </span>
    <a class="public-download" href="/api/v1/public/resumes/{slug}/pdf">
      <svg aria-hidden="true" focusable="false">…</svg>
      <span>Download PDF</span>
    </a>
  </div>
</div>
```

- The class names `public-toolbar`, `public-credit`, and `public-download` stay,
  so the server's HTML validator and the existing tests keep their hooks.
- The credit anchor keeps exactly `class` and `href` and one text child, as the
  validator requires. The mark therefore sits beside the anchor, not inside it.
- The mark is the `AppLogo` mark alone (the gradient document with the `a` cut
  out), drawn as plain SVG with per-instance gradient and mask IDs. The public
  page loads no Tailwind, so it takes plain CSS classes.
- The download icon is Lucide `download` at 16 px with `currentColor` stroke.
- The download anchor's accessible name is its visible label.

### Copy

The bar follows the resume language, as today.

| Element  | Vietnamese resume     | Any other language      |
| -------- | --------------------- | ----------------------- |
| Credit   | `Tạo bằng aboutme.vn` | `Built with aboutme.vn` |
| Download | `Tải PDF`             | `Download PDF`          |

### Tokens

The bar takes the Aurora values from [DESIGN.md](../../DESIGN.md). The public
page does not load the application theme, so `print.css` declares these as
custom properties scoped to `.public-toolbar`.

| Role                  | Light                              | Dark                        | Aurora source    |
| --------------------- | ---------------------------------- | --------------------------- | ---------------- |
| Bar ground            | `#F5F8FF`                          | `#071126`                   | Page background  |
| Bar bottom rule       | `#DCE5F5`                          | `rgba(180, 200, 255, 0.16)` | Border           |
| Credit text           | `#56648C`                          | `#9EACCA`                   | Muted foreground |
| Credit hover          | `#123EDB`                          | `#8FB3FF`                   | Link             |
| Button fill           | `#FFFFFF`                          | `#0D1935`                   | Card             |
| Button border         | `#DCE5F5`                          | `rgba(180, 200, 255, 0.24)` | Border           |
| Button label and icon | `#123EDB`                          | `#8FB3FF`                   | Link             |
| Button hover fill     | `#EAF2FF`                          | `#1A2B52`                   | Secondary        |
| Focus ring            | `#1A5CEB`                          | `#72A0FF`                   | Ring             |
| Button shadow         | `0 1px 2px rgba(16, 27, 63, 0.06)` | none                        | Shadow xs        |

Measured contrast: credit text 5.5:1 light and 8.2:1 dark; button label 7.7:1 on
white, 6.8:1 on the hover fill, 8.3:1 dark, and 6.7:1 on the dark hover fill.
The mark keeps its gradient in both schemes.

### Size and placement

| Property               | Value                                                   |
| ---------------------- | ------------------------------------------------------- |
| Bar height             | 48 px, or 56 px on a touch screen; 8 px block padding   |
| Inner row width        | the resume measure plus both page margins, centered     |
| Inner inline padding   | `var(--page-margin-x)`; 16 px below 40 rem (640 px)     |
| Row layout             | brand left, download right; 16 px gap, 12 px on phones  |
| Wrap                   | the row wraps with an 8 px row gap; download goes last  |
| Credit type            | Be Vietnam Pro 500, 13 px on 20 px                      |
| Mark                   | 18 by 20 px, 8 px before the credit text                |
| Button                 | 32 px high, 10 px radius, 1 px border                   |
| Button padding         | 10 px left, 12 px right                                 |
| Button type            | Be Vietnam Pro 500, 14 px on 20 px; icon 16 px, gap 6px |
| Button, coarse pointer | 40 px high, same padding                                |
| Credit hover           | link color and underline with a 3 px offset             |
| Focus                  | 2 px ring, 2 px offset, on the credit and the button    |

The button matches the Aurora `outline` button at size `sm`. It replaces today's
filled button, 2.75em high (about 35 px) in the template's link color.

At 1440 px the brand sits on the resume's left text edge and the button's right
edge on the resume's right text edge. On a phone the bar keeps a 16 px gutter.
Both languages fit one row at 360 px on a touch screen. At 320 px the English
row wraps and the button sits under the credit; the Vietnamese row still fits.

### Behavior

- The bar is not sticky. It scrolls away with the page, so it never covers the
  resume on any screen. A reader sees it first, where the download is most
  useful, and the page needs no script for it.
- The resume keeps its own top padding (`--page-margin-y`) under the bar.
- When the owner turned PDF download off, the bar holds the brand alone on the
  left, at the same height.
- Print hides the bar, as today.
- Under `forced-colors: active`, the mark fills with `CanvasText` and the button
  shows a `ButtonBorder` border.

## Color scheme setting

### Document field

The scheme is a presentation leaf of the resume document:
`customization.colorScheme`, an optional enum of `light`, `dark`, and `system`.
Absent means `light`, so every existing resume stays light without a migration.

- A preset never sets it. Applying a template keeps the owner's value, or its
  absence, as it keeps `font.textAlign` and `header.photoPosition`.
- The column toggle does not touch it.
- It changes no content, order, or visibility.
- Agents can set it through the same customization path as any other leaf.

### Editor placement and copy

The field is the first item of the Design panel's Colors group, above the five
colors. It uses the same select control as the panel's other enum fields.

| Item         | Vietnamese                                                                    | English                                                               |
| ------------ | ----------------------------------------------------------------------------- | --------------------------------------------------------------------- |
| Label        | `Giao diện trang web`                                                         | `Web page theme`                                                      |
| Option light | `Sáng`                                                                        | `Light`                                                               |
| Option dark  | `Tối`                                                                         | `Dark`                                                                |
| Option match | `Theo thiết bị`                                                               | `Match device`                                                        |
| Hint         | `Người xem thấy giao diện này trên trang công khai. PDF và bản in luôn sáng.` | `Readers see this on your public page. The PDF and print stay light.` |

The label says web page because the setting applies only there. Match device
says what readers get in plainer words than System.

### Editor preview

- Web mode shows the chosen scheme: the continuous sheet takes the dark ground
  and roles. The sheet keeps its 2 px radius and paper shadow.
- Match device follows the editing device's `prefers-color-scheme`, which is
  what a reader on the same device sees. The editor's own light or dark theme
  does not drive the preview.
- PDF mode is always light, because it shows the PDF.
- Template thumbnails, the Library, template pages, the homepage sample, and the
  new-resume screens always render light.

### No viewer toggle

The public page has no viewer toggle. Match device already follows each reader's
own setting, and a toggle would override the owner's explicit Light or Dark. A
remembered choice would also need a script to run before first paint, which the
public page's strict script policy forbids, so the page would flash the wrong
scheme on every load.

## Dark palette rule

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

### How it reaches the page

- `resolveRenderModel` computes the dark role set only when the render context
  is the public page or the editor's Web preview and the scheme is `dark` or
  `system`. It writes each dark role next to its light role at every scope that
  carries roles (the article, a tinted header band, a tinted sidebar), as
  `--dark-color-<role>`.
- `.public-resume-page`, and the editor's Web preview sheet, carry
  `data-color-scheme="dark"` or `"system"`; light carries no attribute.
- A screen-only rule in `print.css` points each `--color-<role>` at its
  `--dark-color-<role>` under `dark`, and under `system` inside
  `@media (prefers-color-scheme: dark)`. The rule also sets `color-scheme: dark`
  on the root so scrollbars and overscroll match. It needs no script, so the
  first paint is already correct.
- `light-dark()` is not used: Safari before 17.5 drops it, which would leave
  text black on the dark ground.
- The page adds no `theme-color` meta; the HTML validator rejects it.

### Template mapping

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

## What stays light

| Output                                      | Why it stays light                                          |
| ------------------------------------------- | ----------------------------------------------------------- |
| PDF download and the owner's PDF            | Paged print context never computes the dark set             |
| Browser print of the public page            | The dark rule is screen-only; print uses the light roles    |
| Link-preview card                           | The card renderer ignores `colorScheme`                     |
| Library, template pages, thumbnails, sample | Their render context carries no scheme                      |
| "What an ATS reads" tab                     | A sample's text on a `.paper-surface` panel, unchanged      |
| `/{slug}.md` and the PDF text layer         | Text only; the scheme changes no content, order, or wording |

The ATS text is the same in both schemes: the scheme changes only colors, never
the DOM order, text, or hidden state.

## Baselines and tests

| Artifact                                      | Page bar  | Color scheme                        |
| --------------------------------------------- | --------- | ----------------------------------- |
| `public--classic-serif--2560.png`             | Changes   | Unchanged (light)                   |
| `public--modern-sidebar--2560.png`            | Changes   | Unchanged (light)                   |
| `public--modern-sidebar--390.png`             | Changes   | Unchanged (light)                   |
| New `public--modern-sidebar--dark--390.png`   | n/a       | New                                 |
| New `public--creative-accent--dark--1440.png` | n/a       | New                                 |
| New `public--executive-band--dark--1440.png`  | n/a       | New                                 |
| New `public--classic-serif--system--1440.png` | n/a       | New, with emulated dark preference  |
| Every `*--paged.png` and `*--continuous.png`  | Unchanged | Unchanged                           |
| `print-baselines/`, `chrome--*`, `template-*` | Unchanged | Unchanged                           |
| Golden HTML for every preset                  | Unchanged | Unchanged; no fixture sets a scheme |
| Public render assertions in `render.test.ts`  | Change    | New dark and system cases           |

Tests to add or change:

- The public page geometry test keeps its checks and adds: the bar spans the
  viewport, the download button is at most 32 px high on a fine pointer, and the
  bar is hidden in print.
- A unit test runs the dark rule over all twenty presets and both columns and
  asserts every floor above on every surface.
- A dark public page under print media computes the light surface color.
- The render and hydration tests cover `data-color-scheme` and the bar markup.
