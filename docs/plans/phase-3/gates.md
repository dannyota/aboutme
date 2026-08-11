# Phase exit criteria

- [ ] `make schema-check` green, including the new generated sanitizer/template
      artifacts and their faithfulness tests;
      `cd apps/server && go build     ./... && go vet ./... && go test ./...`
      green (workspace resolves `gen/go`).
- [ ] `make server-build server-vet server-test` and `make semgrep` green with
      `internal/sanitize` present; the corpus-output artifact committed and
      byte-stable across two runs.
- [ ] `make web-lint web-typecheck web-test web-build` green — including all 40
      goldens byte-stable (20 presets × both pagination modes, rendered twice
      per run — the 2026-08-11 coverage ruling, `tokens.md` §4.2), the DOMPurify
      corpus + cross-agreement fixed-point suite, fonts sha256/cmap/enum tests,
      the import-rule negative fixtures, and the harness-absence build test.
- [ ] `make web-e2e` (or the documented container invocation, output recorded)
      green: 7 zero-tolerance screenshots — the six named presets
      (`classic-serif`, `engineer-compact`, `modern-sidebar`, `executive-band`,
      `consulting-formal`, `academic-dense`) plus the continuous-mode case, per
      Task 11 Step 3 — offline-fonts proof with zero external request attempts,
      all corpus payloads neutralized in the real browser with zero
      dialogs/pageerrors/CSP violations on the sanitized path, and the CSP
      backstop holding on the raw path.
- [ ] Both blind adversarial suites (Task 4) authored before implementation
      diffs were read (attested in the task reports), landed unweakened, and
      green; the render-bounds number recorded and handed to the integration
      owner.
- [ ] All four sanitizer surfaces demonstrated: bluemonday (Task 2), DOMPurify
      (Task 3), SSR (Task 9 Step 3), real browser + CSP (Task 11 Step 4) — the
      complete AC-SEC-001 evidence set.
- [ ] `docs/plans/traceability/`: AC-SEC-001 and AC-SEC-003 references filled;
      AC-SEC-004's NEW-M7 note resolved to the Task 6 chip tests; AC-REN-001…008
      rows filled (or the phase gate records why not).
- [ ] Requested integration-owner artifacts resolved: `web-e2e`/`web-e2e-update`
      targets + CI job exist (or the gate records the standing exception).
- [ ] Every task delivered per its ADR 0011 risk tier (the risk-tier column of
      the task index in [`README.md`](README.md)): high-risk tasks get author
      TDD, then a fresh worker deriving tests from the spec before reading the
      diff, then a fresh reviewer; normal-risk tasks get author TDD plus
      `make ci`. The sanitizer tasks additionally covered by the Task 4
      independence trail. No author signed off its own work anywhere in the
      phase.
- [ ] **Automated phase-acceptance catalog (B1).** `docs/plans/uat-phase-3.md`
      exists, authored by the integration owner **before** this phase's
      acceptance run and left unmodified during it (pattern:
      `docs/plans/uat-phase-1.md` — run preconditions, acceptance-ID-mapped
      scenarios, `BLOCKED` counts as `FAIL`); a fresh worker that cannot edit
      product code, tests, snapshots, seeds, or criteria (ADR 0011) executes it
      fail-closed and its report is attached to the gate.
- [ ] **Phase defect review (ADR 0011).** A reviewer who authored none of the
      phase's design or implementation has read the phase diff once, covering
      defects, design consistency, interface stability, traceability closure,
      and adversarial challenge (at minimum: the D3/D5/D10 owner rulings, D2's
      agreement definition, and the blind-suite independence claims) as items
      within that single reading — this replaces the separate per-task defect
      review.
- [ ] **Evidence pinned to the shipping commit (B1).** Every UAT row and the
      adversarial review's findings are pinned to the exact commit being
      shipped; any product-code commit landing after they ran makes every row or
      finding that probes a changed path stale, and those scenarios are re-run
      at a new pinned commit before this bullet is satisfied.
- [ ] `make docs-fmt && make docs-lint` green for every `.md` touched.
