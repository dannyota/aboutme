# International Languages

Rationale for `packages/schema/templates/international-lang.json`, against
[`../contract.md`](../contract.md), [`../tokens.md`](../tokens.md),
[`../print.md`](../print.md) and ADRs 0008/0009. For the candidate applying
abroad, where languages are a qualification the reader screens on.

## The four decisions

1. **Languages lead.** `sidebarSectionTypes` is `language, skill, certificate`,
   so ADR 0008 heads the rail with languages. Only `language` gets a widget —
   `bar` against `skill: "text"` — the page's one non-text mark.
2. **Year-only dates.** `MM/YYYY` reads month/day by North American habit;
   `Mon YYYY` is English-only, since `contract.md` §5.4 pins `Mon` to a fixed
   table, not `Intl`. `YYYY` is the enum's one unambiguous, neutral value.
3. **Headings are not transformed.** `tokens.md` §3.3 makes `text-transform`
   locale-sensitive under the root's `lang` — a hazard in Turkish (`i` → `İ`)
   and German (`ß`). `showRule: true` replaces that boundary. Leading is 1.55
   because Vietnamese stacks a tone mark over a vowel mark.
4. **The band is on the header, not the rail.** `surfaceTarget: "header"` at
   1.17:1 groups name, country and phone; the rail stays white so the bar's
   track keeps full contrast. `iconStyle: "none"`: pictograms are not universal.

## Fonts and diacritics

- **`font.family` is not a coverage lever** (OBSERVED).
  `docs/plans/phase-3/task-05-self-hosted-fonts.md` pins one subset list and one
  cmap test for all five, so Vietnamese coverage is equal in each.
- That list is Latin-1 plus Vietnamese, **not** Latin Extended-A (OBSERVED):
  Polish loses ą ć ę ł ń ś ź ż, Turkish ğ ı İ ş, French œ; Cyrillic, Greek and
  CJK are absent, so a language named in its own script has no glyph.
- No binaries exist to check (OBSERVED): `apps/web/app/assets/fonts/` is absent.
  INFERRED: `unicode-range` matches the subset, so an out-of-range codepoint
  falls to the print container's own fonts — a mismatched face or `.notdef`.
- Be Vietnam Pro is named on an INFERRED claim: of the five it is the one whose
  upstream design centres a two-mark stacking script. Unverifiable here, and if
  wrong the preset loses nothing, coverage being equal by subset.

## Page format

`a4`: ISO 216 is standard outside North America, and someone applying abroad
more often applies _to_ an A4 market. ADR 0008's replace is wholesale, so
**applying this preset overwrites the user's `pageFormat`** — `contract.md`
§9.2's failure, worst here because this preset's user is likeliest to have set
Letter on purpose. The containment is an editor warning, not a preset field.

## Contrast, computed

| Role                        | Colour    | On `#ffffff` | On `#f0ede7` | Floor     |
| --------------------------- | --------- | ------------ | ------------ | --------- |
| body text                   | `#2a2724` | 14.85:1      | 12.71:1      | 4.5:1     |
| heading, and name at 26 px  | `#191614` | 18.01:1      | 15.41:1      | 4.5:1     |
| meta (text 25% → surface)   | `#5f5d5b` | 6.56:1       | 6.03:1       | 4.5:1     |
| accent text, link, and bar  | `#5a4a3a` | 8.48:1       | 7.26:1       | 4.5 / 3:1 |
| rule (accent 60% → surface) | `#bdb7b0` | 1.99:1       | 1.92:1       | 1.5:1     |

WCAG 2.1 on the preset's own palette before the clamp, roles derived per
`tokens.md` §4. Every floor is met with no clamping. Track is accent 80% toward
the surface, `#dedbd8`: **fill against track 6.15:1, fill against page 8.48:1**,
both over 3:1. `level` is optional on `languageEntry` and `skillEntry` alike —
neither carries a `required` array in `$defs` — so a bar is empty two ways,
which §5.6 requires to differ: absent renders no widget, `0` an unfilled track,
a hairline at 1.38:1 no token raises.

## Nearest siblings

- **`modern-sidebar`** — same family, same three rail types; mine inverts the
  widgets, leads with `language`, drops the casing transform, moves the tint to
  the header, centres it, prints years, and is taupe.
- **`engineer-compact`** — the other untinted two-column sans, but a density
  design: 1.3 leading, 12 px gaps, skills first. Mine is the same base at 1.55
  and 18 px. No other preset uses `bar` for `language`.

## What the token space would not express

- **No per-widget colour, no track floor.** Bar and rule are both accent
  derivatives, so a louder bar is a louder rule and unearned steps stay faint.
- **The band cannot bleed** — `pageMargin` is uniform, so the tint reads as a
  card — and **nothing survives one column**: toggling drops the rail and
  languages fall below all of `main` (`contract.md` §7).
