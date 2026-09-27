# LinkedIn import page

This spec sets the look, states, copy, and accessibility of
`/app/import/linkedin` and its link in the Create resume dialog. The
[LinkedIn import](linkedin-import.md) design owns the flow, limits, parsing,
mapping, and privacy rules; this page only renders them. Visual rules, tokens,
and components come from [DESIGN.md](../../DESIGN.md) and
[ADR 0050](../adr/0050-aurora-application-identity.md). Copy keys live in a new
`apps/web/app/i18n/import.ts` catalog unless the [copy](#copy) table names an
existing key.

## States

The page is one route with these states. It never changes the URL between them.

| State       | Shown when                                  | Content                                              | Focus on entry                    |
| ----------- | ------------------------------------------- | ---------------------------------------------------- | --------------------------------- |
| Loading     | Session, resume count, or pdf.js is loading | `LoadingState` with `loading`                        | Unchanged                         |
| Unavailable | The resume list or pdf.js failed to load    | Heading, `StatusBanner` error, back link             | Unchanged                         |
| Old browser | The browser lacks what pdf.js needs         | Heading, `StatusBanner` info `oldBrowser`, back link | Unchanged                         |
| Cap         | The account has 3 resumes                   | Heading, `StatusBanner` info `cap`, go to resumes    | Unchanged                         |
| Pick        | Ready, no file chosen, or after a failure   | Instructions, drop zone, privacy note                | Unchanged on first render         |
| Reading     | A file was chosen or dropped                | Drop zone shows progress and Stop                    | Stop button                       |
| Failed      | A check failed or the time limit passed     | Pick, with an error `StatusBanner` above the zone    | Choose another PDF                |
| Review      | Reading succeeded                           | Review form and action panel                         | The review `h1` (`tabindex="-1"`) |
| Creating    | Create was pressed and the request is open  | Review, every control disabled, `aria-busy="true"`   | Unchanged (Create button)         |

Old browser, Cap, and Unavailable show no instructions and no file picker. A
locale change rerenders copy in place and keeps the state, the chosen file's
result, every selection, typed text, and focus, as
[localization](localization.md) requires. Messages are held as codes, not
strings.

## Layout

The page sits in `AppShell` on the Aurora canvas, like `/app/new`, and joins the
localized routes, so the shell shows the language toggle.

| Width        | Pick and message states                | Review                                                                                            |
| ------------ | -------------------------------------- | ------------------------------------------------------------------------------------------------- |
| Below 640 px | `max-w-3xl`, `px-4 py-8`, cards `p-4`  | One column, `px-4 py-8`; the action panel sticks to the bottom                                    |
| 640 to 899   | `max-w-3xl`, `px-6 py-10`, cards `p-6` | One column, `px-6 py-10`; sticky bottom action panel                                              |
| 900 px up    | `max-w-3xl` (48 rem), `px-6 py-10`     | `max-w-6xl` (72 rem); grid `minmax(0,1fr) 20rem`, 32 px gap; the panel is a sticky aside, `top-6` |

Every page starts with the same header: a `text-sm` back link to `/app/resumes`
with an `ArrowLeft` 16 px icon (`--link` text, underline on hover), 16 px above
`PageHeader`. The `h1` is `text-2xl font-bold`, and the lead is
`text-muted-foreground`, 8 px under it. Cards are
`rounded-lg border bg-card shadow-product` with 24 px between cards. At 390 px
nothing scrolls sideways; long values wrap with `break-words`.

## Entry link in the create dialog

`CreateResumeDialog` shows `importLinkedIn` as a second link beside `browseAll`,
in the footer's left group, and only while the footer shows Cancel and Create
(not in the uncertain-create state). Both links share one wrapper that keeps
today's `sm:mr-auto sm:self-center`; inside it the links stack with 4 px between
them and keep the `text-sm text-link underline underline-offset-4` style. Below
640 px the wrapper stacks last, as `browseAll` does now, with 8 px between the
links. The link is a plain `<a href="/app/import/linkedin">`, a full page load,
so pdf.js never enters the list page's bundle. It keeps
`data-action="create-import-linkedin"`. The dialog cannot open at the resume
cap, since Create resume is disabled there.

## Pick state

Top to bottom, below the header (`heading`, `lead`):

1. **Steps card.** `h2` `stepsHeading` (`text-base font-semibold`), then an `ol`
   of `step1` to `step4`, 12 px apart. Each item starts with a 24 px circle on
   `--surface-blue` with its number in `--link`, `text-sm font-semibold`,
   `aria-hidden="true"`; the list numbering stays in the `ol`. Under the list,
   16 px down, the `englishOnly` note: a `Languages` 16 px icon in
   `--brand-indigo` and `text-sm` foreground text. The LinkedIn words "More" and
   "Save to PDF" stay in English in both languages, because LinkedIn shows them
   in English.
2. **Drop zone.** A `section` labeled by its visually hidden `h2` `pickHeading`.
   It is a 2 px dashed `--input` border, `rounded-lg`, on `--surface-blue`,
   `p-8` (`p-6` below 640 px), content centered in a column with 12 px gaps: a
   `FileUp` 32 px icon in `--brand-blue`, `dropPrompt` (`font-medium`) and `or`
   (muted), the `default` `Button` `choose` (its visible text is its name), and
   `limits` (`text-xs` muted). Below 640 px the prompt and `or` are hidden and
   the button is full width. The border is decorative; the button carries the
   control contrast.
3. **Privacy note.** 16 px under the zone: a `LockKeyhole` 16 px icon in
   `--brand-blue`, then `privacy` and `ownProfile` as two `text-sm` muted
   sentences in one paragraph.

The file input is `<input type="file" accept="application/pdf,.pdf">`, visually
hidden, `tabindex="-1"`, and `aria-hidden="true"`; only the button opens it. It
is a custom-widget exception to the raw-control rule, like the crop stage.
Dropping is a pointer extra: the whole zone takes drops. While a file is dragged
over it, the border turns solid `--primary`, the fill `--surface-indigo`, and
the prompt reads `dropActive`, with a 150 ms color transition (none under
reduced motion). A drop of more than one file reads nothing and shows `dropOne`
as an error banner.

After a failure the page keeps the steps card and privacy note, shows the error
`StatusBanner` 16 px above the zone, and the button reads `chooseAnother`.

## Reading state

The drop zone keeps its size and border, stops taking drops, and shows, in a
column with 8 px gaps: the file name (`font-medium`, truncated with an ellipsis,
`title` holding the full name), a `role="status"` line reading `reading`, then a
6 px progress track (`--muted`, `rounded-full`) with a `--primary` fill, then
`readingPage` (`text-sm` muted, `aria-hidden="true"`), `readingNote` (`text-xs`
muted), and an `outline` `sm` `Button` `stop` with `aria-label` `stopLabel`. The
track is `role="progressbar"` with `aria-label` `progressLabel`,
`aria-valuemin="0"`, `aria-valuemax` the page count, and `aria-valuenow` the
pages read; it stays empty until the page count is known. The fill width moves
in 150 ms steps (instant under reduced motion). Nothing spins or pulses.

Stop ends the read, returns to Pick with focus on the choose button, and shows
`stopped` in an info `StatusBanner`. At the time limit the page shows
`timedOut`.

## Messages

Pick-state failures use `StatusBanner kind="error"` (`role="alert"`); `stopped`
and the full-page messages use `kind="info"` (`role="status"`). Each message
names the fix. Only one banner shows at a time; a new pick clears it.

| Key            | Shown when                                                     |
| -------------- | -------------------------------------------------------------- |
| `notPdf`       | No `%PDF-` header                                              |
| `notLinkedIn`  | A page lacks the LinkedIn footer                               |
| `notEnglish`   | Footers everywhere, but fewer than two English headings        |
| `unreadable`   | Unreadable text, a truncated or broken file, or a worker crash |
| `encrypted`    | pdf.js asks for a password                                     |
| `tooLarge`     | More than 4 MiB read, or the text or item cap is passed        |
| `tooManyPages` | More than 20 pages                                             |
| `timedOut`     | 15 seconds from the pick                                       |
| `dropOne`      | More than one file dropped                                     |
| `oldBrowser`   | Full page: the browser check fails                             |
| `readerFailed` | Full page: pdf.js or its worker did not load                   |
| `cap`          | Full page: 3 resumes (existing `resumeCreateCopy.cap`)         |

## Review state

The header's `h1` reads `reviewHeading`; the lead reads `reviewLead`. From 640
px the `ghost` `Button` `chooseAnother` sits in `PageHeader`'s actions slot;
below 640 px it sits under the lead, left-aligned. It returns to Pick and drops
the review. The main column holds, in order, with 24 px gaps:

1. **Notices**, only when there are any: `StatusBanner kind="info"` with the
   title `noticesHeading` and a `ul` of notice lines (`noticeDates`,
   `noticeStartOnly`, `noticeCut`, `noticeOverLimit`).
2. **Resume card.** `h2` `resumeHeading`; `TextField` `title` (required, default
   `defaultTitle`); then two `text-sm` muted lines, `languageLine` and
   `templateLine` with the gallery's first template name.
3. **Personal details card.** `h2` `personalDetails`; `TextField`s `fullName`
   and `headline`, prefilled and editable; then a `ul` of detail rows, one
   `CheckboxField` per found detail (email, phone, location, LinkedIn, each
   website). The label is the value; the description is the field name. Email
   and phone start unchecked and add `contactOffHint` to their description.
4. **One card per found section**, in resume order: Profile, Work experience,
   Education, Skills, Languages, Certifications. The card header is a row: the
   group `Checkbox`, the `h2` with the section's editor name, and, pushed right,
   `groupCount` (`text-sm` muted). The group box is checked when every entry is,
   unchecked when none is, and `indeterminate` otherwise; toggling it sets every
   entry. Entries follow in a `ul`, one `CheckboxField` per entry, rows `py-3`
   with `border-t` between them; the whole row is the label's hit area, at least
   44 px high.
5. **Check line.** `checkLine` as `text-sm` muted text.
6. **Not imported**, only when a section was dropped: a card with `h2`
   `notImportedHeading`, a `ul` of `notImportedLine` per section (the PDF's
   English heading and its entry count), and `notImportedNote`. No controls.

Entry rows show plain text only, never HTML:

| Section        | Label (`text-sm font-medium`) | Description (`text-sm` muted)                           |
| -------------- | ----------------------------- | ------------------------------------------------------- |
| Profile        | `summaryEntry`                | The summary text, `line-clamp-3`; list items start "• " |
| Work           | Job title · employer          | Dates · city, country                                   |
| Education      | School                        | Degree · dates                                          |
| Skills         | Skill name                    | None                                                    |
| Languages      | Language name                 | The editor's level name in the interface language       |
| Certifications | Title                         | None                                                    |

Dates show as the editor shows them, with `present` for an open end. An entry
with a notice adds a third line: an `Info` 14 px icon in `--brand-orange` and a
`text-xs` foreground mark, `entryNoDates` or `entryCut`. An entry that fails the
schema check instead gets a `CircleAlert` 14 px icon and `entryInvalid` in
`--destructive`. Entries past 64 in a section start unchecked.

### Action panel

The panel is a `bg-card` card. From 900 px it is the aside:
`rounded-lg border shadow-product p-6`, sticky at `top-6`. Below 900 px it is
the last child of the form, `position: sticky; bottom: 0`, full bleed (`-mx-4`,
`-mx-6` from 640 px), `border-t`, `shadow-product`, `px-4 pt-3`, bottom padding
`max(12px, env(safe-area-inset-bottom))`. It holds, in order:

1. `h2` `panelHeading` (from 900 px only; visually hidden below it).
2. `selectedCount` (`text-sm`; `text-xs` below 900 px).
3. The size meter: `sizeLabel` and `sizeValue` on one `text-sm` row (`text-xs`
   below 900 px), then a 6 px `rounded-full` track on `--muted` with a fill in
   `--primary`, or `--destructive` over the limit. The track is `role="meter"`,
   `aria-label` `sizeLabel`, `aria-valuemin="0"`, `aria-valuemax="262144"`,
   `aria-valuenow` the request bytes, and `aria-valuetext` `sizeValue`. KB
   values are request bytes divided by 1,024, rounded up; the maximum reads 256.
4. The block reason, when one applies, in `text-sm` `--destructive` with an id:
   `sizeOver`, or `invalid` when the schema check fails.
5. Create errors in `StatusBanner kind="error"`.
6. Actions. From 900 px: Create, then Cancel, both full width, 8 px apart. Below
   900 px: one row, Cancel (`outline`) then Create (`default`, `flex-1`), 8 px
   apart.

Create is the `default` `Button` `createAndOpen`, `type="submit"`,
`data-action="import-create"`. While the size or schema check blocks it, it
takes `aria-disabled="true"` (not `disabled`), stays focusable, and points
`aria-describedby` at the reason; pressing it then does nothing, or, for the
schema block, moves focus to the first marked entry's checkbox. With an empty
title, pressing it shows `titleRequired` on the title field and focuses it.
While creating, it reads `creating`, and it, Cancel, Choose another PDF, and
every field and checkbox take `disabled`. Cancel is an `outline`-styled
`NuxtLink` to `/app/resumes`; it asks nothing, and the review is dropped.

After the request:

| Outcome      | Page response                                                             |
| ------------ | ------------------------------------------------------------------------- |
| Created      | Opens the editor at `/app/resumes/{id}`                                   |
| Resume cap   | Error banner `cap`; Create stays blocked; link `returnToResumes`          |
| Failed       | Error banner `createFailed`; controls enabled again                       |
| Retry later  | Error banner `retryLater`; controls enabled again                         |
| Session lost | Error banner `sessionLost`                                                |
| Uncertain    | Error banner `uncertain` and an `outline` link `returnToResumes` under it |

## Theme

Every color is a semantic or brand token, so light and dark switch with the
theme. Points to check in both themes at 390 and 1280 px:

- The drop zone is `--surface-blue` (`#EAF2FF`, dark `#10224A`) with the
  `--input` dashed border; drag-over is `--surface-indigo` with a `--primary`
  border.
- Cards are `--card` (`#FFFFFF`, dark `#0D1935`); the step circles and the drop
  zone use `--surface-blue`; muted text stays AA on both.
- Meter and progress fills are `--primary` on `--muted`; over the limit the fill
  and reason are `--destructive` (`#B42318`, dark `#F0736A`).
- No seal red, gradient, or paper token appears: nothing on this page is public
  or a resume sheet.

## Accessibility

Tab order follows the DOM:

- Pick: shell, back link, Choose PDF.
- Reading: shell, back link, Stop.
- Review: shell, back link, Choose another PDF, title, full name, headline,
  detail checkboxes, then per section the group checkbox and its entry
  checkboxes, then Create and Cancel (Cancel and Create below 900 px).

Names and roles:

- The choose button's name is its visible text; the Stop button's is
  `stopLabel`, which starts with the visible word.
- Each group checkbox has `aria-label` `groupLabel` with the section name.
- Each entry checkbox's name is its label; `CheckboxField` gains an id on its
  description and points `aria-describedby` at it, so the description is read.
- The review form is labeled by the `h1`; each card is a `section` labeled by
  its `h2`.
- `groupCount` and `selectedCount` are plain text, not live regions.

Announcements:

- Reading: the `role="status"` line announces `reading` once; page progress is
  exposed through the progress bar, not announced.
- Failures announce through the error banner's `role="alert"` while focus moves
  to the choose button.
- Review: focus moves to the `h1`, which reads `reviewHeading`.
- Size: a polite live region in the panel announces `sizeOver` once when the
  size crosses the limit, and `sizeOk` once when it drops back.
- Create errors announce through `role="alert"`; `titleRequired` through the
  field's error.

## Copy

New keys. `{n}`, `{m}`, `{s}`, `{name}`, `{section}`, `{entry}`, `{field}`, and
`{max}` are values. Section and field names come from the editor catalogs.

| Key                  | en                                                                                                                                           | vi                                                                                                                                                     |
| -------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `importLinkedIn`     | Import from LinkedIn (PDF)                                                                                                                   | Nhập từ LinkedIn (PDF)                                                                                                                                 |
| `documentTitle`      | Import from LinkedIn                                                                                                                         | Nhập từ LinkedIn                                                                                                                                       |
| `back`               | Back to your resumes                                                                                                                         | Quay lại danh sách CV                                                                                                                                  |
| `heading`            | Import from LinkedIn                                                                                                                         | Nhập từ LinkedIn                                                                                                                                       |
| `lead`               | Create a new resume from your LinkedIn profile PDF. You check what we found before anything is saved.                                        | Tạo CV mới từ tệp PDF hồ sơ LinkedIn của bạn. Bạn sẽ xem lại nội dung đọc được trước khi lưu.                                                          |
| `stepsHeading`       | Get your profile PDF                                                                                                                         | Lấy tệp PDF hồ sơ                                                                                                                                      |
| `step1`              | On a computer, open your profile on linkedin.com. The LinkedIn phone app cannot save a PDF.                                                  | Mở hồ sơ của bạn trên linkedin.com bằng máy tính. Ứng dụng LinkedIn trên điện thoại không lưu được PDF.                                                |
| `step2`              | Set LinkedIn's language to English, and check that your profile is written in English.                                                       | Chuyển ngôn ngữ LinkedIn sang tiếng Anh và đảm bảo hồ sơ của bạn viết bằng tiếng Anh.                                                                  |
| `step3`              | On your profile, choose More, then Save to PDF.                                                                                              | Trên trang hồ sơ, chọn More, rồi Save to PDF.                                                                                                          |
| `step4`              | Choose the downloaded file below.                                                                                                            | Chọn tệp vừa tải về ở bên dưới.                                                                                                                        |
| `englishOnly`        | Only English profiles work, because LinkedIn saves only English text to PDF.                                                                 | Chỉ nhập được hồ sơ tiếng Anh, vì LinkedIn chỉ lưu văn bản tiếng Anh vào PDF.                                                                          |
| `pickHeading`        | Choose the PDF                                                                                                                               | Chọn tệp PDF                                                                                                                                           |
| `dropPrompt`         | Drop your LinkedIn PDF here                                                                                                                  | Thả tệp PDF LinkedIn vào đây                                                                                                                           |
| `dropActive`         | Release to read this file                                                                                                                    | Thả tệp để đọc                                                                                                                                         |
| `or`                 | or                                                                                                                                           | hoặc                                                                                                                                                   |
| `choose`             | Choose PDF                                                                                                                                   | Chọn tệp PDF                                                                                                                                           |
| `chooseAnother`      | Choose another PDF                                                                                                                           | Chọn tệp PDF khác                                                                                                                                      |
| `limits`             | One PDF, up to 4 MB and 20 pages.                                                                                                            | Một tệp PDF, tối đa 4 MB và 20 trang.                                                                                                                  |
| `privacy`            | The file stays on this device: your browser reads it, and aboutme never receives it. We store only the resume you create.                    | Tệp không rời khỏi thiết bị này: trình duyệt của bạn tự đọc tệp và aboutme không bao giờ nhận tệp. Chúng tôi chỉ lưu CV bạn tạo.                       |
| `ownProfile`         | Import your own profile, not someone else's.                                                                                                 | Hãy nhập hồ sơ của chính bạn, không phải của người khác.                                                                                               |
| `reading`            | Reading your PDF…                                                                                                                            | Đang đọc tệp PDF…                                                                                                                                      |
| `readingPage`        | Page {n} of {m}                                                                                                                              | Trang {n}/{m}                                                                                                                                          |
| `readingNote`        | This takes a few seconds. We stop after 15 seconds.                                                                                          | Việc này mất vài giây. Nếu quá 15 giây, chúng tôi sẽ dừng.                                                                                             |
| `progressLabel`      | Reading progress                                                                                                                             | Tiến độ đọc                                                                                                                                            |
| `stop`               | Stop                                                                                                                                         | Dừng                                                                                                                                                   |
| `stopLabel`          | Stop reading the PDF                                                                                                                         | Dừng đọc tệp PDF                                                                                                                                       |
| `stopped`            | Stopped. Choose a PDF to try again.                                                                                                          | Đã dừng. Hãy chọn tệp PDF để thử lại.                                                                                                                  |
| `notPdf`             | This file is not a PDF. Choose the PDF that LinkedIn saved.                                                                                  | Tệp này không phải PDF. Hãy chọn tệp PDF bạn đã lưu từ LinkedIn.                                                                                       |
| `notLinkedIn`        | This is not a LinkedIn profile PDF. Open your profile on LinkedIn, choose More, then Save to PDF.                                            | Đây không phải tệp PDF hồ sơ LinkedIn. Hãy mở hồ sơ trên LinkedIn, chọn More, rồi Save to PDF.                                                         |
| `notEnglish`         | This LinkedIn PDF is not in English. LinkedIn saves only English profiles to PDF. Set LinkedIn's language to English and save the PDF again. | Tệp PDF LinkedIn này không viết bằng tiếng Anh. LinkedIn chỉ lưu hồ sơ tiếng Anh vào PDF. Hãy chuyển ngôn ngữ LinkedIn sang tiếng Anh rồi lưu lại PDF. |
| `unreadable`         | We cannot read the text in this file. Save the PDF from LinkedIn again and choose the new file.                                              | Không đọc được chữ trong tệp này. Hãy lưu lại PDF từ LinkedIn rồi chọn tệp mới.                                                                        |
| `encrypted`          | This PDF is locked with a password. LinkedIn profile PDFs have none, so save yours from LinkedIn again.                                      | Tệp PDF này có mật khẩu. PDF hồ sơ LinkedIn không có mật khẩu, nên hãy lưu lại PDF từ LinkedIn.                                                        |
| `tooLarge`           | This file is too large to import. A LinkedIn profile PDF is much smaller; check that you chose the right file.                               | Tệp này quá lớn để nhập. PDF hồ sơ LinkedIn nhỏ hơn nhiều; hãy kiểm tra xem bạn đã chọn đúng tệp chưa.                                                 |
| `tooManyPages`       | This PDF has more than 20 pages. Choose the PDF that LinkedIn saved from your profile.                                                       | Tệp PDF này có hơn 20 trang. Hãy chọn tệp PDF bạn đã lưu từ hồ sơ LinkedIn.                                                                            |
| `timedOut`           | Reading this file took longer than 15 seconds, so we stopped. Try again, or save the PDF from LinkedIn again.                                | Đọc tệp này mất hơn 15 giây nên chúng tôi đã dừng. Hãy thử lại, hoặc lưu lại PDF từ LinkedIn.                                                          |
| `dropOne`            | Drop one file at a time.                                                                                                                     | Mỗi lần chỉ thả một tệp.                                                                                                                               |
| `oldBrowser`         | This browser cannot read PDFs on this page. Use a recent version of Chrome, Edge, Firefox, or Safari.                                        | Trình duyệt này không đọc được PDF trên trang này. Hãy dùng phiên bản mới của Chrome, Edge, Firefox hoặc Safari.                                       |
| `readerFailed`       | The PDF reader did not load. Check your connection and reload the page.                                                                      | Không tải được trình đọc PDF. Hãy kiểm tra kết nối rồi tải lại trang.                                                                                  |
| `reviewHeading`      | Check your import                                                                                                                            | Kiểm tra nội dung từ LinkedIn                                                                                                                          |
| `reviewLead`         | Choose what goes into your new resume. Nothing is saved until you create it.                                                                 | Chọn nội dung đưa vào CV mới. Chúng tôi chỉ lưu khi bạn tạo CV.                                                                                        |
| `noticesHeading`     | Check before you create                                                                                                                      | Kiểm tra trước khi tạo                                                                                                                                 |
| `noticeDates`        | {entry}: we could not read the dates. Add them in the editor.                                                                                | {entry}: không đọc được thời gian. Hãy thêm trong trình chỉnh sửa.                                                                                     |
| `noticeStartOnly`    | {entry}: only a start date was found. Add the end in the editor.                                                                             | {entry}: chỉ tìm thấy ngày bắt đầu. Hãy thêm ngày kết thúc trong trình chỉnh sửa.                                                                      |
| `noticeCut`          | {field} was shortened to {max} characters.                                                                                                   | {field} đã được rút gọn còn {max} ký tự.                                                                                                               |
| `noticeOverLimit`    | {section} has more than 64 entries. The first 64 are selected; the rest stay off.                                                            | {section} có hơn 64 mục. 64 mục đầu đã được chọn; các mục còn lại không được chọn.                                                                     |
| `resumeHeading`      | Resume                                                                                                                                       | CV                                                                                                                                                     |
| `defaultTitle`       | LinkedIn resume                                                                                                                              | CV từ LinkedIn                                                                                                                                         |
| `languageLine`       | Language: English, like your LinkedIn profile.                                                                                               | Ngôn ngữ: tiếng Anh, theo hồ sơ LinkedIn của bạn.                                                                                                      |
| `templateLine`       | Template: {name}. You can change it in the editor.                                                                                           | Mẫu: {name}. Bạn có thể đổi mẫu trong trình chỉnh sửa.                                                                                                 |
| `contactOffHint`     | Off at first, because a published resume shows it.                                                                                           | Mặc định không chọn, vì CV công khai sẽ hiển thị thông tin này.                                                                                        |
| `groupLabel`         | Import all of {section}                                                                                                                      | Nhập toàn bộ {section}                                                                                                                                 |
| `groupCount`         | {n} of {m} selected                                                                                                                          | Đã chọn {n}/{m}                                                                                                                                        |
| `summaryEntry`       | Profile summary                                                                                                                              | Đoạn tóm tắt hồ sơ                                                                                                                                     |
| `entryNoDates`       | No dates                                                                                                                                     | Chưa có thời gian                                                                                                                                      |
| `entryCut`           | Shortened                                                                                                                                    | Đã rút gọn                                                                                                                                             |
| `entryInvalid`       | Cannot be saved as is. Deselect it or fix it in the editor later.                                                                            | Mục này không lưu được như hiện tại. Hãy bỏ chọn, hoặc sửa sau trong trình chỉnh sửa.                                                                  |
| `checkLine`          | Check each entry. The PDF layout can split or join lines; you can fix them in the editor.                                                    | Hãy kiểm tra từng mục. Bố cục PDF có thể tách hoặc nối nhầm dòng; bạn có thể sửa trong trình chỉnh sửa.                                                |
| `notImportedHeading` | Not imported                                                                                                                                 | Các phần không nhập                                                                                                                                    |
| `notImportedLine`    | {section} ({n})                                                                                                                              | {section} ({n})                                                                                                                                        |
| `notImportedNote`    | aboutme does not import these sections. Add them in the editor if you need them.                                                             | aboutme không nhập các phần này. Bạn có thể tự thêm trong trình chỉnh sửa nếu cần.                                                                     |
| `panelHeading`       | Your new resume                                                                                                                              | CV mới của bạn                                                                                                                                         |
| `selectedCount`      | {n} items from {s} sections                                                                                                                  | {n} mục từ {s} phần                                                                                                                                    |
| `sizeLabel`          | Resume size                                                                                                                                  | Dung lượng CV                                                                                                                                          |
| `sizeValue`          | {n} KB of {max} KB                                                                                                                           | {n} KB / {max} KB                                                                                                                                      |
| `sizeOver`           | Too large to create. Deselect some entries to go under {max} KB.                                                                             | CV quá lớn để tạo. Hãy bỏ chọn bớt mục để dưới {max} KB.                                                                                               |
| `sizeOk`             | The resume fits again.                                                                                                                       | CV đã vừa dung lượng cho phép.                                                                                                                         |
| `invalid`            | Some entries cannot be saved as they are. Deselect the marked entries.                                                                       | Một số mục không lưu được như hiện tại. Hãy bỏ chọn các mục được đánh dấu.                                                                             |

English plurals: `readingPage`, `groupCount`, and `notImportedLine` need none;
`selectedCount` uses "1 item" and "1 section" in the singular. Vietnamese has no
plural forms.

Reused keys, unchanged: `resumeCreateCopy` `loading`, `cap`, `returnToResumes`,
`title`, `titleRequired`, `createFailed`, `retryLater`, `sessionLost`,
`uncertain`, `creating`, `createAndOpen`, and `cancel`; `resumeListCopy`
`unavailable`; the editor's personal-detail title and field names
(`personalDetails`, `fullName`, `headline`, `email`, `phone`, `location`,
`website`, `linkedin`), section type names, language level names, and `present`.
The tab title is `pageTitle(documentTitle)` from `i18n/meta.ts`.
