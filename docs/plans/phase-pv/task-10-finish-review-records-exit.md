# PV T10 — finish review, browser proofs, records, exit

## Contract

Integration owner, alone, on one candidate commit after W4 is accepted.

### Code-led finish

The planned Impeccable CLI, detector scripts, shipped reviewer/documenter, and
`document.md` reference are not present in the repository or installed toolset.
Use the checked-in direction contract and repository gates directly instead of
recording commands that cannot run.

1. Reload the direction contract from
   `.impeccable/surfaces/apps-web-app-pages-index-vue.md`.
2. With `make dev-native` up and the dev account seeded, capture into
   `.impeccable/review/` after motion settles: `desktop.png` (1440 wide, full
   page) and `mobile.png` (390 wide) for `/`, plus `list-desktop.png`,
   `list-mobile.png`, `settings-desktop.png`, `editor-desktop.png`,
   `editor-mobile.png`, in light; and `desktop-dark.png`, `editor-dark.png`.
   Open each file once and confirm it shows what its name says.
3. Run the repository's UI boundary, lint, type, unit, build, and browser gates.
   Fix mechanical findings and list any remaining visual finding for the
   reviewer.
4. Spawn a fresh Sol reviewer that authored none of the phase with: the original
   request ("the UI is not good; design and plan"), the confirmed answers
   (Vietnamese tech job seekers; "the resume is public, you are not"; rebase
   PU), the artifact paths (`apps/web/app/pages/index.vue`, `EditorShell.vue`,
   `ResumeList.vue`, `sessions.vue`, `PublishDialog.vue`), the screenshot paths,
   the direction contract, the repository gate results, the checked-in design
   authorities, and no comp. Act on the disposition: `recapture`, `fix`,
   `rebuild`, or `ship`. Two rounds is the budget; list any open finding in the
   exit report for the owner.
5. Write `DESIGN.md` from the built pages, `PRODUCT.md`, the direction contract,
   and the checked-in design authorities. Recheck it after any visual fix.

### Browser proofs and hooks

Update the HTTPS specs for the hook changes each task listed (entry button
order, password toggle label, list menu, page-count mark, customization labels,
Copy link) and run:

```sh
make web-build
make dev-https-auth-check dev-https-editor-check dev-https-mcp-check \
  dev-https-entry-check dev-https-public-check dev-https-publish-check
```

Add the axe scan of `/`, `/login`, `/app/resumes`, `/app/settings/sessions`, and
the editor in both themes to `entry.spec.ts` and `editor.spec.ts` where the
existing scans live.

### Records

- `docs/architecture.md`: describe the chrome by component under "Implemented
  authenticated editor" and a new "Implemented application chrome" paragraph:
  toolkit layers, tokens, the seal and state marks, the landing sample render,
  the narrow layout. No phase IDs in prose.
- `apps/web/README.md`: the same by component, plus the Impeccable records
  (`PRODUCT.md`, `DESIGN.md`, `.impeccable/surfaces/`).
- `docs/plans/traceability/ac-ui.md`: `AC-UI-001` through `013` to `PROVEN` with
  exact test and proof references.
- `docs/runbooks/local-uat.md`: update any screenshot or string the runbook
  names.

### Phase review and exit

Hand the integrated diff to a fresh non-author reviewer with the invariant list
in [`exit-criteria.md`](exit-criteria.md). Findings return to the owning author;
the same reviewer confirms fixes. Then run the exit checklist, `make ci` alone,
and connected `make scan` on the unchanged candidate, delete this directory,
commit with explicit paths, and push.

## Ownership and checks

Owned paths: `.impeccable/review/**` (local), `DESIGN.md`, `apps/web/e2e/**` and
`scripts/test/**` HTTPS specs, the records above.

Acceptance: `AC-UI-013`, and every PV row to `PROVEN`.

Report: the candidate SHA, each command's outcome, the reviewer disposition
history, open findings with the owner's decision, and the pushed SHA when the
owner authorizes the push.
