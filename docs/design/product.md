# 1. Product scope

aboutme is an open-source resume builder and hosted display service. It serves
people who want to create, tailor, publish, and export a small set of resumes
without publishing an account profile.

## Core journeys

1. Sign in with Google, GitHub, or LinkedIn.
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
| Authentication      | Google, GitHub, and LinkedIn; no password database                                                       |
| Preview and publish | Instant local preview, granular autosave, public SSR page, and live refresh                              |
| Discovery           | Search engine optimization (SEO) and generative engine optimization (GEO), only after explicit opt-in    |
| Export              | Owner PDF; optional public PDF                                                                           |
| Mobile              | Deferred until the deployed web v1; the API and document format remain language-neutral                  |

Out of v1: cover letters, a job tracker, AI writing tools, custom domains,
teams, analytics, a multilingual application interface, and collaborative
editing.

## Public namespace

A resume slug is globally unique, 4–30 characters, and matches
`^[a-z0-9]+(-[a-z0-9]+)*$`. Reserved root segments cannot be claimed. A resume
keeps its slug when unpublished. Rename or deletion releases the old slug into a
180-day tombstone so another account cannot immediately take over an old link.
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

## Product boundaries

- Drafts accept partial data. Publish applies a separate completeness policy.
- Public pages disclose resume content, not account data or a list of the
  account's other resumes.
- Publishing explains that public content can be delivered through a global
  content-delivery network. The discovery option separately explains crawler and
  AI-engine access.
- The v1 application interface is English. Vietnamese resume content is a
  first-class fixture and fallback target because the initial community is
  Vietnamese. Other scripts remain valid content; font choices state measured
  coverage instead of claiming universal coverage.
- Accessibility is a release requirement for the editor, publish flow, public
  page, and generated artifacts.
