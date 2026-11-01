# Resource rules

Rules for anything that runs on the owner's laptop. Read this file before any local command beyond reading files and small formatting checks.

Local tools support Linux and macOS. Each local command's complete process tree has a sampled RSS limit equal to the machine's total RAM minus 10 GiB. `scripts/run-capped.sh` enforces that limit and runs the command at nice level 10. These rules override older plans and briefs:

- **One database container total.** `aboutme-test-db`, started idempotently by `make test-db-up` with a 512 MB cap. It holds `aboutme` for tests and `aboutme_dev` for native work. Workers never stop it; only the manager runs `make test-db-down`, after every live-DB worker is idle.
- **Local interaction checks use `make dev-native`** (plus `-down`, `-status`, `-logs`) only under a bounded manager brief. Native Go, Nuxt, and Caddy use `aboutme_dev` and serve `http://localhost:20080`. Logs and PIDs live under ignored `.dev/`. Say so before restarting a stack another session uses.
- **`make dev` is only an HTTP image and self-hosting smoke.** It fails while `aboutme-test-db` runs, by design.
- **CI by default.** Run builds, Nuxt typechecks, all tests, race tests, full lint suites, image builds, and browser suites on GitHub runners. The one standing local exception is the [pre-push check](#pre-push-check). Never run local `make ci`, including for CI debugging. Inspect hosted logs first and fix forward. Do not install dependencies just to duplicate CI.
- **Local exceptions are narrow and bounded.** The manager must name the exact command, why CI cannot answer it, its timeout, and the expected evidence. Use one local check process across all worktrees. Hold the shared advisory lock at `$(git rev-parse --path-format=absolute --git-common-dir)/aboutme-local-check.lock` in a parent with `python3 scripts/lib/local-check.py <lock-path> 0 bash scripts/run-capped.sh --timeout <seconds> -- <command> [args...]`. The lock descriptor never reaches children. Never fall back to an uncapped command. `GOMEMLIMIT`, `GOGC`, worker counts, and `GOMAXPROCS` are extra hints, not substitutes for the watchdog.
- The watchdog reads total RAM from Linux `/proc/meminfo` or macOS `sysctl -n hw.memsize`. It refuses when total RAM cannot be read or the reserve leaves no positive limit. Every 0.5 seconds it sums RSS across the command's process group and descendants from `ps`; exceeding the limit or losing sampling terminates the tree. Sampling can miss bursts shorter than 0.5 seconds, and a process that leaves both group and tree escapes. Container memory inside a Podman VM is outside the host process tree; keep container limits in the tools that own them.
- Check active project processes before a local exception. Do not start alongside another local check. Do not stop unrelated applications, other repositories, or the shared DB.
- After an OOM or cap failure, stop the task and inspect the cause without executing it. Do not rerun the same command, raise the cap, or move an unbounded allocation into CI without fixing it first.
- The primary verifies diffs and exact-commit CI evidence instead of rerunning an author's checks. Reviewers read code and existing evidence by default; they do not launch tests or delegate test execution.
- Never run Nuxt tooling in a checkout with an active browser proof. Never run Nuxt tests, installs, typechecks, or builds in a checkout used by a browser proof or native stack. Those commands can change `.nuxt` mid-proof.
- Keep only one aboutme development stack active. Start it only for a named interaction check, subject to the same memory budget, and stop it when the proof ends. Do not start a stack during ordinary editing or review.
- Reconfiguring the database container is scheduled work: announce it, wait for an idle window, recreate, then verify both databases and a host-port test.

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

Codex sandboxes cannot run local checks or write the Go build cache or npm cache. The primary prefetches dependencies and runs `go mod tidy` and npm lockfile updates with network access under this file's lock and cap rules; workers review the results. Local checks run only in the primary session; workers and reviewers read its output.

## Pre-push check

`make pre-push` (`scripts/pre-push.sh`) is the one local check allowed without a manager brief, besides formatting. It is static only: ESLint on changed web sources, gofmt on every changed Go file, golangci-lint on changed packages in each Go module that carries its own `.golangci.yml` (found from `go.mod` files, not a fixed list), Prettier and markdownlint on changed Markdown people read, `scripts/check-lengths.sh`, and `bash -n` plus shellcheck on changed shell scripts. A module outside `go.work`'s `use` list lints with `GOWORK=off`. It never runs tests, typechecks, builds, databases, stacks, or browsers; all tests run in CI.
- The primary runs it before every push and pushes only on exit 0, or on exit 2 after the summary with each refused check named in its report.
- It holds the shared advisory lock in a Python parent and runs through `scripts/run-capped.sh --timeout 600` with peak RSS reporting. Bash 4+ and Python 3 are required; Bash 3.2 refuses with a setup message. Exit 1 means a check failed or the watchdog stopped it. Exit 2 before the summary (lock busy, missing tools, unreadable total RAM, or no positive memory limit) means nothing ran: resolve the refusal and run it again. Exit 2 after the summary means the checks it lists as `REFUSED` could not run locally (a changed manifest or lockfile, or stale tools), so CI alone covers them.
- A worktree borrows the main checkout's `node_modules` and `apps/web/.nuxt` and removes them on exit. If the main checkout's lint tools are missing or differ from the lockfile, the check refuses; the manager refreshes the main checkout with `npm ci` under a brief.
