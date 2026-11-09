# 0002: Go API, one Vue renderer, HTTP autosave, and SSE refresh

Status: Accepted (2026-08-01, 2026-11-10).

## Context

The resume layout must render the same way in three places: the editor's live
preview, the public page that search engines and AI crawlers read, and the PDF.
Two renderers (for example Go templates and Vue components) guarantee drift.

The editor autosaves while the user types, and open preview and public tabs
should refresh when the resume changes. WebSocket-first designs were considered
for both.

Health checks serve the container platform and edge checks, not API clients. A
later `/api/v2` must never break a check that has no reason to know a version
exists.

## Decision

**One renderer.** The renderer is written once as pure Vue components. Nuxt runs
it in the browser (editor preview) and on the server (public pages). The Go
server owns authentication, the JSON API, and SSE, and produces PDFs and images
by printing an internal Nuxt route with headless Chromium.

**Autosave is debounced HTTP PATCH only.** At about one save per second, HTTP
gives status codes, `If-Match` and `412`, idempotency keys, rate limits, and
logging for free. A WebSocket write path would re-implement all of it as a
second, less-tested path.

**Refresh is Server-Sent Events.** `EventSource` carries one-way notifications
and the client refetches. It reconnects natively with `Last-Event-ID` and runs
as plain HTTP through the edge and Caddy with heartbeats. The fallback ladder is
conditional polling (`If-None-Match`), then a full reload only on a client or
document schema mismatch. Events are invalidation signals, never data carriers;
a reconnect always refetches, because PostgreSQL `NOTIFY` is not durable.

**Health routes sit at the site root.** `/healthz` and `/readyz` are served at
`https://aboutme.vn/healthz` and `/readyz`, outside `/api/v1`, like
`/sitemap.xml`, `/robots.txt`, and `/llms.txt`.

- `/healthz` is liveness only and never touches the database, so a database
  outage cannot restart-loop the container.
- `/readyz` is readiness. It checks the database and render queue saturation and
  returns `503` with an error envelope when not ready. It gates traffic
  admission, not liveness.
- OpenAPI gives these two operations a path-level `servers` override (root
  `https://aboutme.vn`).

## Alternatives rejected

- Separate Go and Vue renderers drift across preview, public pages, and PDF.
- WebSocket autosave duplicates HTTP preconditions, idempotency, limits, and
  logs.

## Consequences

- Node runs in production beside Go (two app services and Caddy).
- Preview, public page, and PDF share one renderer, so content and structure
  match, and preview page breaks match the PDF. Text measurement still differs
  slightly: a word within about 0.4 px of a line edge can wrap differently, so
  the editor preview may differ from the PDF by up to two line breaks in the
  cases `apps/web/e2e/preview-gap-expected.json` lists. Chromium print is
  authoritative. Golden snapshots, display checks, and the preview-gap ceilings
  protect this.
- No bidirectional channel exists. Collaborative editing (CRDT or OT) would be a
  new decision.
- SSE streams need heartbeats, connection caps, and slow-client eviction; the
  [realtime design](../design/realtime.md) states them.

## History

Consolidates former ADRs 0002 (renderer split), 0003 (SSE and HTTP autosave),
and 0007 (unversioned health endpoints), all accepted 2026-08-01 and unchanged
in substance.

On 2026-11-10 the owner accepted up to two preview line breaks that differ from
the PDF, because no shared renderer change removed the measured drift; Context
and Consequences now say "the same way" instead of "identically".
