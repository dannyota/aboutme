# Preset — One-Page Tight

Status: **Draft v1** (2026-08-12). Not approved.

`packages/schema/templates/one-page-tight.json` — a point in token space
(`contract.md` §1) tuned to land a fourteen-year career on one A4 sheet.

## Defining decisions

| Lever                              | Value                   | Saves vs §2 baseline |
| ---------------------------------- | ----------------------- | -------------------- |
| `layout.placement: "byType"`       | skill, language, cert   | 587 px               |
| `sectionDisplay.*.style: "text"`   | no bars, dots, tags     | 255 px (sidebar)     |
| `spacing.lineHeight` 1.4 → 1.25    | 34 body lines           | 66 px                |
| `header.detailsLayout: "inline"`   | contact row, not column | 49 px                |
| `spacing.pageMargin` 15 → 12/11 mm | see below               | 30 px                |
| `spacing.sectionGap` 16 → 10       | rule carries the break  | 28 px                |
| `spacing.entryGap` 8 → 6           | `--gap-block` 2.4 px    | 26 px                |
| `header.iconStyle: "none"`         | contact row stays two   | 16 px                |

Two columns are a space device, not a look, and `text` widgets stop the sidebar
becoming the binding flow. `certificate` is listed because a substantial resume
overflows without it; `project` and `custom` rewrap 72 → 36 characters there, so
they are not. `showRule: true` costs 4.25 px a section but drops `sectionGap` to
10; an untinted `surfaceTarget` buys nothing; `MM/YYYY` is the shortest format
with a month; `"a4"` matters because Letter is 22 mm shorter.

## Printable area on A4

A4 is 210 × 297 mm; `pageMargin {x: 12, y: 11}` leaves **186 × 275 mm** — 703 ×
1039 CSS px at 96 px/in against 680 × 1009 px at 15 mm, buying 30.2 px of
height, 1.9 body lines. Vertical is cut harder (−4 vs −3 mm) because height is
scarce; horizontal holds at 12 mm because past ~190 mm the main column passes 72
characters and readability binds. **Hardware:** most printers cannot image
within ~5 mm of the edge, worst at the trailing one, so 11 mm keeps ~6 mm of
clearance; 8 mm would gain three lines at ~3 mm, inside an inkjet's tolerance.

**Does it fit.** Document: no photo, 5 details, a 230-char summary, 4 roles of 3
bullets, 2 degrees, 2 projects, 12 skills, 3 languages, 2 certs. At
`--sidebar-ratio` 32 % columns are 448 px (~72 ch) and 225 px; the 86 px header
leaves 943 px a flow. Main runs 874 px, sidebar 587 px — **971 of 1039 px
used**, four lines spare: a fifth bullet fits, a fifth role does not.

**Type size and the floor.** `baseSizePx` 13 is **9.75 pt**: name 19.5 pt,
heading 10.7 pt, title, subtitle and body 9.75 pt, meta `max(0.9 × 13, 9)`
**8.78 pt**. The 13 px floor is `colors.md` §5's, not mine; 11 px would have
saved ~120 px — more than every spacing cut together — and was refused, because
geometry, not type size, buys this page.

## Contrast

| Role              | Value                       | Ratio   | Target |
| ----------------- | --------------------------- | ------- | ------ |
| body              | `#1c2126`                   | 16.22:1 | 4.5:1  |
| heading           | `#22303c`                   | 13.50:1 | 4.5:1  |
| meta              | text mixed 25 % → `#55585c` | 7.15:1  | 4.5:1  |
| link, accent-text | `#2f4858`                   | 9.59:1  | 4.5:1  |
| rule              | accent 60 % → `#acb6bc`     | 2.06:1  | 1.5:1  |

WCAG 2.x luminance on `#ffffff`, roles per §4. The 19.5 pt name takes the 3:1
target and gets 13.50:1; accent-solid clears 3:1 but is never painted, as `text`
widgets draw no chip. Nothing clamps, so the goldens pin the authored hexes.

## Nearest siblings

- **academic-dense** — the same 13 px base and dense intent, but a different
  grid and opposite purpose: tight over many pages versus tight to reach exactly
  one. Page two is free there, so it need not re-home sections. Both presets set
  skills and languages to `text` and render no level widgets; here, sidebar
  placement is the largest extra lever.
- **editorial-wide** — the polar opposite: it spends the page on air, this
  spends 186 of 210 mm on ink.
- **modern-sidebar** — same mechanism, other motive: it tints the sidebar as a
  signature; this leaves it untinted and admits three section types.
- **engineer-compact** — also dense, with 11 × 12 mm margins and level widgets.

## Unexpressible intent

- **An empty sidebar wastes 32 % of the width.** `print.md` §5 forbids
  stretching and no token says "collapse the sidebar if empty".
- **The fit is a claim, not enforced.** Nothing measures the page, the editor
  count is advisory, and a photo overruns the header
  ([limitations item 3](../limitations.md)).
- **`--sidebar-ratio` is fixed at 32 %.** Main binds in every fit above, so a
  template-level ratio would beat the margin token (`geometry.md` §6).
