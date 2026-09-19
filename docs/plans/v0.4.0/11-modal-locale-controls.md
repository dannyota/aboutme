# Workspace modal language controls

Role: frontend. Model: `gpt-5.6-terra`.

Provide the accepted language-switching behavior inside modal focus scopes.
Follow the localization design, ADR 0047, and `AGENTS.md`. Keep modal focus
traps, security controls, entered values, pending commands, and revisions.

## Contract

- Add an optional `header-actions` slot to `FormDialog` and `ConfirmDialog`.
  Ordinary callers stay unchanged.
- Make `LocaleToggle.testId` optional. Omit root and option test hooks when
  absent. Existing shell and editor hooks stay unchanged.
- Put a locale control at the end of each scoped modal header. Reuse the
  existing localized language label. Add no catalog, package, or wrapper.
- Keep the control outside the form, enabled while busy, with `type="button"`.
- Prevent pointer-down focus transfer. Pointer activation preserves the active
  field and selection. Keyboard users Tab to a locale button; activation keeps
  focus on that button.
- Never close, reopen, remount, submit, cancel, save, or replay a command when
  locale changes.
- Preserve initial focus: first form field, existing confirmation action,
  template retry action, session-recovery action, or first phone outline action.

## Owned files

Under `apps/web/app/components/`:

- `app/LocaleToggle.vue`, `app/FormDialog.vue`, `app/ConfirmDialog.vue`.
- `editor/list/CreateResumeDialog.vue`, `RenameResumeDialog.vue`, and
  `DeleteResumeDialog.vue`.
- `editor/PublishDialog.vue`.
- `editor/forms/SectionPanel.vue`.
- `editor/structure/StructurePanel.vue`.
- `editor/photo/PhotoPanel.vue`.
- `editor/templates/TemplatePartialDialog.vue`.
- `editor/EditorShell.vue`, including session recovery and the phone sheet.

Under `apps/web/test/`:

- `app/form-dialog.test.ts` and `app/confirm-dialog.test.ts`.
- `workspace-locale.test.ts`.
- `editor/resume-list.test.ts`, `editor/publish-dialog.test.ts`,
  `editor/entry-forms.test.ts`, `editor/structure-controls.test.ts`,
  `editor/photo-panel.test.ts`, `editor/template-panel.test.ts`, and
  `editor/editor-shell.test.ts`.

QA owns browser-spec changes separately. Do not edit its files. Do not perform
Git operations, restart services, or change shared infrastructure. Preserve
other uncommitted work. Coordinate source writes with the manager's browser
window.

## Proof and report

Observe a failing regression first. Test pointer and keyboard behavior in real
dialog components. Assert stable nodes, entered values, and command counts.
Cover one form and one confirmation dialog in depth, then check each caller's
control and initial focus. Confirm ordinary dialog callers remain unchanged.

Run scoped ESLint and affected Vitest files. The manager runs the final web gate
after integration. QA uses controls inside the active dialog by the names
`Tiếng Việt` and `English`; it must not bypass the focus trap.

Report exact files, red and green checks, omitted checks with reasons, and open
items. Use short plain text with no em dash. Do not cite this plan in code.
