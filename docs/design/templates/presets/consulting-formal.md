# Consulting Formal — preset rationale

Status: **Draft v1** (2026-08-12). Not approved.

Rationale for `packages/schema/templates/consulting-formal.json`; bare § refs
are to [`../contract.md`](../contract.md). See also
[`../tokens.md`](../tokens.md) and [`../print.md`](../print.md).

## Who it is for

Someone applying into a strategy firm or in-house strategy team, where the
resume is screened in a minute against a shape the reader already knows, then
printed into an interview packet. Unsurprising is the point.

## Defining decisions

| Decision                                            | Why                                                                                                                                                                                                                                    |
| --------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `heading.style: "titlecase"` with `showRule: true`  | Title case at zero tracking reads as a phrase — "Professional Experience" — not a shouted field label, and the rule carries the boundary the tight section gap gives up. The signature pairing, and the one a sibling should not copy. |
| `header` left, inline, `iconStyle: "none"`          | A centred letterhead reads formal-legal; name and headline flush left over one contact line read as a document title.                                                                                                                  |
| `columns: 1` with `placement: "keep"`               | Applying moves nothing, and a document already using a sidebar renders it below `main` (§7). `sidebarSectionTypes` is absent, as `keep` requires; the omission is deliberate.                                                          |
| `surfaceTarget: "none"`, no `colors.surface`        | One white surface. `primary` and `accent` are two steps of one navy — headings darker, links and rules brighter — so the hierarchy needs no second hue.                                                                                |
| `sectionGap: 15`, `entryGap: 9`, `lineHeight: 1.45` | Section overhead is 21 px against 30.8 px for `classic-serif` — a third less per section, which is where the extra content on the sheet comes from.                                                                                    |
| `dateFormat: "MM/YYYY"`, both `sectionDisplay` text | Dates become a column of numerals, compact enough for that density; and no bar, dot, or chip, because a proficiency infographic is the wrong register here.                                                                            |

## Letter, and what the choice costs

Letter is deliberate: this reader prints into US trays, where the driver scales
or clips A4. Under ADR 0008 the apply is wholesale, so an A4 user who tries this
preset ships a Letter PDF with no signal from the preset layer
([limitations item 2](../limitations.md)) — 5.9 mm wider and 17.6 mm shorter
here, so the page re-paginates, trading ~3.5 lines of height for one point of
measure. `pageFormat` has no `"keep"`, so containment is the editor's job;
picking `a4` would only hide the cost behind a template that prints wrong for
its readers. The 25 mm side margins absorb a flip back to `a4`, which still
gives a 160 mm measure.

## Colour and size

`Inter` is the enum's neutral grotesque, its closest thing to deck typography
and the one sans neither nearest sibling uses. Base 13 px sets `--fs-name` to
19.5 pt, a name that identifies rather than announces; Inter's large x-height
keeps the body legible at that base, and its wider advance holds the 165.9 mm
measure to ≈ 93 characters, just past the 45–90 band. Ratios are pre-clamp.

| Pair                                     | Colours             |   Ratio | Floor |
| ---------------------------------------- | ------------------- | ------: | ----- |
| body text on background                  | `#33383d`/`#ffffff` | 11.84:1 | 4.5:1 |
| heading (`colors.primary`) on background | `#14304f`/`#ffffff` | 13.41:1 | 4.5:1 |
| `--color-meta` (text 25% → background)   | `#666a6e`/`#ffffff` |  5.45:1 | 4.5:1 |
| `--color-accent-text`, links             | `#1c4e80`/`#ffffff` |  8.57:1 | 4.5:1 |
| `--color-accent-solid`                   | `#1c4e80`/`#ffffff` |  8.57:1 | 3:1   |
| name at 26 px, large text                | `#14304f`/`#ffffff` | 13.41:1 | 3:1   |
| `--color-rule` (accent 60% → background) | `#a4b8cc`/`#ffffff` |  2.04:1 | 1.5:1 |

`--color-meta` binds, not the body colour, and earlier than it looks: any
neutral grey lighter than about `#4a4a4a` clears 4.5:1 itself while its mixed
meta falls below. No text-on-surface or non-text row exists.

## Nearest siblings

- **`classic-serif`** — Inter against its Roboto Serif, title case against its
  uppercase, flush left against centred, Letter against A4, `MM/YYYY` against
  `Mon YYYY`, 15/9 spacing against 22/12; navy in every heading, not just links.
- **`government-formal`** — the closer: it shares Letter, `MM/YYYY`, one column,
  a left header, `text` displays, and a 25 mm side margin. What a reader sees
  first separates them — uppercase against title case, `#000000` against navy
  over grey, a stacked contact block against one inline line, 33.6 px of section
  overhead against 21 px. An airy black form against a dense navy one.

## What the token space would not express

- **Dates flush right.** Title left and dates hard right on one baseline is the
  defining consulting entry; `meta` is a renderer-fixed slot (§5.2).
- **Real title case.** `capitalize` capitalises every word ("Awards And
  Honors"), cannot lowercase a `displayName` typed "EXPERIENCE", and is
  locale-sensitive — deterministic only through the pinned `lang`.
- **Proficiency in words.** `text` never spells the level, as no vocabulary for
  0–5 exists (§5.6), so "French — Fluent" is unreachable.
- **A header rule, or an icon-free page.** `showRule` covers section headings
  only, and `iconKey` still renders despite `iconStyle: "none"`
  ([limitations item 5](../limitations.md)).
