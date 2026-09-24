# Template presets

The twenty presets in `packages/schema/templates/<id>.json` are the implemented
data. Each file carries a one-line `description` of its intent. A preset is a
point in the token space of the [template contract](../contract.md); it cannot
add a token or renderer behavior.

## Rules every preset follows

- Its own colors meet the floors in
  [colors.md §5](../colors.md#5-accessibility-floor) before the renderer clamp,
  for body, heading, meta, link, accent, and on-accent roles, on the page
  background and on any tinted surface it paints.
- It ships the artifacts in
  [contract.md §8](../contract.md#8-conformance--what-a-preset-must-ship).
- It stays visibly distinct from its nearest sibling at a glance. The table
  below names the sibling and the difference a reader sees first.
- It claims only what a mechanism supports. `ats-plain` makes the
  text-extraction claims; no other preset does. The shared limits in
  [limitations.md](../limitations.md) (no photo suppression, no global icon
  switch, fixed sidebar width, A4 seed only) apply to every preset.

## Distinctness

| Preset               | Nearest sibling     | Visible difference                                                                                         |
| -------------------- | ------------------- | ---------------------------------------------------------------------------------------------------------- |
| `academic-dense`     | `one-page-tight`    | Tight over many pages, not tight to one: one column, 13 px, year-only dates, section gap 3.3× entry gap    |
| `ats-plain`          | `minimal-air`       | Normal-case ruled headings, stacked contact block, pure black, no icons or level widgets                   |
| `classic-serif`      | `elegant-serif-two` | One full-measure serif column with centred letterhead and ruled uppercase headings; nothing moves on apply |
| `consulting-formal`  | `government-formal` | Navy over grey, title-case headings, one inline contact line, tighter grid                                 |
| `creative-accent`    | `designer-tag`      | One vermilion carries name, headings, rules, links, and skill tags over a tinted header wash               |
| `designer-tag`       | `creative-accent`   | Bone paper, centred nameplate, no rules; olive chips for skills and languages are the only fills           |
| `editorial-wide`     | `minimal-air`       | Alegreya 16 on cream at book proportions: wide margins, open leading, normal-case headings                 |
| `elegant-serif-two`  | `modern-sidebar`    | Serif page with an ecru rail, claret title-case headings, certificates at the top of the rail              |
| `engineer-compact`   | `modern-sidebar`    | Untinted sidebar, 13 px base, skill bars and language dots, 11 mm margins, numeric dates                   |
| `executive-band`     | `startup-bold`      | Filled navy masthead with a large name; the pages below stay plain, uppercase-led, and ruleless            |
| `government-formal`  | `mono-print`        | One ink, stacked contact block, ruled uppercase headings, 25 mm margins, `MM/YYYY` dates                   |
| `graduate-friendly`  | `modern-sidebar`    | Warm tinted identity band on top, 15 px title-case headings, skill tags in the flow, wide entry spacing    |
| `high-contrast`      | `mono-print`        | 18 px base and one AAA blue accent that degrades to greyscale                                              |
| `international-lang` | `modern-sidebar`    | Languages lead an untinted rail as bars, normal-case headings, tinted header band, year-only dates         |
| `minimal-air`        | `editorial-wide`    | Achromatic Inter on white with uppercase labels; no rules, fills, or marks                                 |
| `modern-sidebar`     | `nordic-muted`      | Near-black body with one saturated teal accent and a tinted rail of skills, languages, and certificates    |
| `mono-print`         | `high-contrast`     | Pure black, no accent, no rules, tints, or widgets: every distinction survives a photocopier               |
| `nordic-muted`       | `minimal-air`       | Blue-grey inks, a faint tinted sidebar panel, and hairline rules                                           |
| `one-page-tight`     | `academic-dense`    | Two columns with cut margins and an untinted sidebar of three section types, tuned to one A4 sheet         |
| `startup-bold`       | `executive-band`    | Largest base in the set, uppercase black labels instead of rules, no accent, monochrome dot proficiency    |
