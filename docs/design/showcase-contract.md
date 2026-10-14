# Community showcase contract

The [showcase design](showcase.md) owns product scope, decisions, and approvals.
The [approved copy](showcase-copy.md) owns interface and legal text.

## What a listing shows

A listing is one tile (**Owner approval** S2):

1. The stored link-preview card at its versioned URL,
   `/api/v1/public/resumes/{slug}/og/{version}.png`
   ([ADR 0014](../adr/0014-public-page-head-and-link-preview.md)). The card
   already holds only the name, headline, photo, accent, and
   `aboutme.vn/{slug}`, and never a contact detail. Its `alt` text is the card's
   image text.
2. One meta line: the template name, or "Custom design" when no preset matches,
   and the resume language.
3. The role chip, when the owner picked one.
4. A footer row: `aboutme.vn/{slug}` as text (**Owner approval** S14) and a
   Report link.

The whole tile except the footer row links to `/{slug}` in the same tab with
`rel="nofollow"`. A tile never shows a contact detail, a date, a view count, or
anything from the resume body. The slug shows as text only in the footer row; it
also reaches the visitor through the card image, the image's `alt` text when the
card has no name, and the Report link's accessible name.

The page's count line shows the listing `total`, the number of listings that
match the filters (**Owner approval** S14). The total is not a per-listing field
and not a view count.

**Template.** The document stores no template identity
([template limits](templates/limitations.md)), so Go derives it: a resume
matches a preset when its `customization` equals the preset's, compared as
canonical JSON without five leaves and without placement. The five leaves are
the four a template apply keeps for the owner (`pageFormat`, `font.textAlign`,
`header.photoPosition`, `colorScheme`) and `dateFormat`, which apply resets and
the owner may change after. Placement is `layout.sections` on the resume and
`layout.placement` and `layout.sidebarSectionTypes` on the preset
([template contract](templates/contract.md)). A committed generator builds the
Go preset table from `packages/schema/templates/`, and a test fails when the
table is stale. No match shows "Custom design" (**Owner approval** S4).

**Language.** The primary subtag of the resume language: `vi`, `en`, or `other`.

**Roles.** The nine Library role chips plus Other: `backend`, `frontend`,
`mobile`, `devops`, `data-ai`, `qa`, `fresher`, `brse`, `security`, `other`,
with the Library's labels.

## Order and filters

Listings are ordered oldest opt-in first: `requested_at ASC, resume_id ASC`
(**Owner approval** S5, as changed by N4). Editing never moves a resume. Turning
the switch off and on again moves it to the end, because it starts a new opt-in.
There is no popularity order: view counts are owner-only
([ADR 0022](../adr/0022-viewer-privacy-and-counting.md)), and ordering by them
would publish them.

Three filters combine: role (the ten roles), language (`vi`, `en`), and template
(a preset ID or `custom`). They live in the page URL (`?role=backend&lang=vi`),
so a filtered view is a plain link. The page stores nothing in cookies or
browser storage, runs no counting script, and personalizes nothing.

Twelve listings show per page (**Owner approval** S13), with Previous and Next
links and `?page=`. Card images load lazily; a page of 12 moves at most about 3
MB of card images, usually about 1.5 MB.

## Derived values and reports

Nothing is reviewed: a resume is listed at once while its opt-in exists, it is
live, and sign in to view is off, so abuse stands until someone reports it.

**Keeping derived values current.** The showcase row holds the current card
version and the derived template ID. Every committed write to an opted-in resume
(document, photo, and publish settings) recomputes both in the same transaction,
through the one resume write boundary. At start, Go recomputes them for every
row, so a changed preset or card layout applies after a deploy.

**Report, then the operator deletes the account.** Each tile's Report link opens
an email to `danny@aboutme.vn` with the subject "Report showcase: {slug}".
aboutme stores nothing about the reporter. When a report shows a serious breach
of the Terms, or the law requires action, the operator deletes the account that
owns the reported slug (**Owner approval** N3; see
[Terms of Service](showcase-copy.md#terms-of-service)). It runs
`server account-delete`, through the durable host operation runner
([ADR 0003](../adr/0003-public-namespace-and-no-operator-surface.md)):

1. `show <slug>` is read-only. It prints the user ID that owns the resume
   holding the slug and every current slug of that account, or "not found" for a
   tombstoned or unknown slug.
2. `confirm <slug> <user-id>` checks again, inside the deletion transaction,
   that the slug's owner is that user ID. A mismatch deletes nothing. A match
   deletes the account through the self-delete path: media, sessions, grants,
   OAuth tokens, showcase rows, and public pages go, and each slug becomes a
   tombstone for 180 days. New public requests return `404` at once; responses
   already admitted finish
   ([ADR 0010](../adr/0010-public-artifact-revocation.md)).

The [root-only host audit](../runbooks/account-delete.md#audit-and-failures)
retains action, outcome, UUID, reported, shown, and returned slugs, and event
UTC for 180 days. It stores no email, name, headline, or report body. Server
journald and CI are not the audit store. The
[host operations design](vietnam-host-operations.md) owns confirmation,
unknown-outcome reconciliation, retention, and separate verification outcomes.

## Delivery, caching, and revocation

`/showcase` is a Nuxt page whose server HTML holds no listing. The browser reads
the listing from Go, which computes it from committed PostgreSQL state on every
request and never stores or caches it:

`GET /api/v1/public/showcase?role=&lang=&template=&page=`

- Query: every parameter optional; `role` in the ten roles, `lang` in `vi` or
  `en`, `template` a preset ID or `custom`, `page` an integer from 1 to 100. Any
  other parameter, repeated parameter, or value is `400 request_invalid`.
- Response: `{ items, page, pageCount, total }`, each item
  `{ slug, cardVersion, imageText, language, templateId, role }`, with
  `templateId` and `role` null when unset. `imageText` follows the card's image
  text rule, so it holds no contact detail.
- Headers: `Cache-Control: no-store`, `X-Robots-Tag: noindex, nofollow`. It
  reads no cookie and sets none.
- Rate limit: 60 requests a minute per IP, with the ADR 0007 overflow. Card
  images keep their limit of 300 artifact reads a minute per IP.
- An item appears only while the opt-in exists, the resume is live, and sign in
  to view is off. The query checks all three, not only the opt-in row.

Because nothing is cached, the ADR 0010 rule holds without a fence: a listing
request that reaches the origin after opt-out, unpublish, sign in to view,
rename, or delete succeeds reads the new state and omits the resume. The card
URL and the resume link return the uniform public 404 at the same moment. An
open showcase tab keeps its tiles and loaded card images until reload; there is
no live update. An edit that changes the card version (name, headline, photo, or
color) keeps the listing, and tiles fetched before it show a missing image until
reload.

## Data and contract

**Table `resume_showcase`**, one row per opted-in resume:

| Column            | Rule                                                                 |
| ----------------- | -------------------------------------------------------------------- |
| `resume_id`       | Primary key; references `resumes` with `ON DELETE CASCADE`           |
| `requested_at`    | Not null; the opt-in time, which orders the listing                  |
| `role`            | Null or one of the ten roles                                         |
| `card_version`    | 16 lowercase hex, not null; maintained by Go                         |
| `template_id`     | Null or a preset ID matching the slug grammar, at most 64 characters |
| `review_key`      | Unused; not null, default `0000000000000000`; Go never writes it     |
| `reviewed_key`    | Unused; always null                                                  |
| `review_outcome`  | Unused; always null                                                  |
| `reviewed_at`     | Unused; always null                                                  |
| `first_listed_at` | Unused; always null                                                  |

An index on `(requested_at DESC, resume_id DESC)` serves the listing.
`aboutme_app` keeps select, insert, update, and delete. A row is about 100 bytes
in use; three resumes per account bound the table. Nothing in the resume
document changes.

**Migration 00011** keeps the earlier release able to run against the new
schema. It never edits 00010 and only adds a default and an index, besides the
row changes (**Owner approval** N1):

1. Delete every row whose `review_outcome` is `declined` (see
   [N1](showcase.md#owner-approvals)).
2. Set `reviewed_key`, `review_outcome`, `reviewed_at`, and `first_listed_at` to
   null on every remaining row. Pending and approved opt-ins are then listed in
   opt-in order, and no review result stays stored.
3. Set the default of `review_key` to `0000000000000000`, so inserts omit it.
4. Create the listing index above. The old partial index stays until a later
   migration drops the unused columns with it.

`Down` drops the index and the default; it cannot restore deleted rows or
cleared results. The unused columns and the old index go in a later contract
migration, once no supported rollback reads them.

**Older release on the new schema.** The earlier release lists only rows with a
matching approved result; after 00011 none has one, so it lists nothing and
shows every owner `pending`. Its review command could list resumes again, but
never one the new rules would not. Rows it writes are valid opt-ins under the
new rules, and the new release ignores the `review_key` it rewrites at start.

**Publish request.** `PublishResumeRequest` gains two optional fields. Omitted
keeps the stored value, so an older web tab that does not know them leaves the
opt-in unchanged.

- `showcaseEnabled` (boolean). `true` with `live` false is `requires_live`;
  `true` with sign in to view on is the new issue `requires_open_view`. A
  request that turns on sign in to view or turns off `live` ends the opt-in even
  when the field is omitted.
- `showcaseRole` (string). Empty clears it; any value outside the ten roles is
  `invalid_format`. Allowed only with the switch on.

The owner resume resource carries `showcase`: `null` when off, otherwise
`{ state, role }` with `state` always `listed`. The field keeps its shape, so an
open tab of the earlier web app reads it unchanged, and a later state can join
the enum without a new shape. OpenAPI gains the listing path and these fields;
the public resume JSON, MCP tools, and the resume document schema are unchanged.

**Security.** The listing route is unauthenticated `GET` and `HEAD` with strict
query parsing, no cookie reads, and closed output fields; the page renders every
value as escaped text and loads images only from its own origin under the
existing app CSP. The showcase adds no privileged route. Its one operator
command, `account-delete`, runs out of band as ADR 0003 requires and prints no
contact data.
