# aboutme home page

The home page at `/`, in the identity the owner chose on 2026-11-15
([ADR 0020](../../adr/0020-application-visual-identity.md)). Part of the
[visual design](../../../DESIGN.md). The copy lives in the web app's landing
copy file, in Vietnamese and English.

The page title is `aboutme.vn · CV miễn phí, riêng tư đến khi bạn muốn`, and in
English `aboutme.vn · Free resumes, private until you choose`.

## Scope

The identity reaches the application one change at a time. Each change ships
alone and leaves a coherent look.

| Order | Surfaces                                                                                                 | State              |
| ----- | -------------------------------------------------------------------------------------------------------- | ------------------ |
| 1     | The home page                                                                                            | Shipped in v0.9.12 |
| 2     | Tokens, type, primitives, and the shell header                                                           | Shipped in v0.9.16 |
| 3     | Sign-in, registration, password reset, email verification, second factor, and consent                    | Shipped in v0.9.17 |
| 4     | The editor shell, the publish dialog, and the resumes list                                               | Shipped in v0.9.18 |
| 5     | The Library, template pages, and the showcase                                                            | Shipped in v0.9.19 |
| 6     | A footer shared by the public pages, the MCP guide, the legal pages, the verify page, and the error page | Next               |
| 7     | Settings, views, the projects workspace, LinkedIn import, and removing the retired token aliases         | After 6            |
| 8     | The public page bar and public project pages                                                             | After 2            |
| 9     | Icons, Open Graph images, brand art, the link-preview card pin, the maintenance page, and the mail color | Next               |

Until change 2 ships, the home route scopes its tokens and the chrome typeface
on the root element, and leaving the home page changes ground, ink, primary
color, typeface, and button shape. After change 2 every route shares the
[tokens](typography-and-tokens.md); a surface whose own change has not shipped
keeps its old layout in the new colors, and tokens that the identity removes
stay as aliases of their replacements until change 7 deletes them. Until change
9, icons, images, the card pin, and mail still use the earlier red `#CC2649`.

## Tokens

### Color

The [token page](typography-and-tokens.md#color) holds every color and its
contrast. The home page uses `--brand-panel` for the hero panel, mist for the
editor panel, the AI card, and row hover, blush for the template fan block and
the maker panel, sun for the data card, and the indigo panel for the publish
panel.

### Shape, shadow, spacing

- Radii: `--radius-panel` 28 px, 36 from 48rem, 44 from 64rem. `--radius-card`
  22, 26, 28. Pills 999 px. Inputs keep 10 px, dialogs 14 px, the sheet 2 px.
- `--shadow-paper-hero` (hero sheet):
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
| h1                      | Plus Jakarta Sans 800, tracking -0.025em | 40/1.3; 48/1.3 from 48rem; 60/1.3 from 80rem                      |
| h2 section              | 800, -0.02em                             | 32/1.3; 40/1.3 from 64rem                                         |
| h2 in a card            | 800, -0.015em                            | 24/1.3; 32/1.3 from 64rem                                         |
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

The lotus red is reserved for the logo and public. The main color is chàm, so a
primary button can never read as the seal.

- Public is the ticket: a rounded ticket tilted -6 degrees with a dashed inner
  line, the ring "a", the word PUBLIC or CÔNG KHAI, and the link. No other
  element uses that shape or uppercase.
- The ticket carries its own paper fill (`--card` plus a 7% tint). On the resume
  sheet it keeps light values in both themes.
- Seal words and strokes use `--seal-text`, not the fill.
- The Publish button keeps its own variant: `--seal` fill, `--seal-hover` on
  hover, and the ring "a" before the label. It is the only red fill.
- Decorative chips on the home page are pills and circles, never tickets.

## Logo

Shapes are unchanged (`AppLogo`, `favicon-v3.svg` geometry).

| Ground                           | Wordmark           | Mark and the dot before "vn" |
| -------------------------------- | ------------------ | ---------------------------- |
| Light `#FFFFFF`                  | `#1A1650` (16.5:1) | `#DE1B5A` (4.76:1)           |
| Dark `#0E0A0D`                   | `#D3CAD0` (12.3:1) | `#E2788F` (6.85:1)           |
| Indigo footer or panel `#1A1650` | `#FFFFFF`          | `#FF7AA2` (6.71:1)           |

On the hero panel the whole logo is white. Favicon stroke: `#DE1B5A`, `#E2788F`
under `prefers-color-scheme: dark`; the [brand files](identity-and-seal.md#logo)
list the current set.

## Layout

The page performs no data fetch. It keeps the compiled-in sample resume rendered
by the shared renderer on a whole white A4 sheet; signed-in and signed-out
actions; decorative chips skipped by screen readers; footer links and their
order (now on a `--footer` band, logo left, links right, license line below).
Breakpoints: 28rem, 48rem, 64rem, 80rem (72rem for the header only). Everything
stacks below 64rem.

| Section                        | Below 64rem                                        | From 64rem                                                                                                        |
| ------------------------------ | -------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| Hero, `--brand-panel`          | copy, then sheet centered                          | copy `1fr` and sheet column, gap 44; `6fr 5fr`, gap 48 from 80rem; copy centered vertically, sheet bottom-aligned |
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
  seal ticket bottom right. Rings: the logo's two rings in black at 14% over the
  hero ground, cropped by the panel's top right corner. Hero text and the
  outline pill take `--panel-foreground`; the filled hero pill is
  `--panel-foreground` with a `--coin-ink` label.
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
  crop, 168 px, 176 from 48rem, 232 from 64rem, 6 px white border. Buttons: main
  pill (Create, or Open your resumes when signed in), then two outline pills.
