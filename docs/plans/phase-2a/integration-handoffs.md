# Integration handoffs (owner-applied, not worker-applied)

| Shared file                          | Needed change                                                                                | When          |
| ------------------------------------ | -------------------------------------------------------------------------------------------- | ------------- |
| `.github/workflows/ci.yml`           | Add exact released-schema/type append-only job beside migrations                             | Task 2b       |
| root `Makefile`                      | Retain landed resume DB coverage                                                             | Gate          |
| `docs/plans/implementation-plan.md`  | Drift-gate wording correction (script → `cmd/migrate/gen`); phase-status row when gates pass | Task 1 / gate |
| P2B plan/OpenAPI tests               | Full-document writes plus old-client accept/project/persist/emit behavior                    | P2B planning  |
| root `go.work.sum` (if materialized) | commit as lockfile                                                                           | whenever      |
