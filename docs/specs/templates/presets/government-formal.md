# Government Formal — preset rationale

DRAFT v1 (2026-08-11) — not approved.

Rationale for `packages/schema/templates/government-formal.json`, written
against [`../contract.md`](../contract.md), [`../tokens.md`](../tokens.md) and
[`../print.md`](../print.md); it adds no requirement to any of them.

## Who it is for

Someone submitting into a process rather than applying to a person: a vacancy
announcement, a defence contractor, a regulated-role intake at a bank or a
hospital. The reader is a panel scoring against published criteria, often from a
photocopy, and completeness beats compression.

## Defining decisions

1. **Stacked contact block.** `align: "left"` + `detailsLayout: "stacked"` +
   `iconStyle: "none"`: one contact datum per line at the top left. Enumeration
   is the register, and no pictogram stands in for a label a photocopy can lose.
2. **One column, `placement: "keep"`.** Announcements prescribe the order in
   which qualifications are addressed, so a `byType` rule would re-home sections
   on apply and break that silently. `sidebarSectionTypes` is `[]`, inert here.
3. **Letter, 25 mm margins, MM/YYYY.** 25 mm is one inch to the nearest
   millimetre, the standard instruction. Numeric dates need no month table and
   make a gap readable to the month without fabricating precision: a `{y}` with
   no `m` still renders as a year (`contract.md` §5.4). At base 14 the 166 mm
   measure runs to about 90 characters, offset by `lineHeight: 1.5`.
4. **One ink.** `primary` and `text` are both `#000000` on `#ffffff`, with
   `surfaceTarget: "none"` and no `colors.surface`. Black is a lever, not a
   default: `--color-meta` is `colors.text` mixed 25 % toward the surface, so
   only `#000000` puts the slot carrying the dates at 10.37:1, not 7.69:1.
5. **No proficiency widgets.** Both `sectionDisplay` styles are `text`: an
   unlabelled 4-of-5 bar is the ambiguity `MM/YYYY` avoids, and the schema
   defines no vocabulary for 0–5 (`contract.md` §5.6). Source Sans 3 is the same
   argument in type — a humanist text sans where the enum's serifs read as
   journal or courtroom.

## The omitted accent, and the numbers

`colors.accent` is the only optional colour: `$defs.customization.colors`
requires `primary`, `text` and `background` only. Omitting it is valid, and
`useResumeStyles` falls back to `colors.primary`, never a brand colour
(`tokens.md` §4), so links and `--color-accent-solid` are `#000000`,
`--color-rule` is the 60 % mix `#999999`, and `--color-track` the 80 % mix.

Computed WCAG 2.x ratios, before clamping; each clears its floor.

| Pair                                     | Colours             |   Ratio | Floor |
| ---------------------------------------- | ------------------- | ------: | ----- |
| body text on background                  | `#000000`/`#ffffff` | 21.00:1 | 4.5:1 |
| heading (`colors.primary`) on background | `#000000`/`#ffffff` | 21.00:1 | 4.5:1 |
| `--color-meta` (text 25 % → background)  | `#404040`/`#ffffff` | 10.37:1 | 4.5:1 |
| links, `--color-accent-solid` (fallback) | `#000000`/`#ffffff` | 21.00:1 | 4.5:1 |
| `--color-rule` (primary 60 % → bg)       | `#999999`/`#ffffff` |  2.85:1 | 1.5:1 |

The name at 28 px clears its 3:1 floor at 21.00:1. No text-on-surface row exists
(`colors.surface` unset), and `--color-track` (`#cccccc`, 1.61:1) carries
nothing. No colour carries information, so WCAG 1.4.1 holds by construction.

## Nearest siblings

- **`mono-print`** — if it is monochrome we share the palette outright, and a
  black-and-white page is not distinctness on its own. What separates them is
  Letter at 25 mm, the stacked contact block, and 24/14 gaps that spend space.
- **`ats-plain`** — identical on every axis a parser sees (one column, one ink,
  `text` widgets, a common sans), opposite on every axis a human sees: ATS work
  compresses for keyword density per page, this takes inch margins and base 14.
- **`classic-serif`** runs the same ruled-uppercase skeleton in serif, on A4 and
  centred; **`consulting-formal`** keeps an accent and a tighter grid.

## What the token space would not express

- **A continuation header.** These submissions repeat the name and "page N of M"
  on every sheet; `@page` margin boxes are empty by rule (`print.md` §2).
- **Suppressing a photo.** Many such processes forbid photographs; nothing in
  `customization` hides one that is present (`contract.md` §9.3).
- **Field labels.** A form labels its fields; entry slots are unlabelled.
- **A rendered level.** `language` has no body field: its section is bare names.
- Apply resets `pageFormat` and `dateFormat` (§9.2), so an A4 user gets Letter.
