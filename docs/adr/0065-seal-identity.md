# 0065: Seal identity

Status: Proposed (2026-09-27). The owner chose the logo direction and asked for
the seal redesign; this record awaits approval in review.

Amends [ADR 0050](0050-aurora-application-identity.md): its logo, its seal
stamp, its pink and orange accents, its duplicate blues, its destructive hue,
and its chrome type scale.

## Context

The Aurora identity gave the chrome a recognizable color, but the logo carried
it through a cyan to blue to indigo gradient that reads like many other SaaS and
AI products, and its cut-out "a" blurred below 24 px. The product's one
distinctive act, a person stamping their resume public, appeared only in the
round seal, whose ring text overlapped itself once a slug passed about ten
characters. For Vietnamese readers a red seal on white paper already means an
official document.

A design review found five smaller problems:

- Three near-identical blues (`--primary`, `--brand-blue`, `--brand-deep-blue`
  with `--link`) with no rule for choosing between them.
- `--destructive` (`#B42318`) sat next to `--seal` (`#CC2649`), so the filled
  Delete button read like the filled Publish button, against the one-red,
  one-meaning rule.
- `--brand-pink` and `--brand-orange` were almost unused.
- `--text-3xl` (44 px) was larger than `--text-4xl` (40 px), and the 1.2 heading
  line height left little room for stacked Vietnamese diacritics (ệ, ở, Ử).
- `--seal` on the midnight ground is 3.5:1, too low for red words or strokes.

## Decision

- **Logo B, the seal.** The mark is a stamp: a thick outer ring, a hairline
  inner ring, and a single-story "a", tilted −8° like the public stamp, in seal
  red. The wordmark keeps its stroked lowercase `aboutme.vn` in the text color,
  and only the dot before "vn" is seal red. The logo has no gradient, mask, or
  id. At 24 px and in the favicon, where it would render under a pixel, the
  hairline inner ring drops out.
- **The seal extends to the logo.** Red still has one meaning, public, and the
  logo carries the same promise. `AppLogo` joins `AppSeal` and the Publish
  button as the only direct consumers of the seal tokens.
- **`AppSeal` v2.** The stamp is a rounded ticket: the logo's seal mark (its
  outer ring and "a") and the word PUBLIC (CÔNG KHAI in Vietnamese) on top, a
  hairline, then the public link. It is rotated −6°. Its width comes from
  measured Be Vietnam Pro advances for the word and for each slug character
  (`sealLayout.ts`): at least 156 px, growing with the link to 260 px, and past
  that it squeezes the link to fit, so a slug never overlaps itself. The word
  follows the page language, so `locale` is a required prop. The mark is a 20 px
  seal tile with a check.
- **`--seal-text`.** Seal red for words and thin strokes: `#CC2649` in light
  theme, `#FF6B8A` in dark theme (6.9:1 on `#071126`). Fills keep `--seal`.
- **Two blues.** `--primary` fills and `--link` colors blue text. `--brand-blue`
  and `--brand-deep-blue` become aliases of those two.
- **Burnt-orange destructive.** `--destructive` is `#B54708` in light theme and
  `#FD8A4B` in dark theme, and the destructive button is an outline. Delete can
  no longer be mistaken for Publish.
- **Pink and orange retire** as brand colors. `--surface-pink` stays for the
  third feature card, and the canvas glows keep their faint pink.
- **Type scale.** `--text-sm` is 14 px and `--text-base` (the chrome body) is 15
  px. `--text-3xl` is 40 px and `--text-4xl` is 48 px. Headings take line height
  1.3 (lg to 2xl), 1.25 (3xl, 4xl), and 1.2 (6xl).
- **Spacing tokens.** The 8 px module and the named rhythms from `DESIGN.md`
  become `--space-*` custom properties.
- **Brand art from tokens.** The README banners, the GitHub social preview, and
  the site Open Graph image are HTML sources in `docs/brand/src/`, rendered with
  `apps/web/scripts/render-brand.mjs`. The Open Graph image follows the page
  language; Vietnamese is the default.

## Rejected alternatives

- **A: the current document mark, flat.** The smallest change, but it keeps a
  generic shape and adds no meaning.
- **C: wordmark only, red ".vn".** Quiet, but it has no mark for favicons or the
  link-preview card, and red on the domain says nothing about publishing.
- **Keep the round text-on-a-path stamp.** Its ring cannot hold a 30-character
  slug at a legible size.

## Consequences

- `theme.css`, `base.css`, `AppLogo`, `AppSeal`, and the button primitive
  change, and their guard tests change with them. The destructive variant joins
  `default`, `link`, and `seal` as a guarded token edit (ADR 0052).
- Icons move to `-v3` names (`favicon-v3.svg`, `icon-32-v3.png`,
  `apple-touch-icon-v3.png`, `icon-192-v3.png`, `icon-512-v3.png`,
  `site-v3.webmanifest`) and `favicon.ico` is redrawn. The Open Graph image
  moves to `og-image-v4.jpg` and `og-image-v4-en.jpg`. The `-v2` icons and
  `og-image-v3.jpg` stay in `public/` for one release, unlinked, so caches,
  shared links, and crawlers that hold the old URLs get no 404; remove them in
  the release after this one.
- Chrome screenshot baselines change: the body and control sizes, the logo, the
  seal, and the destructive buttons all move. The renderer, its tokens, golden
  HTML, and document screenshot baselines do not change.
- The stored link-preview card (ADR 0055) draws `AppLogo` mark-only, so the card
  layout version rises from 1 to 2 on the web and in Go, and every live card
  gets a new URL. The card stylesheet pins the mark to `#CC2649`, because the
  print stylesheet has no Tailwind utilities and the white card needs the light
  seal red in both themes.
- The production maintenance page (`deploy/caddy/production/maintenance.html`)
  carries its own copy of the tokens and an inline logo. It takes Logo B, the
  `--seal` values, and the primary blue in its glows; its CSP hashes are
  recomputed from the page at build, and `test.sh` probes a fetched `url()` on
  the wordmark stroke instead of the removed gradient.
