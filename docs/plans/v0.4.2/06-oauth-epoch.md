# OAuth and connected-agent epoch brief

Role: backend. Model: `gpt-5.6-terra`.

## Objective and authority

Fence OAuth grants, authorization codes, token families, bearer authentication,
and consent on the account authentication epoch. Read `AGENTS.md`,
`docs/design/second-factor-authentication.md`,
`docs/design/passkey-second-factor-contract.md`, ADR 0026, ADR 0048, and the
verified storage and session reports before editing.

## Owned paths

- Modify `apps/server/internal/oauthsrv/authorize.go`.
- Modify `apps/server/internal/oauthsrv/authorize_test.go`.
- Modify `apps/server/internal/oauthsrv/consent.go`.
- Modify `apps/server/internal/oauthsrv/consent_test.go`.
- Modify `apps/server/internal/oauthsrv/token_endpoint.go`.
- Modify `apps/server/internal/oauthsrv/token_endpoint_test.go`.
- Modify `apps/server/internal/oauthsrv/revoke.go`.
- Modify `apps/server/internal/oauthsrv/revoke_test.go`.
- Modify `apps/server/internal/oauthsrv/session_http.go`.
- Modify `apps/server/internal/oauthsrv/session_http_test.go`.
- Modify `apps/server/internal/mcpapi/bearer.go`.
- Modify `apps/server/internal/mcpapi/bearer_test.go`.

Do not edit auth session primitives, factor code, account API, mail, OpenAPI,
web files, migrations, or infrastructure.

## Required behavior

- Grant and authorization-code issue lock the OAuth client when applicable, then
  the user, before reading and copying the current epoch.
- Authorization codes bind the exact grant ID and epoch. Exchange rejects a
  missing, revoked, foreign, or stale grant and a stale account epoch.
- Bearer authentication compares token, grant, and account authority on every
  request. A stale epoch authenticates nothing.
- Consent approval and silent grant reuse require current session authority and
  both recent verification timestamps for an enrolled account. Denial does not
  require recent proof.
- Every factor epoch change leaves no live grant or token family. Concurrent
  code issue, exchange, refresh, consent, and factor change have one valid
  authority outcome.
- Keep MCP tools and publishing scope unchanged.

## Test-first cycle and checks

Start with `git status --short`. Add failing tests for stale grants, codes,
bearers, refresh tokens, silent reuse, concurrent issue and exchange, and
factor-change revocation. The top manager must grant the database and normal
test lanes before these commands run:

```bash
(cd apps/server && go test -count=1 ./internal/oauthsrv ./internal/mcpapi)
(cd apps/server && golangci-lint run ./internal/oauthsrv/... ./internal/mcpapi/...)
make server-build server-vet server-test
```

Definition of done: no browser or agent authority issued before an epoch change
works after it, and no consent path bypasses both recent proofs. Report exact
files, checks and results, skipped checks with command and reason, race
evidence, and open items. Identify the exact `authorize.go` and
`token_endpoint.go` hunks that the later v0.4.4 MCP workflow must preserve when
the top manager integrates releases in order. Do not perform Git operations. Use
short plain text with no em dash.
