# 0014: Public page title, emoji favicon, and a stored link-preview card

Status: Accepted (2026-09-19, 2026-09-26). The owner approved the card and its
product-visible choices, listed in the
[link-preview design](../design/link-previews.md#owner-decisions).

## Context

A public resume page's tab read `<full name> — Resume` with the site icon, and
owners want a custom tab title and emoji favicon per resume. The public page
head is closed: the server's HTML validator accepts only the exact values it
expects, so any per-resume head value must be computed and checked by the
server.

A share image captured from the top of the rendered resume was 156 KB, took 1.6
s to render on a cache miss, showed unreadable text at thumbnail size, and
included the email address and phone number. The origin render cache held it for
at most 60 seconds (ADR 0010), so most crawler fetches rendered again. Facebook
asks for content "within a few seconds"; Discord gives a whole fetch 10 seconds.
Chat apps and social networks cache a card by page URL and image URL for days or
longer, so the first card a platform fetches is the one people see.

## Decision

### Title and favicon

1. **Publication settings, not document fields.** `publicTitle` and
   `faviconEmoji` live on the resumes row beside the slug and the download and
   discovery flags. The resume document and its schema version are unchanged.
   The owner sets them through the existing publish request: absent keeps the
   stored value, an empty string clears it, and `null` is malformed. The owner
   resource returns both, `null` when unset. The public JSON does not carry
   them; only the HTML head uses them.
2. **Validation.** The server trims Unicode white space, then:
   - `publicTitle` is at most 70 grapheme clusters and 560 code points
     (`too_long`), with no Unicode control characters and no format characters
     other than U+200D ZERO WIDTH JOINER (`invalid_characters`). That rules out
     bidi overrides and isolates and zero-width characters. The title is text
     and the page escapes it.
   - `faviconEmoji` is exactly one grapheme cluster of at most 16 code points:
     an Extended_Pictographic base followed only by other Extended_Pictographic
     characters, ZWJ, variation selectors, skin-tone modifiers, or tags; or one
     flag of two Regional Indicators (`invalid_emoji`). Grapheme clusters come
     from `github.com/rivo/uniseg` v0.4.7. Extended_Pictographic comes from a
     table generated from Unicode 17.0.0 `emoji-data.txt` by a committed
     generator.
3. **Rendering.** The server computes the exact `<title>`: the owner's title or
   `<full name> — Resume`. When an emoji is set, it also computes the exact
   favicon URL: `data:image/svg+xml,` followed by an SVG holding the emoji in a
   `<text>` element, with every byte outside the URL-unreserved set
   percent-encoded. It passes both to the renderer. The page emits
   `<link rel="icon" href="…">` only when an emoji is set.
4. **Output validation.** The public HTML validator accepts only that exact
   title and, when set, exactly one `<link rel="icon">` with exactly `rel` and
   the exact `href`. Any other icon link, a second one, or an icon when none is
   set is rejected. The page CSP already allows `data:` images through
   `img-src 'self' data:`, which browsers apply to icons.

### Link-preview card

1. The share image is a **preview card**: a fixed layout built for thumbnails
   that shows the name, headline, photo, and aboutme branding, and no contact
   details. Its content comes from a closed card envelope, not the resume
   document, so no contact field can reach it.
2. Go derives a **card version**: the first 16 hex digits of SHA-256 over the
   card layout version and every card input. The page names the image by
   version: `/api/v1/public/resumes/{slug}/og/{version}.png`. Only the current
   version answers; any other version is the ordinary public 404.
3. The card is **rendered ahead of time** when a resume goes live and after any
   committed change that alters the card version, and **stored** in PostgreSQL,
   one row per live resume, holding only the current version.
4. Every read of a stored card passes the ADR 0010 live-state gate first. A
   stored card is never authority. Unpublish, rename, and delete remove the row
   in the same transaction that changes the public state. A card job stores its
   result only while the resume is live and the version is still current.
5. When a crawler asks for the current version before the stored card exists,
   the route joins the pending job or renders once and stores the result.
6. `/api/v1/public/resumes/{slug}/og.png` stays as an alias for the current card
   so cards that platforms fetched earlier keep working.

The stored card is keyed by content, not by public generation. An autosave that
does not touch a card input leaves the version and the stored bytes unchanged,
so a crawler gets the card from one row read instead of a Chromium render.

| Option                 | Result                                                                                                                       |
| ---------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| Render on request      | 1 to 2 s first byte on most crawler fetches; renders compete with owner PDF exports                                          |
| PostgreSQL row, chosen | Deleted by the same transaction as unpublish and by cascade on delete; no cleanup ledger; moves with the database to Vietnam |
| Private object storage | Needs a deletion ledger entry per change and a sweep, like photos; a second copy to keep consistent with the live state      |
| Edge long-TTL cache    | Breaks ADR 0010: an edge copy would outlive unpublish until an eventually consistent invalidation                            |

A card is at most 512 KiB and one row exists per live resume.

## Consequences

- Title and favicon are public, like the slug. They are resume content the owner
  writes, so they add no new category of personal data; the account export
  includes them.
- The editor may pre-validate with the same rules (`app/utils/publicPage.ts`). A
  shared corpus of hostile inputs keeps the Go and TypeScript checks in
  agreement; the server stays authoritative.
- Emoji added after Unicode 17.0 are rejected until the table is regenerated.
- Saving either setting bumps the revision like any publish, so cached pages and
  live readers refresh.
- ADR 0010's 60-second limit for private render caches does not apply to the
  stored card. Its other rules do: gate before reuse, revocation inside the
  mutation transaction, and no-cache responses with a strong entity tag.
- Changing the card layout needs a new layout version, which rebuilds every live
  card through a bounded background sweep and gives each a new URL.
- Platforms refetch an image only when its URL changes. The versioned URL makes
  an edit show on the next scrape; it cannot change a card already fetched.
- A rollback leaves the card table in place and unused. The card is derived
  data, so nothing is lost.

## History

- Former ADR 0032 (2026-09-05): one live-gated 1200 by 630 top-of-resume share
  image rendered on request. Replaced by the stored card because of its render
  cost, unreadable thumbnail, and exposed contact details.
- Former ADR 0042 (2026-09-19): owner-set title and emoji favicon. Unchanged.
- Former ADR 0055 (2026-09-26): the stored, versioned link-preview card.
  Unchanged.
