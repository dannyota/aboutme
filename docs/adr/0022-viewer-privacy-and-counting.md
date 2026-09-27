# 0022: Viewer privacy: anonymous view counts and sign in to view

Status: Accepted (2026-09-26). The owner approved the seven counting layers, the
label "real views", and the WAF cost in the
[viewer analytics design](../design/viewer-analytics/README.md#owner-approval),
dropped consented viewer tracking for good, and chose the no-storage model for
sign in to view, which ships in a later release than view counts.

## Context

Resume owners want to know whether their published resumes are read, and some
want only people, not scrapers, to read them. The readers are not aboutme users:
they have no account, accepted no terms, and often open the link from Zalo,
LinkedIn, or email.

Telling a real reader from a bot needs the request's IP address and user agent
for a moment, which is personal data while it is processed (Law 91/2025/QH15
Article 2(1)). Recorded views of a person online are sensitive personal data
(Decree 356/2025/NĐ-CP Article 4(1)(l)). A service that processes personal data
on behalf of others is a personal data processing service, which only an
enterprise may run under a Ministry of Public Security certificate (Decree
Articles 21(1), (3), 22(1), 24(1)); aboutme is run by an individual.

Hosted resume services report view counts that include link-preview fetchers,
search engines, scrapers, and monitoring. The public page HTML is identical for
every viewer and revalidated through the origin gate (ADR 0010), and every
public representation is served without authentication.

## Decision

### Controller and retention

1. aboutme is the controller and processor (Law Article 2(9)) of the transient
   viewer data counting uses, and fixes the fields, filters, and retention for
   every resume. Owners cannot change what is processed.
2. aboutme records nothing about any viewer. It stores only daily totals per
   resume, which are not personal data, and keeps the IP address, user agent,
   and the day's network key in memory only.
3. Counting is always on for every published resume and needs no consent; the
   privacy notice discloses it. Owners receive only totals.
4. Daily totals are kept 400 days. Agents never read them.

### Counting layers

A view counts when it passes every layer
([counting](../design/viewer-analytics/counting.md)):

1. Known crawlers, by user agent on the HTML request, never count; link-preview
   fetchers become share signals.
2. AWS WAF Bot Control, Common level, and the Anonymous IP list run in Count
   mode on the collect path only, and pass their labels to the origin as two
   request headers. Nothing is blocked.
3. Hosting and data-center networks are counted separately.
4. The page's script collects only after 8 s of visible time and one trusted
   interaction.
5. The script gets a one-time token from a start call at page load, sealed with
   AES-256-GCM under a per-process key so it is authenticated and opaque;
   collect accepts it 8 s to 30 min later, once. The token is not embedded in
   the HTML, so the HTML stays shared and cacheable.
6. An ALTCHA v2 proof of work, self-hosted with the MIT Go and JavaScript
   libraries, solved on the main thread and bound to its token.
7. One count per network per resume per day, through an HMAC key that lives only
   in memory for that day; the owner's views are dropped; an hourly cap per
   resume turns bursts into anomalies. Every filtered outcome is also recorded
   at most once per network per resume per day.

Counts are daily aggregates written from a bounded in-memory buffer every 60 s.

### Sign in to view

1. A resume with sign in to view on serves a gate at `/{slug}`. Its JSON, photo,
   PDF, and live stream need a pass for that resume; discovery is forced off.
   The link-preview card and title stay public.
2. Sign-in uses the existing provider flows with a new unauthenticated OAuth
   purpose, `view`, bound to the resume. Its callback never reads, creates,
   links, or signs in an account, never creates a session, and discards every
   claim once the ID token verifies. Nothing about the viewer is stored or
   logged, and the owner is not told who viewed.
3. The pass is a `__Host-view-pass` cookie holding signed passes that name a
   resume, its pass epoch, and an expiry 7 days out, never extended. aboutme
   keeps no copy. It authorizes public reads of that resume only.
4. Turning sign in to view on advances the public generation and waits for the
   revocation fence; turning it on again raises the resume's pass epoch, which
   ends older passes.
5. The release raises the production release fence to itself, so a rollback
   cannot silently make gated resumes public.
6. After sign-in the viewer goes straight to the resume, where a dismissible
   join invite may appear.

## Rejected

| Option                                         | Why not                                                                                    |
| ---------------------------------------------- | ------------------------------------------------------------------------------------------ |
| Consented per-view detail, viewer cookies      | The owner dropped viewer tracking; it needs a consent popup, records, and owner terms      |
| Browser fingerprinting or JA3/JA4 hashes       | The owner ruled out fingerprinting; it identifies viewers without consent                  |
| Bot Control Targeted, or CAPTCHA and Challenge | Targeted uses browser interrogation and fingerprints; challenges interrupt readers         |
| A third-party analytics or CAPTCHA service     | Sends viewer data to another processor abroad                                              |
| Token embedded in the HTML                     | Breaks the shared HTML, the public cache, and `ETag` revalidation                          |
| An HMAC token with a new random resume column  | Needs a column that every resume export would carry; sealing hides the ID instead          |
| Stored hashed IP addresses for dedupe          | An IPv4 hash is reversible by brute force; a stored key makes it personal data             |
| Show the owner who viewed                      | The owner ruled out tracking; it would make viewer identities sensitive data aboutme keeps |
| A light aboutme account for each viewer        | Would put viewers under the account terms and deletion rules                               |
| A random pass stored as a hash in the database | A stored row links a browser to a sign-in; a signed pass needs no row                      |
| An interstitial join page after sign-in        | The owner chose to show the resume at once                                                 |

## Consequences

- No consent popup, viewer cookie for counting, consent record, viewer rights
  page, or owner terms exist. No release waits on a DPIA update for viewer data;
  the next regular update notes the transient processing.
- The privacy notice states that views are counted as described and never
  implies tracking.
- About USD 15 a month in WAF fees at current traffic.
- A deploy ends outstanding tokens and the day's dedupe map: some views are lost
  and some counted twice on deploy days.
- Two people on one network count once a day; one person on two days counts
  twice. The page states this.
- The Vietnam move loses layers 2 and 3 unless vCDN offers equivalents.
- A second replica needs a shared dedupe and nonce store.
- The OAuth transaction table gains a purpose value and a resume column; the
  resumes table gains the switch and the pass epoch. Every gated public route
  checks the pass before the public cache.
- A pass cannot be revoked one by one; raising the epoch or rotating the pass
  key ends passes together.
- LinkedIn appears on the gate only while LinkedIn sign-in is enabled.
- Operators must turn the switch off on every resume before lowering the fence.
- Any future feature that records a viewer needs a new decision.

## History

- Former ADR 0060 (2026-09-26): aboutme controls viewer counting and keeps no
  viewer data. Accepted narrowed: its per-view detail, consent popup, viewer
  cookies, consent records, owner terms, and viewer data page were withdrawn.
- Former ADR 0061 (2026-09-26): seven-layer human-view counting.
- Former ADR 0062 (2026-09-26): sign in to view without an account.
