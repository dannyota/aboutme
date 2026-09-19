# Locale foundation brief

Role: frontend. Model: `gpt-5.6-terra`.

## Objective and authority

Extend route locale gating and shared application chrome for the accepted
workspace design. Read `AGENTS.md`,
`docs/adr/0047-bilingual-resume-workspace.md`,
`docs/design/editor-localization.md`, `docs/design/web.md`, `AC-EDITOR-018`, and
`AC-UI-014` before editing.

## Owned paths

- Create `apps/web/app/components/app/LocaleToggle.vue`.
- Create `apps/web/app/i18n/workspace.ts`.
- Create `apps/web/test/workspace-locale.test.ts`.
- Modify `apps/web/app/i18n/locale.ts`.
- Modify `apps/web/app/i18n/shell.ts`.
- Modify `apps/web/app/i18n/meta.ts`.
- Modify `apps/web/app/components/app/AppShell.vue`.
- Modify `apps/web/app/components/app/AccountMenu.vue`.
- Modify `apps/web/app/components/app/AppSeal.vue`.
- Modify `apps/web/app/components/app/StateMark.vue`.
- Modify `apps/web/test/app/app-shell.test.ts`.
- Modify `apps/web/test/app/seal.test.ts`.
- Modify `apps/web/test/app/state-mark.test.ts`.

Do not edit editor, list, page, controller, manifest, design, or deployment
files. Preserve current homepage, auth, legal, gallery, settings, and
`/authorize` behavior.

## Interfaces and behavior

- Export `WorkspaceCopy<T>` exactly as declared in the main plan.
- `LocaleToggle` accepts `label` and `testId`, uses `useLocale()`, retains the
  existing full accessible names, and keeps `landing-locale-*` hooks when used
  by `AppShell`. Editor code will use `workspace-locale-*`.
- `isLocalizedPath()` returns true for `/app/resumes`, `/app/new`, and one
  nonempty `/app/resumes/{id}` segment after trailing-slash normalization. It
  returns false for settings, `/authorize`, extra segments, traversal-shaped
  paths, and lookalikes.
- `workspaceTitles` is a `WorkspaceCopy` with `resumes`, `newResume`, and
  `editor`. Keep English-only `appTitles.settings` and `appTitles.authorize`.
- Extend `ShellCopy` with primary-navigation and account-menu accessible copy.
  `AccountMenu` selects copy from `useRouteLocale()`.
- `AppSeal` gains an optional caller-supplied accessible label and retains its
  current English default. `StateMark` supplies localized state and public-link
  copy from the current route locale.

## Test-first cycle and checks

1. Run `git status --short`. Add failing tests for route boundaries, invalid
   cookies, catalog parity, both shell locales, account menu, and custom seal
   labels.
2. Run
   `cd apps/web && npx vitest run test/workspace-locale.test.ts test/app/app-shell.test.ts test/app/seal.test.ts test/app/state-mark.test.ts`.
   Expect failures for `/app` locale coverage and Vietnamese chrome.
3. Implement the smallest typed copy and component changes. Do not add a
   dependency or change stable hooks.
4. Rerun the same command and require PASS.

## Definition of done and report

Both locales react without navigation. Invalid cookie input resolves to `vi`.
Deferred routes stay English. Report the exact new and changed files, checks and
results, skipped checks with command and reason, evidence paths, and open items.
Do not perform Git operations. Use short plain text with no em dash. Do not put
plan or task IDs in code, tests, or comments.
