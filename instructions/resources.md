# Resource rules

Rules for anything that runs on the owner's laptop. Read this file before any local command beyond reading files and small formatting checks.

The owner accepted [local tooling simplification](../docs/design/local-tooling-simplification.md) on 2026-11-01 under [ADR 0024](../docs/adr/0024-delivery-gates.md#amendment-tooling-and-display-checks-2026-11-01). Remove laptop RSS/nice enforcement and numeric job ceilings; keep memory hints, timeouts, managed locks, and owned-child cleanup. This is authority for implementation, not permission to run checks. The wrapper, session, dependency, and native browser changes remain pending.

## Execution and transition

- **Primary session only.** Codex sandboxes cannot run local checks or write the Go build cache or npm cache. The primary prepares dependencies and runs approved commands; workers and reviewers read its output and exact-commit CI evidence instead of repeating checks.
- **CI by default.** Builds, typechecks, tests, race tests, full lint suites, image builds, and browser suites run on GitHub runners. Standing local exceptions are the static pre-push check and owner landing gate below. Other execution needs a bounded primary brief naming the command, reason, timeout, and expected evidence. Inspect hosted logs first; do not install dependencies just to duplicate CI.
- **Interim command.** Until the wrapper and caller changes land, use the existing parent-held lock and `python3 scripts/lib/local-check.py <lock-path> 0 bash scripts/run-capped.sh --timeout <seconds> -- <command> [args...]`. The lock path is `$(git rev-parse --path-format=absolute --git-common-dir)/aboutme-local-check.lock`. Current code still enforces sampled process-tree RSS at total RAM minus 10 GiB and nice level 10, and refuses unreadable RAM or a nonpositive limit. Do not bypass it while callers still depend on it.
- **After wrapper integration.** `run-owned.sh` retains timeout, TERM/KILL, child joining, identity, and failure status, with no RSS discovery, sampling, reporting, or nice. Ordinary commands need no mandatory wrapper or lock. Pre-push, ci-local, and dependency preparation retain their managed lock and deadlines; MCP and production-proof locks remain. Lock descriptors do not reach children. Add no locks or memory-warning sampler.
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

The accepted dependency change requires checkout-local `npm ci` and `npm --prefix apps/web ci` under the managed lock. Reject linked dependencies, generated directories, and ancestors before installing; preserve link targets. Pre-push must never install, borrow, copy, link, repair, or delete dependencies after that change lands. Until then, pre-push still borrows the main checkout's dependencies and `.nuxt`; use only its existing supported path and report refusals. Local CI already installs in the tested checkout. The primary runs dependency preparation, `go mod tidy`, and lockfile updates with network access under the applicable transition rules.

## Pre-push check

`make pre-push` (`scripts/pre-push.sh`) is allowed without a separate brief. It is static only: changed-source ESLint, gofmt, golangci-lint in each changed module with its own configuration, human Markdown Prettier/markdownlint, lengths, `bash -n`, and shellcheck. A module outside `go.work` uses `GOWORK=off`. It runs no tests, typechecks, builds, databases, stacks, or browsers.

- Run before every push. Push only on exit 0, or exit 2 after the summary with each refused check named in the report.
- Keep Bash 4+, Python 3, the shared parent-held lock, and the 600-second deadline. Until wrapper integration, `run-capped.sh` also enforces and reports RSS. Exit 1 means failure; exit 2 before the summary means nothing ran and requires resolving the refusal. Exit 2 after the summary names checks refused for missing/stale tools or changed manifests; CI covers them.
- After checkout-local dependency integration, selected-tool drift still refuses. It does not certify the full dependency graph; no install receipt is required. The primary prepares dependencies separately in an inactive checkout.

## Owner local gate

The primary may run `make ci-local` on the owner's Mac as the landing gate, only on owner-written or owner-reviewed commits. It holds the managed lock, installs each npm root in the tested checkout, and runs `scripts/ci.sh` with a 3600-second deadline. Until wrapper integration it also uses the RSS/nice watchdog. It does not run browser suites; Linux-only checks skip on macOS. A pass permits the owner's admin bypass. Full main push CI still gates tags and deploys on that exact commit.

## Native functional proofs

After native runner integration and acceptance, opt-in macOS HTTPS and display proofs use the [native admission and account contract](../docs/design/local-tooling-simplification.md#native-admission-and-account). Require a clean owner-written/reviewed commit, disposable builds/profiles, explicit test-only environments, and strict TLS. Review runner, configuration, spec, workflow, and dependency changes before admission. Contributor heads use hosted CI. These measures limit inputs, not owner-account filesystem access. Keep `ci-local` separate from functional proofs.

Native HTTPS excludes WebKit and the exact whole server-rendering journeys in the design, with partial evidence that never satisfies Linux CI completeness. Native display harness PDFs still run. Profile CA trust, cleanup, and both-platform display assertions require runtime acceptance before use; this authority update authorizes no local run. Production proof authorization, custody, container limits, and Linux render supervision remain unchanged.
