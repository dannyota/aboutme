# aboutme logo and seal

The logo, the public seal, and the state marks. Part of the
[visual design](../../../DESIGN.md).

## Logo

The logo is the seal. The mark is a stamp in seal red: a thick outer ring, a
hairline inner ring, and a single-story `a`, tilted −8° like the public stamp.
The wordmark is lowercase `aboutme.vn` in the text color, and only the dot
before `vn` is seal red. Strokes and a filled-circle dot draw the letters
without a font, and nothing uses a gradient or an id.

`AppLogo` renders it inline at 24, 32, or 48 px high, or as the mark alone, as
one image named “aboutme.vn”. At 24 px the hairline inner ring drops out. The
mark and the dot take `--seal-text`, so they lift to `#FF6B8A` in dark theme;
the link-preview card pins them to `#CC2649` on its white ground. Under
`forced-colors: active`, they use the text color. `public/favicon-v3.svg` is the
mark with the inner ring dropped, and it follows the system color scheme.
Home-screen and install icons are the mark on a white square; icon file names
carry a version so browsers fetch a changed icon. Brand files, banners, and the
Open Graph images, with where each is used and how to re-render them, are in
[`docs/brand/`](../../brand/README.md).

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
