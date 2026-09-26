# 0061: Layered human-view counting without fingerprinting

Status: Proposed (2026-09-26). The owner approved the seven layers; the cost and
the count label wait for approval in the
[viewer analytics design](../design/viewer-analytics/README.md#owner-approval).

## Context

Hosted resume services report view counts that include link-preview fetchers,
search engines, scrapers, and monitoring. A count that says "someone read your
resume" must exclude them, without fingerprinting and without a cookie for
viewers who never agreed to one. The public page HTML is identical for every
viewer and revalidated through the origin cache
([ADR 0022](0022-public-artifact-revocation.md)).

## Decision

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
5. The script gets a one-time HMAC token from a start call at page load; collect
   accepts it 8 s to 30 min later, once. The token is not embedded in the HTML,
   so the HTML stays shared and cacheable.
6. An ALTCHA v2 proof of work, self-hosted with the MIT Go and JavaScript
   libraries, solved on the main thread.
7. One count per network per resume per day, through an HMAC key that lives only
   in memory for that day; the owner's views are dropped; an hourly cap per
   resume turns bursts into anomalies.

Counts are daily aggregates written from a bounded in-memory buffer every 60 s.

## Rejected

| Option                                         | Why not                                                                            |
| ---------------------------------------------- | ---------------------------------------------------------------------------------- |
| Browser fingerprinting or JA3/JA4 hashes       | The owner ruled out fingerprinting; it identifies viewers without consent          |
| Bot Control Targeted, or CAPTCHA and Challenge | Targeted uses browser interrogation and fingerprints; challenges interrupt readers |
| A third-party analytics or CAPTCHA service     | Sends viewer data to another processor abroad                                      |
| Token embedded in the HTML                     | Breaks the shared HTML, the public cache, and `ETag` revalidation                  |
| Stored hashed IP addresses for dedupe          | An IPv4 hash is reversible by brute force; a stored key makes it personal data     |

## Consequences

- About USD 15 a month in WAF fees at today's traffic.
- A deploy ends outstanding tokens and the day's dedupe map: some views are lost
  and some counted twice on deploy days.
- Two people on one network count once a day; one person on two days counts
  twice. The page states this.
- The Vietnam move loses layers 2 and 3 unless vCDN offers equivalents.
- A second replica needs a shared dedupe and nonce store.
