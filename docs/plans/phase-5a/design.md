# Phase 5A publish and public SSR design

Status: **Approved — ready for implementation planning.**

## Document authority

This core design owns Phase 5A scope, domain policy, relational state, and trust
boundaries. Three linked documents own details without restating them here:

- [Public contract](public-contract.md) owns HTTP responses, projections, and
  validators.
- [Public formats](public-formats.md) owns internal-render input and budgets,
  markdown and discovery bytes, and the closed JSON-LD form.
- [Revocation](revocation.md) owns generation transitions, leases, lock order,
  transaction outcomes, recovery, and concurrency acceptance.

Approved product design and ADRs remain higher authority. Implementation stops
if these phase documents disagree with them.

## Purpose and scope

Phase 5A publishes one resume without making its owner or other resumes public.
It adds:

- owner publish controls and publish-time completeness;
- slug claim, rename, retention, and tombstones;
- live-gated public JSON and normalized photo reads;
- Go-controlled public HTML rendered by Nuxt;
- discovery-gated markdown and aggregate discovery;
- the generated public-root registry consumers;
- durable discovery generation and in-process revocation fences; and
- public-read sanitizing and production public-page security headers.

It consumes Phase 2B's write-safety, compare-and-swap (CAS), and private-media
contracts and Phase 3's projector, sanitizer, renderer, fonts, and CSP.

It excludes the publish dialog, Server-Sent Events (SSE), PDF and images,
account deletion and privacy workers, direct storage URLs, cloud resources, and
the full port-443 UAT deployment. Existing resume deletion is extended only as
needed to release its slug, revoke public access, and preserve media cleanup.

## Approved representation decisions

Discoverable HTML contains the one deterministic JSON-LD script defined in
[the public formats](public-formats.md#json-ld-and-csp). Its response-specific
CSP adds only the SHA-256 source for that script's exact text bytes to the Phase
3 policy. Nondiscoverable HTML has neither JSON-LD nor that source. No script
nonce or `script-src 'unsafe-inline'` is allowed.

Every public strong validator uses the exact-body rule in
[the public contract](public-contract.md#strong-validators). Go emits the quoted
lowercase SHA-256 digest of the unencoded response bytes. Caddy may suffix the
viewer validator for its selected content encoding; the suffix is not part of
Go's validator.

These four documents are fixed input to the implementation plan. The landed
[numeric budget authority](../budgets.md#provenance-of-the-p5a-rows) owns all
Phase 5A bounds used here.

## Publish state

`POST /api/v1/resumes/{id}/publish` is an authenticated resume mutation. Its
exact wire contract is in
[the public contract](public-contract.md#owner-publish-request). It uses the
existing session, CSRF, Origin, schema-version, CAS, idempotency, rate, and
no-oracle boundaries.

The requested booleans replace all three stored controls. An omitted slug
preserves the stored value. Going private preserves the slug and may preserve
the download choice, but requires discovery off. Effective live state requires a
slug.

Every accepted fresh request increments resume revision once, including a
repeated-value request. Its idempotency result commits with the state. Owner
resume summaries and full responses add required `downloadEnabled` and
`seoGeoEnabled` fields; current OpenAPI omits them and must be corrected before
handler work.

## Publish completeness

Draft writes remain permissive. Completeness runs only when effective
`live=true`. Unpublish and private control changes do not require completeness.

Nonblank means a code point remains after trimming Unicode whitespace. Profile
rich text is first sanitized, decoded to text with element boundaries treated as
whitespace, then trimmed. Markup such as `<p><br></p>` is blank.

An entry is visible when `isHidden` is absent or false. Hidden entries never
block publication. Publication requires a nonblank full name, at least one
visible entry, and these nonblank fields on every visible entry:

| Section type        | Fields                 |
| ------------------- | ---------------------- |
| `profile`           | `text`                 |
| `work`              | `jobTitle`, `employer` |
| `education`         | `degree`, `school`     |
| `skill`, `language` | `name`                 |
| `certificate`       | `title`                |
| `project`, `custom` | `title`                |

Validation reports all deterministic issues, preserves absent versus cleared
fields, and writes nothing on failure. The public contract fixes response paths
and codes.

## Slug and tombstone policy

An effective slug is lowercase ASCII, 4–30 characters, matches
`^[a-z0-9]+(-[a-z0-9]+)*$`, and is absent from the generated public-root
registry. Claims serialize on a transaction-level per-slug advisory lock. A
rename locks old and new slug keys in bytewise order before reading either
claim, preventing cross-rename deadlock. Hash collisions may serialize unrelated
slugs but cannot weaken uniqueness.

The lock key is PostgreSQL `hashtextextended('aboutme.slug.v1:' || slug, 0)`
passed to `pg_advisory_xact_lock(bigint)`. Callers order the original slug
strings, not their hashes. Every claim, rename, and release uses this exact
domain and seed.

Unpublish keeps its slug. Initial claim assigns a previously absent slug. Rename
atomically claims the new slug and inserts a tombstone for the old slug. Resume
deletion inserts the same tombstone in its deletion transaction. Rename and
deletion require recent reauthentication because they release a link; initial
claim and unpublish do not.

A tombstone stores slug, `released_at`, and the releasing user when available.
It blocks claims before `released_at + 180 days`. Claim is allowed at that exact
instant. Under the slug lock, a successful claim atomically deletes the expired
tombstone and assigns the slug. Rollback restores both states.

Once that claimant later releases the slug, the same transaction inserts a new
tombstone with a new release time. It never updates or refreshes an existing
tombstone on conflict. Concurrent reclaim has one winner; the loser sees
`slug_taken`. Concurrent `a→b` and `b→a` renames lock both names in the same
order, then fail normally while either current claim exists. An exact replay of
a release returns its stored result and does not insert again. A fresh repeated
release fails CAS or ownership/state checks rather than changing the original
release time.

Tombstone existence, age, and former owner never cross a public response.

## Relational public state

Phase 5A adds one checked singleton `public_state` row with a positive monotonic
`discovery_generation`. Startup loads it before readiness.

A publish transaction atomically changes flags and slug, increments revision,
writes a tombstone when needed, advances discovery generation when required, and
stores the idempotency result. Resume deletion atomically tombstones its slug,
enqueues its exact current photo deletion job, advances discovery generation,
deletes the row, and stores its bodyless result.

Discovery eligibility is exactly:

```text
slug IS NOT NULL AND live = true AND seo_geo_enabled = true
```

Generation advances for a slug, `live`, or `seo_geo_enabled` change and deletion
of a slug-bearing resume. Content, language, owner title, photo, crop, and
download-only changes leave it unchanged. Discovery queries read only eligible
slugs in bytewise order; they do not read names, revisions, timestamps, or
document JSON.

## Resume deletion extension

The existing `DELETE /api/v1/resumes/{id}` keeps its P2B method, fingerprint,
CAS, media-job, and `204` result. Phase 5A adds recent reauthentication for a
fresh slug release, tombstoning, discovery generation, and the applicable
resume/global transition. Exact replay ordering and response bytes are fixed in
[the public contract](public-contract.md#resume-delete-extension). Fence,
rollback, and ambiguity behavior are fixed in
[the revocation protocol](revocation.md#resume-delete).

## Public data and media privacy

Public transport contains public resume content, not account data or a list of
other resumes. Only the account ID and server-owned resume ID are removed for
identity privacy. Document-local entry/contact UUIDs and section keys remain:
the generated schema and renderer require them, and no public endpoint accepts
them as lookup authority.

Hidden contacts, empty-valued contacts, and hidden entries are removed. A
section left empty after filtering is removed from content and layout. The
private photo key is always removed. The browser projection carries an
authorized same-origin URL and crop. The private internal renderer receives the
same public projection and adapts photo presence with fixed non-storage marker
`public-render-photo`; Nuxt never receives the object key.

Object storage remains private. Go verifies current slug, live state,
generation, and current photo reference before reading an immutable key. It
never redirects, signs a storage URL, accepts a request key, or treats object
existence as authority.

## Go-controlled public SSR

Go owns public admission, the lease, cache choice, direct render request,
headers, and viewer response. Nuxt only renders the bounded public snapshot. The
exact body and bounds are in
[the public formats](public-formats.md#internal-public-render-contract).

Go projects current schema, re-sanitizes every rich-text field, removes hidden
and private data, authorizes a photo URL, and calls exact
`POST /internal-render/public` on the direct Nuxt listener. Nuxt has no cookie,
ID lookup, database, Go API, public fetch, or ambient network capability. Caddy
denies `/internal-render` and `/internal-render/*` before its default handler.

Go validates internal status, type, size, and accepted security result before
sending success. It releases the lease only after the viewer response finishes
or aborts. Failure never falls back to direct public Nuxt routing or ID-based
client rendering.

## Public routes and registry

Go owns public JSON, photo, markdown, sitemap, robots, and `llms.txt`. Public
HTML reaches Go first and then the direct Nuxt renderer. The exact method,
absence, error, content-type, HEAD, conditional, and byte contracts live only in
[the public contract](public-contract.md#public-route-matrix).

The versioned public-root registry alone generates the Caddy matcher and
internal denial, Go slug reservation set and dispatch fixtures, Nuxt manifest
fixture, and route-parity fixtures. Dynamic resume routes are not fixed-root
entries. Drift among registry, OpenAPI roots, Caddy, Go, or Nuxt fails the
build.

## Numeric budget authority

[The landed budget authority](../budgets.md#provenance-of-the-p5a-rows) fixes:

- the internal-render body, canonical-origin, HTML-response, and deadline values
  with the size proof in
  [the public formats](public-formats.md#internal-render-budgets);
- a dedicated slug claim/rename rate of **30 attempts per account per hour**, in
  addition to the normal resume-write limiter.

The slug policy applies only when the requested slug differs from stored state.
It runs before availability details are exposed and returns the existing
`429 rate_limited`. Thirty attempts allow correction and normal renames while
making the existing 240 writes/minute unsuitable as the sole anti-squatting
control.

## Security, accessibility, and failure boundaries

Go re-sanitizes before public JSON, markdown, or SSR. Public hydration contains
only the public projection. A cache hit, ETag, object key, Nuxt route, or SSE
event is never authority. Public absence states are indistinguishable within a
route. Public reads neither consume nor rotate an owner session.

HTML uses the continuous pure renderer, canonical root language, visible heading
order, underlined links, focus styles, accessible level names, decorative photo
`alt=""`, and renderer color behavior. Its shell has a skip link, one `main`,
one title from the public full name, and a canonical URL for the exact current
slug. It works without JavaScript. Rename never redirects the old slug.

Go is not ready until `public_state` is valid, transition recovery is complete,
PostgreSQL is reachable, and the bounded direct-Nuxt check passes. Liveness does
not depend on them. Nuxt failure fails HTML without bypass. Storage failure
fails photo without exposing a key. Edge invalidation remains retried defense in
depth and never defines mutation success.

Logs may contain request ID, route class, generation, duration, and stable code.
They never contain document content, claim bodies, account data, object keys,
internal snapshots, or idempotency response bodies.

## Traceability boundary

Phase 5A owns AC-PUB-001, AC-PUB-002, AC-PUB-004, and only its HTML/JSON/photo/
markdown slice of AC-PUB-003. PDF, generated images, and SSE remain pending in
their assigned phases. The current AC-PUB-003 `404/410` wording drifts from the
product's uniform public `404`; the row must be corrected without claiming the
later slices.

Phase 5A owns only the public-read slice of AC-SEC-001. Prior sanitizer evidence
remains and later internal-print evidence stays pending. It extends AC-OPS-005
for new routing. AC-OPS-012b remains open for live UAT; its content ETag is the
exact-body validator fixed by this design.

The phase's [revocation acceptance](revocation.md#acceptance-and-race-matrix)
closes its ADR 0022 evidence without rewriting completed phase records.
