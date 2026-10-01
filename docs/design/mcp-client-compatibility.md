# MCP client compatibility

Status: implemented in v0.6.18 (2026-09-28). The owner approved every numbered
choice below as written.

aboutme's OAuth server and MCP endpoint are proven with one client, the official
Go SDK runner in the [MCP owner workflow](mcp-owner-workflow.md). Claude's
clients, and other clients built on the official TypeScript SDK, send requests
that the server rejects today. This proposal lists each gap with its evidence
and the smallest rule change that closes it, so the [MCP guide](mcp-guide.md)
can teach steps that work. It keeps [ADR 0018](../adr/0018-mcp-agent-access.md):
one endpoint, first-party OAuth 2.1, public clients with PKCE, dynamic client
registration (DCR), account-wide scopes, and no publish tool.

## Evidence

Anthropic's [connector authentication page][claude-auth] states what Claude's
OAuth client needs; the same client backs claude.ai, Claude Desktop, Claude
mobile, Claude Code, and Cowork:

- The protected resource metadata `resource` must equal the URL the user enters,
  including the path.
- Hosted apps redirect to `https://claude.ai/api/mcp/auth_callback`. Claude Code
  uses `http://localhost:PORT/callback` on an ephemeral port.
- Claude uses DCR when the server does not advertise Client ID Metadata
  Documents (CIMD), and registers a new client on each fresh connection.
  [MCP Client ID Metadata Documents](mcp-cimd.md) proposes CIMD support.
- Refresh errors must be `invalid_grant`. Traffic comes from `160.79.104.0/21`.

The [TypeScript SDK source][ts-auth] on `main` shows the request shapes. This
design did not capture Claude's own requests, so each shape below is confirmed
for the SDK and assumed for Claude until the proof in [Proofs](#proofs) runs:

- `registerClient` posts the client metadata plus `grant_types`, a derived
  `application_type` (`native` for a loopback redirect), and `scope` when known.
- `selectResourceURL` sends the metadata `resource` as a parsed URL, so
  `https://aboutme.vn` goes out as `https://aboutme.vn/`.
- Token requests, refresh included, add `resource` and, for a public client,
  `client_id`.

## Gaps and proposed rules

| Gap | Today                                            | Effect on Claude                |
| --- | ------------------------------------------------ | ------------------------------- |
| 1   | Metadata `resource` is `https://aboutme.vn`      | Not the URL entered; `/` added  |
| 2   | Registration rejects any member outside four     | Registration fails              |
| 3   | `native` registration rejects `localhost`        | Claude Code registration fails  |
| 4   | Refresh accepts only `grant_type`, refresh token | Refresh fails; reconnect hourly |
| 5   | Five registrations an hour per client address    | Shared egress exhausts it       |
| 6   | Consent shows only the self-asserted client name | Impersonation is hard to spot   |
| 7   | Grant limit shows the generic invalid message    | Reconnects hit a silent wall    |
| 8   | Unknown client at the token endpoint returns 400 | Claude does not re-register     |

### 1. Resource identifier

`/.well-known/oauth-protected-resource` names
`"resource":"https://aboutme.vn/mcp"`, the URL a person enters. The
authorization endpoint, the code exchange, and refresh accept zero or one
`resource`, byte for byte one of two values: `https://aboutme.vn/mcp` or the
origin `https://aboutme.vn`. The origin stays accepted so any client that cached
the old metadata keeps working. The Go SDK runner itself needed an update, not
just backward-compat acceptance: `go-sdk` v1.8.0 checks the challenge's
protected-resource metadata against the MCP endpoint URL and then sends that
metadata `resource` back, so the pinned runner
([owner workflow](mcp-owner-workflow.md#sdk-and-http-boundary)) now requires
exactly `https://aboutme.vn/mcp` and fails closed on anything else. The
raw-field check applies to both spellings. Every other value, including the
trailing-slash origin, keeps the closed `invalid_request` at authorize and
`invalid_grant` at the token endpoint. Both spellings name the one protected
resource, so tokens, grants, and tables do not change. The `/mcp` challenge
still names the metadata URL.

### 2. Registration members

Registration ignores members it does not know, as RFC 7591 section 2 requires,
and never stores or echoes them. Three known members are checked when present:
`grant_types` must be a non-empty subset of `authorization_code` and
`refresh_token`; `response_types` must be exactly `["code"]`; `scope` must name
only `resumes:read` and `resumes:write`. `scope` at registration grants nothing;
consent still decides. The 4,096-byte body cap, duplicate-member rejection,
required `client_name` and `redirect_uris`, the redirect grammar, and
`token_endpoint_auth_method` `none` stay.

### 3. Native loopback host and web application type

`application_type` `native` accepts `http://localhost` redirects on any port, as
well as `127.0.0.1` and `[::1]`. RFC 8252 section 8.3 prefers IP literals, but a
client that omits `application_type` can already register `localhost` under the
general grammar, so the narrower native rule protects nothing and breaks Claude
Code. Redirects still match the registered value exactly, port included.

Registration also accepts `application_type` `web`, which must name only `https`
redirect URIs. The [TypeScript SDK][ts-auth] sends `web` for an https callback
such as claude.ai's, and an `https` redirect was already accepted without any
`application_type` under the general grammar, so honoring the label adds no
reach beyond what an omitted `application_type` already allowed; it only lets a
client that sends the member register at all. This was answered during the
build, not a separate owner approval.

### 4. Refresh parameters

A refresh request may add `client_id`, which must equal the refresh token's
client, and `resource`, under rule 1. A mismatch is `invalid_grant`. A dead,
reused, or expired refresh token stays `invalid_grant`, which Claude needs to
start a new sign-in. Rotation and family revocation do not change.

### 5. Registration rate from shared egress

The per-address registration bucket stays at five an hour. A configured list of
published client egress ranges, starting with Anthropic's `160.79.104.0/21`,
shares one bucket per range of 120 registrations an hour, inside a new global
ceiling of 600 an hour. The list is deployment configuration, not code, and an
address outside it keeps today's limit. The idle-client sweep already removes a
registration with no grant after 24 hours. Token and tool limits do not change.

CIMD would remove registration for Claude entirely, but it needs a server-side
fetch of client-supplied URLs with SSRF controls.
[MCP Client ID Metadata Documents](mcp-cimd.md) holds that design, a proposal
awaiting owner decisions.

### 6. Where approval returns

The consent page adds one line under the client name, from the validated
redirect URI of the request:

- `https` redirect: “Sau khi cho phép, bạn sẽ quay lại {host}.” / “After you
  approve, you return to {host}.”
- Loopback redirect: “Sau khi cho phép, bạn quay lại một ứng dụng trên máy tính
  này.” / “After you approve, you return to an app on this computer.”

The host is rendered as text. The [MCP authorization specification][mcp-auth]
requires the redirect host on the consent screen. The consent API needs no
change: the page already holds the validated `redirect_uri`.

### 7. Grant limit message

Consent refused by the ten-grant limit gets its own closed code,
`agent_limit_reached`, and the page says: “Bạn đã có 10 trợ lý được kết nối. Thu
hồi một trợ lý trong Cài đặt rồi thử lại.” / “You already have 10 connected
agents. Revoke one in Settings, then try again.” with a link to
`/app/settings/sessions`. OpenAPI gains the code on the consent operation.

### 8. Unknown client at the token endpoint

A token request naming a client that no longer exists returns `401` with
`invalid_client`, as RFC 6749 section 5.2 allows, so Claude registers again.
Deleting a client cascades to delete its authorization codes, so an
`authorization_code` exchange looks up the client before the code: a swept
client must fail `invalid_client`, not the `invalid_grant` that a merely missing
code would give for an unrelated reason. A `refresh_token` request looks up the
refresh token first; only when the token itself is missing and the request named
`client_id` does it check the client, returning `invalid_client` for a swept
registration and `invalid_grant` for any other missing or dead token. A known
client with a bad grant keeps `400` `invalid_grant`.

## Older clients and loss

Every change widens what the server accepts or adds text, with one exception:
the Go SDK runner needed a matching update, because `go-sdk` v1.8.0 requires the
`/mcp` resource rather than the origin (rule 1). Every other older client keeps
working: the origin `resource` for a client that never adopts `/mcp`, the four
original registration members, IP loopback redirects, and the two-member refresh
stay valid. Nothing is stored in a new shape, so there is no migration,
backfill, or loss rule.

Rolling back does not leave Claude working. A refresh request naming
`client_id`, which Claude always sends once it holds a token issued under this
change, gets `invalid_grant` from the older code, and the protected-resource
metadata reverts to naming the origin instead of `/mcp`. A registered client
keeps its stored name and redirects, but every connected Claude agent must
re-register and reconnect after a rollback, the same as after any other
client-visible incompatibility.

## Security

- Ignored registration members never reach storage, logs, or the consent page.
- `localhost` for native clients adds no reach beyond the general grammar, and
  `https`-only redirects for `web` clients add none either.
- Accepting `/mcp` as a second resource spelling does not widen the audience:
  both name the one resource server.
- Two rate changes apply, not one: the egress-range bucket, and a global
  600-an-hour registration ceiling that bounds every admitted registration
  together, ranged or not. An IPv6 address outside a configured range is keyed
  on its `/64`, not its full address, since one host usually controls a whole
  `/64` and could otherwise spread registrations across it. The range list must
  come from Anthropic's published page and be reviewed like any production
  configuration.
- The consent host line and the grant-limit message make impersonation and
  silent failure easier to spot. The guide teaches people to check the line.
- The OAuth routes keep cookie isolation, closed errors, and body caps. The
  reviewer confirms by name: PKCE S256, exact redirect match, refresh rotation
  and reuse revocation, cross-client refresh rejection, scope enforcement per
  tool, and revocation reaching `/mcp` as `401`.

## Open questions

Neither item below is decided; both are for the owner to weigh separately from
the approvals recorded below.

- **Reserved room for Claude's range under the global ceiling.** The 600-an-hour
  global registration ceiling is shared by every range and every ordinary
  address. A surge of registrations from ranges other than `160.79.104.0/21`
  could exhaust the ceiling before Claude's own users hit their 120-an-hour
  range budget. Whether to reserve room for Claude's range inside the global
  ceiling is open.
- **RFC 9728 per-resource metadata path.** [RFC 9728][rfc9728] section 3.1
  places protected-resource metadata for the `/mcp` resource at
  `/.well-known/oauth-protected-resource/mcp`. The server serves only the root
  `/.well-known/oauth-protected-resource`. Clients work today either through the
  `/mcp` challenge's `resource_metadata` URL, which already names the root path,
  or the SDK's fallback discovery. Whether to add the resource-specific path is
  open.

## Proofs

- **Go tests** for each rule, including the rejected neighbors: trailing-slash
  origin, `grant_types` with `client_credentials`, `response_types` with
  `token`, a refresh `client_id` from another client, and a native custom
  scheme.
- **CI proof with the TypeScript SDK.** A dev-https proof beside the Go SDK
  proof drives the pinned official TypeScript SDK client with a loopback
  redirect and a synthetic login: discovery, registration, consent, token,
  `tools/list`, one private write, a forced refresh, revoke, and `401`.
- **Owner proof in production** with the owner test account: Claude on the web
  and Claude Code each add `https://aboutme.vn/mcp`, approve, list and read
  resumes, make one private edit, work again after more than one hour, and fail
  after a revoke in Settings. Evidence records outcomes only, never content or
  tokens.

## Design text to update in the build

[API](api.md#agent-access-and-the-bearer-world), the OAuth server contract in
[MCP owner workflow](mcp-owner-workflow.md#oauth-server-contract),
[Security](security.md#agent-authorization-and-the-bearer-world), and
[Budgets](budgets.md) for the egress-range bucket and global ceiling.

## Owner approvals

1. **Resource `/mcp` in metadata, origin still accepted.** Approved
   (2026-09-27): Claude requires it, and the origin stays valid for any older
   client; the Go SDK runner itself needed a matching update (see
   [Older clients and loss](#older-clients-and-loss)).
2. **Ignore unknown registration members; check three known ones.** Approved
   (2026-09-27): RFC 7591 requires ignoring them.
3. **`localhost` for native loopback redirects.** Approved (2026-09-27): the
   current rule is bypassable and blocks Claude Code.
4. **`client_id` and `resource` on refresh.** Approved (2026-09-27): without it
   every Claude user reconnects each hour and piles up grants.
5. **Egress-range registration bucket: 120 an hour per range, 600 global.**
   Approved (2026-09-27): a handful of Claude users an hour would otherwise lock
   everyone out. Alternative: build CIMD first, a larger release.
6. **Consent line naming where approval returns.** Approved (2026-09-27): the
   MCP specification requires the host, and the guide relies on it.
7. **`agent_limit_reached` with its own message.** Approved (2026-09-27):
   reconnects accumulate grants and the generic error hides the cause.
8. **`401 invalid_client` for an unknown client.** Approved (2026-09-27): lets
   Claude recover from a swept registration.
9. **One backend and frontend release before the guide.** Approved (2026-09-27):
   small, additive, and provable in CI before the owner's production proof.

[claude-auth]: https://claude.com/docs/connectors/building/authentication
[ts-auth]:
  https://github.com/modelcontextprotocol/typescript-sdk/blob/main/packages/client/src/client/auth.ts
[mcp-auth]:
  https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization
[rfc9728]: https://www.rfc-editor.org/rfc/rfc9728
