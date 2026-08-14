# Phase 5A file structure and ownership

Each implementation path has one author. New testdata directories are owned as a
whole only where stated. The integration owner serializes shared/generated
paths.

## T00 — Topology, public roots, and edge parity

- Registry/digests: `packages/publicroots/public-roots.v4.json`,
  `packages/publicroots/app-build-sources.v1.json`,
  `packages/publicroots/renderer-build-sources.v1.json`, and
  `packages/publicroots/public-roots.test.mjs`; static invalid registries under
  `packages/publicroots/testdata/`.
- Generator/output: `scripts/generate-public-roots.mjs`,
  `apps/server/internal/publicroots/generated.go`,
  `apps/server/internal/publicroots/generated_test.go`,
  `apps/web/app/public-roots.generated.ts`,
  `apps/web/test/public-roots.generated.test.ts`,
  `deploy/caddy/public-roots.generated.caddy`, and
  `deploy/caddy/testdata/public-roots.generated.json`.
- Runtime/topology: `deploy/caddy/Caddyfile`, `deploy/compose.yml`,
  `deploy/web.Dockerfile`,
  `apps/server/internal/routetable/route_table_test.go`,
  `scripts/dev-native.sh`, `scripts/dev-https.sh`, `scripts/dev-https-test.sh`,
  and `scripts/test/render-topology-test.sh`.
- Static safety: `deploy/dev-https-browser/static-test.sh`,
  `deploy/dev-https-browser/network-policy.ts`,
  `scripts/test/ci-scan-adversarial-test.sh`,
  `scripts/test/toolchain-contract-test.sh`,
  `scripts/test/makefile-safety-test.sh`, `.env.example`, and root `Makefile`.

## T01 — Public-state storage

- `apps/server/migrations/00007_add_public_state.sql` and
  `apps/server/migrations/public_state_test.go`.
- `apps/server/sql/queries.sql`.
- Generated `apps/server/internal/store/db.go`, `models.go`, `querier.go`, and
  `queries.sql.go` only in this serialized window.
- `apps/server/internal/store/public_contract.go` and
  `apps/server/internal/store/public_state_test.go`.

## T02 — Generation fences

- New `apps/server/internal/publicstate/coordinator.go`, `fence.go`, `lease.go`,
  `recovery.go`, and `metrics.go`.
- New `apps/server/internal/publicstate/coordinator_test.go`, `fence_test.go`,
  `lease_test.go`, `recovery_test.go`, and `metrics_test.go`.

## T03 — Idempotency re-probe

- `apps/server/internal/resume/idempotency.go`, `idempotency_test.go`, and
  `export_test.go`.
- New `apps/server/internal/resume/idempotency_db_test.go`.

## T04 — OpenAPI public contract

- `docs/api/openapi.yaml`.
- Generated `apps/web/app/api/generated/openapi.ts`.
- `docs/api/test/openapi.test.ts`, `resumes.contract.test.ts`, and
  `generate-wiring.test.ts`.

## T05 — Projection, cache, JSON, and photo

- New `apps/server/internal/publicresume/origin.go`, `origin_test.go`, `dto.go`,
  `dto_test.go`, `projection.go`, `projection_test.go`, `reader.go`, and
  `reader_test.go`. T05 owns `apps/server/internal/publicresume/testdata/`.
- New `apps/server/internal/publiccache/cache.go` and `cache_test.go`.
- New `apps/server/internal/publicapi/json.go`, `json_test.go`, `photo.go`,
  `photo_test.go`, `response.go`, `response_test.go`, `conditional.go`,
  `conditional_test.go`, and `test_support_test.go`.

## T06 — Publish policy

- New `apps/server/internal/resumeapi/publish.go`, `publish_test.go`,
  `completeness.go`, `completeness_test.go`, `slug_limiter.go`, and
  `slug_limiter_test.go`.

## T07 — Mutation transitions and recovery

- Existing `apps/server/internal/resumeapi/routes.go`, `routes_test.go`,
  `chain.go`, `chain_test.go`, `writesafety.go`, `writesafety_test.go`,
  `persist.go`, `persist_test.go`, `resumes.go`, `resumes_test.go`, `photo.go`,
  `photo_test.go`, `entries.go`, `entries_test.go`, `sections.go`,
  `sections_test.go`, `structure.go`, `structure_test.go`,
  `personal_details.go`, `personal_details_test.go`, `customization.go`, and
  `customization_test.go`.
- New `apps/server/internal/resumeapi/transition.go`, `transition_test.go`,
  `recovery.go`, and `recovery_test.go`.

## T08 — Public formats

- New `apps/server/internal/publicformat/markdown.go`, `markdown_test.go`,
  `discovery.go`, `discovery_test.go`, `jsonld.go`, and `jsonld_test.go`.
- T08 owns exact-byte fixtures under
  `apps/server/internal/publicformat/testdata/public-format/`.
- New `apps/server/internal/publicapi/markdown.go`, `markdown_test.go`,
  `discovery.go`, and `discovery_test.go`.

## T09 — Nuxt worker and hydration

- New `apps/web/server/routes/internal-render/public.post.ts`.
- New `apps/web/server/workers/public-render/worker-entry.ts` and `render.ts`.
- New `apps/web/server/utils/public-render/envelope.ts`, `runner.ts`, and
  `worker-build.ts`.
- New `apps/web/server/plugins/public-render-worker.ts` and
  `apps/web/types/public-render-worker.d.ts`.
- New `apps/web/app/components/public/PublicResumeApp.vue` and
  `apps/web/app/public/public-resume.client.ts`; modify
  `apps/web/nuxt.config.ts`.
- New `apps/web/test/public-render/handler.test.ts`, `build.test.ts`,
  `worker-lifecycle.test.ts`, `hydration.test.ts`, and `spin-worker.mjs`.
- Integration-owner W4b alone edits `apps/web/package.json` and
  `apps/web/package-lock.json`.

## T10 — Direct render and HTML

- New `apps/server/internal/directrender/origin.go`, `origin_test.go`,
  `client.go`, `client_test.go`, `types.go`, and `types_test.go`.
- New `apps/server/internal/publicapi/html.go` and `html_test.go`.

## T11 — Public router and readiness

- New `apps/server/internal/publicapi/service.go`, `service_test.go`,
  `routes.go`, and `routes_test.go`.
- New `apps/server/internal/publicstate/readiness.go` and `readiness_test.go`.
- `apps/server/internal/api/router.go` and `router_test.go`.
- `apps/server/internal/config/config.go` and `config_test.go`.
- `apps/server/cmd/server/main.go` and `main_test.go`.

## T12 — Isolated native capture

- New `apps/server/cmd/p5a-native-fixture/main.go`, `fixture.go`,
  `fixture_test.go`, `testdata/resume-v2.json`, `testdata/photo.png`, and
  `testdata/hashes.json`.
- New `scripts/p5a-native-http-capture.sh` and
  `scripts/test/p5a-native-http-capture-test.sh`.
- Root `Makefile` capture target, serialized after T00's root edit.

## Ownership notes

Task 05 owns shared `publicapi/test_support_test.go`; T08/T10/T11 consume it
without editing it. T06 owns only its six new policy files; T07 owns the listed
existing mutation surfaces and its four new transition files. Root `Makefile` is
repeated only between the same integration owner in T00 and T12.

The root integration owner releases cross-phase shared paths in this total
order: Phase 4 T00; Phase 5A T00; Phase 5A T01; Phase 5A T04; dependent Phase 4
transport/public work; Phase 5A T09/W4b before final browser windows; Phase 4
T15; then Phase 5A T12. A task starts from the integrated prior window.

## Compile dependency direction

```text
publicroots -> resumeapi, publicapi routing, Caddy, Nuxt parity
store -> publicstate composition, resumeapi, publicresume
publicstate -> resumeapi transitions, publicresume admission
publicresume -> publicapi, publicformat, directrender
publicformat -> publicapi HTML/text
directrender -> publicapi HTML
publicapi -> api router composition only through PublicRoutes
```

No lower package imports `resumeapi`, `api`, `cmd/server`, Caddy fixtures, or
Nuxt runtime code. `publicstate` receives database and renderer probes as
functions and never imports `publicapi` or `directrender`.

## Root-owned records outside task paths

After T12 and before fresh review, the integration owner edits
`docs/plans/implementation-plan.md`, `docs/plans/traceability/README.md`,
`docs/architecture.md`, the public-serving/native runbook under
`docs/runbooks/`, and phase state/evidence in the three P5A traceability files.
The owner commits those records locally and includes the commit in the review
candidate. Approved Phase 5A inputs and completed phase records stay unchanged.
