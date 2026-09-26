# Link previews

A shared public resume link (`https://aboutme.vn/<slug>`) shows a clear card in
chat apps and social networks, with no new setting. The page head carries a
title, a description from the resume summary, the language, the canonical URL,
and a preview card image built for thumbnails. The card holds no contact
details. [ADR 0055](../adr/0055-stored-link-preview-card.md) records the stored
card and the choices it replaces.

Status: accepted. **Verify** marks a fact that comes from community sources or
inference and that the live checks must confirm.

## Owner decisions

The owner settled every product-visible choice:

1. The card is built at publish and rebuilt when it changes.
2. The description is the resume summary.
3. **Card look.** The card follows the template accent, so it matches the page
   the reader opens ([Preview card](#preview-card)). The owner chose it over a
   fixed brand blue after comparing mockups.
4. **Preview title.** The public page title when set, otherwise the full name
   alone. The default tab title ends in the English word "Resume" even on a
   Vietnamese resume, and Apple and Facebook ask for a title without site
   branding.
5. **Publish-panel preview.** It ships in the same release as the card.
6. **Privacy notice.** The wording in [Privacy](#privacy). The frontend adds the
   Vietnamese text, and the owner reviews it before that release ships.

## Current state

Measured on production on 2026-09-26 for `/danny`: the page sends only
`og:image`, its width and height, `twitter:card`, and `twitter:image`. The image
is a 156 KB top crop of the resume with the email and phone visible. It took 1.6
s to render on a miss and returns `Cache-Control: no-cache, must-revalidate`
with `X-Cache: Miss from cloudfront`. The HTML first byte took 0.39 s from one
client.

## Platforms

Official documentation first. "Silent" means the platform publishes no rule.

| Platform        | Tags read                                                                                                             | Image rules                                                                                                      | Fetch limits                                                                                    |
| --------------- | --------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| Facebook        | `og:url`, `og:type`, `og:title`, `og:description`, `og:image` and its width, height, type; `og:locale` [fb-wm]        | Min 200 by 200; 1200 by 630 recommended; at most 8 MB; keep near 1.91:1 to avoid cropping [fb-img]               | Crawl "within a few seconds"; may send `Range: bytes=0-524288` [fb-crawl]                       |
| Messenger       | Silent; uses Facebook's crawler (inferred)                                                                            | As Facebook (inferred)                                                                                           | As Facebook (inferred)                                                                          |
| Zalo            | Silent. Community threads show `og:title`, `og:description`, `og:image` [zalo-c]                                      | Silent; 1200 by 630 is common practice (**Verify**)                                                              | Silent                                                                                          |
| LinkedIn        | Requires `og:title`, `og:image`, `og:description`, `og:url` [li-share]                                                | Min 1200 by 627; 1.91:1; at most 5 MB; under 401 px wide shows as a thumbnail; square images may crop [li-share] | Silent                                                                                          |
| X               | Official card pages no longer load (HTTP 402). Former rule: X tags first, then Open Graph fallback (**Verify**)       | Community: 2:1, min 300 by 157, at most 5 MB [x-comm]                                                            | Silent                                                                                          |
| Telegram        | Silent. `TelegramBot (like TwitterBot)` reads Open Graph (community)                                                  | Silent                                                                                                           | Silent                                                                                          |
| WhatsApp        | Non-empty `og:title`, `og:description`, `og:url` in a head within the first 300 KB [wa]                               | Under 600 KB; at least 300 px wide; ratio at most 4:1 [wa]                                                       | Built on the sender's device; description shows 1 or 2 lines, "80 characters will suffice" [wa] |
| Slack           | oEmbed, X card, and Open Graph tags [slack]                                                                           | Fetched again by `Slack-ImgProxy` to validate [slack]                                                            | Fetches as little as it can with `Range` [slack]                                                |
| Discord (draft) | Open Graph and X tags; `twitter:card` `summary_large_image` gives the large image; `theme-color` sets the accent [dc] | Declare width and height or the image may be dropped; default layout is a small thumbnail [dc]                   | No JavaScript; 2xx; at most 5 redirects; whole fetch within 10 s [dc]                           |
| iMessage        | `og:title`, `og:image`, `og:site_name`; `og:description` only for social posts [apple]                                | At least 900 px wide; under 150 px is ignored; avoid text in images [apple]                                      | No JavaScript, no meta redirects; page at most 1 MB; images and icons at most 10 MB [apple]     |

| Platform  | Cache and refresh                                                                                                           |
| --------- | --------------------------------------------------------------------------------------------------------------------------- |
| Facebook  | Images are cached by URL and "won't be updated unless the URL changes" [fb-wm]; the Sharing Debugger scrapes again [fb-dbg] |
| Messenger | As Facebook (inferred)                                                                                                      |
| Zalo      | Silent on lifetime; the official sharing debugger has a recollect button (Thu thập lại) [zalo-dbg]                          |
| LinkedIn  | Post Inspector refreshes new posts only; existing posts keep their preview [li-pi]; lifetime silent (about 7 days reported) |
| X         | Silent; no public validator                                                                                                 |
| Telegram  | Silent; the official @WebpageBot refreshes a URL; sent messages keep their preview (community)                              |
| WhatsApp  | Silent; each preview is built on the device when composing                                                                  |
| Slack     | Cached for about 30 minutes across the service [slack]                                                                      |
| Discord   | Cached for about 30 minutes; the Embed Debugger shows what it reads [dc]                                                    |
| iMessage  | Silent                                                                                                                      |

Discord's rules come from its link-preview documentation, still an open pull
request [dc]. Crop behavior differs by layout. Facebook, LinkedIn, X, and
Discord's large layout show the wide image. Discord's default layout, small
LinkedIn images, and small chat thumbnails (Slack, and Zalo and Telegram in some
views) show a small, often square crop (**Verify** per app). The card therefore
keeps everything that identifies the person inside the centered square.

Only WhatsApp documents `Accept-Language`, and only Apple and Discord state that
they run no JavaScript. Neither matters here: the Go gateway renders the full
head on the server, and the page language comes from the resume's
`metadata.lng`, never from request headers.

## Page head

Every live resume page carries exactly these head elements. The Go HTML
validator accepts each one once, with the exact value Go computed, and rejects a
missing, repeated, or extra one.

| Element                     | Value                                                              |
| --------------------------- | ------------------------------------------------------------------ |
| `<title>`                   | ADR 0042 title, unchanged                                          |
| `link rel="canonical"`      | `https://aboutme.vn/<slug>`, unchanged                             |
| `meta name="description"`   | Description                                                        |
| `og:type`                   | `profile`                                                          |
| `og:site_name`              | `aboutme.vn`                                                       |
| `og:title`                  | Preview title                                                      |
| `og:description`            | Description                                                        |
| `og:url`                    | Canonical URL                                                      |
| `og:locale`                 | Locale; omitted when none maps                                     |
| `og:image`                  | `https://aboutme.vn/api/v1/public/resumes/<slug>/og/<version>.png` |
| `og:image:type`             | `image/png`                                                        |
| `og:image:width`, `:height` | `1200`, `630`                                                      |
| `og:image:alt`              | Image text                                                         |
| `twitter:card`              | `summary_large_image`                                              |
| `twitter:image`             | Same URL as `og:image`                                             |
| `twitter:image:alt`         | Image text                                                         |

The page sends no `twitter:title` or `twitter:description`; X and Discord fall
back to Open Graph. It sends no `theme-color`, which would also tint the mobile
browser bar of the public page. The renderer escapes every value for a
double-quoted attribute.

## Text rules

Go derives every value from the admitted public snapshot, the validated public
title, and the slug. The snapshot already drops hidden entries, hidden contact
details, and sections with no visible entry, and it sanitizes rich text.

**Normalize** each source string: replace control and bidirectional formatting
characters (U+061C, U+200E, U+200F, U+202A to U+202E, U+2066 to U+2069) with a
space, collapse white space runs to one space, and trim. Count length in
grapheme clusters with `github.com/rivo/uniseg`; never split a cluster.

**Scrub** contact data from the description, the image text, the card text, and
a title built from the name: remove any token that looks like an email address
(`local@domain.tld`) and any run of nine or more digits that may include spaces,
dots, dashes, parentheses, and a leading `+`. Also remove any exact visible
contact detail value. Normalize again after removal. An owner-set public title
is used as written.

**Preview title**: the public title when set, else the full name cut to 70
clusters, else `aboutme.vn/<slug>`.

**Description**, at most 160 clusters:

1. The summary: the first `profile` section in layout order (main, then
   sidebar), its visible entries as plain text. Block and list-item ends become
   a space; entities are decoded.
2. When the summary is empty after scrubbing: the headline and the latest role
   joined by a middle dot with a space on each side, or whichever exists. The
   latest role is the first visible entry of the first `work` section in layout
   order, as `<job title>, <employer>`, or whichever field exists.
3. When both are empty: `CV trên aboutme.vn` for a Vietnamese resume,
   `Resume on aboutme.vn` for any other language.

**Cut** a description longer than 160 clusters: keep the longest run of whole
sentences that fits when it is at least 60 clusters. Otherwise cut at the last
space at or before cluster 159, drop trailing `,;:–-` and spaces, and append
`…`. With no space in the last 40 clusters, as in Chinese or Japanese text, cut
at cluster 159. Every platform reads the same tag, so one value serves all, and
the page does not vary by user agent. WhatsApp documents about 80 characters and
Facebook asks for 2 to 4 sentences; the others publish no limit, and Zalo shows
about two lines (**Verify**). Keeping whole sentences first puts a complete
first sentence in front of apps that show less.

**Locale**: `vi` becomes `vi_VN`. `en` becomes `en_US`. A tag with an explicit
region becomes `language_REGION`, such as `en-GB` to `en_GB`. Any other language
and `und` omit `og:locale`.

**Image text**: the name, plus a spaced middle dot and the headline when the
card shows it, cut to 200 clusters; `aboutme.vn/<slug>` when there is no name.

## Preview card

The card is a 1200 by 630 PNG built from a closed card envelope. The envelope
holds only: card layout version, language, slug, name, headline, photo (the
normalized inline image and its crop), and accent color. It has no contact,
section, or document field, so no email, phone, or address can reach the image.
Name and headline pass the same normalize and scrub rules; a field that loses a
scrubbed token is left off the card.

Layout rules; the designer owns the final spec in
[Link-preview card](link-preview-card.md):

- White paper background, `--paper-ink` text, fixed `Be Vietnam Pro` at 400 and
  700, which covers Vietnamese. The template's font does not apply.
- Everything that identifies the person sits in the centered safe square, x 315
  to 885 and y 15 to 615, so a square crop and X's 2:1 crop keep it.
- Photo, when present: a circle about 208 px wide at the top of the square, from
  the document photo and its crop. Without a photo, the name and headline center
  vertically. No initials, since name order differs between Vietnamese and
  English.
- Name: bold, 72 px, stepping down to 52 px as it grows, at most two lines.
- Headline: regular, 32 px, `--paper-muted`, at most two lines.
- Footer inside the square: the aboutme mark and `aboutme.vn/<slug>`.
- Accent: the template accent (`colors.accent`, else `colors.primary`), clamped
  to 3:1 against white like `--color-accent-solid`, used only for decoration
  outside the safe square and for the photo ring. Text never takes the accent.
- At 300 px wide, the size of a phone chat card, the name renders at about 18
  px. The headline is secondary and may be small there.
- Apple advises against text in preview images. The card keeps its text large
  and short, and every app also shows the title as text.

Size: at most 524,288 bytes, under WhatsApp's 600 KB; the expected size is 60 to
250 KB. The card page carries no script and uses the print CSP.

## Build, storage, and serving

**Version.** Go hashes the card envelope inputs (layout version, slug, language,
name, headline, photo storage key digest and crop, accent) with SHA-256 and
keeps the first 16 hex digits. The storage key itself never leaves Go. A web
test pins a hash of the card component and CSS, so a layout change fails CI
until the layout version is raised.

**When a card builds.** A Go card scheduler:

1. builds at once when a resume goes live or is renamed;
2. after a committed change to a live resume, waits for 10 seconds without
   further changes, then builds only if the version changed. It learns of
   changes from the PostgreSQL notifications the realtime hub already uses, so
   editor, agent, and publish writes all count; and
3. at start, sweeps live resumes whose stored version is not current, one at a
   time, one start every 5 seconds.

Scheduled builds use the existing render queue and one-use print capability at
low priority. A scheduled build enters the queue only while at most 2 of its 8
places are taken, never makes readiness fail, and retries after 30 seconds up to
5 times. Owner PDF exports keep their place.

**What the page names before the build ends.** The page always names the current
version. If a crawler asks before the stored card exists, the route joins the
pending build for that resume or starts one, waits within the 20 s render
deadline, and serves the result. A resume shared seconds after publish therefore
gets the right card, not a placeholder a platform would cache.

**Storage.** Table `resume_preview_cards`: `resume_id` (primary key, references
`resumes` with `ON DELETE CASCADE`), `version` (16 lowercase hex), `png`
(`bytea`, 1 to 524,288 bytes), `rendered_at`. `aboutme_app` gets select, insert,
update, and delete. A build stores its result in one transaction that locks the
resume row for share, recomputes the version from the committed row, and writes
only if the resume is live and the version matches. Unpublish, rename, and
delete lock the row for update, so they serialize with a store.

**Route.** `GET` and `HEAD` `/api/v1/public/resumes/{slug}/og/{version}.png`,
with `version` matching `^[0-9a-f]{16}$` and no query. The route takes the ADR
0022 lease, computes the current version, and returns the public 404 for any
other version. It serves `image/png`,
`Cache-Control: no-cache, must-revalidate`, a strong entity tag over the bytes,
and `X-Robots-Tag: noindex, noarchive` when discovery is off. `/og.png` serves
the same current card. `robots.txt` gains `Allow: /api/v1/public/resumes/*/og/`.
The per-IP limits stay: 300 artifact reads and 20 render misses a minute.

**Edge and first byte.** CloudFront keeps passing public routes through uncached
([CloudFront edge](cloudfront-edge.md#cache-and-forwarding)). A stored card
costs the gate read and one row read at the origin, so its first byte is close
to the page's 0.39 s. A revalidating edge behavior for the card is left out: it
would save little, and whether CloudFront stores a `no-cache` response at
minimum TTL 0 is unclear in its documentation [cf-exp].

**Cost.** One card render of about 1 second per card change, on the existing
host. At most 512 KiB of database storage per live resume. About 150 KB of
transfer per crawler fetch. No new cloud resource.

## Privacy

- Unpublished, private, renamed, and deleted resumes expose nothing: the page,
  both image routes, and every old version return the same public 404, and the
  stored row is gone in the same transaction.
- Preview text and the card come only from the public snapshot. Hidden entries,
  hidden contact details, sections outside the layout, and the storage key never
  reach them. The card never shows contact details; the description drops
  anything that looks like one.
- Link sharing stays independent of discovery. A resume with discovery off gets
  the same tags and card, keeps `noindex`, and gets no JSON-LD.
- Logs carry closed reason codes only, never the slug, text, or version.
- A platform keeps what it fetched. Unpublishing stops new fetches; it cannot
  remove a card already shown.

Privacy notice changes, English text; the frontend adds the Vietnamese text for
the owner to review:

- "What we collect", Content: "the resumes you write, the photos you upload,
  and, while a resume is public, the preview image we make from it for link
  previews."
- "Public only by your choice", new paragraph: "When anyone shares your public
  link in a chat app or social network, that service fetches the page's title,
  summary, and preview image (your name, headline, and photo) and may keep its
  own copy after you unpublish."

## Crawlers, WAF, and rate limits

The web ACL runs the Amazon IP reputation group, whose rules block IPs "actively
engaging in malicious activities" and reconnaissance, and count DDoS sources
[waf-ip]. Nothing in its description targets platform crawlers, which run from
the platforms' own networks, so a block is unlikely (inference, **Verify**).
Sharing one link costs two to three requests, far under the rate rule's 2,000
per 5 minutes per IP. The web ACL must not gain the Anonymous IP list or Bot
Control in blocking mode without an allow rule for the crawlers above: the
Anonymous IP list blocks hosting providers by default [waf-ip]. Viewer analytics
adds both in Count mode only, scoped to its collect path
([counting](viewer-analytics/counting.md#layers-2-and-3-edge-labels)). With WAF
logging off, devops checks the web ACL's sampled requests for blocked crawler
user agents during the live checks.

## Compatibility, loss, and rollback

- No resume document, schema, MCP tool, or public JSON change. OpenAPI gains the
  versioned image route. Older web clients need no update.
- Cards platforms fetched from `/og.png` keep working through the alias.
- The HTML cache format version rises, so no cached page without the new head is
  served.
- The migration only adds the table. A rollback leaves it unused; the card is
  derived, so nothing is lost, and the start sweep refills it on the next
  release.
- The account export leaves the card out: it is derived from exported data.

## Security and size

The route parses the version strictly and reads no query. Nuxt decodes the card
envelope against a closed schema and rejects unknown keys. The accent is a
schema-validated hex color; text enters the page as escaped text; the photo is
the normalized inline image the PDF path already uses. The head adds about 1.6
KB, and the render request adds at most 2 KB, inside the 532,480-byte request
bound; a test with the largest valid document proves the fit.

## Publish-panel preview

The publish dialog can show the card and a neutral chat card with the title and
description, with no new setting. The dialog renders the same card component in
the browser, so it works before the first publish and needs no new route. A
TypeScript copy of the text rules checks against the Go rules with one shared
fixture set, as ADR 0042 does for the public title.

## Tests

- Go: text rules per language (`vi`, `en`, `en-GB`, `fr`, `und`), truncation at
  and past each limit, sentences first, clusters, scrubbing, and each fallback.
  Unique sentinel strings sit in hidden profile and work entries, hidden
  contacts, and a profile section outside the layout, and must not appear.
- Go: validator acceptance and each rejection, version mismatch 404, store
  refused after unpublish, the stale-row case, and the alias.
- Web: head snapshots for a Vietnamese and an English resume; the card envelope
  decoder rejects any extra key.
- Contact data: a fixture with every contact type carrying a sentinel; the card
  envelope Go sends and the card page text contain none of them.
- Pixels: card baselines for Vietnamese with photo, English without photo,
  longest name and headline, and a light and a dark accent. A geometry check
  proves the photo, name, and headline boxes sit inside the safe square.
- Size: the noisiest photo fixture stays under 524,288 bytes.

Live checks after each deploy, on a fictional published resume, with drafts that
are never sent: Facebook Sharing Debugger, Messenger draft, Zalo sharing
debugger and a Zalo chat draft, LinkedIn Post Inspector, X draft, Telegram draft
and @WebpageBot, WhatsApp draft, Slack message preview, Discord Embed Debugger,
and an iMessage draft. Each check records the title, description, image, and
crop, then the same after unpublish.

[fb-wm]: https://developers.facebook.com/documentation/sharing/webmasters.md
[fb-img]:
  https://developers.facebook.com/documentation/sharing/webmasters/images.md
[fb-crawl]:
  https://developers.facebook.com/documentation/sharing/webmasters/web-crawlers.md
[fb-dbg]: https://developers.facebook.com/tools/debug/
[zalo-c]: https://developers.zalo.me/community/detail/1de172004e45a71bfe54
[zalo-dbg]: https://developers.zalo.me/tools/debug-sharing
[li-share]:
  https://www.linkedin.com/help/linkedin/answer/a521928/making-your-website-shareable-on-linkedin?lang=en
[li-pi]: https://www.linkedin.com/help/linkedin/answer/a6269011
[x-comm]: https://devcommunity.x.com/t/twitter-card-summary-large-image/144086/2
[wa]:
  https://developers.facebook.com/documentation/business-messaging/whatsapp/link-previews
[slack]: https://api.slack.com/robots
[dc]: https://github.com/discord/discord-api-docs/pull/8606
[apple]:
  https://developer.apple.com/documentation/technotes/tn3156-create-rich-previews-for-messages
[waf-ip]:
  https://docs.aws.amazon.com/waf/latest/developerguide/aws-managed-rule-groups-ip-rep.html
[cf-exp]:
  https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/Expiration.html
