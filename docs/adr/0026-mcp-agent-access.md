# 0026 — MCP agent access

Status: Accepted (2026-09-01)

Supersedes the "no programmatic client" scope in the Approved v4 product, API,
and security design.

## Context

Approved v4 gave the resume API exactly one caller: the first-party browser
application, authenticated by an opaque `__Host-` session cookie and fenced by
CSRF and exact-Origin checks. Every write reached the aggregate boundary through
a cookie-authenticated handler. The deferred mobile client was the only
acknowledged second caller, and it had no protocol.

People already use general-purpose agents to draft and tailor resumes. Today
they copy text between an agent and the editor by hand. V1 ships
bring-your-own-agent access instead: the user connects an agent they already
have, and it edits their resumes through the same validated boundary the editor
uses. The product does not ship a first-party AI writing feature, and it does
not host or resell a model.

That raises three questions Approved v4 does not answer. What protocol does an
arbitrary third-party agent speak? How does that agent obtain authority over one
account's resumes without holding a session cookie? And what may it do once it
has that authority?

## Decision

**One remote Streamable HTTP MCP endpoint.** Agents connect to `/mcp` on the
canonical origin. The handler runs in stateless JSON mode: one JSON-RPC message
per POST, one JSON response, no SSE stream and no server notification in v1.
There is no local stdio distribution to install, version, and support. The
handler is built on the pinned official `modelcontextprotocol/go-sdk`, so the
protocol version and its framing are an upstream contract rather than a
hand-rolled one.

**A first-party OAuth 2.1 authorization server inside the Go binary.**
`internal/oauthsrv` implements dynamic client registration, the authorize flow,
token issue, rotation and revocation, and the RFC 8414 and RFC 9728 discovery
documents. Authorization is code-with-PKCE (S256 required, `plain` rejected)
with rotating refresh tokens, for public clients only. The MCP authorization
spec expects exactly this shape, so an unmodified agent can discover and connect
without a bespoke handshake.

**Editor parity minus publish.** Fifteen tools cover resume lifecycle, content,
and photo operations, and each dispatches into the same validation, sanitizer,
bounds, idempotency, and CAS chain as its REST handler. Shared checks that live
only in an HTTP handler today are factored into functions both callers use;
there is no second validation path. There is no publish, unpublish, or
public-read tool. Making a resume public stays a human decision taken in the web
UI, because it is the one irreversible-in-effect action an agent could take on a
person's behalf.

**Account-wide scopes.** Consent grants `resumes:read` and `resumes:write` over
the account. Photo tools ride on `resumes:write`. Per-resume grants are not
offered in v1.

## Rejected alternatives

- **Personal access tokens only.** A user-pasted long-lived bearer token needs
  no authorization server, but every agent then handles a full-authority secret
  in its own storage, revocation is per token rather than per connected agent,
  and no MCP client discovers it automatically. It trades a bounded protocol for
  an unbounded credential-handling surface.
- **A separate agent service.** Splitting `/mcp` into its own deployable would
  duplicate the validation, sanitizer, bounds, and CAS chain or force a second
  network hop into the API. Either outcome creates the second write authority
  the design has avoided everywhere else.
- **Per-resume grants.** Scoping consent to one resume reads as safer, but it
  makes the tool surface, the consent page, and the grant table depend on resume
  identity and lifetime, and an agent that creates a resume would need a second
  consent to edit it. With a three-resume cap and no publish capability,
  account-wide read and write scopes carry a bounded blast radius.
- **Reusing the session cookie.** A cookie-authenticated agent would inherit the
  full account surface, including publish and account deletion, and would put a
  non-browser client inside the CSRF boundary.

## Consequences

- The public-root registry moves to v6 and gains four fixed roots:
  `/.well-known`, `/oauth`, and `/mcp` dispatch to Go, and `/authorize` is the
  Nuxt consent page. Finer paths dispatch inside the Go routers, following the
  `/api` pattern.
- One additive migration adds four bounded tables — `oauth_clients`,
  `oauth_authorization_codes`, `oauth_grants`, and `oauth_tokens`. Token and
  code material exists only as a 32-byte SHA-256 digest; PKCE verifiers are
  never stored.
- A bearer world now runs beside the cookie world. `/mcp`, `/oauth/token`,
  `/oauth/register`, and `/oauth/revoke` never read cookies and have no CSRF
  surface. The authorize and consent surfaces keep the full session, CSRF, and
  exact-Origin chain. The ADR 0014 rule that an `Authorization` header never
  relaxes CSRF on a cookie-authenticated route is unchanged.
- The raw OAuth endpoints and `/mcp` stay outside `docs/api/openapi.yaml`: they
  are specified by the RFCs and the MCP spec and use non-JSON or JSON-RPC media
  types. OpenAPI gains only the session-authenticated consent and grant
  operations.
- Publish scope, per-resume grants, confidential clients, a local stdio wrapper,
  and SSE streaming are deferred, not designed away.
- The feature is additive: no existing route, session, or resume behavior
  changes for an account that never authorizes an agent.
