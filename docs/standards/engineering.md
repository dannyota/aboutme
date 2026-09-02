# Engineering documentation and comments

Write each fact in the artifact that owns it. This keeps source files readable
and prevents an old task narrative from becoming an accidental contract.

## Documentation ownership

| Information                                    | Owner                                      |
| ---------------------------------------------- | ------------------------------------------ |
| Intended product or architecture               | `docs/design/`                             |
| Numeric limits and benchmark protocol          | `docs/design/budgets.md`                   |
| One proposed or accepted choice and trade-offs | `docs/adr/`                                |
| Current implemented system                     | Code, configuration, OpenAPI, architecture |
| Work order, gates, and delivery state          | `docs/plans/`                              |
| Operational procedure                          | `docs/runbooks/`                           |
| Stable contributor guidance                    | Repository or component `README.md`        |
| Acceptance ownership and evidence              | `docs/plans/traceability/`                 |

Do not repeat a detailed contract in several places. State the rule once, then
link to it from artifacts that need context.

## Code comments

A comment explains a constraint that the code cannot express clearly. Useful
examples include a security invariant, a non-obvious failure boundary, an
external protocol requirement, or why a tempting simplification is unsafe.

Keep a comment beside the smallest unit it governs. Name the effect before the
history. Link to an ADR or design section when the reasoning is longer than a
short paragraph.

Do not leave these in live source:

- task numbers, review rounds, reviewer names, or report filenames;
- a narrative of how the implementation evolved;
- copied design sections or acceptance criteria;
- claims about deleted files or retired tools;
- line-by-line descriptions that repeat the code.

Generated files and UAT-baselined migrations are exceptions. Change generated
sources or leave immutable migration history intact; do not hand-edit either to
improve prose. Before the first UAT baseline, the migration correction rule in
the data design applies.

## Plans and records

Plans describe work that remains. When a phase exits, delete its plan directory;
git history keeps it. The traceability rows it proved, the architecture
narrative, and the code record what it built. Never rewrite a proven
traceability row's evidence to match later behavior; change its state and cite
the new evidence.

Use exact states: `planned`, `in progress`, `landed`, `verified`, `accepted`, or
`blocked`. `Landed` means present in the repository. It does not imply that
review, acceptance, or design approval passed.

## Links and size

Use relative links inside `docs/`. Keep Markdown files near 300 lines. Split a
larger living document into a directory with a `README.md` index and focused
topic pages. Do not split immutable history only to meet the guideline.
