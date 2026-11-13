# Project entry points

Status: Accepted (2026-11-13); not yet implemented. Owns the resume page's
render request fields, renderer rules, validator rules and hydration for the
links to project pages. The [projects contract](projects-contract.md) owns the
link context, the project pages and print; the
[page design](ui/project-pages.md) owns their layout.

The resume page's render request gains `projectsUrl`, the project list's
address, whenever Go serves entry points, and `projectLinks`, the same `entryId`
and `url` pairs as the public JSON's `projectLinks` at that revision, in layout
and entry order, only when at least one project is available. Go computes the
links with the project pages' own availability rule, including a name the page
can show, so a title never links to a page that answers 404.

- The renderer injects the map into every template's project section. A linked
  entry's title links to its project page instead of the entry's own `link`,
  through an anchor carrying `data-project-entry` with the entry's id. It keeps
  the title's weight (`<strong><a>`) and otherwise the link styling the template
  already gives a title with its own link. A display check proves a linked and
  an unlinked title of the same text measure the same in every template, on
  screen and at the A4 and Letter print widths, including a title that wraps.
- The page bar shows one `a.public-projects` link, "Dự án" or "Projects", left
  of Download PDF, only when the map is non-empty. Its accessible name is its
  visible label. At 320 px it may wrap with Download PDF, both stay legible, and
  each target is at least 44 px; a display check proves it. No count of projects
  appears anywhere on the public page.
- Go's resume page validator accepts `a.public-projects` once, with exactly
  `projectsUrl`, and only when links exist. Each anchor carrying
  `data-project-entry` must name a map entry and have exactly its URL. Each map
  entry with a non-empty title has exactly one, and an untitled entry none.
  Links the owner wrote in a description keep the existing rule, even when they
  point to a project page. The HTML format version rises with this change.
- The server-rendered root carries `data-project-links` whenever the request
  carried `projectsUrl`, with or without links. The browser builds the map from
  the public JSON it fetches only when that marker is present, and replaces the
  map whole on every refetch, so a project's first link appears, and a renamed,
  hidden or removed project's link disappears, with the revision that changed
  it.

The print envelope's link map follows the same rule.
`printsnapshot.WithProjectLinks` is the only code that builds version 2, and it
checks each value against the canonical origin and slug. `Marshal` checks only
each value's shape, so no caller may set the map directly.
