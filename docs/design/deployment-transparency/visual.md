# Verify page visual spec

This is the visual spec for `/verify`, "Kiểm chứng phiên bản đang chạy" /
"Verify what's running". [page.md](page.md) owns the behavior, the state order,
and the data; this page owns how it looks. It uses the Aurora tokens and
primitives in [DESIGN.md](../../../DESIGN.md) and adds no theme value except
`--surface-destructive`.

## Frame

The page uses the marketing shell and header call to action like `/terms`, on
the Aurora canvas. It has no footer of its own. Content sits in one column.

| Measure          | Below 42 rem (390 px phone) | From 42 rem (1280 px desktop) |
| ---------------- | --------------------------- | ----------------------------- |
| Column width     | Full width, 16 px gutters   | 60 rem, 24 px gutters         |
| Top and bottom   | 32 px, 64 px                | 48 px, 96 px                  |
| Gap between rows | 32 px                       | 40 px                         |
| `h1`             | 24 px, weight 700           | 32 px, weight 700             |
| Card padding     | 20 px                       | 28 px                         |
| Chain            | Vertical list               | Four columns                  |
| Component row    | Name, then image lines      | 10 rem name column            |
| Limits           | One column                  | Two columns, 32 px gap        |

Top to bottom: title and lead, status card, chain card, components card, verify
it yourself card, and the limits section. The lead is 16 px `--muted-foreground`
at most 40 rem wide. Section headings are `h2` at 18 px, weight 600, 16 px above
their content. A section hint sits on the heading's baseline at the right, 13 px
`--muted-foreground`, and wraps under it on phones.

Cards are `--card` with a border, the 20 px feature radius, and
`--shadow-product`. Nothing on the page uses seal red, a gradient, or motion.

## Tokens

The page defines these on its root element, never in `theme.css`, except
`--surface-destructive`, which the build adds beside the other surface tokens.

| Token                   | Light and dark value                                    | Use                     |
| ----------------------- | ------------------------------------------------------- | ----------------------- |
| `--verify-verified`     | `--link`                                                | Verified mark and chips |
| `--verify-rolling`      | `--brand-indigo`                                        | Rollout mark and rail   |
| `--verify-neutral`      | `--muted-foreground`                                    | Stale and unknown marks |
| `--verify-bad`          | `--destructive`                                         | Mismatch mark and text  |
| `--surface-destructive` | `--destructive` mixed into `--card`: 7% light, 12% dark | Mismatch tint           |

Measured contrast: `--destructive` on `--surface-destructive` is 5.9:1 light and
5.2:1 dark; `--muted-foreground` on every card tint is at least 5.1:1; the white
icon on the indigo disk is 5.0:1 and the dark icon 5.9:1.

## Status card

A grid with the mark, the text, and the release pill. Below 42 rem the mark is
40 px with a 22 px icon, the gap is 16 px, and the pill drops under the text.
From 42 rem the mark is 56 px with a 28 px icon, the gap is 20 px, and the pill
sits at the top right.

- Title: 20 px, 24 px from 42 rem, weight 700.
- Detail: 15 px, one sentence.
- Meta row: 13 px `--muted-foreground`: "Kiểm tra lúc 10:14" / "Checked at
  10:14", the relative age, and "Tự làm mới mỗi phút" / "Refreshes every
  minute", 12 px apart and wrapping.
- Pill: 28 px high, card fill, border, 13 px weight 600.

| State         | Mark (Lucide)            | Mark color                                       | Card fill               |
| ------------- | ------------------------ | ------------------------------------------------ | ----------------------- |
| Loading       | None                     | None                                             | `--muted`               |
| Unavailable   | `info`, ring             | `--verify-neutral` ring and icon                 | `--muted`               |
| Outdated page | `refresh-cw`, ring       | `--verify-neutral` ring and icon                 | `--muted`               |
| Stale         | `clock`, ring            | `--verify-neutral` ring and icon                 | `--muted`               |
| Mismatch      | `shield-alert`, filled   | `--verify-bad` disk, white or `--background`     | `--surface-destructive` |
| Unverified    | `circle-dashed`, ring    | `--verify-neutral` ring and icon                 | `--muted`               |
| Rolling out   | `arrow-right-left`, fill | `--verify-rolling` disk, white or `--background` | `--surface-indigo`      |
| Verified      | `shield-check`, filled   | `--verify-verified` disk, `--card` icon          | `--surface-blue`        |

A ring is a 2 px `--muted-foreground` border on a transparent disk. The filled
icon is white in light theme and `--background` in dark theme. The mismatch card
takes a border of `--destructive` mixed 45% into `--border`, and its detail line
is `--destructive` text.

Titles come from [page.md](page.md#states). Detail lines and pills:

| State         | Detail, Vietnamese                                                                     | Detail, English                                                                      | Pill                         |
| ------------- | -------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------ | ---------------------------- |
| Loading       | Đang tải…                                                                              | Loading…                                                                             | None                         |
| Unavailable   | Bạn vẫn có thể mở tài liệu JSON hoặc chạy các lệnh bên dưới.                           | You can still open the JSON document or run the commands below.                      | None                         |
| Outdated page | Máy chủ dùng định dạng mới hơn trang này.                                              | The server uses a newer format than this page.                                       | Button: Tải lại / Reload     |
| Stale         | Bản ghi đã cũ {age}. Các giá trị bên dưới là của lần kiểm tra đó, không phải hiện tại. | The record is {age} old. The values below are from that check, not from now.         | v0.6.0 lần cuối / last known |
| Mismatch      | {component} đang chạy một image không có chữ ký của GitHub.                            | {component} runs an image that has no signature from GitHub.                         | None                         |
| Unverified    | Chưa kiểm tra xong chữ ký của {component}. Trang tự làm mới sau một phút.              | The signature check for {component} hasn't finished. The page refreshes in a minute. | None                         |
| Rolling out   | {component} đang chuyển từ {old} sang {new}. Cả hai bản đều đã được ký.                | {component} is moving from {old} to {new}. Both builds are signed.                   | v0.5.21 → v0.6.0             |
| Verified      | Cả 3 thành phần chạy bản {version}, do GitHub dựng và ký từ commit {commit}.           | All 3 components run {version}, built and signed by GitHub from commit {commit}.     | v0.6.0 `3b1f9e0`             |

Mismatch has three more detail lines, chosen by cause:

- Invalid signature: "Chữ ký của {component} không hợp lệ." / "The signature for
  {component} is invalid."
- Missing component: "{component} không có image nào đang chạy." / "{component}
  has no running image."
- The page's own summary disagrees with the document: "Bản tóm tắt không khớp
  với từng thành phần." / "The summary doesn't match the components."

When more than one component fails, the line names the first in document order
and adds "và {n} thành phần khác" / "and {n} more". The stale meta row reads
"Kiểm tra lần cuối lúc 10:02" / "Last checked at 10:02" and drops the refresh
note. The Outdated page button is the `outline` button variant.

## Chain

The chain card's heading is "Từ mã nguồn đến bản đang chạy" / "From source to
running", with the hint "Mỗi bước phải khớp với bước trước." / "Each step must
match the one before it." It is an `ol` of four steps. Each step has a 40 px
`--muted` disk with a 20 px icon and a border, a 13 px label, a 16 px weight 600
value, a 13 px muted line, and a state chip.

| Step    | Icon                    | Label VI / EN       | Value                        | Muted line                                                                          |
| ------- | ----------------------- | ------------------- | ---------------------------- | ----------------------------------------------------------------------------------- |
| Source  | `git-commit-horizontal` | Mã nguồn / Source   | Version                      | `dannyota/aboutme` @ short commit, both links                                       |
| Build   | `workflow`              | Bản dựng / Build    | GitHub Actions               | "release-images, lần chạy / run {id}", a link                                       |
| Image   | `package`               | Image / Image       | `ghcr.io/dannyota/aboutme-*` | "3 image, đã được GitHub ký" / "3 images, signed by GitHub"                         |
| Running | `server`                | Đang chạy / Running | aboutme.vn                   | "AWS Singapore, {n} bản sao, từ 09:41" / "AWS Singapore, {n} replicas, since 09:41" |

The Image value is 14 px monospace with a break opportunity after `dannyota/`.
Per-component digests live in the component rows, not in the chain. The short
commit is 7 characters in monospace.

Chips are 24 px pills, 12 px weight 600, with a 14 px icon:

| Chip          | Vietnamese        | English       | Look                                                  |
| ------------- | ----------------- | ------------- | ----------------------------------------------------- |
| Match         | Khớp              | Matches       | `check`, `--link` on `--surface-blue`                 |
| Updating      | Đang cập nhật     | Updating      | `arrow-right-left` indigo, text on `--surface-indigo` |
| Not rechecked | Chưa kiểm tra lại | Not rechecked | `minus`, muted text, border                           |
| Not verified  | Chưa kiểm chứng   | Not verified  | `minus`, muted text, border                           |
| Failed        | {cause}           | {cause}       | `x`, `--destructive` on `--surface-destructive`       |

A failed chip names the cause in few words: "web không có chữ ký" / "No
signature for web", "Chữ ký sai" / "Invalid signature", or "caddy không chạy" /
"caddy isn't running". A failed step's disk takes a `--destructive` border and
icon.

Connectors join each disk to the next. A connector is a 2 px line with a 20 px
card-colored dot in the middle holding a 12 px icon:

| Connector | Line                      | Dot                             |
| --------- | ------------------------- | ------------------------------- |
| Agree     | Solid `--link`            | `check` in a `--link` ring      |
| Updating  | Solid `--brand-indigo`    | `arrow-right-left`, indigo ring |
| Unknown   | Dashed `--input`, 4 px on | None                            |
| Disagree  | Dashed `--destructive`    | `x` in a `--destructive` ring   |

From 42 rem the four steps are equal columns 40 px apart; a connector runs from
the previous disk's right edge to the next disk's left edge at the disks' center
line. Below 42 rem the steps are rows of a 40 px disk column and the text, 32 px
apart; the connector runs down the disk column between disks.

| Page state                 | Steps and connectors                                                                                         |
| -------------------------- | ------------------------------------------------------------------------------------------------------------ |
| Verified                   | All Match, all Agree                                                                                         |
| Rolling out                | Source, Build, Image Match for the newest version; Running Updating; Image to Running Updating               |
| Unverified                 | Unchecked step Not verified, the connector into it Unknown, earlier steps Match                              |
| Mismatch                   | The failing step Failed and the connector into it Disagree; later steps Not verified with Unknown connectors |
| Stale                      | Last known values, every chip Not rechecked, every connector Unknown                                         |
| Loading                    | Labels shown; values, lines, and chips as `skeleton` bars                                                    |
| Unavailable, Outdated page | The chain card is hidden                                                                                     |

A failed signature fails Image. A missing component fails Running. A summary
disagreement shows every step Not verified.

During a rollout or a mismatch `release` is `null`, so the chain shows the
newest verified version and commit among the running images. Frontend confirms
this rule with the architect before building it (see the last section).

## Component rows

The components card's heading is "Thành phần" / "Components". During a rollout
its hint is "Image mới nhất ở cuối" / "Newest image last". It is a `ul`; rows
are 16 px apart with a top border between them.

A row has the component name in 15 px weight 700 monospace and a 13 px muted
role, then one image line per running image, 10 px apart:

| Name          | Role VI / EN                     |
| ------------- | -------------------------------- |
| `server`      | Máy chủ API / API server         |
| `web`         | Ứng dụng web / Web app           |
| `caddy`       | Cổng HTTPS / HTTPS proxy         |
| `maintenance` | Trang bảo trì / Maintenance page |

From 42 rem the name and role stack in a 10 rem column and each image line is a
17 rem digest column plus a facts group. Below 42 rem the name and role share a
line, the digest chip takes its own line, and the facts wrap under it.

Facts, 13 px, 14 px apart:

- Version in weight 600, or an en dash with the accessible text "Chưa rõ phiên
  bản" / "Version unknown" when it is `null`.
- During a rollout, a tag: "Cũ hơn" / "Older" in `--muted` or "Mới nhất" /
  "Newest" in `--surface-indigo`, 11 px weight 700 pill.
- Replicas: one 8 px square per replica, 3 px apart, up to eight, then the text
  "{n} bản sao" / "{n} replica" or "{n} replicas". Squares are `--brand-blue`,
  `--input` for the older image, and hidden from screen readers.
- Signature and SBOM marks: a 14 px icon plus the word.

| Status      | Signature VI / EN                            | Icon and color                       |
| ----------- | -------------------------------------------- | ------------------------------------ |
| `verified`  | Chữ ký / Signature                           | `check`, `--link`, muted text        |
| `not_found` | Không có chữ ký / No signature               | `x`, `--destructive` weight 600 text |
| `invalid`   | Chữ ký sai / Invalid signature               | `x`, `--destructive` weight 600 text |
| `unchecked` | Chưa kiểm tra chữ ký / Signature not checked | `minus`, muted                       |

SBOM uses the same icons with "SBOM", "Không có SBOM" / "No SBOM", "SBOM sai" /
"Invalid SBOM", and "Chưa kiểm tra SBOM" / "SBOM not checked". In the stale
state every mark shows `minus` and muted text.

A component in rollout carries a 3 px `--brand-indigo` rail on the card's left
edge beside its row; a failing component carries a `--destructive` rail, and its
failing digest chip takes a `--destructive` border. The rail is decorative; the
tag and mark text carry the state.

## Digest chip and copy

A chip is 32 px high with a border, an 8 px radius, and `--muted` fill. It shows
`sha256:` and the first 8 and last 4 hex characters with `…` between, in 13 px
monospace, with a 32 px copy button attached on the right behind a border. It
never wraps; it is never wider than its line.

- The visible short text is hidden from screen readers; a visually hidden span
  holds the full digest. The `title` and the tooltip primitive show the full
  digest on hover and focus.
- The copy button is a real `button` named "Sao chép mã băm của {component}" /
  "Copy the {component} digest", adding " {version}" during a rollout.
- On activation it writes the full digest. The icon swaps from `copy` to `check`
  in `--link` and a 12 px weight 600 `--link` "Đã sao chép" / "Copied" shows at
  the end of the facts for 2 seconds. A page-level visually hidden
  `role="status"` region, separate from the status card, announces the same
  word.
- On failure the region announces "Không sao chép được. Hãy tự chọn và sao
  chép." / "Couldn't copy. Select it and copy it yourself." and the tooltip
  opens on the full digest.

## Verify it yourself

The card's heading is "Tự kiểm chứng" / "Verify it yourself", followed by 15 px
muted text: "Các lệnh này kiểm tra trực tiếp với GitHub và Sigstore, không cần
tin aboutme." / "These commands check against GitHub and Sigstore directly. They
don't trust aboutme."

The component selector, labeled "Thành phần" / "Component", is a single-choice
`toggle-group` in a `--muted` track with 13 px monospace segments; the selected
segment takes `--card` and a 1 px shadow. It is one tab stop: arrows move, Enter
or Space selects. A component with two images appears once per image as "web
v0.5.21" and "web v0.6.0". It starts on `server`, or on the failing component in
the mismatch state.

Two numbered steps follow, 20 px apart. Each has a 22 px `--surface-blue` number
disk with `--link` digits and a weight 600 label:

1. "Đọc các mã băm đang chạy" / "Read the running digests".
2. "Kiểm chứng cách image được dựng (GitHub CLI cần đăng nhập)" / "Verify how
   the image was built (the GitHub CLI must be signed in)".

A command block is a `pre` on `--muted` with a border and the 10 px radius, 13
px monospace at 1.65 line height, with 14 px padding and 52 px on the right for
the copy button. The `$` prompt and its space are muted and not copied. Live
values (component, digest, version) are weight 700 foreground on a
`--surface-blue` highlight. The block scrolls sideways inside the card, has
`tabindex="0"` and the name of its step so keyboard users can scroll it, and
never widens the page at 360 px.

The 32 px copy button sits 8 px from the block's top right, on `--card` with a
border, named "Sao chép lệnh {n}" / "Copy command {n}". It copies the command
without prompts and confirms like the digest chip. Under step 2, 13 px muted
text: "Để kiểm tra SBOM, chạy cùng lệnh với
`--predicate-type https://spdx.dev/Document/v2.3`." / "To check the SBOM, run
the same command with …".

Links follow a top border, 14 px weight 500 `--link`, 20 px apart and wrapping:
"Tài liệu JSON" / "JSON document" with a `file` icon before it, then "Mục nhật
ký minh bạch" / "Transparency log entry", "Lần chạy bản dựng" / "Build run", and
"Hướng dẫn GitHub CLI" / "GitHub CLI manual", each with an `external-link` icon
after it.

Without JavaScript, or in the Unavailable state, live values read `<digest>` and
`<version>` in muted italics, and the selector is hidden.

## Limits

A plain section, not a card, headed "Trang này chứng minh gì" / "What this
proves". It has two lists under 14 px weight 600 labels, each item 15 px on a 16
px icon column 10 px from the text.

"Chứng minh" / "It proves", with `check` in `--link`:

- AWS báo cáo đúng các mã băm này đang chạy, với số bản sao như trên. / AWS
  reports these exact digests as running, with these replica counts.
- GitHub đã dựng và ký từng image từ commit công khai được nêu, và chữ ký nằm
  trong nhật ký công khai của Sigstore. / GitHub built and signed each image
  from the named public commit, and the signature is in the public Sigstore log.

"Không chứng minh" / "It does not prove", with `minus` in muted, holds the three
sentences from [page.md](page.md#limits). Vietnamese:

- Trang cho thấy những gì AWS báo cáo đang chạy, không chứng minh bản thân máy
  chủ trung thực.
- Trang nói về mã nguồn, không nói về cấu hình hay bí mật.
- Bản dựng đã kiểm chứng nghĩa là GitHub dựng nó từ mã nguồn công khai, không có
  nghĩa mã nguồn không có lỗi.

A 14 px link follows: "Danh sách giới hạn đầy đủ trên GitHub" / "Full list of
limits on GitHub", to the [overview](README.md#what-this-proves) on GitHub. The
owner reviews all Vietnamese copy before release.

## Focus, keyboard, and screen readers

- Headings: the `h1`, a visually hidden `h2` "Trạng thái" / "Status" in the
  status card, then an `h2` per card and for the limits, and `h3` for the two
  limit lists.
- Tab order follows the reading order: header, source and build links in the
  chain, each digest copy button, the selector, each command block and its copy
  button, the links, and the limits link. The Outdated page button comes first
  when shown.
- Focus uses the primitives' ring: 2 px `--ring`, 2 px offset. The chain links
  and plain links use the same ring.
- The status card is `role="status"`. Its content changes only when the state,
  title, or detail changes, so a refetch with the same state announces nothing.
  Ages in the meta row update inside an `aria-hidden` span with a static
  accessible time beside it.
- Each chain `li` reads its label, value, and chip text. Connectors are hidden
  from screen readers; steps 2 to 4 add visually hidden text "Khớp với bước
  trước" / "Agrees with the previous step", "Đang cập nhật" / "Updating", "Không
  khớp với bước trước" / "Doesn't match the previous step", or nothing when
  unknown.
- Icons are decorative; every state has words.
- Under `forced-colors: active`, marks, rings, connector lines, rails, and chip
  borders use `CanvasText`, and tints drop to `Canvas`.

## Footer link

The landing footer reads `aboutme.vn`, Điều khoản / Terms, Quyền riêng tư /
Privacy, and then Kiểm chứng / Verify, in that order, with the same classes as
its siblings (`text-link`, underline on hover), 16 px apart, wrapping at phone
width. Its test id is `landing-verify-link`. The label sits with the other
footer labels in the web i18n copy.

## Mockups

Local renders of this spec, not committed: `.dev/design/verify/` in the main
checkout holds
`{verified,rolling,stale,mismatch}-{light,dark}-{phone,desktop}-{en,vi}.png` and
`footer-{light,dark}-phone-{en,vi}.png`. The designer reviews the built page
against them at 390 px and 1280 px in both themes and languages.

## Open points

- `release` is `null` outside the verified state, so [page.md](page.md) does not
  say which version the chain shows during a rollout or a mismatch. This spec
  shows the newest verified version; the architect confirms.
- Page.md lists the Image step's value as a short digest. With three components
  the chain shows the image family and a count instead, and each digest appears
  once, in its component row.
- The "It proves" list comes from the [overview](README.md#what-this-proves);
  page.md names only the three limits.
- DESIGN.md keeps blue for actions, links, and focus. The verified mark and the
  Match chips add a state meaning to blue, as page.md proposes. When the page
  ships, the guardrail in DESIGN.md adds "and the verify page's verified state".
- Page.md allows a success mark only in the Verified state. This spec reads that
  rule as the status card's mark: chain Match chips still show in Rolling out,
  Unverified, and Mismatch, because each names one passed check.
