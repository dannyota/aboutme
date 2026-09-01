# AC-MCP traceability rows

Ten acceptance-criterion rows use the `AC-MCP-` prefix. Phase PM owns them all;
the design clause column cites
[`../mcp-agent-access-design.md`](../mcp-agent-access-design.md). See
[README.md](./README.md) for matrix rules and the full prefix index.

| ID         | Design clause                      | Statement                                                                                                                                                         | Phase/task         | State   | Test / acceptance reference |
| ---------- | ---------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------ | ------- | --------------------------- |
| AC-MCP-001 | OAuth: registration                | Dynamic client registration enforces the M1 name/URI grammar and bounds, stores no secret, and garbage-collects idle clients in bounded sweeps                    | PM T00/T01/T03/T04 | PLANNED | Planned in phase tasks      |
| AC-MCP-002 | OAuth: authorize and consent       | Authorize validates client, exact redirect URI, scopes, and S256 challenge before any redirect; consent is explicit, revalidated server-side, and skip-safe       | PM T02/T05/T10     | PLANNED | Planned in phase tasks      |
| AC-MCP-003 | OAuth: codes                       | Codes are digest-only, 60-second, single-use under a row lock, bound to client/user/scopes/challenge/URI; replay revokes every token issued from the code         | PM T01/T03/T05/T06 | PLANNED | Planned in phase tasks      |
| AC-MCP-004 | OAuth: tokens                      | Tokens are digest-only with 1-hour access and 30-day rotating refresh families; superseded reuse revokes the family; revocation kills grant and families together | PM T01/T03/T06     | PLANNED | Planned in phase tasks      |
| AC-MCP-005 | OAuth: discovery                   | RFC 8414/9728 metadata derives only from the canonical origin, and an unauthenticated `/mcp` request returns the 401 bootstrap pointer                            | PM T07             | PLANNED | Planned in phase tasks      |
| AC-MCP-006 | Security: bearer boundary          | `/mcp` and the token-world endpoints never read cookies; absent, malformed, expired, revoked, superseded, and cross-user tokens fail byte-identically closed      | PM T07/T08         | PLANNED | Planned in phase tasks      |
| AC-MCP-007 | Tools: editor parity minus publish | Fifteen tools enforce scope, bounds, sanitizing, ownership, and CAS identically to REST; no publish, unpublish, or public-read capability is reachable            | PM T08             | PLANNED | Planned in phase tasks      |
| AC-MCP-008 | Bounds: rate and caps              | Register/token/tool-call rates, the 4-concurrent cap, the 10-grant cap, and the 4 MiB body cap enforce the M5 budgets with bounded key stores and Retry-After     | PM T05/T09         | PLANNED | Planned in phase tasks      |
| AC-MCP-009 | Web: consent and connected agents  | The consent page and Connected agents settings render client data as text, act through the session/CSRF chain, and revoke grants visibly to live agents           | PM T02/T10/T11     | PLANNED | Planned in phase tasks      |
| AC-MCP-010 | Verification: end-to-end proof     | One native HTTPS Playwright process proves register→authorize→consent→exchange→build→editor-visible→revoke→rejected with zero error counters                      | PM T12             | PLANNED | Planned in phase tasks      |
