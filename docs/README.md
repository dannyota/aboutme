# Documentation

This directory separates intended design, current behavior, delivery work, and
operational procedures. Put each fact in the document type that owns it, then
link to that source instead of copying the contract.

## Authority

| Question                                          | Authority                                                                |
| ------------------------------------------------- | ------------------------------------------------------------------------ |
| What should the product and system become?        | [`design/`](design/README.md)                                            |
| Which numeric limit applies?                      | [`design/budgets.md`](design/budgets.md)                                 |
| What does the HTTP API implement now?             | [`api/openapi.yaml`](api/openapi.yaml)                                   |
| What does the repository implement now?           | Code, deployment configuration, and [`architecture.md`](architecture.md) |
| Why was an architecture choice accepted?          | [`adr/`](adr/README.md)                                                  |
| What runs next, and which gates apply?            | [`plans/`](plans/README.md)                                              |
| Which acceptance criterion owns a requirement?    | [`plans/traceability/`](plans/traceability/README.md)                    |
| How is an implemented operation performed?        | [`runbooks/`](runbooks/README.md)                                        |
| How does a contributor or operator get started?   | [`guides/`](guides/README.md)                                            |
| How should documentation and comments be written? | [`standards/`](standards/README.md)                                      |

The design wins over a plan. An accepted ADR controls its decision when the
design text conflicts with it. Code, deployment configuration, and OpenAPI must
agree about implemented behavior. A disagreement between those artifacts is a
defect, not permission to choose one silently.

## Directory map

| Path                                     | Contents                                                       | Lifecycle                                               |
| ---------------------------------------- | -------------------------------------------------------------- | ------------------------------------------------------- |
| [`design/`](design/README.md)            | Intended product, architecture, budgets, and template contract | Approved at v4; changes need an ADR or a new revision   |
| [`adr/`](adr/README.md)                  | One decision per record                                        | Accepted records are append-only; supersede, never edit |
| [`api/`](api/README.md)                  | OpenAPI source, contract tests, and client-generation notes    | Living with implemented HTTP behavior                   |
| [`plans/`](plans/README.md)              | Roadmap, active phase plans, and acceptance traceability       | Living; a phase's plan is deleted when the phase exits  |
| [`research/`](research/flowcv/README.md) | External observations used as evidence                         | Not project authority                                   |
| [`guides/`](guides/README.md)            | Setup and usage guidance                                       | Living with supported workflows                         |
| [`runbooks/`](runbooks/README.md)        | Exact operating, verification, and recovery procedures         | Added only when the operation exists                    |
| [`standards/`](standards/README.md)      | Documentation and code-comment rules                           | Living repository policy                                |

Root [`README.md`](../README.md), [`CONTRIBUTING.md`](../CONTRIBUTING.md),
[`SECURITY.md`](../SECURITY.md), and [`LICENSE`](../LICENSE) cover repository
entry points and project policy.

## Current entry points

- [Approved v4 design](design/README.md)
- [Current-state architecture](architecture.md)
- [Template system](design/templates/README.md)
- [OpenAPI contract](api/openapi.yaml)
- [Self-hosting guide](guides/self-hosting.md)
- [Native development runbook](runbooks/native-development.md)
- [Local UAT runbook](runbooks/local-uat.md)
- [Authentication email runbook](runbooks/email.md)

Design v4 is approved. Changing a decision needs a new ADR; see the
[design index](design/README.md#approval-rule).

## Writing and checks

- Use relative links inside `docs/`.
- Use Mermaid for Markdown diagrams.
- Keep living Markdown files near 300 lines. Split larger subjects into a
  directory with a `README.md` index and focused pages.
- Delete a phase's plan when the phase exits; git history keeps it.
- Move long-lived design reasoning out of code comments and into the owning
  design page or ADR.
- Run `make docs-fmt` and `make docs-lint` after Markdown or YAML changes.

The full rules are in [`standards/engineering.md`](standards/engineering.md).
