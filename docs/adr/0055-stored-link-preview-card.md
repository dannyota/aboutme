# 0055: Stored link-preview card replaces the top-crop share image

Status: Accepted (2026-09-26). The owner approved the card and its
product-visible choices, listed in the
[link-preview design](../design/link-previews.md#owner-decisions). This record
supersedes [ADR 0032](0032-public-share-image.md) and amends
[ADR 0022](0022-public-artifact-revocation.md) for one stored artifact.

## Context

ADR 0032 serves a 1200 by 630 capture of the top of the rendered resume. On
production on 2026-09-26 that image was 156 KB, took 1.6 s to render on a cache
miss, showed unreadable text at thumbnail size, and included the email address
and phone number. The origin render cache holds it for at most 60 seconds (ADR
0022), so most crawler fetches render again. Facebook asks for content "within a
few seconds"; Discord gives a whole fetch 10 seconds.

Chat apps and social networks cache a card by page URL and image URL for days or
longer, so the first card a platform fetches is the one people see.

## Decision

1. The share image becomes a **preview card**: a fixed layout built for
   thumbnails that shows the name, headline, photo, and aboutme branding, and no
   contact details. Its content comes from a closed card envelope, not the
   resume document, so no contact field can reach it.
2. Go derives a **card version**: the first 16 hex digits of SHA-256 over the
   card layout version and every card input. The page names the image by
   version: `/api/v1/public/resumes/{slug}/og/{version}.png`. Only the current
   version answers; any other version is the ordinary public 404.
3. The card is **rendered ahead of time** when a resume goes live and after any
   committed change that alters the card version, and **stored** in PostgreSQL,
   one row per live resume, holding only the current version.
4. Every read of a stored card passes the ADR 0022 live-state gate first. A
   stored card is never authority. Unpublish, rename, and delete remove the row
   in the same transaction that changes the public state. A card job stores its
   result only while the resume is live and the version is still current.
5. When a crawler asks for the current version before the stored card exists,
   the route joins the pending job or renders once and stores the result.
6. `/api/v1/public/resumes/{slug}/og.png` stays as an alias for the current card
   so cards that platforms fetched earlier keep working.

## Why stored, and why PostgreSQL

The stored card is keyed by content, not by public generation. An autosave that
does not touch a card input leaves the version and the stored bytes unchanged,
so a crawler gets the card from one row read instead of a Chromium render.

| Option                    | Result                                                                                                                       |
| ------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| Render on request (today) | 1 to 2 s first byte on most crawler fetches; renders compete with owner PDF exports                                          |
| PostgreSQL row, chosen    | Deleted by the same transaction as unpublish and by cascade on delete; no cleanup ledger; moves with the database to Vietnam |
| Private object storage    | Needs a deletion ledger entry per change and a sweep, like photos; a second copy to keep consistent with the live state      |
| CloudFront long-TTL cache | Breaks ADR 0022: an edge copy would outlive unpublish until an eventually consistent invalidation                            |

A card is at most 512 KiB and one row exists per live resume, so the table stays
small next to resume documents.

## Consequences

- ADR 0022's limit of 60 seconds for private render caches does not apply to the
  stored card. Its other rules do: gate before reuse, revocation inside the
  mutation transaction, and no-cache responses with a strong entity tag.
- Changing the card layout needs a new layout version, which rebuilds every live
  card through a bounded background sweep and gives each a new URL.
- Platforms refetch an image only when its URL changes. The versioned URL makes
  an edit show on the next scrape; it cannot change a card already fetched.
- A rollback leaves the table in place and unused. The card is derived data, so
  nothing is lost.
