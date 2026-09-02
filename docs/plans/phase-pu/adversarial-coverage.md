# Phase PU adversarial coverage

The author of each task writes these cases test-first. A case listed under a
task is that author's job; no separate reviewer supplies it.

## T01 — Toolkit foundation

- `tailwind.css` containing `preflight` in any form fails the toolkit test.
- A `base.css` rule without the `html[data-ui='app'] body` prefix, or a
  descendant selector without the renderer exclusion, fails the toolkit test.
- A generated primitive importing `lucide-vue-next` fails the wrapper script.
- The harness route renders `<html>` without `data-ui`; the app routes render it
  with `data-ui="app"`.
- The renderer golden HTML suite and the screenshot suite produce no diff.

## T02 — Preview photo fallback

- Seeded personal details are byte-equal to the fixture's personal details minus
  the `photo` key, with every other key in the original order.
- The fixture copy test still enforces byte equality with the schema fixture.
- Preview with photo metadata and no URL renders the document without
  `personalDetails.photo` and never includes the key text in its HTML.
- Preview with photo metadata and a URL passes the URL and keeps the metadata.
- A render error while the fallback projection is active still shows the
  existing safe notice.

## T03 — Shared composites

- `TextField`: every row of the U4 matrix; Enter in a multiline field inserts a
  newline and commits nothing; blur after the component unmounts emits nothing;
  an external model change while dirty keeps the draft; Escape then blur emits
  nothing.
- `FormField`: `describedBy` contains only ids that exist; `invalid` is
  `undefined` without an error; a caller-supplied id is used verbatim.
- `ConfirmDialog`: typed confirmation with a different case, trailing space, or
  a stale target stays disabled; Escape while `busy` does not emit; focus
  returns to the opener after confirm and after cancel; hostile `title` text
  renders as text.
- `FormDialog`: submit while `busy` is ignored; the first focusable control
  receives focus; overlay click emits `cancel`.
- `AppShell`: a `/me` 500 renders the signed-out variant; hostile user name
  renders as text; the account menu items navigate and log out.
- `StatusBanner`: `error` has `role="alert"`, others `role="status"`;
  `focusOnMount` focuses the banner.

## T04 — Entry pages

- Every `?error=` value outside the closed vocabulary renders the generic
  message; `__proto__` and `constructor` render the generic message.
- `next` validation on the login page keeps the full adversarial set (`//`,
  scheme, control characters, over-length, non-leading slash).
- Consent page renders a hostile client name as text and posts no client-side
  URL; double submit posts once.
- Password toggle keeps `aria-pressed` and label text; autofill still works (no
  `paste` handler).

## T05 — Resume list

- Delete stays disabled for a title that differs by case or whitespace.
- Rename with the unchanged title emits nothing.
- The opaque-create branch shows Refresh and Abandon and hides the form.
- A hostile resume title renders as text in the row, the rename dialog, and the
  delete dialog.
- Removing a row moves focus to the next row's actions or to Create resume.

## T06 — Settings

- Revoke, log-out-everywhere, and provider start stay disabled without a CSRF
  token.
- A `reauth_required` error on revoke shows the prompt and keeps the list.
- Agent revoke confirmation cannot be dismissed while pending; a `not-found`
  failure refreshes the list.
- Hostile agent client names and user-agent strings render as text.
- The password success and error banners keep their `data-testid` and roles.

## T07 — Editor shell

- Session-lost dialog cannot be dismissed with Escape or the overlay; its three
  actions keep their behavior; the editor stays mounted behind it.
- Narrow mode keeps both regions mounted and toggles `data-narrow-active`.
- The rail buttons expose `aria-pressed` and tooltips; keyboard focus order is
  rail, outline, preview, inspector.
- A long title truncates with an ellipsis and keeps `data-resume-title`.
- Error summary focus and conflict controls keep their `data-action` values.

## T08 — Personal details

- Full name and headline follow the U4 matrix through the panel's intent
  mapping; no `clear` intent is produced anywhere.
- Contact type change to a web profile with a non-`https://` value shows the URL
  error and emits nothing.
- Adding a seventeenth contact shows the limit error and emits nothing.
- Issue buttons focus the right control by `data-field` or `data-detail-*`.

## T09 — Section panel and entries

- Every entry type maps every field through the U4 matrix; link fields reject
  invalid links without emitting.
- Delete-entry confirmation rebinds when the entry changed while open.
- Date fields: invalid start, end before start, present clears end, all-empty is
  `unset`, unchanged emits nothing.
- Rich text: hostile paste sanitizes; empty document maps to `unset`; toolbar
  buttons keep `aria-keyshortcuts`.
- Issue buttons focus inputs, native selects, and the contenteditable root.

## T10 — Structure and templates

- Create section for an existing key shows the status and emits nothing; custom
  keys must be repository UUIDs.
- Delete-section confirmation rebinds when placement or the section changed.
- Move and reorder emit complete permutations only.
- Template partial dialog focuses Retry, maps every unavailable reason, and
  keeps Escape inside the controlled dialog without emitting recovery.
- Hostile section display names render as text in the outline and controls.

## T11 — Customization

- Out-of-range numbers, non-hex colors, and unknown enumerations show the local
  error and emit nothing.
- Switching a group on emits the frozen defaults; switching it off emits `unset`
  once.
- Accent and surface remove buttons emit `unset` only when a value exists.
- Issue buttons focus the control by `data-field`.

## T12 — Photo panel

- Delete confirmation rebinds when the key changed while open.
- Opaque outcome shows Keep and Replace, and Replace stays disabled without a
  file.
- Crop stage keyboard steps clamp to bounds; out-of-bounds numbers show the
  error and emit nothing.
- The preview `img` is absent unless the read is `ready` for the current key.

## T13 — Cleanup and proofs

- The surface-boundary test fails on a raw control introduced anywhere in scope.
- The CSP proof passes with a dialog and a dropdown open.
- Every browser proof passes without a selector change beyond the listed hook
  changes.
