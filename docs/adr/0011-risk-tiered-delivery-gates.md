# 0011: Delivery gates are risk-tiered, and the local gate is authoritative

Status: Accepted (2026-08-11). Superseded in part by
[ADR 0024](0024-single-pass-delivery-gates.md).

## Context

The delivery process applied the same ceremony to every change. Each task got an
independent defect review by a fresh worker; high-risk and low-risk work alike
passed through the same sequence. Each phase then got five separate gates:
design and consistency review, traceability closure, a fresh adversarial review,
independent fail-closed automated acceptance, and independent evidence
verification.

One phase shows the cost concretely. Of its twelve tasks, three exist only to
have a second worker write tests: one for write-safety and cap concurrency, one
for doc-migration purity and CAS-vs-autosave races, and one for an independently
derived size-bound limit+1 matrix. Every remaining task carries its own
"independent defect review, then commit" step. A single task therefore costs
roughly four agent passes before it lands.

That structure was not wrong. It was calibrated for changes where a defect is
expensive and hard to detect: session rotation, CAS races, sanitizer bypass. It
caught real defects at two early phase gates, both of which returned no-ship on
first run. The problem is that it is applied uniformly, so a Nuxt page or a
documentation restructure pays the same price as the session store.

Two of the five phase gates also overlap in practice. Design and consistency
review, traceability closure, and adversarial review are all judgments a single
competent reviewer makes while reading the phase diff against the spec; running
them as three sequential fresh workers mostly re-reads the same material.

Separately, verification latency was dominated by GitHub Actions. Every check in
`.github/workflows/ci.yml` can run on the development laptop: Go, Node, podman,
golangci-lint, govulncheck, sqlc, caddy, semgrep, and gitleaks are all installed
locally, and Postgres runs in a throwaway podman container. Pushing to observe a
result that was already available locally added a network round trip to every
iteration.

## Decision

**Review effort follows risk.** Two tiers:

| Tier          | Applies to                                                                                                                                                                                                              | Required before commit                                                                                      |
| ------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------- |
| **High risk** | authentication, authorization, sessions, CSRF; concurrency, CAS, idempotency; migrations and schema; rich-text sanitization; publish and cache invalidation; SSE; render and resource bounds; any code handling secrets | Author TDD, then a fresh worker deriving tests from the spec before reading the diff, then a fresh reviewer |
| **Normal**    | everything else: UI, editor surfaces, docs, refactors, scaffolding, configuration, tooling                                                                                                                              | Author TDD plus `make ci`                                                                                   |

**Phases have two gates, not five.** A phase defect review by a reviewer who
authored none of the phase, which absorbs design-consistency checking,
interface-stability checking, traceability closure, and adversarial challenge as
items within one reading; and a fail-closed automated acceptance run by a fresh
worker that cannot edit product code, tests, snapshots, seeds, or criteria. The
fail-closed report contract is unchanged: commit, exact commands, timestamps,
state changes, retry count, one expected/observed/PASS|FAIL|BLOCKED row per
criterion, `BLOCKED` counting as failure.

**`make ci` is the gate of record.** It runs the full check set locally,
including the podman-backed database suites, and is what must pass before a
handoff. Pushing happens once per phase rather than once per commit. GitHub
Actions remains for fork pull requests and for jobs that need repository
secrets.

**Security scanning splits by cost.** `gitleaks` runs per commit through
`.githooks/pre-commit`, because this repository is public and a secret in
history is exposed on push and cannot be recalled. Semgrep runs at phase gates
through `make scan`, batching SAST and Supply Chain analysis rather than paying
them per commit.

**Browser validation moves earlier.** User-visible changes are exercised through
scripted headless Playwright (`make web-e2e` and the `make dev-https-*-check`
suites) as they land, instead of deferring all browser defects to UAT. The
project-scoped Playwright MCP server is for agent exploration and test authoring
only. UAT and its evidence review are unchanged and remain the gate before AWS
authorization.

**Design work runs in parallel with implementation.** Template, UI, and spec
design depends only on frozen contracts, so it does not queue behind the store
and API phases.

This carries forward the suites already in flight under the old five-gate
system: independently derived, blind suites for write-safety, cap concurrency,
doc-migration purity, and CAS-vs-autosave races stay separate, because those
surfaces are high-risk. A suite whose expected values are mechanically derivable
from `budgets.md` and the schema, such as the size-bound limit+1 matrix, folds
into the author's own tests instead, because a second worker re-deriving it buys
accuracy that the shared bounds-parity test already provides.

## Consequences

Correct tier assignment is now load-bearing. A change misfiled as normal skips
the independent suite and the separate reviewer, so ambiguous cases are
classified high-risk. The tier list is deliberately concrete rather than
principled, so that classification is a lookup and not a judgment call.

The phase defect reviewer carries more at once: defects, spec consistency,
interface stability, traceability, and adversarial challenge in one reading.
This trades some of the independence that came from separate fresh workers for
latency. It is a real reduction in assurance, accepted knowingly, and the
mitigation is that the high-risk tier keeps its three separate passes where the
consequences of a miss are worst.

Batching Semgrep to phase gates means SAST findings arrive after the code they
would have changed. The blast radius is bounded to one phase rather than the
whole feature set, and `make semgrep` runs offline with no token for a quicker
local check when a change is security-sensitive.

Local CI shifts verification onto one machine. A defect that depends on the CI
environment will now be found at phase push rather than per commit: a missing
pinned tool, a Linux-only path, a permissions difference. The append-only
migration check is the clearest example: locally it compares against
`origin/main` rather than a pull-request base, so it is weaker than the CI job
it mirrors.

This changes process, not product. It supersedes prior agent-workflow and
testing-strategy guidance and the delivery gates section of `AGENTS.md`.
Completed gate records keep their historical role labels and verdicts and are
not rewritten.
