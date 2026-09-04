# AC-UI traceability rows

Rows `AC-UI-001` through `AC-UI-006` (toolkit, three layers, field commit model,
preview without photo, accessibility and theme, test hooks) arrive with the
phase PU rebase in PV T01 as `LANDED` and are re-proven in PV T10. The rows
below own the visual identity; they become `PROVEN` only in the PV record commit
after the gates in `docs/plans/phase-pv/exit-criteria.md`.

| ID        | Design clause                              | Statement                                                                                                                                                                                                        | Phase/task | State   | Test / acceptance reference |
| --------- | ------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------- | ------- | --------------------------- |
| AC-UI-007 | Application UI; ADR 0030 tokens            | Tokens match the ADR 0030 table; `--seal` is consumed only by the seal, the state mark, and the Publish variant; the chrome font is Be Vietnam Pro; the preview sheet keeps its own background in the dark theme | PV T02     | PLANNED | Pending                     |
| AC-UI-008 | Landing and entry; ADR 0030 landing        | The landing renders the compiled-in sample resume and its seal at server render with no data fetch and the unchanged base CSP; signed-in visitors see "Open your resumes"                                        | PV T03     | PLANNED | Pending                     |
| AC-UI-009 | Application UI; publish controls           | The resume list shows each resume's publish state and canonical link from `ResumeSummary` only, up to three sheet slots, with Rename and Delete in an overflow menu                                              | PV T05     | PLANNED | Pending                     |
| AC-UI-010 | Application UI; sessions                   | Settings describes devices as browser and OS with an injected-clock relative time, and the theme cookie is honored at server render on every `/app/**` page                                                      | PV T06     | PLANNED | Pending                     |
| AC-UI-011 | Application UI; ADR 0030 module and motion | The editor top bar has exactly one `--seal` control; at 390 px the preview sheet is whole and the edit view is unclipped behind a fixed bottom switch                                                            | PV T07     | PLANNED | Pending                     |
| AC-UI-012 | Publish controls; ADR 0030 motion          | Publish success shows the stamp with the canonical link and Copy link; the title mark and preview-sheet stamp appear in one press and instantly under reduced motion                                             | PV T09     | PLANNED | Pending                     |
| AC-UI-013 | Application UI; delivery gates             | The Impeccable detector, finish review (`ship`), and documenter (`DESIGN.md`) ran on the candidate; captures exist for desktop and mobile                                                                        | PV T10     | PLANNED | Pending                     |
