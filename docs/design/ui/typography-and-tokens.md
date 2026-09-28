# aboutme typography and tokens

Chrome type, semantic and brand tokens, spacing, radius, buttons, and dialogs.
Part of the [visual design](../../../DESIGN.md).

## Typography and tokens

Application chrome uses `Be Vietnam Pro`, then `Inter`, `system-ui`, and
`sans-serif`. Uppercase text is used inside the seal. The chrome body is
`text-base`, 15 px, and controls and labels are `text-sm`, 14 px. The scale runs
12, 14, 15, 16, 20, 24, 32, 40, 48, and 60 px. Headings take line height 1.3 (20
to 32 px), 1.25 (40 and 48 px), and 1.2 (60 px), so stacked Vietnamese
diacritics such as ệ, ở, and Ử clear the line above. The renderer keeps its own
typography and tokens.

The semantic tokens below are defined on `:root` and switched by
`html[data-theme="dark"]`. The palette is keyed to the logo's two colors: navy
ink and seal red on warm paper. The dark theme is ink black with warm white
text.

| Role                              | Light     | Dark                        |
| --------------------------------- | --------- | --------------------------- |
| Page background                   | `#F9F8F5` | `#0C1020`                   |
| Foreground ink                    | `#101B3F` | `#F3F1EC`                   |
| Card                              | `#FFFFFF` | `#141A2E`                   |
| Popover                           | `#FFFFFF` | `#1A2138`                   |
| Primary action and focus ring     | `#26409C` | `#8FA6F0`                   |
| Primary hover                     | `#1E3483` | `#A3B6F5`                   |
| Primary foreground                | `#FFFFFF` | `#0C1020`                   |
| Secondary and accent              | `#ECEEF6` | `#1A2138`, `#212A45`        |
| Muted                             | `#EFEDE6` | `#1A2138`                   |
| Secondary/muted/accent foreground | `#101B3F` | `#F3F1EC`                   |
| Muted foreground                  | `#5C6178` | `#A5ABBF`                   |
| Border                            | `#E5E1D6` | `rgba(230, 225, 210, 0.13)` |
| Input border                      | `#7D8398` | `#6A7390`                   |
| Link                              | `#23399A` | `#A3B6F5`                   |
| Seal                              | `#CC2649` | `#CC2649`                   |
| Editor canvas                     | `#F1EFE9` | `#0C1020`                   |
| Seal text and strokes             | `#CC2649` | `#FF6B8A`                   |
| Destructive                       | `#B54708` | `#FD8A4B`                   |

Brand and surface tokens have Tailwind color utilities such as `bg-surface-blue`
and `text-brand-indigo`:

| Token                                | Light                           | Dark                 |
| ------------------------------------ | ------------------------------- | -------------------- |
| `--brand-blue`, `--brand-deep-blue`  | `var(--primary)`, `var(--link)` | same aliases         |
| `--brand-indigo`, `--brand-jade`     | `#4A4DBF`, `#0F7C6E`            | `#9A9CFF`, `#5FCFBB` |
| `--brand-ochre`                      | `#A86D12`                       | `#E2B45C`            |
| `--surface-blue`                     | `#ECEEF7`                       | `#151D36`            |
| `--surface-indigo`, `--surface-sand` | `#EFEEF8`, `#F6EEDF`            | `#1A1B3D`, `#231D16` |
| `--surface-destructive`              | 7% destructive, card            | 12% destructive      |

Jade and ochre are the seal's partners: neither sits near red, so seal red stays
the only warm, saturated color in the chrome.

Text on the canvas, a card, or a tinted surface meets WCAG AA: 4.5:1 for normal
text and 3:1 for large text, input borders, and focus rings, measured over the
brightest canvas glow. Blue text uses `--link`, not `--primary` or the brand
colors. Brand colors are for fills, icons, and large text, and each meets 3:1 on
every ground. `theme.test.ts` checks these ratios for every token pair.

`--gradient-brand` runs from brand blue to brand indigo; it colors at most one
key phrase or hero action on a page. The logo no longer uses it.
`--gradient-aurora` holds the canvas glows: ink blue and indigo at the top
corners, jade and ochre further down, at 5 to 8 percent opacity in light theme
and 7 to 18 percent in dark theme.

Chrome spacing follows the 8 px module, and its named rhythms are custom
properties: `--space-module` (8 px), `--space-dialog-title` (6 px),
`--space-field` (16 px), `--space-dialog` (24 px), `--space-section` (80 px),
and `--space-section-lg` (112 px).

The standard radius is 10 px, dialogs use 14 px, feature and marketing cards use
`--radius-feature` (20 px), and the sheet stays at 2 px. `rounded-md` and
`rounded-lg` both resolve to the 10 px `--radius`. Product surfaces use
`--shadow-product`, a soft ink-tinted shadow. The sheet uses `--shadow-paper`, a
neutral shadow with no blue. The theme preference is persisted in the
`aboutme-theme` cookie.

Chrome that stands for a resume sheet uses the paper tokens, defined once on
`:root` and never switched by the dark theme: `--paper` `#FFFFFF`, `--paper-ink`
`#171A18`, `--paper-muted` `#5F6763`, and `--paper-hover` `#F0F2F1`. The
`.paper-surface` class paints the paper ground and ink and rebinds foreground,
muted foreground, accent, ring (`#26409C`), and link (`#23399A`) to their light
values, so shadcn controls inside stay legible on white paper in dark theme. It
is never applied inside the renderer.

Buttons use the button primitive's variants. `default` fills with `--primary`,
darkens to `--primary-hover` on hover, and carries `--shadow-primary`, a soft
blue lift in light theme and a plain dark shadow in dark theme. `link` is
`--link` text with an underline on hover. `seal` fills with `--seal` for
Publish. `destructive` is a `--destructive` outline with matching text and a 10
percent tint on hover; these four are the guarded exception below. `outline`,
`secondary`, and `ghost` keep the generated classes.

Dialogs share one rhythm: 24 px between the header, the body, and the actions; 6
px from title to description; and 16 px between fields, with hints 6 px under
their control. Inputs and selects fill the dialog width. Actions sit
right-aligned from 640 px up and stack full-width below it, primary first.
