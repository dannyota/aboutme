# Designer Tag

DRAFT v1 (2026-08-11) — not approved. Preset
`packages/schema/templates/designer-tag.json`, written against `../contract.md`,
`../tokens.md` and `../print.md`; bound by ADR 0008 and ADR 0009.

## Who it is for

Visual designers and art directors, whose CV is read as layout first. It assumes
one or two pages and a skills list worth showing as a set, not a ranking.

## Defining decisions

- **Chips for skills and languages both.** Two bands of colour at different
  heights, not one; the second answers the first and gives the lower half of the
  sheet something to sit on.
- **The chips are the only fills.** No rules (`showRule: false`), no tint
  (`surfaceTarget: "none"`). Nothing else is painted, so the bands alone mark
  where the page changes register.
- **Centred nameplate over one centred credit line.** `center` + `inline` +
  `outline` icons: repeated glyph-and-value pairs, rhyming with the chips below.
- **One column, `placement: "keep"`.** A 32 % sidebar wraps a band into a ragged
  two-word stack; full measure lets it run.
- **Two olives and an ink.** Colour appears only where structure is: `#2f3a1e`
  headings, `#54692a` chips, neutral `#242220` on `#f4f1ea` bone.
- **A 2.4 px vertical grid at base 15 px.** `sectionGap` 24 and `entryGap` 12
  derive `--gap-heading` 9.6 and `--gap-block` 4.8: every vertical interval is a
  multiple of 2.4 px. Margins 26 × 22 mm leave a 158 mm measure.
- **Year-only dates**, for short even meta lines. Cost: a user on `MM/YYYY`
  loses month precision on apply, unwarned (`contract.md` §9.2).

## Chip geometry I would change

`--tag-padding: 0.15em 0.5em` and the 3 px radius are renderer-fixed; all three
changes below are contract changes, not preset values (`contract.md` §8).

1. Vertical padding `0.15em` → `0.35em`. At base 15 that is ~2.2 px above the
   text against 7.5 px beside it: a highlight behind a word, not an object.
2. Radius 3 px → 0. Square chips on bone read as specimen swatches; 3 px is a
   screen-UI convention borrowed onto print.
3. A chip-row gap token. Nothing names the space between chips or between the
   wrapped rows of a band; without a row gap distinct from the column gap, a
   band's second row lands at the wrong interval.

## Contrast

WCAG 2.x, against this preset's own hexes, before any clamp.

| Pair                                               | Ratio | Floor | Result |
| -------------------------------------------------- | ----- | ----- | ------ |
| body `#242220` on paper `#f4f1ea`                  | 14.05 | 4.5   | pass   |
| heading `#2f3a1e` on paper                         | 10.67 | 4.5   | pass   |
| name at 30 px `#2f3a1e` on paper                   | 10.67 | 3     | pass   |
| meta `#585652` (text mixed 25 % to paper) on paper | 6.49  | 4.5   | pass   |
| chip text `#ffffff` on chip fill `#54692a`         | 6.12  | 4.5   | pass   |
| chip fill `#54692a` on paper                       | 5.43  | 3     | pass   |
| link `--color-accent-text` `#54692a` on paper      | 5.43  | 4.5   | pass   |

Nothing clamps, so chips and links stay one olive. `--color-rule` and
`--color-track` are unused. Incidental finding: `tokens.md` §4's outline
fallback is unreachable — contrast(black) × contrast(white) = 21 for every
colour, so the larger is always ≥ √21 = 4.58 and a `tag` chip always fills.

## Nearest siblings

| Sibling         | Shares                   | Visible difference                                                                   |
| --------------- | ------------------------ | ------------------------------------------------------------------------------------ |
| creative-accent | tag chips for skills     | languages chipped too, so two bands; centred header; no rules; bone paper, not white |
| classic-serif   | centred header, 1 column | sans not serif; unruled headings; the only ink besides text is the chips             |
| minimal-air     | one column, low ornament | air is that template's subject; here restraint buys two saturated bands as the event |

## Unexpressible intent

- The chip is a **level** widget, so an entry with no `level` renders as a bare
  name (`contract.md` §5.6). A section mixing rated and unrated entries gives a
  ragged mix of chips and plain words, and no preset controls that.
- No token sorts chips, so band ragging follows entry order.
- Heading colour and chip fill want one hue at two lightness stops. As two
  hexes, editing `primary` alone silently breaks the family.
- `pageMargin.y` is symmetric; this page wants more head than foot.
