# Documentation rules for agents

Read this file when selecting documents, writing agent files, or handling plans and evidence. Human writing rules, documentation ownership, and ADR policy live in the [engineering standard](../docs/standards/engineering.md).

## Document selection

Use the [documentation ownership table](../docs/standards/engineering.md#documentation-ownership) to choose the artifact. Read the [authority map](../AGENTS.md#map) before resolving conflicting documents.

## Agent-file formatting

Files only agents read (`AGENTS.md`, `CLAUDE.md`, `instructions/`, `.claude/`, `.codex/`, `docs/plans/`) stay minified: one line per paragraph or list item, tables without padding. Prettier and markdownlint skip them. Markdown that people read keeps Prettier's 80-column wrap.

## Plans and records

Plans describe work that remains. When a phase exits, delete its plan directory; git history keeps it. The traceability rows it proved, the architecture narrative, and the code record what it built. Never rewrite a proven traceability row's evidence to match later behavior; change its state and cite the new evidence.

Use exact states: `planned`, `in progress`, `landed`, `verified`, `accepted`, or `blocked`. `Landed` means present in the repository. It does not imply that review, acceptance, or design approval passed.

## Evidence

Reports go to the scratchpad outside the repository. Findings not fixed at once become lines in `docs/plans/backlog.md`.

Save assigned persona evidence under `.dev/personas/`. Persona work never edits product files. Report each defect with steps, URL, expected and actual behavior, severity, screenshot or other evidence, and owning lane.
