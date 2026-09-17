# Engineering documentation and comments

Write each fact once, in the artifact that owns it, in short plain words. Plans
say what to do next and are deleted when the work ends; design docs and ADRs say
how the system is and why. Code, tests, and living docs must stand on their own
after every plan is gone.

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

Keep a comment beside the smallest unit it governs. Link to an ADR or design
section when the reasoning is longer than a short paragraph.

Do not leave these in code, tests, or living docs:

- plan, phase, task, slice, review-round, finding, or probe IDs (for example
  "Task 11", "P8-priv", "phase 10", "finding B2"), reviewer names, or report
  filenames; cite the design doc, ADR, or `AC-*` ID, or state the rule;
- history: how the code evolved, or words such as "now", "no longer", and "used
  to";
- copied design sections or acceptance criteria;
- claims about deleted files or retired tools;
- line-by-line descriptions that repeat the code.

Generated files and migrations frozen by `apps/server/migrations/.uat-baseline`
are exceptions. Change generated sources or leave frozen migrations intact; do
not hand-edit either to improve prose.

## Keeping text current

When a change makes a statement false, fix or delete it in the same change. When
your work touches a doc or comment that breaks these rules, rewrite it as part
of that work; do not start repository-wide sweeps. Cite files, commands, and
uncertainty, and claim only checks that ran. Reviews check these rules on the
files a change touches.

ADRs are decision records. Edit an accepted ADR only for wording, links, plan
IDs, and its status line; supersede a decision with a new ADR.

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

Use relative links inside `docs/` and Mermaid for diagrams.

`scripts/check-lengths.sh` enforces two limits in CI, `make check`, and the
pre-commit hook:

- Markdown files: at most 450 lines. Split a larger document into a directory
  with a `README.md` index and focused pages, or trim it.
- Non-test code (Go, TypeScript, Vue, JavaScript, shell): at most 700 lines.
  Split by topic into sibling files; Go stays in the same package.

Generated files and tests are exempt. Keep functions short enough to read in one
screen; split long ones into helpers.
