# AC-RT traceability rows

Two acceptance-criterion rows with the `AC-RT-` prefix. See
[README.md](./README.md) for states and ownership rules.

| ID        | Spec clause | Statement                                                                                                                  | Phase/task | Test / UAT reference                                                                                                                                                                                                                                                                                                                    |
| --------- | ----------- | -------------------------------------------------------------------------------------------------------------------------- | ---------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| AC-RT-001 | §8          | SSE connection and reconnect always refetch; polling repairs transport failure without losing editor work or viewer scroll | 6.1, 6.2   | PROVEN; `internal/realtime/listener_test.go`, `internal/realtimeapi/service_test.go`, `internal/store/realtime_test.go`; web `test/editor/realtime.test.ts`, `realtime-page.test.ts`, `coordinator.test.ts`, and `test/public-render/hydration.test.ts`; `make dev-https-publish-check`: cross-tab and public refresh, preserved scroll |
| AC-RT-002 | §8          | Unpublish, rename, and delete close public streams before success and deny subsequent live reads, including cached JSON    | 6.1, 6.2   | PROVEN; `TestRealtimePublicMutationClosesStreamAndRejectsCachedJSON` covers HTTP unpublish, rename, delete, and agent delete; `TestPublicStreamOmitsOwnerMetadataAndClosesBeforeRevocationCompletes`; `make dev-https-publish-check`: automatic public 404 after unpublish                                                              |

Paths under `internal/` are in `apps/server/`; web test paths are in
`apps/web/`. [The realtime runbook](../../runbooks/realtime.md) records commands
and the local connection measurement. Hosted resource and edge evidence belongs
to Phase 10.
