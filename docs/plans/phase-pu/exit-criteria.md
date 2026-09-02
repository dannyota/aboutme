# Phase PU exit criteria

The integration owner checks every item at one unchanged candidate commit.
Failed or unsatisfiable items are corrected and rerun under ADR 0024.

## Authorities and dependencies

- [ ] ADR 0029, the web design "Application UI" and "Pure renderer" text, the
      traceability rows, and this plan agree; no design text still describes
      Set, Clear, or Remove field buttons or a preview that waits for the photo.
- [ ] `apps/web/package.json` carries exactly the U1 pins plus any transitive
      package recorded in decisions U1; `npm ls` reports no extraneous or
      missing package; `make tools-check` passes.
- [ ] No `lucide-vue-next` import exists anywhere under `apps/web/app`.

## Renderer isolation (AC-UI-001)

- [ ] `tailwind.css` matches decisions U2 byte for byte except for the four
      legacy import lines, which are absent; the toolkit test proves no
      Preflight import and the two selector guards on every `base.css` rule.
- [ ] `npx vitest run test/renderer` and `make web-e2e` pass with no baseline
      change since the PM candidate.
- [ ] The renderer import-boundary lint passes and `app/components/resume/**`,
      `app/components/public/**`, `app/public/**`, `server/**`, and
      `app/pages/_harness/**` have no diff since the PM candidate.

## Component layers (AC-UI-002, AC-UI-003)

- [ ] `test/ui/surface-boundary.test.ts` passes: no raw `<button>`, `<input>`,
      `<select>`, `<textarea>`, or `role="dialog"`/`role="alertdialog"` element
      outside `components/ui`, `components/app`, the renderer, the crop stage,
      and the ProseMirror root.
- [ ] The four legacy stylesheets, `OptionalField.vue`, and
      `OptionalCustomizationField.vue` are deleted; no `<style>` block exists
      under `app/components/app`, `app/components/editor`, or `app/pages` except
      `_harness`.
- [ ] Every dialog traps focus, closes on Escape unless busy, and returns focus
      to its opener; the composite suites prove it.
- [ ] The `TextField` matrix in decisions U4 passes; `fieldIntent.ts` has no
      `clear` member; `app/editor/**`, `app/stores/**`, and
      `app/composables/useResumeEditor.ts` have no diff since the PM candidate.

## Preview and photo (AC-UI-004)

- [ ] The dev-seed suite proves the seeded personal details carry no `photo` and
      the fixture copy still matches the schema fixture byte for byte.
- [ ] The editor-preview suite proves the preview renders a photo-less
      projection while the read is loading or unavailable, passes the authorized
      data URL when ready, and never renders the stored key.
- [ ] On the native stack after `dev-seed cleanup` and `seed`, opening the
      sample resume logs no `resume photo key invariant failed` line and the
      preview shows pages.

## Accessibility, theme, and copy (AC-UI-005)

- [ ] The accessibility suites pass; the native entry and editor proofs report
      zero console and page errors.
- [ ] Every migrated screen was reviewed in light and dark at 1440×1000 and
      1024×768 with screenshots under `.dev/design-qa/pu/`; no P0–P2 finding
      remains open.
- [ ] Reduced motion disables the primitives' animations; the theme cookie
      persists across the shell and the editor; the CSP proof
      (`normal-csp.spec.ts`) passes with the teleported dialogs open.

## Regression proof and records (AC-UI-006)

- [ ] Every retained hook in `file-structure.md` resolves in its surface; every
      "Hook changes" entry in a task file has a matching test update.
- [ ] `make web-lint web-typecheck web-test web-build` pass; `make web-e2e`,
      `make dev-https-auth-check`, `make dev-https-editor-check`,
      `make dev-https-mcp-check`, `make dev-https-entry-check`, and
      `make dev-https-public-check` pass with only the copy changes the task
      files list.
- [ ] Every T00–T13 report matches the handoff format; shared edits and unrun
      checks are resolved or block exit.
- [ ] The owner updates and commits the master plan, traceability (AC-UI rows
      `PROVEN`), architecture narrative, and the web README before review.
- [ ] One fresh non-author reviews the full candidate and confirms by name the
      renderer isolation, dialog focus, field commit, hostile-text rendering,
      hook retention, and theme and CSP invariants; the same reviewer confirms
      fixes.
- [ ] `make ci` passes alone, then connected `SEMGREP_APP_TOKEN` `make scan`
      passes alone on the same unchanged candidate; this directory is deleted in
      the exit commit.
