# 8. Realtime

Realtime has separate write and read paths. Autosave remains ordinary HTTP;
Server-Sent Events (SSE) carry invalidation only.

## Writes

The editor updates its local document and preview immediately. After about one
second of inactivity, it coalesces the pending change into one granular HTTP
mutation. HTTP retains preconditions, idempotency, status codes, request logs,
and route-specific rate limits. There is no WebSocket write path to keep in
sync. [ADR 0003](../adr/0003-sse-over-websocket.md) records the choice.

## Refresh ladder

| Rung | Mechanism                                     | Trigger                                   |
| ---- | --------------------------------------------- | ----------------------------------------- |
| 1    | SSE notification followed by document refetch | Default                                   |
| 2    | Conditional ETag polling every 30–60 seconds  | Repeated SSE failure or buffering proxy   |
| 3    | Full page reload                              | Client cannot render the document version |

An event contains identity and revision metadata, never resume content. The
client refetches the public or preview document and renders in place without
resetting scroll or editor state.

## Delivery semantics

PostgreSQL `NOTIFY` fans out an invalidation to each Go task's local hub. It is
not durable. `Last-Event-ID` supports deduplication and observability, not
replay after process loss. Every initial connection and reconnect performs an
unconditional refetch, which repairs a notification missed during restart.

The server sends a heartbeat every 25 seconds. It caps connections per IP and
account, budgets file descriptors, and evicts slow clients. An unpublish closes
the public stream and makes the following refetch return `404`.

SSE does not make edge cache invalidation synchronous. An open JavaScript client
converges through its uncached refetch; a crawler or no-JavaScript visitor may
observe the bounded public cache lifetime.
