# Resume history panel

Status: Accepted by the owner (2026-11-21), for v0.10.0. The
[change review design](../change-review.md#checkpoints) owns checkpoints,
restore, deletion, retention, privacy, and owner decisions D1 and D2; the
[history contract](../resume-history-contract.md) owns the history API. This
document owns the editor's History panel, the version preview, their dialogs,
and their copy.

## Entry point

The editor rail gains History (“Lịch sử”), a 44 px rail tool with the lucide
`History` icon and `data-action="open-history"`, after the rail's spacer: at the
bottom of the vertical rail and the end of the horizontal one. Like Design or
Photo, it opens its panel in the inspector and is pressed while open. Below 360
px the horizontal rail's gap drops from 8 to 4 px, so six tools fit at 320 px.

While History is open, errors, conflicts, the held-write banner, and the session
banner stay above the panel. The top bar is unchanged: save state keeps
reporting saves in flight, and Download PDF and Publish act on the current
resume. From 72 rem the outline stays visible; choosing an outline row, another
rail tool, or Back to editing leaves History and shows the current resume. The
panel has no fields, so no edit starts while it is open. It reads the version
list when it opens, after each of its own actions, and when the accepted
revision changes while it is open, one read at a time: taking a checkpoint
notifies nobody.

## Version list

The panel uses `InspectorPanel`: the h2 “Lịch sử”, the count “12 / 50” in muted
14 px at the end of the heading row, and the lead line. The list is a listbox,
newest session first. Its first option is Current version (`FileText` icon),
selected when History opens and followed by a hairline. Each version shows:

- line 1: the absolute session start time, weight 600, then “ · ” and the
  relative time in muted text while the version is under 7 days old;
- line 2, muted, one line, truncated: the cause and actor;
- a 16 px muted icon beside line 1, by cause.

| Cause       | Icon         | Line 2 key and rule                                                                                      |
| ----------- | ------------ | -------------------------------------------------------------------------------------------------------- |
| `edit`      | `Pencil`     | `causeEdit`; a library rewrite the owner caused reads the same                                           |
| `assistant` | `Bot`        | `causeAssistant` with the copied display name as text, or `causeAssistantUnnamed` when the name is empty |
| `restore`   | `RotateCcw`  | `causeRestore`                                                                                           |
| `accept`    | `CheckCheck` | `causeAccept`; appears from v0.10.1                                                                      |

| Time part              | Vietnamese                                             | English                                          |
| ---------------------- | ------------------------------------------------------ | ------------------------------------------------ |
| Absolute, current year | `HH:mm, d thg M`: 14:05, 21 thg 11                     | `MMM d, h:mm a`: Nov 21, 2:05 PM                 |
| Absolute, other year   | 09:30, 28 thg 12, 2026                                 | Dec 28, 2026, 9:30 AM                            |
| Relative, under 7 days | resume list words: vừa xong, 5 phút trước, 2 giờ trước | just now, 5 minutes ago, 2 hours ago, 3 days ago |

Absolute times use the browser's time zone, since the hour matters, in
`<time datetime>` with the UTC instant. Relative text refreshes each minute
without an announcement. `{time}` in all copy is the absolute form.

Under the list, 24 px down, the retention line in 12 px muted text, then after a
`--border` top rule and 16 px, Clear history.

| State        | What the panel shows                                                                                                         |
| ------------ | ---------------------------------------------------------------------------------------------------------------------------- |
| Loading      | The heading and lead, then three 64 px skeleton rows; no count                                                               |
| Empty        | A bordered 10 px panel with the empty title as an h3 (`tabindex="-1"`) and body; no Current version, count, or Clear history |
| Load failed  | An error `StatusBanner` with `loadFailed` and Try again; no rows                                                             |
| Session lost | The editor's sign-in dialog; the list reads again after sign-in                                                              |
| 50 versions  | “50 / 50”; nothing else changes, since the next checkpoint removes the oldest                                                |

## Version preview

Choosing a version (click, Enter, or Space) reads it and shows it in the preview
region; choosing Current version shows the live resume again. The version
renders through the editor preview's path, `ResumeDocument` in `ScaledSheet`:
paged mode, `nameHeading: 'p'`, the version's resume language, no project link
map, with the zoom pill and page count. PDF open is hidden, since a PDF is the
current resume. The controller passes the version's document with the current
photo and crop, because restore keeps them; with no current photo, none. The
version never enters the store record, the save queue, or field drafts.

A version bar above the preview's scroll area, not scrolling, holds:

- the title `versionTitle`, an h2 at 14 px weight 600, truncated,
  `tabindex="-1"`, with `viewOnly` under it in 12 px muted text from 42 rem;
- Delete this version: a 36 px destructive outline icon pill, `Trash2`, with a
  tooltip and `deleteVersionLabel` as its name;
- Back to editing, an outline pill, and Restore, a main pill. Restore is
  `aria-disabled`, with `restoreBlocked` under the buttons, while save state is
  not Saved, a conflict or held write shows, or the session check fails, so its
  `If-Match` names a revision the owner has seen.

While the version loads, the sheet area shows `LoadingState` with
`previewLoading` and the bar's buttons are disabled. A failed read shows
`previewFailed` with Try again; a 404 shows `versionGone`, reloads the list, and
selects Current version. Back to editing leaves History: the inspector returns
to the tool open before it, and narrow layouts switch to Edit.

## Restore

Restore opens an alert dialog under the
[dialog rule](typography-and-tokens.md#controls), at most 32 rem wide:

1. `restoreTitle`.
2. The public effect, weight 600: `restorePublic` with the display link as text,
   or `restorePrivate`. The dialog reads the accepted record as it renders, so a
   publish change elsewhere swaps the sentence.
3. `restoreWhat`, then `restoreNewTitle` and `restoreNewLanguage` only when they
   differ from the current resume, naming the language as the resume language
   field does.
4. `restoreKept`, then `restoreUndoNote`.

Actions are Cancel (outline) and Restore (main pill); focus starts on Cancel.
Each confirmation sends `If-Match` with the accepted revision and a new
idempotency key; a retry after a network failure reuses both. Alerts are error
`StatusBanner`s 16 px above the actions.

| Outcome                      | Dialog and editor                                                                                                         |
| ---------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| In flight                    | Restore reads `restoring`; both buttons disabled; Escape and outside clicks ignored; `aria-busy`                          |
| Saved                        | Closes; the editor takes the result as below                                                                              |
| `412` revision mismatch      | The editor reconciles with the response's resume, as for any 412; `restoreChanged`; Restore uses the new revision and key |
| Validation failure (`422`)   | `restoreInvalid`; Restore hidden; Cancel reads Close                                                                      |
| Version missing (`404`)      | `versionGone`; Restore hidden; Cancel reads Close; the list reloads on close                                              |
| Network failure or no answer | `restoreNetwork`; Restore reads Try again and resends the same request                                                    |
| Rate limited or unavailable  | `restoreBusy`; Restore disabled until any `Retry-After` passes                                                            |
| Session lost                 | Closes; the editor's sign-in dialog opens; nothing is resent on its own                                                   |
| Any other code               | `restoreFailed`                                                                                                           |

After a restore, the editor takes the returned resume as accepted and current:
title, resume language, and preview update, and a template Undo offer from
before is dropped. History stays open on Current version and the list reloads
with the new `restore` row on top. A success `StatusBanner` at the top of the
panel takes focus with `restoredPublic` or `restoredPrivate` and Undo, which
opens this dialog for that new row; it stays until the next History action or
until History closes. Narrow layouts switch to Edit so the banner shows.

## Delete and clear

Delete this version opens a dialog with `deleteTitle`, `deleteBody`, Cancel, and
Delete version (destructive outline), focus on Cancel; while deleting, the
button reads `deleting` and both are disabled. On success or a 404, the row
leaves, the preview shows Current version, focus moves to the next older option,
else the next newer, else Current version or the empty heading, and a polite
status reads `deleted`. A failure keeps the dialog with `deleteFailed`.

Clear history, a 36 px destructive outline pill, opens `clearTitle`,
`clearBody`, Cancel, and Clear history, focus on Cancel. On success the panel
shows the empty state with focus on its heading, the preview shows the current
resume, and a polite status reads `cleared`; a failure keeps the dialog with
`clearFailed`. Neither action changes the resume, its revision, or save state.

## Layout

| Width                 | Layout                                                                                                                                                  |
| --------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| From 72 rem (1440 px) | Panel in the inspector column, 22 rem, draggable to 48 rem; version bar across the preview column                                                       |
| 42 to 72 rem          | Horizontal rail; panel in the editor region. Choosing a version switches the bottom tabs to Preview and focuses the bar title; Edit returns to the list |
| Below 42 rem (390 px) | As above; the bar has a title row and a button row: 40 px Delete icon pill, then Back to editing and Restore sharing the rest; 40 px buttons            |

- Options: at least 64 px high, 10 by 12 px padding, 10 px radius, 4 px apart,
  10 px from icon to text, 14/1.5 text; hover `--accent`; selected `--accent`
  with line 1 at weight 700, as outline rows; focus a 2 px `--ring` at 2 px
  offset. The Current version hairline has 8 px space on each side.
- Version bar: `--background`, a 1 px `--border` bottom, 12 by 24 px padding (16
  px below 42 rem), 12 px gaps. The title block grows from 16 rem; the buttons
  wrap under it when the preview is narrower. Dialog paragraphs: 14/1.5, 12 px.
- Nothing overflows from 320 px. Titles, assistant names, and links truncate
  under the 0.3 em padding rule of the
  [type scale](typography-and-tokens.md#type).

## Keyboard, screen readers, and motion

- The listbox, named by `listLabel`, is one tab stop. Up and Down move focus,
  Home and End jump, Enter or Space selects. Selection does not follow focus, so
  arrowing loads nothing. An option's name is line 1 then line 2; the count's
  name is `countName`.
- On selection from 72 rem, focus stays in the list and a polite status reads
  `showing`; narrower, focus moves to the bar title. The preview section's label
  becomes `previewLabel`. The `aria-disabled` Restore stays focusable.
- Escape in the list or the bar returns to Current version and focuses the
  option that was shown; narrower, it also switches to Edit. Back to editing
  focuses the rail button of the tool it returns to. Dialogs trap focus and
  return it to their trigger on Cancel; their errors are `role="alert"`.
- A locale toggle rerenders copy and times in place, keeps the selection,
  preview, open dialog, request in flight, and errors, and sends nothing. The
  preview keeps the version's own resume language.
- No motion is added. Option fills use the 150 ms color transition and skeletons
  pulse; reduced motion makes both instant and static. The selected option
  scrolls into view without smooth scrolling.

## Copy

New keys go in an `editorHistory` catalog; `editorShell.history` names the rail
tool and the panel. Try again, Close, and the relative words reuse
`editorShell.tryAgain`, `editorShell.close`, and `resumeList.relativeTime`.
`{name}`, `{title}`, and `{link}` render as text, never HTML.

| Key                                   | Vietnamese                                                                                                                                           | English                                                                                                                                                                  |
| ------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `editorShell.history`                 | Lịch sử                                                                                                                                              | History                                                                                                                                                                  |
| `lead`                                | Mỗi phiên bản là CV ngay trước một lượt chỉnh sửa.                                                                                                   | Each version is the resume just before a round of edits.                                                                                                                 |
| `count(n)`, `countName(n)`            | {n} / 50 · {n} trên tối đa 50 phiên bản                                                                                                              | {n} / 50 · {n} of at most 50 versions                                                                                                                                    |
| `listLabel`                           | Các phiên bản của CV                                                                                                                                 | Resume versions                                                                                                                                                          |
| `current`, `currentDetail`            | Phiên bản hiện tại · CV như trong trình chỉnh sửa                                                                                                    | Current version · The resume as it is in the editor                                                                                                                      |
| `causeEdit`                           | Trước khi bạn chỉnh sửa                                                                                                                              | Before your edits                                                                                                                                                        |
| `causeAssistant(name)`                | Trước khi trợ lý {name} chỉnh sửa                                                                                                                    | Before edits by the assistant {name}                                                                                                                                     |
| `causeAssistantUnnamed`               | Trước khi một trợ lý AI chỉnh sửa                                                                                                                    | Before edits by an AI assistant                                                                                                                                          |
| `causeRestore`                        | Trước khi bạn khôi phục một phiên bản                                                                                                                | Before you restored a version                                                                                                                                            |
| `causeAccept`                         | Trước khi bạn chấp nhận thay đổi được đề xuất                                                                                                        | Before you accepted proposed changes                                                                                                                                     |
| `retention`                           | Lịch sử giữ tối đa 50 phiên bản trong 30 ngày. Khi đầy, phiên bản cũ nhất tự động bị xóa.                                                            | History keeps up to 50 versions for 30 days. When it is full, the oldest is removed automatically.                                                                       |
| `emptyTitle`, `emptyBody`             | Chưa có phiên bản nào · Khi bạn hoặc trợ lý AI bắt đầu chỉnh sửa, CV trước lượt đó được lưu ở đây trong 30 ngày.                                     | No versions yet · When you or an AI assistant start editing, the resume as it was before is kept here for 30 days.                                                       |
| `loading`, `loadFailed`               | Đang tải lịch sử · Không tải được lịch sử.                                                                                                           | Loading history · Couldn't load history.                                                                                                                                 |
| `versionTitle(time)`                  | Phiên bản lúc {time}                                                                                                                                 | Version from {time}                                                                                                                                                      |
| `viewOnly`                            | Chỉ xem. Khôi phục sẽ giữ ảnh hiện tại.                                                                                                              | View only. Restoring keeps your current photo.                                                                                                                           |
| `previewLabel(time)`, `showing(time)` | Bản xem trước phiên bản lúc {time} · Đang xem phiên bản lúc {time}                                                                                   | Preview of the version from {time} · Showing the version from {time}                                                                                                     |
| `previewLoading`, `previewFailed`     | Đang tải phiên bản · Không tải được phiên bản này.                                                                                                   | Loading version · Couldn't load this version.                                                                                                                            |
| `versionGone`                         | Phiên bản này không còn trong lịch sử. Có thể nó đã quá 30 ngày hoặc đã bị xóa.                                                                      | This version is no longer in history. It may be over 30 days old or deleted.                                                                                             |
| `backToEditing`                       | Quay lại chỉnh sửa                                                                                                                                   | Back to editing                                                                                                                                                          |
| `restore`                             | Khôi phục                                                                                                                                            | Restore                                                                                                                                                                  |
| `restoreBlocked`                      | Bạn có thể khôi phục khi mọi thay đổi đã được lưu.                                                                                                   | You can restore once every change is saved.                                                                                                                              |
| `deleteVersionLabel`                  | Xóa phiên bản này                                                                                                                                    | Delete this version                                                                                                                                                      |
| `restoreTitle(time)`                  | Khôi phục phiên bản lúc {time}?                                                                                                                      | Restore the version from {time}?                                                                                                                                         |
| `restorePublic(link)`                 | CV này đang công khai. Người mở {link} sẽ thấy phiên bản này ngay khi bạn khôi phục.                                                                 | This resume is public. Anyone who opens {link} sees this version as soon as you restore it.                                                                              |
| `restorePrivate`                      | CV này chưa công khai, nên chỉ bạn thấy thay đổi.                                                                                                    | This resume isn't public, so only you see the change.                                                                                                                    |
| `restoreWhat(time)`                   | Nội dung, thông tin cá nhân, bố cục, thiết kế, tiêu đề và ngôn ngữ CV trở về như lúc {time}.                                                         | The text, personal details, layout, design, title and resume language go back to how they were at {time}.                                                                |
| `restoreNewTitle(title)`              | Tiêu đề CV sẽ là “{title}”.                                                                                                                          | The resume title becomes “{title}”.                                                                                                                                      |
| `restoreNewLanguage(language)`        | Ngôn ngữ CV sẽ là {language}.                                                                                                                        | The resume language becomes {language}.                                                                                                                                  |
| `restoreKept`                         | Giữ nguyên: ảnh và vùng cắt ảnh, địa chỉ CV, cài đặt xuất bản và chia sẻ, tiêu đề và biểu tượng của trang công khai, cùng các dự án trong mục Dự án. | Kept as they are: the photo and its crop, the resume address, publish and sharing settings, the public page title and icon, and the projects in Projects.                |
| `restoreUndoNote`                     | Phiên bản hiện tại được lưu vào lịch sử trước, nên bạn có thể hoàn tác.                                                                              | The current version is saved to history first, so you can undo this.                                                                                                     |
| `cancel`                              | Hủy                                                                                                                                                  | Cancel                                                                                                                                                                   |
| `restoring`                           | Đang khôi phục…                                                                                                                                      | Restoring…                                                                                                                                                               |
| `restoreChanged`                      | CV vừa thay đổi ở nơi khác, và trình chỉnh sửa đã tải bản mới nhất. Chọn Khôi phục để thử lại; bản mới nhất sẽ được lưu vào lịch sử trước.           | The resume just changed elsewhere, and the editor now has the latest version. Choose Restore to try again; the latest version is saved to history first.                 |
| `restoreInvalid`                      | Không thể khôi phục phiên bản này vì nó không đạt các kiểm tra hiện tại của CV. CV không thay đổi.                                                   | This version can't be restored because it doesn't pass the current resume checks. Nothing changed.                                                                       |
| `restoreNetwork`                      | Chưa xác nhận được việc khôi phục vì lỗi kết nối. Hãy thử lại; CV sẽ không bị khôi phục hai lần.                                                     | We couldn't confirm the restore because of a connection problem. Try again; it won't be applied twice.                                                                   |
| `restoreBusy`                         | Hệ thống đang bận. Hãy thử lại sau ít phút.                                                                                                          | The service is busy. Try again in a few minutes.                                                                                                                         |
| `restoreFailed`                       | Chưa khôi phục được. CV không thay đổi.                                                                                                              | Couldn't restore. Nothing changed.                                                                                                                                       |
| `restoredPrivate(time)`               | Đã khôi phục phiên bản lúc {time}. Phiên bản trước đó đã được lưu vào lịch sử.                                                                       | Restored the version from {time}. The version before it is saved in history.                                                                                             |
| `restoredPublic(time, link)`          | Đã khôi phục phiên bản lúc {time}, và {link} đang hiển thị phiên bản này. Phiên bản trước đó đã được lưu vào lịch sử.                                | Restored the version from {time}, and {link} now shows it. The version before it is saved in history.                                                                    |
| `undo`                                | Hoàn tác                                                                                                                                             | Undo                                                                                                                                                                     |
| `deleteTitle(time)`                   | Xóa phiên bản lúc {time}?                                                                                                                            | Delete the version from {time}?                                                                                                                                          |
| `deleteBody`                          | Phiên bản này sẽ bị xóa khỏi lịch sử và không thể khôi phục. CV hiện tại không thay đổi.                                                             | This version is removed from history and can't be restored. Your resume as it is now doesn't change.                                                                     |
| `deleteConfirm`                       | Xóa phiên bản                                                                                                                                        | Delete version                                                                                                                                                           |
| `deleting`, `clearing`                | Đang xóa… · Đang xóa…                                                                                                                                | Deleting… · Clearing…                                                                                                                                                    |
| `deleteFailed`                        | Chưa xóa được. Hãy thử lại.                                                                                                                          | Couldn't delete. Try again.                                                                                                                                              |
| `deleted(time)`                       | Đã xóa phiên bản lúc {time}.                                                                                                                         | Deleted the version from {time}.                                                                                                                                         |
| `clear`, `clearConfirm`               | Xóa toàn bộ lịch sử · Xóa lịch sử                                                                                                                    | Clear history · Clear history                                                                                                                                            |
| `clearTitle`                          | Xóa toàn bộ lịch sử của CV này?                                                                                                                      | Clear this resume's history?                                                                                                                                             |
| `clearBody(n)`                        | Thao tác này xóa vĩnh viễn {n} phiên bản khỏi lịch sử. Nội dung bạn đã xóa khỏi CV sẽ không còn được giữ ở đây. CV hiện tại không thay đổi.          | This deletes {n} versions from history for good (“1 version” for one). Text you removed from the resume is no longer kept here. Your resume as it is now doesn't change. |
| `clearFailed`                         | Chưa xóa được lịch sử. Hãy thử lại.                                                                                                                  | Couldn't clear history. Try again.                                                                                                                                       |
| `cleared`                             | Đã xóa toàn bộ lịch sử.                                                                                                                              | History cleared.                                                                                                                                                         |

## Mockups

Desktop, 1440 px, previewing a version (`H*` pressed tool, `#` selected option,
`[x]` delete icon pill):

```text
+-----------------------------------------------------------------------------------+
| (a) aboutme.vn | CV Backend  v Đã lưu  [a] aboutme.vn/an  VI|EN (PDF) (Xuất bản)  |
+---+-----------+-----------------------------------------+-------------------------+
| D | CV     v +| Phiên bản lúc 14:05, 21 thg 11          | Lịch sử         12 / 50 |
| S | Thông tin | Chỉ xem. Khôi phục sẽ giữ ảnh hiện tại. | Mỗi phiên bản là CV...  |
| T | Kinh ngh. |   [x] (Quay lại chỉnh sửa) (Khôi phục)  | [F] Phiên bản hiện tại  |
| M | Học vấn   +-----------------------------------------+ ----------------------- |
| P | Dự án     |       +-----------------------+         |#[e] 14:05, 21 thg 11 ·  |
|   |           |       | Nguyễn Văn An         |         |#    Trước khi bạn chỉnh |
|   |           |       | Backend Engineer      |         | [b] 09:12, 21 thg 11 ·  |
|H* |           |       +-----------------------+         |     Trước khi trợ lý .. |
|   |           |       / 2 trang     (- 84% + Mặc định)  | (Xóa toàn bộ lịch sử)   |
+---+-----------+-----------------------------------------+-------------------------+
```

Phone, 390 px, Edit tab, then Preview tab after choosing a version:

```text
+----------------------------+  +----------------------------+
| (a) v Đã lưu         VI|EN |  | (a) v Đã lưu         VI|EN |
|   (Tải PDF) (Xuất bản)  O  |  |   (Tải PDF) (Xuất bản)  O  |
| [D][S][T][M][P]       [H*] |  | Phiên bản lúc 14:05, 21... |
| Lịch sử            12 / 50 |  | [x] (Quay lại…) (Khôi phục)|
| [F] Phiên bản hiện tại     |  |  +----------------------+  |
|#[e] 14:05, 21 thg 11 ·     |  |  | Nguyễn Văn An        |  |
|#    Trước khi bạn chỉnh sửa|  |  +----------------------+  |
| [b] 09:12, 21 thg 11 ·     |  |  / 2 trang                 |
+----------------------------+  +----------------------------+
| ((Chỉnh sửa))  (Xem trước) |  | (Chỉnh sửa)  ((Xem trước)) |
+----------------------------+  +----------------------------+
```

## Decisions

1. The preview shows the current photo, as restore produces; the checkpoint
   holds none.
2. Absolute times use the browser's time zone. This panel is owner-only, so the
   UTC rule for identity and agent dates in [localization](../localization.md)
   does not apply.
3. The [history contract](../resume-history-contract.md) supplies the list and
   read fields. An invalid restore answers `422` (`ResumeDocumentInvalid`), a
   missing version `404` (`ResumeNotFound`). A display name can be empty, so
   `causeAssistantUnnamed` stays.
4. Restore shows the generic `restoreKept` line only; it names no dropped
   project link.
5. Restore waits until every change is saved, so it never sends a revision the
   owner has not seen.
