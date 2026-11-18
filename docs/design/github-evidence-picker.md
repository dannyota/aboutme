# GitHub evidence: picker and project page

Status: Accepted (2026-11-17). Roadmap features 6 and 7. Builds on the
[GitHub connection](github-evidence.md) and
[ADR 0030](../adr/0030-github-evidence.md), which own the connection, token,
evidence session, limits, confirmation rules and privacy. This page owns
attaching evidence to a library project, the GitHub reads behind it, its API,
storage and the project page; where the connection design and this page overlap,
this page wins and the connection design links here.

## Model

An evidence item belongs to one library project. Its mark is derived at read
time, never stored:

- **Confirmed** when its last check met the item's rule, the account has a live
  connection, and that connection's binding is the one the check ran under.
- **Flagged** when its last check fetched the item and it failed the rule
  (`rule_failed`), or GitHub answered 404 or 410 for it (`gone`). It stays a
  plain link until the owner checks again or removes it.
- **Plain link** otherwise.

The connection gains a `binding` counter beside its `generation`. `generation`
still advances on every connect and disconnect and fences sessions. `binding`
advances only when the connection is cleared, or installed for a GitHub user id
other than the one it held; a Continue with the same GitHub account keeps it, so
starting a session never drops a mark. Each check stores the binding it ran
under.

Connecting while a connection holds a different, non-null GitHub user id and the
account has evidence is refused: the callback redirects with
`evidence=different_account` and Settings asks the owner to disconnect first.
Without evidence, a different account replaces the connection as today. So
disconnect is the only connection change that alters what a page shows.

A transient GitHub failure changes no stored item.

## GitHub reads

Every call uses the live session's token, `Accept: application/vnd.github+json`,
`X-GitHub-Api-Version: 2022-11-28` and `User-Agent: aboutme.vn`, under the
[outbound rules](github-evidence.md#outbound-requests): fixed hosts, 5 s and 512
KiB per response, redirects never followed. The client's allowlist grows by
exactly these shapes, with query strings built by the server from fixed names.

| Use                     | Call                                                                                                       | Kept                                                          |
| ----------------------- | ---------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------- |
| List repositories       | `GET /user/repos?affiliation=owner&visibility=public&sort=pushed&per_page=30&page=n`                       | id, full name, owner id, private, fork, created               |
| List merged PRs         | `GET /search/issues?q=is:pr+is:merged+is:public+author:{login}&sort=updated&order=desc&per_page=30&page=n` | repository URL, number, title, closed, `incomplete_results`   |
| List releases           | `GET /repos/{owner}/{repo}/releases?per_page=30&page=n`                                                    | id, tag, name, draft, published, author id                    |
| Repository by name      | `GET /repos/{owner}/{repo}`                                                                                | id, full name, owner id, private, fork, created               |
| Repository after rename | `GET /repositories/{id}`, only as below                                                                    | as above                                                      |
| Pull request            | `GET /repos/{owner}/{repo}/pulls/{number}`                                                                 | id, number, title, merged, `merged_at`, `user.id`, HTML URL   |
| Release                 | `GET /repos/{owner}/{repo}/releases/{release_id}` or `/releases/tags/{tag}`                                | id, tag, name, draft, prerelease, `published_at`, `author.id` |

**Repository resolution.** Attach and check again first fetch the repository by
name: from the pasted link, from a search result's repository URL, or from the
stored full name. When GitHub answers 301 for a renamed or transferred
repository, the client does not follow it; it accepts the `Location` only when
it is exactly `https://api.github.com/repositories/{digits}` and, on check
again, the digits equal the stored repository id, then makes that one call. Any
other `Location` answers `evidence_upstream` and changes nothing, so following a
rename is best effort. A repository id that differs from the stored one after
resolution flags the item `gone`.

**Rules.**

- Repository evidence: public, not a fork, and its owner id equals the
  connection's GitHub user id. Archived repositories count.
- Pull request: the repository is public, the pull request has `merged_at`, and
  its `user.id` equals the connection's GitHub user id. The repository's owner
  does not matter.
- Release: the repository is public, the release is not a draft, has
  `published_at`, and its `author.id` equals the connection's GitHub user id.
  Prereleases count. A release whose author is a bot fails.
- Confirmation compares ids only; the login in the search query is a filter.

**Dates.** Repository: `created_at`. Pull request: `merged_at`. Release:
`published_at`.

**Costs.** A listing page is one call; an attach or check again is two (three
after a rename). Each counts toward the 120 calls per account per hour.

**GitHub errors.**

| GitHub answer                                                                      | Result                                                                                                                                     |
| ---------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------ |
| 401                                                                                | The session ends and its token is revoked; `evidence_session_required`                                                                     |
| 429, or 403 with `retry-after`, `x-ratelimit-remaining: 0` or a rate-limit message | `rate_limited`; `Retry-After` is GitHub's `retry-after`; else, when `x-ratelimit-remaining` is 0, the time to `x-ratelimit-reset`; else 60 |
| 404 or 410 for the item or its repository                                          | Attach: `evidence_not_confirmed`. Check again: flagged `gone`. On a listing: `evidence_upstream`                                           |
| Any other 4xx or 5xx, a timeout, an oversized or unparsable body                   | `evidence_upstream`; nothing stored changes                                                                                                |

A pasted link is parsed, never fetched as given. Accepted after trimming:
`https://github.com/{owner}/{repo}`, `/pull/{number}` and `/releases/tag/{tag}`,
with an optional trailing slash. `http`, another host, `www.`, more segments, a
query or a fragment answer `evidence_unsupported_link`. The kind comes from the
link; a request kind that disagrees answers the same error.

## Picker

The project editor gains an **Evidence** section after Details and before the
resumes a project appears on. It is absent when the capability is off.

- **Add evidence from GitHub** opens a dialog (a bottom sheet below 640 px) with
  four tabs: Repositories, Pull requests, Releases (choose one of the owner's
  repositories, then its releases) and Paste a link. Each row has its own
  **Attach**; one attach per request, applied at once like linking, and disabled
  while the editor has unsaved text edits. Rows already on this project show
  "Attached" instead.
- Without a live session the dialog shows **Continue with GitHub**. The start
  call's JSON body may carry `returnTo`, a string that must equal
  `/app/projects/{uuid}` for a project of the account; the server stores the
  project id in the OAuth transaction and the callback redirects to the route it
  builds from that id, with `evidence=connected` or `evidence=failed`, and the
  editor reopens the dialog. A missing, malformed or foreign value falls back to
  Settings; a repeated key fails the request.
- States: loading rows; an empty tab; GitHub's incomplete search results
  ("GitHub returned only part of the results; paste a link to find more"); an
  upstream error with **Try again**; a rate limit with the time it lifts; a
  session that ended (the list reloads after Continue); the 10-item limit.
- The section lists items in the owner's order with move up and down buttons,
  **Check again** and **Remove**. A flagged item shows its reason; the owner
  checks again or removes it, and until then it renders as a plain link.
- When the project has no evidence yet, the dialog states above the tabs that
  the project page will link to the owner's GitHub activity and that every
  resume showing the project shows it.

## API contract

Session cookie and CSRF only. `kind` is `repository`, `pullRequest` or
`release`; evidence ids are UUIDs.

- `GET /api/v1/projects/{id}/evidence/candidates?kind=&page=&repository=`
  replaces the account-level candidates route. `page` is 1 to 10; `repository`,
  a numeric GitHub repository id from a repository listing, is required for
  releases. It answers
  `{items: [{candidateId, kind, title, repository: {id, fullName}, date, attached}], nextPage, incomplete}`.
  `repository.id` is `null` for pull requests, whose search result carries no
  repository id. Repository listings drop forks and private repositories, and
  release listings drop drafts and releases by another author, after fetching,
  so a page may hold fewer than 30 rows. A release listing for a repository id
  the live session did not list answers `evidence_session_required`. A pull
  request's candidate date is `closed_at`; attach stores `merged_at`. "Attached"
  matches a pull request by repository full name and number; `nextPage` is
  `page + 1` when GitHub returned 30 rows and `page` is below 10, else `null`.
- `POST /api/v1/projects/{id}/evidence` with `{kind, link}` or
  `{kind, candidateId}`; `POST .../{evidenceId}/recheck`;
  `PUT .../evidence/order` with `{order: [evidenceId]}` naming every item once;
  `DELETE .../{evidenceId}`. Each is a library mutation and answers 200 with
  `{evidence, projectRevision, resumeRevisions}`.
- Project reads and the export's project record gain a read-only `evidence`
  array of
  `{id, kind, url, repositoryName, title, itemDate, checkedAt, confirmed, flagReason}`,
  `flagReason` being `rule_failed`, `gone` or `null`. Export stays version 2;
  the record carries no generation, binding or account id.

| Code                          | Status | When                                                                                  |
| ----------------------------- | ------ | ------------------------------------------------------------------------------------- |
| `evidence_unavailable`        | 404    | Capability off, on every evidence route including the project-nested ones             |
| `evidence_session_required`   | 409    | No live session, a candidate from an ended session, or GitHub's 401                   |
| `evidence_not_confirmed`      | 422    | The item fails its rule, or GitHub reports it gone, on attach                         |
| `evidence_duplicate`          | 409    | The same kind and item id are already on this project                                 |
| `evidence_limit`              | 422    | An eleventh item                                                                      |
| `evidence_unsupported_link`   | 422    | A link outside the accepted grammar                                                   |
| `evidence_upstream`           | 502    | Any GitHub failure not listed above, and a 404 or 410 on a listing                    |
| `rate_limited`                | 429    | The hourly budget or GitHub's limit; `Retry-After` gives the seconds                  |
| `evidence_connection_changed` | 409    | Disconnect named a generation that is no longer current                               |
| `evidence_not_found`          | 404    | An evidence id that is not an item of this project, on check again, remove or reorder |

**Size.** Each stored item counts as 1 KiB toward the 96 KiB project bound,
whatever its content and whether or not the capability is on. Attach and every
project write check the document plus 1 KiB per item, and a write over it gets
the existing project size error.

When the capability is off, stored evidence is kept, exported and deleted with
the account, but project reads omit the `evidence` field and nothing renders.

## Storage

Migration `00021` adds `binding` to `github_connections` and the table
`project_evidence`, cascading from `projects`: `id` UUID, `project_id`,
`position`, `kind`, `repository_id`, `item_id` (the repository, pull request or
release id), `pull_number`, `release_tag`, `url` (at most 300),
`repository_name` and `title` (at most 200 characters), `item_date`,
`checked_at`, `checked_binding`, `rule_met`, `flag_reason`, unique on
`(project_id, kind, item_id)`. The 10-item bound is checked under the owner
lock. Check again rewrites every field after `item_id` when GitHub answers; a
transient failure rewrites nothing.

## Disconnect

`DELETE /api/v1/github-evidence?keepLinks=&generation=` becomes a guarded write
under [ADR 0006](../adr/0006-transactional-idempotency.md):

- `generation` is required: the top-level integer `GET /api/v1/github-evidence`
  reports beside `hasEvidence`, 0 when no connection row exists. A different
  current value answers 409 `evidence_connection_changed`, so a lost response
  cannot remove evidence attached after a reconnect. No ETag is involved.
- `Idempotency-Key` is required. The fingerprint covers the route, `keepLinks`
  and `generation`. A stored replay for the key is answered before the
  generation check; the same key with a different fingerprint answers the
  existing idempotency conflict.
- When any evidence item exists, the affected set is every live resume linking a
  project with evidence. Planning reads it and acquires and closes the fences
  outside the account transaction, in the coordinator's order, as for a
  [library mutation](projects-contract.md#library-mutations); after three failed
  plans it answers `public_state_busy` and nothing changes.
- Inside the owner-locked transaction it rechecks the affected set and the
  generation, clears the connection (advancing `generation` and `binding`), with
  `keepLinks=false` deletes every evidence item of the account and advances each
  changed project's revision, advances each affected resume's revision
  non-draining, and writes the idempotency record. Ending the session and
  revoking its token come last before commit, under the
  [connection's ending rules](github-evidence.md#evidence-session).
- It answers 200 with `{generation, projectRevisions, resumeRevisions}`. Open
  editors meet an advanced project revision as any other conflict.
- Settings asks before disconnecting when `hasEvidence` is true: keep the items
  as plain links (the default) or remove them from every project.

## Project page

Evidence has its own section after the project text and before related projects,
never inside the facts panel. A heading, then one row per item in the owner's
order: for a confirmed item the label, the title as a link, the repository name,
the item date and "checked on {date}"; otherwise the title as a link and the
repository name. Dates follow the resume's date format and language, in UTC. No
stars and no images; static SVG icons only.

- The project envelope gains `evidence` with
  `{kind, title, url, repositoryName, itemDate, checkedAt, confirmed}`; the
  renderer derives the label from `kind` and `confirmed`. The server builds
  `url` itself, always under `https://github.com/`. The web and Go decoders and
  the parity corpus change together; the project page format version rises to 3.
- The Go validator admits `a.project-evidence` only on a project page, exactly
  once per envelope item and in envelope order, with exactly the attributes
  `class="project-evidence"`, `href` equal to the item's `url`, and
  `rel="noopener noreferrer nofollow"`, and with text equal to the item's title.
  Any other evidence anchor, or one on the list page, fails the page.
- Evidence appears nowhere else: not on the resume page, the PDF, Markdown,
  public JSON, JSON-LD, the head, previews, the showcase or `llms.txt`.

## Revocation and privacy

Attach, check again, reorder and remove are project edits in the
[transition table](projects-contract.md#generations-and-revocation):
non-draining over each linking live resume, an ordinary row write otherwise.
Disconnect advances the same resumes as above, so no cached page keeps a mark
after it. Viewing a page reads only stored evidence: nothing reaches GitHub, and
nothing about a viewer is stored.

## MCP

`get_project` returns the read-only `evidence` array; `list_projects` omits it.
No tool input names evidence, and the decoders keep rejecting unknown fields.
`delete_project` deletes a project with its evidence, as deleting it in the app
does; no tool attaches, checks again, reorders or removes a single item.

## Copy

| Key                | Vietnamese                                                                                                | English                                                                                                         |
| ------------------ | --------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| Section            | Bằng chứng trên GitHub                                                                                    | GitHub evidence                                                                                                 |
| Add                | Thêm bằng chứng từ GitHub                                                                                 | Add evidence from GitHub                                                                                        |
| Tabs               | Kho mã · Pull request · Bản phát hành · Dán đường dẫn                                                     | Repositories · Pull requests · Releases · Paste a link                                                          |
| Attach             | Gắn                                                                                                       | Attach                                                                                                          |
| Attached           | Đã gắn                                                                                                    | Attached                                                                                                        |
| Pull request label | Tác giả của pull request đã được merge                                                                    | Author of a merged pull request                                                                                 |
| Release label      | Tác giả của bản phát hành này                                                                             | Author of this release                                                                                          |
| Repository label   | Chủ sở hữu kho mã này                                                                                     | Owner of this repository                                                                                        |
| Checked            | kiểm tra ngày {date}                                                                                      | checked on {date}                                                                                               |
| Check again        | Kiểm tra lại                                                                                              | Check again                                                                                                     |
| Rule failed        | Lần kiểm tra gần nhất không còn đáp ứng điều kiện.                                                        | The last check no longer met the rule.                                                                          |
| Gone               | GitHub không còn tìm thấy mục này.                                                                        | GitHub no longer finds this item.                                                                               |
| Empty              | Không tìm thấy mục nào.                                                                                   | Nothing found.                                                                                                  |
| Incomplete         | GitHub chỉ trả về một phần kết quả; hãy dán đường dẫn để tìm thêm.                                        | GitHub returned only part of the results; paste a link to find more.                                            |
| Upstream           | GitHub chưa phản hồi. Hãy thử lại sau.                                                                    | GitHub did not respond. Try again later.                                                                        |
| Limit              | Mỗi dự án có tối đa 10 mục.                                                                               | A project holds at most 10 items.                                                                               |
| First attach       | Trang dự án sẽ có đường dẫn tới hoạt động GitHub của bạn, và mọi CV hiện dự án này cũng hiện các mục này. | The project page will link to your GitHub activity, and every resume that shows this project shows these items. |
| Different account  | Tài khoản GitHub này khác tài khoản đã kết nối. Hãy ngắt kết nối trước.                                   | This GitHub account differs from the connected one. Disconnect first.                                           |
| Disconnect ask     | Giữ các mục đã gắn dưới dạng đường dẫn thường?                                                            | Keep the attached items as plain links?                                                                         |
| Disconnect keep    | Giữ đường dẫn                                                                                             | Keep links                                                                                                      |
| Disconnect remove  | Xoá khỏi mọi dự án                                                                                        | Remove from every project                                                                                       |
