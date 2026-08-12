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
`packages/schema/scripts/generate.mjs`, `packages/schema/package.json`,
`packages/schema/test/{gen,schema,released-versions,released-append-only,type-fidelity}.test.ts`,
`packages/schema/fixtures/**`, `packages/schema/templates/*.json`, generated
`packages/schema/gen/go/{resume.go,rawschema.go,released.go,v2/resume.go,v2/rawschema.go}`,
`packages/schema/gen/ts/{resume.ts,released.ts,v2/resume.ts}`, and
`apps/server/internal/resume/docmigrate/{docmigrate.go,v1_v2.go,v1_v2_test.go}`.
Never edit `packages/schema/resume.v1.schema.json` or retained v1 output.

This is the middle exclusive generator window: Task 1 must be verified first,
and Task 8 cannot start until this task is verified. This author writes the two
adversarial files test-first, before the implementation. OpenAPI has no
resume-document examples at the current base; if that fact changes before
dispatch, stop and assign the OpenAPI source and generated client as a separate
integration-owner contract change.

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
- `released-versions.json` remains the reviewed source for three independent
  declarations: `currentVersion: 2`, `acceptedVersions: [1, 2]`, and
  `emittedVersions: [1, 2]`. None is inferred from the released `versions`
  array. The generator validates every declared version is released and emits
  `CURRENT_VERSION`, `ACCEPTED_VERSIONS`, and `EMITTED_VERSIONS` in
  `gen/ts/released.ts`. It emits `CurrentVersion`, `AcceptedVersions()`, and
  `EmittedVersions()` in `gen/go/released.go`; both functions return copies.
- `packages/schema/package.json` exposes that generated TypeScript registry at
  the exact `@aboutme/schema/released` subpath. Renderer code imports
  `CURRENT_VERSION` from that subpath. The Go production projector consumes the
  generated current, accepted, and emitted declarations instead of maintaining a
  second hand-written list.
- Generic `EmitWire` remains lossless by default. Add an immutable production
  emission-loss policy whose input is the current, emitted, and restored full
  document plus the target version. It permits only the declared v2→v1 font
  fallback and rejects any other changed JSON value as `ErrLossyConversion`.
  Synthetic projectors without that policy retain exact round-trip behavior.
- Current storage advances to v2 only through normal writes or CAS backfill.
  Reads remain pure.

## Steps

- [ ] **Base gate.** Confirm Task 5's final catalog and assets are committed and
      reviewed. Record the exact base commit.
- [ ] **Write the adversarial tests first.** This author writes only
      `font-v2-adversarial.test.ts` and `v1_v2_adversarial_test.go` from the
      catalog, design, and interface above, runs their focused commands, and
      records the expected failure before the integration owner reads or writes
      any implementation diff. The integration owner never edits these files.
- [ ] Write separate failing author tests for ordered catalog/schema array
      equality, immutable-v1, converter matrix, generated drift, and preset
      validation.
- [ ] Add `resume.v2.schema.json`; update the release manifest to current v2,
      accepted `[1, 2]`, and emitted `[1, 2]`; then regenerate current plus
      retained v2 types and all three registry declarations. Add the
      `./released` package export. Verify v1 bytes and types are unchanged.
- [ ] Write concise v2 schema descriptions that state current constraints and
      point to `docs/design/` for rationale. Do not copy v1's task, review, or
      retired-file history into the new release; keep the immutable v1 bytes
      unchanged.
- [ ] Implement adjacent v1↔v2 converters and every catalog fallback. Test all
      families, unknown IDs, missing mappings, exact round trips for the
      original five, the declared font-only loss for every new family, and
      rejection when any non-font value changes.
- [ ] Wire the production projector to the generated current, accepted, and
      emitted declarations. Prove accepted/emitted arrays stay independently
      authored, every member is released, the package export resolves
      `CURRENT_VERSION === 2`, and the production loss policy is present only
      for v2→v1 font fallback.
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
- [ ] Record all owned diffs and exact output. The fresh integrated phase review
      inspects this high-risk change before the phase gates and push.

## Acceptance mapping

- AC-DOC-012: add the v2 immutable release, adjacent conversion, and fail-closed
  generated registry as a P3 extension to the row's retained P2A proof. Do not
  replace or erase the P2A evidence.
- AC-REN-009: schema, catalog, presets, types, and v1 fallbacks agree.
