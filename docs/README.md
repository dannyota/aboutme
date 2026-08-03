# aboutme documentation

Map of `docs/` — what lives where, and the rules for each kind of document.

| Directory                               | Contents                                                                                                                                                           | Mutability                                                   |
| --------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------ |
| [`specs/`](specs/)                      | Design specs (`<topic>-design.md`). A spec is a decision record of _what we intend to build_ and why; approval dates live inside the doc.                          | Frozen once approved; corrections go in a new spec or an ADR |
| [`plans/`](plans/)                      | Implementation plans: master `implementation-plan.md` (phases, gates, agent workflow) + per-phase task plans (`phase-N-<name>.md`).                                | Living until executed; checkbox tasks tracked during run     |
| [`adr/`](adr/)                          | Architecture Decision Records, numbered `NNNN-<slug>.md`. One decision per file: context → decision → consequences.                                                | Immutable; superseding requires a new ADR that links back    |
| [`api/`](api/)                          | `openapi.yaml` — the `/api/v1` contract, lint, and conformance tests. Generated TypeScript client tooling is a queued pre-P2B correction; Dart is deferred to P11. | Living; changes reviewed like code                           |
| [`runbooks/`](runbooks/)                | Operational procedures: deploy, rollback, restore drill, EIP recovery, secret rotation.                                                                            | Living                                                       |
| `architecture.md` _(added at scaffold)_ | Current-state system overview reconciled with code, deployment configuration, and OpenAPI.                                                                         | Living                                                       |
| `self-hosting.md` _(added at scaffold)_ | Operator guide for self-hosters (podman compose); linked from the root README.                                                                                     | Living                                                       |

Root-level (not in `docs/`, added at scaffold): `README.md`, `CONTRIBUTING.md`,
`SECURITY.md`, `LICENSE`.

## Conventions

- **Mermaid** (not ASCII) for diagrams in all `.md` files.
- Formatting/linting: `make docs-fmt` / `make docs-lint` (Prettier +
  markdownlint-cli2 — configs at repo root); CI enforces both.
- ADR template: `## Context` / `## Decision` / `## Consequences`; status line
  (`Accepted | Superseded by NNNN`) at the top.
- Authority hierarchy: the design spec owns intended product/architecture and
  wins over implementation plans; code, deployment configuration, and OpenAPI
  jointly own current behavior; `architecture.md` and runbooks own current
  narrative/operations; accepted ADRs explain or supersede decisions. A
  disagreement is a defect to repair across the affected authorities, not a
  reason to choose the convenient artifact.

## Current documents

- [Current-state architecture](architecture.md) — what is implemented on `main`
  now.
- [aboutme design](specs/aboutme-design.md) — intended v1 product and
  architecture (`DRAFT v3`; two independent review rounds applied).
- [Numbered implementation roadmap](plans/implementation-plan.md#numbered-delivery-index)
  — done/current/next delivery waves and phase gates.
- [Phase 2A automated acceptance catalog](plans/uat-phase-2a.md) — immutable
  fail-closed criteria for the current data-layer phase gate; the historical
  filename retains the older `UAT` terminology.
- [Phase 9 local manual UAT plan](plans/phase-9-local-uat.md) — main-session
  user validation of the complete Podman deployment through Playwright MCP
  before AWS authorization.
- [OpenAPI contract](api/openapi.yaml) — current health, auth, identity, and
  session HTTP surface.
- [Self-hosting guide](self-hosting.md) — the runnable podman compose stack.

`UAT` is reserved for the P9 local and P9A staging user-workflow gates. Earlier
`uat-phase-*` catalogs and reports are immutable automated phase-acceptance
history; their filenames, criteria, corrections, and verdicts are not rewritten.
