# 0018: MCP agent access

Status: Accepted (2026-09-01).

## Context

The resume API began with one caller: the first-party browser application,
authenticated by an opaque `__Host-` session cookie and fenced by CSRF and
exact-Origin checks.

People already use general-purpose agents to draft and tailor resumes and copy
text between an agent and the editor by hand. The product offers
bring-your-own-agent access instead: the user connects an agent they already
have, and it edits their resumes through the same validated boundary the editor
uses. The product does not ship a first-party AI writing feature, and it does
not host or resell a model.

That raises three questions. What protocol does an arbitrary third-party agent
speak? How does it obtain authority over one account's resumes without a session
cookie? And what may it do with that authority?

## Decision

**One remote Streamable HTTP MCP endpoint.** Agents connect to `/mcp` on the
canonical origin. The handler runs in stateless JSON mode: one JSON-RPC message
per POST, one JSON response, no SSE stream and no server notification. There is
no local stdio distribution. The handler is built on the pinned official
`modelcontextprotocol/go-sdk`, so the protocol version and its framing are an
upstream contract.

**A first-party OAuth 2.1 authorization server inside the Go binary.**
`internal/oauthsrv` implements dynamic client registration, the authorize flow,
token issue, rotation and revocation, and the RFC 8414 and RFC 9728 discovery
documents. Authorization is code with PKCE (S256 required, `plain` rejected)
with rotating refresh tokens, for public clients only. An unmodified agent can
discover and connect without a bespoke handshake.

**Editor parity minus publish.** Fifteen tools cover resume lifecycle, content,
and photo operations, and each dispatches into the same validation, sanitizer,
bounds, idempotency, and CAS chain as its REST handler. Shared checks are
factored into functions both callers use; there is no second validation path.
There is no publish, unpublish, or public-read tool. Making a resume public
stays a human decision in the web UI.

**Account-wide scopes.** Consent grants `resumes:read` and `resumes:write` over
the account. Photo tools ride on `resumes:write`. Per-resume grants are not
offered.

## Rejected alternatives

- **Personal access tokens only.** Every agent would hold a full-authority
  secret in its own storage, revocation would be per token rather than per
  connected agent, and no MCP client discovers it automatically.
- **A separate agent service.** Would duplicate the validation, sanitizer,
  bounds, and CAS chain or add a network hop into the API: a second write
  authority.
- **Per-resume grants.** Makes the tool surface, consent page, and grant table
  depend on resume identity and lifetime, and an agent that creates a resume
  would need a second consent. With a three-resume cap and no publish tool,
  account-wide scopes carry a bounded blast radius.
- **Reusing the session cookie.** The agent would inherit the full account
  surface, including publish and account deletion, and a non-browser client
  would sit inside the CSRF boundary.

## Consequences

- The public-root registry has four fixed roots for this feature:
  `/.well-known`, `/oauth`, and `/mcp` dispatch to Go, and `/authorize` is the
  Nuxt consent page. Finer paths dispatch inside the Go routers.
- Four bounded tables hold OAuth state: `oauth_clients`,
  `oauth_authorization_codes`, `oauth_grants`, and `oauth_tokens`. Token and
  code material exists only as a 32-byte SHA-256 digest; PKCE verifiers are
  never stored.
- A bearer world runs beside the cookie world. `/mcp`, `/oauth/token`,
  `/oauth/register`, and `/oauth/revoke` never read cookies and have no CSRF
  surface. The authorize and consent surfaces keep the full session, CSRF, and
  exact-Origin chain. An `Authorization` header never relaxes CSRF on a
  cookie-authenticated route.
- Grants and authorization codes bind the account's authentication epoch (ADR
  0017).
- The raw OAuth endpoints and `/mcp` stay outside `docs/api/openapi.yaml`; the
  RFCs and the MCP spec define them. OpenAPI carries only the
  session-authenticated consent and grant operations.
- Publish scope, per-resume grants, confidential clients, a local stdio wrapper,
  and SSE streaming are deferred, not designed away.
- An account that never authorizes an agent sees no change in any route,
  session, or resume behavior.

## History

Former ADR 0026, unchanged in substance.
