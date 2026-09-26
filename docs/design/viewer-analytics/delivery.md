# Delivery

Schema, API, edge, limits, cost, rollback, tests, and live checks for
[viewer analytics](README.md). Numeric limits live in
[budgets](../budgets.md#viewer-analytics).

## Schema

Migrations are additive. Each release adds its own; the manager assigns the
numbers after the migrations already queued.

| Table or column            | Release         | Shape                                                                                                                                                       |
| -------------------------- | --------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `resume_view_days`         | View counts     | `(resume_id, day)` key; `counted`, `bot`, `datacenter`, `anomaly`, `invalid`, `crawler` as `integer not null default 0`, each ≥ 0; cascades with the resume |
| `resume_share_signal_days` | View counts     | `(resume_id, day, platform)` key; `fetches integer ≥ 0`; `platform` closed set; cascades                                                                    |
| `resumes.sign_in_to_view`  | Sign in to view | `boolean not null default false`                                                                                                                            |
| `resumes.view_pass_epoch`  | Sign in to view | `integer not null default 0`; raised each time sign-in is turned on                                                                                         |
| `oauth_transactions`       | Sign in to view | Purpose check gains `view`; nullable `resume_id` required exactly when purpose is `view`                                                                    |

View counts is migration `00008_resume_view_counts.sql`. Every new table grants
`aboutme_app` explicitly
([ADR 0038](../../adr/0038-single-baseline-and-plain-migrator.md)). A `day` is
an Asia/Ho_Chi_Minh date. Both day tables have a `day` index for the privacy
sweep, which deletes rows older than 400 days in bounded pages. No resume column
is added for counts: the view token carries the resume ID sealed
([counting](counting.md#layers-5-and-6-token-and-proof-of-work)). No table holds
anything about a viewer.

Size: daily rows are about 60 bytes per resume per day, so 1,000 resumes kept
400 days is about 24 MB.

## API

OpenAPI holds every route; the web client is regenerated with it.

| Route                                                  | Auth                       | Purpose                                                         |
| ------------------------------------------------------ | -------------------------- | --------------------------------------------------------------- |
| `POST /api/v1/public/resumes/{slug}/views/start`       | None; exact `Origin`, JSON | Token and challenge, or the owner flag                          |
| `POST /api/v1/public/views/collect`                    | None; exact `Origin`, JSON | Token, challenge, solution; 204 always after validation         |
| `GET /api/v1/views`                                    | Cookie session             | Owner summary: 7, 30, and 90 days per resume                    |
| `GET /api/v1/views/{id}`                               | Cookie session             | One resume: 90 daily rows, 12 months, link previews by platform |
| `GET /api/v1/auth/{provider}/start?purpose=view&slug=` | None                       | Viewer sign-in start (sign in to view)                          |

The public `POST` routes accept only a single `application/json` media type and
an `Origin` equal to the canonical origin, so a cross-site page cannot send them
without a CORS preflight, which the origin never grants. They check method,
Origin, media type, and a 4 KiB body bound before any session or database work.
They set no CSRF token because they hold no session authority. Collect answers
204 for every outcome after validation, so a client cannot probe which layer
rejected it.

Owner routes are cookie-only; bearer tokens never reach them, and no MCP tool
reads counts. The public start and collect read the session cookie only to
recognize the owner, never to authorize anything. Sign in to view changes the
publish route's request with its switch in its own release.

## Edge and Caddy

- **Caching:** public pages and every new route stay on the default CloudFront
  behavior, `CachingDisabled`, all methods
  ([CloudFront edge](../cloudfront-edge.md#cache-and-forwarding)). Nothing new
  is cached at the edge. The gate is `no-store`.
- **Origin request policy:** unchanged. No geography header is forwarded.
- **WAF:** the two managed groups and two label rules in
  [counting](counting.md#layers-2-and-3-edge-labels), all Count, scoped to
  `POST` on the collect path. WAF logging and sampled requests stay off.
- **Caddy CloudFront listener:** passes `x-amzn-waf-aboutme-bot` and
  `x-amzn-waf-aboutme-dc` to Go and removes every other `x-amzn-waf-*` header;
  every other listener strips both. Go reads them only from its trusted proxy
  and only as exactly one value `1`.
- **Vietnam move:** vCDN has no known equivalent. At the move, layers 2 and 3
  turn off; the other layers stay.

## Rate limits

New policies on the existing bounded limiter
([ADR 0018](../../adr/0018-bounded-rate-limiter.md)):

| Route                | Limit                                              |
| -------------------- | -------------------------------------------------- |
| Start                | 30 per minute per client IP                        |
| Collect              | 30 per minute per client IP                        |
| Owner views reads    | 600 per minute per account and IP, as resume reads |
| Viewer sign-in start | The anonymous login start policy                   |

In-memory bounds: the network key map, the used view-ID set, the share-signal
dedupe set, and the aggregate buffer each have a fixed entry cap in
[budgets](../budgets.md#viewer-analytics). When the network map is full, the
oldest entry of the day is evicted, which can only double count; when the
used-ID set is full, start answers 503 and nothing is counted, which fails
closed; when the buffer is full, the outcome is dropped and logged.

## Cost

| Item                                          | USD per month                                                              |
| --------------------------------------------- | -------------------------------------------------------------------------- |
| Bot Control subscription                      | 10 ([WAF pricing][waf-price])                                              |
| Bot Control and Anonymous IP list rule groups | 2 (1 each; **Verify** on the bill for Bot Control)                         |
| Two label rules                               | 2                                                                          |
| Bot Control requests                          | 0 inside 10 million inspected a month; then 1 per million collect requests |
| WAF requests                                  | 0.60 per million, as today                                                 |
| CloudFront requests                           | 2 more per page view; inside the free 10 million a month                   |
| Database and compute                          | No change at this size                                                     |
| **Total**                                     | **about 14 to 15**                                                         |

## Security

- Public writes carry no authority: a forged collect can at most add one count
  per network per resume per day, under the hourly cap, after proof of work.
- The owner flag in start reveals ownership only to a request carrying the
  owner's own session.
- Logs carry route, status, and outcome, never tokens, keys, cookie values, IP
  address, user agent, or referrer.
- The pass cookie of sign in to view is `__Host-`, `HttpOnly`, `SameSite=Lax`,
  and set only by the server. A `sign_in` resume is behind the revocation fence;
  see [sign in to view](sign-in-to-view.md#gated-routes).

## Older clients and rollback

- The resume HTML does not change; `public-resume.mjs` gains the beacon. The web
  image starts before the app, so a new script meeting an old server gets 404
  from start and does nothing. An old cached page never calls start.
- Rolling back below view counts leaves the two tables unused; nothing public
  depends on them.
- Rolling back below sign in to view would serve `sign_in` resumes publicly.
  That release raises the release fence to itself
  ([release fence](../passkey-release-fence.md)), so `--rollback` below it is
  refused. To go lower on purpose, first turn the switch off on every resume
  with an operator command, then lower the fence.

## Tests

Tests cite this design, ADRs 0060 to 0062, or `AC-*` IDs.

- Go, counting: each layer's outcome with an injected clock and random source;
  token seal, age bounds, replay; ALTCHA verify with a solved challenge, a
  borrowed challenge, and a stripped key signature; label headers from the
  trusted proxy only; dedupe per day with the key rolled at midnight
  Asia/Ho_Chi_Minh and the IPv6 /64; owner exclusion and owner network marking;
  hourly cap; crawler list cases; buffer flush, a failed flush, and shutdown
  flush; live gate re-check; full sets.
- Go, routes: method, Origin, media type, body bound, strict body shapes, 204
  after validation, owner routes need a session, dense days and months.
- Go, sign in: gate for every gated route with and without a pass; `view`
  purpose never touches accounts or sessions and writes nothing about the
  viewer, including a subject that belongs to an account; turning sign-in on
  waits for the fence and raises the epoch.
- Store: migrations up, down, and up; grants; cascades; constraint checks; the
  upsert, summary, and sweep queries.
- Web: the script sends nothing before 8 s visible and one trusted event, and
  stops for the owner and on errors; the Views pages; the privacy notice text;
  later, the gate and the join invite in both languages.
- Browser proofs: the dev-https public check gains the gate sign-in through the
  mock provider with sign in to view.
- Infrastructure: WAF rules are Count only with the scope-down; Caddy passes the
  two headers on the CloudFront listener and strips every other `x-amzn-waf-*`.

## Live checks

After each deploy, on a fictional published resume, evidence under
`.dev/prod-checks/viewer-analytics/`:

1. From a phone on mobile data, open the resume, wait 10 s, scroll; the Views
   page shows one more real view within 2 minutes. Note how long the phone took
   to solve the proof of work.
2. Repeat on the same network the same day: no change. Signed in as the owner:
   no change.
3. `curl` the page and post a replayed collect: no view; M rises by one
   `invalid` for the collect.
4. Share the link in a Zalo draft and a Messenger draft: "Shared in Zalo" and
   "Shared in Facebook" appear; confirm Zalo's user agent token.
5. From an EC2 instance with headless Chromium: `bot` or `datacenter` rises.
6. The CloudWatch metrics of the two label rules show matches, and no rule
   blocks.
7. Sign in to view, in its release: anonymous `/{slug}`, JSON, PDF, and live
   stream are gated; Google sign-in shows the resume and the invite after
   scrolling; closing the invite keeps it closed on reload; no row names the
   viewer; turning the switch off serves the resume publicly at once.

[waf-price]: https://aws.amazon.com/waf/pricing/
