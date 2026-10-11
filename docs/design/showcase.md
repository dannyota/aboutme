# Community showcase

The community showcase is a public page, `/showcase`, that lists published
resumes whose owners chose to show them, so visitors see real examples before
they sign up. Each listing shows the resume's stored link-preview card and links
to the public resume. Nothing about visitors is recorded.
[ADR 0029](../adr/0029-community-showcase.md) records the decisions.

Status: approved and built ([approvals](#owner-approvals)); the legal dates in
[Copy](showcase-copy.md#copy) change at release. The
[showcase page layout](ui/showcase.md) sets the page's layout.

## Opt-in

Each resume gains one switch in the publish dialog, **Show in the community
showcase**, off by default for every resume, new and existing. The switch is a
publication setting on its own row, not part of the resume document (**Owner
approval** S1).

- The switch can be on only while the resume is live and sign in to view is off.
  The publish request rejects any other combination.
- Unpublishing, turning on sign in to view, and deleting the resume or the
  account end the opt-in in the same transaction. Publishing again starts with
  the switch off.
- Turning the switch off ends the opt-in. Turning it on again starts a new
  opt-in with a new opt-in time.
- Renaming the slug and editing the resume keep the opt-in and the listing.
- Each resume opts in on its own. The listing never groups resumes by account
  and carries no account identifier (**Owner approval** S12).
- The owner may pick one role for the listing from a closed list, or none
  (**Owner approval** S3).
- Connected agents cannot read or change the switch through a tool, the same as
  every other publish setting ([ADR 0018](../adr/0018-mcp-agent-access.md)).

Showcase state for the owner, returned on the owner resume resource:

| State    | Meaning                                                            |
| -------- | ------------------------------------------------------------------ |
| `off`    | No opt-in                                                          |
| `listed` | Opted in; the resume is live with sign in to view off and is shown |

An opt-in is therefore always `listed`; the resource has no hidden or blocked
state.

## What a listing shows

See [What a listing shows](showcase-contract.md#what-a-listing-shows).

## Order and filters

See [Order and filters](showcase-contract.md#order-and-filters).

## Derived values and reports

See
[Derived values and reports](showcase-contract.md#derived-values-and-reports).

## Search engines and discovery

The showcase never changes whether a resume is indexed (**Owner approval** S8).

- `/showcase` sends `robots: noindex, nofollow` and
  `X-Robots-Tag: noindex, nofollow`, and is absent from `sitemap.xml` and
  `llms.txt`. `robots.txt` stays unchanged, so crawlers can read the noindex.
- Tile links carry `rel="nofollow"`.
- A resume's SEO and GEO switch keeps its meaning. A listed resume with the
  switch off keeps `noindex` on its page and its card.
- The listing API sits under `/api/`, which `robots.txt` disallows.

## Delivery, caching, and revocation

See
[Delivery, caching, and revocation](showcase-contract.md#delivery-caching-and-revocation).

## Route and navigation

The page lives at `/showcase` (**Owner approval** S9). Every fixed root is
English (`/templates`, `/verify`, `/privacy`) and the interface language is a
cookie, not a path, so one English root serves both languages. The public-root
registry gains `showcase` with Nuxt dispatch. The migration fails, and so the
deploy stops before the app starts, if any resume holds the slug `showcase`; the
operator then settles it with that owner before retrying.

The header shows **Community** / **Cộng đồng** after Library for every visitor.
On a signed-in phone it drops with Library, as the header already does.

## Empty state and launch

At launch the operator turns the switch on for `/danny` in the publish dialog,
so the page opens with one real listing (**Owner approval** S10). With no
listings, the page shows the [empty state](showcase-copy.md#showcase-page), and
the nav link stays.

## Copy

See [Copy](showcase-copy.md#copy).

### Publish dialog

See [Publish dialog](showcase-copy.md#publish-dialog).

### Showcase page

See [Showcase page](showcase-copy.md#showcase-page).

### Privacy Policy

See [Privacy Policy](showcase-copy.md#privacy-policy).

### Terms of Service

See [Terms of Service](showcase-copy.md#terms-of-service).

## Privacy and abuse

- **Consent.** Nothing is listed without the owner's switch, set in the web UI
  by a person; the help text says anyone can see the page. The switch is
  withdrawn in one click and ends with unpublish.
- **Exposure.** A listing adds no new data: the card, name, headline, and photo
  are already public for every live resume. It adds reach: one page gathers
  them. The copy states this, and the page is kept out of search indexes.
- **Scraping.** The listing is paged, rate limited, and holds no contact data;
  bulk copying cannot be prevented, as the privacy text says.
- **Viewers.** The page records nothing about visitors, sets no cookie, and runs
  no counting script. A click on a tile is an ordinary view of that resume,
  counted as [counting](viewer-analytics/counting.md) describes, with no
  referrer kept.
- **Abuse.** Spam, impersonation, offensive content, and other people's data are
  handled after a report: the operator deletes the account
  ([Report](showcase-contract.md#derived-values-and-reports)). A verified email
  and three resumes per account bound bulk opt-ins.
- **Minors.** The Terms require age 16 and say an account that belongs to a
  younger person is deleted; a report starts the same deletion.
- **Retention.** The opt-in row, with its opt-in time and role, is deleted on
  opt-out, unpublish, sign in to view, and resume or account deletion. Nothing
  about an ended opt-in is kept.
- **Export.** The account export includes each resume's showcase state
  (`listed`), role, and opt-in time.
- **Privacy text.** The privacy and disclosure gate in
  [decision status](decisions.md) covers the new text.

## Data and contract

See [Data and contract](showcase-contract.md#data-and-contract).

## Rejected

| Option                                  | Why not                                                                                           |
| --------------------------------------- | ------------------------------------------------------------------------------------------------- |
| Review before listing                   | Delays every opt-in and costs operator time; the owner chose Report and the Terms instead         |
| Account blocking                        | New account state and checks on every route; the owner chose to delete the account instead        |
| A hide command for reported listings    | A second takedown path; the owner chose to delete the account for a breach                        |
| An in-app review queue for the operator | ADR 0003 forbids an operator surface in the public app                                            |
| A first-page thumbnail of the resume    | A new stored artifact and render per edit; its text can show contact details in the body          |
| Owner-chosen template label             | Goes stale after a template switch and can be wrong; derivation is exact                          |
| Order by views or recent edits          | Publishes owner-only counts, or rewards edits made only to move up                                |
| Indexable showcase                      | Would index names of people who left SEO off; showing only SEO-on resumes to crawlers is cloaking |
| Showcase implies SEO on                 | Couples two choices the owner makes separately                                                    |
| A cached or server-rendered listing     | Needs a discovery-style fence for mutable names; per-request reads meet ADR 0010 without one      |
| `/cong-dong` as the route               | Every other fixed root is English, and the interface language is not in the path                  |

## Owner approvals

The last column records each answer. The owner approved S1 to S13 on 2026-09-27
as each reads. The owner decision of 2026-10-01 (no review; Report and the Terms
stay) superseded S6 and S7, changed S5, S10, and S11, and settled N1 to N4. The
manager approved S14 on 2026-10-03 under owner delegation.

| ID  | Decision                                                                                                                                          | Owner decision                                                                 |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------ |
| S1  | Separate switch, off by default, live and open view only; unpublish, sign in to view, and delete end it; republish starts off                     | Approved as written                                                            |
| S2  | Tile shows the stored preview card, template, language, optional role, and Report; no contact, date, or count; slug text: S14                     | Approved; a page thumbnail can follow later as its own decision                |
| S3  | Owner picks an optional role from the nine Library roles plus Other                                                                               | Approved                                                                       |
| S4  | Template derived by exact token match, else "Custom design"                                                                                       | Approved                                                                       |
| S5  | Newest first by first approval; filters role, language, template in the URL; no popularity order                                                  | Approved; since 2026-10-01 oldest opt-in first (N4)                            |
| S6  | Review before listing through an out-of-band command; re-review when the review key changes; Report by email                                      | Approved; superseded 2026-10-01: no review, Report by email stays              |
| S7  | No operator notice at launch and no promised review time in the copy                                                                              | Approved; superseded 2026-10-01: nothing to review                             |
| S8  | `/showcase` noindex and nofollow, outside sitemap and `llms.txt`; SEO and GEO switch unchanged                                                    | Approved                                                                       |
| S9  | Route `/showcase`; nav link Community / Cộng đồng after Library                                                                                   | Approved                                                                       |
| S10 | Launch with `/danny` opted in and approved; empty state as written; nav link from day one                                                         | Approved; the owner wants `/danny` listed; no approval step since 2026-10-01   |
| S11 | Privacy and Terms text in [Copy](showcase-copy.md#copy); update the dates; no advance email, since nothing changes for anyone who does not opt in | Approved: the text as written, and no advance email; review text changed by N2 |
| S12 | Each resume of an account may be listed on its own, never grouped                                                                                 | Approved                                                                       |
| S13 | Twelve listings per page, lazy card images                                                                                                        | Approved                                                                       |
| S14 | Tile footer shows `aboutme.vn/{slug}` as text; the count line shows the listing `total`                                                           | Approved 2026-10-03 by the manager under owner delegation                      |

Choices that follow from the 2026-10-01 decision:

| ID  | Decision                                                                                                                                                     | Owner decision                      |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------- |
| N1  | Migration 00011 deletes declined opt-ins, lists pending ones at deploy without review, and clears every stored review result                                 | Decided 2026-10-01 by that decision |
| N2  | Legal edits only delete review claims: the review sentences, "our review result" in what we collect, the Terms showcase line; release date; no advance email | Decided 2026-10-01 by that decision |
| N3  | No account blocking; abuse ends in deleting the account that owns the reported slug, by the out-of-band `account-delete` command                             | Decided 2026-10-01 by the owner     |
| N4  | Order by oldest opt-in first, `requested_at ASC, resume_id ASC`; turning the switch off and on moves a resume to the end                                     | Decided 2026-10-01 by the owner     |
| N5  | Privacy Policy discloses the record an operator deletion keeps (text in [Privacy Policy](showcase-copy.md#privacy-policy))                                   | Approved 2026-10-02                 |

**N1.** Every `declined` row is deleted, whatever key it was declined for,
including rows the previous UI showed as `pending`. Their owners see the switch
off and may turn it on again. No review result stays stored.

**N5, historical approval rationale.** The approval relied on the
`account-delete` audit line keeping the user ID and the account's slugs for 180
days, and CloudTrail keeping the task's arguments for 90 days, so the policy
discloses that record; the audit needs both to tie a deletion to its report.

Production uses the Vietnam host under
[ADR 0027](../adr/0027-vietnam-hosted-production.md). CloudTrail is not its
audit store. The host procedure for retaining and tying the deletion audit to
its report remains unresolved; the approved disclosure and 180-day retention
requirement remain unchanged.
