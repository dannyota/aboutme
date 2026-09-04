# PV T07 — editor shell and narrow layout

## Contract

Rebuild the chrome of `EditorShell.vue` and `EditorPreview.vue` on the desk
grammar without touching the editor core, intents, coordinator, or store.

### Top bar (`header.editor-topbar`, 64 px)

Left to right: brand link "aboutme" (`font-semibold`), a rule, the title
`h1[data-resume-title]` (`text-sm font-medium`, truncating), then `StateMark`
for save state (`data-testid="save-status"` held) and, when
`record.current.metadata.live && slug`, `StateMark state="public"` with the
canonical slug (`data-testid="public-mark"`). Right:
`Button variant="seal" data-action="publish"` "Publish" (the only seal control
on the page), then an account menu: `IconButton aria-label="Account menu"` with
the `UserRound` icon opening a `DropdownMenu` with "Settings" (link to
`/app/settings/sessions`), "Dark theme" / "Light theme" (calls the existing
`useTheme().toggleTheme()`, `data-testid="theme-toggle"` held), and "Log out".
`ThemeToggle.vue` and `AccountControl.vue` are no longer mounted in the editor;
the app chrome (`AppShell`) mounts the same account menu in T06's page grammar.
Session-lost keeps its markup and `data-action`s.

### Rail, outline, preview

- Rail buttons wrap in `Tooltip` with their `aria-label`; the pressed state is
  `bg-secondary text-primary`. Nothing green.
- Outline rows carry each section's optional `iconKey` and resolve it with
  `iconFor(section.iconKey ?? '')` from `components/resume/icons.ts`. The
  personal row uses `iconFor('user')`. All unresolved icons fall back to
  `PanelsTopLeft`; rows keep `data-outline-key` and `aria-current`.
- `EditorPreview.vue` drops its header bar. The canvas is `bg-background`
  padding 24 px; the sheet wrapper is
  `rounded-[var(--radius-sheet)] shadow-[var(--shadow-paper)] bg-white` (the
  renderer paints its own background on top). Page count renders as a pencil
  mark under the sheet: "1 page" / "{n} pages" (`data-testid="page-count"`; hook
  change from "Estimated pages").
- Zoom stays 0.84 wide, 0.72 at ≤ 72 rem; at ≤ 42 rem compute
  `zoom = min(1, (viewportWidth - 32) / sheetWidthPx)` from a `ResizeObserver`
  so the sheet is always whole.

### Narrow layout

- ≤ 72 rem: grid `"topbar" / "content"`; the rail becomes a horizontal strip
  (`role="toolbar"`, same buttons) at the top of the inspector; outline and
  inspector stack inside the edit view with the outline collapsible
  (`Collapsible`, default open above 42 rem).
- ≤ 42 rem: the top bar shows brand, save mark, Publish, and the account menu
  only; a fixed bottom `role="tablist"` "Edit | Preview"
  (`data-action="show-editor"` / `"show-preview"` held, `aria-pressed` held)
  with `padding-bottom: env(safe-area-inset-bottom)`; the edit view gets bottom
  padding equal to the switch height; the outline is a `Sheet` opened by a
  "Sections" button at the top of the inspector.

### `app.vue`

Keep `isAppSurface`; make sure `/app/settings/**` is included (T06 also edits
this line; the owner merges).

Strings held: "Publish", "Editor", "Preview", "Resume", "+ Add section", "Sign
in to continue editing", "Resume after sign-in", "Discard and sign in", every
`data-action`, `data-region`, `data-responsive-region`, `data-outline-key`, and
`aria-label` in the current shell.

## TDD cases

Update `test/editor/editor-shell.test.ts` first: exactly one element with
`data-action="publish"` and it carries the seal variant class; the public mark
appears only when live with a slug and reads the canonical slug; the account
menu contains Settings, the theme item, Log out, and toggles the theme; rail
tooltips expose labels; outline rows render the section icons; at a mocked 390
px viewport the bottom tablist exists, `show-preview` reveals the preview region
with `zoom` below 1 and the sheet's scaled width under 390, and the inspector's
`clientWidth` equals the viewport; a hostile title renders as text. Update
`test/editor/editor-preview.test.ts` for the page-count mark and the absence of
the header bar.

## Ownership and checks

Owned paths:

- `apps/web/app/components/editor/EditorShell.vue`
- `apps/web/app/components/editor/EditorPreview.vue`
- `apps/web/app/components/app/AppShell.vue`, `AccountMenu.vue`
- `apps/web/app/app.vue` (route list only)
- `apps/web/test/editor/editor-shell.test.ts`,
  `test/editor/editor-preview.test.ts`, `test/app/app-shell.test.ts`

Acceptance: `AC-UI-011`, `AC-UI-004` and `AC-UI-005` re-proof.

Run:

```sh
cd apps/web
npx vitest run test/editor/editor-shell.test.ts test/editor/editor-preview.test.ts test/app/app-shell.test.ts
npx eslint app/components/editor/EditorShell.vue app/components/editor/EditorPreview.vue app/components/app app/app.vue test/editor test/app/app-shell.test.ts
npx vue-tsc --noEmit
```

Do not edit `app/editor/**`, panels, the publish dialog, or Git state. Report
the first failing test, exact commands, and the measured sheet width at 390 px.
