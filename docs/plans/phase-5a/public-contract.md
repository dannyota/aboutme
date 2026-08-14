# Phase 5A public wire contract

Status: **Approved — ready for implementation planning.**

This document owns Phase 5A HTTP shapes, public projections, response families,
conditional requests, and validators. The [public formats](public-formats.md)
own render input and exact representation formats. The [core design](design.md)
owns domain policy. The [revocation protocol](revocation.md) owns admission and
mutation ordering.

## Owner publish request

The endpoint is:

```http
POST /api/v1/resumes/{id}/publish
```

It requires a valid owner session, CSRF and exact Origin checks, one
`If-Match: "r<revision>"`, one UUID `Idempotency-Key`, JSON Content-Type, and
the normal optional `X-Resume-Schema-Version` header.

The closed request object is:

```json
{
  "slug": "ada-lovelace",
  "live": true,
  "downloadEnabled": true,
  "seoGeoEnabled": false
}
```

`live`, `downloadEnabled`, and `seoGeoEnabled` are required booleans. `slug` is
an optional string; omission preserves stored state. `null`, empty string,
unknown fields, duplicate fields, trailing data, and wrong types are
`400 request_invalid`. Effective `live=true` requires a slug. `live=false`
requires `seoGeoEnabled=false` and may retain `downloadEnabled=true`.

The resolved wire version, normalized precondition, and exact accepted body
bytes enter the idempotency fingerprint. Exact replay returns the stored
response. Changed input under the same key returns `409 idempotency_key_reuse`.

Fresh success is `200 application/json; charset=utf-8` with the normal
`{"data":Resume}` envelope, new parent ETag, and `X-Resume-Schema-Version`.
`ResumeSummary` and `Resume` contain required `downloadEnabled` and
`seoGeoEnabled` booleans.

Errors are:

| Status | Code                    | Condition                                                   |
| ------ | ----------------------- | ----------------------------------------------------------- |
| `400`  | existing request codes  | Malformed path, body, header, precondition, key, or version |
| `401`  | `session_required`      | No valid owner session                                      |
| `403`  | `csrf_rejected`         | CSRF or exact-origin chain failed                           |
| `403`  | `reauth_required`       | Fresh rename would release a slug without recent auth       |
| `404`  | `resume_not_found`      | Missing and wrong owner are identical                       |
| `409`  | `slug_taken`            | Current claim or unexpired tombstone                        |
| `409`  | `idempotency_key_reuse` | Same key, different fingerprint                             |
| `412`  | `revision_mismatch`     | Stale CAS with winning revision and document                |
| `422`  | `publish_invalid`       | Effective state, slug, or completeness failed               |
| `428`  | `precondition_required` | Missing `If-Match`                                          |
| `429`  | `rate_limited`          | Write or dedicated slug-attempt policy                      |
| `503`  | `public_state_busy`     | Required cancellation did not drain in five seconds         |
| `500`  | `internal_error`        | Safe internal or unresolved-outcome response                |

`public_state_busy` sends `Retry-After: 1` and means no mutation transaction
began. `publish_invalid` has outer message `resume cannot be published` and
returns every applicable issue from this closed catalog:

| Condition                             | Path                                                   | Code                     | Message                                                              |
| ------------------------------------- | ------------------------------------------------------ | ------------------------ | -------------------------------------------------------------------- |
| Live without effective slug           | `slug`                                                 | `required_for_live`      | `slug is required when live is enabled`                              |
| Discovery without live                | `seoGeoEnabled`                                        | `requires_live`          | `discovery requires live to be enabled`                              |
| Nonempty slug fails length or grammar | `slug`                                                 | `invalid_format`         | `slug must be 4 to 30 characters and match ^[a-z0-9]+(-[a-z0-9]+)*$` |
| Slug is a fixed public root           | `slug`                                                 | `reserved`               | `slug is reserved`                                                   |
| Full name is blank                    | `personalDetails.fullName`                             | `required`               | `full name is required for publication`                              |
| No visible entry                      | `content`                                              | `visible_entry_required` | `at least one visible entry is required`                             |
| Required entry field is blank         | `content.<sectionKey>.entries[<stored-index>].<field>` | `required`               | `field is required for publication`                                  |

Issues sort by UTF-8 path bytes, then code, then message; an identical triple
appears once. Independent semantic issues do not short-circuit each other.
Missing required booleans, `slug:null`, `slug:""`, unknown or duplicate fields,
wrong JSON types, and trailing data are request-shape failures and remain
`400 request_invalid`. A present nonempty slug with wrong length or grammar, a
reserved slug, invalid flag relations, and completeness failures are semantic
`422 publish_invalid`. A valid but unavailable slug remains `409 slug_taken`.

## Resume delete extension

Existing `DELETE /api/v1/resumes/{id}` keeps its zero-length body fingerprint
and P2B request contract. Request processing order is exact:

1. Authenticate, enforce CSRF/Origin, validate path and singleton headers,
   resolve wire version, and derive the operation identity and fingerprint.
2. Read the retained idempotency record under the existing per-user
   serialization.
3. An exact committed replay returns immediately. It runs before recent-reauth
   freshness and before any fence work.
4. A record with another fingerprint returns `409 idempotency_key_reuse`.
5. Only a fresh execution checks recent reauthentication and continues through
   preflight, revocation, and the deletion transaction.

This order keeps a committed deletion replay stable after the 15-minute reauth
window expires. It does not bypass session, CSRF, request syntax, or fingerprint
checks.

Fresh execution adds `403 reauth_required` when a stored slug would be released
and `503 public_state_busy` when required leases do not drain. It retains every
existing P2B error. Success and exact replay are identical `204` responses with
zero bytes and no `Content-Type`, `ETag`, `Location`, or
`X-Resume-Schema-Version`. The authenticated outer policy still sends
`Cache-Control: no-store, no-transform`.

The transaction tombstones the current slug, enqueues the exact current photo
key for deletion, advances discovery generation when required, deletes the
resume under CAS, and stores that exact bodyless result. The
[revocation protocol](revocation.md#resume-delete) fixes rollback and ambiguous
outcomes.

## Public JSON projection

`PublicResume` is a dedicated OpenAPI schema, not an owner `Resume`. Its closed
shape is:

```text
PublicResume {
  slug: string
  revision: decimal string
  lng: canonical BCP 47 string
  downloadEnabled: boolean
  document: PublicResumeDocument
}
```

`PublicResumeDocument` retains `schemaVersion`, `personalDetails`, `content`,
and `customization`. Its rules are:

- Account ID, server-owned resume ID, owner title, publish flags other than the
  exposed download choice, and timestamps are absent.
- Document-local contact and entry UUIDs and section map keys remain. They are
  required for deterministic schema/renderer behavior and grant no public lookup
  capability.
- Hidden contacts, contacts with empty `value`, and hidden entries are removed.
  Their `isHidden` members are not emitted. Sections left with no visible entry
  are removed from `content` and both layout arrays.
- Other draft-optional fields retain their stored absence versus empty value.
- Present photo becomes closed `{url:string,crop?:PhotoCrop}`. `url` is exactly
  `<canonicalOrigin>/api/v1/public/resumes/<slug>/photo`, with the validated
  ASCII slug inserted unchanged and no query or fragment. The storage `key`
  never appears.
- Rich text is current-version Go-sanitized immediately before projection.

`personalDetails.details[]` therefore contains document-local `id`, `type`,
optional `label`, and `value`. Public entry objects retain document-local `id`
and type-specific public fields but omit `isHidden`. Section display metadata,
entry order, customization, and current schema version remain unchanged.

The JSON success body is the standard envelope with one trailing LF. Object keys
serialize in their declared schema order; `content` keys retain bytewise sorted
order for deterministic output. No map iteration order is observable.

## Public route matrix

| Route                                 | Success type                       | Gate                             |
| ------------------------------------- | ---------------------------------- | -------------------------------- |
| `/api/v1/public/resumes/{slug}`       | `application/json; charset=utf-8`  | live                             |
| `/api/v1/public/resumes/{slug}/photo` | stored `image/jpeg` or `image/png` | live and current photo reference |
| `/{slug}`                             | `text/html; charset=utf-8`         | live                             |
| `/{slug}.md`                          | `text/markdown; charset=utf-8`     | live and discovery enabled       |
| `/sitemap.xml`                        | `application/xml; charset=utf-8`   | global discovery generation      |
| `/robots.txt`                         | `text/plain; charset=utf-8`        | valid canonical-origin config    |
| `/llms.txt`                           | `text/plain; charset=utf-8`        | global discovery generation      |
| `/internal-render/public`             | `text/html; charset=utf-8`, direct | strict private POST              |

Public routes accept only GET and HEAD. A wrong method returns `405`, the
route-family error body below, and exact `Allow: GET, HEAD` before any resource
lookup. The internal route accepts only POST on the direct listener; Caddy
returns its generic public `404` without proxying every viewer method.

HEAD performs the same gate, conditional evaluation, representation selection,
and bounded generation as GET. It sends the same status and headers, including
the GET body's `Content-Length`, but suppresses all body bytes. This applies to
success and error responses.

Within each dynamic route, unknown, never-published, private, renamed, deleted,
tombstoned, wrong-target, and flag-disabled state returns the same `404`, header
set, and route-family body. There is no `410`. An admitted dependency failure or
transition mismatch returns `503` with `Retry-After: 1`; it never falls back to
stale bytes. A public response never sends `Set-Cookie`.

A dynamic path segment that does not satisfy the slug grammar is the same
route-family `404`; it is not a public `400` oracle.

Successful HTML with discovery disabled sends exact
`X-Robots-Tag: noindex, noarchive`. With discovery enabled, the Phase 5A origin
omits that per-resume header and includes the JSON-LD defined by the public
formats. An environment-wide edge noindex policy remains additive. Discovery
disabled also keeps markdown at the dynamic-route `404` and excludes the slug
from sitemap and `llms.txt`.

JSON-family errors apply to public JSON and photo. They use
`application/json; charset=utf-8` and exact standard encoding plus one LF:

| Status | Code                      | Message                           |
| ------ | ------------------------- | --------------------------------- |
| `400`  | `request_invalid`         | `request is invalid`              |
| `404`  | `public_not_found`        | `public resume not found`         |
| `405`  | `method_not_allowed`      | `method is not allowed`           |
| `503`  | `temporarily_unavailable` | `service temporarily unavailable` |

For example, JSON-family `404` is exactly:

```text
{"error":{"code":"public_not_found","message":"public resume not found"}}
```

The actual body includes one LF after `}`.

HTML-family errors use `text/html; charset=utf-8`. `{label}` is exactly
`Bad request`, `Not found`, `Method not allowed`, or `Temporarily unavailable`
for `400`, `404`, `405`, or `503`. The bytes are this UTF-8 template with LF
line endings and one final LF:

```text
<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <title>{label}</title>
  </head>
  <body>
    <a href="#main">Skip to content</a>
    <main id="main"><h1>{label}</h1></main>
  </body>
</html>
```

Text-family errors apply to markdown, sitemap, robots, and `llms.txt`, use
`text/plain; charset=utf-8`, and are exact `Bad request.\n`, `Not found.\n`,
`Method not allowed.\n`, or `Service temporarily unavailable.\n`. Aggregates
have no ordinary `404`; private resumes are absent from their successful body.

Every `404`, `405`, and `503` sends `Cache-Control: no-cache, must-revalidate`
and no ETag. Security and robots headers remain route-appropriate. A `400`
conditional-header rejection uses the same cache policy and no ETag.

## Conditional request grammar

Method dispatch and the applicable live/discovery gate precede `If-None-Match`
parsing. A private route therefore remains `404` even when the header is
hostile. After admission, every public representation accepts either no header
or exactly one strong entity-tag header field.

The accepted grammar mirrors the owner-photo parser:

- exactly one `If-None-Match` field value;
- first and last byte are `"`;
- each interior byte is at least `0x21`, is not `0x7f`, and is not `"`;
- an empty opaque value is syntactically valid; and
- no leading/trailing whitespace, comma, `W/`, wildcard `*`, or second value.

Repeated fields, comma-folded lists, weak tags, wildcards, malformed quotes,
control bytes, and whitespace fail closed with route-family
`400 request_invalid`. The parser never accepts one valid member from a
malformed list. A valid nonmatching tag returns full `200`; an exact current
match returns `304`.

A `304` has zero body and no `Content-Length`. It retains successful
`Cache-Control`, ETag, Content-Type, CSP/security, and robots headers. The
applicable lease remains held through header completion.

## Strong validators

Every successful public representation that carries an ETag uses the same origin
rule. Go computes SHA-256 over the exact unencoded selected response body and
emits the lowercase 64-hex digest inside quotes:

```text
"<64 lowercase hex characters>"
```

There is no domain, revision, generation, representation, format, build, or
content-type prefix in the ETag input or opaque value. Equality therefore means
the selected unencoded bodies are byte-identical. HEAD uses the exact GET body
it would have sent. Photo uses the exact normalized image bytes under the same
rule. Go buffers the bounded body once unless it already has a trusted SHA-256
digest of those exact bytes. A key-derived or object-store-provider ETag is not
a byte validator.

Caddy v2.11.4 appends `-gzip` or `-zstd` inside the quoted viewer ETag when it
applies that content encoding. It strips the recognized encoding suffix from a
viewer `If-None-Match` value before upstream revalidation. Go emits and compares
only the unsuffixed 64-hex tag; it never embeds Caddy's suffix. Route-parity
tests pin response suffixing, request stripping, compressed-body variance, and
identity fallback for both encodings.

Public responses are transformable and intentionally omit `no-transform`, so
Caddy may encode eligible bodies and owns that suffix boundary. Authenticated
API responses keep the design-wide exact
`Cache-Control: no-store, no-transform`; Caddy does not apply the public
transformation or suffix contract to them.

A private cache key contains route class, representation and variant, stable
resume ID when resume-scoped, the database resume revision or discovery
generation, immutable format version, application build digest, and renderer
build digest when rendered. A deploy therefore rerenders even if database state
is unchanged. The ETag remains derived only from the resulting exact body.
Private entries live at most 60 seconds.

All successful public responses send `Cache-Control: no-cache, must-revalidate`.
Edge retention is at most 60 seconds with minimum/default TTL zero. The
applicable gate runs before cache selection or conditional comparison.
