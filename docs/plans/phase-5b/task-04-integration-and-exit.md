# P5B T04 — integration and exit

## Contract

At the integrated candidate, update living documentation and traceability to
describe only behavior actually proved. Do not mark the phase complete before
the native HTTPS proof, full web gate, fresh review, `make ci`, connected
`make scan`, and the phase checklist pass at one unchanged candidate commit.

Update:

- `docs/architecture.md` for the owner publish workflow and explicit remaining
  P6/P7 boundaries.
- `docs/runbooks/local-uat.md` for `make dev-https-publish-check`.
- `docs/plans/traceability/ac-pub.md` evidence and states for
  `AC-PUB-006`–`AC-PUB-010`.
- `docs/plans/traceability/README.md` only if counts change.
- `docs/plans/implementation-plan.md` after exit.

One fresh Sol reviewer who authored none of P5B reads the integrated diff for
defects, design fit, stable interfaces, accessibility, and traceability. The
reviewer must confirm by name: session ownership, password reauthentication,
stable-first provider selection, provider-login capability gating, authorize-URL
allowlisting, second-click new-tab navigation without polling, retained-intent
explicit retry, CSRF refresh bound, parent CAS, retained idempotency across
uncertain replay, stale no-auto-republish, slug lifecycle, completeness issue
focus, public revocation, secret-free evidence, and the human-only MCP boundary.
An author fixes findings; the same reviewer confirms the fix.

## Ownership and checks

Owner: integration owner. Acceptance: close `AC-PUB-006` through `AC-PUB-010`;
do not close `AC-PUB-003` or `AC-PUB-005` before P7B.

Run the exact [`exit-criteria.md`](exit-criteria.md) checklist. After every item
passes, delete `docs/plans/phase-5b/` in the exit commit, update the roadmap to
P5B complete and pushed, inspect the explicit staged file list, run per-commit
gitleaks, commit with a Conventional Commit message, and push `main`. If any
check fails, fix it and rerun the affected check plus the unchanged-candidate
sequence.
