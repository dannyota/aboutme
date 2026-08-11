# Executive Band

Rationale for `packages/schema/templates/executive-band.json`. For a senior
leader whose name and title are the headline — C-suite, partner, VP — read by
one person deciding in the top third of page one.

## The four decisions

1. **The band is the heading ink at page scale.** `colors.surface` and
   `colors.primary` are one value, `#16273d`, under
   `layout.surfaceTarget: "header"`, so band-against-paper equals
   heading-against-paper at 15.09:1.
2. **One column, `placement: "keep"`.** The spine is a chronology of roles, and
   a sidebar demotes it. `"keep"` is not a no-op: an already-split document
   renders `main` then `sidebar` full width (`contract.md` §7).
3. **Uppercase headings, no rule.** Letterspaced 700 caps are already a
   horizontal event; a hairline under each would leave the plate one device
   among many. Spacing separates instead: 24 / 12 / 9.6 / 4.8 px, a halving
   cascade.
4. **A typographic masthead, not an iconographic one.** `header.align: "left"`
   puts the name's left edge on the axis of every heading below it, which makes
   an inset plate read as architecture; `iconStyle: "none"` and
   `detailsLayout: "inline"` keep the band one line of facts deep.

## Why the band is inset, and what page two looks like

Chromium paints no element background into the empty `@page` margin boxes
(`print.md` §2), so no `spacing.pageMargin` yields a bleed and `y: 0` would only
push body text to the sheet edge. The band is therefore a deliberate plate: 18
mm of white beside it, 14 mm above, the tighter top anchoring it to the head of
the sheet. Page two has no band and cannot have one — no running-header token
exists — so it opens with 14 mm of white, and continuity comes from what never
depended on the fill: text edges where the plate's edges were, uppercase
headings in the same `#16273d`, the same 24 px rhythm. `.resume-header` is
`break-inside: avoid`, so the plate never splits. Without
`print-color-adjust: exact` and `printBackground: true` (`print.md` §6) the fill
drops, and since this header's text is light-on-dark, page one degrades to a
nearly invisible name, not merely a plainer one.

## Colour and size

Base 15 px (11.25 pt) clears `tokens.md` §5's 13 px advisory, puts `--fs-name`
at 30 px / 22.5 pt, and shortens the measure: 174 mm of content is ~88
characters at 15 px against ~95 at 14 px. Brass, not blue: the accent shows only
in links and dots, never competing with the plate. Ratios below use WCAG 2.x and
the `tokens.md` §5 clamp (OKLCH walk, step 0.005, hue and chroma held).

| Where | Role                      | Input     | Clamped   | Ratio   | Floor |
| ----- | ------------------------- | --------- | --------- | ------- | ----- |
| Page  | heading (`primary`)       | `#16273d` | unchanged | 15.09:1 | 4.5   |
| Page  | body (`text`)             | `#1f2937` | unchanged | 14.68:1 | 4.5   |
| Page  | meta (text 25% → surface) | `#575e69` | unchanged | 6.54:1  | 4.5   |
| Page  | link / accent-text        | `#8a5a1e` | unchanged | 5.90:1  | 4.5   |
| Page  | accent-solid (dots)       | `#8a5a1e` | unchanged | 5.90:1  | 3.0   |
| Band  | name (heading)            | `#16273d` | `#7b8faa` | 4.56:1  | 4.5   |
| Band  | headline (body)           | `#1f2937` | `#828e9f` | 4.54:1  | 4.5   |
| Band  | details (meta)            | `#1d2838` | `#818fa2` | 4.59:1  | 4.5   |
| Band  | link (accent-text)        | `#8a5a1e` | `#b6834a` | 4.55:1  | 4.5   |

The name is 30 px at 700 — large text, needing 3:1 — and gets 4.56:1 because
`--color-heading` clamps to 4.5 anyway; the details beside it are 13.5 px at
400, small text at 4.5:1, and reach 4.59:1. Under an `--color-on-accent`-style
on-surface rule instead, the band takes white at 15.09:1 (black 1.39:1). No
`colors.primary` serves both unclamped (4.5:1 needs luminance ≥ 0.263 on the
band, ≤ 0.183 on white), so the per-surface clamp is load-bearing, not a safety
net.

## Nearest siblings

- **consulting-formal** — also senior, also restrained. Mine fills a plate and
  drops every rule; that one keeps a white page divided by rules.
- **startup-bold** — also fills and sets a large name; mine is near-black navy
  and brass, airy at 24 px, where a bold startup fills bright and packs tight.

## What the token space would not express

- **No on-band ink** — the preset names the fill, not the text on it: the
  difference between a minimum-passing `#7b8faa` and a brilliant white name.
- **No band depth**, no bleed, no photo suppression (`contract.md` §9.3); apply
  also resets `pageFormat`/`dateFormat` (§9.2), so a Letter user ships A4.
