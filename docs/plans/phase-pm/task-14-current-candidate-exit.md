# Task 14 — Current-candidate review and phase exit

**Acceptance:** Close AC-MCP-001–010 under ADR 0024 without changing the MCP
product contract.

**Depends on:** T00–T13 complete; PM review fixes at `7899535`; phase PF exited.

**Author:** The integration owner owns candidate preparation, commands, and
records. The original PM non-author reviewer owns the review verdict and fix
confirmation. No worker edits Git state.

## Files

- Review: the complete PM range beginning at `54b24cf^`, with focused
  confirmation of `3134f67..7899535` and the PM-relevant changes after
  `7899535`.
- Reconcile if drift exists:
  `docs/{architecture.md,api/openapi.yaml,design/api.md,design/product.md,design/security.md,design/web.md,runbooks/local-uat.md}`,
  `docs/plans/traceability/{README.md,ac-mcp.md}`, and this phase plan.
- Current overlapping implementation:
  `apps/server/cmd/server/{main.go,main_test.go}`,
  `apps/server/internal/api/{capabilities.go,capabilities_test.go}`,
  `apps/server/internal/config/{config.go,config_test.go}`,
  `apps/web/app/{composables/useCapabilities.ts,pages/login.vue,pages/app/settings/sessions.vue}`,
  `apps/web/test/{authorize.test.ts,connected-agents.test.ts,login.test.ts,sessions.test.ts,support/capabilities.ts}`,
  `deploy/dev-https-browser/{harness-lib.ts,mcp.spec.ts,network-policy.ts,playwright.config.ts,run.sh,static-test.sh}`,
  `scripts/{dev-https-check.sh,dev-https-test.sh,dev-https.sh}`, and `Makefile`.
- Remove only in the final exit-record commit after Steps 1–4 pass:
  `docs/plans/phase-pm/` and `docs/plans/mcp-agent-access-design.md`. Phase exit
  remains pending until that unchanged commit passes Step 6.

## Interfaces

- Consumes: Approved v4, ADRs 0018/0024/0026/0027, the M1–M9 decisions,
  AC-MCP-001–010, and the current implementation and test surface.
- Produces: the original reviewer's written confirmation of T13, the earlier
  review fixes, and the current overlap; a passing M9 proof; passing `make ci`
  and connected `make scan` results at one unchanged commit; master-plan and
  traceability state that no longer name PM as active.

## Current-candidate review

The original PM review preceded PF. PF did not change the OAuth or MCP protocol
contract, but it changed shared composition, capability-gated settings UI,
provider-login defaults, and browser-harness diagnostics. The review therefore
must not stop at `7899535`. It confirms these current behaviors by name:

- disabled MCP registers no agent routes, reports `agentAccess=false`, hides
  Connected agents, and makes no grant-list request;
- enabled MCP reports `agentAccess=true` and retains consent, tool, and
  revocation behavior;
- every mutating tool requires a caller UUID idempotency key, an exact retry
  mutates once, and changed intent under one key fails closed without writing;
- the MCP HTTPS lifecycle enables provider login only for its seeded local
  sign-in and keeps the v1 default disabled elsewhere;
- the changed diagnostic filter does not mask an unexpected console, page,
  certificate, or external-request failure from `mcp-proof.json`.

## Closeout cycle

- [ ] **Step 1: establish a clean review candidate**

  Run:

  ```sh
  git status --short --branch
  git diff --check
  git diff --cached --name-only
  ```

  Expected: no staged files and no unexpected worktree changes. The current
  user-owned `AGENTS.md` change is a blocker to a clean candidate; preserve it
  and ask the human owner to resolve it. Do not stage, commit, stash, restore,
  or reset it.

- [ ] **Step 2: reconcile the plan and authorities before review**

  Run:

  ```sh
  make docs-lint
  make public-roots-check route-table-test api-check
  ```

  Expected: every command passes. Any authority disagreement is a defect; fix
  the authority and implementation together before continuing.

- [ ] **Step 3: obtain the current-candidate review verdict**

  Give the original reviewer the candidate commit and these read-only diffs:

  ```sh
  git diff --stat 54b24cf^..HEAD
  git diff 3134f67..7899535
  git diff --stat 7899535..HEAD
  ```

  Expected: the reviewer confirms the prior findings are fixed and explicitly
  names every invariant required by `exit-criteria.md`, including T13 and the
  four PF overlap behaviors above. A finding returns to an owning author for a
  test-first fix; the same reviewer confirms that fix before this step passes.

- [ ] **Step 4: refresh the native HTTPS proof**

  Run alone, with no other browser or build worker:

  ```sh
  make dev-native-down
  make dev-https
  make dev-https-status
  make dev-https-browser-image
  make dev-https-mcp-check
  make dev-https-down
  make dev-native
  ```

  Expected: all ten M9 steps are `true`; the repeated create returns the first
  resume without a second row; certificate, console, external-request, and page
  error counters are zero; the proof is at most 4,096 bytes and mode `0600`;
  fixture cleanup leaves no PM rows or resume.

- [ ] **Step 5: prepare the phase-exit record**

  Update `docs/plans/implementation-plan.md` to remove PM from active work and
  its dependency graph, update `docs/plans/traceability/README.md` so PM is no
  longer active, then delete `docs/plans/phase-pm/` and
  `docs/plans/mcp-agent-access-design.md`. Git history retains the plan and its
  design draft. Run `make docs-fmt`, inspect the exact diff, stage only those
  paths, and commit with `docs: record phase PM exit`.

- [ ] **Step 6: run the unchanged-candidate gates**

  Record `git rev-parse HEAD`. Run `make ci` alone. Then run connected
  `make scan` alone with `SEMGREP_APP_TOKEN` already present in the environment;
  never print the token. Confirm `git rev-parse HEAD` still matches and
  `git status --short` is empty.

  Expected: `make ci` and `make scan` pass at the same commit. A failure reopens
  the phase, returns to a test-first fix and reviewer confirmation, and requires
  both gates to rerun at the new candidate.

- [ ] **Step 7: push the phase candidate**

  Push only the reviewed commit that passed both gates. Do not start P5B until
  the remote branch contains that exact commit.
