# Elegant Serif Two-Column — preset rationale

DRAFT v1 (2026-08-11) — not approved.

Rationale for `packages/schema/templates/elegant-serif-two.json`, written
against [`../contract.md`](../contract.md), [`../tokens.md`](../tokens.md) and
[`../print.md`](../print.md). It adds no requirement to them.

**Who it is for.** An applicant whose field still reads printed matter: a law
firm associate, a curator, an archivist, a foundation programme officer. The
rail is a panel of credentials, not a skills dashboard.

## The four decisions

1. **Alegreya at 14 px, margins 18×16 mm.** `classic-serif` set Alegreya aside
   as literary; literary is the register here. The margins leave 174 mm, so at
   the fixed 32% rail and 8 mm gutter `main` runs ≈110 mm — 66 characters
   against a one-column serif's 93. Base 14 is the rail's own limit.
2. **A warm ecru rail, credentials first.** `surfaceTarget: "sidebar"` with
   `surface: #f2ebdf` tints it 1.19:1 — warm, not the cool grey a two-column
   template defaults to, and light enough that a full-height band is not a slab
   of ink. `placement: "byType"` orders the rail `certificate`, `skill`,
   `language`: the admission outranks the skill list, so it leads.
3. **Title-case headings, ruled, one ink.** `uppercase` adds 0.06em tracking and
   makes a serif heading legal boilerplate; `titlecase` keeps reading case over
   a hairline. `colors.accent` is omitted, so claret `primary` carries heading,
   link, rule and dot alike: a second hue is what this register cannot absorb.
4. **Left letterhead, no icons, skills as prose.** Set left, the name sits on
   `main`'s axis rather than floating between both. The panel is the only
   ornament, hence `iconStyle: "none"`. `skill` is `text` — a self-scored bar is
   pseudo-precision here — and `language` keeps `dots`.

## Contrast, computed

WCAG 2.1 before the clamp (`contract.md` §8), roles per `tokens.md` §4.

| Role                          | Colour              | On `#ffffff` | On `#f2ebdf` | Floor   |
| ----------------------------- | ------------------- | -----------: | -----------: | ------- |
| body text                     | `#2b2622`           |      14.97:1 |      12.63:1 | 4.5:1   |
| heading, 15.4 px bold         | `#6b2737`           |      10.66:1 |       8.99:1 | 4.5:1   |
| name at 28 px, large text     | `#6b2737`           |      10.66:1 |            — | 3:1     |
| meta (text 25% → surface)     | `#605c59`/`#5d5751` |       6.62:1 |       6.01:1 | 4.5:1   |
| accent text, link, solid dots | `#6b2737`           |      10.66:1 |       8.99:1 | 4.5/3:1 |
| rule (accent 60% → surface)   | `#c4a9af`/`#bc9d9c` |       2.18:1 |       2.10:1 | 1.5:1   |
| dot track (accent 80% → surf) | `#e1d4d7`/`#d7c4bd` |       1.44:1 |       1.42:1 | none    |

Nothing clamps on either ground. The language dot is the only non-text mark,
8.99:1 on the tint; its 1.42:1 track nearly vanishes, so `0` reads as empty.

## The tint at a page break

`print.md` §5 requires the tint to be painted by the fragmenting element, so it
repeats rather than ending with the first fragment. The expectation:

- Page 2 resumes the band in the same 32% position, inset by the same margins,
  never bleeding to the trim and never migrating into `main`.
- The band is as tall as the sidebar flow, not the page: a rail ending
  mid-page-1 stops it at an arbitrary height, and page 2 with an empty rail
  carries **no band at all**. No token forces full height, hence the pale tint.
- `print-color-adjust: exact` and `printBackground: true` are both required
  (`print.md` §6); without either the rail is missing from the PDF alone.

## Nearest siblings

- **`modern-sidebar`** — same structure, sans. A serif page, an ecru rail not
  blue-grey, reading-case headings not tracked caps, `certificate` atop the rail
  not `skill`, skills as a list not bars, no second hue.
- **`classic-serif`** — same voice, one column. That holds one 160 mm measure
  and moves nothing on apply; this splits the page, re-homes three section types
  by type and paints one column.

## What the token space would not express

- **Small caps and italic**: the subsets ship 400 and 700 roman only
  (`tokens.md` §3.2), so an italic employer line is unreachable.
- **A rail set smaller than `main`** — `baseSizePx` is global.
- **A survivable tint**: one column degrades the sidebar to the main treatment
  (`contract.md` §7) and the panel vanishes.
- **An icon-free page**: headings still render `iconKey` (`contract.md` §9.5).
- Apply resets `pageFormat` and `dateFormat` (`contract.md` §9.2).
