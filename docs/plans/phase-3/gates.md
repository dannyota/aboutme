# Phase 3 exit criteria

Every item passes at one unchanged candidate commit. A wrong or unsatisfiable
item is corrected in this phase, with the change noted.

## Product

- [x] Resume schema v1 and its retained output are byte-unchanged. V2 is
      released through the manifest, generated types, adjacent converters, and
      current registry. The manifest declares current 2, accepted `[1, 2]`, and
      emitted `[1, 2]`; generated Go and TypeScript declarations and the
      `@aboutme/schema/released` export agree. Catalog IDs, schema enum, and
      preset IDs match exactly.
- [x] Every font asset passes AC-FONT-001 against its exact official source,
      license, Reserved Font Names, final hashes, internal names, and declared
      asset policy. Every license and the generated notice index ship in the
      built runtime artifact.
- [x] Coverage labels match the final bytes. Selected faces and fallbacks load
      locally with no third-party request. The English, Vietnamese, and renderer
      punctuation fixtures use only bundled fonts.
- [x] Go and client sanitizers derive from one allowlist and pass the shared
      hostile corpus. SSR ships no Node DOM sanitizer. Browser output produces
      no dialog, page error, or unexplained CSP violation.
- [x] The renderer is pure and renders every section, optional state, contact
      rule, template, and both display modes without losing content or order.
      Plain fields stay escaped text.
- [x] Preset apply satisfies ADR 0008 and ADR 0021; section iteration satisfies
      ADR 0009.
- [x] Pagination preserves every block exactly once and in order in both modes,
      and is deterministic for identical inputs.
- [x] HTML goldens are byte-stable across two renders. The named screenshot
      subset passes at zero tolerance in the pinned AMD64 browser environment.
      Both PDF fragmentation cases raster and pass at zero tolerance. Baseline
      update flags are absent and no retry occurs. P9A owns the ARM64 rerun.
- [x] The production build excludes the renderer harness. Import and
      nondeterminism negative fixtures fail for the intended reason.

## Checks

- [x] `(cd packages/schema/gen/go && go test ./... -count=1)` and
      `make schema-check api-check server-build server-vet server-test` pass.
- [x] `make web-lint web-typecheck web-test web-build` and the pinned-browser
      E2E command pass, with every Playwright artifact under
      `PLAYWRIGHT_RESULTS_DIR`.
- [x] `make ci` and connected `make scan` pass once at the candidate commit.
- [x] The browser source-boundary test passes, including its tracked and
      untracked secret-like negative controls, with manifest and archive hashes
      recorded.

## Review and records

- [x] A fresh reviewer that authored none of the phase reports no blocking
      defect in behavior, design fit, license handling, interface stability, or
      traceability. Fixes are confirmed by the same reviewer.
- [x] The adversarial cases in [adversarial coverage](adversarial-coverage.md)
      are present and unweakened.
- [x] AC-SEC-001 and AC-SEC-003 record P3's evidence slices without marking the
      cross-phase rows complete. AC-SEC-004 records the renderer URL recheck,
      AC-DOC-012 records the v2 release, and AC-REN-001…009 plus AC-FONT-001 are
      `PROVEN` with exact evidence. The shipped P2B write sanitizer integration
      stays distinct from the P5A public-read and P7A print-read handoffs.
- [x] `docs/architecture.md` describes the implemented v2 registry, sanitizer,
      font, renderer, pagination, preset, golden, and browser boundaries, and
      distinguishes the shipped P2B write integration from the later P5A/P7A
      read integrations.

## Evidence

The implementation candidate was `bb762b6826f62bdfdd82d00bf67b053255c72ab9`. The
results below are its recorded history. The record commit is accepted and pushed
only after the integration owner reruns `make ci`, connected `make scan`, and
the `p3-final` browser command without changing it; this avoids a self-
referential commit hash in the record.

- `make ci`: passed every operational, documentation, schema, API, Go, web,
  live-database, race-enabled S3/media, migration, and route-table check.
- `make scan`: connected scan `209992782` passed 135,494 rules over 328 tracked
  files with zero blocking findings; full-history gitleaks scanned 234 commits
  and found no leak.
- The prior `WEB_E2E_RUN_ID=p3-review-fixes make web-e2e` run passed 64 harness
  tests and one normal Nuxt CSP test in the pinned AMD64 Chromium environment.
  Before push, the record commit must pass the same zero-tolerance comparison as
  `WEB_E2E_RUN_ID=p3-final make web-e2e`; all artifacts must stay under
  `PLAYWRIGHT_RESULTS_DIR`.
- `bash scripts/web-e2e-source.test.sh`: passed the exact-commit, dirty-state,
  index-flag, and secret-like rejection matrix. The source manifest SHA-256 is
  `9c758f7a27bee8ef971781734e9b3472d2a960924de83cdf98e71f1328320263`; the
  deterministic source archive SHA-256 is
  `f3ae851caf58eb454465322c8ca152b595871e1a8531cce85acbec791b950267`.
- Fresh integrated review found no remaining defect after rechecking sanitizer
  authority, renderer purity, pagination, templates, font licensing, v1/v2
  immutability, CSP, browser baselines, update safety, and archive boundaries.

Traceability records P3's evidence slices for AC-SEC-001/003/004 and AC-DOC-012,
and marks AC-REN-001…009 plus AC-FONT-001 `PROVEN`. P2B write sanitizing has
shipped; P5A public-read and P7A print-read sanitizer integration remain with
those phases.
