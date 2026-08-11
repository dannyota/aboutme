# Task 9: Golden snapshot harness (both modes × templates × fixtures)

Satisfies **AC-REN-001** and the golden half of AC-REN-002; the master plan's
"Renderer golden" CI row.

**Files:** create `apps/web/test/renderer/golden.test.ts`,
`apps/web/test/renderer/golden/*.html` (committed),
`packages/schema/fixtures/vn-full.json` (schema-valid; Vietnamese diacritics in
`fullName`, headline, every section type's text fields, dates, chips — also Task
11's screenshot subject).

**Matrix — 40 goldens** (name = `<preset>--<mode>.html`), per the owner's
coverage ruling of 2026-08-11 recorded in `tokens.md` §4.2:

| Fixture                    | Presets                                | Modes                 |
| -------------------------- | -------------------------------------- | --------------------- |
| `full` (fixture of record) | all 20 in `packages/schema/templates/` | `continuous`, `paged` |

20 presets × 2 modes = 40. The ruling scopes the two artifacts differently
because they cost differently: **string goldens cover every preset**, since they
are cheap, deterministic, and byte-diffable; **pixel baselines cover a named
six-preset subset only** (Task 11 Step 3). A preset outside that subset is still
covered here; what it loses is pixel-level regression detection.

Three consequences to implement literally:

- The preset list is enumerated from the `templates/` directory listing, never
  hand-written — the same anti-drift rule Task 8 applies to `TEMPLATES`. Adding
  a 21st preset must add two goldens and fail until they exist.
- `full.json` is the fixture of record because it is the richest document: all
  eight section types, a hidden work entry, and both `main` and `sidebar`
  populated, so one-column presets exercise main-then-sidebar and two-column
  presets exercise both flows. Per-surface clamp resolution (`tokens.md` §4.2)
  is pinned by the seven presets that tint — four `surfaceTarget: "header"`,
  three `"sidebar"` — not by the fixture, because `applyTemplate` replaces
  `customization` wholesale (ADR 0008), so the preset's tint is what renders.
- The fixture axis is collapsed deliberately, and what it used to cover moves to
  stronger and cheaper assertions: `minimal.json`'s absent-optional-token
  behaviour is a table case in Task 6's `useResumeStyles` test (each of the
  seven optional tokens absent → its fallback), the draft fixtures are Task 6
  Step 3, and `vn-full` is Task 11's screenshot subject plus Task 5's cmap test.
  Record this in the task report so the phase review reads it as a decision, not
  an omission.

## Steps

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
