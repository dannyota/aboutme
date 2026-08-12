# API contract

| File           | Purpose                                                                                                                                                                 |
| -------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `openapi.yaml` | Implemented `/api/v1` and health-route contract: operations, envelopes, errors, write safety, and examples. Contract changes edit this source and regenerate consumers. |
| `redocly.yaml` | Lint config for `openapi.yaml` (scoped rule overrides, with rationale in comments).                                                                                     |
| `test/`        | Vitest suite that lints the contract and pins its invariants.                                                                                                           |

Lint:
`npx @redocly/cli lint docs/api/openapi.yaml --config docs/api/redocly.yaml`
Test: `npx vitest run docs/api/test`

## Generated TypeScript client

The web app does not hand-transcribe this contract. `openapi.yaml` is the input
to a pinned generator, and the output is committed:

| Path                                  | What it is                                                              |
| ------------------------------------- | ----------------------------------------------------------------------- |
| `apps/web/app/api/generated/`         | Generated path/schema types. Never edited by hand; `eslint` ignores it. |
| `apps/web/app/api/client.ts`          | Hand-written typed-client factory over those types.                     |
| `apps/web/scripts/openapi-gen.sh`     | The single generator invocation, used by generation and the drift gate. |
| `apps/web/scripts/api-drift-check.sh` | Non-mutating drift gate (generates into `mktemp -d`, then `diff -r`).   |

- **Generator:** `openapi-typescript`, pinned exactly in
  `apps/web/package.json`. It emits types only — no runtime — so the artifact
  stays reviewable and adds nothing to the bundle.
- **Transport:** `openapi-fetch`, pinned exactly in the same manifest. It is a
  thin `fetch` wrapper that infers method, path, params, and response union from
  the generated `paths` type.
- **Two clients, one contract:** `openapi.yaml` overrides `/healthz` and
  `/readyz` back to the unversioned site root, so `client.ts` splits the path
  map. Asking the `/api/v1` client for `/healthz` is a type error.

Regenerate: `make api-gen` (`npm run api:gen` in `apps/web`). Drift gate:
`bash apps/web/scripts/api-drift-check.sh`, wired into `make api-check`. It
generates into a throwaway directory and compares — it never writes inside the
repository, so it is safe to run in a dirty or shared worktree, and it reports
drift instead of quietly repairing it.
