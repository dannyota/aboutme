# Task 9: Golden snapshot harness (both modes × templates × fixtures)

Satisfies **AC-REN-001** and the golden half of AC-REN-002; the master plan's
"Renderer golden" CI row.

**Files:** create `apps/web/test/renderer/golden.test.ts`,
`apps/web/test/renderer/golden/*.html` (committed),
`packages/schema/fixtures/vn-full.json` (schema-valid; Vietnamese diacritics in
`fullName`, headline, every section type's text fields, dates, chips — also Task
11's screenshot subject).

**Matrix** (32 goldens — name = `<fixture>--<template>--<mode>.html`):

| Fixtures                                                         | Templates | Modes                 |
| ---------------------------------------------------------------- | --------- | --------------------- |
| `minimal`, `full`, `vn-full`, `draft-cleared-name-empty-section` | all 4     | `continuous`, `paged` |

- [ ] **Step 0: Fixture addition gate, serialized with P2A (B11).**
      `packages/schema/fixtures/vn-full.json` lands in the shared top-level
      fixtures directory that `packages/schema/test/schema.test.ts` enumerates
      via `readdirSync` (it will pick up and schema-validate the new file
      automatically) and that `packages/schema/gen/go/store_validate_test.go` —
      **P2A-owned** — reads fixtures from by name. Before committing this file:
      coordinate the add through the integration owner if P2A is running
      concurrently (both tasks must observe the same directory contents mid-run,
      not a partial add), then run `make schema-check` **and**
      `cd apps/server && go test ./...` (workspace resolves `gen/go`) and
      confirm both remain green. If
      `TestValidateDocument_CleanFixturesProduceNoIssues`'s explicit fixture
      list needs `vn-full.json` added to stay meaningful, that edit belongs to
      P2A's ownership of `store_validate_test.go`, not this task — report the
      need rather than editing it directly.
- [ ] **Step 1: Failing harness.** `golden.test.ts` opens with a file-level
      `// @vitest-environment node` pragma (B7 — this suite renders via plain
      `vue/server-renderer`, and the golden diff itself is the renderer-purity
      proof; happy-dom silently masking a stray DOM call would defeat that). For
      each cell: build props from the fixture +
      `applyTemplate(fixtureCustomization, preset)`, render via plain
      `renderToString` (paged mode uses the committed synthetic measurer),
      compare byte-exact against the committed golden;
      `UPDATE_GOLDEN=1 npm test` writes instead of compares. First run FAILs (no
      goldens); generate; re-run compares clean.
- [ ] **Step 2: Determinism proof.** The suite renders every cell **twice** in
      one run and asserts identity, and CI compares against the committed bytes
      (double protection: intra-run and cross-environment). Any `TZ`/locale
      sensitivity is a bug — verify by running the suite once with
      `TZ=Pacific/Kiritimati LANG=vi_VN.UTF-8` locally and recording the clean
      result in the task report.
- [ ] **Step 3: Hostile-document SSR surface.** Build an in-memory document
      embedding every corpus payload in every rich-text field (generated from
      `HOSTILE_CORPUS`, not hand-written), render it, and assert the
      neutralization predicate over the full page HTML — this is the **SSR** leg
      of AC-SEC-001's four-surface requirement. Not a golden (corpus changes
      shouldn't churn goldens).
- [ ] **Step 4: Review ergonomics.** Goldens are committed reviewable HTML; note
      in the test header that a golden diff **is** the review artifact (master
      plan: "CI diff = review"). If the golden dir needs a lint ignore, state
      the exact glob (`apps/web/test/renderer/golden/**`) in the task report for
      Task 10 to land — this task does not edit `eslint.config.mjs` itself (B8).
- [ ] **Step 5: Gate + commit.**
