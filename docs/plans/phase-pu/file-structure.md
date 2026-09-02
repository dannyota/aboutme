# Phase PU file structure and ownership

Each path has one task author. Shared and generated paths belong to the
integration owner and are serialized. All paths are under `apps/web/` unless
stated otherwise.

Sequential migrations require narrow owner windows on a few final-owner paths.
The integration owner serializes these exact edits:

| Final owner | Earlier window  | Path and permitted edit                                                                                                        |
| ----------- | --------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| T07         | T02             | `EditorPreview.vue` and its test: add the photo-less projection and temporary notice; `EditorShell.vue`: pass `photoRead` only |
| T07         | T01             | `EditorShell.vue`: change moved shell imports and remove the legacy CSS import only                                            |
| T03         | T01             | Shell/theme tests: update moved import paths only                                                                              |
| T01         | T03             | `app/app.vue`: replace the moved `AppChrome` render with `AppShell` only                                                       |
| T13         | T01/T04/T05/T07 | Legacy stylesheets: move tokens/imports, delete only named surface rules, then delete the files                                |

No two windows on one row run together. The final owner rebuilds its file only
after every earlier window is integrated.

## T00 — Authorities and records (owner)

- `docs/adr/0029-application-ui-toolkit.md`
- `docs/design/{README,web,decisions}.md`
- `docs/plans/traceability/{README,ac-ui}.md`
- `docs/plans/implementation-plan.md`
- `docs/plans/phase-pu/**`

## T01 — Toolkit foundation (owner)

- `package.json`, `package-lock.json`, `nuxt.config.ts`, `eslint.config.mjs`,
  `components.json`
- `app/app.vue`
- `app/lib/utils.ts`
- `app/assets/css/{tailwind,theme,base}.css`; token block removed from
  `app/assets/css/app.css`; `editor.css` import removed from `EditorShell.vue`;
  `auth.css` and `landing.css` imports removed from the pages that import them
- `app/components/ui/**` (generated; the three existing files move to
  `app/components/app/`)
- `scripts/ui-add.sh`
- `test/ui/toolkit.test.ts`; contract updates in `test/fonts.test.ts` and
  `test/harness-absent.test.ts`
- `scripts/web-e2e-source.manifest` (integration-owner source-path update)
- Import-path updates in `app/app.vue`, `EditorShell.vue`,
  `test/app-chrome.test.ts`, `test/logout-state.test.ts`,
  `test/editor/theme.test.ts`

## T02 — Preview photo fallback

- `apps/server/cmd/dev-seed/seed.go`, `apps/server/cmd/dev-seed/seed_test.go`
- `app/components/editor/previewProjection.ts`
- `test/editor/preview-projection.test.ts`
- T02 owner window in the shared-path table for `EditorPreview.vue`,
  `EditorShell.vue`, and `test/editor/editor-preview.test.ts`

## T03 — Shared composites

- `app/components/app/{AppShell,AccountMenu,ThemeToggle,PageHeader,FormField,TextField,SelectField,CheckboxField,SwitchField,ConfirmDialog,FormDialog,StatusBanner,EmptyState,IconButton,LoadingState}.vue`
- `app/components/app/AppChrome.vue` and `AccountControl.vue` deleted
- `test/app/{app-shell,form-field,text-field,confirm-dialog,form-dialog,status-banner}.test.ts`
- `test/app-chrome.test.ts` and `test/logout-state.test.ts` (retargeted to
  `AppShell`), `test/editor/theme.test.ts`

The integration owner narrows `app/components/editor/forms/fieldIntent.ts` after
T08 and T09 remove every `clear` producer, then runs web typecheck before W5.

## T04 — Entry pages

- `app/pages/{index,login,register,forgot-password,reset-password,verify-email,authorize}.vue`
- `app/components/auth/PasswordField.vue`
- `test/{landing,login,register,forgot-password,reset-password,verify-email,authorize}.test.ts`
- Legacy rules deleted from `app/assets/css/{auth,landing}.css` (files stay
  until T13)

## T05 — Resume list

- `app/pages/app/resumes/index.vue`
- `app/components/editor/list/{ResumeList,CreateResumeDialog,RenameResumeDialog,DeleteResumeDialog}.vue`
- `test/editor/resume-list.test.ts`
- `.resume-list*` rules deleted from `app/assets/css/app.css`

## T06 — Settings

- `app/pages/app/settings/sessions.vue`
- `app/components/auth/PasswordSettings.vue`
- `app/components/settings/ConnectedAgents.vue`
- `test/{sessions,sessions-csrf-gating,sessions-nullable-fields,sessions-password,sessions-privileged-start-adversarial,password-settings,connected-agents}.test.ts`

## T07 — Editor shell

- `app/components/editor/{EditorShell,EditorPreview,SaveStatus,ErrorSummary,ConflictPanel,InspectorPanel,PreviewToolbar}.vue`
- `app/pages/app/resumes/[id].vue`
- `test/editor/{editor-shell,editor-preview,accessibility}.test.ts`
- `.editor-shell`, `.editor-topbar`, `.editor-app-rail`, `.editor-outline`,
  `.editor-preview*`, `.editor-session-lost`, `.save-status`,
  `.editor-error-summary`, `.editor-conflicts` rules deleted from
  `app/assets/css/editor.css`

## T08 — Personal details

- `app/components/editor/forms/{PersonalDetailsPanel,ContactList}.vue`
- `test/editor/personal-details.test.ts` (the `OptionalField`, `DateRangeField`,
  and `YearMonthField` blocks removed; T09 owns their replacements)

## T09 — Section panel and entries

- `app/components/editor/EntryCard.vue`
- `app/components/editor/forms/{SectionPanel,DateRangeField,YearMonthField}.vue`
- `app/components/editor/forms/entries/*.vue`
- `app/components/editor/richtext/RichTextEditor.vue` (template and toolbar
  only; `schema.ts` and `serialize.ts` unchanged)
- `test/editor/{entry-forms,rich-text}.test.ts`, new
  `test/editor/date-fields.test.ts`

## T10 — Structure and templates

- `app/components/editor/structure/{StructurePanel,SectionControls,EntryOrderControls}.vue`
- `app/components/editor/templates/{TemplatePanel,TemplatePartialDialog}.vue`
- `test/editor/{structure-controls,template-panel}.test.ts`

## T11 — Customization

- `app/components/editor/customization/{CustomizationPanel,ColorField,OptionalCustomizationField}.vue`
  (`OptionalCustomizationField.vue` deleted), `fields.ts` unchanged
- `test/editor/customization-controls.test.ts`

## T12 — Photo panel

- `app/components/editor/photo/{PhotoPanel,CropEditor}.vue`
- `test/editor/photo-panel.test.ts`

## T13 — Cleanup, proofs, records (owner)

- `app/assets/css/{app,editor,auth,landing}.css` deleted and their
  `tailwind.css` import lines removed
- `app/components/editor/forms/OptionalField.vue` deleted
- `test/ui/surface-boundary.test.ts`
- `README.md` (UI conventions section)
- `docs/architecture.md`, `docs/plans/implementation-plan.md`,
  `docs/plans/traceability/ac-ui.md`, `docs/runbooks/local-uat.md` if a proof
  changes

## Retained hooks

Every hook below survives unless the owning task file lists it under "Hook
changes".

### Shell and entry (T03, T04)

- `data-testid`: `landing-point`, `landing-license`, `login-error`,
  `login-form-error`, `register-error`, `register-success`, `forgot-error`,
  `forgot-success`, `reset-error`, `reset-success`, `verify-error`,
  `verify-success`, `consent-error`, `consent-client-name`
- `data-decision="approve"`, `data-decision="deny"`
- ids `login-email`, `login-password`, `forgot-email`, `password-new`,
  `password-current`, and `{id}-confirm`
- Labels: `Email`, `Password`, `Name`, `Confirm password`, `New password`,
  `Current password`; the `PasswordField` toggle `aria-label`
  `Show {label}`/`Hide {label}` with `aria-pressed` and text `Show`/`Hide`
- Texts: `aboutme`, `Resumes`, `Settings`, `Sign in`, `Create account`,
  `Forgot password?`, `Send reset link`, `Back to sign in`, `Reset password`,
  `Approve`, `Deny`; `aria-label="Primary navigation"`,
  `aria-label="Requested permissions"`; theme toggle `aria-label`
  `Switch to {theme} theme` and texts `Light mode`/`Dark mode`

### Resume list (T05)

- `data-testid`: `create-resume`, `resume-row-{id}`
- `aria-label="Your resumes"`, `Rename {title}`, `Delete {title}`
- Dialog titles `Create resume`, `Rename resume`, `Delete resume`; labels
  `Title`, `Current title`, `Language`; buttons `Create`, `Save`, `Cancel`,
  `Refresh list`, `Abandon`, `Delete`; texts `No resumes yet.`,
  `Create a new private resume.`,
  `We could not confirm whether this resume was created.`

### Settings (T06)

- `data-testid`: `revoke-error`, `link-error`, `reauth-prompt`,
  `session-row-{id}`, `revoke-button`, `revoke-all-button`,
  `add-provider-button`, `password-settings`, `password-status`,
  `password-success`, `password-error`, `password-action`,
  `password-set-submit`, `password-reauth-submit`, `password-cancel`,
  `password-provider-reauth-{provider}`, `agents-loading`, `agents-error`,
  `agents-retry`, `agent-row`, `agent-revoke`
- Texts: `Signed-in devices`, `This device`, `Log out`, `Revoke`,
  `Log out everywhere`, `Add another sign-in provider`, `Link {provider}`,
  `Sign in again with {provider}`, `Connected agents`, `Revoke access`,
  `Cancel`, `Password`, `You have a password.`, `No password set.`,
  `Change password`, `Add a password`, `Save password`, `Continue`,
  `Continue with {Provider}`

### Editor shell (T07)

- `data-resume-title`; `data-action`: `show-editor`, `show-preview`,
  `resume-after-auth`; `data-region`: `app-rail`, `outline`, `preview`,
  `inspector`; `data-responsive-region`, `data-narrow-active`,
  `data-outline-key`, `data-state` on the save status, `data-conflict`,
  `data-estimated-pages-label`
- `aria-label`: `Editor tools`, `Document`, `Structure`, `Design`, `Templates`,
  `Photo`, `Account settings`, `Resume outline`, `Add section`, `Editor view`,
  `Estimated page count`; `aria-current="page"` on the active outline item;
  `aria-pressed` on rail and view buttons
- Texts: `Editor`, `Preview`, `Estimated pages`, `Resume`, `Add section`,
  `Sign in to continue editing`, `Resume after sign-in`, `Discard and sign in`,
  `Open sign-in in another tab`, `Check these fields`, `Review changes`,
  `Accept latest`, every `ConflictPanel` control label, every `SaveStatus` text,
  `Preview is temporarily unavailable. Your edits are still safe.`

### Personal details (T08)

- `data-field="fullName"`, `data-field="headline"`, `data-issue`;
  `data-detail-index`, `data-detail-id`, `data-detail-type`,
  `data-detail-label`, `data-detail-value`, `data-detail-is-hidden`;
  `data-action`: `unset-detail-label`, `move-detail-up`, `move-detail-down`,
  `remove-detail`, `add-detail`, `unset-details`; `data-error`: `contact-url`,
  `detail-limit`
- Labels and texts: `Personal details`, `Full name`, `Headline`,
  `Contact details`, `Contact detail {n}`, `Type`, `Label`, `Value`,
  `Hide this detail`, `Move up`, `Move down`, `Remove detail`, `Add detail`,
  `Remove contact list`, `Remove label`, `Use a lowercase https:// URL.`,
  `You can add up to 16 contact details.`

### Section panel and entries (T09)

- `data-section-key`, `data-section-id-text`, `data-entry-id`,
  `data-entry-id-text`, `data-entry-field="{path}"`, `data-delete-dialog`,
  `data-issue`; `data-action`: `add-entry`, `toggle-hidden`, `delete-entry`,
  `confirm-delete-entry`, `cancel-delete-entry`, `unset` on date fields;
  `data-part`: `start-year`, `start-month`, `end-year`, `end-month`, `present`,
  `year`, `month`; `data-error="date-order"`
- `aria-label="Section issues"`, `aria-label="Rich-text controls"` with
  `role="toolbar"`, toolbar button `aria-label`s `Paragraph`, `Line break`,
  `Bold`, `Italic`, `Underline`, `Ordered list`, `Bullet list`, `Link`, `Unlink`
  with their `aria-keyshortcuts`
- Labels: `Job title`, `Employer`, `Employer link`, `City`, `Country`,
  `Work description`, `Name`, `Level`, `Skill information`, `Degree`, `School`,
  `Hidden`, `Date range`, `Start year`, `Start month`, `End year`, `End month`,
  `Present`, `Year`, `Month`; texts `Add entry`, `Delete entry`, `Delete`,
  `Cancel`, `Entry {n}`, `Remove date range`, `Remove date`

### Structure and templates (T10)

- `data-section`, `data-entry-order`; `data-action`: `section-type`, `create`,
  `displayName`, `iconKey`, `move-up`, `move-down`, `move-main`, `move-sidebar`,
  `reorder`, `delete`, `confirm-delete`, `cancel-delete`, `entry-up`,
  `entry-down`, `reopen-placement`, `reopen-entry-order`, `undo-template`,
  `retry-remaining`, `restore-pre-apply`, `keep-partial`; `data-template`,
  `data-template-preview`
- `aria-label`: `Section placement controls`, `Entry order`, `Template presets`,
  `Template warnings`, `Template change progress`
- Labels and texts: `Sections`, `Section type`, `Column`, `Section name`,
  `Icon key`, `Add section`, `Delete section`, `Move to main`,
  `Move to sidebar`, `Move to start`, `Move entry up`, `Move entry down`,
  `Review the highlighted section controls.`, `Templates`, `Apply`,
  `Undo template changes`, `Template changes are ready to save.`,
  `Saving template`, `Template saved`, `Template needs attention`, `No changes`,
  `Template changes need review`, `Retry remaining`, `Restore pre-apply`,
  `Keep partial`

### Customization (T11)

- `data-field="{path}"`, ids `customization-{path-with-dashes}` and
  `customization-{path-with-dashes}-error`, `data-error-for`, `data-issue`;
  `data-action`: `unset-accent`, `unset-surface`, `unset-surface-target`
- Labels: `Primary color`, `Text color`, `Background color`, `Accent color`,
  `Surface color`, `Page margins`, `Horizontal margin`, `Vertical margin`,
  `Header`, `Header alignment`, `Contact layout`, `Icon style`,
  `Surface target`; texts `Customization`, `Remove surface target`,
  `Enter a value within the allowed range.`, `Enter a six-digit hex color.`,
  `Choose one of the available options.`

### Photo (T12)

- `data-photo-preview`, `data-photo-outcome`, `data-crop-stage`,
  `data-crop-rectangle`; `data-action`: `delete`, `retry-photo`,
  `confirm-delete`, `cancel-delete`, `keep-observed`, `replace`, `reopen-crop`,
  `clear-crop`
- Labels: `Upload photo`, `Replace photo`, `Select a replacement photo`,
  `Crop position` (`role="application"`), `X`, `Y`, `Width`, `Height`
- Texts: `Photo`, `Authorized photo preview.`, `Photo preview is loading.`,
  `Photo preview is unavailable.`, `No photo has been added.`,
  `Uploading photo.`, `Choose a JPEG or PNG image.`,
  `This image exceeds the allowed size.`, `This image could not be used.`,
  `Delete photo`, `Delete the current photo?`, `Cancel`, `Save crop`,
  `Clear crop`, `Enter a crop within the image bounds.`, `Keep observed photo`,
  `Replace photo`, `Retry photo request`, `Reopen crop`
