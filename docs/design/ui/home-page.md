# aboutme home page

The home page at `/`, in the identity the owner chose on 2026-11-15
([ADR 0020](../../adr/0020-application-visual-identity.md)). Part of the
[visual design](../../../DESIGN.md). The copy lives in the web app's landing
copy file, in Vietnamese and English.

## Scope

The identity ships in two stages, at most one release apart.

1. The home page alone. Its tokens and the chrome typeface are scoped to the
   home route on the root element, so the shell header and logo on that page
   take the new colors while their layout stays. Every other route, the seal
   geometry, the renderer, brand art, and icons are untouched.
2. The rest of the chrome: header and footer layout, sign-in and registration,
   the editor shell, the Library, the showcase, the MCP guide, legal pages, the
   public page bar, brand art, icons, Open Graph images, the link-preview card
   pin, and the maintenance page.

Between the stages, leaving the home page changes ground, ink, primary color,
typeface, and button shape, and two reds exist: `#DE1B5A` on the home page and
`#CC2649` on every seal, Publish button, and icon.

## Tokens

### Color

| Token                                  | Light                           | Dark                               | Role                                                                                           |
| -------------------------------------- | ------------------------------- | ---------------------------------- | ---------------------------------------------------------------------------------------------- |
| `--background`                         | `#FFFFFF`                       | `#0F0D26`                          | page ground                                                                                    |
| `--foreground`                         | `#1A1650`                       | `#F3F1FF`                          | text, outline pills, wordmark                                                                  |
| `--muted-foreground`                   | `#55517D`                       | `#B7B2DB`                          | leads, descriptions, notes                                                                     |
| `--brand` (also `--primary`, `--seal`) | `#DE1B5A`                       | `#DE1B5A`                          | hero panel, primary pills, switch on, seal fills; text on it is `--brand-foreground` `#FFFFFF` |
| `--brand-hover`                        | `#C7134D`                       | `#C7134D`                          | hover fill, tone-on-tone hero rings                                                            |
| `--brand-text` (also `--seal-text`)    | `#C7134D`                       | `#FF7AA2`                          | pink words and thin strokes on a ground                                                        |
| `--surface-mist`                       | `#F1EFFA`                       | `#191640`                          | editor panel, AI card, locale toggle, row hover                                                |
| `--surface-blush`                      | `#FFE4EC`                       | `#3B1431`                          | template fan block, maker panel                                                                |
| `--surface-sun`                        | `#FFF2C2`                       | `#332A0E`                          | data card                                                                                      |
| `--panel`, `--footer`                  | `#1A1650`, `#1A1650`            | `#322C8F`, `#191640`               | publish panel and footer band; text on them is `#FFFFFF`, lead `#D9D6F2`                       |
| `--card`                               | `#FFFFFF`                       | `#25225A`                          | cards and tags inside panels                                                                   |
| `--border`, `--hairline`               | `#E3E0F2`, `rgba(26,22,80,.14)` | `#34315C`, `rgba(243,241,255,.16)` | decorative borders, row separators                                                             |
| `--control`                            | `#7F7AAE`                       | `#8580B8`                          | input border, switch off, dashed tag                                                           |
| `--coin`, `--coin-edge`                | `#FFD23F`, `#E9B416`            | same                               | coins, bullets, link icon; ink on it is `#1A1650` (11.4:1)                                     |
| `--ring`                               | `#1A1650`                       | `#C9C3FF`                          | focus ring; `#FFFFFF` on brand, panel and footer                                               |

### Contrast (text on ground)

WCAG 2 ratios, computed by script.

| Pair                                                 | Light                        | Dark                         |
| ---------------------------------------------------- | ---------------------------- | ---------------------------- |
| foreground on background, mist, blush, sun, card     | 16.5, 14.5, 13.8, 14.7, 16.5 | 17.0, 15.3, 14.2, 12.7, 13.0 |
| muted on background, mist, blush, sun, card          | 7.3, 6.5, 6.1, 6.6, 7.3      | 9.4, 8.5, 7.8, 7.0, 7.2      |
| white on `--brand`, on `--brand-hover`               | 4.76, 5.79                   | 4.76, 5.79                   |
| `--brand-text` on background, card, mist             | 5.79, 5.79, 5.09             | 7.74, 5.88, 6.96             |
| white, `#D9D6F2` on `--panel`                        | 16.5, 11.7                   | 11.1, 7.9                    |
| seal words on the ticket fill (card plus 7% tint)    | 5.19                         | 5.30                         |
| `--control` on card, mist, sun (3:1 needed)          | 3.96, 3.49, 3.54             | 3.97, 4.70, 3.91             |
| `--ring` on background, mist, blush (3:1 needed)     | 16.5, 14.5, 13.8             | 11.5, 10.4, 9.6              |
| `--brand` fill against background, card (3:1 needed) | 4.76, 4.76                   | 3.99, 3.03                   |

Never set `--brand-text` light on `--panel` (2.85) and never set white text on
`--coin`.

### Shape, shadow, spacing

- Radii: `--radius-panel` 28 px, 36 from 48rem, 44 from 64rem. `--radius-card`
  22, 26, 28. Pills 999 px. Inputs keep 10 px, dialogs 14 px, the sheet 2 px.
- `--shadow-paper` (hero sheet):
  `0 36px 56px -28px rgba(0,0,0,.5), 0 2px 6px rgba(0,0,0,.16)`, neutral, same
  in both themes.
- `--shadow-float` (chips, coins, avatar):
  `0 16px 24px -14px rgba(26,22,80,.55)`, dark `rgba(0,0,0,.7)`. `--shadow-card`
  (cards inside panels): `0 32px 48px -32px rgba(26,22,80,.45)`, dark
  `rgba(0,0,0,.75)`.
- The 8 px module stays. Panel padding 32x24, 56x48 from 48rem, 64x56 from
  64rem, 72 from 80rem. Page gutter 16, 24, 32. Container 1264 px including
  gutters.
- `--space-section` 80 and `--space-section-lg` 112 stay. Adjacent panels sit 16
  px apart, 24 from 48rem. Motion: 150 ms color transitions only, off under
  reduced motion; nothing animates on load.

### Type

| Role                    | Family, weight                           | Size / line height                                                |
| ----------------------- | ---------------------------------------- | ----------------------------------------------------------------- |
| h1                      | Plus Jakarta Sans 800, tracking -0.025em | 40/1.18; 48/1.18 from 48rem; 60/1.15 from 80rem                   |
| h2 section              | 800, -0.02em                             | 32/1.25; 40/1.18 from 64rem                                       |
| h2 in a card            | 800, -0.015em                            | 24/1.3; 32/1.25 from 64rem                                        |
| h3 rows, template names | 800                                      | 16/1.4                                                            |
| Hero lead               | 500                                      | 16/1.6; 18 from 48rem; 20 from 80rem                              |
| Section lead            | 500, muted                               | 16/1.6; 18 from 48rem                                             |
| Body, facts             | 500, facts 700                           | 16/1.6; descriptions 15/1.5; notes and labels 14/1.5              |
| Buttons, nav            | 700, nav 600                             | 16/1.25, min height 52; header button 14, height 42 to 44; nav 15 |
| Resume sheet, seal      | Be Vietnam Pro, unchanged                | renderer and `sealLayout.ts` values                               |

Plus Jakarta Sans already ships as
`app/assets/fonts/plus-jakarta-sans-var.woff2`. Fallback stack:
`'Plus Jakarta Sans', 'Be Vietnam Pro', system-ui, sans-serif`.

## The public mark

`--seal` equals `--brand`: a second red beside it would differ by 1.12:1. Public
is carried by the mark, not the hue.

- Public is the ticket: a rounded ticket tilted -6 degrees with a dashed inner
  line, the ring "a", the word PUBLIC or CÔNG KHAI, and the link. No other
  element uses that shape or uppercase.
- The ticket carries its own paper fill (`--card` plus a 7% tint), so it never
  sits pink on pink. On the resume sheet it keeps light values in both themes.
- Seal words and strokes use `--seal-text`, not the fill.
- The Publish button keeps its own variant: brand fill plus the ring "a" before
  the label. No other pink button carries the ring "a".
- Decorative chips on the home page are pills and circles, never tickets.

## Logo

Shapes are unchanged (`AppLogo`, `favicon-v3.svg` geometry).

| Ground                           | Wordmark           | Mark and the dot before "vn" |
| -------------------------------- | ------------------ | ---------------------------- |
| Light `#FFFFFF`                  | `#1A1650` (16.5:1) | `#DE1B5A` (4.76:1)           |
| Dark `#0F0D26`                   | `#F3F1FF` (17.0:1) | `#FF7AA2` (7.74:1)           |
| Indigo footer or panel `#1A1650` | `#FFFFFF`          | `#FF7AA2` (6.71:1)           |

On brand pink the whole logo is white. Favicon stroke: `#DE1B5A`, `#FF7AA2`
under `prefers-color-scheme: dark`.

## Layout

The page performs no data fetch. It keeps the compiled-in sample resume rendered
by the shared renderer on a whole white A4 sheet; signed-in and signed-out
actions; decorative chips skipped by screen readers; footer links and their
order (now on a `--footer` band, logo left, links right, license line below).
Breakpoints: 28rem, 48rem, 64rem, 80rem (72rem for the header only). Everything
stacks below 64rem.

| Section                        | Below 64rem                                        | From 64rem                                                                                                        |
| ------------------------------ | -------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| Hero, `--brand` panel          | copy, then sheet centered                          | copy `1fr` and sheet column, gap 44; `6fr 5fr`, gap 48 from 80rem; copy centered vertically, sheet bottom-aligned |
| Editor, `--surface-mist` panel | text and facts, then picture                       | `4fr 6fr`, gap 48; `5fr 6fr`, gap 64 from 80rem                                                                   |
| Templates, no panel            | heading, fan, list, button                         | fan left spanning both rows, heading and list right, `6fr 5fr`, gap 64                                            |
| Publish, `--panel`             | text, then card, gap 64                            | `5fr 6fr`, gap 56                                                                                                 |
| AI and data cards              | stacked                                            | `7fr 5fr`                                                                                                         |
| Maker, `--surface-blush`       | centered stack below 48rem; avatar left from 48rem | avatar 232 px left, text right, gap 56                                                                            |

Hero sheet (rotated -3 degrees, hangs below the panel):

| Width       | Zoom | Sheet        | Overhang |
| ----------- | ---- | ------------ | -------- |
| below 28rem | 0.36 | 286 x 404 px | 48       |
| from 28rem  | 0.5  | 397 x 561    | 48       |
| from 48rem  | 0.6  | 476 x 673    | 64       |
| from 64rem  | 0.5  | 397 x 561    | 72       |
| from 80rem  | 0.54 | 429 x 606    | 72       |

- Hero actions stack full width below 28rem. Signed out: Create (white pill),
  Browse (white outline), Sign in (white text link). Signed in: Open your
  resumes (white pill), Browse.
- Hero chips, all `aria-hidden`: link pill top left (rotated 4 degrees), "0đ"
  coin on the left edge (64 px, 80 from 28rem), small "a" coin top right, the
  seal ticket bottom right. Rings: the logo's two rings in `--brand-hover`,
  cropped by the panel's top right corner.
- Editor facts: four rows with hairlines and a 14 px coin bullet. The picture is
  a labeled illustration of the editor until a real screenshot replaces it. Data
  card: a 150 px coin (124 from 64rem, 190 from 80rem) cropped by the bottom
  right corner.
- Template fan: `--surface-blush`, four real first pages, 40% wide, rotated -9,
  -3, 3, 9 degrees, `aria-hidden`. List: four rows, each one link to
  `/templates/{id}`; a 124 px category label and the name with its catalog
  purpose from 28rem. Outline pill to `/templates`.
- Publish card: "Example settings", three rows (title, link on the first,
  description, state word and a decorative switch), stamp on the top right edge.
- Maker: `maker-avatar-320.webp` and `maker-avatar-640.webp` by `srcset`, circle
  crop, 168 px, 176 from 48rem, 232 from 64rem, 6 px white border. Buttons:
  brand pill (Create, or Open your resumes when signed in), then two outline
  pills.
