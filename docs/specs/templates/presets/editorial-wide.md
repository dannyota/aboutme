# Editorial Wide — `editorial-wide`

DRAFT v1 (2026-08-11) — not approved.

One column of Alegreya at 16 px, inside 35 mm margins, at 1.65 leading, no rules
and no widgets. For senior individual contributors, writers, and researchers
whose CV is read in full. Wrong for a first-pass screen: it costs about one page
in three against a conventional setting, deliberately.

## Defining decisions

- **35 mm sides, 22 mm head and foot.** The text block is two thirds of the A4
  width — the Van de Graaf canon's horizontal proportion, chosen for measure and
  not for the look of the white space. That canon's ~50 mm head and foot buys a
  fragmenting CV nothing, so this takes its width and refuses its height.
- **Base 16 px (12 pt), leading 1.65.** Book size, and every renderer size is a
  multiple of it, so `--fs-meta` lands at 14.4 px, not near its 9 px floor. The
  leading is a notch above book-normal because entries are short blocks, not
  chapters, and that space is what stops a description from reading as a form.
- **Gaps of 2 em and 1 em** (32 / 16 px). Heading-to-body is the renderer's
  `0.4 × sectionGap` = 12.8 px, so space above a heading is 2.5× the space below
  and the heading binds downward to its section. That binding replaces the rule.
- **`heading.style: normal`, `showRule: false`.** `uppercase` adds 0.06 em
  tracking and reads as a form label; `titlecase` capitalizes every word and is
  locale-sensitive. The heading is the user's own words in the text face.
- **`text` for skills and languages** renders no widget at all (`contract.md`
  §5.6) — a name plus whatever prose sits in `infoHtml`. A five-dot meter exists
  to be scanned; nothing here is. `Mon YYYY` follows: "Mar 2019 – Present" is
  prose, `MM/YYYY` is a field.
- **Warm paper, two inks.** `#faf8f3` kills the glare of `#ffffff`; headings sit
  in a warmer, denser umber-black. Cost: `print-color-adjust: exact` (`print.md`
  §6) makes a home printer lay a full-page tint. Extraction is unaffected.

## Measure

- 210 mm − 2 × 35 mm = **140 mm** content width; × 96 ÷ 25.4 = **529.1 px**.
- Average advance in running English for a text serif is ≈ 0.45–0.50 em, and
  Alegreya is on the economical side of that: **7.2–8.0 px** at 16 px base.
- 529.1 ÷ 8.0 = **66.1**; ÷ 7.52 (0.47 em) = **70.4**; ÷ 7.2 = **73.5**.

The band, **66–74 characters**, sits inside the comfortable 45–75 range near its
upper end, which is why the leading is 1.65 and not 1.4. It assumes an advance
ratio rather than measuring the shipped subset, so a golden render is the check.
On Letter, 146 mm of content moves the narrow end to ~77; vertically, 253 mm at
26.4 px leading holds ~36 lines against ~51 for a 14 px / 1.4 / 15 mm setting.

## Contrast (WCAG 2.x, before any renderer clamp)

| Role                     | Value     | On `#faf8f3` | Floor |
| ------------------------ | --------- | ------------ | ----- |
| `--color-body` (text)    | `#24211c` | 15.11:1      | 4.5:1 |
| `--color-heading`        | `#3a2c1e` | 12.69:1      | 4.5:1 |
| `--color-meta` (25% mix) | `#5a5752` | 6.78:1       | 4.5:1 |
| `--color-accent-text`    | `#7a4a24` | 6.99:1       | 4.5:1 |
| `--color-accent-solid`   | `#7a4a24` | 6.99:1       | 3:1   |

Nothing clamps. `--color-rule` would be `#c7b2a0` at 1.92:1 but never paints,
and `--color-track` is unused because both level styles are `text`.

## Nearest siblings

- **minimal-air** also spends whitespace, but evenly; here it is asymmetric —
  margins and section gaps, while the entry block stays tight and the ink heavy
  at 15:1. A sparse page reads fast; this one is built to be read slowly.
- **elegant-serif-two** shares the serif and probably the warm palette, but two
  columns halve the measure and make the page a layout.
- **classic-serif** is the conventional serif resume: default margins, tighter
  leading, rules under headings. The distance is geometric.

## Unexpressible intent

1. **A display face for headings.** `font.family` is one enum for the document,
   so "serif headings" means a serif document.
2. **Asymmetric page margins.** `pageMargin` is `{x, y}`, so head and foot are
   equal; the tradition this borrows from has the deeper foot.
3. **A first-line indent or hanging date column.** Entry anatomy is fixed
   (`contract.md` §5.2) and no token addresses indentation.
4. **A named identity.** `customization` stores no template id (`contract.md`
   §9.1): after apply, nothing separates these values from a user who reached
   them by hand — which is, in the end, the point.
