# Phase 3 exit criteria

Every item passes at one unchanged candidate commit.

## Contracts and implementation

- [ ] Resume schema v1 and retained output are byte-unchanged. V2 is released
      through the manifest, generated types, adjacent converters, and current
      registry. Catalog IDs, schema enum, and preset IDs agree exactly.
- [ ] Every font asset passes AC-FONT-001 against its exact official source,
      license, Reserved Font Names, final hashes, internal names, and declared
      asset policy. Required notices ship.
- [ ] Coverage labels match the final bytes. Selected faces and fallbacks load
      locally with no third-party request. The declared English, Vietnamese, and
      renderer punctuation fixtures use only bundled fonts.
- [ ] Go and client sanitizers derive from one allowlist and pass the shared
      hostile corpus. SSR ships no Node DOM sanitizer. Browser output produces
      no dialog, page error, or unexplained CSP violation.
- [ ] The renderer is pure and renders every section, optional state, contact
      rule, template, and both display modes without losing content or order.
- [ ] All preset apply operations satisfy ADR 0008 and all section iteration
      satisfies ADR 0009.
- [ ] HTML goldens are byte-stable across two renders. The named screenshot
      subset passes at zero tolerance in the pinned, locally runnable AMD64
      browser environment. Both actual-PDF fragmentation cases raster and pass
      at zero tolerance. P9A owns the production ARM64 launch-gate rerun.
- [ ] The production build excludes the renderer harness. Import and
      nondeterminism negative fixtures fail for the intended reason.

## Checks and traceability

- [ ] `(cd packages/schema/gen/go && go test ./... -count=1)` and
      `make schema-check api-check server-build server-vet server-test` pass.
- [ ] `make web-lint web-typecheck web-test web-build` and the pinned-browser
      E2E command pass. Baseline update flags are absent and no retry occurs.
      Its commit/run result directory was new, and every Playwright reporter and
      comparison artifact remained below `PLAYWRIGHT_RESULTS_DIR`.
- [ ] `make ci` and `make scan` pass once at the candidate commit.
- [ ] The browser source-boundary test diff predates its implementation. Its
      tracked and untracked secret-like negative controls pass, the explicit
      manifest and archive hashes are recorded, and the fresh boundary reviewer
      reports no blocking finding.
- [ ] Independent sanitizer and render-bound suites were derived before their
      authors read the implementation diff and remain unweakened. One frozen
      test-only diff contains only the plain-field and bounds suites and
      predates Task 6. A second pagination-only test diff, written by a
      different fresh author after the Task 6 renderer gate, predates Task 7.
      Evidence records both diffs separately.
- [ ] AC-SEC-001, the P3 half of AC-SEC-003, AC-REN-001…009, and AC-FONT-001 are
      `PROVEN` with exact evidence. P2B write, P5A public-read, and P7A
      internal-print-read sanitizer handoffs remain assigned to their owning
      gates.

## Phase gates

- [ ] A fresh reviewer that authored none of the phase reports no blocking
      defect in behavior, design fit, license handling, interface stability,
      assumptions, or traceability. Fixes receive independent re-review.
- [ ] A fresh acceptance worker runs a catalog frozen before the run, edits no
      code, tests, fixtures, snapshots, seeds, or criteria, and reports every
      row `PASS` at the exact commit. `FAIL`, `BLOCKED`, missing evidence, or an
      undisclosed retry fails the phase. The fresh catalog author owns
      `docs/plans/phase-3/acceptance-catalog-r1.md` and freezes it before this
      worker starts; a correction uses the next revision.
