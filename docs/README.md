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

| Path                                | Contents                                                       | Lifecycle                                               |
| ----------------------------------- | -------------------------------------------------------------- | ------------------------------------------------------- |
| [`design/`](design/README.md)       | Intended product, architecture, budgets, and template contract | Approved design with accepted ADR amendments            |
| [`brand/`](brand/README.md)         | Brand assets and usage rules                                   | Living with the approved visual identity                |
| [`adr/`](adr/README.md)             | One decision per record                                        | Update the topic record with owner approval and History |
| [`api/`](api/README.md)             | OpenAPI source, contract tests, and client-generation notes    | Living with implemented HTTP behavior                   |
| [`plans/`](plans/README.md)         | Roadmap, active phase plans, and acceptance traceability       | Living; a phase's plan is deleted when the phase exits  |
| [`research/`](research/README.md)   | Historical observations and experiments                        | Not project authority                                   |
| [`guides/`](guides/README.md)       | Setup and usage guidance                                       | Living with supported workflows                         |
| [`runbooks/`](runbooks/README.md)   | Exact operating, verification, and recovery procedures         | Supported commands and explicitly open host procedures  |
| [`standards/`](standards/README.md) | Documentation and code-comment rules                           | Living repository policy                                |

Root [`README.md`](../README.md), [`CONTRIBUTING.md`](../CONTRIBUTING.md),
[`SECURITY.md`](../SECURITY.md), and [`LICENSE`](../LICENSE) cover repository
entry points and project policy.

## Current entry points

- [Live service](https://aboutme.vn)
- [Production runbook](runbooks/production.md)
- [Approved v4 design](design/README.md)
- [Current-state architecture](architecture.md)
- [Template system](design/templates/README.md)
- [OpenAPI contract](api/openapi.yaml)
- [Self-hosting guide](guides/self-hosting.md)
- [Native development runbook](runbooks/native-development.md)
- [Local UAT runbook](runbooks/local-uat.md)
- [Authentication email runbook](runbooks/email.md)

To change a decision in force, update its topic ADR with the owner's approval
and add a History entry naming what changed and why. Open a new ADR only for a
new topic. See the [ADR policy](adr/README.md).

## Writing and checks

Follow [`standards/engineering.md`](standards/engineering.md). Format only the
files you change, then run `make docs-lint` and `make docs-links-check`. The
link check covers changed Markdown, the main documentation indexes, ancestor
indexes, and indexes linking to changed or deleted targets. It does not crawl
unchanged historical pages. Set `DOC_LINK_BASE` to a commit to compare against;
the default is `HEAD`, including working changes and untracked files. CI uses
the event base commit. See the [contributor checks](../CONTRIBUTING.md#checks)
for commands.
