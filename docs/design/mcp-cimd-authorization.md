# MCP CIMD authorization

Status: proposal. Nothing here is built.
[MCP Client ID Metadata Documents](mcp-cimd.md) holds the motivation, sources,
release prerequisites, and owner decisions.

Discovery, consent, stored clients, errors, privacy, and compatibility belong to
this boundary. See [fetch](mcp-cimd-fetch.md) and
[verification](mcp-cimd-verification.md).

## Discovery

Authorization server metadata adds
`"client_id_metadata_document_supported":true` while the feature flag is on
([Release plan](mcp-cimd.md#release-plan)). `registration_endpoint` stays, and
`token_endpoint_auth_methods_supported` stays `["none"]`. A client without CIMD
support, and every client while the flag is off, keeps using DCR.

The server tells the two kinds of `client_id` apart by form. A value that starts
with `https://` is a client ID URL. Any other value must be a canonical UUID, as
DCR issues. Anything else is an unknown client.

## Errors

| Where                                 | Failure                                                                            | Response                                                     |
| ------------------------------------- | ---------------------------------------------------------------------------------- | ------------------------------------------------------------ |
| Authorize, consent page, consent POST | URL rule, address refused, fetch, status, type, size, deadline, document, redirect | `400` closed page; never redirects to the presented URI      |
| Authorize, consent page, consent POST | A fetch limit or the concurrency cap                                               | `429` closed page with `Retry-After`; never redirects        |
| Authorize                             | Valid client and redirect, bad other parameter                                     | Existing error redirect (`invalid_request`, `invalid_scope`) |
| Token, refresh, revoke                | URL passes the rules but has no stored client, or fails the rules                  | `401` `invalid_client`, as rule 8 of the compatibility note  |
| Token, refresh                        | URL client differs from the code's or refresh token's client                       | `400` `invalid_grant`                                        |
| Token                                 | Any client authentication other than `none`                                        | Existing rejection, unchanged                                |

A failed fetch leaves the redirect URI untrusted, so no error goes back to the
client (RFC 6749 section 4.1.2.1). The page says only that the app could not be
checked; logs carry the closed reason.

## Consent

For a client ID URL, the consent page leads with the host, not the self-asserted
name:

- “**{host}** muốn truy cập CV của bạn.” / “**{host}** wants access to your
  resumes.”
- Under it: “Ứng dụng tự gọi là “{client_name}”.” / “The app calls itself
  “{client_name}”.”
- Then the existing line naming where approval returns
  ([rule 6](mcp-client-compatibility.md#6-where-approval-returns)).
- For a loopback redirect, a warning: “Chỉ cho phép nếu bạn vừa bắt đầu đăng
  nhập từ một ứng dụng trên máy tính này.” / “Approve only if you just started
  this sign-in from an app on this computer.”

The host and name render as text, never markup or links. The copy needs owner
approval (**Owner decision** C6). Connected agents in Settings shows the same
host beside the stored name.

**Silent reauthorization.** Today an authorize request whose user already holds
a live grant covering the scopes gets a code without the consent page. For a URL
client with a loopback redirect, the server always shows consent instead
(**Owner decision** C5). Every Claude Code user shares one `client_id` and its
loopback redirects, so without this rule any local process could take a code
silently by naming that `client_id` and its own port. A URL client with an
`https` redirect keeps silent reauthorization: its code can only reach a
redirect the current document lists, on the client ID URL's own host. The
claude.ai document in [Sources](mcp-cimd.md#sources) fits that case: its one
redirect is on `claude.ai`, the client ID URL's host, so it passes C3, and C5
leaves its silent reauthorization on.

## Stored clients

A URL client gets a row in `oauth_clients`, so codes, grants, and tokens keep
their foreign keys and every token path stays the same:

- A new nullable column `metadata_url` holds the URL, unique when present.
  `client_name` and `redirect_uris` hold the last approved document's values, as
  a record; redirect checks use the document, not the row.
- The row is created or updated only by an approved consent, which is signed in
  and CSRF-checked. An anonymous authorize request reads the cache and writes
  nothing.
- The token, refresh, and revocation endpoints find the row by `metadata_url`
  and never fetch. Claude allows 10 seconds for token requests and 30 for
  refresh; a remote fetch there would spend that budget on a third party.
- One row per URL means one live grant per user and client. A reconnect replaces
  the user's grant for that client instead of adding one toward the ten-grant
  limit.
- The idle-client sweep removes a URL client's row like any other, 24 hours
  after its last grant and token end. The next approved consent creates it
  again.

## Data and privacy

| Data                              | Where               | Kept                                                |
| --------------------------------- | ------------------- | --------------------------------------------------- |
| URL, `client_name`, redirect list | Memory cache        | Clamped cache lifetime, or until evicted or restart |
| URL, `client_name`, redirect list | `oauth_clients` row | Until 24 hours after its last grant and token end   |
| Fetched body                      | Nowhere             | Parsed and dropped                                  |

Logs, traces, and metrics carry the URL host, a SHA-256 digest of the URL, the
client row ID, and a closed outcome such as `address_refused`, `status`,
`content_type`, `too_large`, `deadline`, `document_invalid`,
`client_id_mismatch`, `redirect_mismatch`, or `rate_limited`. They never carry
the body, the full URL path, a `client_name`, or a redirect URI. As the draft
notes, a fetch tells the client's operator that someone is signing in at aboutme
around that time; the request carries nothing more. The privacy notice needs no
change: the only stored data describes the client, not the user.

**Size.** One additive migration adds the column (at most 256 bytes) and a
partial unique index.

## Older clients and loss

DCR clients, rows, grants, and tokens do not change. The Go SDK runner has no
client ID URL, so it should keep DCR; its CI proof confirms it (assumption).

Turning the flag off stops advertising CIMD and refuses new authorizations for
URL clients. Token, refresh, and revocation keep serving existing URL clients,
so connected users stay connected. Claude rereads metadata about every five
minutes ([lazy authentication][claude-lazy]) and then falls back to DCR for new
connections.

Rolling back to a release without CIMD code leaves the column in place, unread.
A refresh naming a URL `client_id` then fails `invalid_grant`, so each
URL-client user reconnects through DCR, as after other client-visible rollbacks.
Removing the column would first delete URL-client rows, which cascades to their
grants and tokens; that is a deliberate, separate change.

[claude-lazy]: https://claude.com/docs/connectors/building/lazy-authentication
