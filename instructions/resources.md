# Resource rules

Rules for anything that runs on the owner's laptop. Read this file before any local command beyond reading files and small formatting checks.

The owner accepted [local tooling simplification](../docs/design/local-tooling-simplification.md) on 2026-11-01 under [ADR 0024](../docs/adr/0024-delivery-gates.md#amendment-tooling-and-display-checks-2026-11-01). Laptop checks keep memory hints, timeouts, managed locks, and owned-child cleanup, without RSS or nice enforcement or numeric job ceilings. The portable session and native browser changes remain pending. This is not permission to run extra checks.

## Execution and transition

- **Who runs checks.** The primary, its subagents, and Codex `workspace-write` workers (implementer, troubleshooter, qa, designer) run the brief's bounded commands under the managed lock. Those Codex roles have network access and write only the worktree, fixed per-user Go and golangci-lint caches, a new private `$TMPDIR` per launch (not `/tmp`), and the lock directory, never `.git` or the pnpm store or cache; the dispatching session installs a worktree's dependencies before it dispatches a worker. Read-only reviewers and the explorer run no checks; they read the author's output and exact-commit CI evidence. Do not repeat a check just to confirm a report.
- **CI by default.** Builds, typechecks, tests, race tests, full lint suites, image builds, and browser suites run on GitHub runners. Standing local exceptions are the static pre-push check and the owner local gate below. Other execution needs a bounded primary brief naming the command, reason, timeout, and expected evidence. Inspect hosted logs first; do not install dependencies just to duplicate CI.
- **Bounded checks.** For a check that the brief authorizes under the lock, run `python3 scripts/lib/local-check.py <lock-path> 0 bash scripts/run-owned.sh --timeout <seconds> -- <command> [args...]`. The lock is the main checkout's `.dev/locks/local-check.lock`, outside `.git`, shared by every worktree and Codex sandbox: `$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")/.dev/locks/local-check.lock`. Keep the pre-move `.git/aboutme-local-check.lock` compatibility path until no branch older than the move remains. `make pre-push` and `make ci-local` take the lock themselves; run them directly, not inside another wrapper.
- **Process ownership.** `run-owned.sh` keeps timeouts, child identity, TERM/KILL, child joining, and command exit status. `run-capped.sh` is a compatibility shim. Ordinary commands need no mandatory wrapper or lock. Pre-push, ci-local, and dependency preparation retain their managed lock and deadlines; MCP and production-proof locks remain. Lock descriptors do not reach children. Add no locks or memory-warning sampler.
- **Memory hints remain.** Keep `GOGC=50` in `scripts/ci.sh` and pre-push heap/concurrency hints. Without RSS enforcement, runaway work can exhaust RAM, swap, trigger OS kills, and lose unsaved work; the owner accepted that risk. Container and production limits remain independent and unchanged.
- **Coordinate work.** Use separate worktrees for concurrent writers and coordinate shared fixture writes. Managed local checks remain serialized. Check active project processes before execution; never stop unrelated applications, repositories, or the shared DB. Diagnose a failed or OOM command before rerunning; never retry unchanged or move an unbounded allocation into CI.
- **One database container.** `aboutme-test-db`, started idempotently by `make test-db-up` with a 512 MB cap, holds `aboutme` for tests and `aboutme_dev` for native work. Workers never stop it. Only the primary runs `make test-db-down`, after every live-DB worker is idle. Reconfiguration requires an announced idle window and verification of both databases and the host port.
- **One development stack.** Start a stack only for a named interaction check and stop it when the proof ends. Native HTTP uses `make dev-native` plus `-down`, `-status`, and `-logs`, serves `http://localhost:20080`, and stores logs/PIDs in ignored `.dev/`. Announce a restart of a stack another session uses. `make dev` is only an HTTP image/self-hosting smoke and refuses while the shared DB runs.
- **Active checkout safety.** Never run Nuxt tests, installs, typechecks, builds, or other tooling in a checkout used by a browser proof or native stack; these mutate `.nuxt` mid-proof. Stop old stacks with old helpers before the session change, then reject old PID state. Preserve DB, keys, media, and generated files; missing/ambiguous ownership requires manual recovery, never name-based kills.

Development ports are localhost `20000`–`21000`:

|Port|Service|
|-|-|
|20432|Shared PostgreSQL container|
|20080|Native Caddy origin|
|20081|Native Go server|
|20082|Private print redemption|
|20030|Nuxt development server|
|20090–21000|Test harnesses and explicitly pinned stub servers|

Test DSN: `postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable`. Native DSN: `postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme_dev?sslmode=disable`. Rootless port 443 needs `net.ipv4.ip_unprivileged_port_start <= 443`; only a host administrator changes that. Agents and scripts never use `sudo`.

## Dependencies

[pnpm workspace](../docs/design/pnpm-workspace.md) owns installs. CI and image builds install the `.tool-versions` pnpm with `npm install --global --ignore-scripts pnpm@<pin>`. A developer machine may run pnpm through a corepack shim when `pnpm --version` in the repository prints the pin; `scripts/test/pnpm-policy-test.sh` then skips the no-download refusal case, which needs the npm-installed pnpm. Under the managed lock, install a checkout or worktree with `pnpm install --frozen-lockfile --offline` at the repository root and in `deploy/dev-https-browser`; when the offline install misses the store, run `pnpm install --frozen-lockfile`. Only a deliberate dependency change resolves without `--frozen-lockfile`. `pnpm run` and `pnpm exec` refuse stale dependencies (`ERR_PNPM_VERIFY_DEPS_BEFORE_RUN`) and never install; a pnpm whose version differs from `packageManager` refuses with `ERR_PNPM_BAD_PM_VERSION` and never downloads another. The pre-push and ci-local guard accepts only symlinks into the same project's `node_modules/.pnpm`, `.bin` files inside its `node_modules`, and declared `workspace:` members. Reject linked dependencies, generated directories, and ancestors before installing; preserve link targets. Pre-push never installs, borrows, copies, links, repairs, or deletes dependencies. Local CI installs in the tested checkout. The primary runs dependency preparation, `go mod tidy`, and lockfile updates with network access under the applicable resource rules.

## Pre-push check

`make pre-push` (`scripts/pre-push.sh`) is allowed without a separate brief. It is static only: changed-source ESLint, gofmt, golangci-lint in each changed directory with its own configuration, human Markdown Prettier/markdownlint, lengths, `bash -n`, and shellcheck. It runs no tests, typechecks, builds, databases, stacks, or browsers.

- Run before every push. Push only on exit 0, or exit 2 after the summary with each refused check named in the report.
- Keep Bash 4+, Python 3, the shared parent-held lock, and the 600-second deadline. Exit 1 means failure; exit 2 before the summary means nothing ran and requires resolving the refusal. Exit 2 after the summary names checks refused for missing/stale tools or changed manifests; CI covers them.
- The checkout-local guard refuses selected-tool drift. It does not certify the full dependency graph; no install receipt is required. The primary prepares dependencies separately in an inactive checkout.

## Owner local gate

The owner's work lands on the [local gate](verification.md#local-gate) on the owner's Ubuntu host (owner, 2026-11-10). That host's session may run the gate's builds, unit and integration tests, web E2E shards, and HTTPS proof groups under the managed lock and timeouts, one heavy job at a time, smallest first, stopping at the first out-of-memory kill. It never runs the full `make ci-local`. Gate only owner-written or owner-reviewed commits. Full main push CI still gates tags and deploys on that exact commit.

The primary may still run `make ci-local` on the owner's Mac. It holds the managed lock, installs both pnpm projects afresh in the tested checkout, and runs `scripts/ci.sh` through `run-owned.sh` with a 3600-second deadline. It does not run browser suites; Linux-only checks skip on macOS.

## Native functional proofs

After native runner integration and acceptance, opt-in macOS HTTPS and display proofs use the [native admission and account contract](../docs/design/local-tooling-simplification.md#native-admission-and-account). Require a clean owner-written/reviewed commit, disposable builds/profiles, explicit test-only environments, and strict TLS. Review runner, configuration, spec, workflow, and dependency changes before admission. Contributor heads use hosted CI. These measures limit inputs, not owner-account filesystem access. Keep `ci-local` separate from functional proofs.

Native HTTPS excludes WebKit and the exact whole server-rendering journeys in the design, with partial evidence that never satisfies Linux CI completeness. Native display harness PDFs still run. Profile CA trust, cleanup, and both-platform display assertions require runtime acceptance before use; this authority update authorizes no local run. Production proof authorization, custody, container limits, and Linux render supervision remain unchanged.
