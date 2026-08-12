# Documentation

This directory separates intended design, current behavior, delivery work, and
operational procedures. Put each fact in the document type that owns it, then
link to that source instead of copying the contract.

## Authority

| Question                                          | Authority                                                                |
| ------------------------------------------------- | ------------------------------------------------------------------------ |
| What should the product and system become?        | [`design/`](design/README.md)                                            |
| What does the HTTP API implement now?             | [`api/openapi.yaml`](api/openapi.yaml)                                   |
| What does the repository implement now?           | Code, deployment configuration, and [`architecture.md`](architecture.md) |
| Why was an architecture choice accepted?          | [`adr/`](adr/README.md)                                                  |
| What runs next, and which gates apply?            | [`plans/`](plans/README.md)                                              |
| Which acceptance criterion owns a requirement?    | [`plans/traceability/`](plans/traceability/README.md)                    |
| How is an implemented operation performed?        | [`runbooks/`](runbooks/README.md)                                        |
| How does a contributor or operator get started?   | [`guides/`](guides/README.md)                                            |
| How should documentation and comments be written? | [`standards/`](standards/README.md)                                      |

The design wins over a plan. Code, deployment configuration, and OpenAPI must
agree about implemented behavior. A disagreement between those artifacts is a
defect, not permission to choose one silently.

## Directory map

| Path                                     | Contents                                                    | Lifecycle                                                |
| ---------------------------------------- | ----------------------------------------------------------- | -------------------------------------------------------- |
| [`design/`](design/README.md)            | Intended product, architecture, and template contract       | Draft until approved; frozen by revision after approval  |
| [`adr/`](adr/README.md)                  | One proposed or accepted decision per record                | Draft until accepted; accepted records are append-only   |
| [`api/`](api/README.md)                  | OpenAPI source, contract tests, and client-generation notes | Living with implemented HTTP behavior                    |
| [`plans/`](plans/README.md)              | Roadmap, active phase plans, budgets, gates, and evidence   | Active plans are living; completed records are immutable |
| [`research/`](research/flowcv/README.md) | External observations used as evidence                      | Not project authority                                    |
| [`guides/`](guides/README.md)            | Setup and usage guidance                                    | Living with supported workflows                          |
| [`runbooks/`](runbooks/README.md)        | Exact operating, verification, and recovery procedures      | Added only when the operation exists                     |
| [`standards/`](standards/README.md)      | Documentation and code-comment rules                        | Living repository policy                                 |
| [`specs/`](specs/aboutme-design.md)      | Compatibility links for the retired layout                  | No new documents                                         |

Root [`README.md`](../README.md), [`CONTRIBUTING.md`](../CONTRIBUTING.md),
[`SECURITY.md`](../SECURITY.md), and [`LICENSE`](../LICENSE) cover repository
entry points and project policy.

## Current entry points

- [Draft v4 design](design/README.md)
- [Current-state architecture](architecture.md)
- [Implementation roadmap](plans/implementation-plan.md)
- [Template system](design/templates/README.md)
- [OpenAPI contract](api/openapi.yaml)
- [Self-hosting guide](guides/self-hosting.md)
- [Native development runbook](runbooks/native-development.md)
- [Local UAT runbook](runbooks/local-uat.md)

Draft v4 is not approved or frozen. Approval rules live in the
[design index](design/README.md#approval-rule).

## Writing and checks

- Use relative links inside `docs/`.
- Use Mermaid for Markdown diagrams.
- Keep living Markdown files near 300 lines. Split larger subjects into a
  directory with a `README.md` index and focused pages.
- Do not rewrite completed phase records or acceptance catalogs.
- Move long-lived design reasoning out of code comments and into the owning
  design page or ADR.
- Run `make docs-fmt` and `make docs-lint` after Markdown or YAML changes.

The full rules are in [`standards/engineering.md`](standards/engineering.md).

`UAT` means the P9 local and P9A staging user-workflow gates. Earlier files
named `uat-phase-*` are immutable automated acceptance history; their names do
not redefine the term.
