# Phase 4 integration handoffs

Reserved files and cross-task evidence belong to the integration owner. A task
author reports a requested shared edit and stops.

## Serialized owner windows

| Window                     | Exact change                                                                                                                                                           | Release gate                                                                            |
| -------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| W0 planned records         | Add Phase 4 as planned to `docs/plans/implementation-plan.md`; add AC-EDITOR link/count as planned to `docs/plans/traceability/README.md`                              | Focused Prettier and markdownlint                                                       |
| W0 authenticated transport | Set exact cache policy; update OpenAPI/generated client; add isolated transport browser mode/target; conditionally correct Caddy                                       | Server/API gate, harness static/Make safety, auth and transport live checks             |
| W0 web dependencies        | Export schema/validation; install exact Pinia, Ajv, and ProseMirror packages; configure Pinia and client-only route                                                    | `make schema-check web-lint web-typecheck web-test web-build`                           |
| W9 editor harness          | Add pinned axe, editor spec/fixtures/mode/verdict/source hash/root target                                                                                              | Static/Make safety, then all three live browser targets                                 |
| W10 candidate records      | Update architecture/runbook, exact AC evidence, exit record, and phase status after preliminary gates; create one record commit containing every candidate record edit | Preliminary `make ci` and connected `make scan`, then focused docs checks               |
| W11–W12 review/exit        | Sole fresh review reads the record commit; integrate fixes; same reviewer confirms; rerun definitive full gates at final record commit                                 | Unchanged-candidate `make ci` and connected `make scan`, then push without another edit |

`deploy/caddy/Caddyfile` changes only if Task 00 transport RED proves Caddy
2.11.4 changes `no-transform`, strong parent ETag, or request precondition. The
owner changes `scripts/dev-https-test.sh` and
`apps/server/internal/routetable/route_table_test.go` in the same window. No
client strips/rebuilds a tag. Both native specs record IDs only from validated
`201` responses and delete only those IDs. A fixed-account cap error fails the
run; it never authorizes deleting an existing resume to make room.

## Dependency commands

Task 00 owner runs this repository-root command:

```sh
(cd apps/web && npm install --save-exact \
  pinia@latest @pinia/nuxt@latest ajv@latest ajv-formats@latest \
  prosemirror-model@latest prosemirror-state@latest prosemirror-view@latest \
  prosemirror-schema-list@latest prosemirror-commands@latest \
  prosemirror-history@latest prosemirror-keymap@latest)
```

Task 15 owner runs this repository-root command:

```sh
(cd deploy/dev-https-browser && \
  npm install --save-dev --save-exact @axe-core/playwright@latest)
```

Owners report resolved versions and retain web TypeScript 6.0.3. No task
hand-edits a lockfile.

## Cross-task interfaces

| Producer | Consumers      | Single definitions                                                                                                                                                                                                                                    |
| -------- | -------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 01       | 02–05, 08–14   | `Revision`, `ParentETag`, `SaveState`, unconstrained `Presence`, explicit `ProjectionValue`/`ProjectionContextKey`, snapshots, `EditorRuntime`, command/delta unions, validation, reducers, projections, coalescing, and shared accepted test fixture |
| 02       | 04, 05, 12, 14 | Conflict/reconciliation unions and functions; template child/group/input/undo/state/recovery types and functions; `EditorQueueItem`                                                                                                                   |
| 03       | 04–06, 13      | Operation/payload/attempt/result unions, distinct `{path,code}` `ServerValidationIssue`, stale/list/read/photo results, `ObjectETag`, `ResumeApi`, freeze/request/object-tag functions, and `readOwnerPhoto`                                          |
| 04       | 05, 06, 10–14  | Generation-bound `PhotoReadState`, `OpaquePhotoOutcome`, `CompletionAdoption`, attempt/record/store types, `useResumeStore`, and store actions                                                                                                        |
| 05       | 06, 10–14      | `AuthState`, opaque create/decision/result types, `ResumeMutationCoordinator`, exact `ResumeEditorActions`, `createResumeEditorActions`, `useResumeEditor`, and resolved `useAuth`                                                                    |
| 07       | 09, 14         | `richTextSchema`, parser/serializer, `RichTextEditor`                                                                                                                                                                                                 |
| 08       | 09             | `FieldIntent`, shared date/optional controls                                                                                                                                                                                                          |
| 10–13    | 14             | Structure, customization, template, and photo panels                                                                                                                                                                                                  |
| 14       | 15             | Stable roles, labels, exact `Estimated pages`, settled visible contiguous `data-page-index` observer, status/conflict controls, and route behavior                                                                                                    |

A consumer never redeclares or renames a producer type. `readOwnerPhoto` is the
only private-photo transport method name in every task.

## Task report format

```text
Phase/task: exact P4 task number and title
Owned paths: complete path list
Acceptance: owned AC-EDITOR IDs
RED: each exact command, failing assertion, and expected reason
GREEN: each exact rerun command and result
Adversarial cases: named cases proved
Shared edits requested: exact reserved path and edit, or none
Unrun checks: exact command, reason, and uncertainty, or none
Risks/notes: remaining facts, or none
Suggested commit: Conventional Commit subject
```

The owner rereads exact task paths and reruns the key GREEN before staging.
Workers do not run `make ci`, `make scan`, or Git.

## Candidate record and rerun sequence

1. Before Task 00 dispatch, add the planned Phase 4 and AC-EDITOR index entries.
2. After Task 15, integrate all task reports. Update `docs/architecture.md` and
   `docs/runbooks/native-development.md` with actual command/store/queue/photo/
   preview/HTTPS boundaries. Add exact test references to AC rows.
3. Run preliminary `make ci` alone and connected `make scan` alone. A failure
   returns to its author and repeats this step.
4. After preliminary gates pass, mark AC rows `PROVEN`, check exit evidence, and
   mark Phase 4 complete in `docs/plans/implementation-plan.md`. Create the
   record commit. This is the complete candidate presented to the sole fresh
   reviewer; no record edit is deferred until after review.
5. The fresh non-author reviews the record commit. Findings return to an author;
   the same reviewer confirms each fix. Any fix creates a new candidate commit.
6. At the final record commit, rerun focused docs checks, `make ci` alone, and
   connected `make scan` alone. If any fails, correct code and completion
   records, obtain same-reviewer confirmation, and repeat this step.
7. Push the exact unchanged commit from step 6. Do not add a completion or index
   commit after the definitive gates.

Full P9 U1–U5, port 443, production topology, and cloud work remain pending in
the implementation plan and runbook.
