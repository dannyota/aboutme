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

## Frozen render input

The public render request gains, for all three public HTML pages of a resume:

- `projects`: the available projects in entry order, each with its entry id,
  project slug, absolute page URL on the canonical origin, and, for a project
  page, the resolved project text and fields. It never contains a project `id`.
- The resume page uses it only for the title links and the bar link.

Links are absolute `https` URLs built by Go; the HTML validator accepts them as
it accepts other absolute links. The public JSON and Markdown projections keep
their explicit field lists and gain nothing.

## Generations and revocation

Admission, leases and fences stay per resume
([ADR 0010](../adr/0010-public-artifact-revocation.md)).

- **Cache key.** A project or project-list page is keyed by the resume revision
  and, in the variant, the revision of each project it renders. The resume page
  stays keyed by the resume revision.
- **Content edit of a library project** (text, fields): a non-draining
  transition over the owner's live resumes that link it. Their revisions do not
  change; the project revision does, so no stale project page is reused.
- **Address or availability change** (slug rename, deletion): one revoking
  transition over the same resumes. It advances each such resume's revision in
  the same transaction, because the resume page's links change, and drains
  leases within the existing bound.
- **Link, unlink, hide, unpublish, sign in to view:** existing resume writes and
  transitions; they already advance the revision or revoke.

## Library mutations

Every library mutation runs through the existing mutation chain: `If-Match` on
the project revision, `Idempotency-Key`, the owner lock, re-authentication of
the session or token inside the transaction, and one stored replay response.

**Delete** is one idempotent operation. Under the owner lock it reads every
resume of the owner that names the project, removes `projectId` from those
entries, validates each complete document, saves each with its revision advanced
by one, synchronizes showcase state, deletes the project, and records a slug
hold. Its response lists each changed resume with its new revision. A save in
another tab that was read before the deletion fails its precondition and
refetches, as any concurrent edit does today. The owner lock serializes deletion
against linking, so a link to a deleted project cannot commit.

**Rename** changes the slug, holds the old one, and advances the revision of
each linking live resume, in one transaction.

## Owner API

Proposed shapes; OpenAPI and generated clients change with the code.

| Method and path                                               | Purpose                                                   |
| ------------------------------------------------------------- | --------------------------------------------------------- |
| `GET /api/v1/projects`                                        | List the library with each project's linked resume ids    |
| `POST /api/v1/projects`                                       | Create; server assigns `id`, proposes `slug` from `name`  |
| `GET /api/v1/projects/{id}`                                   | Read one                                                  |
| `PATCH /api/v1/projects/{id}`                                 | Update fields, text or slug                               |
| `DELETE /api/v1/projects/{id}`                                | Delete, as above                                          |
| `POST /api/v1/resumes/{id}/project-links`                     | Link `{projectId}`; a resume write, returns the new entry |
| `POST /api/v1/resumes/{id}/entries/{entryId}/project-refresh` | Refresh the entry text from the library                   |

Errors reuse the existing problem format: `project_limit`, `project_slug_taken`,
`project_slug_held`, `project_not_found` (also for another account's id),
`project_already_linked`, and the existing precondition and validation errors.

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
- No tool publishes, attaches evidence or connects an account.
- Until reviewed changes ship (v0.10), an agent edit to a library project of a
  published resume is public at once, as an agent edit to the resume is today;
  the consent copy and the MCP guide say so.

## Representations

| Artifact                           | Change in v0.9                                                                 |
| ---------------------------------- | ------------------------------------------------------------------------------ |
| Project and project-list HTML      | New. Gate page without a pass; `noindex` unless SEO and GEO is on              |
| Resume HTML                        | Title links and the bar link, from the frozen input                            |
| Canonical and head                 | Each page is its own canonical; title and description from the project text    |
| Link preview card and Open Graph   | Reuses the resume's stored card; a project edit rebuilds nothing               |
| JSON-LD, `/{slug}.md`, public JSON | Unchanged; no project id, slug or URL                                          |
| Sitemap and `llms.txt`             | Unchanged; project pages are not discovery members, so no discovery generation |
| Showcase                           | Unchanged; project pages are never listed                                      |
| Live stream                        | Unchanged; project pages do not live-refresh                                   |
| Public PDF                         | Title links only, below                                                        |

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
unaffected. An unpublished resume and the editor preview of one carry no map.

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
