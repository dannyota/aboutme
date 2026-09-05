# AGENTS.md

## Scope and authority

This file is the repository's single source of coding-agent instructions.
`CLAUDE.md` is a tracked compatibility bootstrap and adds no rules. Both files
are tracked so instruction changes can be reviewed with the repository.

Direct user instructions and platform safety rules come first. Then:

| Question                          | Authority                                                       |
| --------------------------------- | --------------------------------------------------------------- |
| Intended product and architecture | `docs/design/` (Approved v4)                                    |
| Rationale for one decision        | `docs/adr/`                                                     |
| Current implemented behavior      | Code, deployment configuration, and `docs/api/openapi.yaml`     |
| Current architecture narrative    | `docs/architecture.md` and `docs/runbooks/`                     |
| What to build next, and its gate  | `docs/plans/implementation-plan.md`, then the active phase plan |
| Acceptance-criterion ownership    | `docs/plans/traceability/`                                      |

Design wins over a plan. An accepted ADR wins over design text it contradicts;
fix the text. Disagreement between code, deployment config, and OpenAPI is a
defect — repair them together rather than picking one silently.

`aboutme` is a public AGPL-3.0 resume builder and hosted display service: Go
API, Nuxt/Vue web app, PostgreSQL, deferred Flutter app. Resumes, not users,
have public URLs. **The repository and its CI logs are public.** Never commit
secrets, personal data, credentials, or internal notes.

## How we work

Agile and local-first. Build a working slice, review it, improve it. Optimize
for fast correct delivery, not token cost.

- **The phase is the integration and push unit.** Workers run the narrowest
  affected checks. The integration owner makes reviewed local commits as tasks
  become coherent, then runs full `make ci`, connected `make scan`, and the
  phase exit checklist once at the candidate commit before pushing.
- **Parallel by default, up to 20 workers, at most 3 build-heavy.** Builds,
  tests, linters, Semgrep, and browsers are heavy; queue the rest.
- **Every feature is built and verified locally first.** Phase 9 researches AWS
  Singapore costs; Phase 10 deploys UAT at `uat.aboutme.vn` with Cloudflare DNS
  under the owner's authorization. Local checks and the cost decision precede
  activation. Production is Phase 11 and needs separate approval. Use OpenTofu
  and prefer managed AWS services; see
  [ADR 0031](docs/adr/0031-aws-cost-research-and-hosted-uat.md).
- Match review and checks to risk. Do not invent extra gates.

## Delivery gates

[ADR 0024](docs/adr/0024-single-pass-delivery-gates.md) governs. Two passes per
change, not four:

1. **Task: one author.** Write the failing test first, implement the smallest
   correct change, run the narrowest affected checks. Adversarial cases
   (write-safety, CAS races, bounds matrices, hostile input, authz/CSRF) are
   listed in the task file and are the author's job. No separate blind test
   author, no per-task reviewer.
2. **Phase: one fresh review.** Before push, a worker that authored none of the
   phase reads the integrated diff for defects, design fit, interface stability,
   and traceability. For auth, sessions, CSRF, sanitizing, concurrency, CAS,
   idempotency, media privacy, or publish revocation, the reviewer confirms
   those invariants by name. Findings go back to an author; the same reviewer
   confirms the fix.

Phase exit is `docs/plans/phase-<id>/exit-criteria.md` plus `make ci` and
`make scan` at one unchanged candidate commit, run by the integration owner. A
failing item is fixed and rerun. A wrong or unsatisfiable criterion is corrected
when found, in the same phase, with the change noted. There is no frozen
acceptance catalog and no separate acceptance worker.

Model assignment depends on the coordinator:

| Work                                                                 | Claude Code | Codex                                      |
| -------------------------------------------------------------------- | ----------- | ------------------------------------------ |
| Complex implementation, debugging, design, concurrency, and security | Opus        | `gpt-5.6-sol` subagent                     |
| Plan, defect, code, and phase review                                 | Sonnet      | `gpt-5.6-sol` subagent                     |
| Bounded implementation from a complete contract                      | Haiku       | `gpt-5.6-terra` or `gpt-5.6-luna` subagent |
| Search, exploration, summaries, and narrow checks                    | Haiku       | `gpt-5.6-luna` subagent                    |

Use multiple Codex workers for independent tasks with disjoint ownership. Prefer
Sol for complex jobs and use Sol for every review. Terra and Luna handle bounded
implementation and supporting work; they never review. Only Sol, Terra, and Luna
may be used as Codex subagents. The integration owner keeps planning,
coordination, verification, Git operations, and the final answer. Parallel
workers must still obey the build-heavy and RAM limits below.

Claude Code uses Sonnet and Haiku: Haiku implements complete contracts, and
Sonnet reviews the result. When unsure, use the coordinator's high-judgment
model.

## Resource rules

This laptop has 30 GB RAM and has lost a session to an out-of-memory kill. These
are hard:

- **One database container total.** `aboutme-test-db`, started idempotently by
  `make test-db-up` with a 512 MB cap. It holds `aboutme` for tests and
  `aboutme_dev` for native work. Workers never stop it; only the integration
  owner runs `make test-db-down`, after every live-DB worker is idle.
- **Daily development is `make dev-native`** (plus `-down`, `-status`, `-logs`).
  Native Go, Nuxt, and Caddy use `aboutme_dev` and serve
  `http://localhost:20080`. Logs and PIDs live under ignored `.dev/`.
- **`make dev` is only an HTTP image/network smoke and self-hosting check.** It
  fails while `aboutme-test-db` runs, by design. Never use it for daily work.
- Run full `make ci` alone, never beside a heavy worker wave. Large consumers:
  Nuxt build/typecheck, `golangci-lint` (`GOGC=50`), big `go test -race`
  packages, Semgrep.
- Reconfiguring the database container is scheduled work: announce it, wait for
  an idle window, recreate, then verify container state, both databases, and a
  real host-port DB test.

Ruled development ports are localhost `20000`–`21000`:

| Port        | Service                                           |
| ----------- | ------------------------------------------------- |
| 20432       | Shared PostgreSQL container                       |
| 20080       | Native Caddy origin                               |
| 20081       | Native Go server                                  |
| 20082       | Private print redemption                          |
| 20030       | Nuxt development server                           |
| 20090–21000 | Test harnesses and explicitly pinned stub servers |

Test DSN:
`postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable`. Native
DSN:
`postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme_dev?sslmode=disable`.
Kernel-assigned test ports and runner service-container ports are exempt.

Rootless port 443 needs `net.ipv4.ip_unprivileged_port_start <= 443`. Only a
host administrator may change that sysctl; agents and scripts never use `sudo`.

## Start a task

1. Run `git status --short --branch`. Existing changes belong to someone else
   unless ownership is explicit.
2. Read the task file and its named authorities. Establish exact paths,
   ownership, acceptance rows, and the verification command.
3. Inspect existing implementation and tests. Preserve unrelated changes and
   established patterns.
4. Observe a test fail for the expected reason, then make the smallest correct
   implementation.

Stop at a document conflict, overlapping ownership, or missing decision. Report
the concrete boundary to the integration owner; do not invent a contract.

## Parallel dispatch and Git safety

- Split multi-file work into disjoint path sets and dispatch them together. Each
  worker owns one exclusive set in the shared tree and index.
- Workers never touch Git: no add, commit, stash, reset, checkout, branch
  switch, or worktree creation unless the brief authorizes a worktree. A
  worktree brief must require the worker to verify its base commit.
- Every brief names the phase/task, authorities, acceptance IDs, owned paths,
  definition of done, exact check, and report format.
- Root `Makefile`, manifests, workflows, lockfiles, migrations, schema heads,
  and generated snapshots belong to the integration owner. Workers report the
  required shared-file edit. Serialize migrations and schema heads.
- The integration owner rereads worker output and reruns its key check before
  staging. Do not dispatch work that takes one or two tool calls.
- Launch long-running workers in the background. A quiet worker is not failed.
- `PROGRESS.md` is the integration owner's ignored session checklist. Keep
  local-only paths in `.git/info/exclude`, never the committed `.gitignore`.

Before a commit, the integration owner inspects `git diff -- <owned-paths>` and
`git diff --cached --name-only`, stages with `git add -- <paths>`, and commits
with `git commit -m "..." -- <paths>`.

Never use `git add .`, `git add -A`, `git commit -a`, or force-add. Never stage
`.env`. Never reset, stash, amend, rebase, clean, or restore to repair index
contamination — stop for the integration owner. Use Conventional Commits;
messages never mention agents, AI, or automated assistance. Run `gitleaks` per
commit; the repository is public.

## Engineering rules

- Pin the latest stable dependency at scaffold time. Dependency upgrades need
  explicit review. Exact tool versions live in `.tool-versions`;
  `make tools-check` rejects drift.
- The user manages local tool upgrades. When `make tools-check` reports that an
  installed tool is newer than the repository pin, update `.tool-versions` and
  every exact mirror to that installed version. Verify compatibility and run the
  affected checks. Do not adopt older, prerelease, or unsupported versions.
- Follow Google Go and TypeScript style. Use `gofmt`/`goimports` and configured
  ESLint. Tests inject clocks, randomness, and UUIDs and pin renderer inputs.
  Never retry a flaky test into a pass.
- Never hand-edit generated files; change the source and regenerate. Before the
  first UAT baseline the integration owner may correct migration SQL and
  recreate the development database. After
  `apps/server/migrations/.uat-baseline` lands, goose migrations are immutable
  and rollback uses a forward migration.
- A contract change updates schema/OpenAPI sources, generated clients and types,
  tests, examples, the phase plan, and traceability in one reviewed change.
- Do not weaken security controls: least privilege, strict input bounds,
  versioned sanitizing, CSRF and Origin checks, `__Host-` cookies,
  route-specific rate limits, CSP, secret-free logs.
- Design rationale belongs in `docs/`, not code comments. Cite files, commands,
  and uncertainty; claim only checks that actually ran. Use Mermaid, not ASCII
  diagrams. Keep living Markdown near 300 lines. A phase's plan is deleted when
  the phase exits; git history keeps it.

Run the narrowest relevant checks:

| Change area                     | Command or evidence                                                                                                                                                  |
| ------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Integration handoff, owner only | `make ci`; workers never run it                                                                                                                                      |
| Markdown or YAML                | Prettier and markdownlint on owned paths; the owner runs `make docs-fmt`                                                                                             |
| Local instructions              | `npx prettier --check --ignore-path /dev/null AGENTS.md CLAUDE.md`, then `npx markdownlint-cli2` on both                                                             |
| Resume schema/generated types   | `make schema-check`                                                                                                                                                  |
| OpenAPI                         | `make api-check`                                                                                                                                                     |
| Go server                       | `make server-build server-vet server-test`                                                                                                                           |
| Store or migrations             | Go gate plus `make sqlc-check server-test-db server-test-integration server-migration-test`                                                                          |
| Nuxt/Vue                        | `make web-lint web-typecheck web-test web-build`                                                                                                                     |
| Unauthenticated UI              | Relevant gate plus `make web-e2e` (scripted headless Playwright)                                                                                                     |
| Authenticated UI                | `make dev-https-auth-check dev-https-editor-check dev-https-mcp-check dev-https-entry-check` (scripted headless Playwright); complete hosted UAT belongs to Phase 10 |
| Public surface                  | `make p5a-native-http-check` and `make dev-https-public-check`                                                                                                       |
| Phase gate or security work     | `make scan` with `SEMGREP_APP_TOKEN` (connected SAST, SCA, secrets, full-history gitleaks)                                                                           |

Reusable browser automation is scripted headless Playwright, committed and run
through `make web-e2e`, `make dev-https-auth-check`,
`make dev-https-transport-check`, `make dev-https-editor-check`, and
`make dev-https-public-check`, `make dev-https-password-check`,
`make dev-https-mcp-check`, and `make dev-https-entry-check`. To author those
tests, use the Playwright MCP server to drive a live browser — inspect real
selectors, requests, and page state — then write what you observed as headless
`@playwright/test` specs. MCP is an authoring aid only; it never runs the
recorded automation.

Report an unrun check with its exact command, reason, and remaining uncertainty.

## Gotchas

- Use Podman and `podman compose`, never Docker. No `sudo`; install user-local
  tools.
- Goose SQL in `apps/server/migrations/` is the migration source and sqlc reads
  it. There is no Atlas or `sql/schema.sql`. Run `make sqlc-check`.
- `.env` is ignored and holds credentials. Never print, document, or stage
  values; add names with empty values to `.env.example`.
- Caddy is the client-IP trust boundary. Go accepts the canonical header only
  from configured trusted proxies and never parses `X-Forwarded-For`.
- Run `make` at the repository root. Run server Go commands under `apps/server`
  and generated-schema Go checks under `packages/schema/gen/go`. Treat a
  generated root `go.work.sum` as a lockfile for the integration owner.
- Live DB targets must use `-count=1`. Compose PostgreSQL is not host-published.
- `sqlc.yaml` must keep its `public.citext` override; sqlc resolves `out:`
  relative to its config even when it looks absolute.
- Use `npx @redocly/cli`, never `npx redocly`.
- In `apps/web`, TypeScript is pinned to 6.0.3 until `vue-tsc` supports TS 7
  package exports. This is not a repository-wide pin.
- golangci-lint uses `//nolint:`; Semgrep uses `// nosemgrep: <rule-id>`. A
  justified exception needs both, with reasons.
- Avoid `podman ps | grep -q` under `pipefail`; capture output before matching.
- `make docs-lint` scans ignored Markdown too. Keep `PROGRESS.md` formatted.
- If host DB TCP fails while in-container `pg_isready` passes, recreate the
  container; rootless pasta can lose its forward while the container stays up.
- FlowCV credentials stay in `.env`; findings go in `docs/research/flowcv/`.
  External references are evidence, not authority.
