# AC-SAVE traceability rows

4 acceptance-criterion rows with the `AC-SAVE-` prefix, in their original matrix
order. See [README.md](./README.md) for the matrix purpose, maintenance rules,
and the full prefix index.

| ID          | Spec clause | Statement                                                                                                                                                                                                                                                                           | Phase/task | Test / UAT reference |
| ----------- | ----------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------- | -------------------- |
| AC-SAVE-001 | §4          | Stale If-Match returns 412 with current revision                                                                                                                                                                                                                                    | P2B        | (pending)            |
| AC-SAVE-002 | §4          | Idempotent replay returns stored response; different body rejected                                                                                                                                                                                                                  | P2B        | (pending)            |
| AC-SAVE-003 | §4          | The idempotency **store primitive** replays a stored response for a repeated key, rejects a different body under the same key, and rolls back cleanly on a mutation failure — the data-layer half of AC-SAVE-002's HTTP contract                                                    | P2A        | (pending)            |
| AC-SAVE-004 | §3          | A declared old-version client document written to a newer server is accepted, projected, target-validated, and transactionally persisted as the complete current-version document without data loss; a declared supported version can be emitted through the real HTTP/OpenAPI path | P2B        | (pending)            |
