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

SSE is not the public revocation authority. Every shared-cache reuse revalidates
through the live-state gate, and the state mutation waits on the revocation
fence before returning success. An open JavaScript client still converges
through its uncached refetch; a crawler or no-JavaScript visitor gets the same
immediate live-state decision without SSE.
[ADR 0022](../adr/0022-public-artifact-revocation.md) owns that boundary.

## Stream contract

The [OpenAPI contract](../api/openapi.yaml) defines `GET /events` for the
signed-in account and anonymous `GET /live/{slug}`. Revision frames carry
version 1 and a decimal-string revision. Owner frames also carry the resume ID
and deletion flag. Public frames contain neither account IDs nor private resume
IDs. Heartbeat frames carry only version 1 and flush immediately on admission.

Three consecutive transport errors or 60 seconds without a delivered frame
enable conditional polling every 30 seconds. A delivered heartbeat restores the
streaming path. Refetches coalesce and preserve one follow-up when an event
arrives during an active read. Failed reads remain retryable; receiving an event
does not prove its revision was adopted.

A transaction trigger publishes insert, update, and delete metadata. One LISTEN
connection uses the existing pool budget. Listener loss closes subscriptions;
admission resumes after LISTEN succeeds. Each stream queues at most eight
events, then disconnects instead of silently dropping invalidations. Caps are
2,000 per task, 100 per canonical client IP, and 20 per authenticated account.
Unused admission keys are removed when their last connection closes.

Owner streams recheck session validity before every revision and heartbeat.
Public streams hold a metadata-only lease for their lifetime. Writes have a
two-second deadline, cleared after each flush, so idle streams remain open and a
slow writer cannot exceed the five-second public revocation drain.
