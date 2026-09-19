# AGENTS.md

## Scope and authority

This file is the repository's single source of coding-agent instructions, for
Claude Code and Codex alike. `CLAUDE.md` is a tracked bootstrap that points here
and adds no rules. Role agents live in `.claude/agents/` and `.codex/agents/`;
each is a thin wrapper that loads its role from [Roles](#roles).

Direct user instructions and platform safety rules come first. Then:

| Question                          | Authority                                                   |
| --------------------------------- | ----------------------------------------------------------- |
| Intended product and architecture | `docs/design/`                                              |
| Rationale for one decision        | `docs/adr/`                                                 |
| Current implemented behavior      | Code, deployment configuration, and `docs/api/openapi.yaml` |
| Current architecture narrative    | `docs/architecture.md` and `docs/runbooks/`                 |
| Work order for a multi-step goal  | The active plan in `docs/plans/`, when one exists           |
| Acceptance-criterion ownership    | `docs/plans/traceability/`                                  |

Design wins over a plan. An accepted ADR wins over design text it contradicts;
fix the text. Disagreement between code, deployment config, and OpenAPI is a
defect: repair them together rather than picking one silently.

`aboutme` is a public AGPL-3.0 resume builder and hosted display service: Go
API, Nuxt/Vue web app, PostgreSQL, deferred Flutter app. Resumes, not users,
have public URLs. Production runs at `https://aboutme.vn` on one AWS Singapore
host behind Cloudflare
([ADR 0037](docs/adr/0037-single-host-production-without-hosted-uat.md)). **The
repository and its CI logs are public.** Never commit secrets, personal data,
credentials, or internal notes.

## Roles

Every agent works in exactly one role. The owner (Danny) talks to the manager.
Other roles take work only from a manager brief and report back to it.

| Role      | Owns                                                                                        | Never                                                    |
| --------- | ------------------------------------------------------------------------------------------- | -------------------------------------------------------- |
| manager   | Plan, briefs, file ownership, Git, releases, the answer to the owner                        | Writes feature code that another role owns               |
| architect | `docs/design/`, `docs/adr/`, schema and API contract proposals                              | Implements the slice it designed                         |
| backend   | `apps/server/`, `packages/schema/` sources and Go output, `docs/api/`, migrations, MCP      | Edits web UI files or production infrastructure          |
| frontend  | `apps/web/`: pages, editor, renderer, i18n, render workers, web tests and baselines         | Edits Go, migrations, or production infrastructure       |
| designer  | Visual direction through the Impeccable flow, `DESIGN.md`, renderer CSS and template tokens | Changes behavior or data contracts without a brief       |
| qa        | E2E and dev-https proofs, pixel baselines, exploratory and production checks                | Fixes the product code it tests                          |
| devops    | `deploy/`, `.github/workflows/`, Caddy, OpenTofu, Cloudflare, AWS, release scripts          | Reads secret values or applies infrastructure unreviewed |
| reviewer  | Read-only review of a diff, plan, or release candidate                                      | Edits files, or reviews work it authored                 |

A path outside every row belongs to the manager, which assigns it in a brief.

### manager

The manager is the integration owner. It turns the owner's request into small
releases, splits each into disjoint file sets, briefs one role per set, and
keeps planning, coordination, verification, Git, tags, deploy decisions, and the
final answer. It asks the owner only for decisions the owner must make, one
question at a time, with a recommendation.

A manager may hand a bounded scope, such as one feature or one release, to a
**sub-manager**: another manager agent with its own workers. The brief names the
scope, owned paths, and version. A sub-manager coordinates and verifies inside
that scope and reports exact file sets upward; only the top manager commits,
pushes, tags, and deploys. A manager may spawn a sub-manager as a subagent
(Claude Code allows three layers below the main conversation) or run it as a
separate named session.

**Lane managers.** Long-running named sessions (`aboutme-backend`,
`aboutme-frontend`, `aboutme-designer`, `aboutme-devops`) run on Opus as
sub-managers for their lane. A lane manager takes briefs from the top manager,
plans its lane, dispatches its role's worker subagents (backend, frontend, qa,
and so on) at the model tier in [Models](#models), verifies their output, and
reports exact file sets upward. It writes code itself only when a change takes
one or two tool calls. Lanes coordinate with each other directly for contract
details and tell the top manager about every cross-lane file.

### architect

The architect writes design notes, ADRs, and contract proposals (schema
versions, API shapes, MCP tools). A proposal covers migration and loss rules,
older-client behavior, security, size, and the release plan. The owner approves
product-visible choices before any code starts. The architect then answers
contract questions during the build but does not implement it.

### backend and frontend

Implementers. Each works test first inside its owned paths, runs the narrowest
affected checks, and reports. A change that needs a file owned by another role
reports the needed edit instead of making it. Shared generated files (web source
manifest, public roots, OpenAPI client) are regenerated by the role whose source
changed, and the report lists every changed line.

### designer

Routes UI design through the Impeccable plugin: direction, decision page, build
review, finish review. It may implement renderer CSS and template tokens when
the brief says so, and it reviews other roles' UI against its spec.

### qa

Writes and runs scripted headless Playwright (`make web-e2e`, the
`dev-https-*-check` targets), regenerates pixel baselines, dogfoods features as
a real user, and verifies each release in production. It reports defects with
steps, evidence, and the owning role; it does not fix product code.

### devops

Owns infrastructure as code and the release path, following
[the production runbook](docs/runbooks/production.md). A change to production
infrastructure code (OpenTofu, IAM, Cloudflare or DNS, deploy scripts) needs a
reviewer's adversarial pass before merge. Applying or deploying an already
reviewed commit needs no new review: the owner's standing approval covers tofu
apply and deploy, and any unexpected destroy stops the run. It checks that
secrets exist; it never reads their values.

### reviewer

Reviews once per plan or release, after the last code task, and adversarially
for auth, sessions, CSRF, sanitizing, concurrency, idempotency, media privacy,
publish revocation, credentials, and production writes. It names each invariant
it confirmed, ranks findings by severity with a failure scenario, and confirms
the fix. It never reviews its own work.

## Models

| Work                                                         | Claude Code | Codex           |
| ------------------------------------------------------------ | ----------- | --------------- |
| Management, design, complex analysis, planning, and review   | Opus        | `gpt-5.6-sol`   |
| Routine implementation and debugging                         | Sonnet      | `gpt-5.6-terra` |
| Search, summaries, test execution, and small mechanical edit | Haiku       | `gpt-5.6-luna`  |

Set the model on every dispatch. A re-check of a small fix after review uses the
implementation tier. Only Sol, Terra, and Luna may run as Codex subagents.

## How we work

- **Small releases.** One feature per patch release (`v0.3.x`). Ship a feature
  as soon as it is done and green; do not batch features.
- **GitHub CI is the full gate.** Locally, each commit runs only the pre-commit
  `gitleaks` scan; workers run the narrowest affected checks. CI runs `make ci`,
  Semgrep, and full-history gitleaks. A red run is fixed forward at once. A tag
  and a deploy need green CI on that exact commit.
- **Merge to `main` locally and push; no pull requests.** Delete a merged branch
  locally and on the remote.
- **Parallel by default,** up to 20 workers and at most 3 build-heavy (builds,
  typechecks, linters, Semgrep, browsers). Queue the rest.
- **Build and verify locally first.** There is no hosted UAT until about 500
  users; the owner tests in production.
- Match review and checks to risk. Do not invent extra gates.

## Delivery and review

[ADR 0024](docs/adr/0024-single-pass-delivery-gates.md) governs:

1. **One author per task.** Write the failing test first, make the smallest
   correct change, run the narrowest checks. Adversarial cases (write safety,
   races, bounds, hostile input, authz, CSRF) are the author's job.
2. **One fresh review per plan or release,** by the reviewer role, before push.
   Local-only and test-only changes skip it. Findings go back to the author; the
   reviewer confirms the fix.

## Releases

The manager releases; devops owns the scripts and infrastructure. Details live
in [the production runbook](docs/runbooks/production.md).

1. Commit each feature as its own exact file set (see [Git](#git)).
2. A renderer change moves pixel baselines. Before pushing, qa (or the frontend)
   regenerates them with `make web-e2e-update` in a clean worktree at that
   commit, and the manager commits them right after it. Every release commit
   must carry its own baselines.
3. Order commits so each release commit holds only that release. Hold commits
   for a later release (for example routes for pages not yet built) above it.
4. Push the commit alone to `main`, wait for green CI, tag `vX.Y.Z`, wait for
   the release-images workflow, run `tofu plan` from the release worktree, then
   `deploy/aws/scripts/deploy.sh vX.Y.Z`.
5. Verify in production, then report.

A resume schema version bump ships with its renderer and editor support in one
release, so no one can save a field the page does not show. Rolling back past a
schema bump breaks documents saved at the new version.

## Resource rules

This laptop has 30 GB RAM and has lost a session to an out-of-memory kill. These
are hard:

- **One database container total.** `aboutme-test-db`, started idempotently by
  `make test-db-up` with a 512 MB cap. It holds `aboutme` for tests and
  `aboutme_dev` for native work. Workers never stop it; only the manager runs
  `make test-db-down`, after every live-DB worker is idle.
- **Daily development is `make dev-native`** (plus `-down`, `-status`, `-logs`).
  Native Go, Nuxt, and Caddy use `aboutme_dev` and serve
  `http://localhost:20080`. Logs and PIDs live under ignored `.dev/`. Say so
  before restarting a stack another session uses.
- **`make dev` is only an HTTP image and self-hosting smoke.** It fails while
  `aboutme-test-db` runs, by design.
- Run full `make ci` locally only to debug a CI failure, alone. Large consumers:
  Nuxt build and typecheck, `golangci-lint` (`GOGC=50`), large `go test -race`
  packages, Semgrep.
- Reconfiguring the database container is scheduled work: announce it, wait for
  an idle window, recreate, then verify both databases and a host-port test.

Development ports are localhost `20000`–`21000`:

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
Rootless port 443 needs `net.ipv4.ip_unprivileged_port_start <= 443`; only a
host administrator changes that. Agents and scripts never use `sudo`.

## Briefs and reports

A brief names the objective, authorities, owned paths, forbidden actions,
definition of done, exact checks, and report contract. It repeats the writing
rules below, so plan and task IDs never reach code.

Start every task with `git status --short`: existing changes belong to someone
else unless the brief says otherwise. Read the named authorities, inspect the
code and tests, and observe a test fail for the expected reason before fixing.
Stop at a document conflict, overlapping ownership, or missing decision, and
report the boundary; do not invent a contract.

A report gives: the exact file set (new, changed, deleted; for a file shared
with another role, the hunks that are yours), checks run with results, checks
not run with the exact command and reason, screenshots or evidence paths, and
open items. Never claim a check that did not run.

## Git

Only the manager touches Git. Workers never add, commit, stash, reset, checkout,
switch branches, or create worktrees unless the brief authorizes a worktree, and
then they verify its base commit.

- Commit exactly the file list a report gives. Never commit "everything except
  X". Stage with `git add -- <paths>`; when a file also holds another role's
  uncommitted hunks, stage a blob without them (`git hash-object -w` plus
  `git update-index --cacheinfo`) and leave the working copy untouched.
- Never use `git add .`, `git add -A`, `git commit -a`, or force-add. Never
  stage `.env`.
- Reorder or amend only commits that are not pushed. Rebuild the chain in a
  scratch worktree, check that the new tip's tree equals the old one, then
  `git reset --soft` to it.
- Conventional Commits. Messages describe the change only and never mention
  agents, AI, or automated assistance.
- Root `Makefile`, manifests, workflows, lockfiles, migrations, schema heads,
  and generated snapshots are shared: the owning role edits them only when its
  brief says so and reports every changed line. Serialize migrations and schema
  versions.
- Keep local-only paths in `.git/info/exclude`, never in the committed
  `.gitignore`. Of `.claude/` and `.codex/`, only `agents/` (and
  `.claude/settings.json`) are tracked.

## Engineering rules

- Pin the latest stable dependency at scaffold time; upgrades need review. Exact
  tool versions live in `.tool-versions`; `make tools-check` rejects drift. When
  an installed tool is newer than the pin, update `.tool-versions` and every
  mirror, then run the affected checks.
- Follow Google Go and TypeScript style with `gofmt`/`goimports` and the
  configured ESLint. Tests inject clocks, randomness, and UUIDs and pin renderer
  inputs. Never retry a flaky test into a pass.
- Never hand-edit generated files; change the source and regenerate. Migrations
  are append-only; roll back with a forward migration and grant `aboutme_app`
  explicitly ([ADR 0038](docs/adr/0038-single-baseline-and-plain-migrator.md)).
- A contract change updates schema or OpenAPI sources, generated clients, tests,
  examples, design docs, and traceability in one change.
- Do not weaken security controls: least privilege, strict input bounds,
  versioned sanitizing, CSRF and Origin checks, `__Host-` cookies,
  route-specific rate limits, CSP, secret-free logs.

Run the narrowest relevant checks:

| Change area                   | Command or evidence                                                                                            |
| ----------------------------- | -------------------------------------------------------------------------------------------------------------- |
| Release candidate             | Green GitHub CI on the pushed commit                                                                           |
| Markdown or YAML              | `node_modules/.bin/prettier --check` and `npx markdownlint-cli2` on touched files                              |
| Resume schema or its types    | `make schema-check`                                                                                            |
| OpenAPI                       | `make api-check`                                                                                               |
| Go server                     | `make server-build server-vet server-test` and `golangci-lint run ./...` over touched packages, tests included |
| Store or migrations           | Go gate plus `make sqlc-check server-test-db server-test-integration server-migration-test`                    |
| Nuxt/Vue                      | `make web-lint web-typecheck web-test web-build`; after adding a web file, `make web-source-manifest-update`   |
| Renderer or public page       | Nuxt gate plus `make web-e2e`; baselines as in [Releases](#releases)                                           |
| Authenticated UI              | `make dev-https-auth-check dev-https-editor-check dev-https-mcp-check dev-https-entry-check`                   |
| Public surface                | `make native-http-check` and `make dev-https-public-check`                                                     |
| Security-sensitive or release | CI's Semgrep and gitleaks jobs; `make scan` with `SEMGREP_APP_TOKEN` locally only to debug them                |

Reusable browser automation is scripted headless Playwright. To author it, use
the Playwright MCP server to inspect real selectors, requests, and state, then
write what you observed as `@playwright/test` specs. MCP never runs the recorded
automation.

## Writing docs and code comments

[`docs/standards/engineering.md`](docs/standards/engineering.md) holds the full
rules. The ones most often broken:

- Keep text short and plain. Say each fact once. No em dashes.
- Never cite plans, phases, tasks, review findings, or their IDs in code,
  comments, tests, or living docs. Cite the design doc, ADR, or `AC-*` ID, or
  state the rule. Plans are deleted when their work ends; design stays.
- Describe the current state, not history.
- Fix stale text in files your work touches, in the same change. No sweeps.
- Markdown stays at or under 450 lines and non-test code at or under 700
  (`scripts/check-lengths.sh`).

## Gotchas

- Use Podman and `podman compose`, never Docker.
- Goose SQL in `apps/server/migrations/` is the migration source and sqlc reads
  it. Run `make sqlc-check`. `sqlc.yaml` keeps its `public.citext` override.
- `.env` is ignored and holds credentials. Never print, document, or stage
  values; add names with empty values to `.env.example`.
- Caddy is the client-IP trust boundary. Go accepts the canonical header only
  from configured trusted proxies and never parses `X-Forwarded-For`.
- Run `make` at the repository root, server Go commands under `apps/server`, and
  generated-schema Go checks under `packages/schema/gen/go`.
- Live DB targets use `-count=1`. Compose PostgreSQL is not host-published.
- Use `npx @redocly/cli`, never `npx redocly`.
- In `apps/web`, TypeScript is pinned to 6.0.3 until `vue-tsc` supports TS 7.
- golangci-lint uses `//nolint:`; Semgrep uses `// nosemgrep: <rule-id>`. A
  justified exception needs both, with reasons. CI enforces errcheck, shadow,
  and US-spelling misspell.
- Go's `filepath.Glob` negates a class with `[^x]`, not `[!x]`.
- `make web-e2e-update` refuses a dirty tree; run it in a clean worktree at the
  commit whose baselines you need.
- `make docs-lint` runs Prettier over every Markdown file, ignored ones too.
- `deploy/web.Dockerfile` copies named paths only. When the web app starts
  importing a new directory, add it there too; CI builds from the full checkout
  and cannot catch the gap, only the release image build does.
- A push to `main` cancels the CI run of any earlier commit still in progress.
  Do not push while a release candidate's CI runs; release the newer commit
  instead if you must.
- Avoid `podman ps | grep -q` under `pipefail`; capture output first.
- If host DB TCP fails while in-container `pg_isready` passes, recreate the
  container; rootless pasta can lose its forward.
- Production has no ad hoc database query path: the hosts run Bottlerocket and
  RDS is private.
