# Modern Sidebar

Status: **Draft v1** (2026-08-12). Not approved.

Rationale for `packages/schema/templates/modern-sidebar.json`, written against
[`../contract.md`](../contract.md), [`../tokens.md`](../tokens.md),
[`../print.md`](../print.md), and ADRs 0008 and 0009.

## Who it is for

The applicant who wants the layout a recruiter already knows how to read: work
history down the wide column, a tinted rail of skills, languages and
certificates beside it. The safe first pick, tuned for legibility at a glance
rather than personality.

## The four decisions

1. **The sidebar is a painted panel, not just a narrow column.**
   `surfaceTarget: "sidebar"` with `surface: #e3ecf0` tints the rail 1.20:1
   against the page — a visible panel, light enough not to print as a grey slab.
   `placement: "byType"` sends `skill`, `language` and `certificate` there and
   nothing else: those three are the types whose entries are a name plus a level
   or one date, so they survive the fixed 32% measure. Rich text stays in
   `main`.
2. **Be Vietnam Pro at 14 px, leading 1.5.** `geometry.md` §6 tunes
   `--sidebar-ratio` to "a two-word skill name at base 14 without wrapping", so
   14 px is the largest base that keeps the rail honest. Be Vietnam Pro over
   Inter because its Vietnamese diacritics are drawn for stacked marks, which a
   narrow column needs, and it is warmer without reading as decorative.
3. **Marine headings, one teal accent.** Body is neutral slate `#1c2b33`,
   headings a deeper bluer `#0e3f52`, and one cyan-teal `#0f6b7d` carries every
   accent role — rule, link, level bar, tag fill. Generic blue-600 `#2563eb` was
   rejected on measurement: 4.49:1 on this tint fails the 4.5:1 floor.
4. **Uppercase headings with a hairline rule, margins 14 × 13 mm.** The rule
   derives as accent mixed 60% toward its surface, so it reads as a faint teal
   hairline that ties the two columns together rather than boxing them in.
   Margins run 1–2 mm inside the renderer default of 15 mm, buying about two
   lines a page without shrinking type. `dateFormat: "Mon YYYY"` and
   `pageFormat: "a4"` are the same least-surprise reflex.

## Contrast, computed

WCAG 2.1 ratios for the preset's own palette before the renderer clamp
(`contract.md` §8), derived roles computed as `colors.md` §4 defines them.

| Role                        | Colour    | On `#ffffff` | On `#e3ecf0` | Floor |
| --------------------------- | --------- | ------------ | ------------ | ----- |
| body text                   | `#1c2b33` | 14.56:1      | 12.15:1      | 4.5:1 |
| heading (`primary`)         | `#0e3f52` | 11.34:1      | 9.46:1       | 4.5:1 |
| name at 28 px (large text)  | `#0e3f52` | 11.34:1      | —            | 3:1   |
| meta (text 25% → surface)   | derived   | 6.46:1       | 5.85:1       | 4.5:1 |
| accent text and link        | `#0f6b7d` | 6.14:1       | 5.12:1       | 4.5:1 |
| accent solid (bars, dots)   | `#0f6b7d` | 6.14:1       | 5.12:1       | 3:1   |
| on-accent (white on solid)  | `#ffffff` | 6.14:1       | 6.14:1       | 4.5:1 |
| rule (accent 60% → surface) | derived   | 1.87:1       | 1.79:1       | 1.5:1 |

Meta resolves to `#556066` / `#4e5b62`, the rule to `#9fc4cb` / `#8eb8c2`. Every
floor is met with no clamping. White beats black on the accent (6.14:1 to
3.42:1), so tag chips fill rather than outline.

## Nearest siblings

- **`nordic-muted`** — the other preset likely to tint a surface. Mine keeps
  near-black body text at 14.56:1 and puts saturated teal on one element at a
  time, so the page reads as neutral with an accent, not as an all-over wash.
- **`engineer-compact`** — the other two-column sans with skills in the rail.
  Mine spends the page budget on air: 20 px section gaps, 10 px entry gaps, 1.5
  leading, and a painted rail. It fits fewer entries per page and never crowds.

## What the token space would not express

- **No bleed.** `pageMargin` is uniform, so the tint is inset on all four sides
  and cannot run to the trim; the panel reads as a card, not a structural band.
- **Sidebar width is renderer-fixed at 32%**, so a long certificate title wraps
  and the only lever is `baseSizePx`. That constraint set the base size.
- **Heading treatment is global.** Inside a 32% rail a hairline under every
  heading is more structure than it needs, and there is no per-column token.
- **The tint is not survivable.** Toggling to one column degrades the sidebar to
  the main treatment (`contract.md` §7) and the signature disappears.
- **A level bar's empty half nearly vanishes on the tint** — `--color-track`
  derives at 1.32:1 there against 1.44:1 in main. The filled half still carries
  the meaning at 5.12:1, and no token can raise the track alone.
