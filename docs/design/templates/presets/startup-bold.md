# startup-bold

Status: **Draft v1** (2026-08-12). Not approved.

One column, no rules, no fills, no accent hue. Emphasis comes from where the two
available weights are spent and from a spacing ladder with hard steps. Preset:
`packages/schema/templates/startup-bold.json`.

## Who it is for

Product, growth, and founding-team candidates writing for a ten-second skim. It
assumes an edited document: 16 px on Letter sets ~74 characters and 43 line
boxes a page, so a long history runs to more pages here than elsewhere.

## The four decisions

**1. Weight presence out of 400/700 only.** Three levers, all arithmetic.
_Base:_ the scale is renderer-fixed (`tokens.md` §3.1), so the 2.00 name and
1.15 headline hold their ratio at any size and only the absolute step moves —
13.60 px at `baseSizePx` 16 against 11.05 px at 13, and the 32 px name inks 1.5x
the area it would at 13 (32² / 26²). _Case:_ `uppercase` trades Inter's ~0.55 em
x-height for its ~0.73 em cap height, a 12.80 px band against 9.61 px at the
same 17.6 px and the same 700, and it is what parts the 1.10x label from the
1.00x title, both 700. _Family:_ Inter has a large x-height, open apertures, and
the required faces for this preset. The claim is about this choice, not a
ranking gate for the catalog.

**2. No rule, because the weight is the boundary.** `showRule: false`.
`colors.md` §5 calls the rule decorative because "the heading's size and weight
already signal the section boundary"; this takes that literally rather than
hedging with a hairline that would render `#9d9e9f` at 2.68:1.

**3. A ladder with visible steps.** 4 px inside an entry, 10 px between entries,
30 px between sections: 1 : 2.5 : 7.5. `--gap-heading` is fixed at 0.4x
`sectionGap`, so a label sits 12 px above its first entry and 30 px below the
previous section, which binds it downward. `lineHeight` 1.35 holds the line box
at 21.6 px, so a section break is 1.4 line boxes and an entry break 0.46.

**4. Two inks, no hue.** `#0b0d10` for name and labels, `#2b3038` for body — L\*
3.6 against 19.7, read as density rather than colour. `accent` is omitted, so
`--color-accent` falls back to `primary` (`colors.md` §4) and the dots land as
near-black marks on a pale track: punctuation, not a meter. `dots` on skill
_and_ language is one widget vocabulary; `iconStyle: "outline"` is line where
`solid` promised mass (`tokens.md` §3.4); `dateFormat: "YYYY"` keeps meta short.

## Contrast

WCAG 2.1 ratios on the declared hexes, before any clamp. `meta`, `track`, and
`rule` are the `colors.md` §4 derivations read as sRGB-channel mixes.

| Role                        | Pair                   | Ratio   | Floor |
| --------------------------- | ---------------------- | ------- | ----- |
| body (16 px)                | `#2b3038` on `#ffffff` | 13.27:1 | 4.5   |
| section label (17.6 px 700) | `#0b0d10` on `#ffffff` | 19.46:1 | 4.5   |
| name (32 px 700)            | `#0b0d10` on `#ffffff` | 19.46:1 | 3.0   |
| meta (14.4 px, derived)     | `#60646a` on `#ffffff` | 5.95:1  | 4.5   |
| link, accent-text           | `#0b0d10` on `#ffffff` | 19.46:1 | 4.5   |
| **filled dot on track**     | `#0b0d10` on `#cecfcf` | 12.46:1 | 3.0   |
| **filled dot on page**      | `#0b0d10` on `#ffffff` | 19.46:1 | 3.0   |

Nothing clamps, so rendered ink equals declared hex. The unfilled track is
`#cecfcf` at 1.56:1 on the page — an 80 % tint of the accent is renderer-fixed
and near 1.55:1 in any palette, so only the filled dot carries meaning. Links
are ink, not colour: hue alone would fail WCAG 1.4.1 anyway.

## Nearest siblings

- **creative-accent** leads with colour; this has no `accent` at all. It spends
  a hue where this spends cap height and gap.
- **modern-sidebar** is two columns of Be Vietnam Pro 14 with a tinted rail and
  `bar` skills; this is one full-measure column, no panel.
- **executive-band** is the real collision — one column, uppercase, rule-free —
  but its authority is a dark header fill, not 2 px of base and 30/10 gaps.
- **minimal-air** is achromatic and rule-free in the opposite register: 1.65
  leading, 36 px gaps, `text` widgets that render no level at all.

## What the token space could not express

- The 1.10x / 1.00x label-to-title split is fixed, so case carries the level
  separation; this wants the label _smaller_ than body at 0.12 em tracking.
- No weight token: a preset cannot promote `subtitle` to 700, where an employer
  name wants to sit here, nor name the entry title's colour role.
- Applying this resets `pageFormat` to `letter`
  ([limitations item 2](../limitations.md)): an A4 user ships a Letter PDF
  unwarned; containment is editor-side.
