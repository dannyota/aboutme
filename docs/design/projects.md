# Projects

Status: Accepted by the owner (2026-11-11); not yet implemented.
[ADR 0029](../adr/0029-account-project-library.md) records the decision and the
[roadmap](../plans/roadmap.md#v09-show-the-work) the order. This document covers
the product, the library and selection. The
[projects contract](projects-contract.md) covers routes, delivery, revocation,
the API, agents, export and print. GitHub connection and evidence get their own
design.

## What a person does

1. Opens Projects in the workspace and writes a project once: name, role,
   technologies, outcome, links, dates and a description.
2. Chooses, in the same step, which of their resumes show it. Each chosen resume
   gets a compact project entry, prefilled, that they may shorten for that
   audience.
3. Publishes the resume as today. Every linked, visible project of a published
   resume has a page, and the resume shows viewers how to reach it.

Nothing is shown by default. A project appears only on resumes it was added to.

## Library

A library project belongs to one account and is private. It has no public URL of
its own and is never listed across accounts.

| Field                      | Rule                                                                             |
| -------------------------- | -------------------------------------------------------------------------------- |
| `id`                       | Server UUIDv7; never sent to a public viewer                                     |
| `slug`                     | Unique within the account; resume slug grammar; 2 to 40 characters               |
| `revision`                 | Compare-and-swap counter, advanced by every write                                |
| `dates`                    | The resume entry date range                                                      |
| `repositoryUrl`, `demoUrl` | Optional; `https` only                                                           |
| `technologies`             | Ordered plain strings                                                            |
| `defaultLanguage`          | A language key present in `text`                                                 |
| `text`                     | Map from language key to `name`, `role`, `summary`, `outcome`, and `description` |

A language key is the lowercase primary subtag of a BCP 47 tag (`vi`, `en`,
`ja`). `description` is rich text and passes the resume sanitizer. A project
needs a non-blank `name` in its default language to be saved and is otherwise
draft-permissive, like a resume entry.

### Limits

Approved by the owner on 2026-11-11. Each row moves to [budgets](budgets.md)
with the code that enforces it.

| Limit                                 | Value                                   |
| ------------------------------------- | --------------------------------------- |
| Projects per account                  | 30                                      |
| Languages per project                 | 4                                       |
| Technologies per project              | 20, each at most 40 characters          |
| `name`, `role`                        | 120 characters each                     |
| `summary` / `outcome`                 | 300 / 600 characters                    |
| `description`                         | 16 KiB, as resume rich text             |
| One stored project                    | 96 KiB                                  |
| Project reads and writes              | Count against the resume route limiters |
| Project slug changes                  | Count against the slug claim limiter    |
| Released project slug held from reuse | 180 days, within the account            |

## Language

A resume's language is a BCP 47 tag or `und`
([localization](localization.md#resume-language)). A project page under a resume
uses the text whose key equals the resume language's primary subtag, and
otherwise the project's default language. A resume with `und` uses the default
language. Every linked project therefore has a page. The editor marks a linked
project whose page falls back to another language.

## Selection

The resume document stays the only authority for what a resume shows.

- A project entry gains an optional `projectId`. An entry with it is linked; one
  without it behaves as today.
- Every write of a resume document checks that each `projectId` names a project
  of the same account and that no two entries of the resume name the same
  project. The check runs inside the owner-locked transaction of every REST and
  agent write, on create, entry upsert and import alike. An agent write that
  adds or changes a reference also needs the project read scope
  ([contract](projects-contract.md#agents)).
- Linking inserts an entry in the resume's project section. When the resume has
  none, linking creates a section with key `projects` at the end of the main
  column, through the existing structure command.
- Prefill from the text chosen by the [language rule](#language): `title` from
  `name`, `subtitle` from `role`, `link` from `repositoryUrl` or else `demoUrl`,
  `dates` from `dates`, `description` from `summary` as one paragraph.
- The entry's text is the resume's own. "Refresh from library" repeats the
  prefill on request; nothing else overwrites it.
- Order, hiding and removal use the existing entry operations.

This adds resume document version 6
([ADR 0004](../adr/0004-resume-document-contract.md)). `projectId` is optional;
emitting version 5 drops it, and a version 5 write keeps the stored value by
entry id, as the version 3 `subtitle` rule does. No stored document is rewritten
by the upgrade.

## Project pages and entry points

- `/{slug}/projects` lists the resume's available projects in entry order;
  `/{slug}/projects/{project-slug}` shows one.
- A project is available on a resume when the resume is live and the linked
  entry is visible. Any other state answers exactly as an unknown resume.
- The page shows name, role, dates, technologies, outcome, description, the
  repository and demo links, and a link back to the resume. It shows no contact
  details beyond the resume page and no account data.
- The public resume page shows "Projects" in the page bar when a project is
  available, and a linked, available entry's title links to its project page.
  The entry's own `link` then appears only on the project page.
- In the PDF and print, the same title is a link to the project page. No line is
  added, so layout and pagination do not change. The owner chose this for v0.9
  on 2026-11-11; a printed address line for paper readers is not planned.

The page uses the public page bar and theme of its resume. Layout, the workspace
Projects screen, the resume picker and bilingual copy are specified with the UI
work in [shell and editor](ui/shell-and-editor.md) and
[localization](localization.md).

## Privacy

- The same project slug, text and evidence may appear under two resumes of one
  account, so a reader of both can connect them. The owner chooses this per
  resume; the editor says so when a project is added to a second published
  resume. No other account identifier and no cross-resume list is ever public.
- A project page gates, indexes and revokes exactly as its resume
  ([contract](projects-contract.md#representations)).
- Project text is user content: it never enters logs or telemetry.
