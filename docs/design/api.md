# 4. HTTP API

Product routes use `/api/v1`. Health routes remain unversioned. The current
implemented contract, including schemas and examples, is
[`../api/openapi.yaml`](../api/openapi.yaml); this page defines the intended v1
behavior that future contract changes must implement.

## Conventions

- JSON success uses `{data: ...}`. Declared binary photo and PDF reads return
  their bytes directly. Failure uses `{error:{code,message,details?}}`;
  `details` is optional and structured per error code.
- Authenticated API responses use exact `Cache-Control: no-store, no-transform`.
  Public responses use `no-cache, must-revalidate` with a strong entity tag
  (ETag).
- Resume revisions are signed 64-bit integers but serialize as decimal strings.
  Authenticated resume resource ETags and `If-Match` use the form
  `"r<revision>"`. Caddy does not encode or alter these responses. Route-parity
  tests prove that an owner read with `Accept-Encoding` still returns exact
  `"r<revision>"` and that a later exact `If-Match` reaches Go unchanged.
- A successful public response that carries a strong ETag uses the quoted
  64-character lowercase hexadecimal SHA-256 digest of its exact unencoded
  selected body. The applicable live-state or discovery gate runs before cache
  selection, conditional comparison, or revalidation. Caddy owns the
  encoding-specific strong-tag suffix and strips it before forwarding
  `If-None-Match`; separate route-parity tests prove encoded public conditional
  requests.
- The optional `X-Resume-Schema-Version` request header declares the client's
  document version. Absence means current. Every response containing resume data
  names its emitted version in the same header.
- Unknown JSON fields, unsupported document versions, malformed preconditions,
  and malformed idempotency keys fail closed.
- Every response carries a request ID. Client-safe errors expose a stable code
  and message; internal details stay in structured server logs keyed by that ID.
  A `5xx` response never includes a dependency, query, decoder, or stack error.

## Endpoint groups

| Endpoint group                                                     | Purpose                                                      |
| ------------------------------------------------------------------ | ------------------------------------------------------------ |
| `GET /auth/{provider}/start`, `GET /auth/{provider}/callback`      | Login start and callback                                     |
| `POST /auth/{provider}/start`                                      | Authenticated provider link or recent reauthentication start |
| `POST /auth/logout`, `GET /me`                                     | Logout and current identity/CSRF state                       |
| `POST /auth/password/register`, `POST /auth/password/verify`       | Email-and-password registration and single-use verification  |
| `POST /auth/password/login`, `POST /auth/password/reauth`          | Password login and recent reauthentication                   |
| `POST /auth/password/forgot`, `POST /auth/password/reset`          | Password reset request and single-use token consumption      |
| `PUT /me/password`                                                 | Add or replace the account password credential               |
| `GET /sessions`, `DELETE /sessions/{id}`, `DELETE /sessions`       | Device list, per-session revoke, and logout-everywhere       |
| `GET/POST /resumes`, `GET/PATCH/DELETE /resumes/{id}`              | Resume list, create, read, metadata update, and delete       |
| `PATCH /resumes/{id}/entries/{sectionKey}`, `DELETE .../{entryId}` | Entry upsert and delete                                      |
| `PATCH /resumes/{id}/sections/{sectionKey}`                        | Section display metadata and entry order                     |
| `PATCH /resumes/{id}/structure`                                    | Atomic section create, delete, move, or reorder              |
| `PATCH /resumes/{id}/personal-details`, `PATCH .../customization`  | Personal details and allowlisted customization deltas        |
| `POST/GET/PATCH/DELETE /resumes/{id}/photo`                        | Owner-only photo upload, read, crop, replace, and delete     |
| `POST /resumes/{id}/publish`                                       | Slug claim and three publish controls                        |
| `GET /resumes/{id}/pdf`                                            | Owner PDF                                                    |
| `GET /events`, `GET /live/{slug}`                                  | Authenticated and public SSE invalidation streams            |
| `GET /public/resumes/{slug}`, `GET /public/resumes/{slug}/photo`   | Live-gated public document and photo                         |
| `GET /public/resumes/{slug}/pdf`                                   | Live and download-gated public PDF                           |
| `GET /oauth/consent`, `POST /oauth/consent`                        | Agent consent read and the approve/deny decision             |
| `GET /me/agents`, `DELETE /me/agents/{grantId}`                    | Connected-agent list and grant revocation                    |
| `GET /me/export`, `DELETE /me`                                     | Data export and recent-reauthenticated account deletion      |

Password routes use strict JSON with a 4,096-byte body cap and the exact
`application/json` media type. Registration and forgot-password return an
identical `202`; login, verification, reset, reauthentication, and add/change
succeed with `204`. A well-shaped but absent, expired, consumed, or replaced
token is `400 credential_token_invalid`. The public error vocabulary is closed
and never discloses account state, provider, token, or hash detail. `GET /me`
adds the non-null Boolean `hasPassword`; provider emails are not exposed through
linked identities.

### Photo intake

The photo object key is server-derived and never accepted from a request.
Uploads accept exactly one raw multipart part named `file`. The request is at
most 2,162,688 bytes and the file is at most 2,097,152 bytes. A JPEG, PNG, or
WebP decoder must recognize and fully decode the source; the filename and client
media type grant nothing. Source width and height are each at most 8,192 pixels,
and their overflow-safe product is at most 16,777,216 pixels. Animated,
malformed, truncated, conflicting-orientation, and trailing-data inputs fail
before object storage changes.

The service applies Exif orientation, converts pixels to 8-bit color, strips all
source metadata and color profiles, and stores only a canonical static JPEG or
PNG. It never stores source container bytes. Opaque output is JPEG at quality 85
with a maximum 2,048-pixel edge. Output with alpha is PNG with a maximum
1,024-pixel edge. Neither is upscaled, and a fixed downscale ladder keeps the
stored object at or below 2,097,152 bytes. The key extension and served media
type come from this normalized output. Replacing a photo stores the new key with
no crop: a normalized crop belongs to one image and never carries across a
replacement.

Owner reads are private and conditional. Public reads are served through the
live-gated Go route; there is no direct public object-store path in v1. Exact
resource and time limits live in [the numeric budgets](../plans/budgets.md).
`PATCH /resumes/{id}/photo` changes only the optional normalized crop rectangle
or clears it. It preserves the transaction-read server-owned object key and
performs no object I/O.

## Agent access and the bearer world

Agent traffic uses fixed roots outside `/api/v1`. Only `/oauth/authorize` reads
a session, and it is a redirecting `GET`; every other route below ignores
cookies entirely, so no CSRF surface exists there.

| Route                                     | Method | Contract                                                            |
| ----------------------------------------- | ------ | ------------------------------------------------------------------- |
| `/.well-known/oauth-authorization-server` | GET    | RFC 8414 metadata derived only from the canonical origin            |
| `/.well-known/oauth-protected-resource`   | GET    | RFC 9728 metadata naming the authorization server                   |
| `/oauth/register`                         | POST   | RFC 7591 dynamic client registration; strict JSON, bounded          |
| `/oauth/authorize`                        | GET    | Session-aware; validates fully, then redirects to the consent page  |
| `/oauth/token`                            | POST   | Exact `application/x-www-form-urlencoded`; closed parameter set     |
| `/oauth/revoke`                           | POST   | RFC 7009 revocation of a grant and its token families               |
| `/mcp`                                    | POST   | MCP Streamable HTTP in stateless JSON mode; one message per request |

`/oauth/authorize` is the Go validator; `/authorize` is the Nuxt consent page it
redirects to. The OAuth error vocabulary is closed and carries one fixed generic
sentence, never dependency or input detail. The MCP tool error vocabulary is
separately closed: `validation_failed`, `revision_conflict`, `not_found`,
`payload_too_large`, `scope_denied`, `rate_limited`, and
`agent_access_unavailable`.

**These routes are not part of the OpenAPI contract.** They are agent-facing,
use non-JSON or JSON-RPC media types, and are specified by the named RFCs and
the MCP specification; describing them a second time in
[`../api/openapi.yaml`](../api/openapi.yaml) would create a source that drifts
from the RFC without adding a generated client. OpenAPI gains only the
session-authenticated consent and connected-agent operations listed above, so
code, Caddy, and OpenAPI continue to agree.

Every tool dispatches into the same validation, sanitizer, bounds, and store
chain as its REST counterpart; a shared check that lives only in an HTTP handler
is factored into a function both callers use. Mutating tools take the same
revision validator the editor sends and return the new one plus the canonical
stored state after sanitizing, so an agent observes exactly what a
hostile-markup strip did. A lost race returns `revision_conflict` and instructs
the agent to re-read. There is no publish, unpublish, or public-read tool.
[ADR 0026](../adr/0026-mcp-agent-access.md) records the protocol choice.

## Resume write safety

Every resume mutation requires an `Idempotency-Key` UUID. A record is scoped to
the user and the canonical operation identity: method, registered operation, and
concrete target IDs. Its request fingerprint includes the resolved wire version,
normalized precondition, other operation-declared semantic inputs, and bounded
body or raw file-part bytes. A matching retry returns the stored response
without executing again. A reused key with a different fingerprint returns `409`
and writes nothing. The record and mutation commit in one transaction.
[ADR 0016](../adr/0016-transactional-idempotency.md) records the mechanism.

Every mutation of an existing resume also requires `If-Match`. Missing is `428`,
malformed is `400`, and stale is `412` with the current revision and document in
`error.details`. `POST /resumes` is the only resume mutation with no prior
revision: it rejects `If-Match` and relies on its idempotency key.

Granular endpoints read the complete aggregate, apply the change, sanitize rich
text, validate the current document, and persist the complete aggregate with
revision CAS. A request declared at an older supported version is down-emitted,
changed at that version, converted back to current, validated, and stored at
current. Values that exist only in the current version are preserved from the
stored aggregate unless the operation explicitly targets their field. This is
required for v1 mutations against a v2 document whose font emits through a v1
fallback. The response is emitted at the declared version. No endpoint performs
an unchecked `jsonb_set` write.

### Structure command ordering

A structure request is one ordered command list. Each command reads the result
of every earlier command in that list. Its `index` is a zero-based insertion
position. For `createSection`, a target column of length `N` accepts
`0 <= index <= N`; `N` appends. For `moveSection`, the service first removes the
key from its current column, then measures the target column and applies the
same inclusive bound. A same-column move therefore measures the shortened
column, and the inserted key's final array index is exactly `index`.

A non-integer index is `400 request_invalid`. An integer outside the applicable
bound is `422 document_invalid`. Any invalid command rejects the complete list;
no intermediate content, layout, or revision change is persisted.

## Public absence and caching

All public routes return the same `404` for a missing, private, deleted,
renamed, tombstoned, or unauthorized resume. Public responses never reveal
account identity. Unpublish, delete, rename, and material publish-state changes
advance the public generation and drain old-generation origin leases before
success. Every affected HTML, markdown, image, PDF, sitemap, and `llms.txt`
reuse then passes the origin live-state gate; retained old bytes cannot be
served. Edge invalidation releases those bytes as defense in depth. ETags and
SSE refetch improve client freshness but are not the revocation authority. Go
holds a per-resume lease through each public response, including a Nuxt SSR
response proxied through Go. Sitemap and `llms.txt` use a separate aggregate
discovery generation; membership-changing mutations drain its old leases too.
