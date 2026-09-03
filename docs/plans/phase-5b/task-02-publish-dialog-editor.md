# P5B T02 — publish dialog and editor integration

## Contract

Add `PublishDialog.vue` and open it from a clear editor-topbar action. Use the
controller from T01; do not duplicate transport logic in the component.

The dialog has one fieldset with exactly these product choices:

1. **Public resume** controls `live`.
2. **PDF download** controls `downloadEnabled`.
3. **SEO and GEO** controls `seoGeoEnabled`.

PDF download and SEO/GEO are independent of each other but require Public
resume. Turning Public resume off also turns both dependent values off so the UI
cannot knowingly send `requires_live`. A never-published resume begins with SEO
and GEO off. Editing an existing publication begins from canonical stored
metadata. The slug input validates the 4–30 character lowercase ASCII grammar
`^[a-z0-9]+(-[a-z0-9]+)*$` locally; the server remains authoritative for
reserved, claimed, and tombstoned names.

Place these disclosures in separate paragraphs:

- Public resumes may be delivered through a global content-delivery network.
- SEO and GEO allow search crawlers and AI answer engines to discover and reuse
  public resume content.

After success, show the canonical link `/${returnedSlug}` as an anchor. Do not
construct it from unaccepted input. A live-slug rename that receives
`reauth_required` replaces the action area with the account's supported factor:
a current-password prompt when `user.hasPassword` is true, or the stable first
linked provider when provider login is enabled. Password success resumes the
frozen intent. If no supported factor is available, fail closed with fixed copy.
All other failures use fixed local copy and explicit recovery actions.
`publish_invalid` issues are buttons that close the dialog and call the editor's
existing issue-focus behavior.

Provider reauth uses two explicit user actions so asynchronous URL retrieval
cannot lose browser user activation. The first button asks T01 to start the
provider transaction and validate the returned URL. The dialog then renders that
URL as a normal `target="_blank"` anchor with `rel="noopener noreferrer"`. It
never calls `window.open`. After the link is activated, the dialog shows fixed
instructions to finish reauthentication in the new tab, return to the editor,
and choose Retry publish. A missing or rejected URL leaves the fixed unavailable
state and no navigable element.

The modal follows the existing accessible dialog pattern: labelled
`role="dialog"`, initial focus, focus return, Escape close when idle, no close
while dispatching, one submit at a time, disabled invalid controls, and
`aria-live` or `role="alert"` status. It must work at narrow and wide editor
layouts. Session loss and unresolved editor state leave the trigger visible but
explain why publishing cannot start.

Do not add publish state to the resume list in this phase. Do not add copy-link
clipboard behavior, provider endpoints, identity-management scope, provider
reauth beyond the supported provider-only rename flow, a public Nuxt page, or
any MCP operation.

## TDD cases

Write `publish-dialog.test.ts` first and observe failures for defaults, exact
controls, dependent toggles, slug validation, disclosures, save-first state,
issue focus, password and provider reauth selection, same-intent retry,
canonical link, keyboard close/focus return, busy duplicate suppression, and
fixed errors. The provider tests prove the second-click allowlisted anchor,
`target`/`rel`, absence of programmatic `window.open`, explicit retry state, and
no anchor for a missing, blocked, or rejected start result. Update
`editor-shell.test.ts` from the current no-Publish assertion to prove the
trigger and wiring.

## Ownership and checks

Owned paths:

- `apps/web/app/components/editor/PublishDialog.vue`
- `apps/web/app/components/editor/EditorShell.vue`
- `apps/web/app/assets/css/editor.css`
- `apps/web/test/editor/publish-dialog.test.ts`
- `apps/web/test/editor/editor-shell.test.ts`

Acceptance: `AC-PUB-006`, `AC-PUB-008`, `AC-PUB-009`, `AC-PUB-010`.

Run:

```sh
cd apps/web
npx vitest run test/editor/publish-dialog.test.ts test/editor/editor-shell.test.ts
npx eslint app/components/editor/PublishDialog.vue \
  app/components/editor/EditorShell.vue test/editor/publish-dialog.test.ts \
  test/editor/editor-shell.test.ts
npx vue-tsc --noEmit
```

Do not edit T01 files, Git state, list UI, browser harnesses, generated files,
or plan records. Provider authorize-URL validation and network work belong to
T01; this task owns only factor presentation and the user-triggered new-tab
handoff. Report the first observed failing test, changed files, exact commands
and results, and any accessibility or controller contract gap.
