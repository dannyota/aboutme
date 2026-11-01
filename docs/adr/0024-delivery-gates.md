# 0024: Delivery gates

Status: Accepted (2026-08-11, 2026-08-12, 2026-09-19, 2026-09-20, 2026-10-10,
2026-11-01), by the owner's repository policy.

## Decision

- **One author pass.** The author writes the failing test first, makes the
  smallest correct change, and runs only checks that are safe under the local
  resource policy. When the regression test cannot run safely, the author
  records the exact unrun command and the expected failure before implementing
  the change. No gate requires the author to observe the failure locally.
- **Adversarial cases belong to the owning task.** The author writes the cases
  for write safety, races and CAS, size bounds, hostile input, authorization,
  and CSRF. There is no separate blind test author.
- **Per-commit gitleaks.** Each commit runs the pre-commit gitleaks scan.
- **One fresh review.** One reviewer who authored none of the work reads each
  plan or release before push. Local-only, test-only, and one-line changes skip
  this review. For one-line changes, including production DNS, the manager shows
  plan or diff evidence instead. Findings return to the author, and the same
  reviewer confirms each fix at `xhigh`. The reviewer runs as a separate job
  from the author and may use the author's model. A security-sensitive change
  beyond one line (authentication, sessions, CSRF, sanitizing, concurrency and
  CAS, idempotency, media privacy, publish revocation) needs the reviewer to
  confirm those invariants by name.
- **GitHub CI is the main push and release gate.** `ci.yml` runs checks per job,
  including builds, typechecks, tests, linters, browser proofs, and VN
  infrastructure. The `contracts` job runs full-history gitleaks. The aggregate
  `ci` job is the only required check on `main`. Hosted CI never calls
  `make ci`. A red run is fixed forward at once.
- **Narrow pull request selection.** Only pull requests whose entire change
  matches the [prose allowlist](../design/ci-path-selection.md#selection) may
  omit the named application and infrastructure jobs. Preflight, all contracts,
  and `ci` still run. Unknown paths and path-selection failures select the full
  gate; invalid comparison bases still fail preflight. Main pushes and every
  manual dispatch keep the full gate. Workflow triggers have no path filter.
  `ci` accepts an omitted job only with valid, explicit pull request selection;
  failures, cancellations, missing outputs, and unexplained skips fail it.
- **Exact green commit.** A release commit is pushed alone to `main`. Tagging
  and deployment wait for a green main push run on that exact commit.
- **Owner local landing gate.** Accepted 2026-11-01: the owner may land after a
  passing `make ci-local` on his Mac under the shared lock and the total RAM
  minus 10 GiB watchdog, using the always-on repository admin bypass on
  `main-required-ci`. The separate `main-history` ruleset blocks force pushes
  and deletion of `main` without a bypass. Other contributors need a pull
  request and green CI. Browser, pixel, and VN infrastructure suites run only in
  GitHub CI; failures on the main push are fixed forward. The local gate does
  not authorize a tag or deployment without green CI on the exact main push
  commit. Run `make ci-local` only on commits the owner wrote or has reviewed,
  because it executes the checkout's scripts and npm lifecycle hooks with the
  owner's privileges. Review changes to the gate script, Makefile, workflows, or
  dependency manifests before a local run; send contributor heads through hosted
  CI.
- **Bounded local work.** The primary uses the shared lock, the watchdog, nice
  level 10, and a time limit for local checks. See the
  [resource rules](../../instructions/resources.md). Diagnose a failed or
  out-of-memory command before rerunning it; never repeat it unchanged to seek a
  pass.
- **Reuse evidence.** The manager and reviewer reuse check evidence from the
  exact commit under review and do not duplicate a check for role-specific
  evidence.
- **Browser checks early.** User-visible changes are exercised through scripted
  headless Playwright suites as they land. A check that proves user interface
  behavior with no hosted CI equivalent may run as a narrow, bounded local
  exception under the same resource limits; it cannot replace or weaken the
  release proof.
- **Correctable criteria.** An acceptance criterion that is wrong,
  unsatisfiable, or tests the wrong thing is fixed when found, with the change
  noted.

**Code scanning.** CodeQL uses `security-extended` for Go and
JavaScript/TypeScript on main pushes, every Monday at 03:00 UTC, and manual
dispatch. It is non-blocking and runs outside the required `ci` check; review
alerts in GitHub's Security tab. CI references no stored secrets; GitHub's
per-run `GITHUB_TOKEN` remains allowed.

ESLint enforces the [sanitizer control](0008-ssr-sanitizer-authority.md).
Forbidigo guards trusted HTML conversions. String-built pgx SQL has no blocking
lint; queries come from sqlc static SQL files, and CodeQL runs SQL injection
queries on main pushes and weekly. CodeQL covers insecure cookies. Full-history
gitleaks, npm audit, and govulncheck retain their CI gates. `make scan` runs
full-history gitleaks locally.

`AGENTS.md` and `instructions/` carry the operational detail.

## Context

A tag and deployment must refer to the exact commit that passed every required
hosted check. A later commit cannot inherit an earlier commit's green result.
The repository is public, so a secret in history is exposed on push and cannot
be recalled. A capped local gate gives the owner a faster landing path while
full hosted checks remain the release authority.

## Compatibility, security, and size

This is delivery process only. It changes no product behavior, schema, API,
stored data, migration, or client contract.

Per-commit gitleaks is the boundary before a secret can enter public Git
history. Full-history gitleaks remains required on the exact release commit. CI
references no stored secrets. CodeQL runs outside the required `ci` check.
Bounded local diagnostics limit the effect of hostile or faulty test inputs on
the shared laptop.

## Consequences

- A defect that a second independent test author would have caught can reach the
  review instead. That is the accepted trade.
- Environment-specific defects surface in hosted CI rather than on the laptop.
- Allowlisted prose pull requests use fewer runners. Keeping all contracts
  limits the savings but preserves documentation and secret checks. Omitted
  checks first run on main; a green reduced pull request never authorizes a
  release. Application clients and stored data need no migration.
- An independent acceptance pass returns only through a new ADR.

## History

### Amendment: One-line review exemption (2026-10-10)

The owner exempted one-line changes, including production DNS, from the reviewer
pass and required manager plan or diff evidence instead. The current review rule
above retains this exemption.

- Former ADR 0011 (2026-08-11): risk-tiered review, two phase gates, local
  `make ci` as the gate of record, per-commit gitleaks, phase-batched Semgrep,
  and early browser checks. Its tier table, phase gates, and parallel design
  lane were process choices that 0024 and 0046 replaced.
- Former ADR 0024 (2026-08-12): one author pass, adversarial cases in the owning
  task, one review per phase, correctable criteria.
- Former ADR 0046 (2026-09-19, updated 2026-09-20): GitHub CI as the full gate,
  replacing the local gate of record and the phase-level review. Its rules are
  the core of this record.
- 2026-10-10: The owner exempted one-line changes, including production DNS,
  from the reviewer pass and required manager plan or diff evidence instead. Fix
  confirmation uses the original reviewer unless cross-model routing requires a
  replacement.
- 2026-10-11: The owner moved every review and fix confirmation to one model,
  `gpt-6.1-sol` at `xhigh`, run as a separate job from the author. Reviews no
  longer alternate by author model; `gpt-6-astra` writes designs only.
- 2026-10-11: Corrected the hosted CI entry point to match `ci.yml`: checks run
  per job and aggregate under `ci`, rather than calling local `make ci`.
- 2026-10-31: The owner approved Linux and macOS local tooling with a sampled
  total RAM minus 10 GiB process-tree budget and nice level 10. Hosted CI
  remains the full gate, and the primary alone runs local checks.
- 2026-10-31: The owner limited connected Semgrep to main pushes and main
  dispatches with a main-only environment secret to keep its token out of pull
  request and branch runs. That policy required exact-release Semgrep checks.
- 2026-11-01: The owner chose a narrow prose allowlist for pull requests to
  reduce runner queues, with unknown paths falling back to the full gate. Main
  pushes remain full, and `ci` remains the single required check. The selection
  design and this amendment require independent review before implementation.
- 2026-11-01: The owner removed Semgrep and its token, selected non-blocking
  CodeQL on main pushes and weekly, and required CI to hold no stored secrets.
  ESLint and forbidigo carry the retained HTML controls.
- 2026-11-01: The owner accepted the capped local landing gate and admin-only
  bypass for his Mac, retaining full main push CI and the exact-commit release
  gate. The `main-required-ci` ruleset has an always-on repository admin bypass;
  `main-history` blocks force pushes and deletion without a bypass.
- 2026-11-01: Restricted local gate runs to trusted commits and required review
  of gate and dependency changes before execution because they run with the
  owner's privileges.
