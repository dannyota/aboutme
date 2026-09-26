# Delivery

Schema, API, edge, limits, cost, rollback, tests, and live checks for
[viewer analytics](README.md). Numeric limits live in
[budgets](../budgets.md#viewer-analytics).

## Schema

Migrations are additive. Each release adds its own; the manager assigns the
numbers after the migrations already queued.

| Table or column            | Release         | Shape                                                                                                                                                                                               |
| -------------------------- | --------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `resumes.viewer_mode`      | View counts     | `text not null default 'count'`, check `count`, `ask`, `sign_in`; `ask` and `sign_in` rejected by the API until their release                                                                       |
| `resumes.view_ref`         | View counts     | `bytea not null unique`, 8 random bytes, filled for existing rows                                                                                                                                   |
| `resumes.viewer_terms_at`  | Viewer tracking | `timestamptz null`; the API requires it before `ask` or `sign_in`                                                                                                                                   |
| `resume_view_days`         | View counts     | `(resume_id, day)` key; `counted`, `bot`, `datacenter`, `anomaly`, `invalid`, `crawler` as `integer not null default 0`; cascades with the resume                                                   |
| `resume_share_signal_days` | View counts     | `(resume_id, day, platform)` key; `fetches integer`; `platform` closed set; cascades                                                                                                                |
| `resume_view_events`       | Viewer tracking | Append-only: UUIDv7 `id`, `resume_id`, `viewer_key bytea(16)` or `viewer_id`, exactly one; `viewed_at`, `device`, `city`, `country`, `source`, `expires_at = viewed_at + 90 days`; cascades         |
| `resume_view_durations`    | Viewer tracking | Insert-once: `view_id` primary key referencing the event, `visible_seconds` 0 to 3,600; cascades with the event                                                                                     |
| `view_consents`            | Viewer tracking | Append-only: `id`, `resume_id`, `viewer_key` or `viewer_id`, `kind` (`ask`, `sign_in`), `decision` (`agree`, `decline`, `withdraw`), `notice_version`, `at`, `expires_at = at + 180 days`; cascades |
| `resume_viewers`           | Sign in to view | `id`, `resume_id`, `provider`, `subject`, `name`, nullable `email`, `first_seen`, `last_seen`; unique `(resume_id, provider, subject)`; cascades                                                    |
| `viewer_passes`            | Sign in to view | `(token_hash, resume_id)` key, `viewer_id`, `expires_at = created_at + 7 days`; cascades with the resume and the viewer                                                                             |
| `oauth_transactions`       | Sign in to view | Purpose check gains `view`; nullable `resume_id` required exactly when purpose is `view`                                                                                                            |

`decline` rows hold only resume, decision, version, and time, since a declining
viewer has no key. Every new table grants `aboutme_app` explicitly
([ADR 0038](../../adr/0038-single-baseline-and-plain-migrator.md)). Events never
update; the only deletes are expiry, withdrawal, owner deletion, and cascades.
The privacy sweep deletes expired events, consents, passes, and viewers without
events in bounded batches, and daily rows older than 400 days.

Size: a view event is about 150 bytes with its index entries; 100,000 recorded
views in 90 days is about 15 MB. Daily rows are about 60 bytes per resume per
day.

## API

OpenAPI gains every route; the web client is regenerated.

| Route                                                  | Auth                       | Purpose                                                                  |
| ------------------------------------------------------ | -------------------------- | ------------------------------------------------------------------------ |
| `POST /api/v1/public/resumes/{slug}/views/start`       | None; exact `Origin`, JSON | Token, challenge, mode, consent state, owner flag                        |
| `POST /api/v1/public/views/collect`                    | None; exact `Origin`, JSON | Token, proof, `referrerHost`; 204 always after validation                |
| `POST /api/v1/public/views/duration`                   | None; exact `Origin`       | `sendBeacon` body with a view capability and seconds; tracked views only |
| `POST /api/v1/public/resumes/{slug}/views/consent`     | None; exact `Origin`, JSON | `agree` or `decline` with the notice version; sets cookies               |
| `GET`, `DELETE /api/v1/public/viewer/data`             | Viewer cookies or pass     | Viewer's own records; withdraw and delete one resume or all              |
| `GET /api/v1/views`, `GET /api/v1/resumes/{id}/views`  | Cookie session             | Owner summary and one resume's counts, signals, and events               |
| `PUT /api/v1/resumes/{id}/viewer-mode`                 | Cookie session, CSRF       | Set mode; accepts terms; `sign_in` waits for the revocation fence        |
| `DELETE /api/v1/resumes/{id}/viewer-data`              | Cookie session, CSRF       | Delete all events, viewers, passes, and consents for the resume          |
| `GET /api/v1/auth/{provider}/start?purpose=view&slug=` | None                       | Viewer sign-in start                                                     |

The public `POST` routes accept only `Content-Type: application/json` (the
`sendBeacon` body is a JSON `Blob`) and an `Origin` equal to the canonical
origin, so a cross-site page cannot send them without a CORS preflight, which
the origin never grants. They set no CSRF token because they hold no session
authority. Collect answers 204 for every outcome after validation, so a client
cannot probe which layer rejected it. For the same reason collect always returns
a view capability, a second HMAC over a random value that names the event only
when one was recorded; duration drops a capability that names none.

Owner routes are cookie-only; bearer tokens never reach them, and no MCP tool
reads viewer data. The public start and collect read the session cookie only to
recognize the owner, never to authorize anything.

## Edge and Caddy

- **Caching:** public pages and every new route stay on the default CloudFront
  behavior, `CachingDisabled`, all methods
  ([CloudFront edge](../cloudfront-edge.md#cache-and-forwarding)). Nothing new
  is cached at the edge. The gate and viewer data responses are `no-store`.
- **Origin request policy:** the header list gains `CloudFront-Viewer-Country`
  and `CloudFront-Viewer-City`. CloudFront percent-encodes non-ASCII city names
  ([headers][cf-headers]); Go decodes, NFC-normalizes, and drops a value over 64
  bytes. **Verify:** a viewer-supplied `CloudFront-Viewer-City` is replaced; if
  not, a viewer can only misstate their own place.
- **WAF:** the two managed groups and two label rules in
  [counting](counting.md#layers-2-and-3-edge-labels), all Count, scoped to the
  collect path. WAF logging stays off.
- **Caddy CloudFront listener:** passes the two `CloudFront-Viewer-*` headers
  and the two `x-amzn-waf-aboutme-*` headers to Go, removes every other
  `x-amzn-waf-*` header, and strips all four on any other listener. Go trusts
  them only from loopback.
- **Vietnam move:** vCDN has no known equivalent. At the move, place comes from
  a vCDN geo header if one exists (**Verify** in the Vietnam design) or is
  dropped; layers 2 and 3 turn off; the other layers stay.

## Rate limits

New policies on the existing bounded limiter
([ADR 0018](../../adr/0018-bounded-rate-limiter.md)), per canonical client IP:

| Route                  | Limit                                      |
| ---------------------- | ------------------------------------------ |
| Start                  | 30 per minute                              |
| Collect and duration   | 30 per minute                              |
| Consent                | 20 per minute                              |
| Viewer data            | 30 per minute; `DELETE` 10 per hour        |
| Viewer sign-in start   | The anonymous login start policy           |
| Owner views reads      | The resume read policy per account and IP  |
| Viewer mode and delete | The resume write policy per account and IP |

In-memory bounds: the network key map, the used-nonce set, and the aggregate
buffer each have a fixed entry cap in [budgets](../budgets.md#viewer-analytics).
When the dedupe map is full, the oldest entry of the day is evicted, which can
only double count; when the nonce set is full, start answers 503 and nothing is
counted, which fails closed.

## Cost

| Item                                          | USD per month                                                              |
| --------------------------------------------- | -------------------------------------------------------------------------- |
| Bot Control subscription                      | 10 ([WAF pricing][waf-price])                                              |
| Bot Control and Anonymous IP list rule groups | 2 (1 each; **Verify** on the bill for Bot Control)                         |
| Two label rules                               | 2                                                                          |
| Bot Control requests                          | 0 inside 10 million inspected a month; then 1 per million collect requests |
| WAF requests                                  | 0.60 per million, as today                                                 |
| CloudFront requests                           | 2 to 3 more per page view; inside the free 10 million a month              |
| Database and compute                          | No change at this size                                                     |
| **Total**                                     | **about 14 to 15**                                                         |

## Security

- Public writes carry no authority: a forged collect can at most add one count
  per network per resume per day, under the hourly cap, after proof of work.
- Cookies are `__Host-`, `HttpOnly`, `SameSite=Lax`, set only by the server.
  `__Host-view-pass` stores only a hash server-side.
- The owner flag in start reveals ownership only to a request carrying the
  owner's own session.
- Logs carry route, outcome, and resume ID, never tokens, keys, cookie values,
  city, IP address, user agent, or referrer.
- The popup and invite render text only; no resume data reaches them.
- A `sign_in` resume is behind the revocation fence; see
  [sign in to view](sign-in-to-view.md#gated-routes).

## Older clients and rollback

- The resume HTML does not change; `public-resume.mjs` gains the beacon. The web
  image starts before the app, so a new script meeting an old server gets 404
  from start and does nothing. An old cached page never calls start.
- Editor tabs opened before a release do not show the Viewers panel; the mode
  stays as set.
- Rolling back below view counts or viewer tracking leaves new tables and
  columns unused; nothing public depends on them.
- Rolling back below sign in to view would serve `sign_in` resumes publicly. The
  sign in to view release raises the release fence to itself
  ([release fence](../passkey-release-fence.md)), so `--rollback` below it is
  refused. To go lower on purpose, first set every `sign_in` resume to `count`
  with an operator command, then lower the fence.

## Tests

Tests cite this design, ADRs 0060 to 0062, or `AC-*` IDs.

- Go, counting: each layer's outcome with an injected clock and random source;
  token tag, age bounds, replay; ALTCHA verify with a solved fixture; label
  headers from loopback only; dedupe per day with the key rolled at midnight
  Asia/Ho_Chi_Minh; owner exclusion and owner network marking; hourly cap;
  crawler list cases; buffer flush and shutdown flush; live gate re-check.
- Go, tracking: consent record before any event; decline stores no key; view key
  derivation vector; field classifiers (device, source, city decoding); duration
  insert-once; withdrawal deletes in one transaction; sweep expiry.
- Go, sign in: gate for every gated route with and without a pass; `view`
  purpose never touches accounts or sessions, including a subject that belongs
  to an account; mode change to `sign_in` waits for the fence; mode change away
  revokes passes; unverified email not stored.
- Store: migrations, grants, cascades, and constraint checks.
- Web: popup and invite in both languages, equal buttons, no stacking, bottom
  padding on phones, close stored; the script sends nothing before 8 s and one
  trusted event; `sendBeacon` at `pagehide`; the Views pages; the viewer data
  page; privacy notice text.
- Browser proofs: the dev-https public check gains count, consent agree and
  decline, withdrawal, and gate sign-in through the mock provider; public page
  pixel baselines with each overlay at phone and desktop widths.
- Infrastructure: WAF rules are Count only with the scope-down; the origin
  request policy lists the new headers; Caddy strips the headers per listener.

## Live checks

After each deploy, on a fictional published resume, evidence under
`.dev/prod-checks/viewer-analytics/`:

1. From a phone on mobile data, open the resume, wait 10 s, scroll; the Views
   page shows one more real view within 2 minutes.
2. Repeat on the same network the same day: no change. Signed in as the owner:
   no change.
3. `curl` the page and post a replayed collect: no view; M rises by one
   `invalid` for the collect.
4. Share the link in a Zalo draft and a Messenger draft: "Shared in Zalo" and
   "Shared in Facebook" appear; confirm Zalo's user agent token.
5. From an EC2 instance with headless Chromium: `bot` or `datacenter` rises.
6. WAF sampled requests show the label rules matching and no block.
7. `ask`: the popup shows; decline leaves no `__Host-view-id`; agree records one
   event with device, city, and source; `/privacy/viewer` lists it; withdraw
   deletes it.
8. `sign_in`: anonymous `/{slug}`, JSON, PDF, and live stream are gated; Google
   sign-in shows the resume and the invite after scrolling; closing the invite
   keeps it closed on reload; the owner sees the name and email.
9. Switch back to `count`: the pass stops working at once.

[cf-headers]:
  https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/adding-cloudfront-headers.html
[waf-price]: https://aws.amazon.com/waf/pricing/
