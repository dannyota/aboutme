# Realtime operation and verification

The Go server serves account events at `/api/v1/events` and anonymous live
resume events at `/api/v1/live/{slug}`. Events invalidate a document; HTTP
remains the read and mutation authority. Public events contain only wire version
and decimal-string revision. They never contain private IDs or content.

## Bounds and recovery

One LISTEN connection uses the existing PostgreSQL pool, whose ceiling is 20.
The hub admits at most 2,000 streams, 100 per canonical client IP, and 20 per
account. Each connection queues eight events. Overflow disconnects the client;
reconnection triggers an unconditional read. Admission also requires at least
25% file-descriptor headroom. Unused admission keys are reclaimed.

The listener closes streams immediately on connection loss or malformed
notification. It retries with bounded backoff and reopens admission only after
LISTEN succeeds. Shutdown cancels the listener and joins it before pool close.
Writes have a two-second deadline, cleared after each flush; heartbeats arrive
immediately on admission and every 25 seconds. Public revocation waits for the
writer to finish and release its lease before the mutation returns.

Clients refetch on connection, reconnection, and newer revision. Three transport
errors or 60 seconds without a frame enable 30-second conditional polling.
Failed reads retry independently of heartbeats. Session loss stops owner updates
while retaining in-memory edits. Public 404 reloads the authoritative page; an
unknown document or wire version reloads the client.

## Local connection measurement

On 2026-09-05 the integration owner ran `make server-test-realtime-stress` on
Linux AMD64 with Go 1.27.1, `GOMAXPROCS=2`, and `GOGC=50`. The test opens 2,000
real HTTP/1.1 TCP streams, checks two rounds of at least five seconds with five
samples per round, and replaces 200 connections per round while dispatching
metadata. It also checks admission failure, synthetic hub availability recovery,
and cleanup. Listener recovery is covered separately by `internal/realtime`
tests. Document size does not affect these metadata-only transport frames.

The owner run passed. Peak Go heap growth was 104,369,848 bytes, including both
test clients and server in one process. Cleanup returned to eight file
descriptors, zero sockets, and two goroutines. Raw evidence remains local at
`.dev/phase-6/connection-measurement-owner.log`. The process used Go's inherited
effective descriptor limit; the test did not change host limits.

This is a local transport baseline. It does not prove the whole application fits
an AWS task's 512 MiB budget or establish latency through AWS and Cloudflare.
Phase 10 repeats resource and edge measurements on the selected runtime. The
stress test does not emit a machine hostname or resume content.

## Checks

Run heavy commands serially at the repository root. `make server-test-db` proves
committed notification delivery, rollback silence, and real-session routes
against the one shared database. Injected listener lifecycle tests prove
recovery, single-connection ownership, and joined shutdown. This distinction
corrects Phase 6's exit criterion, which had grouped listener recovery under
PostgreSQL integration. `make server-test-realtime-stress` is opt-in and opens
thousands of local sockets; do not run it beside another build, browser, or
scan.

`make dev-https-publish-check` uses the trusted local HTTPS harness to check
cross-tab owner refresh, in-place public refresh, scroll preservation, and
automatic 404 navigation after unpublish. See [local checks](local-uat.md) for
setup and bounded evidence handling. The traceability rows record the latest
accepted proof state.
