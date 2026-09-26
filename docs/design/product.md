# 1. Product scope

aboutme is an open-source resume builder and hosted display service. It serves
people who want to create, tailor, publish, and export a small set of resumes
without publishing an account profile.

## Core journeys

1. Sign in with email and password, or with an enabled provider. Production can
   enable Google and LinkedIn; GitHub exists but stays off. Server configuration
   may close new password registration without disabling existing password
   sign-in. An account may add a passkey or authenticator app as a
   [second factor](second-factor-authentication.md).
2. Create up to three resumes and edit incomplete drafts without save-time
   completeness errors.
3. Preview the same layout used by the public page and PDF.
4. Publish a resume at `aboutme.vn/{slug}` with explicit download and discovery
   choices.
5. Share, update, unpublish, rename, export, or delete the resume and account.
6. Connect an agent the person already uses and let it read and edit their
   resumes, then review and publish the result themselves.

## V1 scope

| Area                | V1 decision                                                                                              |
| ------------------- | -------------------------------------------------------------------------------------------------------- |
| Resume editor       | Eight section types; rich text; one- or two-column layout; fonts, colors, spacing, headings, and presets |
| Resume count        | At most three per account, enforced in PostgreSQL                                                        |
| Public identity     | One globally unique slug per resume; no username or account profile                                      |
| Authentication      | Email/password, providers, and optional second factor; providers and registration are server-configured  |
| Preview and publish | Instant local preview, granular autosave, public SSR page, and live refresh                              |
| Discovery           | Search engine optimization (SEO) and generative engine optimization (GEO), only after explicit opt-in    |
| Export              | Owner PDF; optional public PDF                                                                           |
| Agent access        | Remote Model Context Protocol (MCP) endpoint; editor parity minus publish; account-wide consent scopes   |
| Viewer analytics    | Owner-only view counts; viewer detail only with consent or sign-in                                       |
| Mobile              | Deferred until the deployed web v1; the API and document format remain language-neutral                  |

Out of v1: cover letters, a job tracker, first-party AI writing features, custom
domains, teams, interface languages beyond Vietnamese and English, and
collaborative editing.

## Landing and entry

The home page introduces the product in a few lines and offers sign-in and, when
enabled, registration. It is static server-rendered content with no data fetch,
links to the public template gallery, and renders one compiled-in sample resume
through the shared renderer. Its copy names only shipped behavior. Password
registration verifies the email before an account exists.

## Agent access

A person may connect an agent they already use; the product hosts no model and
ships no writing assistant of its own. The agent speaks MCP to one remote
endpoint on the canonical origin and authenticates through the service's own
OAuth 2.1 authorization server; it never holds a session cookie. Consent is
explicit, names the client and the scopes it asked for, and grants account-wide
`resumes:read` and `resumes:write`. The settings page lists every connected
agent with its last-used time and revokes any of them.

Agent tools reach the same validation, sanitizing, bounds, and concurrency
boundary as the editor, so an agent cannot store a document a person could not.
There is no publish, unpublish, or public-read tool: making a resume public
stays a human decision in the web UI. Detailed behavior lives in
[the API design](api.md#agent-access-and-the-bearer-world),
[the security design](security.md#agent-authorization-and-the-bearer-world), and
[ADR 0026](../adr/0026-mcp-agent-access.md).

## Public namespace

A resume slug is globally unique, 4–30 characters, and matches
`^[a-z0-9]+(-[a-z0-9]+)*$`. One versioned public-root registry is the exhaustive
authority for literal first path segments declared by product and infrastructure
route sources and for their dispatch class. It generates the Caddy fixed-root
matcher, the Go slug-claim set, and route-parity fixtures; those consumers have
no separate handwritten exceptions. A fixed root is added to the registry before
any route may claim it, and drift between the registry, OpenAPI root paths, the
Nuxt page manifest, or generated dispatch fails the build.

The registry keys one row per literal top-level segment; finer paths dispatch
inside the owning router. `packages/publicroots/public-roots.v9.json` holds the
exact roots. `admin`, `people`, and `u` are reserved for future use with no
handler ([ADR 0004](../adr/0004-resume-slug-only-urls.md)). The dotted and
underscore-prefixed roots cannot pass the slug grammar but stay in the registry
so dispatch and reservation parity remain exhaustive. Dynamic `/{slug}` and
`/{slug}.md` routes add no rows. Framework-generated paths that are not fixed
product or infrastructure routes fall through to Nuxt outside the registry.

`/authorize` is the Nuxt consent page and `/oauth/authorize` is the Go endpoint
that validates a request before redirecting to it. They are different roots, so
neither shadows the other.

A slug claim validates both the grammar and exact registry membership. Reserved
root segments cannot be claimed. A resume keeps its slug when unpublished.
Rename or deletion releases the old slug into a tombstone that holds no link to
any account. The tombstone blocks the slug for 180 days, then the daily privacy
retention sweep deletes it. [ADR 0004](../adr/0004-resume-slug-only-urls.md)
records the rationale.

## Publish controls

Publishing is a human action taken in the web UI. The publish dialog exposes
three independent choices:

1. **Public resume** controls whether any public representation exists.
2. **PDF download** controls the public PDF. The owner can always export a PDF.
3. **SEO and GEO** controls indexing and discovery surfaces. It defaults off.

It also sets two optional page details: the browser-tab title (default
`<full name> — Resume`) and one emoji shown as the page icon (default none).
Both are public, like the slug
([ADR 0042](../adr/0042-public-page-title-and-favicon.md)).

| State                    | Public behavior                                                                                                                                 |
| ------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| `live=false`             | All public resume, photo, markdown, PDF, image, and live-event routes return `404`; the SSE stream closes                                       |
| Live, discovery disabled | Shareable; HTML, JSON, photo, PDF, and preview card send `X-Robots-Tag: noindex, noarchive`; absent from sitemap and `llms.txt`; markdown `404` |
| Live, discovery enabled  | HTML, structured data, markdown, sitemap, and `llms.txt` discovery surfaces are available                                                       |
| Download enabled         | The public PDF route is available and the public page links it; otherwise the route returns `404` and the page shows no link                    |
| Preview card             | Stored 1200 by 630 PNG with name, headline, and photo, never contact details; live only, independent of download and discovery; ADR 0055        |

The sitemap lists `/`, `/privacy`, `/terms`, `/templates`, and each
`/templates/{id}` page, then every discoverable resume. `llms.txt` follows the
llms.txt convention: a title, a one-line summary, the site pages and source
code, then each discoverable resume's markdown. `robots.txt` allows everything
except `/app/`, `/api/`, and the sign-in pages. Under `/api/` it allows only the
preview card, which link previews fetch ([link previews](link-previews.md)).
Each sign-in page is disallowed by exact path, with or without a query, so a
slug that starts with the same letters stays crawlable.

Deleted, renamed, tombstoned, and never-published slugs all return the same
public `404`. The service does not expose which internal state caused absence.
Every public representation revalidates the current publish state before a
stored response is reused. Unpublish, delete, and rename do not return success
until the old public generation can no longer be admitted. A service-controlled
cache never extends access beyond that success boundary.
[ADR 0022](../adr/0022-public-artifact-revocation.md) defines the revocation
fence and its 60-second cache trade-off.

## Product boundaries

- Drafts accept partial data. Publish applies a separate completeness policy.
- Public pages disclose resume content, not account data or a list of the
  account's other resumes.
- A connected agent acts only on resume content. It reaches no session or
  account surface and cannot publish or unpublish a resume. A write grant may
  delete a published resume; deletion revokes its public URL through the normal
  fence and cleanup path rather than exposing a publish-state control.
- The public application has no operator or platform-admin surface. Operator
  actions run out of band with database credentials.
  [ADR 0028](../adr/0028-no-operator-surface.md) owns this rule.
- Publishing explains that public content can be delivered through a global
  content-delivery network. The discovery option separately explains crawler and
  AI-engine access.
- Deletion copy distinguishes immediate access revocation, private-media removal
  targeted within 24 hours, and expiry from the 30-day backup schedule. An
  overdue physical delete is audited and retried; it does not restore access.
- The homepage, authentication pages, legal pages, template gallery, resume
  workspace, account settings, and agent consent support Vietnamese and English,
  with Vietnamese as the default. Interface language never changes resume data
  or resume language; public resume chrome follows resume language
  ([localization design](localization.md)). Other scripts remain valid content,
  and font choices state measured coverage.
- Accessibility is a release requirement for the editor, publish flow, public
  page, and generated artifacts.
