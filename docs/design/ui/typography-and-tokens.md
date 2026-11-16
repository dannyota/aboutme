# aboutme typography and tokens

Chrome type, color, shape, and elevation tokens, buttons, and dialogs. Part of
the [visual design](../../../DESIGN.md), in the identity of
[ADR 0020](../../adr/0020-application-visual-identity.md). The
[home page spec](home-page.md#scope) lists which surfaces have taken it.

## Rules

Pink acts, ink selects, mist locates, the ticket publishes.

- The brand fill marks primary actions and the on state of switches and
  checkboxes. Nothing else.
- A selected option among several (segmented toggles, filter chips, the editor
  rail tool, tabs) is an ink pill: `--foreground` fill, `--background` text.
- The current location (header link, outline row) is an `--accent` pill.
- Public is the seal ticket and its 20 px tile. Only Publish carries the ring
  "a".
- State is a mark, never a color. No state colors exist.

## Color

The tokens are defined on `:root` and switched by `html[data-theme="dark"]`. The
theme preference is persisted in the `aboutme-theme` cookie.

| Token                                                                    | Light                                        | Dark                                            | Role                                            |
| ------------------------------------------------------------------------ | -------------------------------------------- | ----------------------------------------------- | ----------------------------------------------- |
| `--background`                                                           | `#FFFFFF`                                    | `#0F0D26`                                       | page, header, editor panels, inputs             |
| `--foreground`, and the card, popover, secondary, and accent foregrounds | `#1A1650`                                    | `#F3F1FF`                                       | text, outline pills, wordmark                   |
| `--muted-foreground`                                                     | `#55517D`                                    | `#B7B2DB`                                       | leads, descriptions, notes, icons               |
| `--card`, `--popover`                                                    | `#FFFFFF`                                    | `#25225A`                                       | standalone cards, menus, dialogs, sheets        |
| `--brand`, `--primary`, `--seal`                                         | `#DE1B5A`                                    | `#DE1B5A`                                       | primary pills, switch on, seal fills            |
| `--brand-hover`, `--primary-hover`                                       | `#C7134D`                                    | `#C7134D`                                       | hover fill                                      |
| `--brand-foreground`, `--primary-foreground`, `--seal-foreground`        | `#FFFFFF`                                    | `#FFFFFF`                                       | text on the brand fill                          |
| `--brand-text`, `--seal-text`                                            | `#C7134D`                                    | `#FF7AA2`                                       | seal words, pink words, and thin strokes        |
| `--link`                                                                 | `#4336C9`                                    | `#B9B2FF`                                       | links                                           |
| `--destructive`                                                          | `#B54708`                                    | `#FD8A4B`                                       | destructive outline and text, error text        |
| `--surface-destructive`                                                  | 7% destructive in card                       | 12% destructive in card                         | error banners                                   |
| `--surface-mist`, `--muted`, `--editor-canvas`                           | `#F1EFFA`                                    | `#191640`                                       | tinted panels, tracks, the editor canvas        |
| `--surface-blush`                                                        | `#FFE4EC`                                    | `#3B1431`                                       | the Library header, the maker panel             |
| `--surface-sun`                                                          | `#FFF2C2`                                    | `#332A0E`                                       | data card                                       |
| `--accent`, `--secondary`                                                | `#F1EFFA`                                    | `#332F76`                                       | hover fill, current location, secondary button  |
| `--panel`, `--panel-foreground`, `--panel-muted`                         | `#1A1650`, `#FFFFFF`, `#D9D6F2`              | `#322C8F`, `#FFFFFF`, `#D9D6F2`                 | inverse panels                                  |
| `--footer`                                                               | `#1A1650`                                    | `#191640`                                       | footer band; text as on `--panel`               |
| `--border`                                                               | `#E3E0F2`                                    | `#34315C`                                       | decorative borders                              |
| `--hairline`, `--hairline-inverse`                                       | `rgba(26,22,80,.14)`, `rgba(255,255,255,.2)` | `rgba(243,241,255,.16)`, `rgba(255,255,255,.2)` | row separators, popover borders                 |
| `--input`, `--control`                                                   | `#7F7AAE`                                    | `#8580B8`                                       | control edges, switch off, dashed outlines      |
| `--ring`, `--ring-inverse`                                               | `#1A1650`, `#FFFFFF`                         | `#C9C3FF`, `#FFFFFF`                            | focus ring; inverse on brand, panel, and footer |
| `--coin`, `--coin-edge`, `--coin-ink`                                    | `#FFD23F`, `#E9B416`, `#1A1650`              | same                                            | coins, bullets, text selection                  |
| `--chart-bar`                                                            | `#5B52C7`                                    | `#9A93F5`                                       | views chart bars                                |
| `--overlay`                                                              | `rgba(26,22,80,.55)`                         | `rgba(5,4,16,.72)`                              | dialog scrim                                    |
| `--logo-mark`, `--logo-mark-inverse`                                     | `#DE1B5A`, `#FF7AA2`                         | `#FF7AA2`, `#FF7AA2`                            | logo mark and dot; inverse on indigo grounds    |

The application sets `<meta name="theme-color">` to the page ground, `#FFFFFF`
light and `#0F0D26` dark. With no stored preference it emits one tag per
`prefers-color-scheme`; with a stored theme it emits that theme's value, and the
theme button updates it. The public resume page emits none.

The identity has no gradient, no glow, and no blue, jade, or ochre brand color.
A consumer of a removed token takes its replacement: `--brand-blue` becomes
`--brand` and `--brand-deep-blue` becomes `--link`; `--brand-indigo`,
`--brand-jade`, and `--brand-ochre` icons take `--muted-foreground` or sit in a
coin; `--surface-blue` and `--surface-indigo` become `--surface-mist` and
`--surface-sand` becomes `--surface-sun`; `--radius-feature` becomes
`--radius-card`, or `--radius-panel` for a page-level band; `--shadow-product`,
`--shadow-primary`, and the gradients have no replacement.

### Contrast

WCAG 2 ratios, light then dark. Text needs 4.5, marks and control edges 3.
`theme.test.ts` checks every pair.

| Ground              | Foreground   | Muted      | Link       | Brand text | Destructive | Control    |
| ------------------- | ------------ | ---------- | ---------- | ---------- | ----------- | ---------- |
| background          | 16.48, 17.03 | 7.34, 9.41 | 8.05, 9.83 | 5.79, 7.74 | 5.43, 8.06  | 3.96, 5.22 |
| card, popover       | 16.48, 12.95 | 7.34, 7.15 | 8.05, 7.47 | 5.79, 5.88 | 5.43, 6.13  | 3.96, 3.97 |
| mist, editor canvas | 14.49, 15.31 | 6.46, 8.46 | 7.08, 8.84 | 5.09, 6.96 | 4.77, 7.25  | 3.49, 4.70 |
| blush               | 13.78, 14.16 | 6.14, 7.82 | 6.73, 8.17 | 4.84, 6.43 | 4.54, 6.70  | 3.31, 4.34 |
| sun                 | 14.70, 12.74 | 6.55, 7.04 | 7.18, 7.36 | 5.16, 5.79 | 4.84, 6.03  | 3.54, 3.91 |
| accent, secondary   | 14.49, 10.41 | 6.46, 5.75 | 7.08, 6.01 | 5.09, 4.73 | 4.77, 4.93  | 3.49, 3.19 |
| surface-destructive | 14.91, 10.81 | 6.64, 5.97 | 7.28, 6.24 | 5.24, 4.91 | 4.91, 5.11  | 3.59, 3.31 |

- White on `--brand` is 4.76 and on `--brand-hover` 5.79. White and
  `--panel-muted` on `--panel` are 16.48 and 11.65 light, 11.10 and 7.85 dark.
- `--ring` on background, card, and mist is 16.48, 16.48, 14.49 light and 11.51,
  8.75, 10.35 dark. `--chart-bar` on card is 6.04 and 5.41.
- The brand fill against its ground is 4.76 on white, 4.19 on mist, and 3.98 on
  blush; in dark theme 3.99 on background, 3.59 on mist, and 3.03 on card. It
  falls under 3 on dark `--surface-sun`, `--accent`, and `--panel`, so no
  brand-filled control sits on those three grounds in dark theme; an outline or
  a white pill takes its place.
- Never set light `--brand-text` on `--panel` (2.85) and never set white text on
  `--coin`.

### Links

`--link` is ink made lighter. Pink is never a link color: for tritan and deutan
readers pink text sits too close to error text.

- A link inside a sentence is always underlined (1 px, offset 4 px), because
  `--link` differs from body text by only 2.05:1 light and 1.73:1 dark.
- A link alone on its line or in a navigation row is weight 600 and underlines
  on hover and focus.
- On `--brand`, `--panel`, and `--footer` a link takes the surface's text color
  with an underline.

### Destructive beside the brand fill

The two hues are 25.6 apart in CIEDE2000 for typical vision but 11.8 for deutan
and 8.0 for tritan readers, and their lightness is nearly equal. So shape and
label carry the difference:

- Destructive is never a fill. It is a 1.5 px outline pill with `--destructive`
  text and a 10 percent tint on hover; a brand action is a filled pill with
  white text.
- The label names the act. Error text keeps its alert icon or a word that says
  it failed.
- No destructive text sits inside or against a seal ticket.

### Paper

Chrome that stands for a resume sheet uses the paper tokens, defined once on
`:root` and never switched by the dark theme: `--paper` `#FFFFFF`, `--paper-ink`
`#171A18`, `--paper-muted` `#5F6763`, and `--paper-hover` `#F0F2F1`. The
`.paper-surface` class paints the paper ground and ink and rebinds foreground,
muted foreground, accent, ring (`#1A1650`), and link (`#4336C9`) to their light
values, so controls inside stay legible on white paper in dark theme. A ticket
on a sheet uses `--paper-seal-ground` `#FFFFFF` and `--paper-seal-ink`
`#C7134D`. None of this is applied inside the renderer.

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
  `seal` is the same fill with the ring "a" before the label, for Publish.
  `outline` is a 1.5 px `--foreground` border with an `--accent` hover.
  `secondary` fills with `--secondary`; on `--surface-mist` in light theme,
  where the fills match, the outline variant takes its place. `ghost` has an
  `--accent` hover. `link` and `destructive` follow the rules above. `default`,
  `link`, `seal`, and `destructive` are the guarded edits of
  [ADR 0019](../../adr/0019-application-ui-toolkit.md).
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
