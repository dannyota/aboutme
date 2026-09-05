# Task 6.2: realtime refresh and unpublish

Implement the [phase contract](README.md), AC-RT-001 reconnect repair, and
AC-RT-002 public unpublish convergence. The web worker owns browser realtime
code, its editor integration, public hydration, and their unit tests under
`apps/web/app/` and `apps/web/test/`. It does not own manifests, generated code,
build configuration, or shared scripts.

Write failing tests before implementation. Cover initial open, reconnect after
missed events, unrelated owner events, duplicate/older revisions, refetch burst
coalescing, errors during a refetch, unknown versions, three-error and silent
connection polling fallback, conditional 304 retention, recovery, and cleanup.
Prove that refresh preserves pending edits and active mutation reconciliation.
Public hydration must patch in place, preserve scroll, and reload on a live
refetch 404 rather than retaining a private or deleted resume onscreen.

Use the existing editor coordinator's unconditional read for reconnect repair.
If owner conditional polling needs an adapter extension, preserve that read's
default behavior and the complete accepted snapshot for a 304. Do not change the
renderer's SSR inputs or load EventSource in the SSR worker.

Narrow check under `apps/web`:
`npx vitest run test/editor/realtime.test.ts test/editor/coordinator.test.ts test/editor/resume-store.test.ts test/editor/resume-api.test.ts test/public-render/hydration.test.ts`.
The owner runs the web gate and native HTTPS proof after integration. Report
exact commands, results, remaining gaps, and changed paths. Workers never run
Git, full CI, containers, browsers, or cloud operations.
