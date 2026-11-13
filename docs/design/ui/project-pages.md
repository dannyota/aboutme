# Project pages

Status: Accepted by the owner (2026-11-12). The two pages are implemented; the
entry points are not. The
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
color scheme, and `.public-measure` for the column. They load the resume's
chrome typeface and tokens and none of the template's own layout, so a project
page looks the same under every template. The list is one column, at most 44 rem
wide; the project page is at most 59 rem wide with a facts column beside its
text. The bar's inner row aligns to the page's column.

The bar keeps the credit. Its right side shows one outlined link back to the
resume and no Download PDF (owner, 2026-11-12): "← {resume owner's name}" with
the accessible name “Về CV của {name}” or “Back to {name}'s resume”. On the
resume page the bar gains a Projects link (“Dự án” or “Projects”) left of
Download PDF when at least one project is available.

## Project list, `/{slug}/projects`

- h1: “Dự án” or “Projects”; under it, muted, the owner's name linking to the
  resume.
- Projects are rows in one bordered card, in the resume's entry order, not a
  grid of cards (owner, 2026-11-12, after comparing both layouts): the name as a
  link to its page (h2) with role and dates muted on the same line, the summary
  as one paragraph, and up to six technology chips with “+n”. Below 40 rem, role
  and dates move under the name.
- A resume with no available project has no list page: it answers as an unknown
  resume, like every other unavailable state, once any sign in to view gate is
  passed.

## Project page, `/{slug}/projects/{project-slug}`

The owner chose text with a facts column on 2026-11-12, after comparing both
layouts. Above both columns, in order:

1. Breadcrumb: owner's name (to the resume), then Projects (to the list).
2. h1: the project name.
3. The summary as a lead paragraph, when present.

Then two columns from 56 rem: the text, and beside it a facts column 17.5 rem
wide. Below 56 rem the facts column comes first, full width, then the text.

Text column:

1. “Kết quả” or “Outcome”: the outcome text in a bordered panel, when present.
2. “Mô tả” or “About the project”: the sanitized rich text.
3. Footer link: “Xem các dự án khác” or “See other projects”, when the resume
   has more than one available project.

Facts column, one bordered panel:

1. Repository as a filled button and Demo as an outlined button, each with an
   icon, `rel="noopener noreferrer nofollow"`, shown only when set. With only a
   demo link, Demo is the filled button. When the resume entry's own `link`
   differs from both, a third outlined button “Liên kết” or “Link” carries it
   (2026-11-13), so a link the owner wrote on that resume is not lost when its
   title links here.
2. “Vai trò” or “Role”, and “Thời gian” or “Dates”, as a definition list.
3. “Công nghệ” or “Technologies”: all chips, wrapping.
4. GitHub evidence, when that feature ships ([design](../github-evidence.md));
   until then the panel ends at Technologies and shows no placeholder.

Empty optional parts are omitted with their headings. A project with no link,
role, dates or technology has no facts column and its text takes the full width;
a project with only a name shows the h1 and the breadcrumb. When the page uses
the project's default language because the resume's language has no text, the
content region carries that language in `lang`, and nothing else marks it.

Dates are the library project's and show even when the resume entry hides its
own dates; hiding is per entry (owner, 2026-11-12). They follow the resume's
date format and language. The page shows no contact details, photo, or other
resume content.

## Head

Title: “{project name} · {owner's name}”; list: “Dự án · {name}” or “Projects ·
{name}”. Description: the summary, else the first 160 characters of the outcome,
else none. Canonical is the page's own address. The preview card is the resume's
stored card. `noindex` unless the resume has SEO and GEO on.

Decided 2026-11-13: the owner's name is the resume's full name, or its effective
page title when the name is blank. `og:title`, `og:description` and `og:url` are
this page's; the image, its alt text and `og:locale` are the resume's. The
favicon is the resume's. A language whose text has a blank name falls back to
the default language, so the page always has an h1. Colors are the resume's
color scheme only, never its template accent. The pages have no script and no
live stream; they change on the next request after the resume's revision
advances.

## Gate

With sign in to view on and no pass, both addresses show the resume's gate page,
which looks unchanged; its provider links carry the page asked for, and after
sign-in the person returns to that page under the resume's current address. No
join invite is shown on project pages.

## Accessibility and fit

One h1 per page; cards and sections are headed regions; link text names its
destination; chips are a list. Nothing overflows horizontally from 320 px. Both
color schemes meet the contrast the public page theme requires. No script is
needed to read either page.
