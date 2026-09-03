# Phase PM file structure and ownership

Each implementation path has one task author. Shared/generated paths belong to
the integration owner and are serialized.

## T00 — Authorities, budgets, traceability, and roots

- `docs/design/{README,product,security,data,api,web,decisions}.md`
- `docs/adr/0026-mcp-agent-access.md`
- `docs/design/budgets.md`
- `docs/plans/traceability/{README,ac-mcp}.md`
- `packages/publicroots/public-roots.v6.json` and removal of v5 after all
  consumers regenerate.
- `scripts/generate-public-roots.mjs` and generated Go/Nuxt/Caddy/testdata
  consumers named by the current generator.
- Runtime v6 references in `scripts/dev-native.sh`, `scripts/dev-https.sh`,
  `scripts/dev-https-test.sh`, and `deploy/dev-https-browser/static-test.sh`.
- Route-table and public-root contract tests affected by the four new root rows
  (`.well-known`, `oauth`, `mcp`, `authorize`).

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
- `apps/server/internal/resumeapi/writesafety.go` and its tests: the
  write-safety principal generalization (session or token authority re-fetched
  inside the write transaction).

## T09 — Rate policies and composition (owner)

- New `apps/server/internal/oauthsrv/rate.go` and
  `apps/server/internal/mcpapi/rate.go` with tests.
- Integration edits to `oauthsrv/{clients,consent,token_endpoint}.go` and tests
  for configured caps, failed-grant reservations, and the session adapter.
- `mcpapi/{server,errors}.go` and tests for route-owned body, rate, and
  concurrency admission.
- New `apps/server/internal/oauthsrv/session_http.go` and tests for the OpenAPI
  consent and connected-agent session routes omitted from T05's service-only
  ownership.
- `apps/server/internal/auth/{handlers,csrf}.go` and tests for optional
  authorize sessions and the consent route's exact JSON media-type response.
- `apps/server/internal/api/router.go` and tests for the route-owned MCP body
  limit.
- `apps/server/internal/mcpapi/bearer_test.go` test-fixture transaction
  isolation required by the multi-package live-DB gate.
- `apps/server/internal/config/config.go` and tests.
- `apps/server/cmd/server/main.go` and `cmd/server` tests.
- `.env.example`, `deploy/compose.yml`, and the focused Compose contract test.

## T10 — Consent page

- Serialized integration-owner correction after the T09 owner window closes:
  `docs/api/openapi.yaml` and generated web types; migration 00009, OAuth
  transaction sqlc sources/generated files, and migration tests; provider
  start/callback implementation and tests under `apps/server/internal/auth/`.
- New `apps/web/app/pages/authorize.vue` and
  `apps/web/app/composables/useOAuthConsent.ts`.
- New `apps/web/test/{authorize,useOAuthConsent}.test.ts`.
- `apps/web/app/pages/login.vue`: bounded `next` query support (validated
  same-origin relative path per M8; fallback `/app/resumes`).

## T11 — Connected agents settings

- New `apps/web/app/components/settings/ConnectedAgents.vue` and
  `apps/web/app/composables/agentGrants.ts`.
- `apps/web/app/pages/app/settings/sessions.vue` (settings integration point).
- New `apps/web/test/{connected-agents,agentGrants}.test.ts`.

## T12 — Native HTTPS MCP UAT (owner)

- `scripts/dev-https.sh` and `scripts/dev-https-test.sh` (enable MCP in the
  isolated HTTPS server environment and pin that lifecycle contract).
- `scripts/dev-https-check.sh` (new `mcp` mode).
- `deploy/dev-https-browser/run.sh` and `playwright.config.ts` (spec list,
  evidence schema, mode map, and timeout).
- New `deploy/dev-https-browser/mcp.spec.ts`.
- `deploy/dev-https-browser/auth.spec.ts` and `harness-lib.ts` (retain the
  provider-login proof after the approved default return path changed to
  `/app/resumes`).
- `deploy/dev-https-browser/static-test.sh` and
  `scripts/test/makefile-safety-test.sh` contract updates.
- `Makefile` `dev-https-mcp-check` target and AGENTS.md check-table row.
- New `apps/server/cmd/mcp-uat-fixture` seed/cleanup command if the proof needs
  a dedicated identity; reuse `uat-google-00x` conventions.

## T13 — MCP idempotency parity

- `apps/server/internal/resumeapi/{validate.go,agent_test.go}`.
- `apps/server/internal/mcpapi/{instructions.go,tools_lifecycle.go,tools_content.go,tools_photo.go,tools_test.go,server_test.go}`.
- `deploy/dev-https-browser/mcp.spec.ts`.
- PM decisions, task index, exit criteria, MCP design draft, Approved v4 API
  clarification, and AC-MCP-007 traceability row.
