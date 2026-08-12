# Mono Print — preset rationale

Status: **Draft v1** (2026-08-12). Not approved.

Rationale for `packages/schema/templates/mono-print.json`, written against
[`../contract.md`](../contract.md), [`../tokens.md`](../tokens.md) and
[`../print.md`](../print.md). It adds no requirement to them.

**Who it is for.** Someone whose resume is read on paper after it leaves their
hands — printed on a mono laser, copied for an interview panel, scanned back to
PDF by an agency. That chain discards hue and screens tone.

## The defining decisions

**Colour is removed, not muted.** `colors.accent` is omitted, so every
accent-derived role falls back to `colors.primary` (`colors.md` §4) and links
set as body text. Black `text` is chosen for the grey it derives, not its own
21:1 — `--color-meta` is `colors.text` mixed 25 % toward the surface and is not
settable, so black is the only input that puts dates and places at `#404040`.

**Nothing is drawn.** No rule, no tint, one column. `--color-rule` is fixed at
accent mixed 60 % toward the surface, here `#999999`: a 1 px 40 % grey hairline
prints as a halftone screen 0.26 mm tall, the scale where copier screening is
least predictable — gone on one machine, heavier on the next. A tint is bimodal
under auto-exposure, and a two-column gutter is 8 mm of white that skew shears.

**Space carries what the rule would have.** `sectionGap: 26`, `entryGap: 12`,
derived `--gap-heading` = 10.4 px: a heading binds to its first entry more
tightly than entries bind to each other, and both sit inside half the section
gap. `heading.style: "uppercase"` is the second boundary signal.

**Sized against ink.** Source Sans 3's open apertures hold `c`/`e`/`s` apart as
strokes thicken; both serifs carry hairlines that break up first. At 14 px the
body sets at 10.5 pt and `--fs-meta` at 9.45 pt, above the ≈ 9 pt where counters
close; `lineHeight: 1.5` holds the 170 mm measure. Margins of 20 mm by 18 mm
clear the default by 5 mm and 3 mm: feed drift over two generations, the ≈ 94 %
auto-scale a Letter copier applies to A4. Details stack, since inline ones are
parted by 7 px of `--gap-inline`, the first white gap spread closes. Icons are
off, and `Mon YYYY` keeps a silhouette where digits and a slash fill in.

## Why both proficiency styles are `text`

Every widget style fails by losing its denominator, not its value. `bar` and
`dots` draw the unfilled remainder in `--color-track` — accent mixed 80 % toward
the surface, `#cccccc`, 1.61:1 — a role `colors.md` §4 gives no contrast target,
so the scale is unguaranteed even on screen, and a copier's tone curve takes a
20 % screen to white. A 3-of-5 bar without its track reads as full, an error in
the candidate's favour; `dots` closes its 1 mm gaps into a dash; `tag` knocks
white out of solid black, the textbook photocopy loss. So `level` never prints
(`contract.md` §5.6) — better than a widget whose scale is gone, since a reader
takes a level off whatever survives. Proficiency that must reach paper belongs
in `infoHtml`, as body text.

## Computed contrast

| Role (WCAG 2.1 on `#ffffff`, none clamped) | Colour    |   Ratio | Floor                 |
| ------------------------------------------ | --------- | ------: | --------------------- |
| body, heading, name, link, accent-solid    | `#000000` |    21:1 | 4.5:1; 3:1 name/solid |
| `--color-meta` (text 25 % → background)    | `#404040` | 10.37:1 | 4.5:1                 |
| `--color-rule` (accent 60 % → background)  | `#999999` |  2.85:1 | 1.5:1, not rendered   |
| `--color-track` (accent 80 % → background) | `#cccccc` |  1.61:1 | none, not rendered    |

## Nearest siblings

- **`high-contrast`** keeps one accent and tunes for a low-vision reader on a
  screen, where hue reproduces exactly; this preset has no `accent` key at all.
- **`ats-plain`** shares one column, no tint and `text` widgets under the
  opposite constraint: a parser reads the DOM, where small type, a rule and
  tight leading cost nothing. Base 14 and 20 mm margins are for paper alone.

## What the token space would not express

- **A black rule.** `--color-rule` is renderer-fixed, so a 1 px black hairline,
  which photocopies perfectly, is unreachable: grey or nothing.
- **An outlined empty level step**, which would survive where a `#cccccc` fill
  does not; widget geometry and `--color-track` are renderer-fixed.
- **Photo and heading icons** ([limitations items 3 and 5](../limitations.md)):
  continuous tone is the worst object in this chain and a hairline SVG the
  second; neither can be suppressed.
- **Meta size.** `--fs-meta` is pinned at 0.9 × base, so the smallest text is
  also the only grey text. Apply also resets `pageFormat`/`dateFormat`
  ([limitations item 2](../limitations.md)).
