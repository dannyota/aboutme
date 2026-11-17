# aboutme typography and tokens

Chrome type, color, shape, and elevation tokens, buttons, and dialogs. Part of
the [visual design](../../../DESIGN.md), in the identity of
[ADR 0020](../../adr/0020-application-visual-identity.md). The
[home page spec](home-page.md#scope) lists which surfaces have taken it.

## Rules

The main color acts, ink selects, mist locates, the red publishes.

- The main color, chàm, fills primary actions and the on state of switches and
  checkboxes, and grounds the home hero. Nothing else.
- A selected option among several (segmented toggles, filter chips, the editor
  rail tool, tabs) is an ink pill: `--foreground` fill, `--background` text.
- The current location (header link, outline row) is an `--accent` pill.
- The lotus red is reserved for the logo and public: the seal ticket, its 20 px
  tile, the word that names the public state, and the Publish button, the only
  red fill, which also carries the ring "a".
- State is a mark, never a color. No state colors exist.

## Color

The tokens are defined on `:root` and switched by `html[data-theme="dark"]`. The
theme preference is persisted in the `aboutme-theme` cookie. The dark theme is
dim: low-chroma grounds, body text near 12:1, no pure white except labels on the
two fills.

| Token                                                                    | Light                                        | Dark                                            | Role                                                           |
| ------------------------------------------------------------------------ | -------------------------------------------- | ----------------------------------------------- | -------------------------------------------------------------- |
| `--background`                                                           | `#FFFFFF`                                    | `#0E0A0D`                                       | page, header, editor panels, inputs                            |
| `--foreground`, and the card, popover, secondary, and accent foregrounds | `#1A1650`                                    | `#D3CAD0`                                       | text, outline pills, wordmark                                  |
| `--muted-foreground`                                                     | `#55517D`                                    | `#A3959F`                                       | leads, descriptions, notes, icons                              |
| `--card`, `--popover`                                                    | `#FFFFFF`                                    | `#1B1419`                                       | standalone cards, menus, dialogs, sheets                       |
| `--brand`, `--primary`                                                   | `#594BAD`                                    | `#7267C0`                                       | the main color: primary pills, switch on                       |
| `--brand-hover`, `--primary-hover`                                       | `#4C3C9C`                                    | `#675BB3`                                       | hover fill                                                     |
| `--brand-foreground`, `--primary-foreground`                             | `#FFFFFF`                                    | `#FFFFFF`                                       | text on the main fill                                          |
| `--brand-text`                                                           | `#4F3DA5`                                    | `#AEA7ED`                                       | main-color words and thin strokes                              |
| `--brand-panel`                                                          | `#594BAD`                                    | `#382961`                                       | home hero ground                                               |
| `--seal`                                                                 | `#DE1B5A`                                    | `#C42D58`                                       | the Publish fill, the public tile                              |
| `--seal-hover`                                                           | `#C7134D`                                    | `#B42650`                                       | Publish hover                                                  |
| `--seal-foreground`                                                      | `#FFFFFF`                                    | `#FFFFFF`                                       | text on the seal fill                                          |
| `--seal-text`                                                            | `#C7134D`                                    | `#E2788F`                                       | seal words and strokes                                         |
| `--link`                                                                 | `#4336C9`                                    | `#B59BDB`                                       | links                                                          |
| `--destructive`                                                          | `#914D00`                                    | `#F0AB72`                                       | destructive outline and text, error text                       |
| `--surface-destructive`                                                  | 7% destructive in card                       | 8% destructive in card                          | error banners                                                  |
| `--surface-mist`, `--muted`, `--editor-canvas`                           | `#F1EFFA`                                    | `#151015`                                       | tinted panels, tracks, the editor canvas                       |
| `--surface-blush`                                                        | `#ECE2FF`                                    | `#1C172F`                                       | the Library header, the maker panel (a tint of the main color) |
| `--surface-sun`                                                          | `#FFF2C2`                                    | `#271A07`                                       | data card                                                      |
| `--accent`, `--secondary`                                                | `#F1EFFA`                                    | `#251B23`                                       | hover fill, current location, secondary button                 |
| `--panel`, `--panel-foreground`, `--panel-muted`                         | `#1A1650`, `#FFFFFF`, `#D9D6F2`              | `#25153A`, `#E7DFE8`, `#C2B6CB`                 | inverse panels; hero text                                      |
| `--footer`                                                               | `#1A1650`                                    | `#150C1C`                                       | footer band; text as on `--panel`                              |
| `--border`                                                               | `#E3E0F2`                                    | `#2E242C`                                       | decorative borders                                             |
| `--hairline`, `--hairline-inverse`                                       | `rgba(26,22,80,.14)`, `rgba(255,255,255,.2)` | `rgba(211,202,208,.14)`, `rgba(255,255,255,.2)` | row separators, popover borders                                |
| `--input`, `--control`                                                   | `#7F7AAE`                                    | `#7E6D7A`                                       | control edges, switch off, dashed outlines                     |
| `--ring`, `--ring-inverse`                                               | `#1A1650`, `#FFFFFF`                         | `#CDBCD9`, `#FFFFFF`                            | focus ring; inverse on the fills, panel, hero, and footer      |
| `--coin`, `--coin-edge`, `--coin-ink`                                    | `#FFD23F`, `#E9B416`, `#1A1650`              | same                                            | coins, bullets, text selection, hero pill label                |
| `--chart-bar`                                                            | `#594BAD`                                    | `#8F85D7`                                       | views chart bars                                               |
| `--overlay`                                                              | `rgba(26,22,80,.55)`                         | `rgba(4,2,3,.72)`                               | dialog scrim                                                   |
| `--logo-mark`, `--logo-mark-inverse`                                     | `#DE1B5A`, `#FF7AA2`                         | `#E2788F`, `#E2788F`                            | logo mark and dot; inverse on indigo grounds                   |

The application sets `<meta name="theme-color">` to the page ground, `#FFFFFF`
light and `#0E0A0D` dark. With no stored preference it emits one tag per
`prefers-color-scheme`; with a stored theme it emits that theme's value, and the
theme button updates it. The public resume page emits none.

The identity has no gradient, no glow, and no blue, jade, or ochre brand color.
The retired tokens no longer exist in `theme.css`; a test fails if any consumer
names one. Each took a replacement: `--brand-blue` becomes `--brand` and
`--brand-deep-blue` becomes `--link`; `--brand-indigo`, `--brand-jade`, and
`--brand-ochre` icons take `--muted-foreground` or sit in a coin;
`--surface-blue` and `--surface-indigo` become `--surface-mist` and
`--surface-sand` becomes `--surface-sun`; `--radius-feature` becomes
`--radius-card`, or `--radius-panel` for a page-level band; `--shadow-product`,
`--shadow-primary`, and the gradients have no replacement.

### Contrast

WCAG 2 ratios, light then dark. Text needs 4.5; control edges, rings, and the
two fills against their ground need 3. `theme.test.ts` checks every pair.

| Ground              | Foreground   | Muted      | Link       | Brand text | Seal text  | Destructive | Control    | Main fill  | Seal fill  |
| ------------------- | ------------ | ---------- | ---------- | ---------- | ---------- | ----------- | ---------- | ---------- | ---------- |
| background          | 16.48, 12.29 | 7.34, 6.89 | 8.05, 8.12 | 8.26, 8.94 | 5.79, 6.85 | 6.43, 10.09 | 3.96, 4.08 | 6.92, 4.14 | 4.76, 3.62 |
| card, popover       | 16.48, 11.31 | 7.34, 6.34 | 8.05, 7.47 | 8.26, 8.23 | 5.79, 6.30 | 6.43, 9.28  | 3.96, 3.75 | 6.92, 3.81 | 4.76, 3.33 |
| mist, editor canvas | 14.49, 11.76 | 6.46, 6.59 | 7.08, 7.77 | 7.26, 8.55 | 5.09, 6.55 | 5.66, 9.65  | 3.49, 3.90 | 6.09, 3.95 | 4.19, 3.46 |
| blush               | 13.25, 10.82 | 5.91, 6.07 | 6.47, 7.15 | 6.64, 7.87 | 4.65, 6.03 | 5.17, 8.88  | 3.19, 3.59 | 5.57, 3.64 | 3.83, 3.19 |
| sun                 | 14.70, 10.61 | 6.55, 5.95 | 7.18, 7.01 | 7.37, 7.72 | 5.16, 5.91 | 5.74, 8.71  | 3.54, 3.52 | 6.17, 3.57 | 4.25, 3.12 |
| accent, secondary   | 14.49, 10.42 | 6.46, 5.84 | 7.08, 6.89 | 7.26, 7.58 | 5.09, 5.81 | 5.66, 8.55  | 3.49, 3.46 | 6.09, 3.51 | 4.19, 3.07 |
| surface-destructive | 14.91, 9.84  | 6.64, 5.51 | 7.28, 6.50 | 7.47, 7.16 | 5.24, 5.48 | 5.82, 8.07  | 3.59, 3.26 | 6.26, 3.31 | 4.31, 2.89 |

- White on `--brand` is 6.92 light and 4.75 dark; on `--brand-hover` 8.63 and
  5.64; on `--seal` 4.76 and 5.44; on `--seal-hover` 5.79 and 6.30.
- Hero: `--panel-foreground` on `--brand-panel` is 6.92 light and 9.72 dark,
  `--panel-muted` 4.89 and 6.54; the hero pill label is 16.48 and 12.65.
- `--panel` text is 16.48 and 12.93, its muted text 11.65 and 8.70; `--footer`
  text 16.48 and 14.64.
- The main fill is 2.38 on light `--panel` and `--footer`, so in light theme a
  control there is a `--panel-foreground` pill. In dark theme the main fill
  clears 3:1 on every ground, the panel, and the footer.
- The seal fill is 2.89 on the dark error banner; no Publish control or public
  tile sits there.
- `--ring` on background, card, and mist is 16.48, 16.48, 14.49 light and 11.04,
  10.16, 10.56 dark. `--chart-bar` on card is 6.92 and 5.60.
- Never set white text on `--coin`.

### Links

`--link` is ink made lighter, close to `--brand-text`. A link is marked by its
underline or weight, never by hue alone.

- A link inside a sentence is always underlined (1 px, offset 4 px), because
  `--link` differs from body text by only 2.05:1 light and 1.51:1 dark.
- A link alone on its line or in a navigation row is weight 600 and underlines
  on hover and focus.
- On `--brand-panel`, `--panel`, and `--footer` a link takes the surface's text
  color with an underline.

### Destructive, main, and seal

Destructive is a browner amber, separated from both the seal red and the main
color by CIEDE2000 10.7 or more under typical, protan, deutan, and tritan
simulation, and the main color is 23.5 or more from the seal. Shape and label
still carry the difference:

- Destructive is never a fill. It is a 1.5 px outline pill with `--destructive`
  text and a 10 percent tint on hover; a main action is a filled pill with white
  text; Publish is the one red fill with the ring "a".
- The label names the act. Error text keeps its alert icon or a word that says
  it failed.
- No destructive text sits inside or against a seal ticket.

### Paper

Chrome that stands for a resume sheet uses the paper tokens, defined once on
`:root` and never switched by the dark theme: `--paper` `#FFFFFF`, `--paper-ink`
`#171A18`, `--paper-muted` `#5F6763`, and `--paper-hover` `#F0F2F1`. The
`.paper-surface` class paints the paper ground and ink and rebinds foreground,
muted foreground, accent, ring (`#1A1650`), link (`#4336C9`), `--brand-text`
(`#4F3DA5`), and `--seal-text` (`#C7134D`) to their light values, so controls
inside stay legible on white paper in dark theme. A ticket on a sheet uses
`--paper-seal-ground` `#FFFFFF` and `--paper-seal-ink` `#C7134D`. None of this
is applied inside the renderer.

## Type

Chrome uses Plus Jakarta Sans, then Be Vietnam Pro, `system-ui`, and
`sans-serif` (`--font-chrome`, `--font-sans`). The seal and the resume use Be
Vietnam Pro (`--font-paper`); the renderer keeps its own typography and tokens.
Uppercase text appears only inside the seal.

| Token                      | Size / line height | Weight and use                                                   |
| -------------------------- | ------------------ | ---------------------------------------------------------------- |
| `--text-xs`                | 12 / 1.5           | 500 captions; 600 tags                                           |
| `--text-sm`                | 14 / 1.5           | 500 small text; 600 labels and navigation in panels; 700 buttons |
| `--text-base`              | 15 / 1.5           | 500 chrome body and inputs; 600 header navigation                |
| `--text-md`                | 16 / 1.6           | 500 leads, legal and guide body; 700 card titles                 |
| `--text-lg`                | 20 / 1.4           | 700 dialog, panel, and section titles                            |
| `--text-xl`                | 24 / 1.35          | 800 section headings, tracking -0.01em                           |
| `--text-2xl`               | 32 / 1.3           | 800 page headings, -0.02em                                       |
| `--text-3xl`               | 40 / 1.3           | 800 Library and showcase headings from 48rem, -0.02em            |
| `--text-4xl`, `--text-6xl` | 48 / 1.3, 60 / 1.3 | the home page only                                               |

Stacked Vietnamese marks are taller in Plus Jakarta Sans than in Be Vietnam Pro.
At weight 800 the tallest capital (Ẳ) rises 1.294 em above the baseline, so
every heading takes line height 1.3 or more. Body sizes at 1.5 clear every pair
up to weight 700. Line height cannot stop clipping under `overflow: hidden`, so
every truncated or clamped chrome text takes `padding-block-start: 0.3em` with
`margin-block-start: -0.3em`.

## Shape and elevation

- Pills (999 px): every button, icon buttons, the locale toggle, the PDF and Web
  toggle, tab lists and triggers, filter chips, tags and badges, header links,
  outline rows, switches, and the zoom control.
- 10 px (`--radius`): inputs, selects, textareas, banners, inspector entry
  cards, and the review checklist. Search boxes are inputs, not pills.
  `rounded-md` and `rounded-lg` both resolve to it.
- 14 px (`--radius-dialog`): dialogs and menus. 8 px: tooltips and menu items. 2
  px (`--radius-sheet`): every sheet.
- `--radius-card` (22 px, 26 from 48rem, 28 from 64rem): standalone cards on the
  page ground. `--radius-panel` (28, 36, 44): page-level tinted bands.
- Surfaces are flat. `--shadow-float` (`0 16px 24px -14px rgba(26,22,80,.55)`,
  dark `rgba(0,0,0,.7)`) lifts menus, floating controls, and sticky bars.
  `--shadow-dialog`
  (`0 32px 64px -24px rgba(26,22,80,.5), 0 2px 8px rgba(26,22,80,.12)`, dark
  `rgba(0,0,0,.8)` and `rgba(0,0,0,.4)`) lifts dialogs. `--shadow-card` lifts
  cards inside panels. Sheets use `--shadow-paper`, a neutral shadow; the home
  hero sheet uses the heavier `--shadow-paper-hero`. Cards and buttons carry no
  shadow.
- Chrome spacing follows the 8 px module: `--space-module` (8 px),
  `--space-dialog-title` (6 px), `--space-field` (16 px), `--space-dialog` (24
  px), `--space-section` (80 px), and `--space-section-lg` (112 px).

## Controls

- Buttons use the button primitive's variants, 36, 40, or 48 px high,
  weight 700. `default` fills with `--primary` and darkens to `--primary-hover`.
  `seal` fills with `--seal`, darkens to `--seal-hover`, and carries the ring
  "a" before the label, for Publish. `outline` is a 1.5 px `--foreground` border
  with an `--accent` hover. `secondary` fills with `--secondary`; on
  `--surface-mist` in light theme, where the fills match, the outline variant
  takes its place. `ghost` has an `--accent` hover. `link` and `destructive`
  follow the rules above. `default`, `link`, `seal`, and `destructive` are the
  guarded edits of [ADR 0019](../../adr/0019-application-ui-toolkit.md).
- Inputs, selects, and textareas: `--background` fill, 1 px `--input` border, 10
  px radius, 40 px high and 44 under `pointer: coarse`, a 2 px `--ring` on
  focus. An invalid field takes a `--destructive` border and its message.
- Text selection in chrome is `--coin` with `--coin-ink` (11.41:1), never inside
  the resume document.
- Scrollbars take `--control` on a transparent track, thin in editor panels
  only. Skeletons fill with `--accent`. Tooltips are `--foreground` with
  `--background` text.
- Menus are `--popover` with a `--hairline` border, 14 px radius, and
  `--shadow-float`; an item is 8 px with an `--accent` focus fill.
- Dialogs and sheets are `--popover` with a `--hairline` border,
  `--shadow-dialog`, and the `--overlay` scrim. A bottom sheet rounds its top
  corners with `--radius-card`.

Dialogs share one rhythm: 24 px between the header, the body, and the actions; 6
px from title to description; and 16 px between fields, with hints 6 px under
their control. Inputs and selects fill the dialog width. Actions sit
right-aligned from 640 px up and stack full-width below it, primary first.
