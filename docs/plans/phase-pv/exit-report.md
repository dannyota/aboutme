# PV exit report

Phase PV is locally accepted at candidate
`c8b9c42e2bc6290ab415ca1081c66310d4e6d93c`. Push and the destructive plan
cleanup remain pending owner authorization.

## Candidate gates

All successful gates below ran against the unchanged candidate.

| Gate                                                                                                                                                                                           | Outcome                                                                                                                                |
| ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| `cd apps/web && npx vitest run test/renderer`                                                                                                                                                  | PASS: 20 files, 232 tests                                                                                                              |
| `WEB_E2E_RUN_ID=pv_c8b9c42_gate1 make web-e2e`                                                                                                                                                 | PASS: 64 renderer/browser tests and 1 normal-CSP test                                                                                  |
| `make web-lint web-typecheck web-test web-build`                                                                                                                                               | PASS: lint and typecheck clean; 112 files, 1,515 tests passed, 2 skipped; production build and font-license byte check passed          |
| `make dev-https-auth-check dev-https-editor-check dev-https-mcp-check dev-https-entry-check dev-https-public-check dev-https-publish-check dev-https-password-check dev-https-transport-check` | PASS: all eight scripted browser proofs                                                                                                |
| `make docs-fmt`                                                                                                                                                                                | PASS: Prettier and markdownlint changed no file                                                                                        |
| `make ci`                                                                                                                                                                                      | PASS: all local CI stages                                                                                                              |
| Connected `make scan`                                                                                                                                                                          | PASS: 138,365 Semgrep rules, 750 tracked targets, 37 non-blocking findings, 0 blocking findings; gitleaks found no leak in 562 commits |
| `git status --short --branch` and `git diff HEAD --exit-code`                                                                                                                                  | PASS: clean tree at the candidate SHA                                                                                                  |

The shared `aboutme-test-db` container stayed running with its 512 MB memory
cap. The native HTTP and HTTPS application stacks were stopped after their
proofs.

## Exit evidence

- `make web-e2e` proved the committed renderer and print baselines remained
  byte-identical. The renderer-boundary diff against `origin/main` contains only
  `apps/web/app/components/resume/measure.ts`, the reviewed T01 rebase change.
- The passing `apps/web/test/ui/toolkit.test.ts` cases reject Tailwind preflight
  and require the application and renderer-exclusion guards.
- `AC-UI-001` through `AC-UI-013` are `PROVEN` in
  `docs/plans/traceability/ac-ui.md`.
- The legacy color-token grep returned no match. Direct `--seal` references are
  limited to the definition and mapping in `theme.css` and consumption in
  `AppSeal.vue`. `StateMark.vue` and the accepted Publish state compose
  `AppSeal`; publish buttons consume the mapped colors through the Button `seal`
  variant.
- A fresh request to `http://localhost:20080/` contained the server-rendered Ada
  Lovelace sample and its seal label. The Go server log did not grow during that
  request, proving no `/api/v1` call served the landing response.
- The HTTPS proofs covered theme persistence, narrow editor Edit and Preview
  views, publish and unpublish marks, reduced motion, stable hooks, hostile
  text, CSP behavior, and axe scans in both themes.
- `DESIGN.md`, ADR 0030, the amended design documents, architecture narrative,
  web README, traceability records, and local UAT runbook match the built UI.
- `make ci`, connected `make scan`, and the phase review found no unresolved
  generated-file, unrelated-change, secret, credential, or personal-data issue.
  `scripts/web-e2e-source.manifest` was regenerated by its repository target,
  not edited by hand.

The nine ignored review captures exist under `.impeccable/review/` and were
opened after their final regeneration:

- `desktop.png`:
  `1ecdbe7bb4a046748c30bc4cc5fd6ea9ddef85954730bde8f814cca2fdaa7f5a`
- `mobile.png`:
  `639494409645fe8da07cd74768ad1d76d568b75cf0a9a53dc7cd03810f3cfcdc`
- `desktop-dark.png`:
  `347f7c58420024fe544a564d849b5eab790ccae6b47125ee756a9acdd962cb84`
- `list-desktop.png`:
  `fd11f878b2d06f730852e579fde75ff964ca19c6841e2ae9353fde46ccc84597`
- `list-mobile.png`:
  `35d56c77984b4c62faeb40025ee09e6328c7e3d56efb322fbe2e787f86b75d21`
- `settings-desktop.png`:
  `478e3d4bec65b5b3377ab6bd9cb0e58977ef95f5620795b3abb67740cb353600`
- `editor-desktop.png`:
  `22cd0e14d586e1caa7f6ee763422334ca2076624cda2ee4233202f6771df5f7b`
- `editor-mobile.png`:
  `5887afcf06984688042e16621d751a809f025a40e59463eacaa21d7b5b7b0fb4`
- `editor-dark.png`:
  `db59f5342aeec0476fc17d25860c36561b30915a43b5a1374ae3058823c8c8bb`

## Review disposition

The fresh Sol phase reviewer first returned `fix` for five findings: raw-link
affordances, a draft overlay blocking its sheet link, the missing landing root
hook, whitespace in the recorded patch, and an inconsistent mobile editor
capture. After those fixes, the reviewer found that the hidden mobile rail still
rendered visible icons. The opacity barrier and regenerated capture resolved
that defect. The reviewer then returned `ship`.

The candidate browser gate later exposed a stale closed-source manifest. The
dedicated manifest check reproduced the failure, the generated snapshot was
updated, and `web-e2e` passed. The same Sol reviewer inspected that final diff
and returned `ship` again. The final record review then returned `fix` for an
incomplete description of the seal token boundary; after the wording was
corrected, the same reviewer returned `ship`. There are no open phase-review
findings.

## Corrected criterion

The original `--seal` criterion named `StateMark.vue` and the Publish control as
direct token consumers while omitting `theme.css`. The implementation and design
use one token definition in `theme.css` and one direct consumer in
`AppSeal.vue`. `StateMark.vue` and the accepted Publish state compose `AppSeal`,
while publish buttons use the mapped colors through the Button `seal` variant.
`exit-criteria.md` now states that testable boundary.

## Pending owner action

No push was authorized. After authorization, update the implementation roadmap
to PV complete and pushed, delete `docs/plans/phase-pv/`, create the
explicit-path exit commit with its gitleaks check, and push `main`. Record the
pushed SHA in Git history through that commit.
