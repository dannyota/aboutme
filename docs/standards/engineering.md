# Engineering documentation and comments

Write each fact once, in the artifact that owns it, in short plain words. Plans
say what to do next and are deleted when the work ends; design docs and ADRs say
how the system is and why. Code, tests, and living docs must stand on their own
after every plan is gone. Do not use em dashes.

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

## Naming

Apply these conventions to new files and files touched by substantive work. Keep
stable workflow paths and job IDs. Use focused contract assertions rather than a
general naming linter. Keep unrelated check names unique; use static text and
allowlisted matrix identifiers. Keep private data out of names.

| Workflow item      | Convention                                                                        |
| ------------------ | --------------------------------------------------------------------------------- |
| Filename           | Lowercase kebab case, purpose-based, `.yml`                                       |
| Workflow name      | Short sentence case; keep acronyms such as `CI`                                   |
| Job ID             | Stable lowercase kebab case, unique within its workflow                           |
| Job name           | Explicit, concise scope followed by purpose                                       |
| Required aggregate | Exact ID and effective check name `ci`; no matrix or other producer               |
| Matrix keys        | Describe dimensions, such as `arch`, `shard`, `group`; keep the image `name` key  |
| Matrix values      | Stable lowercase identifiers, such as `amd64`, `preview-gap`                      |
| Matrix job name    | Include only independent identifying dimensions, without runner or proof payloads |
| Step name          | Capitalized verb plus object, sentence case                                       |
| Referenced step ID | Stable lowercase kebab case; contracts and outputs select IDs                     |
| Reusable workflow  | Same filename rule, directly in `.github/workflows`; use `workflow_call`          |
| Composite action   | Lowercase capability directory, `action.yml`, imperative display name             |

| Test item                      | Convention                                                                                      |
| ------------------------------ | ----------------------------------------------------------------------------------------------- |
| Go file                        | `<subject>[_<scenario>]_test.go`, lowercase snake case; keep build and platform constraints     |
| Go function                    | `TestSubject_Behavior`, meaningful subtests; keep `Benchmark`, `Fuzz`, `Example` forms          |
| Vitest                         | `<subject>.test.ts`, kebab case; source-specific tests may mirror identifiers such as `useAuth` |
| Vitest qualifier               | Use only for distinct suites or generated output, such as `.contract` or `.generated`           |
| Playwright E2E and HTTPS proof | `<feature>[-<scenario>].spec.ts` in the existing test directory                                 |
| Shell contract                 | `<subject>[-<scenario>]-test.sh`                                                                |
| Node                           | `<subject>[-<scenario>].test.mjs`                                                               |
| Python                         | `test_<subject>[_<scenario>].py`, valid Python identifiers; keep explicit runner commands       |
| Helper                         | Name its role without a runnable-test suffix; keep established config and generator names       |
| Test title                     | State behavior and condition, such as `rejects a stale revision`                                |

Preserve discovery rules, explicit selections, Go `-run` and `-skip` consumers,
and standalone harness arguments when renaming tests. Update callers together; a
filename change must preserve selected test paths and execution coverage.

## Code comments

A comment explains a constraint that the code cannot express clearly. Useful
examples include a security invariant, a non-obvious failure boundary, an
external protocol requirement, or why a tempting simplification is unsafe.

Keep a comment beside the smallest unit it governs. Link to an ADR or design
section when the reasoning is longer than a short paragraph.

Do not leave these in code, tests, or living docs:

- citations of plans, phases, tasks, or review findings, with or without IDs;
- plan, phase, task, slice, review-round, finding, or probe IDs (for example
  "Task 11", "P8-priv", "phase 10", "finding B2"), reviewer names, or report
  filenames; cite the design doc, ADR, or `AC-*` ID, or state the rule;
- history: how the code evolved, or words such as "now", "no longer", and "used
  to";
- copied design sections or acceptance criteria;
- claims about deleted files or retired tools;
- line-by-line descriptions that repeat the code.

Generated files and migrations frozen by root `.v1-baseline` are exceptions.
Change generated sources or leave frozen migrations intact; do not hand-edit
either to improve prose.

## Keeping text current

When a change makes a statement false, fix or delete it in the same change. When
your work touches a doc or comment that breaks these rules, rewrite it as part
of that work; do not start repository-wide sweeps. Cite files, commands, and
uncertainty, and claim only checks that ran. Reviews check these rules on the
files a change touches.

ADRs are one record per topic. To change a decision in force, edit its topic
record with the owner's approval and add a line to its History section naming
what changed and why; open a new ADR only for a new topic.

## Plans and records

Agent procedures for plans, states, and evidence live in
[documentation instructions](../../instructions/documentation.md#plans-and-records).

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
