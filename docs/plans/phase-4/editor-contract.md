# Phase 4 Editor Contract

Status: **Approved — ready for implementation planning.**

## Purpose

This contract defines the editor controls and user-visible behavior. It consumes
the [state and route design](design.md),
[mutation contract](mutation-contract.md), and
[template group contract](template-group-contract.md). It does not redefine
endpoint, document, renderer, sanitizer, or template-helper behavior.

## Editor layout

The editor presents:

- owner-visible resume title and language;
- personal details;
- section and entry controls;
- customization and template panels;
- private photo controls;
- save, validation, conflict, partial-group, and session status; and
- a responsive paged preview.

On wide screens controls and preview may appear side by side. On narrow screens
they are switchable regions. Both layouts keep one store and one command queue;
switching layout never refetches or resets the form.

## Draft field behavior

Forms use generated current-v2 types. They preserve absence versus explicit
clear and never add placeholder values to satisfy publish completeness.

Every entry exposes its UUID and optional `isHidden` behavior. IDs are not
editable text. Each section exposes generated `displayName` and `iconKey`
controls.

| Section type  | Editable domain fields                                                |
| ------------- | --------------------------------------------------------------------- |
| `profile`     | Rich-text `text`                                                      |
| `work`        | Job title, employer, employer link, city, country, dates, description |
| `education`   | Degree, school, school link, city, country, dates, description        |
| `skill`       | Name, optional level 0–5, rich-text information                       |
| `language`    | Name and optional level 0–5                                           |
| `certificate` | Title, title link, issuer, date, description                          |
| `project`     | Title, link, dates, description                                       |
| `custom`      | Title, title link, subtitle, city, dates, description                 |

Date controls express only shapes admitted by the generated schema. Local
ordering checks improve feedback; server validation remains authoritative. URL
controls require each field's exact lowercase schemes. Contact website,
LinkedIn, GitHub, and Twitter values accept only exact lowercase `https://` or
an empty draft value.

Clearing a present string writes `""` where allowed. An untouched absent field
stays absent. Removing an optional date, level, crop, icon, or customization
leaf uses that operation's declared unset or null form.

## Section and entry structure

`customization.layout.sections` is the only section-order and placement
authority. The editor never derives order from `content` key iteration.

Every section has keyboard controls for move up, move down, move to main, and
move to sidebar. Drag and drop may mirror them but is never the only path.
Create, delete, move, and section reorder use only the structure endpoint.

Entry reorder operates within one section and sends a permutation through
section metadata. Entry creation generates one UUID before creating its local
command. Entry deletion names its current entry and requires confirmation.

The column-count control changes only the allowlisted `layout.columns` leaf and
preserves both placement arrays. Moving a section remains an explicit structure
action. The control never calls `applyTemplate`.

## Customization controls

The panel covers every generated leaf under:

- font family and base size;
- primary, text, background, optional accent, and optional surface colors;
- section gap, entry gap, line height, and optional page margins;
- heading style and rule;
- header alignment, contact layout, and icon style;
- column count and optional surface target;
- skill and language display styles; and
- page and date formats.

Optional leaves have explicit set/unset controls. Controls use generated enums
and numeric bounds. They show the renderer's point-of-use fallback without
writing that fallback into the document.

Customization commands emit only allowlisted leaf deltas. No customization
control writes `layout.sections`.

## Rich-text contract

ProseMirror uses a closed schema derived from sanitizer allowlist version 1:

- nodes: document, paragraph, text, hard break, ordered list, bullet list, and
  list item;
- marks: strong, emphasis, underline, and link; and
- link attributes: `href`, exact `rel="noopener noreferrer"`, and optional
  `target="_blank"`.

Links accept only explicit `https:`, `mailto:`, or `tel:` schemes. The toolbar
offers paragraph, line break, bold, italic, underline, ordered list, bullet
list, link, and unlink. It cannot create headings, images, tables, embedded
media, styles, classes, or arbitrary attributes.

HTML paste passes through the existing generated-policy DOMPurify wrapper before
ProseMirror parses it. The ProseMirror schema then rejects anything outside the
closed model. File and image paste/drop are prevented. Plain-text paste remains
text.

Empty editor content serializes as `""`, not `<p></p>`. Merely focusing an
absent rich-text field creates nothing. Server write sanitization remains
canonical; the accepted response replaces optimistic serialization after save.

## Template application

Selecting Apply computes:

```text
applyTemplate(current.customization, preset, current.content)
```

The pure result appears immediately as one client-visible action. Content is
never changed.

The placement diff is deterministic. Starting from current placement, it visits
target `main` left to right, then target `sidebar` left to right. A key not at
that target index emits `moveSection` using the server's remove-then-insert
rule. Applying the complete list must yield the helper's exact arrays before any
request is admitted.

Every changed customization leaf except `layout.sections` becomes a
deterministically ordered `set` or `unset` delta. The template group contract
owns group target/context checks, partial recovery, and final saved state.

Before apply, the editor warns when `pageFormat` or `dateFormat` changes. It
also warns at `baseSizePx: 10` and when either page margin is below 5 mm. These
are warnings, not draft validation failures.

The latest fully accepted template group exposes one Undo action. Undo requests
the pre-apply customization and placement as a new guarded reverse group. Any
later change to an affected target invalidates the action. Undo is not general
history, server rollback, or a way around a partial-group conflict.

A partial apply shows the accepted subset, intended final result, and only the
three recovery actions defined by the template group contract. It never labels
the preset selected or saved because the document stores no template identity.

If helper output already equals current customization and placement, Apply
reports **No changes** and creates neither a request nor an undo record.

## Photo lifecycle

Upload sends the selected file unchanged to the owner multipart route. The
browser does not preview, decode, crop, persist, or render source bytes before
server acceptance. The server remains the normalization and media-safety
authority.

After an accepted upload or replacement, the photo controller fetches
`GET /resumes/{id}/photo` with credentials. It retains the strong object ETag
and uses `If-None-Match` on a later read. It accepts only declared JPEG or PNG
responses and converts accepted normalized bytes to an in-memory `data:` URL,
which the renderer CSP permits.

A `304` retains the prior data URL. A `200` replaces it. Delete clears it. The
controller releases the old in-memory value on replacement, deletion, or page
unmount. It binds each data URL to the exact accepted photo reference and
suspends preview when that binding differs. It never builds a media route from
`personalDetails.photo.key`; the renderer never receives that key.

When document photo metadata exists, preview renders only with the authorized
data URL for that same acknowledged photo. Loading shows a suspended-preview
state. Read failure also suspends complete preview; forms and replace/delete
controls remain usable.

Crop controls operate on the normalized owner image. Pointer movement updates
the optimistic normalized rectangle, and numeric x, y, width, and height inputs
provide a complete keyboard path. Saving uses `{crop: rectangle|null}`.
Replacement clears crop. A crop conflict caused by a new photo has no generic
Apply mine action; the owner must reopen crop against the new image.

Upload progress, unsupported type, size, invalid image, busy, network, read, and
revision failures are distinct states. `media_busy` and rate limits show
`Retry-After`. Object keys, filenames, file metadata, CSRF tokens, and raw
dependency errors never enter UI text, evidence, or logs.

## Validation and errors

The page error summary lists every active command error. Server
`details.issues[]` are indexed by exact path.

- A mapped path links to and focuses its field.
- An unmapped path remains in the summary and never disappears silently.
- A bounds issue names the safe product limit without echoing rejected content.
- A conflict shows base, latest, and local values in the appropriate safe text
  or structured control.
- A partial template group names accepted and remaining changes.
- Session loss uses the retained-work flow in the design.

The UI uses stable client messages keyed by server error code. It does not show
raw decoder, database, object-store, stack, or request body text. Request IDs
may be shown for support correlation.

## Conflict controls

The mutation contract owns comparison and fresh-read admission. The UI exposes
generic Apply mine only when it receives a valid replacement command with
separate target and non-target context.

These conflicts require dedicated controls:

- A missing entry or one under a different section type offers explicit recreate
  or selection of another entry.
- A new-entry ID collision regenerates the ID only through explicit recreate.
- Changed entry-order membership reopens reorder against current members.
- A removed or retyped section offers explicit recreate or selection.
- A structural move or reorder whose context lacks a moved or reordered section
  key, or finds a different `sectionType` for that key, reopens placement
  against current section identities. It has no generic Apply mine action.
- A crop with a changed photo key reopens crop against the new normalized photo;
  it never applies old coordinates.
- A template partial uses only its group recovery controls.

Same-membership reorder may be applied against the latest order. A field
override may proceed when entry and section context still holds. Customization
override treats the latest path as new target base, not as a prerequisite. Crop
override treats the latest crop as target base while requiring the same photo
key as context.

Entry delete treats the latest complete entry/membership as target base and the
parent section identity as context. Entry, section, photo, and resume deletion
overrides show the latest target and require destructive confirmation again.
Photo replacement also reconfirms when its source photo changed.

## Accessibility

- Every field has a persistent label and associated error or description.
- The error summary receives focus after a submitted action is rejected.
- Save state and background conflicts use polite live regions. Destructive
  confirmation and terminal session actions use assertive announcements.
- Panels, preset choices, reorder controls, crop controls, dialogs, and preview
  mode are keyboard operable with visible focus.
- Pointer drag has button or numeric equivalents.
- Color controls expose text values and do not rely on color alone.
- Focus does not move on background save success.
- A conflict moves focus only when it blocks the action the owner is handling.
- Preview suspension states explain whether photo, renderer, or network input is
  missing without treating the resume as empty.

## Native HTTPS browser proof

Phase 4 adds a separate editor Playwright suite to the existing native HTTPS
browser image. The authentication scenario remains unchanged. The editor suite
uses `https://localhost:20443`, the deterministic fake Google account, Secure
`__Host-` cookies, the shared development database, and no TLS bypass.

The suite creates only uniquely named test resumes and deletes only IDs it
created. It permits network access only to the fixed HTTPS origin and fails on
an external request, certificate error, page error, unexpected console error, or
secret-bearing evidence. Evidence stays under the ignored harness directory.
Teardown leaves the shared database running.

The browser scenarios prove:

1. login, blank create, editor load, and logout;
2. immediate preview, one-second autosave, and reload persistence;
3. an owner response requested with `Accept-Encoding` retains its exact strong
   `"rN"` ETag through Caddy, and the next mutation reuses that value unchanged
   as `If-Match`;
4. one safe unrelated stale rebase and one same-target visible conflict;
5. conflict actions that obey entry, reorder, and photo context;
6. normalized owner photo read, crop, replacement, and preview suspension on
   read failure;
7. template apply final-state checking, undo, and a forced partial failure;
8. retained unsaved work across session loss and reauthentication in another
   tab; and
9. keyboard operation and automated accessibility checks for the main flow.

This suite proves authenticated editor behavior. It does not satisfy Phase 9
U1-U5, port-443 isolation, production image topology, or cloud UAT.

## Acceptance scenarios

1. Every generated field shape across all eight section types remains editable
   without publish-time completeness checks.
2. Structure and customization controls respect their separate API authorities.
3. Hostile paste, unsafe links, files, images, and unsupported markup cannot
   enter the editor document or rendered preview.
4. Template apply preserves content, reaches the exact helper result before
   showing saved, and exposes only a valid guarded undo.
5. Preview never renders a document/photo mismatch or source upload bytes.
6. Validation, conflict, partial, session, and photo states are labelled,
   keyboard reachable, and safe to display.
7. The separate native HTTPS suite passes without external network, TLS bypass,
   or secret-bearing evidence.
