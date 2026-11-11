# Projects workspace

Status: Accepted (2026-11-11); not yet implemented. The
[projects design](../projects.md) owns the product and its limits, and the
[projects contract](../projects-contract.md#owner-api) owns the API this screen
calls. This document owns the two workspace screens. Choosing which resumes show
a project arrives with resume linking and is not part of these screens yet.

## Navigation

The signed-in shell adds a Projects link (“Dự án”) after Resumes, to
`/app/projects`. It hides below 640 px with the other direct links, and the
account menu gains a Projects item above Views, so the page is always one tap
away. No signed-out page links to it; opening it signed out goes to sign in and
returns.

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
| `reauth_required` on address/delete | Opens the reauthentication step, then repeats the action once           |

Change address opens a dialog with the new slug, its grammar hint, and a note
that links to the old address stop working. Delete opens a confirmation naming
the project. Both need recent reauthentication and reuse the step the publish
dialog uses for releasing a resume address. After deletion the page returns to
the list.

## Accessibility and fit

Every control has a visible label. Language tabs are a tab list with arrow-key
movement. The technology chips are a list whose remove buttons name their chip.
Dialogs trap focus and return it to their trigger. Status changes (saved, error,
limit) are announced politely; errors that block saving are assertive. Nothing
overflows horizontally from 320 px. The page follows the theme tokens and has no
paper surface.
