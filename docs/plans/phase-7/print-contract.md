# Phase 7 private print contract

This contract joins Go snapshot preparation, one-use redemption, and Nuxt SSR.
ADR 0023 remains the authority. No private print endpoint appears in the public
OpenAPI operations.

## Frozen payload

The response is one closed JSON object, with exactly these six fields:

```ts
interface PrintEnvelope {
  version: 1;
  resumeId: string;
  revision: string;
  publicGeneration: string | null;
  lng: string;
  document: PublicResumeDocument;
}
```

- `resumeId` is a canonical nonzero UUID. `revision` is a positive int64 decimal
  string with no leading zero. Public generation equals revision for a public
  job and is `null` for an owner job.
- `lng` is the server's canonical BCP 47 projection, with `und` for invalid
  legacy values. Its limit is 35 characters.
- `PublicResumeDocument` is the existing OpenAPI type, at generated current
  schema version 2. It contains only visible fields and entries. It has no
  account ID, title, storage key, slug, or publish controls. Private print
  validation permits an empty content map because owners may export unfinished
  drafts. The public document validator keeps its existing nonempty-content
  rule. Empty sections are removed by the Go projection.
- A photo URL is absent when the snapshot has no photo. Otherwise its only
  accepted form is canonical base64 in `data:image/jpeg;base64,...` or
  `data:image/png;base64,...`, with at most 2,097,152 decoded bytes. Preserve
  the validated crop. A photo reference with unavailable bytes fails the job.
- Go re-sanitizes rich text before this boundary. Nuxt uses the existing SSR
  sanitizer boundary and cannot import the client DOM sanitizer.
- Maximum UTF-8 JSON bytes: 3,407,872. Reject duplicate keys, extra keys,
  non-current schema, malformed UUID/revision/language/photo, and overflow.
- Queue `Snapshot` metadata must equal this envelope. The queue computes its
  digest over the exact frozen payload bytes.

## Private redemption

Nuxt sends `POST /internal-render/print/redeem` to its configured direct Go
listener. The public API router never registers this endpoint.

```http
Authorization: RenderCapability <43-character unpadded base64url token>
X-Render-Job-ID: <canonical nonzero UUID>
Content-Type: application/json

{"resumeId":"<canonical nonzero UUID>","audience":"nuxt-print"}
```

The body limit is 128 bytes. Reject query strings, cookies, extra or duplicate
headers, duplicate or unknown JSON fields, body compression, and invalid media
types before consuming authority. Do not forward any viewer header. A successful
redemption returns the exact frozen payload as `application/json`, with its
length and `Cache-Control: no-store`. There is no completion endpoint.

All malformed, absent, expired, replayed, and wrongly bound authority returns
the same 404 JSON bytes and no detail. Unsupported methods return 405 with
`Allow: POST`. Every failure is `no-store`. A canceled request cannot leave a
detached operation. The read, request, and response deadline is five seconds,
bounded further by job cancellation.

Development uses `127.0.0.1:20082`, the native HTTPS harness uses
`127.0.0.1:20445`, and same-task deployment uses `127.0.0.1:8081`. The HTTP
self-hosting Compose stack uses the render-network Go address `10.91.0.2:8081`.
The listener is never host-published or put on a trusted-proxy network. Nuxt
accepts only these exact configured origins. Tests inject a loopback transport.

## Nuxt route and SSR

`GET /print/<canonical resume UUID>` accepts exactly one `Authorization` and one
`X-Render-Job-ID` header. Reject query strings, cookies, body, malformed paths,
missing authority, and other methods. The route never falls back to sessions or
an ID lookup. Redact authority from all errors and logs.

The Nuxt handler redeems once, checks the returned resume ID against the path,
then passes only the decoded frozen envelope to its isolated SSR worker. The
worker receives no token, HTTP client, database, cookie, or account context. It
renders the shared resume component tree in continuous mode. Insert the
renderer-owned `renderPageRule` for the document's A4/Letter geometry.

Return a script-free HTML document with the resume language, static local font
and print CSS links, a fixed title `Resume`, and no hydration or application
chrome. Mark the root `data-print-document="true"` and its exact revision.
Chromium's controller waits for fonts and image decode and marks readiness. The
HTML limit is 6,291,456 bytes, including the inline photo. SSR has a five-second
deadline and cancellation terminates and joins the worker.

The response uses `Cache-Control: no-store`, `X-Content-Type-Options: nosniff`,
`Referrer-Policy: no-referrer`, and a restrictive content security policy:
`sandbox allow-same-origin; default-src 'none'; script-src 'none'; style-src 'self' 'unsafe-inline'; font-src 'self'; img-src data:; connect-src 'none'; frame-src 'none'; worker-src 'none'; child-src 'none'; media-src 'none'; manifest-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'`.
Do not permit remote URLs or arbitrary same-origin API requests. Caddy's
existing print-root denial remains unchanged.

## Output and cache limits

PDF output is provisionally capped at 16,777,216 bytes. PNG output is capped at
4,194,304 bytes, enough for the fixed viewport without relying on compression.
The public cache gains a 33,554,432-byte total payload cap in addition to its
existing entry count and 60-second lifetime. These limits require corpus and
whole-task memory measurements before phase exit.

Owner PDF render admission is at most 10 per minute per account and per client
IP. Public PDF and image render admission share at most 20 per minute per client
IP. Apply expensive-render limits to cache misses, before reserving a queue
slot; the existing global request limits still apply to every request. Use the
bounded ADR 0018 limiter. Rejections are 429 with `Retry-After`; queue
saturation is 503.
