# Phase 4 Authenticated Editor Implementation Plan

<!-- prettier-ignore -->
> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a working authenticated browser editor for current-v2 resumes,
with immediate pure-renderer preview, safe one-second autosave, conflict/session
recovery, private photo editing, and trusted native HTTPS proof.

**Architecture:** Client-only `/app/resumes/**` routes feed typed actions into a
Pinia store keyed by resume ID. Pure command, projection, reconciliation, and
template-group modules sit between UI and one coordinator. The coordinator alone
freezes and sends writes. Preview passes only optimistic document, total
language, paged mode, and matching authorized photo data to the Phase 3
renderer.

**Tech Stack:** Nuxt 4, Vue 3, TypeScript 6.0.3, Pinia, ProseMirror,
OpenAPI-derived request types, current-v2 schema types, Vitest, Playwright,
Caddy 2.11.4, and the native HTTPS harness.

## Global Constraints

- The four approved Phase 4 contracts, Design v4, and accepted ADRs 0005, 0008,
  0009, 0016, 0017, and 0024 govern.
- Authenticated calls run only in-browser, same-origin, with credentials.
  `useAuth` response state is the sole CSRF source.
- Every authenticated response carries exact
  `Cache-Control: no-store, no-transform`. Native Caddy preserves exact strong
  parent tag `"rN"`; the next mutation sends the same bytes as `If-Match`.
- Revision is a canonical decimal string in `[1, 9223372036854775807]`, never a
  JavaScript number.
- Each resume has at most one in-flight write. Debounce is exactly one second
  after its last edit. Different resumes may save independently.
- Exact retry preserves operation, path, schema, precondition, semantic bytes,
  key, and first-dispatch cutoff.
- Draft saves preserve absence, null, empty string, zero, and array order. They
  never enforce publish completeness or fabricate placeholders.
- Rich text uses sanitizer allowlist v1 and a closed ProseMirror schema. Plain
  fields remain Vue bindings.
- Renderer files never import editor/store/API/Pinia/Nuxt/network code. Editor
  files do not alter renderer code.
- No resume content, photo bytes, CSRF/key, command, or payload enters local or
  session storage, IndexedDB, URL/query/hash, sendBeacon, logs, or evidence.
- Publishing, public routes, SSE, PDF/image export, cloud work, and full P9
  port-443 UAT are excluded.
- One author owns each task's RED tests, minimal GREEN, and adversarial cases.
  There is no per-task reviewer. ADR 0024 permits one fresh phase review only.
- Workers edit only exact task paths and never use Git. Root manifests,
  lockfiles, generated output, `Makefile`, Caddy, harness, and shared records
  use serialized integration-owner windows.
- Run at most three build/test/lint/browser/scan processes concurrently. Full
  `make ci` and connected `make scan` run alone.

---

## Plan documents

- [Decisions](decisions.md) closes implementation choices.
- [File structure](file-structure.md) assigns each implementation path.
- [Integration handoffs](integration-handoffs.md) owns shared windows, reports,
  and candidate-record ordering.
- [Exit criteria](exit-criteria.md) defines the final unchanged-candidate gate.
- [AC-EDITOR traceability](../traceability/ac-editor.md) assigns acceptance.

## Task index

| Task                                                | Deliverable                                                         | Acceptance                  |
| --------------------------------------------------- | ------------------------------------------------------------------- | --------------------------- |
| [00](task-00-integration-prerequisites.md)          | Cache/ETag browser prerequisite and dependencies                    | 001, 017                    |
| [01](task-01-command-engine.md)                     | Domain types, runtime validation, commands, projections, coalescing | 001, 003, 007               |
| [02](task-02-reconciliation-and-template-groups.md) | Reconciliation, conflicts, template-group engine                    | 006, 012                    |
| [03](task-03-transport-and-attempts.md)             | Frozen attempts, closed result unions, owner-photo transport        | 001, 005, 007, 013          |
| [04](task-04-resume-store.md)                       | Pinia accepted/current/queue/adoption state                         | 003, 007, 016               |
| [05](task-05-coordinator-and-session.md)            | Debounce, retries, reconciliation, create, session recovery         | 004–007, 012, 016           |
| [06](task-06-resume-list.md)                        | List, blank create, open, rename, confirmed delete                  | 002, 016                    |
| [07](task-07-rich-text.md)                          | Closed rich text and hostile-input boundary                         | 011, 015                    |
| [08](task-08-personal-details.md)                   | Shared draft fields, contacts, personal details                     | 008, 015                    |
| [09](task-09-entry-forms.md)                        | All eight entry types                                               | 008, 015                    |
| [10](task-10-structure-controls.md)                 | Section structure and entry order                                   | 009, 015                    |
| [11](task-11-customization-controls.md)             | Every customization leaf and optional group                         | 010, 015                    |
| [12](task-12-template-controls.md)                  | Apply, status, partial recovery, guarded undo                       | 012, 015                    |
| [13](task-13-photo-editor.md)                       | Owner photo read/upload/crop/replace/delete                         | 013, 015                    |
| [14](task-14-editor-shell-and-preview.md)           | Route, pure preview, errors, leave/persistence boundary             | 006, 014–016                |
| [15](task-15-native-https-editor-uat.md)            | Material native HTTPS editor scenarios                              | 001, 002, 004, 006, 012–017 |

## Waves and dependencies

| Wave | Tasks                                   | Start condition                    | Resource rule                                                             |
| ---- | --------------------------------------- | ---------------------------------- | ------------------------------------------------------------------------- |
| W0   | Pre-dispatch records, then 00           | P2B/P3/native HTTPS baseline green | Integration owner alone; serialized record, transport, dependency windows |
| W1   | 01                                      | W0 lands                           | One author establishes domain types                                       |
| W2   | 02, 03                                  | 01 lands                           | Disjoint; at most two heavy checks                                        |
| W3   | 04                                      | 02 and 03 land                     | One store author                                                          |
| W4   | 05                                      | 04 lands                           | One coordinator author; exclusive heavy slot                              |
| W5   | 06, 07, 10                              | 05 lands                           | Disjoint; at most three heavy checks                                      |
| W6   | 08, 11, 12                              | W5 interfaces land                 | Disjoint; at most three heavy checks                                      |
| W7   | 09, 13                                  | 07/08 and photo prerequisites land | Disjoint; at most two heavy checks                                        |
| W8   | 14                                      | 06–13 land                         | One UI integration author                                                 |
| W9   | 15                                      | All focused web gates pass         | Owner browser gate alone                                                  |
| W10  | Candidate records and preliminary gates | Task reports integrated            | Owner alone; create record commit before review                           |
| W11  | Fresh phase review                      | Record commit exists               | One non-author; same reviewer confirms fixes                              |
| W12  | Definitive exit                         | Reviewed candidate unchanged       | Owner runs full gates alone, then pushes                                  |

Task 01 defines domain/runtime types. Task 02 defines conflict/template queue
types. Task 03 defines `ServerValidationIssue` and transport/result types,
including `readOwnerPhoto`. Task 04 alone defines `CompletionAdoption` and
opaque photo state. Task 05 alone defines the compile-ready
`ResumeEditorActions` boundary and opaque create actions. No consumer redeclares
them. UI tasks own disjoint directories/files; Task 14 only composes their
public interfaces and its settled visible-page observer.

## Dispatch and completion

The owner dispatches the exact task file, authorities, acceptance IDs, paths,
and base. The author records each expected RED, implements the smallest GREEN,
runs every owned suite again in the final gate, and returns the report in
[integration-handoffs.md](integration-handoffs.md#task-report-format). Authors
suggest but do not execute commits.

After Task 15, follow the record-commit sequence in integration handoffs. The
sole fresh reviewer reads the complete record commit and confirms
authentication, sessions, CSRF, sanitizing, CAS, idempotency, unknown outcomes,
template partial, media privacy, renderer purity, persistence boundary,
accessibility, and secret-free evidence by name. Findings return to an author;
the same reviewer confirms fixes. The owner then runs definitive `make ci` and
connected `make scan` at the unchanged final candidate before push.
