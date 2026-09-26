# Verify page

The page "Kiểm chứng phiên bản đang chạy" ("Verify what's running") shows the
[deployment document](document.md) as a chain from source to running service,
with one clear status at the top and a way to check it independently. It is
bilingual, works at phone width and in dark mode, and reveals no personal data.
The designer turns this page into a visual spec in `DESIGN.md` and reviews it
before the build starts.

## Path

**Owner approval (A1):** `/verify`. It matches the approved label "Kiểm chứng" /
"Verify", is short to type and say, and reads the same in both languages.
`/transparency` is longer and names the idea rather than the action. The risk of
`/verify` is confusion with `/verify-email`; the page title removes it.

Either path is a new fixed root in the public-root registry (version 9), so it
becomes a reserved slug. Before that release, devops confirms with a read-only
query in production that no resume and no tombstone holds the slug. If one does,
the owner chooses another path; a live resume is never displaced.

The machine document stays at `/.well-known/deployment.json`.

## Footer link

The landing footer lists `aboutme.vn`, Terms, Privacy, and then "Kiểm chứng" /
"Verify", with the same link style and a `data-testid` of `landing-verify-link`.
The label lives beside the other footer labels in the web i18n copy.

## Layout

The page uses the Aurora canvas and existing card, button, and tooltip
primitives. Top to bottom:

1. **Title** and one sentence: "Trang này cho thấy chính xác phiên bản đang chạy
   trên aboutme.vn và cách tự kiểm chứng." / "This page shows exactly which
   build runs aboutme.vn and how to check it yourself."
2. **Status card** with the summary state, its mark, one line of detail, and
   "Kiểm tra lúc 10:14" / "Checked at 10:14" in the viewer's local time, plus
   the relative age.
3. **Chain** for the release: Source → Build → Image → Running. Each step shows
   its key value and a match mark; each connector between steps shows whether
   the two sides agree. On wide screens the steps run left to right; below 42
   rem they stack as a vertical list with the connectors between.
4. **Components:** one row each for `server`, `web`, `caddy`, and `maintenance`
   while it runs. A row shows the short digest with a copy button, the version,
   the replica count, and the signature and SBOM marks. During a rollout a row
   lists each running image with its own replica count, newest last.
5. **Verify it yourself:** the commands from
   [verification](verification.md#verify-it-yourself) with live values, a
   component selector, and a copy button, plus links to the JSON document, the
   transparency log entry, the build run, and the GitHub CLI manual.
6. **Limits:** three plain sentences ([below](#limits)).

| Step    | Value shown                                          | Match when                                              |
| ------- | ---------------------------------------------------- | ------------------------------------------------------- |
| Source  | `dannyota/aboutme`, tag, short commit, links         | The signed certificate names this repository and tag    |
| Build   | "GitHub Actions, release-images", run link           | The signer workflow matches the expected identity       |
| Image   | `ghcr.io/dannyota/aboutme-*`, short digest           | The signed subject equals the digest                    |
| Running | "aboutme.vn, AWS Singapore", replicas, running since | The platform reports that digest; the document is fresh |

A digest shows as `sha256:d1c33a0a…945c` (the first 8 and last 4 hex characters)
in a monospace face. The full value is in the accessible name and the copy
button's payload. Copy confirms with "Đã sao chép" / "Copied" in a status
announcement.

## States

The page decides the state in this order; the first match wins. Only the last
row can show a success mark.

| State         | When                                                              | Vietnamese                                     | English                                        |
| ------------- | ----------------------------------------------------------------- | ---------------------------------------------- | ---------------------------------------------- |
| Unavailable   | Fetch fails, 404, or the body is not a JSON object                | Máy chủ này không công bố thông tin triển khai | This server publishes no deployment record     |
| Outdated page | `schema_version` is higher than the page knows                    | Trang đã cũ. Hãy tải lại.                      | This page is out of date. Reload it.           |
| Stale         | The [staleness rule](README.md#run-freshness-and-staleness) fires | Chưa kiểm tra lại từ {time}                    | Not checked since {time}                       |
| Mismatch      | `summary` is `mismatch`                                           | Có thành phần không khớp với bản dựng đã ký    | Something running doesn't match a signed build |
| Unverified    | `summary` is `unverified`                                         | Chưa kiểm tra được chữ ký                      | Signatures not checked yet                     |
| Rolling out   | `summary` is `rolling_out`                                        | Đang cập nhật, hai phiên bản cùng chạy         | Update in progress, two versions running       |
| Verified      | `summary` is `verified`                                           | Đang chạy đúng mã nguồn trên GitHub            | Running exactly what's on GitHub               |

The page also checks what it can itself: it recomputes `summary` from the
components and shows Mismatch if the two disagree. Unknown `status` values count
as not verified.

Marks follow the single-meaning color rules in `DESIGN.md`. Seal red means
public state, so no transparency state uses it. The designer picks the marks: a
proposal is a blue check for verified, an indigo arrow pair for rolling out,
muted text for stale and unverified, and the destructive token for mismatch.
Every state also has text, so color never carries it alone.

The owner reviews the Vietnamese copy before release, as for the privacy notice.

## Limits

Shown in both languages under the heading "Trang này chứng minh gì" / "What this
proves":

- It shows what AWS reports is running, not proof that the server itself is
  honest.
- It covers the code, not settings or secrets.
- A verified build means GitHub built it from public source, not that the source
  has no bugs.

The [overview](README.md#what-this-proves) holds the full list.

## Behavior

- The page is server-rendered in the visitor's language with the title,
  explanation, and limits. The browser fetches the document from the same origin
  with `cache: 'no-cache'` and fills the status, chain, and components. Until it
  arrives, those areas show a neutral "Đang tải…" / "Loading…" state, never a
  mark.
- It refetches every 60 seconds while the tab is visible and announces a state
  change politely. It stops when the tab is hidden.
- Without JavaScript, the page shows the explanation, a link to the JSON
  document, and the verify commands with placeholders.
- The page makes no request to any other origin. GitHub and Sigstore links open
  only on click, with `rel="noopener noreferrer"`.
- `/verify` joins the localized routes in `useRouteLocale()` and the language
  coverage list in `DESIGN.md`, uses the marketing header CTA like `/terms`, is
  indexable, and has its own title and description.

## Accessibility

The status card is a `role="status"` region. The chain is an ordered list whose
items name their state in text. Copy buttons are real buttons with names such as
"Sao chép mã băm của server" / "Copy the server digest". Code blocks scroll
horizontally inside their card at 360 px wide and never widen the page. Text
meets the contrast rules in `DESIGN.md` in both themes.

## Privacy

The document and the page carry no personal data: no user, resume, visitor, or
commit author data, and no infrastructure identifiers
([sanitizer](document.md#sanitizer)). The page sets no cookie beyond the
existing locale and theme cookies and loads no analytics.
