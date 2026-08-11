# minimal-air

One column, no rules, no fills, no accent: the composition is the text block and
the space around it. Preset: `packages/schema/templates/minimal-air.json`.

## Who it is for

Someone whose material is already edited — a senior IC, designer, or researcher
writing for a reader, not a parsing gate. It trades density for composure: about
38 lines per A4 page against roughly 49 in ats-plain.

## The four decisions

**1. The page frame.** `pageMargin` 30 mm x 24 mm, double the renderer's 15 mm
default, leaving a content column of 150 x 249 mm. Inter at `baseSizePx` 15 sets
about 73 characters on that measure (567 px at a ~0.52 em average advance).
Family, base, and margin are one decision, not three: a neutral grotesque holds
an even grey value, and even tone is all a page without rules or fills has.

**2. Separation by space alone.** `showRule: false`, so the spacing ladder is
the only section signal: 6.4 px inside an entry, 14.4 px heading to first entry,
16 px between entries, 36 px between sections (0.43 / 0.96 / 1.07 / 2.4 em). A
section break is 2.25x an entry break and 1.45x the 24.75 px line box, and the
2.5:1 asymmetry around each label is what binds it to the entries below.

**3. Ink and paper.** No `accent`, no `surface`, `surfaceTarget: "none"`,
background `#ffffff`. `#14161a` sets headings and the name, `#24272c` (near 88%
black) the body, since full black over a 73-character measure at 1.65 leading
reads hard. Links fall back to `primary` (`tokens.md` §4), so they are ink
rather than colour. A tinted background would be a full-page ink fill.

**4. Nothing that draws.** `sectionDisplay` is `text` for skill and language,
which renders no widget at all (`contract.md` §5.6), and `header.iconStyle` is
`"none"`. The header is left-aligned with inline details: one flush-left axis is
the only line in the design. `dateFormat: "Mon YYYY"` keeps slashes off the
page. The cost is that a skill's 0–5 `level` then renders nowhere.

## Contrast

WCAG 2.1 ratios, computed on the declared hexes before any clamp.

| Role                   | Pair                   | Ratio   | Floor |
| ---------------------- | ---------------------- | ------- | ----- |
| body                   | `#24272c` on `#ffffff` | 14.98:1 | 4.5   |
| heading (16.5 px bold) | `#14161a` on `#ffffff` | 18.11:1 | 4.5   |
| name (30 px bold)      | `#14161a` on `#ffffff` | 18.11:1 | 3.0   |
| meta (derived)         | `#5b5d61` on `#ffffff` | 6.60:1  | 4.5   |
| link, accent-text      | `#14161a` on `#ffffff` | 18.11:1 | 4.5   |
| accent-solid           | `#14161a` on `#ffffff` | 18.11:1 | 3.0   |

Nothing clamps, so rendered ink equals declared hex. The meta row reads
`tokens.md` §4's 25 % mix toward the surface as an sRGB-channel mix; in linear
light the same mix of even `#000000` gives 3.35:1, unreachable against 4.5:1.

## Nearest siblings

- **editorial-wide** — the closest of the twenty: also one column, rule-free, at
  1.65 leading. It is a warm serif page (Alegreya 16 on cream `#faf8f3`, brown
  accent, normal-case headings); minimal-air is achromatic Inter on pure white
  with uppercase labels and a 36 px section gap against its 32 px.
- **ats-plain** — black on white and widget-free like this one, but it rules
  every heading and stacks the contact block, and at 18 mm margins with 20/10
  spacing it runs a 174 mm measure and about 49 lines a page.
- **nordic-muted** — two columns with a tinted `#f1f3f5` sidebar panel,
  blue-grey ink, outline contact icons, and dot level widgets. Every one of
  those is a mark; minimal-air puts no mark on the page at all.

## What the token space could not express

- The type scale is renderer-fixed at 2.00 / 1.15 / 1.10 / 1.00 / 0.90 x base,
  so the section label is bold at 1.10x body where this design wants 0.85x at
  0.12 em tracking; `heading.style` offers 0.06 em and nothing else.
- `--gap-heading` is hard-coupled at 0.4 x `sectionGap`, so a decisive section
  break necessarily loosens each label's bond to its own first entry.
- No date rail: meta stays inline, though a one-column minimal resume wants
  dates in a left column.
- A photo (`contract.md` §9.3) and a section `iconKey` (§9.5) still render;
  `header.iconStyle` governs the contact row only. Either puts the one graphic
  mark on a mark-free page.
