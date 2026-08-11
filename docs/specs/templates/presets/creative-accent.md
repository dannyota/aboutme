# Creative Accent

## Who it is for

Marketing, communications, and design-adjacent applicants, for whom a flat grey
CV reads as no point of view rather than as restraint. It is still read in a
stack of documents, so colour is structural here: it marks the identity block,
the section boundaries, and the skill set, and is nowhere an ornament. Written
against [`../contract.md`](../contract.md), [`../tokens.md`](../tokens.md),
[`../print.md`](../print.md), ADR 0008, and ADR 0009.

## The decisions

1. **One ink to read, one to navigate by.** `colors.text` is a warm near-black;
   `colors.primary` **equals** `colors.accent`, so the name, every section
   heading, the rules, the links, and the skill tags are one literal hex
   (`tokens.md` §4). Hierarchy _is_ the colour, not a highlight over one.
2. **The header is a block, not a line.** `surfaceTarget: "header"` with
   `colors.surface` at a 7% tint of the same hue gives the identity block its
   own ground — the one structural device the token space offers, and what lets
   the typography below stay plain.
3. **One column, `placement: "keep"`, wide side margins.** These resumes lead
   with a written profile and prose bullets, which the renderer-fixed 32%
   `--sidebar-ratio` truncates. `pageMargin` 22/18 mm frames the text block;
   `sectionGap` 20 / `entryGap` 10 / `lineHeight` 1.5 keep it vertically
   efficient. `"keep"` also cannot produce the empty second column of ADR 0008.
4. **Tags for skills, plain text for languages.** The chip is the only saturated
   fill on the page, and spending it twice halves it. `header.iconStyle: "none"`
   follows the same budget: accent contact icons would be decoration.

## The accent under the OKLCH clamp

`#c23a14` is `oklch(0.546 0.179 35.2)`, a deep vermilion, and **the clamp is a
no-op for it** in all four colour roles on both surfaces — every step count is
zero, so the page shows the hex that was picked. The margin is deliberate but
small: the binding case is the heading on the header tint at 4.84:1, and
lightening past `L ≈ 0.562` (`#c8401b`) drops it below 4.5:1 and hands the clamp
control of the colour. A brighter accent would not fail but split: `#f97316`
clamps to `#cd4a00` as text and `#f26d04` as fill.

`--color-on-accent` resolves to `#ffffff` at 5.37:1 on the fill; `#000000`
scores 3.91:1 and loses. The outline fallback of `tokens.md` §4 cannot fire here
— or anywhere: the better of black and white is never worse than 4.58:1. Base 15
px (11.25 pt) sits above the 13 px legibility discussion of `tokens.md` §5; at
22 mm side margins on A4 the measure is 166 mm, about 84 characters.

## Contrast

Preset values before clamping; meta and rule are the §4 mixes, in sRGB.

| Role (value)                       | on `#ffffff` | on `#fff0eb` | Target |
| ---------------------------------- | ------------ | ------------ | ------ |
| body text `#1f1a17`                | 17.24        | 15.53        | 4.5    |
| meta `#575351` / `#57504c`         | 7.60         | 7.12         | 4.5    |
| heading, name, link `#c23a14`      | 5.37         | 4.84         | 4.5    |
| tag fill `#c23a14`                 | 5.37         | 4.84         | 3.0    |
| tag text `#ffffff` on that fill    | 5.37         | 5.37         | 4.5    |
| section rule `#e7b0a1` / `#e7a795` | 1.89         | 1.83         | 1.5    |
| header surface `#fff0eb`           | 1.11         | —            | none   |

## Nearest siblings

- **designer-tag** spends the chip as a general aesthetic; here one section has
  chips, languages stay plain text, and skills sit in the main column.
- **startup-bold** buys attention with scale and weight; this holds 15 px in a
  humanist face and spends colour on structure alone.
- **executive-band** also tints a header, as a dark field with reversed text;
  this is a 1.11:1 wash carrying dark text, a ground rather than a band.

## What the token space would not express

- One `--color-heading` role covers the name and the section headings, so a deep
  tone for the name over a brighter tone for headings — this palette's first
  version — is unreachable. One hex is the repair, not the intent.
- The tint paints inside the page margins; with no bleed token the header block
  cannot run to the paper edge.
- Chip geometry, heading letter-spacing, and the rule's fixed 60% mix are
  renderer-owned, so the rule can never read as a full-strength accent bar.
- `iconKey` has no global off switch (`contract.md` §9.5), so section icons
  render beside the coloured headings whatever the preset wants.
