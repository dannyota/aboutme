# 0050: Aurora application identity

Status: Accepted (2026-09-24), approved by the owner.

Supersedes in part [ADR 0030](0030-stamped-document-visual-identity.md): its
signature blue-black ink, its desk neutrals and lamp-lit dark palette, its 6 px
chrome radius, and its seal hex.

## Context

The stamped-document identity made the product's claim visible: the resume is
public and the person is not. Its chrome was a grey desk with blue-black actions
and a text-only `aboutme`. The result read as quiet and generic, the wordmark
was too weak for a header, favicon, or share image, and nothing gave the product
a recognizable color. The owner approved a more colorful direction that keeps
the document and publishing principles.

## Decision

Colorful product UI. Calm white resume.

- **Aurora canvas.** Application pages sit on a pale blue ground (`#F5F8FF`)
  with large, soft, static CSS glows in blue, indigo, cyan, and a little pink.
  Dark theme is midnight blue (`#071126` ground, `#0D1935` cards) with slightly
  stronger glows.
- **Blue leads.** Primary actions and focus use `#1A5CEB` in light theme and
  `#72A0FF` with dark text in dark theme. Links use a text-safe deep blue.
  Indigo, purple, and cyan are supporting accents; pink and orange are small,
  rare accents.
- **Logo.** Lowercase `aboutme` with a mark: a rounded document with a folded
  corner whose body forms a lowercase `a`, in a cyan to blue to indigo gradient.
  `me` takes the blue-to-indigo gradient. The logo is inline SVG with no font or
  external asset.
- **Friendlier shapes.** The standard radius is 10 px, dialogs 14 px, feature
  cards 20 px. Product surfaces take a soft blue-tinted shadow.
- **Seal red** moves to `#CC2649`, closer to coral while keeping white text on
  the Publish button above 4.5:1, including its hover state.
- **Contrast.** Text meets WCAG AA over the brightest point of the canvas.

These parts of ADR 0030 stay in force: one red with one meaning, state as a mark
and not a hue, Be Vietnam Pro as the one chrome typeface, the 8 px module and
the whole sheet, the motion rules, and a landing page that shows the real
renderer.

**Renderer isolation.** No Aurora token, gradient, radius, shadow, or chrome
style reaches the resume renderer, the render harness, the print path, or the
document area of a public resume page. The canvas and chrome tokens apply only
under `html[data-ui="app"]`, which those paths never set, and the chrome reset
keeps excluding `.resume-document`, `.paged-resume`, and their descendants. The
sheet keeps its 2 px radius, its neutral paper shadow, and its white ground in
both themes.

## Rejected alternatives

- **Keep the grey desk.** Calm and consistent, but it does not give the product
  a recognizable face.
- **Gradient everywhere.** Energetic, but it competes with the resume and
  weakens the single meaning of each color.
- **A raster logo.** Faithful to the supplied artwork, but it adds a runtime
  asset, cannot follow the theme, and blurs at small sizes.

## Consequences

- `theme.css` takes the new values and keeps every shadcn token name. It adds
  brand, surface, link, gradient, radius, and product shadow tokens.
- `base.css` paints the canvas on the application body only.
- `AppLogo` joins `components/app`; `favicon.svg` becomes the mark.
- The seal, Publish button, and public mark change hue with `--seal`.
- The renderer, its tokens, fonts, golden HTML, and screenshot baselines do not
  change.
- Tests that assert chrome token values change with those values.
