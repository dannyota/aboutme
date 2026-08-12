# Phase 3 exit criteria

Every item passes at one unchanged candidate commit. A wrong or unsatisfiable
item is corrected in this phase, with the change noted.

## Product

- [ ] Resume schema v1 and its retained output are byte-unchanged. V2 is
      released through the manifest, generated types, adjacent converters, and
      current registry. The manifest declares current 2, accepted `[1, 2]`, and
      emitted `[1, 2]`; generated Go and TypeScript declarations and the
      `@aboutme/schema/released` export agree. Catalog IDs, schema enum, and
      preset IDs match exactly.
- [ ] Every font asset passes AC-FONT-001 against its exact official source,
      license, Reserved Font Names, final hashes, internal names, and declared
      asset policy. Every license and the generated notice index ship in the
      built runtime artifact.
- [ ] Coverage labels match the final bytes. Selected faces and fallbacks load
      locally with no third-party request. The English, Vietnamese, and renderer
      punctuation fixtures use only bundled fonts.
- [ ] Go and client sanitizers derive from one allowlist and pass the shared
      hostile corpus. SSR ships no Node DOM sanitizer. Browser output produces
      no dialog, page error, or unexplained CSP violation.
- [ ] The renderer is pure and renders every section, optional state, contact
      rule, template, and both display modes without losing content or order.
      Plain fields stay escaped text.
- [ ] Preset apply satisfies ADR 0008 and ADR 0021; section iteration satisfies
      ADR 0009.
- [ ] Pagination preserves every block exactly once and in order in both modes,
      and is deterministic for identical inputs.
- [ ] HTML goldens are byte-stable across two renders. The named screenshot
      subset passes at zero tolerance in the pinned AMD64 browser environment.
      Both PDF fragmentation cases raster and pass at zero tolerance. Baseline
      update flags are absent and no retry occurs. P9A owns the ARM64 rerun.
- [ ] The production build excludes the renderer harness. Import and
      nondeterminism negative fixtures fail for the intended reason.

## Checks

- [ ] `(cd packages/schema/gen/go && go test ./... -count=1)` and
      `make schema-check api-check server-build server-vet server-test` pass.
- [ ] `make web-lint web-typecheck web-test web-build` and the pinned-browser
      E2E command pass, with every Playwright artifact under
      `PLAYWRIGHT_RESULTS_DIR`.
- [ ] `make ci` and connected `make scan` pass once at the candidate commit.
- [ ] The browser source-boundary test passes, including its tracked and
      untracked secret-like negative controls, with manifest and archive hashes
      recorded.

## Review and records

- [ ] A fresh reviewer that authored none of the phase reports no blocking
      defect in behavior, design fit, license handling, interface stability, or
      traceability. Fixes are confirmed by the same reviewer.
- [ ] The adversarial cases in [adversarial coverage](adversarial-coverage.md)
      are present and unweakened.
- [ ] AC-SEC-001 and AC-SEC-003 record P3's evidence slices without marking the
      cross-phase rows complete. AC-SEC-004 records the renderer URL recheck,
      AC-DOC-012 records the v2 release, and AC-REN-001…009 plus AC-FONT-001 are
      `PROVEN` with exact evidence. P2B write, P5A public-read, and P7A print-
      read sanitizer handoffs stay with their owning phases.
- [ ] `docs/architecture.md` describes the implemented v2 registry, sanitizer,
      font, renderer, pagination, preset, golden, and browser boundaries, and
      distinguishes shipped behavior from later P2B/P5A/P7A integration.
