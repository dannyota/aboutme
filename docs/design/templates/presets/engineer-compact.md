# engineer-compact

Status: **Draft v1** (2026-08-12). Not approved.

`packages/schema/templates/engineer-compact.json`, written against
`../contract.md`, `../tokens.md`, `../print.md`, ADR 0008 and ADR 0009. For
engineers read by another engineer or by a recruiter screening for a stack: that
reader answers "what do they build with, and for how long?" in ten seconds
before deciding to read prose.

## The four decisions that define it

1. **An untinted sidebar** (`surfaceTarget: "none"`, no `colors.surface`). The
   column is defined by alignment and the rule under each heading, not by
   colour: a tint needs `printBackground` to reach paper (`print.md` §6),
   repaints on every page fragment (§5), and is what a photocopier destroys
   first.
2. **Base 13 px, no smaller** — 9.75 pt, the floor `colors.md` §5 names before
   it calls print type unreadable. All other compression is spacing:
   `sectionGap` 12, `entryGap` 6, `lineHeight` 1.3, margins 11 mm × 12 mm.
3. **Bars for skills, dots for languages.** One widget for both scales reads as
   a single chart and dilutes the ten-second target. The bar is the headline
   instrument; dots are discrete and countable, plainly another measurement.
4. **A `byType` sidebar ordered for scanning**, not seniority: `skill`,
   `certificate`, `language`, `education` — shortest first, education last where
   wrapping is cheap. `main` keeps everything else at full measure.

## When a skill has no level

`skill.level` is optional and `contract.md` §5.6 requires **no widget at all**
when it is absent: no zero-length bar, no empty track, no "N/A". A bar at zero
would assert "rated, and rated lowest", which is false; `0` is explicit and
renders as zero of five against a visible track. So some rows carry a bar and
some are a bare name — correct, since no bar means unrated. `fixtures/full.json`
is that case (`Go` at 5, `TypeScript` unrated), so a snapshot pins it.

## Colour and size

One hue family, three lightness steps on pure white; off-white only costs toner.
`#1a1f24` cool graphite for body, `#10394d` dark petrol for headings, `#0d5a73`
mid petrol for bars, dots, rules and links. Headings separate by structure
(uppercase at 0.06 em, 700, 1.10 × base, a rule) and only faintly by hue:
precision, not warmth. `Source Sans 3`, sibling of Source Code Pro, sets
narrower than `Inter`, which keeps `TypeScript` on one line in the 32% sidebar.

## Contrast, computed

WCAG 2.x relative luminance on authored values, before the renderer's clamp;
derived roles mixed in sRGB per `colors.md` §4. An OKLab mix changes no verdict.

| Pair                                            | Ratio     | Floor    | Verdict |
| ----------------------------------------------- | --------- | -------- | ------- |
| body `#1a1f24` on background                    | 16.60 : 1 | 4.5      | pass    |
| meta `#53575b` (text 25% → bg) on background    | 7.29 : 1  | 4.5      | pass    |
| `#10394d` on bg, heading 14.3 px / name 26 px   | 12.25 : 1 | 4.5 / 3  | pass    |
| `#0d5a73` on bg, as link text / as bar-dot fill | 7.70 : 1  | 4.5 / 3  | pass    |
| bar and dot fill on their track `#cfdee3`       | 5.58 : 1  | 3        | pass    |
| track `#cfdee3` on background                   | 1.38 : 1  | none set | below   |
| section rule `#9ebdc7` on background            | 1.99 : 1  | 1.5      | pass    |

Nothing clamps, so published colours are the authored ones. With no surface
there is no text-on-surface pair, and `--color-on-accent` is never exercised.

## Nearest siblings

**modern-sidebar** is also two columns with a sidebar; the visible differences
are no tint behind it, base 13 px against a conventional 14–15, bars over a
track for skills with dots for languages, and 11 mm margins — enough to change
the page count. **ats-plain** shares the white ground but uses `Mon YYYY`, not
this preset's numeric dates; it is one column with no widgets, while this one
pays its density in bars no ATS can parse.

## What the token space could not express

- **The track cannot read as a denominator.** `--color-track` is fixed at the
  accent mixed 80% toward the surface, which on white caps it near 1.6 : 1 even
  for a black accent and lands at 1.38 : 1 here. It survives screen and PDF and
  dies in a grayscale photocopy, where a level-3 bar becomes a floating segment.
  A track role independent of the accent is a contract change, not a preset.
- **No sidebar width.** `--sidebar-ratio` is fixed at 32%
  ([limitations item 6](../limitations.md)), so `spacing.pageMargin.x` is the
  only lever on it — hence 11 mm, not 15 mm.
- **No wrapped skill run.** A stack reads best as a wrapped row of names, but
  skill entries stack vertically like any section: 20 skills are 20 rows.
