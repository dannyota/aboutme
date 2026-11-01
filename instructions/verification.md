# Common engineering rules and verification

Common code rules, CI lifecycle, exact-commit evidence, and failure handling. Implementers, QA, reviewers, and the primary read this file.

Follow the [naming conventions](../docs/standards/engineering.md#naming) for workflows, steps, matrices, and tests.

- Pin the latest stable dependency at scaffold time; upgrades need review. Exact tool versions live in `.tool-versions`; `make tools-check` rejects drift. When an installed tool is newer than the pin, update `.tool-versions` and every mirror, then run the affected checks.
- Never hand-edit generated files; change the source and regenerate. Migration rules live in [Go and database rules](go.md).
- A contract change updates schema or OpenAPI sources, generated clients, tests, examples, design docs, and traceability in one change.
- Do not weaken security controls: least privilege, strict input bounds, versioned sanitizing, CSRF and Origin checks, `__Host-` cookies, route-specific rate limits, CSP, secret-free logs.

Required change-area gates and browser automation rules live in [testing](testing.md). Read [Go](go.md), [web](web.md), or [deployment](deploy.md) rules for the change area.

## GitHub CI

### Pin maintenance

Dependabot checks GitHub Actions, all four Go modules, and all four npm lockfile roots weekly. Actions share one group; Go and npm minor and patch updates share a group per ecosystem and directory set, while major updates arrive separately. Each entry allows two open version-update PRs. Mirrored Caddy and Playwright pins, the HTTPS harness's axe pin, and the web TypeScript pin stay manual through documented ignores. Toolchains and their mirrors, container image digests, and other custom pins also stay manual. External actions and reusable workflows keep full commit SHA pins with version comments. `scripts/test/workflow-contract.test.mjs`, called by `scripts/test/toolchain-contract-test.sh` in `make operational-test-fast`, checks those pins, the Dependabot configuration, and every hosted tool-version mirror against `.tool-versions`.

`SEMGREP_APP_TOKEN` lives only in the `semgrep` GitHub environment, whose deployment branch policy allows only `main`. Pull request and Dependabot runs never receive it.

For rotation, pipe a fresh token from its source into `gh secret set SEMGREP_APP_TOKEN --env semgrep`, never print the value or store it elsewhere in GitHub, then revoke the old token in Semgrep. The cutover is complete only after the fresh token is in that environment, the old token is revoked, and outstanding untrusted runs are cancelled.

The repository owner reviews pins outside that automation on the first working day of each month: `.tool-versions` and its mirrors, runner labels, Python and OpenTofu versions, container image digests, and Playwright versions and browser-image digests. Record the review date, checked pins, and any deferred upgrade with its reason in a tracking issue. Each upgrade needs green CI and every affected workflow before landing, including [browser baseline checks](web.md) and [image checks](releases.md#releases). Preserve cache trust boundaries: only main saves Go, tool, and Semgrep pip caches; branch Playwright caches never reach main.

### Runs and evidence

The `preflight` job in `ci.yml` validates comparison bases and runs the migration and released-schema guards before any other checks start. Both guards validate refs before allowing pre-v1 changes. A trusted comparison base containing root `.v1-baseline` freezes the marker and existing migration SQL and released schema snapshots. Guards compare the base and candidate trees directly, including non-ancestor histories; deleting the candidate marker cannot disable the freeze. The local schema guard checks both index and worktree. The guards do not enforce manifest entry history. Every large job, including VN infrastructure, needs `preflight`. The `ci` job is the complete required check for `main`. It needs every other job, including the reusable `vn-infra.yml` workflow. Triggers have no path filter; only pull requests selected as `prose` may omit the jobs named in the [selection design](../docs/design/ci-path-selection.md). VN infrastructure checks run Tofu formatting and validation, host shell checks, deployment safety tests, and pinned CrowdSec integration without secrets or write permissions. `vn-infra.yml` also supports manual dispatch; it has no independent push or pull-request trigger and no concurrency group that can cancel a caller. Every needed job must succeed unless the aggregate validates an explicit skip permission. A pull request with valid `prose` selection may skip only the eight jobs named in the selection design; preflight and the entire contracts job still run. The complete job-key set, successful preflight, and known selection output are required even when all jobs succeed. `prose` on a push or dispatch fails `ci`. The two Semgrep jobs may also skip when the run is not a `push` or `workflow_dispatch` on `refs/heads/main`; both must succeed on main pushes and main dispatches. Failure, cancellation, a missing job or result or output, an unknown value, or any unexplained skip fails `ci`. Successful HTTPS proofs still require aggregate shard evidence and coverage checks. Every pull request retains full-history gitleaks and npm audit for every npm lockfile. Full runs also include govulncheck for every Go module. Pull-request runs execute the candidate's own workflow files, so `ci` cannot stop a pull request that edits `.github/workflows/` or the workflow contract tests to pass without checking; review those edits as gate changes before the signed local merge.

GitHub CI runs every build and test. `main` and other branches run it differently:

||`main`|Other branches|
|-|-|-|
|Trigger|Every push runs `ci.yml`|A pull request against `main` runs `ci.yml` on the branch merged into `main`, again on every push; without one, dispatch `gh workflow run ci.yml --ref <branch> -f base_sha=<sha>`|
|Selection|Full, with the existing Semgrep event rule|Pull requests use the prose allowlist over the combined change, including landing trains; other paths and selector errors select full. Every dispatch selects full|
|Release gate|A green push run on the exact commit is required to tag and deploy|Never counts as a release gate|
|Caches|Saves Go, tool, and Semgrep pip caches|Restores Go caches but never saves them, so the first run can be slower; Playwright caches save per branch and never reach `main`; Semgrep pip caches save only on `main`|
|Canceling|Each push has its own group and retains its result|A newer pull-request run cancels the older PR run; a newer dispatch cancels the older dispatch on that branch. Dispatch and push groups are separate|

A dispatch run tests a branch only. It never satisfies the main ruleset's required `ci` check, so it cannot serve as a landing gate. A pull request needs no dispatch: its run tests the branch merged into `main` as `main` stood when the run started. A later push to `main` does not rerun it, so if `main` moved since the last run, merge `origin/main` into the branch and push before landing; CI must cover the new head, with independent Codex review under [workflow](workflow.md#reviews). Before dispatching on a branch without a pull request:

1. Merge `origin/main` into the branch (never rewrite pushed commits) so branch testing includes current main.
2. Fetch `origin/main` and pass `base_sha=$(git merge-base origin/main HEAD)`, a full 40-character SHA. Preflight fetches `main` independently and requires the supplied SHA to equal the unique merge base of that fetched main and the candidate. Malformed, non-commit, unrelated, stale, or candidate-selected bypass bases fail preflight before the large jobs start. If main advances and changes the merge base before dispatch runs, fetch and merge main again before dispatching.
3. Dispatch once and wait; do not dispatch again on the same branch while a run is in progress.
4. Read results for the exact head SHA (`gh run list --branch <branch>` shows `headSha`); a green run on an older commit proves nothing about the head.

CI concurrency groups include the event name. Pull requests and dispatches cancel superseded runs for their own ref; each main push uses its run ID and never cancels another push. Preflight and the aggregate gate have 5-minute timeouts. Contracts, server, server integration, resume API, web, and each Semgrep job have 20-minute timeouts. Web E2E and HTTPS proofs have 40-minute timeouts. The reusable VN workflow bounds its own jobs.

Both web-e2e shards upload `browser-diagnostics-<shard>` even when comparison fails. The allowlist covers committed renderer, print, and template page baseline names with expected, actual, and difference PNGs. The collector decodes bounded, non-interlaced 8-bit RGB/RGBA PNGs and re-encodes pixels without metadata or appended bytes. Diagnostics include source commit, source manifest and tar hashes, browser-image digest, and redacted failure summaries with spec file, line, and fixed status only. Test titles, error messages, console output, raw responses, traces, and HTML reports stay out of diagnostics. The existing preview-gap report and HTTPS proof-evidence redaction remain unchanged. `Baselines` uploads the same diagnostics on failure and includes `source-metadata.txt` with successful `candidate-baselines`; failed generation never uploads candidates. Comparison still forbids snapshot updates and uses exact pixel comparison.

After merging to `main`, the `main` push run is the gate: wait for it before tagging.

Hosted Semgrep scans run only on main pushes and main dispatches. They cover the full tree and may report advisories against unchanged pins, so a green pull request can still produce a red main run.

Push preflight uses `github.event.before` as its comparison base. After an authorized history rewrite, that commit must remain fetchable. If it is unavailable, stop and resolve the comparison-base failure; do not substitute a candidate-selected base.

## When a check fails

Find the cause before changing anything. Never rerun a failed job hoping for a pass, add retries or longer timeouts, or loosen a check without evidence that it is the cause.

If the log proves a third-party service failed before the check ran, record the evidence and rerun only failed jobs on the same commit with `gh run rerun <run> --failed`. This exception does not cover findings, failing tests, or memory failures.

1. Read the failing log and name the exact failing step. If the log hides the cause (for example the browser proofs withhold console output), add a diagnostic that records it (to a CI artifact, never secrets) on a debug branch that is never merged, and run that.
2. Bisect when a failure starts at a known point: find the first bad commit by CI runs on earlier commits.
3. Read the code on both sides and research the tool's docs and issues; cite sources in the report.
4. Fix the root cause, add a test that fails without the fix, and prove it with green CI on the exact head (twice for intermittent failures).
5. If only a local session can show the cause, stop and ask the manager with the exact commands and why CI cannot show it.
