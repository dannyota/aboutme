# aboutme documentation

Map of `docs/` — what lives where, and the rules for each kind of document.

| Directory                               | Contents                                                                                                                                  | Mutability                                                   |
| --------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------ |
| [`specs/`](specs/)                      | Design specs (`<topic>-design.md`). A spec is a decision record of _what we intend to build_ and why; approval dates live inside the doc. | Frozen once approved; corrections go in a new spec or an ADR |
| [`plans/`](plans/)                      | Implementation plans: master `implementation-plan.md` (phases, gates, agent workflow) + per-phase task plans (`phase-N-<name>.md`).       | Living until executed; checkbox tasks tracked during run     |
| [`adr/`](adr/)                          | Architecture Decision Records, numbered `NNNN-<slug>.md`. One decision per file: context → decision → consequences.                       | Immutable; superseding requires a new ADR that links back    |
| [`api/`](api/)                          | `openapi.yaml` — the `/api/v1` contract. TS/Dart clients are generated from it.                                                           | Living; changes reviewed like code                           |
| [`runbooks/`](runbooks/)                | Operational procedures: deploy, rollback, restore drill, EIP recovery, secret rotation.                                                   | Living                                                       |
| `architecture.md` _(added at scaffold)_ | Current-state system overview distilled from the approved specs.                                                                          | Living                                                       |
| `self-hosting.md` _(added at scaffold)_ | Operator guide for self-hosters (podman compose); linked from the root README.                                                            | Living                                                       |

Root-level (not in `docs/`, added at scaffold): `README.md`, `CONTRIBUTING.md`,
`SECURITY.md`, `LICENSE`.

## Conventions

- **Mermaid** (not ASCII) for diagrams in all `.md` files.
- Formatting/linting: `make docs-fmt` / `make docs-lint` (Prettier +
  markdownlint-cli2 — configs at repo root); CI enforces both.
- ADR template: `## Context` / `## Decision` / `## Consequences`; status line
  (`Accepted | Superseded by NNNN`) at the top.
- Authority hierarchy: for current behavior, **code, deployment configuration,
  and `docs/api/openapi.yaml` are authoritative**; among narrative documents,
  `architecture.md` and runbooks supersede frozen specs. Any disagreement
  requires a gap issue (a stale runbook is a defect).

## Current documents

- [aboutme design](specs/aboutme-design.md) — v1 product + architecture spec
  (two independent review rounds applied).
