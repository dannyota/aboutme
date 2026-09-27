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
`html[data-theme="dark"]`. The dark theme is midnight blue.

| Role                              | Light     | Dark                        |
| --------------------------------- | --------- | --------------------------- |
| Page background                   | `#F5F8FF` | `#071126`                   |
| Foreground ink                    | `#101B3F` | `#F4F7FF`                   |
| Card                              | `#FFFFFF` | `#0D1935`                   |
| Popover                           | `#FFFFFF` | `#132244`                   |
| Primary action and focus ring     | `#1A5CEB` | `#72A0FF`                   |
| Primary hover                     | `#1550D4` | `#8FB3FF`                   |
| Primary foreground                | `#FFFFFF` | `#071126`                   |
| Secondary and accent              | `#EAF2FF` | `#132244`, `#1A2B52`        |
| Muted                             | `#EDF1FA` | `#132244`                   |
| Secondary/muted/accent foreground | `#101B3F` | `#F4F7FF`                   |
| Muted foreground                  | `#56648C` | `#9EACCA`                   |
| Border                            | `#DCE5F5` | `rgba(180, 200, 255, 0.16)` |
| Input border                      | `#7886AE` | `#5A6A95`                   |
| Link                              | `#123EDB` | `#8FB3FF`                   |
| Seal                              | `#CC2649` | `#CC2649`                   |
| Editor canvas                     | `#EEF3FC` | `#071126`                   |
| Seal text and strokes             | `#CC2649` | `#FF6B8A`                   |
| Destructive                       | `#B54708` | `#FD8A4B`                   |

Brand and surface tokens have Tailwind color utilities such as `bg-surface-blue`
and `text-brand-indigo`:

| Token                                | Light                           | Dark                 |
| ------------------------------------ | ------------------------------- | -------------------- |
| `--brand-blue`, `--brand-deep-blue`  | `var(--primary)`, `var(--link)` | same aliases         |
| `--brand-indigo`, `--brand-cyan`     | `#6254FF`, `#35C8F5`            | `#8B80FF`, `#54D6FF` |
| `--brand-purple`                     | `#A855F7`                       | `#C08BFF`            |
| `--surface-blue`                     | `#EAF2FF`                       | `#10224A`            |
| `--surface-indigo`, `--surface-pink` | `#F0EEFF`, `#FFF0FA`            | `#1A1A4A`, `#2A1533` |
| `--surface-destructive`              | 7% destructive, card            | 12% destructive      |

Text on the canvas, a card, or a tinted surface meets WCAG AA: 4.5:1 for normal
text and 3:1 for large text, input borders, and focus rings, measured over the
brightest canvas glow. Blue text uses `--link`, not `--primary` or the brand
colors. Brand colors are for fills, icons, and large text.

`--gradient-brand` runs from brand blue to brand indigo; it colors at most one
key phrase or hero action on a page. The logo no longer uses it.
`--gradient-aurora` holds the canvas glows, at 7 to 14 percent opacity in light
theme and 10 to 24 percent in dark theme.

Chrome spacing follows the 8 px module, and its named rhythms are custom
properties: `--space-module` (8 px), `--space-dialog-title` (6 px),
`--space-field` (16 px), `--space-dialog` (24 px), `--space-section` (80 px),
and `--space-section-lg` (112 px).

The standard radius is 10 px, dialogs use 14 px, feature and marketing cards use
`--radius-feature` (20 px), and the sheet stays at 2 px. `rounded-md` and
`rounded-lg` both resolve to the 10 px `--radius`. Product surfaces use
`--shadow-product`, a soft blue-tinted shadow. The sheet uses `--shadow-paper`,
a neutral shadow with no blue. The theme preference is persisted in the
`aboutme-theme` cookie.

Chrome that stands for a resume sheet uses the paper tokens, defined once on
`:root` and never switched by the dark theme: `--paper` `#FFFFFF`, `--paper-ink`
`#171A18`, `--paper-muted` `#5F6763`, and `--paper-hover` `#F0F2F1`. The
`.paper-surface` class paints the paper ground and ink and rebinds foreground,
muted foreground, accent, ring (`#1A5CEB`), and link (`#123EDB`) to their light
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
