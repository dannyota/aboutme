# 8. Realtime

Realtime has separate write and read paths. Autosave is ordinary HTTP;
Server-Sent Events (SSE) carry invalidation only
([ADR 0003](../adr/0003-sse-over-websocket.md)).

## Writes

The editor updates its local document and preview at once. After about one
second of inactivity it sends the pending change as one granular HTTP mutation,
which keeps preconditions, idempotency, status codes, request logs, and
route-specific rate limits. There is no WebSocket write path.

## Refresh ladder

| Rung | Mechanism                                     | Trigger                                   |
| ---- | --------------------------------------------- | ----------------------------------------- |
| 1    | SSE notification followed by document refetch | Default                                   |
| 2    | Conditional ETag polling every 30 seconds     | Repeated SSE failure or buffering proxy   |
| 3    | Full page reload                              | Client cannot render the document version |

Three consecutive transport errors, or 60 seconds without a delivered frame,
switch the client to polling; a delivered heartbeat restores streaming. The
client refetches the public or preview document and renders in place without
resetting scroll or editor state. Refetches coalesce and keep one follow-up when
an event arrives during a read. Receiving an event does not prove its revision
was adopted, and a failed read stays retryable.

## Delivery semantics

A transaction trigger publishes insert, update, and delete metadata, and
PostgreSQL `NOTIFY` fans it out to the local hub over one `LISTEN` connection
from the existing pool. Delivery is not durable: `Last-Event-ID` serves
deduplication and observability, not replay. Every initial connection and
reconnect refetches unconditionally, which repairs anything missed. Listener
loss closes subscriptions, and admission resumes after `LISTEN` succeeds.

SSE is not the revocation authority. Every shared-cache reuse revalidates
through the live-state gate, and state mutations wait on the revocation fence
([ADR 0022](../adr/0022-public-artifact-revocation.md)). An unpublish closes the
public stream, and the next refetch returns `404`.

## Stream contract

The [OpenAPI contract](../api/openapi.yaml) defines `GET /events` for the
signed-in account and anonymous `GET /live/{slug}`. Revision frames carry
version 1 and a decimal-string revision; owner frames add the resume ID and a
deletion flag. Public frames carry no account ID or private resume ID. Heartbeat
frames carry only version 1, flush on admission, and repeat every 25 seconds.

Each stream queues a bounded number of events, then disconnects rather than
silently dropping one. Connections are capped per task, per canonical client IP,
and per account, and an admission key disappears with its last connection;
[Numeric budgets](budgets.md) holds the numbers. Owner streams recheck the
session before every revision and heartbeat. Public streams hold a metadata-only
lease for their lifetime. A write deadline, cleared after each flush, keeps idle
streams open while a slow writer cannot outlast the public revocation drain.
