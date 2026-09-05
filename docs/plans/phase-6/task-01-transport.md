# Task 6.1: SSE transport

Implement the [phase contract](README.md) and AC-RT-001 transport behavior. The
Go hub/listener worker owns `apps/server/internal/realtime/`. The integration
owner owns `internal/realtimeapi/`, SQL, migrations, generated store and API
files, public routing, public-state representation, and server composition.

Write failing tests before implementation. Cover account and resume isolation,
all connection limits, key reclamation under churn, full-queue eviction,
malformed notifications, listener loss/reconnect, transaction commit versus
rollback, and process shutdown. Route tests cover missing/revoked/expired
sessions, cross-account isolation, hostile paths and methods, ignored public
cookies, missing/private/deleted/renamed slugs, immediate flushes, heartbeat,
bounded slow writers, and stream closure before a revoking mutation completes.
No stream retains a resume document or one database connection of its own.

Narrow checks: `go test -race -count=1 -p=1 ./internal/realtime/...` and
`go test -race -count=1 -p=1 ./internal/realtimeapi/...` under `apps/server`,
with the shared test database for integration cases. The owner additionally runs
`make sqlc-check api-check` and the affected public-state, public-route, and
composition tests. Report exact commands, results, remaining gaps, and changed
paths. Workers never run Git, migrations, full CI, containers, browsers, or
cloud operations.
