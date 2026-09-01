# Phase PM — MCP agent access implementation plan

Status: **Exit candidate** (2026-09-02). AC-MCP-001–010 are proven by focused
suites and the M9 native HTTPS proof. The phase review fixes await confirmation;
the unchanged-candidate gates have not run.

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let users bring their own agents to build resumes through a remote
Streamable HTTP MCP endpoint authenticated by a first-party OAuth 2.1
authorization server, with editor parity minus publish.

**Architecture:** `internal/oauthsrv` owns dynamic client registration, the
authorize/consent flow, PKCE code exchange, rotating refresh-token families,
revocation, and discovery metadata. `internal/mcpapi` owns bearer validation,
scope checks, and fifteen tools that dispatch into the same validation,
sanitizer, bounds, and CAS chain as the REST handlers. Four bounded tables store
clients, code digests, grants, and token digests; raw token material never
exists in PostgreSQL. `/mcp` and the token-world endpoints ignore cookies
entirely; the consent surfaces use the existing session/CSRF chain.

**Tech Stack:** Go 1.27.0, PostgreSQL 18, sqlc,
`github.com/modelcontextprotocol/go-sdk` v1.7.0, OpenAPI 3.1, Nuxt 4, Vue 3,
TypeScript 6.0.3, Caddy, Playwright 1.62.1, and Podman.

**Spec:** [`../mcp-agent-access-design.md`](../mcp-agent-access-design.md)
(Approved for implementation planning, 2026-09-01).

## Global Constraints

- Task 00 first amends Approved v4 (product, api, security, data, web) and
  accepts ADR 0026. No PM code lands while the design authorities and the spec
  disagree.
- The tool surface is editor parity minus publish: no publish, unpublish, or
  public-read tool, ever, in this phase. Scope grants are account-wide
  `resumes:read` and `resumes:write` only.
- PKCE is S256 only; `plain` is rejected. Public clients only
  (`token_endpoint_auth_method: none`). Redirect URIs are exact-match against
  registration; the M1 grammar admits only `https://` or loopback HTTP.
- Codes and tokens are opaque 256-bit random values stored as SHA-256 digests
  only, compared in constant time after exact shape decoding. A replayed code
  revokes every token issued from it; a superseded refresh token revokes its
  whole family.
- `/mcp`, `/oauth/token`, `/oauth/register`, and `/oauth/revoke` never read
  cookies and have no CSRF surface. The consent read/decision operations use the
  full session, CSRF, and exact-Origin chain.
- Every mutating tool takes and returns the same revision validator the editor
  uses; a lost race returns the closed `revision_conflict` error. No second
  validation path exists: shared checks are factored into functions both the
  REST handler and the tool call.
- Token material, code material, PKCE verifiers, and resume content never enter
  logs, traces, metrics labels, errors, panic text, or evidence. Logs carry
  client ID, grant ID, token row ID, tool name, resume ID, and closed outcomes
  only.
- All limits compose the ADR 0018 bounded limiter and the canonical client
  address from Caddy. Exact numbers are the M5 budget rows landed at T00.
- Existing per-user resume caps, sanitizer versioning, media bounds, and
  idempotency behavior apply to agent writes unchanged. REST behavior must not
  change for users who never authorize an agent.
- Each task has one author who writes RED first and owns its adversarial cases.
  There is no per-task reviewer. One non-author performs the ADR 0024 phase
  review after completion records are committed.
- Workers edit only exclusive paths and never use Git. The integration owner
  serializes Approved v4/ADR/budgets/registry, migration/sqlc, OpenAPI/generated
  client, dependency pins, composition, harness scripts, and final records.
- At most three heavy checks run at once. Full `make ci` and connected
  `make scan` run alone on one unchanged candidate commit.

## Plan documents

- [Decisions](decisions.md) freezes M1–M9: registration grammar, code/token
  formats and lifetimes, endpoint media types, closed error vocabularies, rate
  numbers, MCP body bounds, consent statelessness, and browser evidence.
- [File structure](file-structure.md) assigns every implementation path once.
- [Integration handoffs](integration-handoffs.md) freezes producer/consumer
  interfaces and shared owner windows.
- [Exit criteria](exit-criteria.md) is the unchanged-candidate phase gate.

## Task index

| Task                                  | Deliverable                                                    | Acceptance               | Owner               |
| ------------------------------------- | -------------------------------------------------------------- | ------------------------ | ------------------- |
| [00](task-00-authorities-roots.md)    | Approved v4/ADR 0026/budgets/traceability and v6 public roots  | MCP-001…010 rows PLANNED | Integration owner   |
| [01](task-01-oauth-storage.md)        | Migration 00009, constraints, sqlc, `OAuthQueries` contract    | MCP-001/003/004          | Integration owner   |
| [02](task-02-openapi-consent.md)      | Consent/grant operations and generated TS client               | MCP-002/009              | Integration owner   |
| [03](task-03-oauth-primitives.md)     | Token/code/PKCE/redirect/scope primitives                      | MCP-001/003/004          | OAuth-core author   |
| [04](task-04-client-registration.md)  | DCR handler, bounds, idle-client GC                            | MCP-001                  | Registration author |
| [05](task-05-authorize-consent.md)    | Authorize validation, consent service, code issue              | MCP-002/003              | Authorize author    |
| [06](task-06-token-endpoint.md)       | Token exchange, refresh rotation, revocation                   | MCP-003/004              | Token author        |
| [07](task-07-discovery-bearer.md)     | Discovery metadata, 401 bootstrap, bearer/scope middleware     | MCP-005/006              | Resource author     |
| [08](task-08-mcp-server-tools.md)     | go-sdk server, fifteen tools, shared-validation refactor       | MCP-006/007              | Integration owner   |
| [09](task-09-rate-composition.md)     | Rate policies, caps, config validation, main.go composition    | MCP-008                  | Integration owner   |
| [10](task-10-consent-page.md)         | Nuxt `/authorize` consent page and login-redirect preservation | MCP-002/009              | Web-consent author  |
| [11](task-11-connected-agents.md)     | Settings "Connected agents" list and revocation                | MCP-009                  | Web-settings author |
| [12](task-12-native-https-mcp-uat.md) | `make dev-https-mcp-check` end-to-end browser+agent proof      | MCP-010 and all          | Integration owner   |

## Frozen waves

Phase PM starts from the integrated PA candidate. Shared owner windows never
overlap another task that reads or writes the same surface.

| Wave | Tasks                 | Start condition                     | Heavy limit                                     |
| ---- | --------------------- | ----------------------------------- | ----------------------------------------------- |
| W0a  | 00                    | Plan committed; PA complete         | Owner alone; authorities and route registry     |
| W0b  | 01                    | T00 lands                           | Owner alone; migration/sqlc and one database    |
| W0c  | 02                    | T01 lands                           | Owner alone; OpenAPI/generated client           |
| W1   | 03                    | T01/T02 interfaces frozen           | One Go check                                    |
| W2   | 04, 05, 06            | T03 lands                           | Three disjoint Go checks; at most two live DB   |
| W3   | 07                    | T06 interfaces land                 | One Go check                                    |
| W4   | 08                    | T07 lands; owner pins go-sdk v1.7.0 | MCP server alone; live DB and SDK client tests  |
| W5   | 09, 10                | T08 lands                           | Owner composition and web page disjoint         |
| W6   | 11                    | T10 shared web primitives land      | Web settings alone                              |
| W7   | 12                    | T00–T11 focused gates pass          | One Playwright process; no other browser/build  |
| W8   | Records, review, exit | T00–T12 reports accepted            | Records, one fresh review, then candidate gates |

## Dispatch and completion

The integration owner commits this approved plan and dispatches T00. Each task
brief includes its task file, integrated base commit, authorities, acceptance
IDs, owned paths, exact check, and report format.

After T12, the owner updates phase state, master plan, traceability,
architecture, and runbook evidence and commits those records locally. One fresh
non-author then reviews the complete candidate and names the PKCE, code-replay,
token-rotation, revocation, scope, cookie-isolation, redirect, enumeration,
rate-limit, sanitizing, CAS, and no-publish invariants. Findings return to the
owning author and the same reviewer confirms fixes. The owner then runs the exit
checklist, `make ci`, and connected `make scan` on one unchanged candidate
before push.
