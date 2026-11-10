# 0029: Account project library and project pages

Status: Proposed (2026-11-10). The owner chose the direction on 2026-11-10
([roadmap](../plans/roadmap.md#owner-decisions)); this record and the
[projects design](../design/projects.md) await his approval before code.

## Context

A project on a resume is a print-sized entry: title, subtitle, link, dates and
description. It cannot carry a role, technologies, an outcome or evidence, and a
person with three resumes retypes the same project in each.

The owner decided that a project is written once, that each resume chooses which
projects it shows, that a project gets a page under the resume, and that the
resume shows viewers where those pages are.

Three accepted decisions bound the design. Public URLs belong to a resume and a
person has no public page (ADR 0003). The resume document is self-contained and
is validated, exported and rendered as one aggregate (ADR 0004). Every public
reuse passes the live-state gate and unpublish takes effect at once (ADR 0010).

## Decision

1. **Library.** Each account has a private project library. A library project
   holds the detail a page needs: a project slug, role, technologies, outcome,
   repository and demo links, dates, and a long description, with text held per
   language. It has no public URL of its own and is never listed across
   accounts.
2. **Selection lives in the resume.** A resume shows a library project when it
   holds a project entry that names the library project by id. The entry keeps
   its own compact text for print, prefilled from the library when added and
   edited freely after. Order and hiding stay with the entry. No selection table
   exists, and editing the library never rewrites a resume.
3. **Project page.** A published resume serves `/{slug}/projects/{project-slug}`
   for each visible linked entry whose library project has text in the resume's
   language, and `/{slug}/projects` as their list. Both follow the resume's
   publish, sign in to view and indexing settings and pass the same live-state
   gate on every request.
4. **Viewer entry points.** The public page links each linked project entry to
   its page and shows a Projects link; the PDF and print carry the address of
   the resume's project list.
5. **Agents.** MCP tools may read and edit library projects and link entries.
   They cannot publish, and they reach the same validation as the editor.

## Alternatives rejected

- **Projects shown on every resume.** Publishes work the owner did not choose to
  show to that audience.
- **Copy a project between resumes.** Edits and evidence diverge between copies.
- **A selection table with entries generated from the library.** The resume
  document stops being self-contained, and a library edit would write to every
  selecting resume.
- **A project namespace of its own (`/p/{id}`).** Creates a public object that
  outlives the resume's publish and privacy settings.
- **Project pages under an account profile.** Adds the identity layer ADR 0003
  rejects.

## Consequences

- The resume schema gains an optional project reference on the project entry in
  a new document version (ADR 0004); existing entries stay valid and unlinked.
- The same project slug, text and evidence may appear under two resumes of one
  account, so a reader of both can link them. The owner chooses this per resume;
  the editor says so when a project is added to a second published resume.
- A linked entry whose library project is deleted becomes an ordinary entry; its
  page stops at once.
- The library is account data: it is exported, deleted with the account, and
  bounded by [budgets](../design/budgets.md).
- GitHub evidence attaches to the library project and needs its own design and
  ADR before code.

## History

Proposed 2026-11-10 from the owner's roadmap decisions.
