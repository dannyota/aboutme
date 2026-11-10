# Projects

Status: Proposed (2026-11-10), awaiting owner approval.
[ADR 0029](../adr/0029-account-project-library.md) records the decision and the
[roadmap](../plans/roadmap.md#v09-show-the-work) the order. This design covers
the library, selection, project pages and viewer entry points. GitHub connection
and evidence get their own design.

## What a person does

1. Opens Projects in the workspace and writes a project once: name, role,
   technologies, outcome, links, dates and a description, in Vietnamese, English
   or both.
2. Adds it to a resume. The resume gets a compact project entry, prefilled, that
   they may shorten for that audience.
3. Publishes the resume as today. Each linked, visible project now has a page,
   and the resume shows viewers how to reach it.

Nothing is shown by default. A project appears only on resumes it was added to.

## Library

A library project belongs to one account and is private.

| Field                      | Rule                                                                                    |
| -------------------------- | --------------------------------------------------------------------------------------- |
| `id`                       | Server UUIDv7                                                                           |
| `slug`                     | Unique per account, same grammar as a resume slug, 2 to 40 characters                   |
| `dates`                    | Same date range as a resume entry                                                       |
| `repositoryUrl`, `demoUrl` | Optional, `https` only                                                                  |
| `technologies`             | Ordered list of plain strings                                                           |
| `text`                     | Map of language (`vi`, `en`) to name, role, summary, outcome, and rich-text description |
| `revision`                 | Compare-and-swap counter, as on a resume                                                |

Rich text passes the same sanitizer and size bound as resume rich text. A
project needs a name in at least one language to be saved and is otherwise
draft-permissive. Numeric limits (projects per account, technologies per
project, field lengths) are set in [budgets](budgets.md) with this change.

Renaming a project slug changes its page address on every resume at once; the
old address returns 404. No tombstone is kept, because the address is scoped to
a resume slug that already has one.

## Selection

The resume document stays the only authority for what a resume shows.

- A project entry gains an optional `projectId`. An entry with it is linked; one
  without it behaves as today.
- Adding a library project to a resume inserts a linked entry in the resume's
  project section, creating the section if needed, with `title`, `subtitle`,
  `link`, `dates` and `description` prefilled from the library text in the
  resume's language, or left empty when that language has no text.
- The entry's text is the resume's own. "Refresh from library" overwrites it on
  request; nothing overwrites it otherwise.
- Order, hiding and removal use the existing entry operations. One library
  project is linked at most once per resume.
- Deleting a library project clears `projectId` from every entry that names it,
  in the same transaction, and leaves the entry text.

This needs a new resume document version for the optional field
([ADR 0004](../adr/0004-resume-document-contract.md)). No stored document is
rewritten.

## Project pages

| Route                             | Serves                                          |
| --------------------------------- | ----------------------------------------------- |
| `/{slug}/projects`                | The resume's available projects, in entry order |
| `/{slug}/projects/{project-slug}` | One project                                     |

A project is available on a resume when the resume is live, the linked entry is
visible, and the library project has a name in the resume's language. Any other
state returns the same 404 as an unknown resume.

- **Gate.** Each request checks the live state, sign in to view and availability
  in one read. No project page is cached past the rules the resume page already
  has, so unpublish, hide, unlink and delete take effect at once
  ([ADR 0010](../adr/0010-public-artifact-revocation.md)).
- **Sign in to view.** A project page gates exactly as its resume page and
  stores nothing about the viewer
  ([ADR 0022](../adr/0022-viewer-privacy-and-counting.md)).
- **Indexing.** Project pages follow the resume's SEO and GEO setting and are
  otherwise `noindex`. They are never listed on `/showcase`.
- **Representations.** HTML only. No Markdown, JSON or PDF of a project page.
  The link preview reuses the resume's stored card.
- **Content.** Name, role, dates, technologies, outcome, description, repository
  and demo links, and a link back to the resume. No contact details beyond what
  the resume page already shows; no account data.
- **Counting.** A project page view is not counted in v0.9.

The page uses the public page bar and theme of its resume. Its layout is
specified with the UI work, not here.

## Viewer entry points

- **Public page.** The page bar shows "Projects" when at least one project is
  available. A linked, available entry's title links to its project page; the
  entry's own `link` field keeps its meaning.
- **PDF and print.** When the resume is live and has an available project, the
  projects section prints one line with the address `aboutme.vn/{slug}/projects`
  under its heading, as a link in the PDF. The owner can turn the line off per
  resume. Templates define its type and spacing with the template work, and the
  pagination checks cover it.
- **Editor.** The preview shows the line and the links as the public page will.

## Agents

MCP gains tools to list, read, create, update and delete library projects and to
link a project to a resume, under the existing scopes. A tool cannot publish,
attach evidence or connect an account. Until reviewed changes ship (v0.10), an
agent edit to a library project of a published resume is public at once, as an
agent edit to the resume is today; the MCP guide says so.

## Data, export and deletion

- New table for library projects, owned by a user and cascading with the
  account.
- The account export includes the library. Account deletion removes it.
- Project text is user content: it never enters logs or telemetry.

## Checks

- A project page is served only in the available state; each transition out of
  it (unpublish, sign in to view on, hide, unlink, delete, slug rename, language
  text removed) returns 404 or the gate on the next request.
- A resume with unlinked project entries renders and prints exactly as before.
- Deleting a library project leaves every resume valid.
- No route returns a project without a live resume slug in the path.
- Two accounts may use the same project slug.

## Open points for the owner

1. Limits: 30 projects per account and 20 technologies per project
   (recommended), or other values.
2. The printed line: on by default (recommended), or off by default.
