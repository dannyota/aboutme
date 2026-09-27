# 0020: Application visual identity: the seal, the aurora canvas, and a white resume

Status: Accepted (2026-09-04, 2026-09-24, 2026-09-27). The owner chose each
direction. The public page color scheme below is Proposed, pending owner
approval.

## Context

The product's one claim a neighbor cannot copy is that the resume is public and
the person is not. For Vietnamese readers a red seal on white paper already
means an official document. The first stamped-document chrome made that claim
visible but read as quiet and generic; the colorful aurora chrome that followed
gave the product a face, but its gradient logo looked like many SaaS and AI
products, blurred below 24 px, and its round seal's ring text overlapped once a
slug passed about ten characters. A design review also found three
near-identical blues, a destructive red next to the seal red, unused pink and
orange, an inverted type scale, and a seal red too dark on the midnight ground.

## Decision

Colorful product UI, calm white resume, and one red that means public.

**Principles.**

- **One red, one meaning.** Seal red means "public at this link". Only the seal
  (`AppSeal`), the logo (`AppLogo`), the public state mark, and the Publish
  control use the seal tokens. Destructive actions use a separate hue.
- **State is a mark, not a hue.** Saved, saving, failed, draft, and public are a
  pencil tick, pencil text, destructive text, or the seal mark with the link
  (`StateMark`), never colored chips.
- **One chrome typeface.** `'Be Vietnam Pro', 'Inter', system-ui, sans-serif`,
  designed for Vietnamese diacritics and already shipped for the renderer.
  Uppercase appears only inside the seal.
- **One module, whole sheet.** An 8 px module rules chrome alignment, exposed as
  `--space-*` custom properties with the named rhythms. The preview sheet is
  never cropped; chrome restacks around it, down to 390 px.
- **Motion answers actions.** 150 ms color transitions, the primitives' own open
  and close, and the stamp: the public mark lands in one 180 ms press and lifts
  in 120 ms. Nothing animates on load. Reduced motion disables all of it.
- **The landing shows the product.** The home page renders one compiled-in
  sample resume through the shared renderer at server render, with its seal, and
  performs no data fetch.

**Canvas and color.**

- Application pages sit on a pale blue ground (`#F5F8FF`) with large, soft,
  static CSS glows in blue, indigo, cyan, and a faint pink. Dark theme is
  midnight blue (`#071126` ground, `#0D1935` cards) with slightly stronger
  glows.
- Two blues: `--primary` fills (`#1A5CEB` light; `#72A0FF` with dark text in
  dark theme) and `--link` colors blue text. `--brand-blue` and
  `--brand-deep-blue` are aliases of those two. Indigo, purple, and cyan are
  supporting accents. Pink and orange are not brand colors; `--surface-pink`
  stays for the third feature card.
- `--seal` fills are `#CC2649`, keeping white Publish text above 4.5:1 including
  hover. `--seal-text` colors words and thin strokes: `#CC2649` light, `#FF6B8A`
  dark (6.9:1 on `#071126`).
- `--destructive` is burnt orange, `#B54708` light and `#FD8A4B` dark, and the
  destructive button is an outline, so Delete cannot be mistaken for Publish.
- Text meets WCAG AA over the brightest point of the canvas.

**Shape and type.** The standard radius is 10 px, dialogs 14 px, feature cards
20 px; product surfaces take a soft blue-tinted shadow. `--text-sm` is 14 px,
`--text-base` (chrome body) 15 px, `--text-3xl` 40 px, and `--text-4xl` 48 px.
Headings take line height 1.3 (lg to 2xl), 1.25 (3xl, 4xl), and 1.2 (6xl).

**Logo.** The mark is a stamp: a thick outer ring, a hairline inner ring, and a
single-story "a", tilted −8° like the public stamp, in seal red. The wordmark is
stroked lowercase `aboutme.vn` in the text color, and only the dot before "vn"
is seal red. The logo is inline SVG with no gradient, mask, id, font, or
external asset. At 24 px and in the favicon, the hairline inner ring drops out.

**Seal.** `AppSeal` is a rounded ticket rotated −6°: the logo's seal mark (outer
ring and "a") and the word PUBLIC (CÔNG KHAI in Vietnamese) on top, a hairline,
then the public link. Its width comes from measured Be Vietnam Pro advances for
the word and each slug character (`sealLayout.ts`): at least 156 px, growing
with the link to 260 px, and past that it squeezes the link to fit, so a slug
never overlaps itself. The word follows the page language, so `locale` is a
required prop. The compact mark is a 20 px seal tile with a check.

**Brand art from tokens.** The README banners, the GitHub social preview, and
the site Open Graph image are HTML sources in `docs/brand/src/`, rendered with
`apps/web/scripts/render-brand.mjs`. The Open Graph image follows the page
language; Vietnamese is the default.

**Renderer isolation.** No chrome token, gradient, radius, shadow, or style
reaches the resume renderer, the render harness, the print path, or the document
area of a public page. Chrome tokens apply only under `html[data-ui="app"]`, and
the public page bar carries its own copy scoped to `.public-toolbar`, as the
maintenance page does. The sheet keeps its 2 px radius, neutral paper shadow,
and white ground in both application themes; only the owner's dark scheme below
changes its ground.

**Public page color scheme (Proposed).** The resume stays white unless its owner
picks otherwise for the public page on screen.

- The owner picks Light (the default), Dark, or Match device per resume, as the
  document field `customization.colorScheme` (ADR 0004). Match device follows
  each viewer's `prefers-color-scheme` through a CSS media query. Viewers get no
  toggle, and the page runs no script for it.
- The scheme applies only to the public page on screen and the editor's Web
  preview. The PDF, browser print, the print path, the link-preview card, the
  sign-in gate, thumbnails, template pages, and the landing sample stay white.
- The dark colors come from one pure rule over the resume's own five colors,
  followed by the unchanged role derivation and its contrast floors. No chrome
  token enters the document area, and no template has hand-picked dark colors.
- The page bar takes the light or midnight values of its scoped tokens to match
  the page's scheme. Its mark is the logo's seal mark in `--seal-text`.

## Rejected alternatives

- **The toolkit default, or the grey desk.** Consistent and cheap, but generic.
- **Gradient everywhere, or a gradient logo.** Competes with the resume, weakens
  each color's meaning, and reads like other SaaS products.
- **A raster logo.** Adds a runtime asset, cannot follow the theme, and blurs at
  small sizes.
- **Logo A, the document mark made flat.** Keeps a generic shape and adds no
  meaning.
- **Logo C, wordmark only with a red ".vn".** No mark for favicons or the
  link-preview card, and red on the domain says nothing about publishing.
- **The round text-on-a-path stamp.** Its ring cannot hold a 30-character slug
  at a legible size.
- **A viewer light and dark toggle on the public page.** It overrides the
  owner's choice, and a remembered choice needs a script before first paint,
  which the public page's script policy forbids.
- **Hand-picked dark palettes per template.** Twenty palettes to keep in step
  with the light ones, and a user's own colors would still need a rule.
- **A dark PDF.** A PDF is printed and forwarded; a dark ground wastes ink and
  loses the white paper the seal stands on.

## Consequences

- `theme.css` keeps every shadcn token name and adds brand, surface, link, seal,
  gradient, radius, spacing, and shadow tokens. `base.css` paints the canvas on
  the application body only.
- The button primitive's guarded edits (ADR 0019) follow these tokens.
- Icons use versioned file names (`favicon-v3.svg`, `icon-*-v3.png`,
  `apple-touch-icon-v3.png`, `site-v3.webmanifest`) and the Open Graph image is
  `og-image-v5.jpg` and `og-image-v4-en.jpg`. A replaced icon or image set stays
  in `public/`, unlinked, for one release so caches and crawlers get no 404.
- The stored link-preview card (ADR 0014) draws `AppLogo` mark-only and pins it
  to `#CC2649` in both themes; a logo change raises the card layout version.
- The production maintenance page (`deploy/caddy/production/maintenance.html`)
  carries its own copy of the tokens and an inline logo, and follows them; its
  CSP hashes are recomputed from the page at build.
- Chrome screenshot baselines change with these values. The renderer, its
  tokens, fonts, golden HTML, and document screenshot baselines do not.
- The public page baselines change with the page bar, and the dark scheme adds
  its own public page baselines. Paged, print, and template baselines do not
  change.
- `PRODUCT.md` and the [UI design pages](../design/ui/identity-and-seal.md) hold
  the built detail; the [public page spec](../design/public-page-theme.md) holds
  the page bar, the scheme, and the dark palette rule.

## History

- Former ADR 0030 (2026-09-04): stamped-document identity: seal red only for
  public, signature-ink blue-black actions, desk neutrals, marks not hues, Be
  Vietnam Pro, the 8 px module, motion, and a landing that shows the renderer.
  Its palette and radius were replaced by 0050.
- Former ADR 0050 (2026-09-24): aurora canvas, blue-led palette, gradient
  document logo, friendlier radii, and seal `#CC2649`. Its logo, stamp, accents,
  duplicate blues, destructive hue, and type scale were replaced by 0065.
- Former ADR 0065 (2026-09-27): Logo B, `AppSeal` v2, `--seal-text`, two blues,
  burnt-orange outline destructive, the type scale, spacing tokens, and brand
  art from tokens.
- Proposed (2026-09-27, pending owner approval): the owner's public page color
  scheme and the page bar's scoped token copy. It narrows the white-ground rule
  to "white unless the owner picks a dark scheme for the public page on screen".
