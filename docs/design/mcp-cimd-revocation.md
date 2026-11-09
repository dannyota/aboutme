# MCP CIMD revocation

[MCP CIMD authorization](mcp-cimd-authorization.md) owns the surrounding
boundary and the [error matrix](mcp-cimd-authorization.md#errors).

Owner decision [C12](mcp-cimd.md#owner-decisions) (2026-10-14) approves
possession-only revocation, the timing limit below, and ignoring invalid hints.

`POST /oauth/revoke` keeps the exact `application/x-www-form-urlencoded` media
type and [body bound](budgets.md). Allow only `token` (required, non-empty),
`token_type_hint` (optional), and `client_id` (optional, non-empty), each once
after decoding. Unknown keys, duplicates, invalid encoding, oversized bodies,
and missing or empty required values return the closed `400 invalid_request`
from [Errors](mcp-cimd-authorization.md#errors). Wrong method and media type
retain `405` and `415`. Ignore all hint values, including empty, unknown, and
opposite-kind values, per [RFC 7009 section 2.2][revoke-response].

Validate a supplied `client_id` before token parsing, transactions, or any
database access: `https://` values use the
[URL grammar](mcp-cimd-fetch.md#client-id-url-rules), others the DCR UUID
parser. Failure uses the closed `400`. Never resolve the claim, read a metadata
cache, perform DNS, or fetch. Cookies and Authorization headers supply no
authority. Both CIMD flag states use this contract. Existing outer admission
stays; no limiter is added. Revocation never consults or changes token-IP or
failed-grant budgets, including overflow; exhausted grants remain revocable.
Outer refusal depends on traffic only, never credential state.

Possession selects authority. A non-empty malformed token returns empty `200`
without database access. Parsed tokens get a digest lookup across both kinds; a
miss returns `200`. Any retained row, including expired, rotated, or revoked
ones, selects its own client, user, then grant locks. Revoke that grant and all
its families atomically; return `200` after commit. Other grants survive.
Concurrent refresh shares these locks: no successor survives revocation.

Compare claims only with the locked client: exact URL bytes for CIMD, parsed
UUID equality with `metadata_url IS NULL` for DCR. Internal UUIDs never match
URL clients. Mismatch still revokes. After commit, log `client_id_mismatch` with
only stored client, grant, and token row IDs. Never log claims or hashes, tokens
or digests, forms, cookies, Authorization headers, or raw database errors.
Telemetry failure cannot affect success; metrics use closed labels. Omitted or
matching claims emit no mismatch event.

All admitted well-formed forms have the same empty `200` and application headers
for malformed, unknown, live, expired, rotated, revoked, and mismatched tokens.
No credential-dependent cookie, challenge, redirect, retry, or rate header;
transport dates and request IDs are independent of token state. Malformed forms
return the same closed error for every token. This is response equality, **not
constant-time or fault-path equivalence**: syntax rejection and malformed tokens
do no database work, misses do a lookup, and hits lock and write. Matches and
mismatches follow the same transaction path, apart from bounded telemetry. Do
not add claim-dependent lookups or delays. Timing and database failures can
still distinguish these work classes.

Database failure returns generic `500`, never success or unknown-token `200`.
Roll back uncommitted writes. Failed commit acknowledgement leaves an unknown
outcome: assume authority may remain and retry idempotently. Reconnection after
successful logout requires fresh authorization; no grant is restored.

[RFC 7009 sections 2.1 and 5][rfc7009] require a public client ID and
token/client binding. Optional IDs and revocation on mismatch deliberately
depart from that rule. [RFC 6749 section 2.3][rfc6749] limits public-client
authentication; it does not waive binding. A holder can omit or replace the
claim. An error exposes binding; silent `200` refusal can leave live authority.
Even a retained dead token can disconnect its still-live grant. Token entropy,
HTTPS, body bounds, and outer admission limit abuse. Issuance keeps its binding
checks.

Ship one backend patch after approval, review, and green CI, then prove logout.
No migration, new flag, key, or stored field. Older binaries can reject claimed
IDs: retry without the ID, which remains supported, or revoke in Settings.

[rfc7009]: https://www.rfc-editor.org/rfc/rfc7009#section-2.1
[revoke-response]: https://www.rfc-editor.org/rfc/rfc7009#section-2.2
[rfc6749]: https://www.rfc-editor.org/rfc/rfc6749#section-2.3
