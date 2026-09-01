# AC-MCP traceability rows

Ten acceptance-criterion rows use the `AC-MCP-` prefix. Phase PM owns them all.
The design clause column names the amended Approved v4 clause
([product](../../design/product.md#agent-access),
[API](../../design/api.md#agent-access-and-the-bearer-world),
[security](../../design/security.md#agent-authorization-and-the-bearer-world),
[data](../../design/data.md#relational-model),
[web](../../design/web.md#agent-consent-and-connected-agents)) whose rationale
is [ADR 0026](../../adr/0026-mcp-agent-access.md); the mechanism detail lives in
[`../mcp-agent-access-design.md`](../mcp-agent-access-design.md) and the frozen
numbers in [`../phase-pm/decisions.md`](../phase-pm/decisions.md). See
[README.md](./README.md) for matrix rules and the full prefix index.

| ID         | Design clause                      | Statement                                                                                                                                                         | Phase/task         | State  | Test / acceptance reference                                        |
| ---------- | ---------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------ | ------ | ------------------------------------------------------------------ |
| AC-MCP-001 | OAuth: registration                | Dynamic client registration enforces the M1 name/URI grammar and bounds, stores no secret, and garbage-collects idle clients in bounded sweeps                    | PM T00/T01/T03/T04 | PROVEN | `oauthsrv/clients*_test.go`; M9 proof                              |
| AC-MCP-002 | OAuth: authorize and consent       | Authorize validates client, exact redirect URI, scopes, and S256 challenge before any redirect; consent is explicit, revalidated server-side, and skip-safe       | PM T02/T05/T10     | PROVEN | `oauthsrv/{authorize,consent}_test.go`; web consent tests; M9      |
| AC-MCP-003 | OAuth: codes                       | Codes are digest-only, 60-second, single-use under a row lock, bound to client/user/scopes/challenge/URI; replay revokes every token issued from the code         | PM T01/T03/T05/T06 | PROVEN | Migration/store contracts; `oauthsrv/token_endpoint_test.go`       |
| AC-MCP-004 | OAuth: tokens                      | Tokens are digest-only with 1-hour access and 30-day rotating refresh families; superseded reuse revokes the family; revocation kills grant and families together | PM T01/T03/T06     | PROVEN | `oauthsrv/{token_endpoint,revoke}_test.go`; M9 revoked-token 401   |
| AC-MCP-005 | OAuth: discovery                   | RFC 8414/9728 metadata derives only from the canonical origin, and an unauthenticated `/mcp` request returns the 401 bootstrap pointer                            | PM T07             | PROVEN | `oauthsrv/metadata_test.go`; `mcpapi/server_test.go`               |
| AC-MCP-006 | Security: bearer boundary          | `/mcp` and the token-world endpoints never read cookies; absent, malformed, expired, revoked, superseded, and cross-user tokens fail byte-identically closed      | PM T07/T08         | PROVEN | `mcpapi/{bearer,server}_test.go`; M9 closed 401                    |
| AC-MCP-007 | Tools: editor parity minus publish | Fifteen tools enforce scope, bounds, sanitizing, ownership, and CAS identically to REST; no publish, unpublish, or public-read capability is reachable            | PM T08             | PROVEN | `mcpapi/server_test.go`; `resumeapi/agent*_test.go`; M9            |
| AC-MCP-008 | Bounds: rate and caps              | Register/token/tool-call rates, the 4-concurrent cap, the 10-grant cap, and the 4 MiB body cap enforce the M5 budgets with bounded key stores and Retry-After     | PM T05/T09         | PROVEN | `oauthsrv/{clients,rate}_test.go`; `mcpapi/server_test.go`         |
| AC-MCP-009 | Web: consent and connected agents  | The consent page and Connected agents settings render client data as text, act through the session/CSRF chain, and revoke grants visibly to live agents           | PM T02/T10/T11     | PROVEN | OAuth session HTTP and web consent/grant tests; M9 revocation      |
| AC-MCP-010 | Verification: end-to-end proof     | One native HTTPS Playwright process proves register→authorize→consent→exchange→build→editor-visible→revoke→rejected with zero error counters                      | PM T12             | PROVEN | `deploy/dev-https-browser/mcp.spec.ts`; `make dev-https-mcp-check` |
