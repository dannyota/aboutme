# 0029: Account project library and project pages

Status: Accepted (2026-11-11). The owner chose the direction on 2026-11-10
([roadmap](../plans/roadmap.md#owner-decisions)) and approved this record, the
[projects design](../design/projects.md) and the
[projects contract](../design/projects-contract.md) on 2026-11-11. Nothing is
implemented yet.

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
   holds the detail a page needs: a project slug unique within the account,
   role, technologies, outcome, repository and demo links, dates, and a long
   description, with text held per language and one default language. It has no
   public URL of its own, its id is never public, and it is never listed across
   accounts.
2. **Selection lives in the resume.** A resume shows a library project when it
   holds a project entry that names the library project by id. Every resume
   write checks that the id belongs to the same account. The entry keeps its own
   compact text for print, prefilled from the library when added and edited
   freely after. Order and hiding stay with the entry. No selection table
   exists, and editing library text never rewrites a resume.
3. **Project page.** A published resume serves `/{slug}/projects/{project-slug}`
   for each visible linked entry, and `/{slug}/projects` as their list, in the
   resume's language or else the project's default language. Both follow the
   resume's publish, sign in to view and indexing settings, are admitted on the
   resume's fence, and are revoked with it.
4. **Viewer entry points.** The public page links each linked project entry to
   its page and shows a Projects link; in the PDF the same title is a link.
5. **Agents.** Library tools need new scopes, `projects:read` and
   `projects:write`, which an existing grant does not hold until the owner
   consents again. No tool publishes.

## Alternatives rejected

- **Projects shown on every resume.** Publishes work the owner did not choose to
  show to that audience.
- **Copy a project between resumes.** Edits and evidence diverge between copies.
- **A selection table with entries generated from the library.** The resume
  document stops being self-contained, and a library edit would write to every
  selecting resume.
- **Library tools under the existing resume scopes.** A token granted for
  resumes would silently read projects never shown on any resume.
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
- Deleting a library project is one transaction that unlinks it from every
  resume of the account and advances their revisions; the entries stay as
  ordinary entries and the pages stop at once. A slug rename does the same for
  live resumes, and a released project slug is held from reuse for 180 days.
- ADR 0018 gains the two scopes, and ADR 0004 gains document version 6.
- The library is account data: it is exported, deleted with the account, and
  bounded by [budgets](../design/budgets.md).
- GitHub evidence attaches to the library project and needs its own design and
  ADR before code.

## History

Proposed 2026-11-10 from the owner's roadmap decisions. Revised 2026-11-11 after
design review: same-account reference checks, language fallback, project scopes,
slug holds, and deletion as one multi-resume transaction. Accepted by the owner
on 2026-11-11 with the proposed limits and the project title as the print entry
point.
