# Integration handoffs (owner-applied, not worker-applied)

| Shared file                          | Needed change                                                                                            | When          |
| ------------------------------------ | -------------------------------------------------------------------------------------------------------- | ------------- |
| `.github/workflows/ci.yml`           | Add exact released-schema/type append-only job beside migrations; add Semgrep policy regression          | Tasks 2b/6b   |
| root `Makefile`                      | Retain landed resume DB coverage; add `semgrep-policy-test`                                              | Task 6b/gate  |
| `docs/plans/implementation-plan.md`  | Drift-gate wording correction (script → `cmd/migrate/gen`); phase-status row when gates pass             | Task 1 / gate |
| `.semgrep.yml` + policy script       | Restrict direct use of named generated write methods to `internal/resume` and prove the negative control | Task 6b       |
| P2B plan/OpenAPI tests               | Full-document writes plus old-client accept/project/persist/emit behavior                                | P2B planning  |
| root `go.work.sum` (if materialized) | commit as lockfile                                                                                       | whenever      |
