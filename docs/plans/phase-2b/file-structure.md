# File structure produced by this phase

One row per owned path, with the single task that owns it. Two tasks never own
the same path.

| File                                                                                                                                                    | Responsibility (task)                                                                                                              |
| ------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| `docs/api/openapi.yaml` (modify)                                                                                                                        | The entire P2B path/schema/parameter surface + examples, contract-first (T1)                                                       |
| `docs/api/test/resumes.contract.test.ts` (create)                                                                                                       | Every write declares idempotency/security; existing-resume mutations declare `If-Match`; create rejects it; examples validate (T1) |
| `apps/web/app/api/generated/openapi.ts` (regenerate)                                                                                                    | Regenerated once by the integration owner after T1; drift gate green                                                               |
| `apps/server/internal/resume/service.go` + `service_test.go` (create), `store.go` (modify)                                                              | Explicit tx operations, commit outcome, usage admission, and bounded cleanup (T2, integration owner)                               |
| `apps/server/migrations/00006_bound_idempotency_retention.sql` + `apps/server/cmd/migrate/idempotency_retention_test.go`                                | Composite cleanup index, response headers, exact retained-usage counters, and prior-head migration proof (T2, integration owner)   |
| `apps/server/sql/queries.sql`, `apps/server/internal/store/**` (regenerate)                                                                             | Aggregate metadata CAS, revision-CAS delete, bounded cleanup, usage, normalized response insert (T2, integration owner)            |
| `apps/server/internal/media/{media.go,fs.go,s3.go,conformance_test.go,fs_test.go,s3_test.go}`                                                           | Backend interface, filesystem + S3-compatible backends, one shared contract (T3)                                                   |
| `apps/server/internal/media/{admission.go,admission_test.go,normalize.go,normalize_test.go,photo_key.go,photo_key_test.go}`                             | Task-wide permit, bounded decode, normalization, and exact resume-photo key constructor/parser (T11)                               |
| `apps/server/internal/config/config.go` + `config_test.go` (modify)                                                                                     | `MEDIA_*` configuration and its validation (T3)                                                                                    |
| `deploy/compose.yml`, `deploy/README.md`, `.env.example` (modify)                                                                                       | Pinned MinIO service + one-shot bucket creation; documented env (T3)                                                               |
| `apps/server/internal/resumeapi/{routes.go,chain.go,writesafety.go,wireversion.go,errors.go,persist.go}`                                                | Route table with construction-only 501 stubs, write-safety kernel, production error vocabulary, persist helper (T4)                |
| `apps/server/internal/resumeapi/{routes_test.go,chain_test.go,writesafety_test.go,wireversion_test.go,errors_test.go,persist_test.go,testutil_test.go}` | Kernel author tests and shared live-DB/media harness (T4)                                                                          |
| `apps/server/internal/api/router.go` + `router_test.go` (modify)                                                                                        | Exact photo-POST dispatch that bypasses buffering `BodyLimit`; all near matches retain it (T4, D14)                                |
| `apps/server/internal/auth/csrf.go` + `csrf_test.go` (modify)                                                                                           | Explicit multipart media-type option for the photo chain; JSON remains the default (T4)                                            |
| `apps/server/cmd/server/main.go` (modify)                                                                                                               | Wire `resumeapi.Service.RegisterRoutes` into `api.New` (T4)                                                                        |
| `apps/server/internal/resumeapi/{sanitize_doc.go,sanitize_doc_test.go}`                                                                                 | Schema-driven rich-text walk calling `sanitize.RichText` (T5)                                                                      |
| `apps/server/internal/resumeapi/{resumes.go,resumes_test.go,resumes_contract_test.go}`                                                                  | Collection + item CRUD handlers (T6)                                                                                               |
| `apps/server/internal/resumeapi/{entries.go,sections.go,entries_test.go,sections_test.go,entries_contract_test.go}`                                     | Entry upsert/delete, section metadata and entry order (T7)                                                                         |
| `apps/server/internal/resumeapi/{structure.go,structure_test.go,structure_contract_test.go}`                                                            | The one transactional structural endpoint (T8)                                                                                     |
| `apps/server/internal/resumeapi/{personal_details.go,personal_details_test.go,personal_details_contract_test.go,wireversion_e2e_test.go}`               | Personal details + the AC-SAVE-004 end-to-end proof (T9)                                                                           |
| `apps/server/internal/resumeapi/{customization.go,customization_allowlist.go,customization_test.go,customization_contract_test.go}`                     | Delta application + fixed path allowlist + schema-walk parity test (T10)                                                           |
| `apps/server/internal/resumeapi/{photo.go,photo_test.go,photo_contract_test.go}`                                                                        | Upload, read, crop, replace, delete, compensation, and key derivation (T11)                                                        |
| `apps/server/internal/resumeapi/authz_adversarial_test.go`                                                                                              | Blind suite D (T12)                                                                                                                |
| `apps/server/internal/resumeapi/writesafety_adversarial_test.go`                                                                                        | Blind suite E (T13)                                                                                                                |
| `apps/server/internal/resumeapi/bounds_adversarial_test.go`                                                                                             | Blind suite F (T14)                                                                                                                |
| `docs/plans/traceability/{ac-media,ac-save,ac-sec,ac-doc}.md`, `docs/architecture.md`, `README.md`, `exit-criteria.md`, `integration-handoffs.md`       | Exact T15 documentation ownership                                                                                                  |
| `apps/server/go.mod` (T3 source), `apps/server/go.sum` (integration owner)                                                                              | AWS SDK for Go v2, `golang.org/x/image`, and direct `golang.org/x/text` pins in one serialized window                              |

**Not touched by this phase:** released migrations 00001–00005,
`packages/schema/**`, `deploy/caddy/Caddyfile` (`/api/v1/*` already covers every
new route — no route-table change, so `make route-table-test` needs no update),
and `apps/web/**` beyond the regenerated client artifact.

`apps/server/internal/api/middleware.go` is unchanged. Task 11 applies
`http.MaxBytesReader` after route admission rather than weakening the ordinary
body-limit middleware.

**Owner-applied, not worker-applied:** the root `Makefile`,
`.github/workflows/ci.yml`, and shared scripts — see
[integration-handoffs.md](integration-handoffs.md).
