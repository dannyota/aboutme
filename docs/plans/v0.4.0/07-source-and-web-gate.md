# Localization source and web gate brief

Role: frontend. Model: `gpt-5.6-terra`. Start after every product task report is
verified and all accepted files are present together.

## Objective and authority

Add the catalog parity and scoped-literal guard, update the generated browser
source manifest, and run the complete local web gate. Read `AGENTS.md`, the
accepted localization design and ADR, `AC-EDITOR-018`, and `AC-UI-014`.

## Owned paths

- Create `apps/web/test/editor/workspace-localization-source.test.ts`.
- Modify generated `scripts/web-e2e-source.manifest` only through
  `make web-source-manifest-update`.

Do not edit product source, catalogs, other tests, package files, lockfiles,
design, deployment, or renderer baselines. Report a product defect to the path
owner instead of fixing it.

## Source-test contract

- Enumerate every localized Vue file, noncatalog TypeScript display helper, and
  locale catalog owned by tasks 1 through 6. Scan full Vue and noncatalog helper
  source, not only templates.
- Extract each SFC template and reject nonempty static text nodes and unbound
  user-facing attributes: `aria-label`, `label`, `title`, `placeholder`,
  `description`, and `confirm-label`.
- Use the installed TypeScript compiler API to inspect each `<script>` block and
  helper module. Reject user-facing string and template literals assigned to
  display properties or identifiers such as `label`, `title`, `message`,
  `description`, `placeholder`, `hint`, `help`, `text`, and `ariaLabel`, and
  human-readable strings returned by display mapping functions.
- Exclude approved locale catalog modules from the untranslated-literal rule.
  Check those modules separately for `Record<Locale, SurfaceCopy>` typing,
  recursive key and value-shape parity, and nonempty Vietnamese and English
  values.
- Keep a per-file exact allowlist only for `aboutme`, `A4`, `Letter`, `100%`,
  `DELETE`, language tags, template names, and other invariant terms approved by
  the design. The test must fail when a new literal is not allowlisted.
- Assert `Object.keys()` parity for every Vietnamese and English workspace
  catalog. Recurse through nested plain objects. Function positions must match
  by key; invoke only functions whose test fixture is declared in the test.
- Scan `useResumeList.ts`, `app/new.vue`, `CreateResumeDialog.vue`,
  `ResumeLanguageField.vue`, and `pdfDownload.ts` for the removed English
  message constants so stored translated state cannot return.
- Assert no workspace catalog import occurs under renderer, public page, print
  route, or print worker source roots.

## Test-first cycle and checks

1. Run `git status --short`. Keep inline negative fixture cases in the source
   test that prove the detector rejects static text, every guarded attribute, a
   script object such as `{ label: 'Current icon' }`, a returned display
   message, and a helper-module literal. Write the source test against the
   pre-localized file set and run
   `cd apps/web && npx vitest run test/editor/workspace-localization-source.test.ts`.
   If a negative fixture is accepted, fix the detector before checking real
   source. The test suite must retain those negative fixtures.
2. Run the test against the integrated product. Report any literal or parity
   failure to its owner. Rerun after that owner fixes it and require PASS.
3. Run `make web-source-manifest-update`, inspect the only generated diff, then
   run `make web-source-manifest-check` and require PASS.
4. Run `make web-lint`, `make web-typecheck`, `make web-test`, and
   `make web-build` sequentially. Require PASS. Do not retry a flaky test.
5. Record the candidate total JavaScript bytes with the exact command in the
   main plan. The manager records the base value from the accepted design commit
   and reports the delta.

## Definition of done and report

The source guard, manifest check, lint, typecheck, full web tests, and build all
pass. Report exact files, every command and result, the byte total, skipped
checks with reason, evidence, and open items. Do not perform Git operations. Use
short plain text with no em dash. Do not put plan or task IDs in code, tests, or
comments.
