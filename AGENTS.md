# AGENTS.md

## Scope and authority

This file owns shared coding-agent rules for Claude Code and Codex. Component `AGENTS.md` files map local sources, boundaries, authorities, and checks. `CLAUDE.md` imports it and adds Claude Code specifics. Agent definitions live in `.claude/agents/` and `.codex/agents/`; each is a thin wrapper that names its duty and the [instruction files](#instruction-files) it reads.

Direct user instructions and platform safety rules come first.

## Map

|Question|Entry point|
|-|-|
|What component owns this work?|Component map below and nearest component `AGENTS.md`|
|What behavior is intended?|`docs/design/README.md` and named accepted ADRs|
|What behavior exists?|Code, deployment configuration, and `docs/api/openapi.yaml`|
|How do the running components connect?|`docs/architecture.md`|
|How is production operated?|`docs/runbooks/README.md`, then the named runbook|
|What work remains?|Active plan and `docs/plans/backlog.md`|
|Which acceptance criteria are covered?|`docs/plans/traceability/`|
|How should documents and comments read?|`docs/standards/engineering.md`|
|Where is historical evidence?|`docs/research/README.md`|

|Doing|Read first|
|-|-|
|Any bounded repository task|Root rules, brief, nearest component guide|
|Planning, implementation, or review|`instructions/workflow.md` and named authorities|
|Go work|`instructions/go.md`|
|Web or renderer work|`instructions/web.md`|
|Deployment work|`instructions/deploy.md`; `instructions/releases.md` for release work|
|Writing or reviewing tests|`instructions/testing.md`; `instructions/verification.md` for gate evidence|
|Writing documentation|`instructions/documentation.md` and engineering standard|
|Running local commands|`instructions/resources.md` and applicable remaining gotchas|

### Component map

|Component|Guide|
|-|-|
|Go API and domains|[apps/server](apps/server/AGENTS.md)|
|Web application|[apps/web](apps/web/AGENTS.md)|
|Pure resume renderer|[resume components](apps/web/app/components/resume/AGENTS.md)|
|Nitro render services|[web server](apps/web/server/AGENTS.md)|
|Resume contract and generators|[schema](packages/schema/AGENTS.md)|
|Documentation|[docs](docs/AGENTS.md)|
|HTTP contract|[API](docs/api/AGENTS.md)|
|Deployment artifacts|[deploy](deploy/AGENTS.md)|
|Vietnam production|[Vietnam host](deploy/vn/AGENTS.md)|
|Edge routing|[Caddy](deploy/caddy/AGENTS.md)|
|Deployment transparency|[observer](deploy/observer/AGENTS.md)|
|Undeployed AWS reference|[AWS](deploy/aws/AGENTS.md)|
|HTTPS browser proofs|[browser harness](deploy/dev-https-browser/AGENTS.md)|
|Repository tooling|[scripts](scripts/AGENTS.md)|

Design wins over a plan. An accepted ADR wins over design text it contradicts; fix the text. Disagreement between code, deployment config, and OpenAPI is a defect: repair them together rather than picking one silently.

`aboutme` is a public AGPL-3.0 resume builder and hosted display service: Go API, Nuxt/Vue web app, PostgreSQL, deferred Flutter app. Resumes, not users, have public URLs. Production runs at `https://aboutme.vn` on one GreenNode host in Vietnam with Cloudflare DNS-only ([ADR 0027](docs/adr/0027-vietnam-hosted-production.md)). **The repository and its CI logs are public.** Never commit secrets, personal data, credentials, or internal notes.

## Instruction files

This file holds the rules every agent needs on every task. Topic files in `instructions/` hold the rest; each agent file in `.claude/agents/` and `.codex/agents/` names the ones its role must read.

|File|Read when|
|-|-|
|[`instructions/workflow.md`](instructions/workflow.md)|Planning, briefing, implementing, or reviewing|
|[`instructions/resources.md`](instructions/resources.md)|Before any local command beyond reading files|
|[`instructions/verification.md`](instructions/verification.md)|Writing, testing, or reviewing code|
|[`instructions/releases.md`](instructions/releases.md)|Reviewing, committing, tagging, or deploying|
|[`instructions/gotchas.md`](instructions/gotchas.md)|Running repository tools|
|[`instructions/go.md`](instructions/go.md)|Go, store, schema Go, or migrations|
|[`instructions/web.md`](instructions/web.md)|TypeScript, Nuxt, renderer, source manifests, or baselines|
|[`instructions/deploy.md`](instructions/deploy.md)|Podman, Caddy, container images, host access, or deployment|
|[`instructions/testing.md`](instructions/testing.md)|Writing tests, browser proofs, or selecting change-area gates|
|[`instructions/documentation.md`](instructions/documentation.md)|Selecting docs, writing agent files, or handling plans and evidence|

## AI agent definitions

The owner talks to the primary agent, which runs as `manager` in `.claude/settings.json`. It plans, briefs, verifies each worker itself, runs capped local checks, and owns Git, tags, deploys, and the answer to the owner. It manages and does not investigate: troubleshooting goes to the Codex `troubleshooter` with the CI logs and artifacts the primary fetches. Workers take one bounded brief and report to the primary. Claude models do design and research only; the manager coordinates that work and Codex jobs. No implementation, troubleshooting, or review runs on a Claude model.

|Tool|Where|Agents with model and effort|
|-|-|-|
|Claude Code|`.claude/agents/`|`manager` (primary; the owner chooses its model), `designer` (Fable high, design and first-party research)|
|Codex|`.codex/agents/`|`implementer` (`gpt-6.1-sol` medium, workspace-write), `troubleshooter` (`gpt-6.1-sol` xhigh, workspace-write, root cause and fix), `code_reviewer` (`gpt-6.1-sol` xhigh, read-only), `design_reviewer` (`gpt-6.1-sol` xhigh, read-only), `qa` (`gpt-6.1-sol` medium, workspace-write), `explorer` (`gpt-6-luna` low, read-only), `designer` (`gpt-6-astra` high, workspace-write, improves each Fable design or designs when Fable is unavailable)|

The Codex `designer` role (`gpt-6-astra` at `high`) reviews and improves each Fable design, or writes the design when Fable is unavailable; Sol then reviews it. Set model and effort explicitly on every dispatch. Reviews and fix confirmations follow [workflow](instructions/workflow.md#reviews): Sol xhigh reviews as a separate job from the author and never implements what it reviews. Track these agent definitions; keep other local tool settings ignored.

The primary assigns a lane and exact owned paths to each implementer brief. Design proposals belong to the designer; QA owns proofs, baselines, release checks, and persona dogfooding, never product fixes.

|Lane|Owned paths|Never|
|-|-|-|
|backend|`apps/server/`, `packages/schema/` sources and Go output, `docs/api/`, migrations, MCP|Edits web UI files or production infrastructure|
|frontend|`apps/web/`: pages, editor, renderer, i18n, render workers, web tests and baselines|Edits Go, migrations, or production infrastructure|
|devops|`deploy/` (`vn/` host, `caddy/`, `observer/`, and the kept `aws/` reference), `.github/workflows/`, OpenTofu, Cloudflare, release scripts|Reads secret values or applies infrastructure unreviewed|

A path outside these lanes belongs to the primary, which assigns it in a brief. Renderer ownership details live in [web rules](instructions/web.md).

## How we work

- **Small releases.** One feature per patch release (`v0.x.y`). Ship a feature as soon as it is done and green; do not batch features.
- **GitHub CI is the full gate.** Locally, each commit runs only the pre-commit `gitleaks` scan; builds and test suites run in GitHub CI. `ci.yml` runs checks per job, including browser proofs and two Semgrep jobs; `contracts` runs full-history gitleaks. The aggregate `ci` job is the only required check on `main`. Hosted CI never calls `make ci`. A red run is fixed forward at once. A tag and a deploy need green CI on that exact commit.
- Branch CI and `main` CI differ; follow [GitHub CI](instructions/verification.md#github-ci) when running CI on a branch.
- **Land through a pull request, merge locally.** Push the branch and open a pull request against `main`. Open it as a draft while iterating; the pull request runs CI. Marking it ready for review makes the `copilot-review` ruleset request one GitHub Copilot review; request another with `gh pr edit <n> --add-reviewer @copilot` after material fixes, not on every push. Fix each Copilot finding on the branch or reply in its thread with the reason it does not apply. The `main-required-ci` ruleset rejects any `main` commit without a `ci` check that passed in a `pull_request` or `push` run, and blocks force pushes and deletion. Land one up-to-date green pull request with a signed local `--no-ff` merge, or several through a train pull request ([workflow](instructions/workflow.md#flow)). GitHub marks each pull request merged; delete its branch locally and on the remote. Copilot's review adds to the independent Codex review and never replaces it; while Copilot is unavailable, land on CI and the Codex review ([workflow](instructions/workflow.md#flow)).
- **Bounded parallel work:** at most four Codex jobs across all work, each concurrent writer in its own worktree. Reading and editing may overlap; local check execution is globally serialized under [resource rules](instructions/resources.md). Do not spawn workers just to repeat verification.
- **Build and verify in GitHub CI.** Do not repeat the CI gates locally before pushing; review your diff carefully and run the static `make pre-push` check instead ([resources](instructions/resources.md)). There is no hosted UAT until about 500 users; use an approved bounded local browser proof only for behavior CI cannot cover. The owner tests in production.
- Match review and checks to risk. Do not invent extra gates.

## Briefs and reports

A brief names the objective, authorities, owned paths, forbidden actions, definition of done, hosted checks, any bounded local exception, and report contract. It repeats the writing rules below, so plan and task IDs never reach code.

Start every task with `git status --short`: existing changes belong to someone else unless the brief says otherwise. Read the named authorities, inspect the code and tests, and write the failing regression test before fixing. Observe its failure in CI or an approved local exception; otherwise report it as unrun. Stop at a document conflict, overlapping ownership, or missing decision, and report the boundary; do not invent a contract.

A report gives: the exact file set (new, changed, deleted; for a file shared with another worker, the hunks that are yours), checks run with results, checks not run with the exact command and reason, screenshots or evidence paths, and open items. Never claim a check that did not run.

## Git

Only the primary changes Git state. Workers may read status and diffs. Workers never add, commit, stash, reset, checkout, switch branches, or create worktrees unless the brief authorizes a worktree, and then they verify its base commit.

- Commit exactly the file list a report gives. Never commit "everything except X". Stage with `git add -- <paths>`; when a file also holds another worker's uncommitted hunks, stage a blob without them (`git hash-object -w` plus `git update-index --cacheinfo`) and leave the working copy untouched.
- Never use `git add .`, `git add -A`, `git commit -a`, or force-add. Never stage `.env`.
- Reorder or amend only commits that are not pushed. Rebuild the chain in a scratch worktree, check that the new tip's tree equals the old one, then `git reset --soft` to it.
- Conventional Commits. Messages describe the change only and never mention agents, AI, or automated assistance.
- Root `Makefile`, manifests, workflows, lockfiles, migrations, schema heads, and generated snapshots are shared: the assigned implementer edits them only when its brief says so and reports every changed line. Serialize migrations and schema versions.
- Keep local-only paths in `.git/info/exclude`, never in the committed `.gitignore`. Of `.claude/` and `.codex/`, only `agents/` (and `.claude/settings.json`) are tracked.

## Writing docs and code comments

Human writing rules, documentation ownership, and ADR policy live in the [engineering standard](docs/standards/engineering.md). Agent-file formatting, document selection, and plan/evidence handling live in [documentation instructions](instructions/documentation.md).
