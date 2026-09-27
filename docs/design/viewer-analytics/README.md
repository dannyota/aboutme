# Viewer analytics

A resume owner learns how many real people opened a published resume, without
aboutme storing anything about the people. The design ships as two releases, one
feature each:

| Release         | What the owner gets                                                                       | Viewer sees                                                             |
| --------------- | ----------------------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| View counts     | The Views page: daily counts of real views, filtered automation, and link-preview fetches | Nothing new; no cookie                                                  |
| Sign in to view | A per-resume switch that requires a Google or LinkedIn sign-in before the resume shows    | A sign-in gate, then the resume and a join invite; nothing kept of them |

Consented viewer tracking (per-view detail, a consent popup, viewer cookies, a
viewer rights page) is dropped for good and will not be built. The owner never
learns who viewed a resume, in either release.

Status: view counts approved and built; sign in to view approved (2026-09-27),
ready to build. **Owner approval** marks a product-visible choice for the owner;
the [list](#owner-approval) collects them. **Verify** marks a fact from
documentation or inference that the live checks must confirm.

## Pages

| Page                                  | Holds                                                                    |
| ------------------------------------- | ------------------------------------------------------------------------ |
| [Counting](counting.md)               | What counts as a view, the seven filter layers, share signals, the limit |
| [Sign in to view](sign-in-to-view.md) | Gate, pass cookie, join invite; no viewer data kept                      |
| [Legal](legal.md)                     | Roles, basis, notices, retention, with article citations                 |
| [Delivery](delivery.md)               | Schema, API, edge, limits, cost, rollback, tests, live checks            |

Decisions: [ADR 0022](../../adr/0022-viewer-privacy-and-counting.md): aboutme
controls the counting and keeps no viewer data, counting is layered without
fingerprinting, and sign in to view stores nothing about the viewer.

## Rules

1. **aboutme is the controller** of the transient viewer data counting uses. The
   fields, purposes, and retention are fixed by aboutme and the same for every
   resume ([legal](legal.md#roles)).
2. **Counts need no consent and keep no personal data.** A count is a number per
   resume per day. The network key used to deduplicate lives in memory for one
   day and is never stored.
3. **Nothing about a viewer is recorded.** No view event, viewer identity, or
   consent record exists. Sign in to view keeps only a pass cookie in the
   viewer's browser that names no person.
4. **No fingerprinting.** Filtering uses edge labels, a page-issued token, proof
   of work, visible time, and interaction. It never combines device traits into
   an identifier.
5. **No IP address is stored.** The IP address is used in memory for rate limits
   and the daily network key only.
6. **Counts never reach agents.** No MCP tool reads them.
7. **Daily counts are kept 400 days**; they are not personal data.

## Counting is always on

Every published resume is counted; there is no per-resume setting (**Owner
approval** V3). Counting stores no personal data and never shows the viewer
anything, so a switch would protect no one, and one fewer setting keeps the
publish dialog simple. The owner's own views are excluded
([counting](counting.md#layer-7-dedupe-owner-anomaly)). Sign in to view adds its
own switch in its release.

## Owner approval

| ID  | Choice                                                                                                                                                  | Decision                                                                                                       |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| V1  | Build order: view counts and the Views page first, then sign in to view in a later release                                                              | Approved                                                                                                       |
| V2  | The count label "N lượt xem thật · M bị lọc" / "N real views · M filtered", with the one-line definition in [counting](counting.md#what-the-owner-sees) | Approved                                                                                                       |
| V3  | Per-resume counting mode                                                                                                                                | Approved as counting always on, no setting ([above](#counting-is-always-on))                                   |
| V4  | All viewer-facing text: the privacy notice change, the Views page, the sign-in gate, and the join invite; the owner reviews the Vietnamese              | Approved (2026-09-27), Vietnamese included                                                                     |
| V5  | A `sign_in` resume keeps its link-preview card and page title public                                                                                    | Carried into sign in to view                                                                                   |
| V6  | AWS WAF Bot Control and the Anonymous IP list in Count mode only: about USD 15 a month ([cost](delivery.md#cost))                                       | Approved                                                                                                       |
| V7  | DPIA and cross-border assessment for viewer tracking                                                                                                    | Dropped with viewer tracking; counting keeps no viewer data ([legal](legal.md#assessments))                    |
| V8  | The join invite shows only to signed-in viewers of a `sign_in` resume, never on ordinary public pages                                                   | Approved                                                                                                       |
| V9  | The owner is gated on their own `sign_in` resume like any viewer; public routes never read the session to admit                                         | Approved (2026-09-27), so public routes stay session-free ([sign in to view](sign-in-to-view.md#sign-in-flow)) |
| V10 | The `view` sign-in requests the scope `openid` only, so the provider returns no name or email                                                           | Approved (2026-09-27), subject to a check that LinkedIn accepts `openid` alone                                 |
| V11 | Ship with sign-in to view off by a server flag, raise the release fence, then turn the flag on and redeploy                                             | Approved (2026-09-27) ([release](sign-in-to-view.md#release-and-rollback))                                     |
| V12 | Below the raised fence, rollback is a forward fix or privileged administration; no operator command to switch every resume off                          | Approved (2026-09-27), as for the second-factor floors                                                         |
| V13 | While sign-in is on, the publish dialog shows the discovery switch disabled and keeps its stored value                                                  | Approved (2026-09-27)                                                                                          |

## Releases

| Release         | Outcome                                                                                                                   | Risk                                                               |
| --------------- | ------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| View counts     | Beacon, filter layers, WAF labels, daily aggregates, share signals, owner exclusion, Views page, notice                   | Medium: new public write endpoint, WAF change, migration           |
| Sign in to view | Per-resume switch behind a server flag, gate, `view` OAuth purpose, pass cookie, gated routes, join invite, release fence | High: OAuth, access control on public routes, revocation, rollback |

## Honest limit

Counts exclude what the layers detect. A person who drives a real browser from
residential networks, waits, scrolls, and solves the proof of work is counted,
and so is a sophisticated automation service that does the same. Two people
behind one network on one day count once; one person on two days counts twice.
The page says so in one line ([counting](counting.md#honest-limit)).
