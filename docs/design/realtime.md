# 8. Realtime

Realtime has separate write and read paths. Autosave is ordinary HTTP;
Server-Sent Events (SSE) carry invalidation only
([ADR 0002](../adr/0002-system-architecture.md)).

## Writes

The editor updates its local document and preview at once. After about one
second of inactivity it sends the pending change as one granular HTTP mutation,
which keeps preconditions, idempotency, status codes, request logs, and
route-specific rate limits. There is no WebSocket write path.

The editor sets a read barrier when it adopts a revision it has not read in
full: the winner of a `412`, the revision a bodyless `204` acknowledges, or a
project link or refresh the queue dropped. The mutation queue owns that barrier,
whatever set it. Once no write is in flight, the queue reads the complete resume
before it sends any queued edit; any complete read at that revision or newer
clears the barrier, including a realtime refetch or the read after signing in
again. A failed read keeps the barrier and is retried on the queue's own timer
with the delays in [Numeric budgets](budgets.md); no other edit, action, or
flush runs it sooner. A lost session waits for sign-in, and a missing resume
ends the retries. While the editor is open its queued edits are held, never
dropped, the save status shows unsaved, and leaving warns. Discarding after a
lost session drops them and lets the page go, though the barrier stays for the
queue. Leaving anyway also drops them and stops the queue's reads and writes;
the resume opens again from a fresh complete read.

A write whose outcome is unknown, because its response never arrived or the
server failed, is sent again unchanged: the same Idempotency-Key, If-Match and
body, so a write that already landed replays its stored answer instead of
running twice ([ADR 0006](../adr/0006-transactional-idempotency.md)). After one
quick replay it is retried with the delays in [Numeric budgets](budgets.md)
until its 23-hour retry window closes; then the editor reads the resume and
reconciles the write against it. Nothing queued behind it is sent meanwhile, and
a read that shows it landed releases the queue at once. A write that meets a
newer revision is checked against it: a change the newer version already holds
is dropped, a real conflict opens the conflict panel, and an unchanged field is
rebased and sent with a new key, up to the rebase limit in Numeric budgets. A
title or delete write whose stale read fails is held and retried the same way.
The editor shows a held write as not saved and retrying, and tells the person
when its outcome has stayed unknown for the alert time in Numeric budgets or it
has used every rebase.

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
was adopted, and a failed read stays retryable. A public page reloads on a `404`
at most once per slug per minute, tracked in session storage, and not at all
when session storage is unavailable.

## Delivery semantics

A transaction trigger publishes insert, update, and delete metadata, and
PostgreSQL `NOTIFY` fans it out to the local hub over one `LISTEN` connection
from the existing pool. Delivery is not durable: `Last-Event-ID` serves
deduplication and observability, not replay. Every initial connection and
reconnect refetches unconditionally, which repairs anything missed. Listener
loss closes subscriptions, and admission resumes after `LISTEN` succeeds.

SSE is not the revocation authority. Every shared-cache reuse revalidates
through the live-state gate, and state mutations wait on the revocation fence
([ADR 0010](../adr/0010-public-artifact-revocation.md)). An unpublish closes the
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
