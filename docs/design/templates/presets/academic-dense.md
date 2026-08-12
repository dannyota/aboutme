# Academic Dense

Status: **Draft v1** (2026-08-12). Not approved.

Preset `packages/schema/templates/academic-dense.json`, for a researcher whose
CV runs four to eight pages and is read for content, not admired.

**13 px body, not lower.** The schema permits 10 px (7.5 pt) unchecked
([limitations item 7](../limitations.md)), but `colors.md` §5 puts
print-legibility practice at 13 px (9.75 pt) and a preset must not ship users
past that line; a CV is read for twenty minutes, not glanced at for seconds.
Scale: name 26 px/19.50 pt, heading 14.30/10.73, body 13/9.75, meta 11.70/8.78;
leading 1.3 is 9.75 on 12.68 pt, below which return sweep fails at this measure.

**Section gaps are cheap, entry gaps expensive.** Over 12 sections and 110
entries, `sectionGap` 20 costs 240 px (0.23 page) and `entryGap` 6 costs 660 px
(0.64 page). Entries must not merge, though: half-leading is 3.9 px, so entries
sit 9.9 px apart against 3.9 px inside one, and derived gaps keep 2.4<6<8<20.

**Margins reshape the frame, not shrink it.** `pageMargin` x 20 / y 12 makes the
A4 block 170 × 273 mm: 61.0 lines per page against 59.7 at the default 15 mm,
but ≈ 99 characters per line against ≈ 105: 3.6% less capacity. Size and leading
repay it at 18% more lines than 14 px/1.4. 99 exceeds the 45–75 ideal, tolerable
since entries run one to three lines; 20 mm left is staple clearance.

**Two inks, no widgets, years only.** `colors.accent` is omitted, so
`--color-accent` falls back to `colors.primary`; `colors.text` and
`colors.primary` remain distinct. Hierarchy also uses size, weight, uppercase,
and rules, all surviving the mono laser printer a CV is printed on.
`surfaceTarget` is `"none"`: a tint is ink with no information. `sectionDisplay`
is `text`, rendering no widget (`contract.md` §5.6); `dateFormat` is `YYYY`,
month precision being noise on a hundred meta lines; and the header is left,
`inline`, `iconStyle: "none"`, saving four lines.

## Multi-page breaks

`placement` is `"keep"` with `columns: 1`; `sidebarSectionTypes` is omitted
because ADR 0008 binds that list to `"byType"`. One column also sidesteps
`print.md` §5's fragmenting grid, making the tight entry rhythm safe. The
chained heading and first-entry-header avoid rules are load-bearing: a 40-entry
Publications heading never strands while its section splits freely. At 6 px a
break between entries resembles one inside an entry; the ruled heading is the
recovery signal.

## Contrast, computed

WCAG 2.1 ratios on `#ffffff`, sRGB per-channel mixes; body 9.75 pt and meta 8.78
pt both take the 4.5:1 small-text floor.

| Role                                         | Value     | Ratio | Floor |
| -------------------------------------------- | --------- | ----- | ----- |
| `--color-body` (`colors.text`)               | `#1a1e22` | 16.76 | 4.5   |
| `--color-heading` (`colors.primary`)         | `#12293f` | 14.84 | 4.5   |
| `--color-meta` (text mixed 25% to surface)   | `#535659` | 7.39  | 4.5   |
| `--color-accent-text` / `--color-link`       | `#12293f` | 14.84 | 4.5   |
| `--color-accent-solid`                       | `#12293f` | 14.84 | 3     |
| `--color-rule` (accent mixed 60% to surface) | `#a0a9b2` | 2.38  | 1.5   |

The name at 19.5 pt (26 px ≥ 24 px) clears its 3:1 floor at 14.84. All pass
before clamping, so `colors.md` §5's clamp never fires.

## Nearest siblings

`one-page-tight` also runs small and tight but budgets a page count where this
budgets a reading rate: `sectionGap` is 3.3× `entryGap` so landmarks survive
page 4, and `baseSizePx` stops at 13 where a one-pager may go lower.
`classic-serif` also sets one column dark on white, but a serif at normal size
with generous leading — three pages against six. With hinting and LCD text
disabled for determinism (`print.md` §7), serif hairlines lose contrast at 9.75
pt on screen, where a PDF CV is read; Source Sans 3 also runs narrower than
Inter.

## Not expressible in token space

1. **Page numbers and per-section breaks** — `print.md` §2 keeps `@page` margin
   boxes empty, so six pages carry no "Name — 3 of 7" and nothing can start
   Publications on a fresh sheet.
2. **Sub-headings and numbered or hanging-indent entries** — all are
   renderer-owned. Links remain visible on a mono print because the renderer
   underlines every inline link, independent of `--color-link`.
3. **Meta size and measure** — `--fs-meta` is pinned at 0.9 × base though the
   year is this CV's scanning key, and `pageMargin` sets margins, not line
   length, so Letter widens the measure to ≈ 103 characters.
