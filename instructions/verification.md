# Common engineering rules and verification

Common code rules, CI lifecycle, exact-commit evidence, and failure handling. Implementers, QA, reviewers, and the primary read this file.

- Pin the latest stable dependency at scaffold time; upgrades need review. Exact tool versions live in `.tool-versions`; `make tools-check` rejects drift. When an installed tool is newer than the pin, update `.tool-versions` and every mirror, then run the affected checks.
- Never hand-edit generated files; change the source and regenerate. Migration rules live in [Go and database rules](go.md).
- A contract change updates schema or OpenAPI sources, generated clients, tests, examples, design docs, and traceability in one change.
- Do not weaken security controls: least privilege, strict input bounds, versioned sanitizing, CSRF and Origin checks, `__Host-` cookies, route-specific rate limits, CSP, secret-free logs.

Required change-area gates and browser automation rules live in [testing](testing.md). Read [Go](go.md), [web](web.md), or [deployment](deploy.md) rules for the change area.

## GitHub CI

### Pin maintenance

Dependabot opens one grouped GitHub Actions update PR each week. External actions and reusable workflows keep full commit SHA pins with version comments. `scripts/test/workflow-contract.test.mjs`, called by `scripts/test/toolchain-contract-test.sh` in `make operational-test-fast`, checks those pins, the Dependabot configuration, and every hosted tool-version mirror against `.tool-versions`.

Dependabot pull requests receive Dependabot secrets, not Actions secrets, so the repository keeps a Dependabot secret named `SEMGREP_APP_TOKEN` for both Semgrep jobs, copied from the same token as the Actions secret. If a Dependabot PR fails both Semgrep jobs on authentication, refresh that copy by piping the value from its source into `gh secret set --app dependabot`; agents never print or read secret values. Keep the jobs' skip conditions unchanged.

The repository owner reviews pins outside that automation on the first working day of each month: `.tool-versions` and its mirrors, runner labels, Python and OpenTofu versions, container image digests, and Playwright versions and browser-image digests. Record the review date, checked pins, and any deferred upgrade with its reason in a tracking issue. Each upgrade needs green CI and every affected workflow before landing, including Baselines for browser changes and manual release build checks for image changes. Preserve cache trust boundaries: only main saves Go and tool caches; branch Playwright and pip caches never reach main.

### Runs and evidence

The `preflight` job in `ci.yml` validates comparison bases and runs the migration and released-schema guards before any other checks start. Both guards compare the base and candidate trees directly; the released-schema guard preserves its byte-exact, one-way v1 exception. Every large job, including VN infrastructure, needs `preflight`. The `ci` job is the complete required check for `main`. It needs every other job, including the reusable `vn-infra.yml` workflow on every trigger without a path filter. VN infrastructure checks run Tofu formatting and validation, host shell checks, deployment safety tests, and pinned CrowdSec integration without secrets or write permissions. `vn-infra.yml` also supports manual dispatch; it has no independent push or pull-request trigger and no concurrency group that can cancel a caller. Every needed job must succeed. Only `semgrep-code` and `semgrep-supply-chain` may be skipped, and only for a pull request whose head repository differs from this repository. Failure, cancellation, a missing result, or any other skip fails `ci`. Pull-request runs execute the candidate's own workflow files, so `ci` cannot stop a pull request that edits `.github/workflows/` or the workflow contract tests to pass without checking; review those edits as gate changes before the signed local merge.

GitHub CI runs every build and test. `main` and other branches run it differently:

||`main`|Other branches|
|-|-|-|
|Trigger|Every push runs `ci.yml`|A pull request against `main` runs `ci.yml` on the branch merged into `main`, again on every push; without one, dispatch `gh workflow run ci.yml --ref <branch> -f base_sha=<sha>`|
|Release gate|A green push run on the exact commit is required to tag and deploy|Never counts as a release gate|
|Caches|Saves Go and tool caches|Restores Go caches but never saves them, so the first run can be slower; the Playwright and pip caches save per branch and never reach `main`|
|Canceling|Each push has its own group and retains its result|A newer pull-request run cancels the older PR run; a newer dispatch cancels the older dispatch on that branch. Dispatch and push groups are separate|

A dispatch run tests a branch only. It never satisfies the main ruleset's required `ci` check, so it cannot serve as a landing gate. A pull request needs no dispatch: its run tests the branch merged into `main` as `main` stood when the run started. A later push to `main` does not rerun it, so if `main` moved since the last run, merge `origin/main` into the branch and push before landing; CI and the Copilot review then cover the combination that lands. Before dispatching on a branch without a pull request:

1. Merge `origin/main` into the branch (never rewrite pushed commits) so branch testing includes current main.
2. Fetch `origin/main` and pass `base_sha=$(git merge-base origin/main HEAD)`, a full 40-character SHA. Preflight fetches `main` independently and requires the supplied SHA to equal the unique merge base of that fetched main and the candidate. Malformed, non-commit, unrelated, stale, or candidate-selected bypass bases fail preflight before the large jobs start. If main advances and changes the merge base before dispatch runs, fetch and merge main again before dispatching.
3. Dispatch once and wait; do not dispatch again on the same branch while a run is in progress.
4. Read results for the exact head SHA (`gh run list --branch <branch>` shows `headSha`); a green run on an older commit proves nothing about the head.

CI concurrency groups include the event name. Pull requests and dispatches cancel superseded runs for their own ref; each main push uses its run ID and never cancels another push. Preflight and the aggregate gate have 5-minute timeouts. Contracts, server, server integration, resume API, web, and each Semgrep job have 20-minute timeouts. Web E2E and HTTPS proofs have 40-minute timeouts. The reusable VN workflow bounds its own jobs.

After merging to `main`, the `main` push run is the gate: wait for it before tagging.

## When a check fails

Find the cause before changing anything. Never rerun a failed job hoping for a pass, add retries or longer timeouts, or loosen a check without evidence that it is the cause.

1. Read the failing log and name the exact failing step. If the log hides the cause (for example the browser proofs withhold console output), add a diagnostic that records it (to a CI artifact, never secrets) on a debug branch that is never merged, and run that.
2. Bisect when a failure starts at a known point: find the first bad commit by CI runs on earlier commits.
3. Read the code on both sides and research the tool's docs and issues; cite sources in the report.
4. Fix the root cause, add a test that fails without the fix, and prove it with green CI on the exact head (twice for intermittent failures).
5. If only a local session can show the cause, stop and ask the manager with the exact commands and why CI cannot show it.
