# Projects workspace

Status: Accepted (2026-11-11). The list and editor are implemented; showing a
project on resumes is accepted (2026-11-12) and not yet implemented. The
[projects design](../projects.md) owns the product and its limits, and the
[projects contract](../projects-contract.md#owner-api) owns the API this screen
calls. This document owns the two workspace screens and the controls that show a
project on a resume.

## Navigation

The signed-in shell adds a Projects link (“Dự án”) after Resumes, to
`/app/projects`. It hides below 768 px, and the account menu gains a Projects
item above Views, so the page is always one tap away. No signed-out page links
to it; opening it signed out goes to sign in and returns.

## Project list

`/app/projects` uses the shell's 76 rem column. The page header holds the h1 “Dự
án” or “Projects”, a one-line lead (“Viết mỗi dự án một lần, rồi chọn CV nào
hiển thị nó.” or “Write each project once, then choose which resumes show it.”),
the count as “3 / 30”, and New project, a primary button with a plus icon. At 30
projects the button is disabled and a line under the header says the limit is
reached.

Projects are rows in one bordered card, newest update first, not paper cards:
the list holds up to 30. A row is a link to the project's editor and shows:

- the name in the project's default language, semibold, truncated to one line;
- the role, muted, when present;
- up to three technology chips and a “+n” chip for the rest;
- a chip for each language with text, the default one marked;
- the relative updated time;
- an overflow menu with Change address and Delete.

Below 640 px a row stacks to name, role, chips and time, and the menu stays at
the top right. The empty list shows one bordered panel: “Chưa có dự án nào.” or
“No projects yet.”, the lead line, and the New project button. Loading shows
three skeleton rows; a failed load shows an alert with Retry.

New project opens a dialog with one field, Name, and Create. It creates the
project in the site language with that name and opens its editor. The server
proposes the address from the name.

## Project editor

`/app/projects/{id}` is a form page in a 48 rem column, with a breadcrumb back
to Projects and the project name as the h1. It is not the resume editor: there
is no preview, rail or inspector.

Sections, in order, each under a top rule as in Settings:

1. **Text.** A tab per language with text, the default one marked, and Add
   language while the project has fewer than four. Each tab has Name, Role,
   Summary, Outcome and Description. Description uses the resume rich text
   editor; the others are plain fields with a character counter that appears
   within 20 characters of the limit. A tab's menu offers Make default and
   Remove language; the default language cannot be removed.
2. **Details.** Dates use the resume entry date range control. Repository link
   and Demo link take `https` addresses. Technologies is a chip input: Enter or
   a comma adds one, Backspace on an empty field removes the last, and each chip
   has a remove button; it stops at 20.
3. **Address.** The slug, read only, with Change address.
4. **Delete.** A destructive Delete project button.

Saving is explicit. A bar fixed to the bottom of the column holds the save state
and Save, enabled only when something changed. Save sends the whole document
with the revision it loaded. Leaving with unsaved changes asks first, with the
guard the resume editor uses. Field errors appear under their fields and in an
error summary above the form that links to each one.

| Outcome                             | What the page does                                                      |
| ----------------------------------- | ----------------------------------------------------------------------- |
| Saved                               | Shows “Đã lưu” or “Saved” and takes the new revision                    |
| Revision mismatch                   | Keeps the typed text, says the project changed elsewhere, offers Reload |
| Validation error                    | Marks the fields and focuses the error summary                          |
| `project_not_found`                 | Shows a not-found panel with a link back to Projects                    |
| Rate limited or unavailable         | Keeps the typed text and says to try again                              |
| `project_limit` on create           | Says the limit of 30 is reached                                         |
| `project_slug_taken` or `…_held`    | Says the address is in use, or was used by this account recently        |
| `reauth_required` on address/delete | Reauthenticates, with any second factor in a new tab, then repeats once |

Change address opens a dialog with the new slug, its grammar hint, and a note
that links to the old address stop working. Delete opens a confirmation naming
the project. Both need recent reauthentication and reuse the step the publish
dialog uses for releasing a resume address. After deletion the page returns to
the list.

## Showing a project on resumes

### In the project screens

New project gains a second field under Name: “Hiển thị trên CV” or “Show on
resumes”, a checkbox per resume of the account (at most three), each with the
resume title and its Public or Draft mark, none checked by default. Create makes
the project, then links each checked resume, and opens the editor. If a link
fails, the project still exists; the editor opens with an alert naming the
resumes that were not linked.

The project editor gains a Resumes section between Details and Address. It lists
every resume of the account:

- a linked resume shows “Đang hiển thị” or “Shown”, and Open in editor, which
  opens that resume with the linked entry selected;
- an unlinked resume shows Add to this resume.

Add to this resume links at once, without Save: it is a resume write, not a
project write. It is disabled while the project has unsaved changes, with the
hint “Lưu dự án trước.” or “Save the project first.”, so the prefill uses what
the person sees. Removing a project from a resume is done in the resume, where
the entry and its text live; this section says so in one muted line.

A resume whose language has no text in the project shows “Trang dự án dùng
{language}” or “Project page uses {language}”, naming the default language.

Delete project names the resumes it will be removed from: “Dự án sẽ bị gỡ khỏi 2
CV. Nội dung mục trong CV được giữ lại.” or “It will be removed from 2 resumes.
The entries keep their text.”

| Outcome                   | What the page does                                       |
| ------------------------- | -------------------------------------------------------- |
| Linked                    | The row turns to Shown; a polite status names the resume |
| `project_already_linked`  | Reloads the list; the row shows Shown                    |
| Resume revision mismatch  | Refetches the resume and repeats the link once           |
| `document_invalid`        | Says the resume could not take the entry and links to it |
| `public_state_busy`       | Says the resume is busy and to try again                 |
| Rate limited, unavailable | Says to try again; nothing is retried automatically      |

### In the resume editor

A project section's Add entry control becomes a menu with two items: Blank
entry, as today, and From your projects, which lists library projects not yet on
this resume by name, with a link to Projects when the library is empty. Choosing
one inserts the prefilled entry and selects it.

A linked entry's card shows a library mark and the project name under its title,
in muted text. Its form shows, above the fields, a `--surface-blue` note: “Mục
này lấy từ dự án {name}. Nội dung ở đây là của riêng CV này.” or “This entry
comes from the project {name}. The text here belongs to this resume.”, with two
actions:

- Refresh from library, which asks first (“Thay nội dung mục bằng nội dung hiện
  tại của dự án?” or “Replace this entry's text with the project's current
  text?”) and then replaces title, subtitle, link, dates and description;
- Open project, to `/app/projects/{id}`.

When the project has no text in the resume's language, the note adds the
fallback line above. Deleting the entry removes the link, as any entry removal
does. Editing, hiding, ordering and undo are unchanged. An entry whose project
was deleted elsewhere arrives without its link after the editor reconciles; the
note disappears and the text stays.

The editor reads the library list once when a project section is opened and
again after a link; it never blocks editing on that read. A failed read hides
From your projects behind a Retry item.

## Accessibility and fit

Every control has a visible label. Language tabs are a tab list with arrow-key
movement. The technology chips are a list whose remove buttons name their chip.
Dialogs trap focus and return it to their trigger. Status changes (saved, error,
limit) are announced politely; errors that block saving are assertive. Nothing
overflows horizontally from 320 px. The page follows the theme tokens and has no
paper surface.
