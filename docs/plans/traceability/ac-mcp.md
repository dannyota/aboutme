# AC-MCP traceability rows

Fifteen acceptance-criterion rows use the `AC-MCP-` prefix. The first ten have
recorded proof. CIMD rows remain in progress. The design clause column names the amended Approved v4 clause
([product](../../design/product.md#agent-access),
[API](../../design/api.md#agent-access-and-the-bearer-world),
[security](../../design/security.md#agent-authorization-and-the-bearer-world),
[data](../../design/data.md#relational-model),
[web](../../design/web.md#agent-consent-and-connected-agents)) whose rationale
is [ADR 0018](../../adr/0018-mcp-agent-access.md). The current mechanism lives
in the implementation and [architecture](../../architecture.md); numeric limits
remain in the approved [budgets](../../design/budgets.md). See
[README.md](./README.md) for matrix rules and the full prefix index.

| ID         | Design clause                      | Statement                                                                                                                                                                                                                       | Phase/task             | State  | Test / acceptance reference                                                               |
| ---------- | ---------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------- | ------ | ----------------------------------------------------------------------------------------- |
| AC-MCP-001 | OAuth: registration                | Dynamic client registration enforces the M1 name/URI grammar and bounds, stores no secret, and garbage-collects idle clients in bounded sweeps                                                                                  | PM T00/T01/T03/T04     | PROVEN | `oauthsrv/clients*_test.go`; M9 proof                                                     |
| AC-MCP-002 | OAuth: authorize and consent       | Authorize validates client, exact redirect URI, scopes, and S256 challenge before any redirect; consent is explicit, revalidated server-side, and skip-safe                                                                     | PM T02/T05/T10         | PROVEN | `oauthsrv/{authorize,consent}_test.go`; web consent tests; M9                             |
| AC-MCP-003 | OAuth: codes                       | Codes are digest-only, 60-second, single-use under a row lock, bound to client/user/scopes/challenge/URI; replay revokes every token issued from the code                                                                       | PM T01/T03/T05/T06     | PROVEN | Migration/store contracts; `oauthsrv/token_endpoint_test.go`                              |
| AC-MCP-004 | OAuth: tokens                      | Tokens are digest-only with 1-hour access and 30-day rotating refresh families; superseded reuse revokes the family; revocation kills grant and families together                                                               | PM T01/T03/T06         | PROVEN | `oauthsrv/{token_endpoint,revoke}_test.go`; M9 revoked-token 401                          |
| AC-MCP-005 | OAuth: discovery                   | RFC 8414/9728 metadata derives only from the canonical origin, and an unauthenticated `/mcp` request returns the 401 bootstrap pointer                                                                                          | PM T07                 | PROVEN | `oauthsrv/metadata_test.go`; `mcpapi/server_test.go`                                      |
| AC-MCP-006 | Security: bearer boundary          | `/mcp` and the token-world endpoints never read cookies; absent, malformed, expired, revoked, superseded, and cross-user tokens fail byte-identically closed                                                                    | PM T07/T08             | PROVEN | `mcpapi/{bearer,server}_test.go`; M9 closed 401                                           |
| AC-MCP-007 | Tools: editor parity minus publish | Fifteen tools enforce applicable scope, bounds, validation, and ownership identically to REST; all twelve mutations also share sanitizing, caller idempotency, and CAS; no publish, unpublish, or public-read tool is reachable | PM T08/T13             | PROVEN | `mcpapi/server_test.go`; `resumeapi/agent*_test.go`; M9 exact-retry proof                 |
| AC-MCP-008 | Bounds: rate and caps              | Register/token/tool-call rates, the 4-concurrent cap, the 10-grant cap, and the 4 MiB body cap enforce the M5 budgets with bounded key stores and Retry-After                                                                   | PM T05/T09             | PROVEN | `oauthsrv/{clients,rate}_test.go`; `mcpapi/server_test.go`                                |
| AC-MCP-009 | Web: consent and connected agents  | When agent access is enabled, consent and Connected agents render client data as text, act through the session/CSRF chain, and revoke grants visibly; when disabled, settings hides the block and makes no grant request        | PM T02/T10/T11; PF T05 | PROVEN | OAuth session HTTP and web consent/grant tests; PF capability-gating tests; M9 revocation |
| AC-MCP-010 | Verification: end-to-end proof     | One native HTTPS Playwright process proves register→authorize→consent→exchange→build→editor-visible→revoke→rejected with zero error counters                                                                                    | PM T12                 | PROVEN | `deploy/dev-https-browser/mcp.spec.ts`; `make dev-https-mcp-check`                        |

## Client metadata documents

These rows are in progress. Test sources exist; CI, independent
review, and hosted HTTPS proof execution remain required. The owner production
proof with Claude Code passed on 2026-10-14; hosted Claude remains unproven.

| ID | Design clause | Statement | State | Test / acceptance reference |
|-|-|-|-|-|
| AC-MCP-011 | CIMD: configuration and storage | Startup flag defaults off; production refuses the stub; exact nullable URLs retain UUID foreign keys and refuse destructive down while present | in progress | `config/cimd_test.go`; `migrations/cimd_test.go` |
| AC-MCP-012 | CIMD: fetch | URL grammar, full address refusals, pinned TLS, closed documents, deadlines, cache and bounded admission fail closed | in progress | `oauthsrv/cimd_test.go`; `oauthsrv/cimd_fetch_test.go` |
| AC-MCP-013 | CIMD: authorization | Reads and denial write no row; approval commits client, grant and code together; loopback always shows consent; token operations never fetch or accept internal UUID aliases | in progress | `oauthsrv/cimd_authorization_test.go`; token and consent tests |
| AC-MCP-014 | CIMD: interface | Both languages lead with the isolated host, quote the name, warn for loopback, and name the redirect hostname; Settings shows the host | in progress | Web authorize, consent and connected-agent tests; `mcp-ts-sdk.spec.ts` phone and desktop proof source; execution pending |
| AC-MCP-015 | CIMD: deployment and proof | Default off; older binary startup checks stopped-app URL rows and MCP configuration; trusted HTTPS SDK proof and owner enablement proof | in progress | `deploy/vn/scripts/cimd-rollback-test.sh`; VN safety/failure tests; `mcp-ts-sdk.spec.ts` CIMD flow; owner production proof passed 2026-10-14 with Claude Code 2.1.296 (consent, list and read, reconnect with consent and one grant, refresh after expiry, Settings revoke then refused request and refresh); hosted Claude proof pending |
