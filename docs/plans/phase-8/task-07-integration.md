# Task 8.7 — Commands, browser proof and phase exit

**Owner:** integration owner; a bounded browser author may own one new spec.
**Acceptance:** all Phase 8 rows. Read every task and the exit checklist.

**Owned paths:** server command dispatch and wiring, Makefile, build and runtime
scripts, generated code, route/OpenAPI parity tests, a new
`deploy/dev-https-browser/privacy.spec.ts`, browser harness registration, living
docs/runbook, traceability, future Phase 10 schedule contracts and Git.

- [ ] Dispatch the four exact one-shot commands before server config/Chromium
      initialization. Load only their needed config. Bound each run at 30
      minutes; cancellation joins work. Unknown command/flag exits nonzero.
      Fixed structured results include overlap, success, backlog, oldest age,
      overdue work and failures.
- [ ] Add make targets and native command smoke evidence; production image must
      contain the same command entry point. Add hourly media deletion to the
      Phase 10 schedule table and consume the final flags/timeouts.
- [ ] Author browser tests with a live Playwright MCP inspection, then commit
      and execute the observed selectors as headless specs. A fresh login is
      recently reauthenticated. Force one exact DELETE `reauth_required`
      response to exercise that UI branch, then use real provider
      reauthentication and explicit confirmed deletion. The live account API
      tests prove actual stale-session refusal.
- [ ] Prove export, cancel, reauth and deletion, then old session/grant/public
      HTML/JSON/photo/PDF/share image/discovery absence and tombstone behavior.
      Use synthetic data and bounded non-personal evidence.
- [ ] Run affected native HTTPS auth/editor/MCP/entry and public checks plus
      HTTP public parity; run focused jobs and route tests.
- [ ] Update architecture, server README, privacy runbook, acceptance rows and
      resource/scheduler handoff from actual results.
- [ ] Commit coherent slices with per-commit gitleaks after inspecting owned
      diffs and the staged file list.
- [ ] Obtain one fresh Sol phase review and fix findings with their authors.
- [ ] Run the phase exit checklist, full `make ci` alone and connected
      `make scan` at one unchanged candidate before push.
- [ ] Delete the committed phase plan at closure; preserve its tested contracts
      in living docs and Git history. Push the reviewed phase and open its PR.

Report only checks actually run. Hosted schedule activation, backup expiry,
alarm delivery and complete hosted UAT remain Phase 10.
