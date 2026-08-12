# Preset: Nordic Muted

Status: **Draft v1** (2026-08-12). Not approved.

Preset: `packages/schema/templates/nordic-muted.json`.

## Who it is for

A mid-length CV in a field that reads restraint as competence — product design,
UX, research, architecture, editorial — with a real skills and languages list.

## Defining decisions

**Muted is a chroma decision, not a luminance one.** Body text holds 11.51:1,
higher than most palettes reach; what drops is saturation. Graphite `#333a42`,
slate `#2f4557`, and fjord blue `#4a6b80` differ by hue and small luminance
steps, not by a jump to black. So this is a colour statement, not its absence:
three inks and a third plane, where an air-driven preset has one ink and paper.

**The page stays white; the tint is a shape.** A tinted `background` paints a
rectangle inset by the page margin, which on paper reads as a printing fault;
the same edge around a column reads as a panel. So the tint is `colors.surface`
`#f1f3f5` with `layout.surfaceTarget: "sidebar"` — a tax of ~0.5:1 on the whole
sidebar, making the panel, not the page, the binding ground for every colour.

**Hierarchy by tracking and hairline, not by darkness.** `heading.style` is
`uppercase` with `showRule: true`, the rule at 1.78:1: faint, above the
decorative floor, never the only boundary signal. `sectionDisplay.skill` is
`text`, since a long skill list drawn as bars is noise; `language` is `dots`, a
quiet rhythm over few rows — and dots mean something, so they answer to 3:1.

**Placement rule** (ADR 0008): `byType` over
`["skill", "language", "certificate"]` — enumerable credentials in the panel,
narrative in `main`, no section keys, so it applies to an unseen document. Base
13 px, `lineHeight` 1.55, margins 18 × 16 mm, tight vertically because that is
the page budget.

## Computed contrast

Per `colors.md` §4; mixes are sRGB lerps; values are pre-clamp. All pass.

| Role                          | Value (page / panel)  | Page    | Panel   | Min   |
| ----------------------------- | --------------------- | ------- | ------- | ----- |
| body text, `colors.text`      | `#333a42`             | 11.51:1 | 10.35:1 | 4.5:1 |
| heading, `colors.primary`     | `#2f4557`             | 9.95:1  | 8.95:1  | 4.5:1 |
| name, 26 px ≥ 24 px           | `#2f4557`             | 9.95:1  | —       | 3:1   |
| meta, text mixed 25% → ground | `#666b71` / `#62686f` | 5.38:1  | 5.06:1  | 4.5:1 |
| accent as text or link        | `#4a6b80`             | 5.67:1  | 5.09:1  | 4.5:1 |
| accent-solid, language dots   | `#4a6b80`             | 5.67:1  | 5.09:1  | 3:1   |
| rule, accent mixed 60% → grd  | `#b7c4cc` / `#aebdc6` | 1.78:1  | 1.73:1  | 1.5:1 |

Were meta instead mixed toward the page globally, `#666b71` still clears the
panel at 4.83:1. No minimum binds the widget track (`#dbe1e6`, 1.32:1) or the
panel itself (1.11:1 on the page); no information depends on seeing either.

## Colours changed to pass

- **Accent `#5b7f96` → `#4a6b80`.** The first choice failed as link text on both
  grounds (4.27:1 page, 3.84:1 panel; min 4.5:1); `#547a91` still failed in the
  panel at 4.13:1, and `#4f7288` passed by 0.11 at 4.61:1, too thin to defend.
  `#4a6b80` clears both at 5.67:1 and 5.09:1, 31% lower in luminance.
- **Text `#3a424b` → `#333a42`.** Not the text but its derived meta: mixed 25%
  toward the panel it reached only 4.63:1, so the clamp would have darkened the
  muted grey this preset exists to produce. `#333a42` puts meta at 5.06:1.

## Nearest siblings

- **minimal-air**, closest: it reaches calm by removing — one ink, one plane, no
  rule; this reaches calm by placing — two columns, a panel, a hairline, dots.
- **high-contrast**: opposite in chroma, not in ratio. Its separation is maximal
  and neutral; here it is nearly as high, but every ink is hued.
- **modern-sidebar**: same skeleton; tinted, bar-rendered skills, low-chroma
  inks.

## Unexpressible intent

- The panel should run full column height on every page; the renderer paints the
  tint on the fragmenting flow (`print.md` §5), so it stops where content stops.
- Rule colour is not settable: it is the accent mixed a fixed 60% toward the
  ground. Label tracking and panel width have no tokens either.
- `spacing.pageMargin` overrides `print.md` §2's 15 mm fallback. This preset's
  explicit 18 × 16 mm value therefore controls its page geometry now.
