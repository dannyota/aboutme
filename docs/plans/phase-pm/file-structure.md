# Phase PM file structure and ownership

Each implementation path has one task author. Shared/generated paths belong to
the integration owner and are serialized.

## T00 — Authorities, budgets, traceability, and roots

- `docs/design/{README,product,security,data,api,web,decisions}.md`
- `docs/adr/0026-mcp-agent-access.md`
- `docs/plans/budgets.md`
- `docs/plans/traceability/{README,ac-mcp}.md`
- `packages/publicroots/public-roots.v6.json` and removal of v5 after all
  consumers regenerate.
- `scripts/generate-public-roots.mjs` and generated Go/Nuxt/Caddy/testdata
  consumers named by the current generator.
- Runtime v6 references in `scripts/dev-native.sh`, `scripts/dev-https.sh`,
  `scripts/dev-https-test.sh`, and `deploy/dev-https-browser/static-test.sh`.
- Route-table and public-root contract tests affected by the eight new roots.

## T01 — OAuth storage

- `apps/server/migrations/00009_add_oauth_agent_access.sql`
- `apps/server/migrations/oauth_agent_access_test.go`
- `apps/server/sql/queries.sql`
- Generated `apps/server/internal/store/{db,models,querier,queries.sql}.go`
- New `apps/server/internal/store/oauth_contract.go` and tests.

## T02 — OpenAPI consent and grant operations

- `docs/api/openapi.yaml`
- Generated `apps/web/app/api/generated/openapi.ts`
- New `docs/api/test/oauth-consent.test.ts` and affected contract tests.
- `apps/web/test/api-client.test.ts` only for generated-operation parity.

## T03 — OAuth primitives

- New `apps/server/internal/oauthsrv/{token,code,pkce,redirect,scope}.go` and
  matching `_test.go` files.

## T04 — Client registration and GC

- New `apps/server/internal/oauthsrv/{clients,clients_gc}.go` and tests.

## T05 — Authorize and consent service

- New `apps/server/internal/oauthsrv/{authorize,consent}.go` and tests.

## T06 — Token endpoint and revocation

- New `apps/server/internal/oauthsrv/{token_endpoint,revoke}.go` and tests.

## T07 — Discovery and bearer middleware

- New `apps/server/internal/oauthsrv/metadata.go` and tests.
- New `apps/server/internal/mcpapi/{bearer,errors}.go` and tests.

## T08 — MCP server and tools (owner)

- Owner dependency window: `apps/server/go.mod`, `go.sum`, `go.work.sum` for
  `github.com/modelcontextprotocol/go-sdk` v1.7.0.
- New `apps/server/internal/mcpapi/{server,instructions}.go`, `tools_read.go`,
  `tools_lifecycle.go`, `tools_content.go`, `tools_photo.go`, and matching tests
  including the go-sdk client integration test.
- Shared-validation extraction inside `apps/server/internal/resumeapi`
  (`validate.go` split; REST behavior unchanged, proven by the existing suites).

## T09 — Rate policies and composition (owner)

- New `apps/server/internal/oauthsrv/rate.go` and
  `apps/server/internal/mcpapi/rate.go` with tests.
- `apps/server/internal/config/config.go` and tests.
- `apps/server/cmd/server/main.go` and `cmd/server` tests.
- `.env.example` (names only).

## T10 — Consent page

- New `apps/web/app/pages/authorize.vue` and
  `apps/web/app/composables/useOAuthConsent.ts`.
- New `apps/web/test/{authorize,useOAuthConsent}.test.ts`.
- Login `next` preservation only if a bounded change is required; report
  otherwise.

## T11 — Connected agents settings

- New `apps/web/app/components/settings/ConnectedAgents.vue` and
  `apps/web/app/composables/agentGrants.ts`.
- `apps/web/app/pages/sessions.vue` (settings integration point).
- New `apps/web/test/{connected-agents,agentGrants}.test.ts`.

## T12 — Native HTTPS MCP UAT (owner)

- `scripts/dev-https-check.sh` (new `mcp` mode).
- `deploy/dev-https-browser/run.sh` (spec list, evidence schema, mode map).
- New `deploy/dev-https-browser/mcp.spec.ts`.
- `deploy/dev-https-browser/static-test.sh` and
  `scripts/test/makefile-safety-test.sh` contract updates.
- `Makefile` `dev-https-mcp-check` target and AGENTS.md check-table row.
- New `apps/server/cmd/mcp-uat-fixture` seed/cleanup command if the proof needs
  a dedicated identity; reuse `uat-google-00x` conventions.
