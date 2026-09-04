# PV T00 — authorities and records

## Contract

Make the identity an accepted authority before any code changes, and give the
phase its acceptance rows.

1. Write `docs/adr/0030-stamped-document-visual-identity.md`, status Accepted,
   amending ADR 0029's sentence "Design tokens keep the existing zinc and
   emerald values" and the web.md paragraph "The chrome is neutral: the zinc
   palette, Inter, and emerald only for saved and active states." Record the
   decision as the token table, type, seal, state-mark, and motion rules in
   [`design.md`](design.md) sections 2 and 5, with the rejected alternatives:
   keeping the zinc default, the assigned "sheet and label" direction, and the
   category-standard landing. Note that ADR 0029 lands on `main` with T01 and
   0030 is accepted alongside it.
2. Amend `docs/design/web.md`: after the T01 rebase brings the "Application UI"
   section, replace its neutral-chrome paragraph with a four-sentence summary of
   the stamped-document identity and a pointer to ADR 0030. Because T01 has not
   run yet, keep this edit as a patch file `docs/plans/phase-pv/web-md.patch`
   the owner applies in T01's records commit.
3. Amend `docs/design/product.md` "Landing and entry": replace "It is static
   server-rendered text with no data fetch" with "It is static server-rendered
   content with no data fetch; it renders one compiled-in sample resume through
   the shared renderer". Note the correction in `docs/design/decisions.md`.
4. Add `docs/design/decisions.md` rows for 0029 and 0030.
5. Create `docs/plans/traceability/ac-ui.md` rows `AC-UI-007` through
   `AC-UI-013` as `PLANNED` (001–006 arrive with T01 as `LANDED` until T10
   re-proves them) and add the `AC-UI` prefix to the matrix index:

   | ID        | Statement                                                                                                                                                                                                      | Phase/task |
   | --------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------- |
   | AC-UI-007 | Tokens match the design table; `--seal` is consumed only by the seal, the state mark, and the Publish variant; the chrome font is Be Vietnam Pro; the preview sheet keeps its own background in the dark theme | PV T02     |
   | AC-UI-008 | The landing renders the compiled-in sample resume and its seal at server render with no data fetch and the unchanged base CSP; signed-in visitors see "Open your resumes"                                      | PV T03     |
   | AC-UI-009 | The resume list shows each resume's publish state and canonical link from `ResumeSummary` only, up to three sheet slots, with Rename and Delete in an overflow menu                                            | PV T05     |
   | AC-UI-010 | Settings describes devices as browser and OS with an injected-clock relative time, and the theme cookie is honored at server render on every `/app/**` page                                                    | PV T06     |
   | AC-UI-011 | The editor top bar has exactly one `--seal` control; at 390 px the preview sheet is whole and the edit view is unclipped behind a fixed bottom switch                                                          | PV T07     |
   | AC-UI-012 | Publish success shows the stamp with the canonical link and Copy link; the title mark appears in one press and instantly under reduced motion                                                                  | PV T09     |
   | AC-UI-013 | The Impeccable detector, finish review (`ship`), and documenter (`DESIGN.md`) ran on the candidate; captures exist for desktop and mobile                                                                      | PV T10     |

6. Add the PV row to `docs/plans/implementation-plan.md` State and Delivery
   order (before P6) and bump its revision.
7. Add `.impeccable/review/`, `.impeccable/questions/`, `.impeccable/build/`,
   and `.impeccable/mocks/` to `.git/info/exclude`. `PRODUCT.md`,
   `.impeccable/config.json`, and `.impeccable/surfaces/` are tracked design
   records; commit them with this task.

## Ownership and checks

Owned paths: the files named above.

Run:

```sh
make docs-fmt
npx markdownlint-cli2 docs/adr/0030-stamped-document-visual-identity.md \
  docs/design/product.md docs/design/decisions.md \
  docs/plans/traceability/ac-ui.md docs/plans/traceability/README.md \
  docs/plans/implementation-plan.md docs/plans/phase-pv/*.md PRODUCT.md
npx prettier --check PRODUCT.md .impeccable/surfaces/*.md
```

Commit with explicit paths. Do not touch `apps/`.

Formatting note: `.impeccable/surfaces/*.md` passes markdownlint as written, but
`make docs-fmt` would rewrap the one-line YAML `related_targets` array the brief
script emits. In step 7, add `.impeccable/surfaces/` to the Prettier ignore list
used by `docs-fmt`, or reformat and confirm `surface-brief.mjs read` still
parses it.
