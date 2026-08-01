# 0003 — SSE for live refresh; HTTP PATCH for autosave (no WebSocket)

Status: Accepted (2026-08-01)

## Context

Two realtime needs: (1) autosave while editing; (2) auto-refresh of open
preview/public tabs. WebSocket-first designs were considered for both.

## Decision

- **Autosave = debounced HTTP PATCH only.** At ≤1 save/s, HTTP's semantics
  (status codes, `If-Match`/`412`, idempotency keys, rate limits, logging) come
  free; a WS write path would re-implement all of it and create a second,
  less-tested code path.
- **Refresh = SSE** (`EventSource`): one-way notify → client refetches. Native
  auto-reconnect with `Last-Event-ID`; plain HTTP through CloudFront/Caddy with
  heartbeats. Fallback ladder: conditional polling (`If-None-Match`), then full
  reload only on client/doc schema mismatch. Events are invalidation signals,
  never data carriers; reconnect always refetches (Postgres `NOTIFY` is not
  durable).

## Consequences

- No bidirectional channel exists; true collaborative editing (CRDT/OT) would be
  a new, separate decision.
- SSE streams need heartbeats, connection caps, and slow-client eviction
  (specified in the design spec §8).
