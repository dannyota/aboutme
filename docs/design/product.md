# 1. Product scope

aboutme is an open-source resume builder and hosted display service. It serves
people who want to create, tailor, publish, and export a small set of resumes
without publishing an account profile.

## Core journeys

1. Sign in with Google, GitHub, LinkedIn, or email and password.
2. Create up to three resumes and edit incomplete drafts without save-time
   completeness errors.
3. Preview the same layout used by the public page and PDF.
4. Publish a resume at `aboutme.vn/{slug}` with explicit download and discovery
   choices.
5. Share, update, unpublish, rename, export, or delete the resume and account.

## V1 scope

| Area                | V1 decision                                                                                              |
| ------------------- | -------------------------------------------------------------------------------------------------------- |
| Resume editor       | Eight section types; rich text; one- or two-column layout; fonts, colors, spacing, headings, and presets |
| Resume count        | At most three per account, enforced in PostgreSQL                                                        |
| Public identity     | One globally unique slug per resume; no username or account profile                                      |
| Authentication      | Google, GitHub, LinkedIn, and email/password; zero or one password credential per account                |
| Preview and publish | Instant local preview, granular autosave, public SSR page, and live refresh                              |
| Discovery           | Search engine optimization (SEO) and generative engine optimization (GEO), only after explicit opt-in    |
| Export              | Owner PDF; optional public PDF                                                                           |
| Mobile              | Deferred until the deployed web v1; the API and document format remain language-neutral                  |

Out of v1: cover letters, a job tracker, AI writing tools, custom domains,
teams, analytics, a multilingual application interface, and collaborative
editing.

## Public namespace

A resume slug is globally unique, 4–30 characters, and matches
`^[a-z0-9]+(-[a-z0-9]+)*$`. One versioned public-root registry is the exhaustive
authority for literal first path segments declared by product and infrastructure
route sources and for their dispatch class. It generates the Caddy fixed-root
matcher, the Go slug-claim set, and route-parity fixtures; those consumers have
no separate handwritten exceptions. A fixed root is added to the registry before
any route may claim it, and drift between the registry, OpenAPI root paths, the
Nuxt page manifest, or generated dispatch fails the build.

The v5 registry starts with these exact roots:

| Root              | Source and dispatch                                                              |
| ----------------- | -------------------------------------------------------------------------------- |
| `admin`           | Protected future namespace from ADR 0004; reserved-only, with no current handler |
| `api`             | OpenAPI server root and Caddy `/api/v1/*`; Go                                    |
| `app`             | Current Nuxt `/app/settings/sessions` page tree; Nuxt                            |
| `forgot-password` | Nuxt `/forgot-password` page; Nuxt                                               |
| `healthz`         | OpenAPI and Caddy `/healthz`; Go                                                 |
| `_nuxt`           | Nuxt build-asset namespace; Nuxt                                                 |
| `internal-render` | Direct Go-to-Nuxt renderer; Caddy denies every viewer request                    |
| `llms.txt`        | Caddy `/llms.txt`; Go                                                            |
| `login`           | Current Nuxt `/login` page; Nuxt                                                 |
| `people`          | Protected future namespace from ADR 0004; reserved-only, with no current handler |
| `print`           | Caddy `/print` and `/print/*`; denied externally and capability-gated internally |
| `readyz`          | OpenAPI and Caddy `/readyz`; Go                                                  |
| `register`        | Nuxt `/register` page; Nuxt                                                      |
| `reset-password`  | Nuxt `/reset-password` page; Nuxt                                                |
| `robots.txt`      | Caddy `/robots.txt`; Go                                                          |
| `sitemap.xml`     | Caddy `/sitemap.xml`; Go                                                         |
| `u`               | Protected future namespace from ADR 0004; reserved-only, with no current handler |
| `verify-email`    | Nuxt `/verify-email` page; Nuxt                                                  |

The root `/` has no first segment. Dynamic `/{slug}` and `/{slug}.md` routes do
not add literal entries. The dotted and underscore-prefixed roots cannot pass
the slug grammar, but stay in the registry so route dispatch and reservation
parity remain exhaustive. Framework-generated paths that are not fixed product
or infrastructure routes remain outside this registry and fall through to Nuxt.

A slug claim validates both the grammar and exact registry membership. Reserved
root segments cannot be claimed. A resume keeps its slug when unpublished.
Rename or deletion releases the old slug into a 180-day tombstone so another
account cannot immediately take over an old link.
[ADR 0004](../adr/0004-resume-slug-only-urls.md) records the rationale.

## Publish controls

The publish dialog exposes three independent choices:

1. **Public resume** controls whether any public representation exists.
2. **PDF download** controls the public PDF. The owner can always export a PDF.
3. **SEO and GEO** controls indexing and discovery surfaces. It defaults off.

| State                    | Public behavior                                                                                                    |
| ------------------------ | ------------------------------------------------------------------------------------------------------------------ |
| `live=false`             | All public resume, photo, markdown, PDF, image, and live-event routes return `404`; the SSE stream closes          |
| Live, discovery disabled | Shareable HTML with `X-Robots-Tag: noindex, noarchive`; absent from sitemap and `llms.txt`; markdown returns `404` |
| Live, discovery enabled  | HTML, structured data, markdown, sitemap, and `llms.txt` discovery surfaces are available                          |
| Download enabled         | The public PDF route is available; otherwise it returns `404`                                                      |

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
- Publishing explains that public content can be delivered through a global
  content-delivery network. The discovery option separately explains crawler and
  AI-engine access.
- Deletion copy distinguishes immediate access revocation, private-media removal
  targeted within 24 hours, and expiry from the 30-day backup schedule. An
  overdue physical delete is audited and retried; it does not restore access.
- The v1 application interface is English. Vietnamese resume content is a
  first-class fixture and fallback target because the initial community is
  Vietnamese. Other scripts remain valid content; font choices state measured
  coverage instead of claiming universal coverage.
- Accessibility is a release requirement for the editor, publish flow, public
  page, and generated artifacts.
