# Viewer analytics

A resume owner learns how many real people opened a published resume, and, when
the owner turns it on and the viewer agrees, when and how they viewed it. The
design ships as three releases, one feature each:

| Feature         | What the owner gets                                                                   | Viewer sees                                   |
| --------------- | ------------------------------------------------------------------------------------- | --------------------------------------------- |
| View counts     | A page with daily counts of real views, filtered automation, and link-preview fetches | Nothing new; no cookie                        |
| Viewer tracking | Per view: time, time on page, device type, city or country, source; no name or email  | A consent popup; declining changes nothing    |
| Sign in to view | Who viewed: name, verified email, and the same per-view details                       | A sign-in gate, then the resume and an invite |

Status: proposed. **Owner approval** marks a product-visible choice the owner
settles before the work that depends on it starts; the [list](#owner-approval)
collects them. **Verify** marks a fact from documentation or inference that the
live checks must confirm.

## Pages

| Page                                  | Holds                                                                    |
| ------------------------------------- | ------------------------------------------------------------------------ |
| [Counting](counting.md)               | What counts as a view, the seven filter layers, share signals, the limit |
| [Viewer tracking](tracking.md)        | Consent popup, cookies, view events, viewer rights                       |
| [Sign in to view](sign-in-to-view.md) | Gate, viewer identity, viewer pass, join invite                          |
| [Legal](legal.md)                     | Roles, basis, notices, rights, retention, DPIA, with article citations   |
| [Delivery](delivery.md)               | Schema, API, edge, limits, cost, rollback, tests, live checks            |

Decisions: [ADR 0060](../../adr/0060-viewer-data-controller-and-consent.md)
(aboutme controls viewer data; detail only with consent),
[ADR 0061](../../adr/0061-layered-human-view-counting.md) (layered counting
without fingerprinting), and
[ADR 0062](../../adr/0062-sign-in-to-view-without-an-account.md) (viewer
identity separate from accounts).

## Rules

1. **aboutme is the controller** of all viewer data. The owner only chooses one
   of three modes per resume. The fields, purposes, and retention are fixed by
   aboutme and are the same for every resume ([legal](legal.md#roles)).
2. **Counts need no consent and keep no personal data.** A count is a number per
   resume per day. The network key used to deduplicate lives in memory for one
   day and is never stored.
3. **Detail needs consent first.** Nothing beyond the anonymous count is
   recorded before the viewer agrees, and a viewer who declines or ignores the
   popup reads the resume normally.
4. **No fingerprinting.** Filtering uses edge labels, a page-issued token, proof
   of work, visible time, and interaction. It never combines device traits into
   an identifier.
5. **No IP address is stored.** City and country come from the edge. The IP
   address is used in memory for rate limits and the daily network key only.
6. **Viewer data never reaches agents.** No MCP tool reads counts or views, so
   viewer data never goes to an AI service the owner connected.
7. **Retention is 90 days** for view events and viewer identities; 7 days for
   viewer passes; 180 days for consent records; 400 days for daily counts, which
   are not personal data.

## Viewer mode

Each resume has one setting, `viewerMode`, in a new "Viewers" panel of the
publish dialog. The default is `count`.

| Mode      | Label (vi / en)                                    | Effect                                                 |
| --------- | -------------------------------------------------- | ------------------------------------------------------ |
| `count`   | Chỉ đếm lượt xem / Count views only                | Anonymous counts only                                  |
| `ask`     | Hỏi người xem / Ask viewers                        | Consent popup; detail for viewers who agree            |
| `sign_in` | Yêu cầu đăng nhập để xem / Require sign-in to view | Sign-in gate; name, email, and detail for every viewer |

Choosing `ask` or `sign_in` shows the owner a short terms box they must accept
once per resume: use viewer data only to follow up on the job search, never
publish or sell it, and know it is deleted after 90 days
([legal](legal.md#terms-for-owners)). The mode is stored on the resume, is not
part of the resume document, and never changes the rendered resume.

## Owner approval

| ID  | Choice                                                                                                                                                                                                           | Recommendation                                                                                              |
| --- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------- |
| V1  | Build order: view counts first, then viewer tracking, then sign in to view, numbered 0.6.4, 0.6.5, 0.6.6 in shipping order                                                                                       | Yes. Counting and bot filtering are the base both other features record into, and they need no consent      |
| V2  | The count label: "N lượt xem thật · M bị lọc là bot hoặc tự động" / "N real views · M filtered as bots or automation", with the one-line definition in [counting](counting.md#what-the-owner-sees)               | Yes. "People" overstates what a count without cookies can know; "views" with the definition is honest       |
| V3  | One three-state viewer mode per resume, default `count`, with owner terms on `ask` and `sign_in`                                                                                                                 | Yes                                                                                                         |
| V4  | All viewer-facing text: consent popup, sign-in gate, join invite, and the privacy notice and terms changes, in [legal](legal.md) and [sign-in](sign-in-to-view.md#join-invite); the owner reviews the Vietnamese | Yes                                                                                                         |
| V5  | A `sign_in` resume keeps its link-preview card and page title public; everything else waits for sign-in                                                                                                          | Yes. The owner shares the link on purpose, the card holds no contact details, and a blank card looks broken |
| V6  | AWS WAF Bot Control and the Anonymous IP list in label-only mode: about USD 15 a month ([cost](delivery.md#cost))                                                                                                | Yes                                                                                                         |
| V7  | The owner updates the DPIA and the cross-border transfer assessment to cover viewer data before viewer tracking ships, and files them within 60 days                                                             | Yes; the law requires both ([legal](legal.md#dpia))                                                         |
| V8  | The join invite shows only to signed-in viewers of a `sign_in` resume, never on ordinary public pages                                                                                                            | Yes ([join invite](sign-in-to-view.md#join-invite))                                                         |

## Releases

| Release         | Outcome                                                                                               | Risk                                                               |
| --------------- | ----------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| View counts     | Beacon, filter layers, WAF labels, aggregates, share signals, owner exclusion, the Views page, notice | Medium: new public write endpoint, WAF change, migration           |
| Viewer tracking | Mode `ask`, consent popup, viewer cookies, view events, consent records, viewer data page, notice     | High: sensitive personal data, consent, cookies on public pages    |
| Sign in to view | Mode `sign_in`, gate, `view` OAuth purpose, viewer pass, gated artifacts, join invite, release fence  | High: OAuth, access control on public routes, revocation, rollback |

The delivery plan is [viewer analytics](../../plans/viewer-analytics.md).

## Honest limit

Counts exclude what the layers detect. A person who drives a real browser from
residential networks, waits, scrolls, and solves the proof of work is counted,
and so is a sophisticated automation service that does the same. Two people
behind one network on one day count once; one person on two days counts twice.
The page says so in one line ([counting](counting.md#honest-limit)).
