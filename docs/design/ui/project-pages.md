# Project pages

Status: Accepted by the owner (2026-11-12); not yet implemented. The
[projects design](../projects.md#project-pages-and-entry-points) owns what a
page shows and when it exists; the [projects contract](../projects-contract.md)
owns routes, the link context, representations and print. This document owns
layout and copy.

## Order of changes

1. Server: the public snapshot with the project link context, `projectLinks` in
   public JSON, and project `PUT` through the planned-set transaction (the
   deferred advancement). No visible change.
2. Pages: Caddy and Go dispatch for `/{slug}/projects` and
   `/{slug}/projects/{project-slug}`, the frozen render request, the two Nuxt
   pages, gate, head and `noindex`. Reachable only by typing the address.
3. Entry points: the Projects link in the public page bar, title links on the
   resume page, and title links in the PDF through the new print envelope
   version. This is the release that makes pages discoverable.

## Shared frame

Both pages reuse the resume's public page: the public page bar on top, the same
color scheme and theme switch, and `.public-measure` for the column. They load
the resume's chrome typeface and tokens and none of the template's own layout,
so a project page looks the same under every template. Content is one column, at
most 44 rem wide, with the bar's inner row aligned to it.

The bar keeps the credit. Its right side shows one outlined link back to the
resume and no Download PDF (owner, 2026-11-12): "← {resume owner's name}" with
the accessible name “Về CV của {name}” or “Back to {name}'s resume”. On the
resume page the bar gains a Projects link (“Dự án” or “Projects”) left of
Download PDF when at least one project is available.

## Project list, `/{slug}/projects`

- h1: “Dự án” or “Projects”; under it, muted, the owner's name linking to the
  resume.
- One card per available project, in the resume's entry order: the name as a
  link to its page (h2), role and dates on one muted line, the summary as one
  paragraph, and up to six technology chips with “+n” (owner, 2026-11-12).
- A resume with no available project has no list page: it answers as an unknown
  resume, like every other unavailable state.

## Project page, `/{slug}/projects/{project-slug}`

In order:

1. Breadcrumb: owner's name (to the resume), then Projects (to the list).
2. h1: the project name. Under it one muted line: role, then dates.
3. Links row: Repository and Demo as outlined buttons with an external-link
   icon, `rel="noopener noreferrer nofollow"`, shown only when set.
4. Technologies: all chips, wrapping.
5. “Kết quả” or “Outcome”: the outcome text, when present.
6. “Mô tả” or “About the project”: the sanitized rich text.
7. Footer link: “Xem các dự án khác” or “See other projects”, when the resume
   has more than one available project.

Empty optional parts are omitted with their headings; a project with only a name
shows the h1 and the breadcrumb. When the page uses the project's default
language because the resume's language has no text, the content region carries
that language in `lang`, and nothing else marks it.

Dates are the library project's and show even when the resume entry hides its
own dates; hiding is per entry (owner, 2026-11-12). They follow the resume's
date format and language. The page shows no contact details, photo, or other
resume content.

## Head

Title: “{project name} · {owner's name}”; list: “Dự án · {name}” or “Projects ·
{name}”. Description: the summary, else the first 160 characters of the outcome,
else none. Canonical is the page's own address. The preview card is the resume's
stored card. `noindex` unless the resume has SEO and GEO on.

## Gate

With sign in to view on and no pass, both addresses show the resume's gate page
unchanged, and after sign-in return to the address asked for.

## Accessibility and fit

One h1 per page; cards and sections are headed regions; link text names its
destination; chips are a list. Nothing overflows horizontally from 320 px. Both
color schemes meet the contrast the public page theme requires. No script is
needed to read either page.
