# File structure produced by this phase

| File                                                                                                                  | Responsibility                                                                                                                |
| --------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| `apps/server/cmd/migrate/gen/main.go` (modify) + tests                                                                | D9 drift-gate extension (Task 1)                                                                                              |
| `packages/schema/validation/store.ts`, `gen/go/store_validate.go` (+tests) (modify)                                   | Entry-id uniqueness validator, both halves (Task 2)                                                                           |
| `packages/schema/scripts/generate.mjs` (modify), `gen/go/rawschema.go` (generated)                                    | D2 raw-schema Go source constant (Task 2); byte-compare coverage via existing `packages/schema/test/gen.test.ts`              |
| `packages/schema/resume.v1.schema.json`, `gen/{go,ts}/v1/**`, registries/tests                                        | Immutable v1 schema and retained generated types (Task 2b)                                                                    |
| `packages/schema/fixtures/bounds/` (generated corpus + `manifest.json`), `packages/schema/test/bounds-parity.test.ts` | D1(e) cross-language verdict-parity corpus: ajv and jsonschema/v6 must agree on every fixture + bounds document (Tasks 5, 11) |
| `apps/server/sql/schema.sql` (append)                                                                                 | `resumes`, `slug_tombstones`, `idempotency_records`, cap function + trigger (Task 3)                                          |
| `apps/server/migrations/00004_add_resume_tables.sql` (generated)                                                      | Tables/constraints/indexes (Task 3)                                                                                           |
| `apps/server/migrations/00005_add_resume_cap_trigger.sql` (hand-written) + `atlas.sum`                                | Function + trigger DDL Atlas cannot diff (Task 3)                                                                             |
| `apps/server/migrations/resume_schema_test.go`                                                                        | Migrated-DB constraint/trigger existence + behavior tests (Task 3)                                                            |
| `apps/server/sql/queries.sql` (append), `apps/server/internal/store/*.go` (regenerated)                               | sqlc queries + generated types (Task 4)                                                                                       |
| `apps/server/internal/resume/{resume.go,codec.go,validate.go,store.go}` + tests                                       | Domain type, codec (D4), validation pipeline (D16), store API (Tasks 5–6)                                                     |
| cleared-contact fixture + Go/TS/store tests                                                                           | Close AC-DOC-009 at shared validation and live-write boundaries (Task 6a)                                                     |
| `apps/server/internal/resume/bounds_test.go`                                                                          | The schema-driven limit+1 harness (Task 5)                                                                                    |
| `apps/server/internal/resume/idempotency.go` + tests                                                                  | D11 idempotency primitive (Task 7)                                                                                            |
| `apps/server/internal/resume/docmigrate/{docmigrate.go,backfill.go}` + tests                                          | D12/D13/D18 projection + CAS backfill + wire-version declarations (Task 8)                                                    |
| `apps/server/internal/resume/{writesafety,docmigrate,bounds}_adversarial_test.go`                                     | Blind suites A, B, and C (Tasks 9–11; three separate fresh authors)                                                           |
| `apps/server/go.mod`/`go.sum` (modify)                                                                                | `santhosh-tekuri/jsonschema/v6` pin (Task 5; serialized per B10)                                                              |
| `docs/plans/traceability/` (modify)                                                                                   | Row closure against owner-minted IDs (Task 12)                                                                                |

Not touched by this phase: `docs/api/openapi.yaml`, `apps/web/**`, and
`deploy/**`. The integration owner already landed the root `Makefile` change
that adds the resume package to `server-test-db`. Task 2b copies the
already-frozen current schema into the first immutable released-version file; it
does not change the schema contract.
