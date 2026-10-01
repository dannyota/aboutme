# Community showcase

The community showcase is a public page, `/showcase`, that lists published
resumes whose owners chose to show them, so visitors see real examples before
they sign up. Each listing shows the resume's stored link-preview card and links
to the public resume. Nothing about visitors is recorded.
[ADR 0029](../adr/0029-community-showcase.md) records the decisions.

Status: approved (2026-09-27), ready to build. The owner approved every choice
marked **Owner approval** S1 to S13, listed with its recommendation in the
[approvals](#owner-approvals) section, as written.

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
  review.
- Renaming the slug and editing the resume keep the opt-in; a change to what the
  listing shows sends it back to review (see [Review](#review)).
- Each resume opts in on its own. The listing never groups resumes by account
  and carries no account identifier (**Owner approval** S12).
- The owner may pick one role for the listing from a closed list, or none
  (**Owner approval** S3).
- Connected agents cannot read or change the switch through a tool, the same as
  every other publish setting ([ADR 0018](../adr/0018-mcp-agent-access.md)).

Showcase state for the owner, returned on the owner resume resource:

| State      | Meaning                                                           |
| ---------- | ----------------------------------------------------------------- |
| `off`      | No opt-in                                                         |
| `pending`  | Opted in; the current review key has no review result             |
| `listed`   | Opted in; the current review key is approved; the resume is shown |
| `declined` | Opted in; the current review key was declined                     |

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
`rel="nofollow"`. A tile never shows a contact detail, the slug as text, a date,
a view count, or anything from the resume body.

**Template.** The document stores no template identity
([template limits](templates/limitations.md)), so Go derives it: a resume
matches a preset when its `customization`, without the five leaves a template
apply keeps or resets for the owner (`pageFormat`, `dateFormat`,
`font.textAlign`, `header.photoPosition`, `colorScheme`) and without
`layout.sections`, equals the preset's `customization` without the same leaves,
compared as canonical JSON. A committed generator builds the Go preset table
from `packages/schema/templates/`, and a test fails when the table is stale. No
match shows "Custom design" (**Owner approval** S4).

**Language.** The primary subtag of the resume language: `vi`, `en`, or `other`.

**Roles.** The nine Library role chips plus Other: `backend`, `frontend`,
`mobile`, `devops`, `data-ai`, `qa`, `fresher`, `brse`, `security`, `other`,
with the Library's labels.

## Order and filters

Listings are ordered newest first by the time of their first approval in the
current opt-in, then by resume ID (**Owner approval** S5). A re-approval after
an edit keeps the first time, so editing never moves a resume up. There is no
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

## Review

The operator reviews every listing before it appears (**Owner approval** S6).
The showcase is a page aboutme.vn curates, so a listing speaks for aboutme.vn
more than a shared link does; at launch volumes a review costs minutes.

**Review key.** Go computes a review key for each opted-in resume: the first 16
hex digits of SHA-256 over the slug, the language, and the card envelope's
scrubbed name, headline, photo storage key digest, and crop. It leaves out the
card layout version and the accent, so a layout release or a color change does
not send every listing back to review. A resume is listed only while its current
review key equals the approved one.

**Keeping derived values current.** The showcase row holds the review key, the
current card version, and the derived template ID. Every committed write to an
opted-in resume (document, photo, and publish settings) recomputes all three in
the same transaction, through the one resume write boundary. At start, Go
recomputes them for every row, so a changed scrub rule, preset, or card layout
applies after a deploy.

**Operator commands.** ADR 0003 allows no operator surface in the public app, so
review runs out of band through the server binary's `showcase-review` command,
started as a one-shot production task:

| Command                | Effect                                                                            |
| ---------------------- | --------------------------------------------------------------------------------- |
| `pending`              | Lists slug, review key, card version, and request date of each pending opt-in     |
| `approve <slug> <key>` | Approves when `key` is still current; sets the first-listed time if unset         |
| `decline <slug> <key>` | Declines when `key` is still current; a listed resume leaves the showcase at once |
| `show <slug>`          | Prints the showcase state of one resume                                           |

The operator reviews the public page and the card at the printed version. A key
that changed since the list was printed makes `approve` and `decline` do nothing
and say so. Output and logs carry slugs, keys, and versions only, never a name
or headline.

**What the operator approves:** a real person's resume in the listed language,
the name matching the person, no impersonation, no illegal or offensive text or
image, no one else's personal data, no sensitive data (ID numbers, health,
religion, political views), no advertising or spam links, and a photo that is a
portrait or none.

**Takedown.** `decline` removes a listing at once and keeps it out until the
review key changes and a new review approves it. Content that breaks the Terms
is handled as for any public resume.

**Reports.** Each tile's Report link opens an email to `danny@aboutme.vn` with
the subject "Report showcase: {slug}". aboutme stores nothing about the
reporter.

**Operator notice.** At launch the operator runs `pending` by hand; no email or
alarm announces new requests, and the copy promises no review time (**Owner
approval** S7).

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
- An item appears only while the resume is live, sign in to view is off, the
  opt-in exists, the review result is approved, and the approved key equals the
  current key. The query checks all five, not only the opt-in row.

Because nothing is cached, the ADR 0010 rule holds without a fence: a listing
request that reaches the origin after opt-out, unpublish, sign in to view,
decline, rename, or delete succeeds reads the new state and omits the resume.
The card URL and the resume link return the uniform public 404 at the same
moment. An already open showcase tab keeps its tiles, and any card image it has
loaded, until the visitor reloads; there is no live update. An edit that changes
the review key removes the listing in the same commit. An edit that changes only
the card version (a color) keeps the listing; tiles fetched before the edit show
a missing image until reload.

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

At launch the operator turns the switch on for `/danny` in the publish dialog
and approves it, so the page opens with one real listing (**Owner approval**
S10). With no listings, the page shows the empty state below, and the nav link
stays.

## Copy

All interface text follows the interface language. Vietnamese uses "xuất bản"
for publish and "CV" for resume, as the rest of the product does.

### Publish dialog

| Part                       | Vietnamese                                                                                                                                                                                            | English                                                                                                                                                                 |
| -------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Switch                     | Hiện trong trang Cộng đồng                                                                                                                                                                            | Show in the community showcase                                                                                                                                          |
| Help                       | Hiện ảnh xem trước, mẫu, ngôn ngữ và vị trí của CV này tại aboutme.vn/showcase sau khi chúng tôi duyệt. Ai cũng xem được trang đó.                                                                    | Shows this resume's preview image, template, language, and role at aboutme.vn/showcase after we review it. Anyone can see that page.                                    |
| Role label                 | Vị trí hiển thị                                                                                                                                                                                       | Role shown                                                                                                                                                              |
| Role hint                  | Tùy chọn. Giúp người xem lọc theo vị trí.                                                                                                                                                             | Optional. Lets visitors filter by role.                                                                                                                                 |
| No role                    | Không chọn                                                                                                                                                                                            | None                                                                                                                                                                    |
| Other role                 | Khác                                                                                                                                                                                                  | Other                                                                                                                                                                   |
| Needs public               | Bật CV công khai để hiện trong trang Cộng đồng.                                                                                                                                                       | Turn on Public resume to show it in the community showcase.                                                                                                             |
| Needs open view            | Không dùng được khi bật Yêu cầu đăng nhập để xem.                                                                                                                                                     | Not available while Require sign-in to view is on.                                                                                                                      |
| Pending                    | Đang chờ duyệt. CV sẽ hiện trong trang Cộng đồng sau khi được duyệt.                                                                                                                                  | Waiting for review. The resume appears in the community showcase once approved.                                                                                         |
| Listed                     | Đang hiện trong trang Cộng đồng.                                                                                                                                                                      | Shown in the community showcase.                                                                                                                                        |
| Listed link                | Xem trang Cộng đồng                                                                                                                                                                                   | View the showcase                                                                                                                                                       |
| Declined                   | CV này chưa được duyệt để hiện trong trang Cộng đồng. Nếu bạn đổi họ tên, tiêu đề, ảnh, đường dẫn hoặc ngôn ngữ của CV, chúng tôi sẽ duyệt lại. Nếu có câu hỏi, hãy gửi email đến `danny@aboutme.vn`. | This resume was not approved for the community showcase. If you change its name, headline, photo, link, or language, we review it again. Questions: `danny@aboutme.vn`. |
| Issue `requires_open_view` | Tùy chọn này cần tắt Yêu cầu đăng nhập để xem.                                                                                                                                                        | This option needs Require sign-in to view off.                                                                                                                          |

### Showcase page

| Part              | Vietnamese                                                                                        | English                                                                                              |
| ----------------- | ------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| Nav link          | Cộng đồng                                                                                         | Community                                                                                            |
| Title and h1      | CV từ cộng đồng                                                                                   | Community resumes                                                                                    |
| Meta description  | CV thật do người dùng aboutme.vn chọn chia sẻ.                                                    | Real resumes that aboutme.vn users chose to share.                                                   |
| Lead              | CV thật do người dùng aboutme.vn xuất bản và chọn hiện ở đây. Mỗi CV được duyệt trước khi hiện.   | Real resumes that aboutme.vn users published and chose to show here. We review each one first.       |
| Order note        | CV mới được duyệt hiện trước.                                                                     | Newest approved first.                                                                               |
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

Changes to `legal.ts` (**Owner approval** S11). The "updated" date becomes the
release date.

| Section                                        | Vietnamese                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        | English                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| ---------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| What we collect, new item after Content        | Trang Cộng đồng: nếu bạn bật tùy chọn này cho một CV, chúng tôi lưu thời điểm bạn bật, vị trí bạn chọn, và kết quả duyệt.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         | Community showcase: if you turn it on for a resume, we store when you turned it on, the role you picked, and our review result.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| Why we use your data, optional features list   | (xuất bản CV công khai, cho phép lập chỉ mục, hiện CV trong trang Cộng đồng, kết nối trợ lý AI, đăng nhập bằng Google hoặc LinkedIn)                                                                                                                                                                                                                                                                                                                                                                                                                                                                              | (publishing, search and AI indexing, the community showcase, connected AI agents, Google or LinkedIn sign-in)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| Public only by your choice, new last paragraph | Trang Cộng đồng (aboutme.vn/showcase) chỉ hiện những CV mà chủ CV bật Hiện trong trang Cộng đồng. Trang này hiện ảnh xem trước của CV (họ tên, tiêu đề và ảnh của bạn), mẫu, ngôn ngữ và vị trí bạn chọn, kèm đường dẫn đến CV. Chúng tôi duyệt từng CV trước khi hiện, và duyệt lại khi họ tên, tiêu đề, ảnh, đường dẫn hoặc ngôn ngữ của CV thay đổi. Khi bạn tắt tùy chọn này, hủy xuất bản, hoặc bật Yêu cầu đăng nhập để xem, CV rời khỏi trang Cộng đồng ngay lập tức. Trang Cộng đồng không cho công cụ tìm kiếm lập chỉ mục, nhưng bất kỳ ai truy cập đều có thể xem và sao chép những gì trang hiển thị. | The community showcase (aboutme.vn/showcase) lists only resumes whose owners turn on Show in the community showcase. It shows the resume's preview image (your name, headline, and photo), its template, language, and the role you picked, with a link to the resume. We review each resume before it appears, and again when its name, headline, photo, link, or language changes. When you turn the option off, unpublish, or turn on Require sign-in to view, the resume leaves the showcase right away. Search engines are asked not to index the showcase, but anyone who visits it can see and copy what it shows. |

### Terms of Service

| Section                                                  | Vietnamese                                                                                                                                                                                                               | English                                                                                                                                                                                                           |
| -------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Your content, new paragraph after the first              | Nếu bạn bật Hiện trong trang Cộng đồng cho một CV, bạn cũng cho phép aboutme.vn hiện ảnh xem trước, mẫu, ngôn ngữ và vị trí bạn chọn của CV đó trên trang Cộng đồng, cho đến khi bạn tắt tùy chọn này hoặc hủy xuất bản. | If you turn on Show in the community showcase for a resume, you also let aboutme.vn show its preview image, template, language, and the role you picked on the showcase page, until you turn it off or unpublish. |
| Acceptable use, new `after` line before the existing one | Chúng tôi duyệt mọi CV trước khi hiện trong trang Cộng đồng và có thể từ chối hoặc gỡ CV khỏi trang này.                                                                                                                 | We review every resume before it appears in the community showcase and may decline or remove it.                                                                                                                  |

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
- **Abuse.** Spam, impersonation, offensive content, and other people's data are
  caught by the review before listing and removed by `decline`. Accounts need a
  verified email and hold at most three resumes, which bounds bulk opt-ins.
- **Minors.** The Terms require age 16; the review declines a resume that
  appears to belong to a younger person.
- **Retention.** The opt-in row, with its request time, role, review result, and
  review time, exists only while the opt-in does. It is deleted on opt-out,
  unpublish, sign in to view, and resume or account deletion. Nothing about an
  ended opt-in is kept.
- **Export.** The account export includes each resume's showcase state, role,
  and request time.
- **Impact assessment.** The next regular update of the data protection impact
  assessment notes the showcase; the privacy and disclosure review gate in
  [decision status](decisions.md) covers the new text.

## Data and contract

**Table `resume_showcase`**, one row per opted-in resume:

| Column            | Rule                                                                 |
| ----------------- | -------------------------------------------------------------------- |
| `resume_id`       | Primary key; references `resumes` with `ON DELETE CASCADE`           |
| `requested_at`    | Not null                                                             |
| `role`            | Null or one of the ten roles                                         |
| `review_key`      | 16 lowercase hex, not null; maintained by Go                         |
| `card_version`    | 16 lowercase hex, not null; maintained by Go                         |
| `template_id`     | Null or a preset ID matching the slug grammar, at most 64 characters |
| `reviewed_key`    | Null or 16 lowercase hex                                             |
| `review_outcome`  | Null, `approved`, or `declined`                                      |
| `reviewed_at`     | Null or a time; the three review columns are all null or all set     |
| `first_listed_at` | Null until the first approval of this opt-in; never changed after    |

A partial index on `(first_listed_at DESC, resume_id DESC)` where
`review_outcome = 'approved'` serves the listing. `aboutme_app` gets select,
insert, update, and delete. A row is about 150 bytes; three resumes per account
bound the table. The migration only adds the table and the slug check; a
rollback leaves the table unused, and the showcase is gone until the release
returns. Nothing in the resume document changes.

**Publish request.** `PublishResumeRequest` gains two optional fields. Omitted
keeps the stored value, so an older web tab that does not know them leaves the
opt-in unchanged.

- `showcaseEnabled` (boolean). `true` with `live` false is `requires_live`;
  `true` with sign in to view on is the new issue `requires_open_view`. A
  request that turns on sign in to view or turns off `live` ends the opt-in even
  when the field is omitted.
- `showcaseRole` (string). Empty clears it; any value outside the ten roles is
  `invalid_format`. Allowed only with the switch on.

The owner resume resource gains `showcase`: `null` when off, otherwise
`{ state, role }` with `state` in `pending`, `listed`, `declined`. The public
resume JSON is unchanged. OpenAPI gains the listing path and these fields. MCP
tools and the resume document schema are unchanged.

**Security.** The listing route is unauthenticated `GET` and `HEAD` with strict
query parsing, no cookie reads, and closed output fields; the page renders every
value as escaped text and loads images only from its own origin under the
existing app CSP. The review command needs database credentials and runs only as
a one-shot task under the deploy role; the public app gains no privileged route.

## Rejected

| Option                                   | Why not                                                                                           |
| ---------------------------------------- | ------------------------------------------------------------------------------------------------- |
| Listed at once, with report and takedown | aboutme.vn curates the page; abuse would be public until someone reports it                       |
| An in-app review queue for the operator  | ADR 0003 forbids an operator surface in the public app                                            |
| A first-page thumbnail of the resume     | A new stored artifact and render per edit; its text can show contact details in the body          |
| Owner-chosen template label              | Goes stale after a template switch and can be wrong; derivation is exact                          |
| Order by views or recent edits           | Publishes owner-only counts, or rewards edits made only to move up                                |
| Indexable showcase                       | Would index names of people who left SEO off; showing only SEO-on resumes to crawlers is cloaking |
| Showcase implies SEO on                  | Couples two choices the owner makes separately                                                    |
| A cached or server-rendered listing      | Needs a discovery-style fence for mutable names; per-request reads meet ADR 0010 without one      |
| `/cong-dong` as the route                | Every other fixed root is English, and the interface language is not in the path                  |

## Owner approvals

Each item needs the owner's answer; the last column records it. The owner
approved every item on 2026-09-27, as its recommendation reads, with the choices
below settled.

| ID  | Decision                                                                                                                      | Owner decision (2026-09-27)                                              |
| --- | ----------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------ |
| S1  | Separate switch, off by default, live and open view only; unpublish, sign in to view, and delete end it; republish starts off | Approved as written                                                      |
| S2  | Tile shows the stored preview card, template, language, optional role, and Report; no contact, slug text, date, or count      | Approved; a page thumbnail can follow later as its own decision          |
| S3  | Owner picks an optional role from the nine Library roles plus Other                                                           | Approved                                                                 |
| S4  | Template derived by exact token match, else "Custom design"                                                                   | Approved                                                                 |
| S5  | Newest first by first approval; filters role, language, template in the URL; no popularity order                              | Approved                                                                 |
| S6  | Review before listing through an out-of-band command; re-review when the review key changes; Report by email                  | Approved                                                                 |
| S7  | No operator notice at launch and no promised review time in the copy                                                          | Approved; add a daily pending-count email once requests pass five a week |
| S8  | `/showcase` noindex and nofollow, outside sitemap and `llms.txt`; SEO and GEO switch unchanged                                | Approved                                                                 |
| S9  | Route `/showcase`; nav link Community / Cộng đồng after Library                                                               | Approved                                                                 |
| S10 | Launch with `/danny` opted in and approved; empty state as written; nav link from day one                                     | Approved; the owner wants `/danny` listed                                |
| S11 | Privacy and Terms text above; update the dates; no advance email, since nothing changes for anyone who does not opt in        | Approved: the text as written, and no advance email                      |
| S12 | Each resume of an account may be listed on its own, never grouped                                                             | Approved                                                                 |
| S13 | Twelve listings per page, lazy card images                                                                                    | Approved                                                                 |
