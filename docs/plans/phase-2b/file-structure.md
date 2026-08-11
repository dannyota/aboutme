# File structure produced by this phase

One row per owned path, with the single task that owns it. Two tasks never own
the same path.

| File                                                                                                             | Responsibility (task)                                                                                               |
| ---------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------- |
| `docs/api/openapi.yaml` (modify)                                                                                 | The entire P2B path/schema/parameter surface + examples, contract-first (T1)                                        |
| `docs/api/test/resumes.contract.test.ts` (create)                                                                | Document-level checks: every P2B write declares `If-Match`/`Idempotency-Key`/security, every example validates (T1) |
| `apps/web/app/api/generated/openapi.ts` (regenerate)                                                             | Regenerated once by T1; drift gate green                                                                            |
| `apps/server/internal/resume/service.go` + `service_test.go` (create), `store.go` (modify)                       | Exported transaction seam: tx-scoped Create/Get/SaveDocument/SaveMetadata/Delete (T2)                               |
| `apps/server/sql/queries.sql` (append), `apps/server/internal/store/**` (regenerate)                             | The one new statement, `UpdateResumeMetadataCAS` — Task 2's exclusive window (T2)                                   |
| `apps/server/internal/media/{media.go,fs.go,s3.go,conformance_test.go,fs_test.go,s3_test.go}`                    | Backend interface, filesystem + S3-compatible backends, one shared contract (T3)                                    |
| `apps/server/internal/config/config.go` + `config_test.go` (modify)                                              | `MEDIA_*` configuration and its validation (T3)                                                                     |
| `deploy/compose.yml`, `deploy/README.md`, `.env.example` (modify)                                                | Pinned MinIO service + one-shot bucket creation; documented env (T3)                                                |
| `apps/server/internal/resumeapi/{routes.go,chain.go,writesafety.go,wireversion.go,errors.go,persist.go}` + tests | Route table with 501 stubs, write-safety kernel, error vocabulary, persist helper (T4)                              |
| `apps/server/internal/api/router.go` + `router_test.go` (modify)                                                 | Per-path body-limit override for the photo upload path (T4, D14)                                                    |
| `apps/server/cmd/server/main.go` (modify)                                                                        | Wire `resumeapi.Service.RegisterRoutes` into `api.New` (T4)                                                         |
| `apps/server/internal/resumeapi/{sanitize_doc.go,sanitize_doc_test.go}`                                          | Schema-driven rich-text walk calling `sanitize.RichText` (T5)                                                       |
| `apps/server/internal/resumeapi/{resumes.go,resumes_test.go,resumes_contract_test.go}`                           | Collection + item CRUD handlers (T6)                                                                                |
| `apps/server/internal/resumeapi/{entries.go,sections.go,entries_test.go,sections_test.go}`                       | Entry upsert/delete, section metadata and entry order (T7)                                                          |
| `apps/server/internal/resumeapi/{structure.go,structure_test.go}`                                                | The one transactional structural endpoint (T8)                                                                      |
| `apps/server/internal/resumeapi/{personal_details.go,personal_details_test.go,wireversion_e2e_test.go}`          | Personal details + the AC-SAVE-004 end-to-end proof (T9)                                                            |
| `apps/server/internal/resumeapi/{customization.go,customization_allowlist.go,customization_test.go}`             | Delta application + fixed path allowlist + schema-walk parity test (T10)                                            |
| `apps/server/internal/resumeapi/{photo.go,photo_test.go}`                                                        | Upload, read, replace, delete; key derivation; orphan sweep (T11)                                                   |
| `apps/server/internal/resumeapi/authz_adversarial_test.go`                                                       | Blind suite D (T12)                                                                                                 |
| `apps/server/internal/resumeapi/writesafety_adversarial_test.go`                                                 | Blind suite E (T13)                                                                                                 |
| `apps/server/internal/resumeapi/bounds_adversarial_test.go`                                                      | Blind suite F (T14)                                                                                                 |
| `apps/server/internal/resumeapi/testutil_test.go`                                                                | Shared httptest harness (live DB + fs media backend), owned by T4                                                   |
| `docs/plans/traceability/ac-media.md` (create), `ac-save.md`/`ac-sec.md`/`README.md` (modify)                    | New rows, closed references, index counts (T15)                                                                     |
| `apps/server/go.mod` / `go.sum` (modify)                                                                         | AWS SDK for Go v2 `s3` pin (T3 only; serialized)                                                                    |

**Not touched by this phase:** `apps/server/migrations/**`,
`packages/schema/**`, `deploy/caddy/Caddyfile` (`/api/v1/*` already covers every
new route — no route-table change, so `make route-table-test` needs no update),
and `apps/web/**` beyond the regenerated client artifact.

**Owner-applied, not worker-applied:** the root `Makefile`,
`.github/workflows/ci.yml`, and `docs/plans/budgets.md` — see
[integration-handoffs.md](integration-handoffs.md).
