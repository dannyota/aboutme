# Graduate Friendly — preset rationale

Status: **Draft v1** (2026-08-12). Not approved.

Rationale for `packages/schema/templates/graduate-friendly.json`, against
[`../contract.md`](../contract.md), [`../tokens.md`](../tokens.md),
[`../print.md`](../print.md), ADR 0008 and ADR 0009. It adds nothing to them.

**Who it is for.** One degree, one or two internships, two projects. The failure
mode is not clutter but a page that stops a third down, so every decision spends
page budget to make little content occupy the sheet.

## The four decisions

1. **Air inside the content.** `entryGap: 18` against `sectionGap: 24` is a 1.33
   ratio where the baseline is 2.0 (16/8), so a few short entries read as
   deliberate blocks, and `showRule: true` carries the boundary the gap drops.
2. **15 px, one column.** 11.25 pt, which only one column allows: `geometry.md`
   §6 tunes `--sidebar-ratio` to base 14, so 15 px wraps names in a rail.
   Margins of 20 × 16 mm leave a 170 mm measure, ≈ 86 characters, inside the
   45–90 band.
3. **Warmth from ink, not paper.** `print.md` §6 forces `printBackground`, so a
   warm `background` prints as a grey wash on a mono laser; warmth is chestnut
   titlecase headings, a terracotta accent, and a sand header `surface`.
4. **Stacked details, `dateFormat: "YYYY"`.** Stacking buys scannable contact
   rows for 4–6 lines; years stop `Jun 2025 – Sep 2025` shouting "three months".

## Does education lead? No — and no preset can make it

No token addresses a section type beyond `sectionDisplay`'s two level widgets,
so this preset cannot give Education more weight, room, or an earlier position.
ADR 0009 makes `customization.layout.sections` the sole authority for order; ADR
0008 lets a preset carry a placement rule only — `keep` preserves both arrays
verbatim, `byType` partitions keys into `sidebar` while `main` **keeps its
current relative order**. Only `PATCH /resumes/{id}/structure` reorders `main`.

The near miss, rejected: one column emits `main` then `sidebar` as one flow
(§7), so `byType` with `sidebarSectionTypes: ["work"]` would sink Work below
Education. That is a reorder wearing a placement rule's clothes, §3 says
one-column presets use `keep`, and a two-column toggle would then strand the
work history in a 32% rail. `keep` instead preserves an order the user already
set: Education above Experience survives the apply. It is an editor action, not
a template one.

## Contrast, computed

WCAG 2.1 ratios before the clamp (`contract.md` §8), roles per `colors.md` §4,
mixes channel-wise in sRGB. Header text sits on `surface`, so each row is twice.

| Role                            | Colour              | On `#ffffff` | On `#f7e8d9` |    Floor |
| ------------------------------- | ------------------- | -----------: | -----------: | -------: |
| body text                       | `#2f2a26`           |      14.19:1 |      11.82:1 |    4.5:1 |
| heading, and name at 30 px      | `#7a3b1c`           |       8.50:1 |       7.09:1 |    4.5:1 |
| `--color-meta` (text 25% → sfc) | `#635f5c`/`#615a53` |       6.32:1 |       5.65:1 |    4.5:1 |
| accent text, link, solid, chip  | `#a04f16`           |       5.79:1 |       4.83:1 | 4.5, 3:1 |
| `--color-rule` (accent 60% → s) | `#d9b9a2`/`#d4ab8b` |       1.84:1 |       1.75:1 |    1.5:1 |

Band tint 1.20:1, `--color-track` 1.34:1 / 1.31:1; neither has a floor. Burnt
orange `#b4530f` scores 5.02:1 on white but **4.18:1 on the tint**, failing the
link floor, so it darkened to `#a04f16`; warm paper `#fdf6ee` went for the print
reason above. White beats black on the accent (5.79:1 vs 3.62:1), so chips fill.

## Nearest siblings

- **`modern-sidebar`** — skills and languages stay in the flow, not a 32% rail;
  the tint is a warm band on top, not a side rail; type is titlecase chestnut at
  15 px, not uppercase marine at 14; and nothing moves on apply.
- **`minimal-air`** — the other preset built on white space; this one puts air
  between entries and re-imposes structure with rules, coloured headings and a
  band, because a thin document needs scaffolding more than quiet.

## What the token space would not express

- **Education-first**, per above, and no per-section-type styling at all.
- **A band reaching the trim**: `pageMargin` is uniform, so the tint insets on
  four sides and reads as a card, not a masthead.
- **My own picks' costs**: `YYYY` renders two 2025 internships `2025 – 2025`,
  and `titlecase` cannot lowercase a `displayName` typed `EDUCATION`.
- **Also unreachable**: photo suppression
  ([limitations item 3](../limitations.md)), icon-free headings
  ([item 5](../limitations.md)), the `pageFormat`/`dateFormat` reset that hands
  a Letter user A4 ([item 2](../limitations.md)), and page-fill targeting — past
  eight entries this bet spills to page 2.
