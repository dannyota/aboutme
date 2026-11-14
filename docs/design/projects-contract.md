# Projects contract

Status: Accepted by the owner (2026-11-11). Implemented: the library, linking,
deletion, replace advancement, the public link context with `projectLinks`, the
project pages with their dispatch, and the resume page's
[entry points](project-entry-points.md). Product scope is in
[projects](projects.md); the decision is
[ADR 0029](../adr/0029-account-project-library.md).

## Routes and dispatch

| Path                              | Dispatch          | Notes           |
| --------------------------------- | ----------------- | --------------- |
| `/{slug}/projects`                | Go public gateway | HTML only       |
| `/{slug}/projects/{project-slug}` | Go public gateway | HTML only       |
| `/api/v1/projects…`               | Go, session       | Owner API below |

- The generated Caddy matcher and the Go route classifier gain the two nested
  shapes. `projects` is a literal second segment, so the public root registry
  gets no new row and `projects` stays claimable as a resume slug. Route parity
  fixtures cover both shapes and a third unknown segment, which stays 404.
- Go handles a request in this order: it reads and admits the resume on its
  fence, and an unknown or non-live resume answers the uniform 404; then sign in
  to view without a valid pass serves the resume's gate for any well-formed
  project address, whether or not that project or any project is available; then
  an unavailable list or project answers the same uniform 404; then the cache;
  then it renders through the existing direct render boundary with a frozen
  request. The gate therefore reveals no more than the resume page's gate does.
  Nuxt never authorizes.
- A malformed shape, such as a trailing slash, `.md`, uppercase or an extra
  segment, matches no Go route and falls to Nuxt's 404, as `/{slug}/` does; the
  decision depends only on the path's form, never on stored data.
- The request feed classifies both shapes as `public`, so a 404 keeps no exact
  path sample. The route locale helper treats them as public resume routes.

## Project link context

One read builds a resume's public snapshot: the resume row and, for each visible
linked entry, the project's slug, page URL and, for a project page, its resolved
text and fields. The resume page, the project list and a project page also read
every linked project's fields and text without its description, to decide which
have a name to show; a linked project without one carries no link. The snapshot
is taken in a single statement, so it is consistent with the resume revision it
carries. It never contains a project `id`. Every state other than a live resume
reads as no rows, and an entry's `projectId` is compared as text, so a malformed
value matches no project.

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

Admission, leases, fences and recovery stay exactly as for a resume
([ADR 0010](../adr/0010-public-artifact-revocation.md)): the generation is the
resume revision, and nothing is keyed by a project revision. The cache key adds
the route class (`project-list` or `project`) and, for a project page, the
project slug, beside the resume's variant and revision.

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

**Replace** (`PUT`) always advances the project revision. It advances linking
live resumes only when the stored slug or the stored canonical document differs
from the result: a document change is non-draining, a slug change or both is
revoking. It compares the stored, sanitized forms, so a request that differs
only in formatting or in what sanitizing removes advances no resume. Its
response lists each advanced resume with its new revision.

**Revision-only advance.** An edit or rename that changes no resume document
advances each resume's revision through its own compare-and-set query, which
leaves `updated_at` and the document unchanged. The revision is the only change
signal for public artifacts: leases, render caches and fences key on it, public
ETags are digests of the response body, and no public response derives a
validator, `lastmod` or discovery state from `updated_at`. A PDF re-rendered at
an advanced revision therefore keeps the date of the resume's last document
save. Showcase rows store no resume revision and the preview card is keyed by
its own card version, so an advance changes neither.

## Owner API

Proposed shapes; OpenAPI and generated clients change with the code.

| Method and path                                               | Request                           | Response                                      |
| ------------------------------------------------------------- | --------------------------------- | --------------------------------------------- |
| `GET /api/v1/projects`                                        | None                              | Projects, each with linked resume ids         |
| `POST /api/v1/projects`                                       | Project document, optional slug   | The project; slug proposed from the name      |
| `GET /api/v1/projects/{id}`                                   | None                              | The project                                   |
| `PUT /api/v1/projects/{id}`                                   | Complete project document         | The project and each advanced resume revision |
| `DELETE /api/v1/projects/{id}`                                | None                              | Each changed resume with its new revision     |
| `POST /api/v1/resumes/{id}/project-links`                     | `{projectId}`, optional `entryId` | The existing entry-write response             |
| `POST /api/v1/resumes/{id}/entries/{entryId}/project-refresh` | None                              | The existing entry-write response             |

A project document is `slug`, `dates`, `repositoryUrl`, `demoUrl`,
`technologies`, `defaultLanguage` and `text`. `PUT` replaces it whole: an
omitted optional field is cleared, and a language absent from `text` is removed.
Every write carries `If-Match` with the project or resume revision and an
`Idempotency-Key`.

With `entryId`, `project-links` links that existing entry instead of inserting
one: the entry must be an unlinked entry of a project section of the same
resume, its text is left as stored, and the response is the same entry-write
response. An unknown entry answers `entry_not_found`; an entry that is already
linked, or of another section type, answers `project_already_linked` or the
existing validation error. The ownership, uniqueness and bearer rules of the
write boundary apply unchanged.

A slug rename and a deletion release a slug, so each needs recent
reauthentication, and for an account with a second factor also a recent
second-factor proof; otherwise it answers `403 reauth_required`.

Errors reuse the existing problem format: `project_limit`, `project_slug_taken`,
`project_slug_held`, `project_not_found` (also for another account's id),
`project_already_linked`, `public_state_busy`, `reauth_required`, and the
existing precondition and validation errors.

## Agents

[ADR 0018](../adr/0018-mcp-agent-access.md) gains two scopes, `projects:read`
and `projects:write`.

- A grant made before this change keeps `resumes:read` and `resumes:write` only.
  Library tools answer `scope_denied` until the owner approves the new scopes on
  the consent page; a client that requests them is shown consent, as the subset
  rule already requires.
- Tools dispatch in process into the same project and resume executors as the
  owner API, as resume tools do; `/api/v1/projects` itself stays session-only.
  Arguments are snake_case, `revision` is a decimal string, every write takes
  `idempotency_key`, and `document` uses the REST names and the same strict
  decoder. A project result is the REST project resource.

  | Tool                    | Arguments                                                                     | Result                                   | Scopes                            |
  | ----------------------- | ----------------------------------------------------------------------------- | ---------------------------------------- | --------------------------------- |
  | `list_projects`         | none                                                                          | `projects`                               | `projects:read`                   |
  | `get_project`           | `project_id`                                                                  | `project`                                | `projects:read`                   |
  | `create_project`        | `idempotency_key`, `document`, optional `slug`                                | `revision`, `project`                    | `projects:write`                  |
  | `update_project`        | `idempotency_key`, `project_id`, `revision`, `slug`, `document`               | `revision`, `project`, `changed_resumes` | `projects:write`, `resumes:write` |
  | `delete_project`        | `idempotency_key`, `project_id`, `revision`                                   | `id`, `deleted`, `changed_resumes`       | `projects:write`, `resumes:write` |
  | `link_project`          | `idempotency_key`, `resume_id`, `revision`, `project_id`, optional `entry_id` | `revision`, `state`                      | `resumes:write`, `projects:read`  |
  | `refresh_project_entry` | `idempotency_key`, `resume_id`, `revision`, `entry_id`                        | `revision`, `state`                      | `resumes:write`, `projects:read`  |

  `update_project` replaces the whole document, as `PUT` does, and
  `changed_resumes` lists each `id` and new `revision` of a resume the change
  rewrote. Update and delete need `resumes:write` because they rewrite linking
  resumes. A rename or delete over MCP needs no recent reauthentication, as
  `delete_resume` does not; the scopes are the gate. A project result has no
  evidence field until evidence ships.

- The scope check sits at the shared resume-write boundary, not only in the link
  tool: any bearer write that adds a `projectId` to a resume or changes one
  needs `projects:read`, whichever tool or route carries it. `projects:write`
  alone does not satisfy it. A write that keeps or removes existing references
  needs only `resumes:write`. Every agent mutation, library or resume, rereads
  the grant inside its transaction under the owner lock and checks its complete
  scope set before it reads a project or writes anything, so a grant narrowed
  meanwhile is honoured. `link_project` and `refresh_project_entry` check
  `projects:read` before reading any project, so an agent without it cannot tell
  its own project id from another account's.
- Two codes join the closed tool error vocabulary: `limit_reached` (the project
  or resume cap) and `slug_unavailable` (a slug taken or held). Another
  account's project is `not_found`.
- The authorization server advertises all four scopes and orders a requested
  scope set canonically before checking it, since OAuth scope order carries no
  meaning; unknown or repeated scopes still fail. A client that asks again with
  fewer scopes keeps only those, as today.
- The scopes ship in two releases, as the
  [schema floor](release-floors.md#schema-floor) requires for a change older
  releases cannot read. The first adds the migration that allows the project
  scope strings and teaches bearer checks, token refresh and Connected agents to
  accept a grant that holds them, while no tool, consent or metadata offers
  them; it is published and verified. The second adds the tools, consent and
  metadata, and raises the schema floor and the tracked fence floor to the
  first. The migration's down step refuses while any project-scope grant or code
  exists.
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

## Render envelope

Go renders both pages through the direct render boundary in two closed modes,
`project-list` and `project`, beside the resume's `continuous` and the `gate`.
The request types are `ProjectListRenderRequest` and `ProjectRenderRequest` in
`apps/server/internal/directrender/project_types.go` and their mirror in
`apps/web/server/utils/public-render/project-envelope.ts`.
`apps/server/internal/publicapi/testdata/project-render.json` is the shared
corpus: each envelope decodes into its Go type with no unknown field and encodes
back unchanged, and each HTML is what the renderer writes and the validator
accepts. The corpus HTML is the renderer's exact output; both sides test against
it.

- Go builds both requests from one admitted snapshot. They carry no project id,
  entry id, account data, contact detail, photo or other resume content.
- Every shown project field is the library project's, including its dates when
  the entry hides its own. The resume entry decides only availability and order,
  with one exception: `entryLink`, the entry's own `https` link, sent only when
  it differs from both the repository and the demo address. The comparison
  lowercases the scheme and host as JavaScript does, drops a default or empty
  port, and reads an empty path as `/`; the rest must match byte for byte. The
  link is sent as stored.
- Go chooses one language per project, as [Language](projects.md#language) says,
  falling back to the default language when the chosen text's name is blank. It
  sends only that text, with `lang` set when it is not the resume language's
  primary subtag, including for an `und` resume. A role, summary, outcome or
  description that is blank by the renderer's trim rule is left out, as if
  absent.
- `uiLng` is `vi` when the resume language's primary subtag is `vi`, otherwise
  `en`, as for the gate. Dates go as stored; the renderer formats them with
  `dateFormat`, as on the resume page.
- A list row carries at most its first six technologies and counts the rest in
  `moreTechnologies`, omitted when none; a project page carries all of them.
- The list page has no description. A project page's is its summary, else the
  first 160 code points of its outcome, else none; a summary blank by the
  renderer's trim rule counts as absent, and an empty `preview.description`
  omits both description elements. Nothing else is cut: the renderer and the
  validator admit a preview description of up to 300 code points and 1,200
  bytes, and a preview title of up to 683 code points and 2,727 bytes, the
  longest a stored project and resume can produce.

Go serves 503 for rendered HTML that breaks any of these rules:

- A doctype, at most 2 MiB, and `html` with `lang` equal to the resume's `lng`
  and at most a `data-scheme` equal to `colorScheme`.
- The head: charset and viewport, the exact title, `canonical` equal to
  `pageUrl`, the favicon only when `faviconHref` is set, and the resume page's
  preview elements with `og:type` `website`, `og:url` equal to `pageUrl`, and
  the description pair only when set, and at most the resume page's one
  `format-detection` element. No CSS in the page: no `style` element and no
  `style` attribute, and no stylesheet but the versioned `print-fonts.css` and
  `project-pages.css`, each at `/_nuxt/assets/{name}?v={styleVersion}`. The
  style version is a hash of every public stylesheet's content, so a cached
  stylesheet never pairs with newer HTML. Inline CSS can render text in too many
  ways to check, so the page's CSS comes only from the image.
- No `script`, `form`, `iframe`, `img`, media or embedding element, and no event
  or resource attribute. SVG only as static icons: `svg`, `g`, `path`, `circle`,
  `rect`, `line`, `polyline`, `polygon` and `title`, with presentation
  attributes and a numeric `transform` (only `rotate`, `translate` and `scale`
  with plain numbers), and no reference, `url()` paint, animation or embedded
  image.
- One `main#public-project`, one `h1`, and one skip link to the main region.
- The response's CSP is the resume page's with `style-src 'self'` and no
  `'unsafe-inline'`; the gate keeps its own.
- Anchors: the credit once, as on the resume page; `a.public-back` once, to the
  resume `url`, with the accessible name in the
  [page design](ui/project-pages.md#shared-frame); internal links only to the
  resume, the list and the requested projects, with only `href` and `class`, and
  on the list each project once; `a.project-external` only for the request's
  repository, demo and entry links, each at most once, with
  `rel="noopener noreferrer nofollow"`; and the description's own links.
- `.project-description` appears once on a project page whose
  `project.description` is set, and nowhere else. Its content, parsed and with
  comments ignored, is the same tree as `project.description` parsed.

A gate that stands in for a project page carries `page` in its envelope
(`GateRenderRequest`): `projects` or `projects/{project-slug}`, omitted for the
resume page. Each provider link then ends in `&page={page}`, and the gate
validator expects exactly those links. The view start stores the page and the
callback returns to it under the resume's current slug.

## Print

The print snapshot envelope gains, in a new envelope version, a map from entry
id to project page URL, frozen and authorized when the snapshot is taken. The
pure render context gains one optional input: a title link per entry that
replaces the entry's own `link`. The renderer adds no element and changes no
geometry, so [print rules](templates/print.md) and the product page images are
unaffected; a display check proves a linked and an unlinked title measure the
same. An unpublished resume and the editor preview of one carry no map.

The envelope is version 2 only when the map is non-empty; otherwise it stays
version 1, byte for byte. Its `projectLinks` maps each visible project entry
with an available page to that page's absolute URL. Each value is exactly the
configured canonical origin plus `/{slug}/projects/{project-slug}`, so an `http`
value is possible only when the origin itself is `http`, as on local
development; one constructor builds every version 2 envelope and refuses any
other value. The print renderer cannot know the origin, so it checks only each
value's shape and relies on Go for its authority. Go computes the map by the
same rule as the public JSON, so a resume's PDF, its page and its JSON link the
same projects. The public PDF and the owner's export of a live resume both carry
it, and the PDF format version rises with it.

## Entry points on the resume page

The resume page's title links, its page bar link and their hydration are
specified in [project entry points](project-entry-points.md).

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
