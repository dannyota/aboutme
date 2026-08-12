# Classic Serif — preset rationale

Status: **Draft v1** (2026-08-12). Not approved.

Rationale for `packages/schema/templates/classic-serif.json`, written against
[`../contract.md`](../contract.md), [`../tokens.md`](../tokens.md), and
[`../print.md`](../print.md). It adds no requirement to them.

**Who it is for.** A reader who expects a document, not a designed artefact: a
lawyer, an academic, a civil-service applicant. Nothing on the page exists to be
noticed — no fill, no tint, no band, no proficiency widget.

## The four decisions

1. **One column, `placement: "keep"`.** Sections span the full text width and
   applying the preset moves none of them; a document that already uses a
   sidebar keeps its arrangement, rendered below `main` (`contract.md` §7).
   `sidebarSectionTypes` is absent, as `keep` requires; the omission is
   deliberate.
2. **Centred letterhead.** `header.align: "center"` with
   `detailsLayout: "inline"` and `iconStyle: "none"` gives name, headline, then
   one centred line of contact values. This triple is the preset's signature.
3. **Ruled uppercase headings.** `heading.style: "uppercase"` with
   `showRule: true` is the standing convention for a formal CV, and the reason
   the page needs no colour to mark a section boundary.
4. **No fills, one near-black accent.** `surfaceTarget: "none"` and no
   `colors.surface`: one white surface. The accent is a navy that reads as ink
   at body size, present only so links differ from text.

## Colour and size

`Roboto Serif` over `Alegreya`, whose calligraphic detailing belongs to a
literary template. Base 13 px (9.75 pt) answers a renderer constraint:
`--fs-name` is fixed at twice the base, so base 14 would set the name at 21 pt,
louder than this register allows, where 13 px gives 19.5 pt. It still does not
read small: an x-height of ≈ 0.53 em yields ≈ 6.9 px, against ≈ 6.6 px for the
11 pt Times this audience knows.

Margins of 25 × 20 mm leave a 160 mm measure (≈ 93 characters, above the 45–90
band for prose) and 257 mm of text height on A4. `lineHeight: 1.5` compensates
for that measure; resume copy is short blocks, not running text.

Computed WCAG 2.1 ratios, before clamping. Every pair clears its floor, so
`useResumeStyles` returns these hexes unchanged. There is no text-on-surface row
(`colors.surface` unset) and no meaningful non-text row: both `sectionDisplay`
styles are `text`, so no bar, dot, or tag ever renders.

| Pair                                     | Colours             |   Ratio | Floor |
| ---------------------------------------- | ------------------- | ------: | ----- |
| body text on background                  | `#23262c`/`#ffffff` | 15.16:1 | 4.5:1 |
| heading (`colors.primary`) on background | `#12151a`/`#ffffff` | 18.29:1 | 4.5:1 |
| `--color-meta` (text 25% → background)   | `#5a5c61`/`#ffffff` |  6.69:1 | 4.5:1 |
| `--color-accent-text`, links             | `#1f3864`/`#ffffff` | 11.62:1 | 4.5:1 |
| `--color-accent-solid`                   | `#1f3864`/`#ffffff` | 11.62:1 | 3:1   |
| name at 26 px, large text                | `#12151a`/`#ffffff` | 18.29:1 | 3:1   |
| `--color-rule` (accent 60% → background) | `#a5afc1`/`#ffffff` |  2.21:1 | 1.5:1 |

## Nearest siblings

- **`academic-dense`** — same serif register and ruled uppercase headings; the
  difference is density. `lineHeight: 1.5` and a 22 px section gap put white
  space between sections, so a mid-career CV runs to a second page here rather
  than being compressed onto one.
- **`elegant-serif-two`** — the other serif template. This one holds a single
  column at the full 160 mm measure and moves nothing on apply; that one splits
  the page and re-homes sections into a sidebar by type.

## What the token space would not express

- **Small caps**, the classic heading treatment. `heading.style` offers only
  `uppercase`/`titlecase`/`normal`, so every letter sets at cap height with the
  renderer's fixed 0.06 em tracking — heavier than intended.
- **A rule under the header.** `showRule` governs section headings only, so the
  other half of a real letterhead is unreachable.
- **An icon-free page.** `iconStyle: "none"` clears the contact line, but
  section headings still render the document's `iconKey`
  ([limitations item 5](../limitations.md)).
- **Rule weight and colour, and justified text.** 1 px, 0.25 em below, and
  accent mixed 60 % toward the surface are renderer-fixed, so the rule can only
  be pale blue-grey; justification is not a token at all.
- Applying the preset resets `pageFormat` to `a4` and `dateFormat` to `Mon YYYY`
  ([limitations item 2](../limitations.md)); a Letter user gets A4 silently.
