# aboutme logo and seal

The logo, the public seal, and the state marks. Part of the
[visual design](../../../DESIGN.md).

## Logo

The logo is the seal. The mark is a stamp in the lotus red, the color that means
public: a thick outer ring, a hairline inner ring, and a single-story `a`,
tilted −8° like the public stamp. The wordmark is lowercase `aboutme.vn` in the
text color, and only the dot before `vn` is red. Strokes and a filled-circle dot
draw the letters without a font, and nothing uses a gradient or an id.

`AppLogo` renders it inline at 24, 32, or 48 px high, or as the mark alone, as
one image named “aboutme.vn”. At 24 px the hairline inner ring drops out. The
mark and the dot take `--logo-mark` (`#DE1B5A` light, `#E2788F` dark) and
`--logo-mark-inverse` (`#FF7AA2`) on indigo grounds; on the home hero the whole
logo is white. The link-preview card pins them to `#DE1B5A` on its white ground.
Under `forced-colors: active`, they use the text color.

Brand files carry a version in their names so browsers and crawlers fetch a
changed file; the previous set stays in `public/`, unlinked, for one release.
The current set:

- `favicon-v4.svg`: the mark with the inner ring dropped, `#DE1B5A`, and
  `#E2788F` under `prefers-color-scheme: dark`.
- `icon-32-v4.png`, `icon-192-v4.png`, `icon-512-v4.png`,
  `apple-touch-icon-v4.png`: the red mark on a white square (owner, 2026-11-17).
  `site-v4.webmanifest` sets `theme_color` and `background_color` to `#ffffff`.
  The MCP server's `serverInfo` names the same files.
- `og-image-v8.jpg` (Vietnamese) and `og-image-v7-en.jpg`: white ground, the
  logo, the headline in indigo ink in Plus Jakarta Sans 800 at line height 1.3,
  and a chàm (`#594BAD`) panel holding a tilted white sheet with the red CÔNG
  KHAI or PUBLIC ticket and a coin. No blue.
- README banners and the repository social preview use the same parts: the light
  banner on white, the dark banner on `#0E0A0D` with `#D3CAD0` text and the mark
  in `#E2788F`. Logo, stamp, wordmark and icon files take `#DE1B5A` for strokes
  on white and `#C7134D` for stamp words; `#26409C` and `#CC2649` leave the
  brand files.

The link-preview card's pin change raises the card layout version from 2 to 3,
in `cardLayout.ts` and `previewcard/card.go` together; the bounded background
sweep then rebuilds every live card. Account emails take `#DE1B5A` for the mark
and dot. The production maintenance page carries its own copy of the tokens
(ground, ink, card, muted, border, ring, link, seal, mist; light and dark) from
the [token page](typography-and-tokens.md#color), and its CSP hashes are
recomputed from the page at build. Brand sources, where each is used, and how to
re-render them are in [`docs/brand/`](../../brand/README.md).

## Seal and state marks

`AppSeal` has two implemented forms:

- The stamp is a 72 px high rounded ticket in `--seal-text`: a 2.5 px border
  over a 7 percent tint on an opaque `--card` ground, a dashed inner hairline,
  the logo's seal mark and the word PUBLIC (CÔNG KHAI when `locale` is `vi`) on
  top, a hairline, and `aboutme.vn/<slug>` underneath. Its width comes from
  measured glyph advances (`sealLayout.ts`): it grows with the link up to 260 px
  and squeezes a longer link to fit, so the text never overlaps, and it shrinks
  to fit a narrow container. Its default rotation is -6 degrees.
- The 20 px mark is a seal tile, a `--seal` square with 6 px corners and a white
  check, and carries the public link beside it.

`StateMark` exposes six states: Saved (pencil tick), Unsaved (an edit held or
queued but not yet sent), Saving…, Save failed, Draft, and Public (the small
seal with the link). Public state requires a link. The editor top bar shows the
small public mark beside the resume title when the resume is public, and a
successful publish response shows the large stamp on its own. The seal never
appears on or over a rendered resume: the homepage sample and the editor preview
show none, so neither can read as an already-public document.
