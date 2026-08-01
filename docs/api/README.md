# api

| File           | Purpose                                                                                                                                                 |
| -------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `openapi.yaml` | `/api/v1` contract — envelope, error shape, write-safety (`If-Match`/`412`), health paths. Every later phase appends paths here; it is never rewritten. |
| `redocly.yaml` | Lint config for `openapi.yaml` (scoped rule overrides, with rationale in comments).                                                                     |
| `test/`        | Vitest suite that lints the contract and pins its invariants.                                                                                           |

Lint:
`npx @redocly/cli lint docs/api/openapi.yaml --config docs/api/redocly.yaml`
Test: `npx vitest run docs/api/test`
