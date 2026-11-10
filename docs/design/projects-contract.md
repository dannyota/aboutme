# Projects contract

Status: Proposed (2026-11-11), awaiting owner approval. Product scope is in
[projects](projects.md); the decision is
[ADR 0029](../adr/0029-account-project-library.md). Nothing here is implemented.

## Routes and dispatch

| Path                              | Dispatch              | Notes           |
| --------------------------------- | --------------------- | --------------- |
| `/{slug}/projects`                | Go public gateway     | HTML only       |
| `/{slug}/projects/{project-slug}` | Go public gateway     | HTML only       |
| `/api/v1/projects…`               | Go, session or bearer | Owner API below |

- The generated Caddy matcher and the Go route classifier gain the two nested
  shapes. `projects` is a literal second segment, so the public root registry
  gets no new row and `projects` stays claimable as a resume slug. Route parity
  fixtures cover both shapes and a third unknown segment, which stays 404.
- Go admits the request on the resume's fence, applies sign in to view after
  admission and before any cache lookup, and renders through the existing direct
  render boundary with a frozen request. Nuxt never authorizes.
- The request feed classifies both shapes as `public`, so a 404 keeps no exact
  path sample. The route locale helper treats them as public resume routes.

## Project link context

One read builds a resume's public snapshot: the resume row and, for each visible
linked entry, the project's slug, page URL and, for a project page, its resolved
text and fields. The snapshot is taken in a single statement, so it is
consistent with the resume revision it carries. It never contains a project
`id`.

- The frozen public render request carries the snapshot for all three HTML pages
  of a resume.
- The public JSON gains `projectLinks`: entry id, project slug and absolute page
  URL for each available project, and nothing else. The browser hydrates the
  title links and the bar link from it and replaces it whole on every refetch,
  so a renamed or removed link disappears with the revision that changed it.
- Markdown, JSON-LD and the showcase listing keep their explicit field lists.

Links are absolute `https` URLs built by Go on the canonical origin; the HTML
validator accepts them as it accepts other absolute links.

## Generations and revocation

Admission, leases, fences, cache keys and recovery stay exactly as for a resume
([ADR 0010](../adr/0010-public-artifact-revocation.md)): the generation is the
resume revision, and nothing is keyed by a project revision.

**Rule.** Every library mutation that can change a public page advances the
revision of each live resume of the owner that links the project, in the same
transaction, without changing that resume's document. A project page, the
project list and the resume page are all keyed by the resume revision, and the
existing recovery proof (each targeted resume at its expected revision plus one)
holds unchanged.

| Library mutation                           | Transition over the linking live resumes |
| ------------------------------------------ | ---------------------------------------- |
| Text or field edit                         | Non-draining                             |
| Slug rename, deletion                      | Revoking                                 |
| Deletion, for each linking draft resume    | Non-draining over that resume            |
| Any edit of a project no live resume links | None; an ordinary row write              |

Link, unlink, hide, unpublish and sign in to view are existing resume writes and
transitions.

A revision advanced this way reaches open editors and viewers as any other
revision does: the editor reconciles, and a public page refetches. Stored
preview cards and showcase rows are unaffected, because their inputs did not
change.

## Library mutations

Every library mutation runs through the existing mutation chain: `If-Match` on
the project revision, `Idempotency-Key`, the owner lock, re-authentication of
the session or token inside the transaction, and one stored replay response.

**Planning and recheck.** A mutation that needs a transition follows the account
deletion pattern:

1. Preflight reads the affected set: every resume whose revision the mutation
   will advance, with its revision. For an edit or a rename that is each linking
   live resume. For a deletion it is every linking resume, live or not.
2. It acquires those fences in the coordinator's order and closes them: revoking
   for a live resume under rename or deletion, non-draining otherwise, including
   every unpublished resume a deletion changes. Each fence reopens at the
   advanced revision.
3. Inside the transaction, under the owner lock, it reads the affected set again
   and requires the same resumes at the same revisions.
4. On any difference it rolls back, reopens the fences and plans again, at most
   three times, then answers `public_state_busy`.

The owner lock serializes the database work against every REST and agent write
of that owner's resumes; the recheck covers a link or save that committed
between planning and the lock.

**Delete** is one idempotent operation. It removes `projectId` from the entries
of every resume of the owner that names the project, live or not, validates each
complete document, saves each with its revision advanced by one, synchronizes
showcase state, deletes the project, and records a slug hold. Its response lists
each changed resume with its new revision. A save from another tab read before
the deletion fails its precondition and refetches, as any concurrent edit does.

**Rename** changes the slug, holds the old one, and advances the linking live
resumes, in one transaction.

## Owner API

Proposed shapes; OpenAPI and generated clients change with the code.

| Method and path                                               | Request                         | Response                                      |
| ------------------------------------------------------------- | ------------------------------- | --------------------------------------------- |
| `GET /api/v1/projects`                                        | None                            | Projects, each with linked resume ids         |
| `POST /api/v1/projects`                                       | Project document, optional slug | The project; slug proposed from the name      |
| `GET /api/v1/projects/{id}`                                   | None                            | The project                                   |
| `PUT /api/v1/projects/{id}`                                   | Complete project document       | The project and each advanced resume revision |
| `DELETE /api/v1/projects/{id}`                                | None                            | Each changed resume with its new revision     |
| `POST /api/v1/resumes/{id}/project-links`                     | `{projectId}`                   | The existing entry-write response             |
| `POST /api/v1/resumes/{id}/entries/{entryId}/project-refresh` | None                            | The existing entry-write response             |

A project document is `slug`, `dates`, `repositoryUrl`, `demoUrl`,
`technologies`, `defaultLanguage` and `text`. `PUT` replaces it whole: an
omitted optional field is cleared, and a language absent from `text` is removed.
Every write carries `If-Match` with the project or resume revision and an
`Idempotency-Key`.

Errors reuse the existing problem format: `project_limit`, `project_slug_taken`,
`project_slug_held`, `project_not_found` (also for another account's id),
`project_already_linked`, `public_state_busy`, and the existing precondition and
validation errors.

## Agents

[ADR 0018](../adr/0018-mcp-agent-access.md) gains two scopes, `projects:read`
and `projects:write`.

- A grant made before this change keeps `resumes:read` and `resumes:write` only.
  Library tools answer `scope_denied` until the owner approves the new scopes on
  the consent page; a client that requests them is shown consent, as the subset
  rule already requires.
- Tools: `list_projects`, `get_project`, `create_project`, `update_project`,
  `delete_project` under the project scopes; `link_project` and
  `refresh_project_entry` need `resumes:write` and `projects:read`.
- The scope check sits at the shared resume-write boundary, not only in the link
  tool: any bearer write that adds a `projectId` to a resume or changes one
  needs `projects:read`, whichever tool or route carries it. A write that keeps
  or removes existing references needs only `resumes:write`.
- No tool publishes, attaches evidence or connects an account.
- Until reviewed changes ship (v0.10), an agent edit to a library project of a
  published resume is public at once, as an agent edit to the resume is today;
  the consent copy and the MCP guide say so.

## Representations

| Artifact                         | Change in v0.9                                                                 |
| -------------------------------- | ------------------------------------------------------------------------------ |
| Project and project-list HTML    | New. Gate page without a pass; `noindex` unless SEO and GEO is on              |
| Resume HTML                      | Title links and the bar link, from the project link context                    |
| Canonical and head               | Each page is its own canonical; title and description from the project text    |
| Link preview card and Open Graph | Reuses the resume's stored card; a project edit rebuilds nothing               |
| Public JSON                      | Gains `projectLinks` only; no project id                                       |
| JSON-LD, `/{slug}.md`            | Unchanged; no project id, slug or URL                                          |
| Sitemap and `llms.txt`           | Unchanged; project pages are not discovery members, so no discovery generation |
| Showcase                         | Unchanged; project pages are never listed                                      |
| Live stream                      | Protocol unchanged; a resume page refetches on the advanced revision           |
| Public PDF                       | Title links only, below                                                        |

As for the resume, the card and title stay public behind sign in to view, and a
preview a third party has already fetched cannot be recalled
([link previews](link-previews.md)).

| Transition                            | Project page on the next request |
| ------------------------------------- | -------------------------------- |
| Unpublish, resume or account deletion | 404                              |
| Sign in to view turned on             | Gate page                        |
| Entry hidden, unlinked or removed     | 404                              |
| Project deleted                       | 404                              |
| Project slug renamed                  | Old address 404; held from reuse |
| Project text edited                   | New content                      |

No project page view is counted and nothing about a viewer is stored
([ADR 0022](../adr/0022-viewer-privacy-and-counting.md)).

## Print

The print snapshot envelope gains, in a new envelope version, a map from entry
id to project page URL, frozen and authorized when the snapshot is taken. The
pure render context gains one optional input: a title link per entry that
replaces the entry's own `link`. The renderer adds no element and changes no
geometry, so [print rules](templates/print.md) and the product page images are
unaffected; a display check proves a linked and an unlinked title measure the
same. An unpublished resume and the editor preview of one carry no map.

## Export and deletion

- The account export moves to `exportVersion` 2 and gains `projects`, ordered by
  creation time, each with its fields, text and revision. Resume documents in
  the export keep `projectId`. The byte cap is unchanged.
- Account deletion removes the library by cascade and the slug holds explicitly.
  The privacy sweep deletes expired slug holds.

## Data

- `projects`: owner, slug unique per owner, revision, a JSON document for the
  fields and text, timestamps.
- `project_slug_holds`: owner, slug, release time.
- OAuth scope constraints on grants and codes accept the two new scopes.

## Documents that change with implementation

[API](api.md) and `docs/api/openapi.yaml`; [data](data.md) and
[budgets](budgets.md); [ADR 0004](../adr/0004-resume-document-contract.md)
history for version 6; [web](web.md) for the render context;
[templates contract](templates/contract.md) for the title link;
[public page bar](public-page-bar.md); [localization](localization.md) and the
consent copy; the [MCP guide](mcp-guide.md); the
[request feed](crowdsec-request-feed.md) route classes; and the
[AC-PROJ rows](../plans/traceability/ac-proj.md).
