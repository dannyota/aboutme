# 0029: Community showcase of opted-in resumes

Status: Accepted (2026-09-27). The owner approved every choice marked **Owner
approval** (S1 to S13) in the [showcase design](../design/showcase.md).

## Context

Visitors want to see real resumes before they sign up, and some owners are glad
to show theirs. Today a resume is reachable only by its link or, when its owner
turns on SEO and GEO, through search engines. No page gathers resumes.

Several accepted decisions bound a gathering page. Users have no public page and
the public app has no operator surface (ADR 0003). Every public reuse passes the
live-state gate, and unpublish takes effect at once (ADR 0010). The stored
link-preview card already shows name, headline, and photo without contact
details (ADR 0014). aboutme records nothing about viewers (ADR 0022). The
document stores no template identity.

## Decision

1. **Opt-in per resume.** A publication setting, off by default, lets the owner
   show a live resume on `/showcase`. It needs the resume live with sign in to
   view off. Unpublish, sign in to view, and deletion end it in the same
   transaction; publishing again starts with it off. It is set only in the web
   UI, never by an agent tool.
2. **Review before listing.** A resume is listed only while its review key, a
   hash of the slug, language, and the card's scrubbed name, headline, and
   photo, equals a key the operator approved. Go keeps the key current inside
   every resume write transaction. The operator reviews through an out-of-band
   server command started as a one-shot production task, so the public app gains
   no privileged route.
3. **What a listing holds.** The stored card at its versioned URL, the derived
   template, the resume language, and an optional owner-chosen role from a
   closed list. No contact detail, date, count, or body text.
4. **Order and filters without tracking.** Newest first by first approval.
   Filters for role, language, and template live in the URL. The page sets no
   cookie, uses no browser storage, and runs no counting script.
5. **Never indexed.** `/showcase` is `noindex, nofollow` and outside the sitemap
   and `llms.txt`. A resume's SEO and GEO switch keeps its meaning.
6. **Uncached listing.** The browser reads the listing from a Go route that
   computes it from committed state on every request with `no-store`. A request
   admitted after an opt-out, unpublish, decline, rename, or delete succeeds
   cannot receive the old listing, which meets ADR 0010 without a fence.
7. **Route.** `/showcase` joins the public-root registry with Nuxt dispatch. The
   migration refuses to run while a resume holds that slug.

## Rejected

| Option                                   | Why not                                                                                   |
| ---------------------------------------- | ----------------------------------------------------------------------------------------- |
| Listed at once, with report and takedown | aboutme.vn curates the page, so abuse would stand as its content until someone reports it |
| In-app review queue                      | Adds the operator surface ADR 0003 rules out                                              |
| Resume first-page thumbnail              | A new stored artifact rendered per edit; the body can hold contact details                |
| Popularity order                         | Publishes view counts that ADR 0022 keeps owner-only                                      |
| Indexable showcase                       | Would index people who left SEO off                                                       |
| Cached or aggregate-fenced listing       | Needs a discovery-style fence for mutable names; a per-request read needs none            |

## Consequences

- The operator must review each opt-in, and again after a name, headline, photo,
  slug, or language change. A review backlog delays listing, never exposure.
- A card layout release or a color change does not reset reviews; the review key
  leaves both out.
- One more public read route; each request costs one indexed query of at most 12
  rows and one count. It has its own per-IP rate limit.
- An open showcase tab keeps old tiles until reload; new requests and every card
  and resume fetch follow the current state.
- Operators must not add an in-app review page later without superseding
  ADR 0003.
- A rollback leaves the table unused and the page gone; no data is lost, and
  `showcase` becomes claimable as a slug on the older release.
- The privacy notice and terms gain the text in the design; the next regular
  impact-assessment update notes the showcase.

## History

- Accepted (2026-09-27): the owner approved S1 to S13 in the showcase design as
  written, including the privacy and terms text and no advance email (S11).
