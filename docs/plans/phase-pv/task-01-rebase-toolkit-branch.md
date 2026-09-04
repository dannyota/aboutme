# PV T01 — rebase phase PU onto main

## Contract

Bring the 46 commits of `codex/phase-pu` (`0187aff..a4c9d21`) onto current
`main` as one reviewed sequence, then give the result the fresh review PU never
had on `main`. Integration owner only; no worker touches Git.

1. From `main` at the candidate SHA, create branch `pv/rebase-pu` and run
   `git rebase --onto main cad75aa codex/phase-pu` in a fresh worktree
   (`.worktrees/pv-rebase`), never in the main tree. Verify the base:
   `git merge-base --is-ancestor main HEAD`.
2. Resolve conflicts in favor of PU's structure and `main`'s behavior. Expected
   conflict files and the rule for each:
   - `apps/web/app/components/editor/EditorShell.vue`: PU's shell markup; keep
     `main`'s `PublishDialog` mount, `publishOpen` state, and the
     `data-action="publish"` trigger.
   - `apps/web/app/components/editor/PublishDialog.vue`: rebuild `main`'s dialog
     on PU's `FormDialog` composite. Keep every P5B behavior: slug grammar,
     dependent switches, disclosures, save-first, reauth (password and
     provider), fixed errors, issue focus, one in-flight request, canonical
     link. Replace checkboxes with `SwitchField` and inputs with `TextField`
     (commit on change for the slug: this dialog submits explicitly, so the
     field is a controlled input, not an intent field).
   - `apps/web/app/assets/css/editor.css`: PU deletes it; drop `main`'s
     publish-dialog rules with it.
   - `apps/web/test/editor/editor-shell.test.ts`,
     `test/editor/publish-dialog.test.ts`: keep every assertion; requery by
     role, label, `data-testid`, and `data-action`; dialogs teleport, so mount
     with `attachTo: document.body`.
   - `scripts/web-e2e-source.manifest`, `apps/web/package.json`,
     `package-lock.json`: take PU's toolkit entries plus `main`'s post-`cad75aa`
     changes; regenerate the manifest with
     `bash scripts/generate-web-e2e-source-manifest.sh`.
   - `docs/plans/implementation-plan.md`, `docs/plans/traceability/README.md`:
     keep `main`; T00 already recorded PV.
   - `docs/plans/traceability/ac-ui.md`: PU brings rows 001–006 and T00 wrote
     007–013; keep both in one table. `docs/design/decisions.md`: T00 already
     lists 0029 and 0030; keep `main`.
   - `docs/adr/0029-application-ui-toolkit.md` and `docs/design/web.md`: take
     PU's additions, then apply `docs/plans/phase-pv/web-md.patch`.
   - PU's deleted `docs/plans/phase-pu/` stays deleted.

3. Re-run PU's own proofs on the rebased tree: the T01 stylesheet test, the
   boundary scan, and the browser proofs listed under checks. The renderer
   suites must pass without a snapshot update.
4. Hand the integrated diff (`git diff main...pv/rebase-pu`) to a fresh reviewer
   who authored none of PU or P5B. The reviewer confirms by name: renderer
   isolation, dialog focus invariants, the field commit rule, hostile-text
   rendering, test-hook retention, theme and CSP behavior, and that the publish
   dialog keeps every P5B case. Findings return to the owner; the same reviewer
   confirms fixes.
5. Fast-forward `main` to the reviewed branch with explicit-path commits
   preserved (no squash), then delete the worktree.

## Ownership and checks

Owned paths: everything the rebase touches; shared files are the owner's.

Run in the worktree:

```sh
cd apps/web && npm ci
make web-lint web-typecheck web-test web-build
cd apps/web && npx vitest run test/renderer
make web-e2e
make dev-https-auth-check dev-https-editor-check dev-https-entry-check \
  dev-https-publish-check dev-https-mcp-check dev-https-public-check
```

Report: conflict files and resolutions, every `main`-only commit accounted for
(`git log --oneline cad75aa..main -- apps docs` each mapped to a surviving
change), the reviewer's named confirmations, and the SHA `main` fast-forwarded
to. `make ci` runs alone after the fast-forward.
