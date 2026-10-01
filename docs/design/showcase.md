# Community showcase

The community showcase is a public page, `/showcase`, that lists published
resumes whose owners chose to show them, so visitors see real examples before
they sign up. Each listing shows the resume's stored link-preview card and links
to the public resume. Nothing about visitors is recorded.
[ADR 0029](../adr/0029-community-showcase.md) records the decisions.

Status: approved (2026-09-27) and built; amended 2026-10-01, when the owner
removed review. An eligible opt-in is listed at once, and abuse is handled by
Report and the Terms. The amendment is built; release waits for N3 and N4 in the
[approvals](#owner-approvals) section. The legal dates in [Copy](#copy) change
at release.

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
  Nothing is reviewed, before listing or after an edit.
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

An opt-in exists only while the resume is live with sign in to view off, so an
opt-in is always `listed`. There is no hidden or blocked state on the resource.

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
4. A Report link.

The whole tile except Report links to `/{slug}` in the same tab with
`rel="nofollow"`. A tile never shows a contact detail, a date, a view count, or
anything from the resume body. The tile's own text (meta line, role chip, and
Report) never shows the slug. The slug still reaches the visitor in three ways:
the card image prints `aboutme.vn/{slug}`, the image's `alt` text is
`aboutme.vn/{slug}` when the card has no name, and the Report link's accessible
label reads "Report resume {slug} by email".

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

Listings are ordered newest first by the opt-in time, then by resume ID (**Owner
approval** S5, as changed by N4). Editing never moves a resume up. Turning the
switch off and on again does, because it starts a new opt-in. There is no
popularity order: view counts are owner-only
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

Nothing is reviewed. A resume is listed while its opt-in exists, it is live, and
sign in to view is off. The owner chose on 2026-10-01 to list every opt-in at
once. The showcase is a page aboutme.vn hosts, so abuse stands as its content
until someone reports it.

**Keeping derived values current.** The showcase row holds the current card
version and the derived template ID. Every committed write to an opted-in resume
(document, photo, and publish settings) recomputes both in the same transaction,
through the one resume write boundary. At start, Go recomputes them for every
row, so a changed preset or card layout applies after a deploy.

**Reports.** Each tile's Report link opens an email to `danny@aboutme.vn` with
the subject "Report showcase: {slug}". aboutme stores nothing about the
reporter. The operator handles a report that shows a breach of the Terms by
blocking the account under the Terms, which ends every listing of that account.

**Known gap.** Account blocking is not built. Until it is, the operator has no
supported way to take a reported listing down; the listing stays until its owner
turns the switch off, unpublishes, or deletes the resume.

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

`/showcase` is a Nuxt page. The server-rendered HTML holds the heading, lead,
filters, and an empty grid; the browser then reads the listing from Go. The
listing is computed from committed PostgreSQL state on every request and is
never stored or cached:

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
already open showcase tab keeps its tiles, and any card image it has loaded,
until the visitor reloads; there is no live update. An edit that changes the
card version (a name, headline, photo, or color) keeps the listing; tiles
fetched before the edit show a missing image until reload.

## Route and navigation

The page lives at `/showcase` (**Owner approval** S9). Every fixed root is
English (`/templates`, `/verify`, `/privacy`) and the interface language is a
cookie, not a path, so one English root serves both languages. The public-root
registry gains `showcase` with Nuxt dispatch. The migration fails, and so the
deploy stops before the app starts, if any resume holds the slug `showcase`; the
operator then settles it with that owner before retrying.

The header shows **Community** / **Cộng đồng** after Library for every visitor.
On a signed-in phone it drops with Library, as the header already does. The
landing page is unchanged.

## Empty state and launch

At launch the operator turns the switch on for `/danny` in the publish dialog,
so the page opens with one real listing (**Owner approval** S10). With no
listings, the page shows the empty state below, and the nav link stays.

## Copy

All interface text follows the interface language. Vietnamese uses "xuất bản"
for publish and "CV" for resume, as the rest of the product does.

### Publish dialog

| Part                       | Vietnamese                                                                                                 | English                                                                                                           |
| -------------------------- | ---------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| Switch                     | Hiện trong trang Cộng đồng                                                                                 | Show in the community showcase                                                                                    |
| Help                       | Hiện ảnh xem trước, mẫu, ngôn ngữ và vị trí của CV này tại aboutme.vn/showcase. Ai cũng xem được trang đó. | Shows this resume's preview image, template, language, and role at aboutme.vn/showcase. Anyone can see that page. |
| Role label                 | Vị trí hiển thị                                                                                            | Role shown                                                                                                        |
| Role hint                  | Tùy chọn. Giúp người xem lọc theo vị trí.                                                                  | Optional. Lets visitors filter by role.                                                                           |
| No role                    | Không chọn                                                                                                 | None                                                                                                              |
| Other role                 | Khác                                                                                                       | Other                                                                                                             |
| Needs public               | Bật CV công khai để hiện trong trang Cộng đồng.                                                            | Turn on Public resume to show it in the community showcase.                                                       |
| Needs open view            | Không dùng được khi bật Yêu cầu đăng nhập để xem.                                                          | Not available while Require sign-in to view is on.                                                                |
| Listed                     | Đang hiện trong trang Cộng đồng.                                                                           | Shown in the community showcase.                                                                                  |
| Listed link                | Xem trang Cộng đồng                                                                                        | View the showcase                                                                                                 |
| Issue `requires_open_view` | Tùy chọn này cần tắt Yêu cầu đăng nhập để xem.                                                             | This option needs Require sign-in to view off.                                                                    |

### Showcase page

| Part              | Vietnamese                                                                                        | English                                                                                              |
| ----------------- | ------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| Nav link          | Cộng đồng                                                                                         | Community                                                                                            |
| Title and h1      | CV từ cộng đồng                                                                                   | Community resumes                                                                                    |
| Meta description  | CV thật do người dùng aboutme.vn chọn chia sẻ.                                                    | Real resumes that aboutme.vn users chose to share.                                                   |
| Lead              | CV thật do người dùng aboutme.vn xuất bản và chọn hiện ở đây.                                     | Real resumes that aboutme.vn users published and chose to show here.                                 |
| Order note        | CV mới thêm hiện trước.                                                                           | Most recently added first.                                                                           |
| Role filter label | Lọc theo vị trí                                                                                   | Filter by role                                                                                       |
| All roles         | Mọi vị trí                                                                                        | All roles                                                                                            |
| Language label    | Ngôn ngữ của CV                                                                                   | Resume language                                                                                      |
| All languages     | Mọi ngôn ngữ                                                                                      | All languages                                                                                        |
| Vietnamese        | Tiếng Việt                                                                                        | Vietnamese                                                                                           |
| English           | Tiếng Anh                                                                                         | English                                                                                              |
| Other language    | Ngôn ngữ khác                                                                                     | Other language                                                                                       |
| Template label    | Mẫu                                                                                               | Template                                                                                             |
| All templates     | Mọi mẫu                                                                                           | All templates                                                                                        |
| Custom design     | Thiết kế riêng                                                                                    | Custom design                                                                                        |
| Report            | Báo cáo                                                                                           | Report                                                                                               |
| Report label      | Báo cáo CV {slug} qua email                                                                       | Report resume {slug} by email                                                                        |
| Report subject    | Báo cáo trang Cộng đồng: {slug}                                                                   | Report showcase: {slug}                                                                              |
| Empty             | Chưa có CV nào ở đây. Xuất bản CV của bạn và bật Hiện trong trang Cộng đồng để là người đầu tiên. | No resumes here yet. Publish your resume and turn on Show in the community showcase to be the first. |
| Empty action      | Tạo CV                                                                                            | Create your resume                                                                                   |
| No match          | Không có CV nào khớp với bộ lọc này.                                                              | No resume matches these filters.                                                                     |
| Load failed       | Không tải được danh sách. Hãy thử lại.                                                            | Could not load the list. Try again.                                                                  |
| Retry             | Thử lại                                                                                           | Try again                                                                                            |
| Previous, Next    | Trang trước, Trang sau                                                                            | Previous, Next                                                                                       |
| Page status       | Trang {page}/{pageCount}                                                                          | Page {page} of {pageCount}                                                                           |

The empty action opens `/register` when password registration is on, otherwise
`/login`; a signed-in visitor gets `/app/resumes` with "Mở CV của bạn" / "Open
your resumes".

### Privacy Policy

Text in `legal-vi.ts` and `legal-en.ts` (**Owner approval** S11, as changed by
N2). The "updated" date becomes the release date.

| Section                                        | Vietnamese                                                                                                                                                                                                                                                                                                                                                                                                                                                                                | English                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| ---------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| What we collect, new item after Content        | Trang Cộng đồng: nếu bạn bật tùy chọn này cho một CV, chúng tôi lưu thời điểm bạn bật và vị trí bạn chọn.                                                                                                                                                                                                                                                                                                                                                                                 | Community showcase: if you turn it on for a resume, we store when you turned it on and the role you picked.                                                                                                                                                                                                                                                                                                                                                                                                 |
| Why we use your data, optional features list   | (xuất bản CV công khai, cho phép lập chỉ mục, hiện CV trong trang Cộng đồng, kết nối trợ lý AI, đăng nhập bằng Google hoặc LinkedIn)                                                                                                                                                                                                                                                                                                                                                      | (publishing, search and AI indexing, the community showcase, connected AI agents, Google or LinkedIn sign-in)                                                                                                                                                                                                                                                                                                                                                                                               |
| Public only by your choice, new last paragraph | Trang Cộng đồng (aboutme.vn/showcase) chỉ hiện những CV mà chủ CV bật Hiện trong trang Cộng đồng. Trang này hiện ảnh xem trước của CV (họ tên, tiêu đề và ảnh của bạn), mẫu, ngôn ngữ và vị trí bạn chọn, kèm đường dẫn đến CV. Khi bạn tắt tùy chọn này, hủy xuất bản, hoặc bật Yêu cầu đăng nhập để xem, CV rời khỏi trang Cộng đồng ngay lập tức. Trang Cộng đồng không cho công cụ tìm kiếm lập chỉ mục, nhưng bất kỳ ai truy cập đều có thể xem và sao chép những gì trang hiển thị. | The community showcase (aboutme.vn/showcase) lists only resumes whose owners turn on Show in the community showcase. It shows the resume's preview image (your name, headline, and photo), its template, language, and the role you picked, with a link to the resume. When you turn the option off, unpublish, or turn on Require sign-in to view, the resume leaves the showcase right away. Search engines are asked not to index the showcase, but anyone who visits it can see and copy what it shows. |

### Terms of Service

The Terms add no showcase line under Acceptable use. Its existing line, "We may
remove content or delete accounts that break these rules.", covers the showcase.

| Section                                     | Vietnamese                                                                                                                                                                                                               | English                                                                                                                                                                                                           |
| ------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Your content, new paragraph after the first | Nếu bạn bật Hiện trong trang Cộng đồng cho một CV, bạn cũng cho phép aboutme.vn hiện ảnh xem trước, mẫu, ngôn ngữ và vị trí bạn chọn của CV đó trên trang Cộng đồng, cho đến khi bạn tắt tùy chọn này hoặc hủy xuất bản. | If you turn on Show in the community showcase for a resume, you also let aboutme.vn show its preview image, template, language, and the role you picked on the showcase page, until you turn it off or unpublish. |

## Privacy and abuse

- **Consent.** Nothing is listed without the owner's switch, set in the web UI
  by a person; the help text says anyone can see the page. The switch is
  withdrawn in one click and ends with unpublish.
- **Exposure.** A listing adds no new data: the card, name, headline, and photo
  are already public for every live resume. It adds reach: one page gathers
  them. The copy states this, and the page is kept out of search indexes.
- **Scraping.** The listing is paged, rate limited, and holds no contact data.
  Bulk copying of public cards cannot be prevented, and the privacy text says
  so.
- **Viewers.** The page records nothing about visitors, sets no cookie, and runs
  no counting script. A click on a tile is an ordinary view of that resume,
  counted as [counting](viewer-analytics/counting.md) describes, with no
  referrer kept.
- **Abuse.** Nothing checks a listing before it appears. Spam, impersonation,
  offensive content, and other people's data are handled after a report: the
  operator blocks the account under the Terms. Account blocking is not built
  (see [Known gap](#derived-values-and-reports)). Accounts need a verified email
  and hold at most three resumes, which bounds bulk opt-ins.
- **Minors.** The Terms require age 16 and say an account that belongs to a
  younger person is deleted; a report starts that.
- **Retention.** The opt-in row, with its opt-in time and role, exists only
  while the opt-in does. It is deleted on opt-out, unpublish, sign in to view,
  and resume or account deletion. Nothing about an ended opt-in is kept.
- **Export.** The account export includes each resume's showcase state
  (`listed`), role, and opt-in time.
- **Impact assessment.** The next regular update of the data protection impact
  assessment notes the showcase; the privacy and disclosure review gate in
  [decision status](decisions.md) covers the new text.

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

1. Delete every row whose `review_outcome` is `declined`. Those owners see the
   switch off and may turn it on again, which lists the resume.
2. Set `reviewed_key`, `review_outcome`, `reviewed_at`, and `first_listed_at` to
   null on every remaining row. Pending and approved opt-ins are then listed in
   opt-in order, and no review result stays stored.
3. Set the default of `review_key` to `0000000000000000`, so inserts omit it.
4. Create the listing index above. The old partial index stays until a later
   migration drops the unused columns with it.

`Down` drops the index and the default; it cannot restore deleted rows or
cleared results. The unused columns and the old index go in a later contract
migration, once no supported rollback reads them.

**Older release on the new schema.** The earlier release lists only rows with an
approved result whose key matches. After 00011 no row has one, so that release
lists nothing, shows every owner `pending`, and its review command could list
resumes again. It never lists a resume the new rules would not. Rows it writes
(no result, a real review key) are valid opt-ins under the new rules. At its own
start it rewrites `review_key` on every row, which the new release ignores.

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
the enum without a new shape. The public resume JSON is unchanged. OpenAPI gains
the listing path and these fields. MCP tools and the resume document schema are
unchanged.

**Security.** The listing route is unauthenticated `GET` and `HEAD` with strict
query parsing, no cookie reads, and closed output fields; the page renders every
value as escaped text and loads images only from its own origin under the
existing app CSP. The showcase adds no operator command and no privileged route.

## Rejected

| Option                                  | Why not                                                                                           |
| --------------------------------------- | ------------------------------------------------------------------------------------------------- |
| Review before listing                   | Delays every opt-in and costs operator time; the owner chose Report and the Terms instead         |
| A hide command for reported listings    | A second takedown path beside account blocking; the owner chose to block the account              |
| An in-app review queue for the operator | ADR 0003 forbids an operator surface in the public app                                            |
| A first-page thumbnail of the resume    | A new stored artifact and render per edit; its text can show contact details in the body          |
| Owner-chosen template label             | Goes stale after a template switch and can be wrong; derivation is exact                          |
| Order by views or recent edits          | Publishes owner-only counts, or rewards edits made only to move up                                |
| Indexable showcase                      | Would index names of people who left SEO off; showing only SEO-on resumes to crawlers is cloaking |
| Showcase implies SEO on                 | Couples two choices the owner makes separately                                                    |
| A cached or server-rendered listing     | Needs a discovery-style fence for mutable names; per-request reads meet ADR 0010 without one      |
| `/cong-dong` as the route               | Every other fixed root is English, and the interface language is not in the path                  |

## Owner approvals

Each item needs the owner's answer; the last column records it. The owner
approved S1 to S13 on 2026-09-27, as each recommendation reads. The owner
decision of 2026-10-01 superseded S6 and S7 and changed S5, S10, and S11; their
rows say how. That decision reads: "no review; an opted-in, published resume
that passes the automatic rules is listed directly, no pending state, no approve
or decline; Report and the Terms stay; abuse is handled by Report, then blocking
the account". It settles N1 and N2.

| ID  | Decision                                                                                                                      | Owner decision (2026-09-27)                                                    |
| --- | ----------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------ |
| S1  | Separate switch, off by default, live and open view only; unpublish, sign in to view, and delete end it; republish starts off | Approved as written                                                            |
| S2  | Tile shows the stored preview card, template, language, optional role, and Report; no contact, slug text, date, or count      | Approved; a page thumbnail can follow later as its own decision                |
| S3  | Owner picks an optional role from the nine Library roles plus Other                                                           | Approved                                                                       |
| S4  | Template derived by exact token match, else "Custom design"                                                                   | Approved                                                                       |
| S5  | Newest first by first approval; filters role, language, template in the URL; no popularity order                              | Approved; since 2026-10-01 ordered by opt-in time (N4)                         |
| S6  | Review before listing through an out-of-band command; re-review when the review key changes; Report by email                  | Approved; superseded 2026-10-01: no review, Report by email stays              |
| S7  | No operator notice at launch and no promised review time in the copy                                                          | Approved; superseded 2026-10-01: nothing to review                             |
| S8  | `/showcase` noindex and nofollow, outside sitemap and `llms.txt`; SEO and GEO switch unchanged                                | Approved                                                                       |
| S9  | Route `/showcase`; nav link Community / Cộng đồng after Library                                                               | Approved                                                                       |
| S10 | Launch with `/danny` opted in and approved; empty state as written; nav link from day one                                     | Approved; the owner wants `/danny` listed; no approval step since 2026-10-01   |
| S11 | Privacy and Terms text above; update the dates; no advance email, since nothing changes for anyone who does not opt in        | Approved: the text as written, and no advance email; review text changed by N2 |
| S12 | Each resume of an account may be listed on its own, never grouped                                                             | Approved                                                                       |
| S13 | Twelve listings per page, lazy card images                                                                                    | Approved                                                                       |

Choices that follow from the 2026-10-01 decision:

| ID  | Decision                                                                                                                                                     | Owner decision                      |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------- |
| N1  | Migration 00011 deletes declined opt-ins, lists pending ones at deploy without review, and clears every stored review result                                 | Decided 2026-10-01 by that decision |
| N2  | Legal edits only delete review claims: the review sentences, "our review result" in what we collect, the Terms showcase line; release date; no advance email | Decided 2026-10-01 by that decision |
| N3  | Release while account blocking is not built, so a reported listing has no takedown until it is                                                               | **Pending**                         |
| N4  | Order by opt-in time, so turning the switch off and on moves a resume to the top                                                                             | **Pending**                         |

**N1.** Every row with a `declined` result is deleted, whatever key it was
declined for. That includes rows declined for an older review key, which the
previous UI showed as `pending`. Their owners see the switch off and may turn it
on again, which lists the resume. No review result stays stored.

**N3.** A reported listing stays up until its owner ends the opt-in. Options:

1. Hold the release until account blocking ships.
2. Release with a minimal out-of-band command that deletes one opt-in row by
   slug. The effect equals the owner's opt-out: the owner sees the switch off
   and may turn it on again. It needs no new state, no operator surface in the
   public app (ADR 0003), and no Terms change, since the Terms already allow
   removing content.
3. Release with a deploy-set flag that empties the listing for everyone until
   the operator clears it.

Recommendation: option 2. Option 1 delays a finished feature on an unrelated
build; option 3 hides every owner for one report and needs a deploy each time.
Option 2 removes one listing at once and can go when account blocking ships.
Choosing it changes the "A hide command" line in [Rejected](#rejected).

**N4.** An owner can turn the switch off and on to return to the top. Options:

1. Accept it, and revisit if owners abuse it.
2. Limit off-to-on toggles per account, for example 3 a day, in the existing
   [ADR 0007](../adr/0007-bounded-rate-limiter.md) bounded limiter with an
   account key. Its buckets live in memory, so the limit stores nothing and a
   restart resets it. ADR 0007 and [budgets](budgets.md) gain the budget, whose
   one-day window exceeds the one-hour longest window ADR 0007 states, and the
   dialog gains one message for a refused toggle.

Recommendation: option 1. Option 2 adds a budget, an ADR edit, and copy for
abuse not yet seen, and can follow later without a schema change.
