# Phase 4 file structure and ownership

Each implementation path has one task author. Only the integration owner may
reuse reserved paths in later serialized windows.

## Reserved integration-owner windows

| Window            | Paths                                                                                                                                                                                                                  | Responsibility                                                        |
| ----------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------- |
| Pre-dispatch      | `docs/plans/implementation-plan.md`, `docs/plans/traceability/README.md`                                                                                                                                               | Add planned Phase 4 and AC-EDITOR index/count before dispatch         |
| 00                | `apps/server/internal/api/{cache_policy.go,cache_policy_test.go,router.go}`, `apps/server/internal/resumeapi/{photo.go,adversarial_remaining_exit_test.go,adversarial_security_exit_test.go}`                          | Exact authenticated no-store/no-transform policy                      |
| 00                | `docs/api/openapi.yaml`, `apps/web/app/api/generated/openapi.ts`                                                                                                                                                       | Header source and generated client                                    |
| 00                | `packages/schema/package.json`, `apps/web/{package.json,package-lock.json,nuxt.config.ts}`, `apps/web/test/nuxt/editor-config.test.ts`                                                                                 | Runtime schema exports, pinned dependencies, Pinia, client-only route |
| 00                | `deploy/dev-https-browser/transport.spec.ts`, `deploy/dev-https-browser/{Dockerfile,playwright.config.ts,run.sh,static-test.sh}`, root `Makefile`, `scripts/test/makefile-safety-test.sh`                              | Trusted transport mode and target                                     |
| 00 conditional    | `deploy/caddy/Caddyfile`, `scripts/dev-https-test.sh`, `apps/server/internal/routetable/route_table_test.go`                                                                                                           | Correction only when transport RED proves parity failure              |
| 15                | `deploy/dev-https-browser/{editor.spec.ts,editor-fixtures.ts,package.json,package-lock.json,Dockerfile,playwright.config.ts,run.sh,static-test.sh}`, root `Makefile`, `scripts/test/makefile-safety-test.sh`           | Editor image/mode/target/evidence                                     |
| 15 conditional    | `deploy/dev-https-browser/network-policy.ts`                                                                                                                                                                           | Exact local forced-failure console classification only                |
| Candidate records | `docs/architecture.md`, `docs/runbooks/native-development.md`, `docs/plans/implementation-plan.md`, `docs/plans/traceability/README.md`, `docs/plans/traceability/ac-editor.md`, `docs/plans/phase-4/exit-criteria.md` | Complete evidence/status record before fresh review                   |

Generated OpenAPI output is regenerated from source, never hand-edited. Task 00
and Task 15 share harness/root paths only because the same integration owner
runs them in W0 and W9.

## Domain, transport, and state

| Task | Create or modify                                                                                                                       | Responsibility                                                              |
| ---- | -------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------- |
| 01   | `apps/web/app/editor/{types,revision,documentValidation,commands,reducer,projections,coalesce}.ts`                                     | Domain/runtime types, validation, pure capture/replay/projection/coalescing |
| 01   | `apps/web/test/editor/{revision,document-validation,commands,reducer,projections,coalesce}.test.ts`, `apps/web/test/editor/fixture.ts` | Domain RED/GREEN matrices and the shared accepted-snapshot fixture          |
| 02   | `apps/web/app/editor/{reconcile,conflicts,templateDiff,templateGroup}.ts`                                                              | Conflict and template queue types/algorithms                                |
| 02   | `apps/web/test/editor/{reconcile,conflicts,template-diff,template-group}.test.ts`                                                      | Reconciliation/group matrices                                               |
| 03   | `apps/web/app/editor/{attempt,resumeApi}.ts`                                                                                           | Frozen attempt, exact result unions, owner-photo read                       |
| 03   | `apps/web/test/editor/{attempt,resume-api}.test.ts`                                                                                    | Attempt bytes and closed response cases                                     |
| 04   | `apps/web/app/stores/resumes.ts`                                                                                                       | Accepted/current/queue/adoption/photo state                                 |
| 04   | `apps/web/test/editor/resume-store.test.ts`                                                                                            | Store transition matrix                                                     |
| 05   | `apps/web/app/editor/coordinator.ts`, `apps/web/app/composables/useResumeEditor.ts`                                                    | Debounce, dispatch, retries, reconciliation, session resume                 |
| 05   | `apps/web/app/composables/useAuth.ts`, `apps/web/test/{useAuth,useAuth-csrf-rotation}.test.ts`                                         | Resolved auth/CSRF state                                                    |
| 05   | `apps/web/test/editor/coordinator.test.ts`                                                                                             | Timing, unknown, stale, template, session matrix                            |

Task 14 is the serialized visual-integration owner after Tasks 06–13 land. It
may restyle the already landed home, login, resume-list, and app-root paths
named in Task 14, but it does not change their transport, auth, or mutation
contracts. Its self-hosted `apps/web/public/theme-bootstrap.js` resolves the
first theme before paint and uses no browser storage.

## List and editor controls

| Task | Create                                                                                                                                                                                    | Responsibility                     |
| ---- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------- |
| 06   | `apps/web/app/pages/app/resumes/index.vue`, `apps/web/app/composables/useResumeList.ts`                                                                                                   | List route and state               |
| 06   | `apps/web/app/components/editor/list/{ResumeList,CreateResumeDialog,RenameResumeDialog,DeleteResumeDialog}.vue`, `apps/web/test/editor/resume-list.test.ts`                               | Lifecycle UI/test                  |
| 07   | `apps/web/app/components/editor/richtext/{schema.ts,serialize.ts,RichTextEditor.vue}`, `apps/web/test/editor/rich-text.test.ts`                                                           | Closed ProseMirror boundary        |
| 08   | `apps/web/app/components/editor/forms/{fieldIntent.ts,OptionalField.vue,YearMonthField.vue,DateRangeField.vue,PersonalDetailsPanel.vue,ContactList.vue}`                                  | Shared fields and personal details |
| 08   | `apps/web/test/editor/personal-details.test.ts`                                                                                                                                           | Personal/details RED/GREEN         |
| 09   | `apps/web/app/components/editor/forms/SectionPanel.vue`, `apps/web/app/components/editor/forms/entries/{Profile,Work,Education,Skill,Language,Certificate,Project,Custom}EntryFields.vue` | All eight entry types              |
| 09   | `apps/web/test/editor/entry-forms.test.ts`                                                                                                                                                | Eight-type matrix                  |
| 10   | `apps/web/app/components/editor/structure/{StructurePanel,SectionControls,EntryOrderControls}.vue`, `apps/web/test/editor/structure-controls.test.ts`                                     | Section/entry order controls       |
| 11   | `apps/web/app/components/editor/customization/{fields.ts,CustomizationPanel.vue,ColorField.vue,OptionalCustomizationField.vue}`                                                           | Schema-derived customization UI    |
| 11   | `apps/web/test/editor/customization-controls.test.ts`                                                                                                                                     | Leaf/delta matrix                  |
| 12   | `apps/web/app/components/editor/templates/{TemplatePanel,TemplatePartialDialog}.vue`, `apps/web/test/editor/template-panel.test.ts`                                                       | Apply/recovery/undo UI             |
| 13   | `apps/web/app/editor/photoController.ts`, `apps/web/app/components/editor/photo/{PhotoPanel,CropEditor}.vue`                                                                              | Bound private photo lifecycle      |
| 13   | `apps/web/test/editor/{photo-controller,photo-panel}.test.ts`                                                                                                                             | Privacy/crop/read matrix           |

## Shell and native proof

| Task | Create                                                                                                                                            | Responsibility                                       |
| ---- | ------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------- |
| 14   | `apps/web/app/pages/app/resumes/[id].vue`, `apps/web/app/components/editor/{EditorShell,EditorPreview,SaveStatus,ErrorSummary,ConflictPanel}.vue` | Route composition, pure preview, status/conflicts    |
| 14   | `apps/web/app/editor/pageCountObserver.ts`                                                                                                        | Settled visible `data-page-index` count              |
| 14   | `apps/web/app/composables/useUnsavedNavigationGuard.ts`, `apps/web/app/assets/css/editor.css`                                                     | Leave guard and responsive styles                    |
| 14   | `apps/web/test/editor/{editor-shell,editor-preview,page-count-observer,navigation-guard,accessibility,persistence-boundary}.test.ts`              | Integration, exact count/label, persistence boundary |
| 15   | Reserved editor harness paths above                                                                                                               | Native HTTPS scenarios and bounded evidence          |

Later tasks import earlier interfaces without reopening producer files. An
interface mismatch returns to the producer author through a serialized fix.
