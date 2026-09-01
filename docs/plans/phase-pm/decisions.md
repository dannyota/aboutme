# Phase PM decisions

These values are frozen for the phase. Changing one is a reviewed authority
change with evidence, never test tuning.

## M1 — Client registration grammar and bounds

- `client_name`: 1–64 Unicode code points after NFC; no control characters;
  rendered as text only, never markup.
- `redirect_uris`: 1–5 entries, each ≤ 512 ASCII bytes, absolute, no fragment,
  no userinfo. Scheme rule: `https://` with any host, or `http://` only for
  hosts `127.0.0.1`, `localhost`, or `[::1]` (any port, any path).
- Registration body: exact `application/json`, ≤ 4,096 bytes, strict JSON (no
  unknown or duplicate fields). Accepted metadata is exactly `client_name` and
  `redirect_uris`; `token_endpoint_auth_method` defaults to and only accepts
  `none`.
- Response: `client_id` (UUID string), echoed metadata, no secret.
- Idle-client GC: a client with no live grant and no live token, created more
  than 24 hours ago, is deleted. GC runs on each successful registration and at
  startup, ≤ 200 rows per sweep.

## M2 — Authorization codes

- 32 random bytes, base64url without padding (43 characters), stored only as a
  32-byte SHA-256 digest.
- TTL 60 seconds. Single use: consumption locks the row, marks it consumed, and
  issues tokens in the same transaction.
- A code binds client ID, user ID, scopes, `code_challenge` (S256 digest
  string), and the exact `redirect_uri` used at authorize time. The token
  request must repeat that exact `redirect_uri`.
- Replay of a consumed code revokes every token issued from that code and
  returns the closed `invalid_grant` error.

## M3 — Tokens and scopes

- Access token: `amat_` + 43-character base64url of 32 random bytes; TTL 1 hour.
  Refresh token: `amrt_` + same shape; family absolute lifetime 30 days from
  first issue.
- Storage is digest-only (SHA-256, 32 bytes) with closed `kind`
  (`access`/`refresh`), `family_id`, and `rotated_from` lineage.
- Refresh rotation: every use issues a new refresh token in the same family and
  marks the presented one superseded. Presenting a superseded refresh token
  revokes the whole family.
- Scope set is closed: `resumes:read`, `resumes:write`. Read tools require
  `resumes:read`; every mutating tool requires `resumes:write`.
- Revocation (RFC 7009 or settings UI) revokes the grant and its token families
  in one transaction. `last_used_at` is updated at most once per 60 seconds per
  token row.

## M4 — Endpoint media types and closed OAuth errors

- `/oauth/token` and `/oauth/revoke`: POST, exact
  `application/x-www-form-urlencoded`, ≤ 4,096 bytes, closed parameter sets
  (`grant_type`, `code`, `redirect_uri`, `client_id`, `code_verifier`,
  `refresh_token`; revoke adds `token` and optional `token_type_hint`). Unknown
  or duplicate parameters are rejected.
- `/oauth/register`: POST, strict JSON per M1.
- `/.well-known/oauth-authorization-server` and
  `/.well-known/oauth-protected-resource`: GET only, static JSON derived from
  the canonical origin; request headers never choose the issuer.
- OAuth error vocabulary is closed to `invalid_request`, `invalid_client`,
  `invalid_grant`, `unauthorized_client`, `unsupported_grant_type`,
  `invalid_scope`, `access_denied`, and `server_error`, with no
  `error_description` detail beyond a fixed generic sentence.
- PKCE: `code_challenge_method=S256` required at authorize; `code_verifier`
  43–128 characters of the RFC 7636 alphabet; verification compares SHA-256
  digests in constant time.

## M5 — Rate, cap, and concurrency budgets

| Budget                          | Value                       |
| ------------------------------- | --------------------------- |
| `/oauth/register` per IP        | ≤ 5/hour                    |
| `/oauth/token` per IP           | ≤ 30/min                    |
| Failed grants per client        | ≤ 10 per 15 min             |
| `/mcp` tool calls per token     | ≤ 120/min                   |
| `/mcp` tool calls per user      | ≤ 240/min                   |
| Concurrent `/mcp` requests/user | ≤ 4                         |
| Live grants per user            | ≤ 10 (11th consent refused) |
| `/mcp` request body             | ≤ 4,194,304 bytes           |
| OAuth request bodies            | ≤ 4,096 bytes               |
| Idle-client GC                  | 24 h idle; ≤ 200 rows/sweep |

These numbers are copied into `docs/plans/budgets.md` at T00; budgets.md is the
enforcement authority afterwards.

## M6 — MCP transport and tool contract

- Streamable HTTP in stateless JSON mode: each POST carries one JSON-RPC message
  and returns one JSON response; no SSE stream, session state, or server
  notification in v1.
- Fifteen tools exactly: `list_resumes`, `get_resume`, `create_resume`,
  `delete_resume`, `update_resume_metadata`, `upsert_entry`, `delete_entry`,
  `update_section`, `update_structure`, `update_personal_details`,
  `update_customization`, `get_photo`, `upload_photo`, `update_photo_crop`,
  `delete_photo`. `get_photo` returns base64 content and content type and
  requires only `resumes:read`.
- Tool error vocabulary is closed: `validation_failed`, `revision_conflict`,
  `not_found`, `payload_too_large`, `scope_denied`, `rate_limited`,
  `agent_access_unavailable`.
- `create_resume` has no prior revision. Every mutation of an existing resume
  takes `revision` as a decimal string. Successful create and update operations,
  including entry and photo deletion, return `{ revision, state }` with the
  complete canonical stored resume after sanitizing. `delete_resume` returns the
  matched revision plus `{ id, deleted: true }` because no row remains.
- MCP mutations have no client-supplied idempotency key or replay guarantee;
  revision CAS is the concurrency contract. `upload_photo` takes base64 content
  bounded by the existing media ceilings after decode.
- `mcpapi` reaches resume state only through a closed in-process `resumeapi`
  facade. The facade dispatches its fifteen operations to the existing handlers,
  so REST and MCP share one validator, sanitizer, bounds, and store path.
- A write grant delegates the user's delete authority. `delete_resume` may
  delete a private or published resume without browser-session recent reauth. A
  published deletion still uses the existing public revocation fence, drain,
  slug tombstone, media cleanup, and atomic rollback path. There are no
  standalone publish or unpublish tools.
- Validation/document/media-shape failures map to `validation_failed`; stale CAS
  maps to `revision_conflict`; missing and cross-user targets map to
  `not_found`; transport or decoded-media overflow maps to `payload_too_large`;
  admission exhaustion maps to `rate_limited`; and a failed in-transaction
  bearer recheck maps to `agent_access_unavailable`.
- Server instructions text describes the document shape, the read-modify-write
  loop, and the absence of publish tools.

## M7 — Bearer boundary

- Exactly one `Authorization: Bearer <token>` header; cookies on `/mcp` are
  ignored entirely (never parsed).
- Validation hashes the presented token and loads token row, grant, and user in
  one query path; expired, revoked, superseded, or cross-kind tokens are
  rejected with the same closed 401 body.
- An unauthenticated or insufficient request returns `401` with
  `WWW-Authenticate: Bearer resource_metadata="<canonical>/.well-known/oauth-protected-resource"`.
- Missing scope returns closed `403 scope_denied` without touching resume state.

## M8 — Stateless consent

- `/oauth/authorize` (GET) validates the full request against the client row,
  then 302s an authenticated browser to `/authorize?<validated-query>`; an
  unauthenticated browser goes to `/login?next=<url-encoded authorize path>` and
  returns with the query intact. The login page has no `next` support today —
  T10 builds it: `next` is honored only as a same-origin relative path (leading
  `/`, not `//`, no scheme, ≤ 2,048 bytes); anything else falls back to the
  current `/app/resumes` destination. Password login navigates to that validated
  path directly. Provider login carries it in the existing digest-bound,
  single-use `oauth_transactions` row and consumes it on a successful callback;
  browser storage, a second cookie, and a caller-controlled callback redirect
  are forbidden.
- The consent read operation re-validates the query server-side and returns only
  client name and scopes. The decision POST carries the exact request parameters
  plus `decision` (`approve`/`deny`); the server re-validates everything against
  the client row before issuing a code or denying.
- Approval records or refreshes the grant; a later request from the same client
  with equal-or-narrower scopes skips consent and issues a code directly. Denial
  redirects with `error=access_denied`.
- No pending agent-authorize server state exists; the provider transaction
  carries only the login return path. Agent authorization replay safety comes
  from code single-use and grant recording.

## M9 — Browser evidence

`dev-https-mcp-check` writes one `mcp-proof.json`, ≤ 4,096 bytes, mode 0600:

```json
{
  "errors": { "certificate": 0, "console": 0, "externalRequest": 0, "page": 0 },
  "origin": "https://localhost:20443",
  "scenario": "mcp-agent-access",
  "schemaVersion": 1,
  "steps": {
    "clientRegistered": true,
    "authorizeRedirected": true,
    "consentApproved": true,
    "tokenExchanged": true,
    "toolsListed": true,
    "resumeCreated": true,
    "entryUpserted": true,
    "editorVisible": true,
    "grantRevoked": true,
    "revokedRejected": true
  }
}
```

All agent-side HTTP (registration, token exchange, MCP calls) runs in the spec's
Node context against the trusted origin; browser traffic stays inside the
existing firewall.

## Rulings

- The raw OAuth protocol endpoints and `/mcp` are not part of the
  `docs/api/openapi.yaml` JSON contract: they are agent-facing, use non-JSON or
  JSON-RPC media types, and are specified by the RFCs and the MCP spec. OpenAPI
  gains only the session-authenticated consent and grant operations (T02). This
  ruling keeps code, Caddy, and OpenAPI in agreement.
- Photo upload over MCP needs a `/mcp` body cap larger than the global 256 KB
  default; the M5 4 MiB route-specific cap follows the P2B photo route
  precedent.
