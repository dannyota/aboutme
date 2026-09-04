# PV exit criteria

All items run against one unchanged candidate commit. Record the candidate SHA
and exact command outcomes in the phase exit report before deleting this
directory. A failure invalidates later results until the fix is committed and
the affected sequence is rerun.

## Renderer isolation

- [ ] `cd apps/web && npx vitest run test/renderer` passes with no snapshot
      update.
- [ ] `make web-e2e` passes with every baseline under `apps/web/e2e/baselines/`
      and `apps/web/e2e/print-baselines/` byte-identical to `main`.
- [ ] `apps/web/app/components/resume/**`, `apps/web/app/public/**`,
      `apps/web/server/**`, and `apps/web/app/pages/_harness/**` show no diff
      against `main` except the T01 rebase's already-reviewed changes.
- [ ] The T01 stylesheet test still refuses `preflight` and any base rule
      without the `html[data-ui='app']` and renderer-exclusion guards.

## Identity and behavior

- [ ] `AC-UI-001` through `AC-UI-013` are `PROVEN` with exact evidence.
- [ ] `grep -rn "positive\|chart-" apps/web/app/assets/css` returns nothing;
      `--seal` appears only in `AppSeal.vue`, `StateMark.vue`, and the Publish
      control's variant.
- [ ] Landing HTML from `curl -s http://localhost:20080/` contains the rendered
      sample resume, the seal `aria-label`, and no `/api/v1` request in the
      server log for that response.
- [ ] The dark theme chosen in the editor persists to `/app/settings/sessions`
      and `/app/resumes` after a full navigation.
- [ ] At 390 px the editor shows the whole preview sheet in Preview view and no
      clipped inspector text in Edit view.
- [ ] Publish and unpublish through the dialog add and remove the seal mark
      beside the title; with `prefers-reduced-motion: reduce` the change is
      instant.
- [ ] Every page passes the axe scan in light and dark with no serious or
      critical violation.

## Impeccable finish

- [ ] `node <impeccable>/scripts/detect.mjs --json apps/web/app` reports no
      mechanical finding that T10 did not resolve or justify.
- [ ] `.impeccable/review/desktop.png` and `mobile.png` exist for the landing,
      list, settings, and editor, captured after motion settled.
- [ ] The finish reviewer's last disposition is `ship`, or every open finding is
      listed in the exit report with the owner's decision.
- [ ] `DESIGN.md` exists at the repository root, written by the documenter from
      the built pages, and `impeccable doctor` reports no drift.

## Documentation and review

- [ ] ADR 0030 is accepted, `docs/design/web.md` and `docs/design/product.md`
      carry the amended text, `docs/design/decisions.md` lists 0029 and 0030,
      and the architecture narrative and `apps/web/README.md` describe the
      implemented chrome by component.
- [ ] A fresh non-author reviewer reports no unresolved finding and confirms by
      name: renderer isolation, dialog focus invariants, the field commit rule,
      hostile-text rendering, test-hook retention, theme and CSP behavior, and
      the human-only publish boundary.
- [ ] The integrated diff contains no generated-file hand edit, unrelated
      change, secret, credential, personal data, or committed local evidence.

## Unchanged-candidate gates

- [ ] `make web-lint web-typecheck web-test web-build`
- [ ] `make dev-https-auth-check dev-https-editor-check dev-https-mcp-check dev-https-entry-check dev-https-public-check dev-https-publish-check`
- [ ] `make docs-fmt` leaves the tree unchanged.
- [ ] `make ci`
- [ ] Connected `make scan` with `SEMGREP_APP_TOKEN` available at runtime.
- [ ] `git status --short --branch` and `git diff HEAD` confirm the candidate
      stayed unchanged throughout the gate sequence.

## Exit

- [ ] Update the implementation roadmap to PV complete and pushed.
- [ ] Delete `docs/plans/phase-pv/`, including `design.md`; Git history retains
      them.
- [ ] Inspect `git diff --cached --name-only`, run the per-commit gitleaks
      check, create the explicit-path exit commit, and push `main`.
