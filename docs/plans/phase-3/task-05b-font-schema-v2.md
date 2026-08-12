# Task 5B: Release font catalog document version 2

The immutable v1 schema contains five font display names. The expanded catalog
cannot widen that released enum. This task releases v2 after Task 5 pins the
assets and final catalog IDs.

**Tier:** High risk (released schema, generated contracts, converters, and
current document storage). It runs after P2A closes and before renderer work.
Because schema heads, release manifests, and generated outputs are reserved
shared paths, the integration owner is this task's implementation author.

**Independent-test files:** `packages/schema/test/font-v2-adversarial.test.ts`
and `apps/server/internal/resume/docmigrate/v1_v2_adversarial_test.go`.

**Implementation files:** `packages/schema/resume.schema.json`, new
`packages/schema/resume.v2.schema.json`,
`packages/schema/released-versions.json`,
`packages/schema/scripts/generate.mjs`,
`packages/schema/test/{gen,schema,released-versions,released-append-only,type-fidelity}.test.ts`,
`packages/schema/fixtures/**`, `packages/schema/templates/*.json`, generated
`packages/schema/gen/go/{resume.go,rawschema.go,released.go,v2/resume.go,v2/rawschema.go}`,
`packages/schema/gen/ts/{resume.ts,released.ts,v2/resume.ts}`, and
`apps/server/internal/resume/docmigrate/{docmigrate.go,v1_v2.go,v1_v2_test.go}`.
Never edit `packages/schema/resume.v1.schema.json` or retained v1 output.

This is the middle exclusive generator window: Task 1 must be verified first,
and Task 8 cannot start until this task is verified. The independent test author
owns only the two adversarial files and freezes them before the implementation
author reads them. OpenAPI has no resume-document examples at the current base;
if that fact changes before dispatch, stop and assign the OpenAPI source and
generated client as a separate integration-owner contract change.

## Conversion contract

- v2 `customization.font.family` uses stable manifest IDs. The enum array equals
  the catalog ID array exactly, including manifest/UI rank order.
- v1→v2 maps `Be Vietnam Pro` → `be-vietnam-pro`, `Inter` → `inter`,
  `Source Sans 3` → `source-sans-3`, `Alegreya` → `alegreya`, and `Roboto Serif`
  → `roboto-serif` without changing their intended family.
- v2→v1 uses each catalog entry's explicit `v1Fallback`. It documents that the
  visual result may change for a family unavailable in v1.
- Converters preserve every non-font field byte-for-byte at the JSON-value level
  and validate source and target.
- Current storage advances to v2 only through normal writes or CAS backfill.
  Reads remain pure.

## Steps

- [ ] **Freeze independent tests first.** A fresh author writes only
      `font-v2-adversarial.test.ts` and `v1_v2_adversarial_test.go` from the
      catalog, design, and interface above, runs their focused commands, and
      records the expected failure before the integration owner reads or writes
      any implementation diff. The integration owner never edits these files.
- [ ] Write separate failing author tests for ordered catalog/schema array
      equality, immutable-v1, converter matrix, generated drift, and preset
      validation.
- [ ] Add `resume.v2.schema.json`, update the release manifest to current v2,
      and regenerate current plus retained v2 types. Verify v1 bytes and types
      are unchanged.
- [ ] Write concise v2 schema descriptions that state current constraints and
      point to `docs/design/` for rationale. Do not copy v1's task, review, or
      retired-file history into the new release; keep the immutable v1 bytes
      unchanged.
- [ ] Implement adjacent v1↔v2 converters and every catalog fallback. Test all
      families, unknown IDs, missing mappings, and round trips for the original
      five.
- [ ] Update the 20 presets to stable catalog IDs. Template behavior and every
      non-font token remain unchanged.
- [ ] Verify the OpenAPI source still has no resume-document example or compiled
      schema reference. If it gained one, stop and assign the OpenAPI source,
      tests, and generated client as a separate integration-owner contract
      change.
- [ ] Run `(cd packages/schema/gen/go && go test ./... -count=1)` and
      `make schema-check api-check server-build server-vet server-test`. With
      the integration owner-provided shared test database, run
      `make server-test-db server-test-integration server-migration-test`. The
      schema append-only test compares the new release with the phase base.
- [ ] Obtain an independent defect review and re-review of fixes. Record all
      owned diffs and exact output; do not stage or commit before phase
      integration.

## Acceptance mapping

- AC-DOC-012: immutable release, adjacent conversion, and fail-closed version
  registry.
- AC-REN-009: schema, catalog, presets, types, and v1 fallbacks agree.
