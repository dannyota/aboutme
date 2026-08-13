# Task 9: Golden snapshot harness (both modes × templates)

Satisfies **AC-REN-001** and the golden half of AC-REN-002; the master plan's
"Renderer golden" CI row.

**Files:** create `apps/web/test/renderer/golden.test.ts` and
`golden.generate.mts`; the integration owner generates and applies
`apps/web/test/renderer/golden/*.html` (committed reserved snapshots); create
`apps/web/app/pages/_harness/photo-fixture.ts` (a fixed inline PNG data URL plus
its SHA-256), `packages/schema/fixtures/vn-full.json` (schema-valid; Vietnamese
diacritics in `fullName`, headline, every section type's text fields, dates,
chips — also Task 11's screenshot subject); modify
`packages/schema/gen/go/store_validate_test.go` only if its explicit clean
fixture list must name the new fixture.

**Matrix — 80 goldens** (name = `<preset>--start-<1|2>--<mode>.html`), per the
coverage contract in `colors.md` §4.2:

| Fixture state                          | Presets                                | Modes                 |
| -------------------------------------- | -------------------------------------- | --------------------- |
| `full`, populated start at one column  | all 20 in `packages/schema/templates/` | `continuous`, `paged` |
| `full`, populated start at two columns | all 20 in `packages/schema/templates/` | `continuous`, `paged` |

20 presets × 2 starting states × 2 modes = 80. The ruling scopes the two
artifacts differently because they cost differently: **string goldens cover
every preset**, since they are cheap, deterministic, and byte-diffable; **pixel
baselines cover a named six-preset subset only** (Task 11 Step 3). A preset
outside that subset is still covered here; what it loses is pixel-level
regression detection.

Three consequences to implement literally:

- The preset list is enumerated from the `templates/` directory listing, never
  hand-written — the same anti-drift rule Task 8 applies to `TEMPLATES`. Adding
  a 21st preset must add four goldens and fail until they exist.
- `full.json` is the fixture of record because it is the richest document: all
  eight section types, a hidden work entry, and both `main` and `sidebar`
  populated, so one-column presets exercise main-then-sidebar and two-column
  presets exercise both flows. Per-surface clamp resolution (`colors.md` §4.2)
  is pinned by the seven presets that tint — four `surfaceTarget: "header"`,
  three `"sidebar"` — not by the fixture, because `applyTemplate` replaces
  `customization` wholesale (ADR 0008), so the preset's tint is what renders.
- The fixture-file axis is collapsed deliberately, but ADR 0008's starting-
  column axis is retained. Clone `full` in memory and set populated one- and
  two-column `layout.sections` before `applyTemplate`; never add a second nearly
  identical fixture file. Other former fixture coverage moves to stronger and
  cheaper assertions: `minimal.json`'s absent-optional-token behaviour is a
  table case in Task 6's `useResumeStyles` test (each of the eight optional
  tokens absent → their fallbacks), the draft fixtures are Task 6 Step 3, and
  `vn-full` is Task 11's screenshot subject. Task 5 owns its earlier standalone
  coverage fixture. Record this mapping in the phase evidence.

## Steps

- [x] **Step 0: Fixture addition gate.** The P2A phase gate has already passed
      by precondition. Add `packages/schema/fixtures/vn-full.json` to the shared
      top-level fixtures directory that `packages/schema/test/schema.test.ts`
      enumerates via `readdirSync` (it will pick up and schema-validate the new
      file automatically) and that
      `packages/schema/gen/go/store_validate_test.go` reads by name. Update that
      explicit clean-fixture list when required. Run `make schema-check`,
      `(cd packages/schema/gen/go && go test ./... -count=1)`, and
      `(cd apps/server && go test ./...)`; confirm all remain green.
- [x] **Step 1: Failing harness.** `golden.test.ts` opens with a file-level
      `// @vitest-environment node` pragma (B7 — this suite renders via plain
      `vue/server-renderer`, and the golden diff itself is the renderer-purity
      proof; happy-dom silently masking a stray DOM call would defeat that). For
      each cell: create the SSR app, provide Task 7's `PaginationMeasureKey`
      with `apps/web/test/renderer/synthetic-measure.ts`, and assert the
      provider accepts `PaginationRequest` and returns `MeasuredLayout` without
      reading `window`, `document`, or an element. Clone `full` into the
      matrix's populated one- or two-column starting layout, then build props
      from that state plus
      `applyTemplate(startingCustomization, preset, fixture.content)`, render
      via plain `renderToString`, which must await `PagedResume`'s async
      provider result, compare byte-exact against the committed golden. Build
      `RenderContext` exactly as `{lng, mode, photoUrl?}`: map `full` to
      `lng: "en"`, `vn-full` to `lng: "vi"`, and the matrix cell to `mode`. Set
      `photoUrl` to the committed inline PNG exactly when the fixture has photo
      metadata. Verify the PNG hash before rendering; a metadata/URL mismatch
      must still exercise Task 6's typed error path. `golden.test.ts` fails
      before reading snapshots if `UPDATE_GOLDEN` or
      `PLAYWRIGHT_UPDATE_SNAPSHOTS` is present, even as an empty string, and can
      never write. The integration owner runs
      `(cd apps/web && npx --no-install tsx test/renderer/golden.generate.mts <ignored-output-dir>)`;
      the generator refuses a tracked output path. The owner reviews and applies
      the staged files to the reserved directory, then reruns the comparison.
- [x] **Step 2: Determinism proof.** The suite renders every cell **twice** in
      one run and asserts identity, and CI compares against the committed bytes
      (double protection: intra-run and cross-environment). Any `TZ`/locale
      sensitivity is a bug. The baseline run pins `TZ=UTC LANG=en_US.UTF-8`;
      verify again with `TZ=Pacific/Kiritimati LANG=vi_VN.UTF-8` locally and
      recording the clean result in the phase evidence.
- [x] **Step 3: Hostile-document SSR surface.** Build in-memory documents from
      every entry in Go's committed `corpus-output.golden.json`, place those
      already-sanitized fragments in every rich-text field, and render them.
      Parse each `.rich-text` element's `innerHTML` with the author-side
      predicate from `apps/web/test/sanitizer/neutralization.ts`; separately
      assert that payload nodes and attributes cannot escape those containers.
      Do not apply the rich-text allowlist to renderer-owned page markup. This
      proves the SSR pass-through leg of AC-SEC-001 without pretending the
      renderer sanitizes raw server input. P5A and P7A separately prove that
      their Go read boundaries produce these safe inputs. This is not a golden
      because a corpus update must not churn renderer snapshots.
- [x] **Step 4: Review ergonomics.** Goldens are committed reviewable HTML; note
      in the test header that a golden diff **is** the review artifact (master
      plan: "CI diff = review"). If the golden dir needs a lint ignore, state
      the exact glob (`apps/web/test/renderer/golden/**`) in the handoff for
      Task 10 to land — this task does not edit `eslint.config.mjs` itself (B8).
- [x] **Step 5: gate.** Run
      `make schema-check web-lint web-typecheck web-test web-build`.

## Completion evidence

- The generated matrix contains 80 committed HTML files: 20 directory-enumerated
  presets × two populated starting layouts × continuous and paged modes.
- `full.json` remains the matrix subject. `minimal.json` optional-token cases
  remain in Task 6, draft behavior remains in Task 6, and `vn-full.json` is the
  Task 11 browser subject.
- The full 117-test golden suite passed under both `TZ=UTC LANG=en_US.UTF-8` and
  `TZ=Pacific/Kiritimati LANG=vi_VN.UTF-8`.
- `make schema-check web-lint web-typecheck web-test web-build` passed on the
  applied baselines. The web suite reported 446 passing tests and one skip.

## Acceptance mapping

- AC-REN-001: every preset and both modes are deterministic within a run and
  byte-stable against committed output.
- AC-REN-002: continuous and paged output preserve content and order.
- AC-SEC-001: hostile rich text is neutralized on the SSR renderer surface.
