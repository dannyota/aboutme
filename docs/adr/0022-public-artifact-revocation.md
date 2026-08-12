# 0022 — Public artifacts pass a live-state gate before reuse

Status: Proposed (2026-08-12)

## Context

An ordinary 60-second public cache can serve a resume, photo, PDF, generated
image, markdown document, or discovery entry after its resume is unpublished,
deleted, or renamed. Server-Sent Events help an open browser converge, but they
do not protect crawlers, disabled-JavaScript clients, or a new request that hits
a shared cache. Best-effort invalidation also cannot define a privacy boundary.

The service cannot retract bytes a viewer already received. It can ensure that
no request admitted after a successful state change receives an artifact from
the old public state.

## Decision

Every public representation passes an origin live-state gate before a stored or
computed artifact is reused. The gate reads the current resume state and checks
the requested slug, `live`, discovery and download flags, and the committed
resume revision used as the public generation. Every publish-state mutation
increments that revision. A cache hit is never authorization.

An ordinary public response uses `Cache-Control: no-cache, must-revalidate` and
a strong entity tag. Every applicable CloudFront behavior sets minimum and
default TTL to zero and maximum TTL to 60 seconds. CloudFront may retain a
response for that bounded period, but it must send a conditional request through
the origin gate before every reuse. No request may be answered under positive
freshness. Private origin render caches may also retain generation-keyed
artifacts for at most 60 seconds. Their key includes resume ID, public
generation, representation, and variant. The gate selects only the current
generation, so an old artifact cannot become visible again after revocation.

Public reads hold a generation lease through the origin response. Unpublish,
delete, and rename take the exclusive per-resume revocation fence, close
old-generation origin admission, cancel every old-generation render and origin
response through its lease-owned context and connection, and wait at most five
seconds for those handlers to exit. If cancellation does not finish within that
hard bound, the mutation fails before its database transaction, reopens the
unchanged old generation, and reports no success. Lease expiry alone never
permits origin bytes to continue. After a successful drain, the mutation commits
and opens the new generation. A database begin failure, definite statement or
commit rollback, canceled request before commit, or other definitely pre-commit
failure also reopens the unchanged old generation; an ambiguous commit keeps
admission closed while Go reads the committed row and generation under the
fence, then opens only the matching generation. If that read cannot resolve the
outcome, readiness fails and public admission stays closed until recovery. After
success:

- the old slug and every representation disabled by the new state return the
  ordinary indistinguishable public `404`;
- a shared cache must revalidate and cannot serve its retained old body; and
- a new renderer or media read cannot select the old generation.

Go owns the origin response while a lease is active. Caddy routes a public
resume HTML request to the Go public-artifact gateway. Under the per-resume
fence, Go validates state, acquires the generation lease, freezes the render
snapshot, and sends it with `POST /internal-render/public` directly to Nuxt's
origin listener. The route accepts one bounded current-version snapshot and
performs no database, Go API, cookie, ID lookup, or public-state lookup. It can
render only the bytes in that request. Caddy denies `/internal-render` and
`/internal-render/*` before its default Nuxt proxy, so a viewer cannot reach the
route through the public origin. Go uses the direct configured Nuxt origin
address, never the viewer-facing Caddy address. Nuxt returns rendered bytes to
Go; it does not write the origin response. Go releases the lease only after the
origin body finishes or its connection closes. A Nuxt failure aborts the
response before release. If the Go process dies, its origin sockets and
mutations die with the in-memory fence, so a replacement process cannot report
mutation success while an old response from that process continues. Scaling
these roles independently requires the distributed fence named below.

CloudFront owns delivery after an origin response has completed. A viewer
request admitted and origin-validated before revocation may therefore finish
after mutation success; those bytes were already admitted under the old state.
Go cannot close that edge-to-viewer connection. Every request first admitted or
revalidated after mutation success must pass the new origin state and cannot
receive the old representation.

Aggregate discovery uses a separate global discovery generation and fence. Its
monotonic generation is stored in PostgreSQL, not inferred from process memory.
`/sitemap.xml` and `/llms.txt` contain only the protocol-fixed heading and the
ordered set of eligible canonical public URLs. They contain no title, name,
`lastmod`, resume document field, or other mutable resume value. Both routes
acquire the committed generation, read one eligible-resume snapshot ordered by
slug, and hold the lease through the Go origin response. A mutation that changes
slug, live state, discovery eligibility, or deletion takes the global discovery
fence first, then every affected per-resume fence in ascending resume UUID byte
order. This same order covers account deletion of up to three resumes. It closes
old admission and cancels and drains every lease set under one five-second
bound. It then increments the durable discovery generation in the same
PostgreSQL transaction as the membership change. After commit, both fences open
at the committed generations. Cache keys and entity tags include the applicable
generation. Startup loads the durable value before Go becomes ready, so a crash
or restart cannot reuse an old generation or validate stale aggregate bytes. Any
drain failure or definite pre-commit/rollback database failure reopens the
global fence and each affected resume fence at the unchanged committed
generations; an ambiguous commit follows the fail-closed resolution rule above.
Success retires or opens each affected fence at its committed state.

Edge invalidation still runs as defense in depth and to release retained bytes,
but mutation success does not depend on an eventually consistent invalidation.
Open clients use SSE and an uncached refetch for prompt UI convergence; that is
not the revocation authority.

## Consequences

- Unpublish, delete, and rename have immediate public effect at the cost of an
  origin authorization check for every public reuse.
- The edge may store public bytes and answer a validated `304`, but it cannot
  absorb a request without revalidation. This trades some origin load and
  latency for a clear privacy guarantee.
- Rendering and immutable media bytes can still use a bounded generation-keyed
  cache behind the gate.
- Concurrency tests must cover a cached old response and a request admitted at
  the edge before revocation, an in-flight proxied Nuxt response, aggregate
  sitemap and `llms.txt` responses, rename of both slugs, a stalled Nuxt render,
  slow and aborted viewer bodies, the five-second fail-before-commit path with
  public readability restored, database rollback and ambiguous commit,
  multi-resume account deletion lock order, a non-membership edit that leaves
  discovery bytes and generation unchanged, connection abort, and invalidation
  failure.
- Route-parity tests must prove public Caddy denies both internal-render paths,
  Go calls the direct Nuxt origin, the internal handler is POST-only and
  bounded, and it has no ambient cookie, ID lookup, database, or Go API path.
- Scaling beyond the v1 single-node application tier requires a distributed
  revocation fence or a replacement ADR.
