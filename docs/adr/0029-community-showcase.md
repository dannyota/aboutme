# 0029: Community showcase of opted-in resumes

Status: Accepted (2026-09-27; amended 2026-10-01). The owner approved the
choices marked **Owner approval** in the
[showcase design](../design/showcase.md); N1 to N4 await the owner.

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
2. **Listed without review.** A resume is listed while its opt-in exists, it is
   live, and sign in to view is off. Nothing is reviewed before listing or after
   an edit. Each tile has a Report link that opens an email; the operator
   handles a breach by blocking the account under the Terms. Go keeps the card
   version and derived template current inside every resume write transaction.
3. **What a listing holds.** The stored card at its versioned URL, the derived
   template, the resume language, and an optional owner-chosen role from a
   closed list. No contact detail, date, count, or body text.
4. **Order and filters without tracking.** Newest first by opt-in time. Filters
   for role, language, and template live in the URL. The page sets no cookie,
   uses no browser storage, and runs no counting script.
5. **Never indexed.** `/showcase` is `noindex, nofollow` and outside the sitemap
   and `llms.txt`. A resume's SEO and GEO switch keeps its meaning.
6. **Uncached listing.** The browser reads the listing from a Go route that
   computes it from committed state on every request with `no-store`. A request
   admitted after an opt-out, unpublish, rename, or delete succeeds cannot
   receive the old listing, which meets ADR 0010 without a fence.
7. **Route.** `/showcase` joins the public-root registry with Nuxt dispatch. The
   migration refuses to run while a resume holds that slug.

## Rejected

| Option                               | Why not                                                                                |
| ------------------------------------ | -------------------------------------------------------------------------------------- |
| Review before listing                | Delays every opt-in and costs operator time; Report and the Terms handle abuse instead |
| A hide command for reported listings | A second takedown path beside account blocking                                         |
| In-app review or takedown page       | Adds the operator surface ADR 0003 rules out                                           |
| Resume first-page thumbnail          | A new stored artifact rendered per edit; the body can hold contact details             |
| Popularity order                     | Publishes view counts that ADR 0022 keeps owner-only                                   |
| Indexable showcase                   | Would index people who left SEO off                                                    |
| Cached or aggregate-fenced listing   | Needs a discovery-style fence for mutable names; a per-request read needs none         |

## Consequences

- Abuse can stand on a page aboutme.vn hosts until someone reports it and the
  operator blocks the account. Account blocking is not built yet; until it is, a
  reported listing has no takedown.
- Turning the switch off and on moves a resume to the top of the order.
- One more public read route; each request costs one indexed query of at most 12
  rows and one count. It has its own per-IP rate limit.
- An open showcase tab keeps old tiles until reload; new requests and every card
  and resume fetch follow the current state.
- Operators must not add an in-app review or takedown page later without
  superseding ADR 0003.
- Removing review keeps the schema expand-only: the release before it still
  runs, lists nothing, and never lists a resume the new rules would not.
- The privacy notice and terms carry the text in the design, with no review
  claim; the next regular impact-assessment update notes the showcase.

## History

- Accepted (2026-09-27): the owner approved S1 to S13 in the showcase design as
  written, including the privacy and terms text and no advance email (S11).
- Amended (2026-10-01): owner decision 2026-10-01: no review. An eligible opt-in
  is listed at once, abuse is handled by Report and account blocking under the
  Terms, and the out-of-band review command is removed.
