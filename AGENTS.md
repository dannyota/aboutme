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

`aboutme` is a public AGPL-3.0 resume builder and hosted display service: Go API, Nuxt/Vue web app, PostgreSQL, deferred Flutter app. Resumes, not users, have public URLs. Production runs at `https://aboutme.vn` on one GreenNode host in Vietnam with Cloudflare DNS-only ([ADR 0026](docs/adr/0026-vietnam-hosted-production.md)). **The repository and its CI logs are public.** Never commit secrets, personal data, credentials, or internal notes.

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
|[`instructions/web.md`](instructions/web.md)|TypeScript, Nuxt, renderer, source manifests, or display checks|
|[`instructions/deploy.md`](instructions/deploy.md)|Podman, Caddy, container images, host access, or deployment|
|[`instructions/testing.md`](instructions/testing.md)|Writing tests, browser proofs, or selecting change-area gates|
|[`instructions/documentation.md`](instructions/documentation.md)|Selecting docs, writing agent files, or handling plans and evidence|

## AI agent definitions

The owner talks to the primary agent, which runs as `manager` in `.claude/settings.json`. It plans, briefs, verifies each worker itself, runs bounded local checks, and owns Git, tags, deploys, and the answer to the owner. It manages and does not investigate: troubleshooting goes to the Codex `troubleshooter` with the CI logs and artifacts the primary fetches. Workers take one bounded brief and report to the primary. Claude runs only the primary session; Codex does all design, research, implementation, troubleshooting, QA, and review.

|Tool|Where|Agents with model and effort|
|-|-|-|
|Claude Code|`.claude/agents/`|`manager` (primary; the owner chooses its model)|
|Codex|`.codex/agents/`|`implementer` (`gpt-6.1-terra` xhigh, workspace-write), `troubleshooter` (`gpt-6.1-terra` xhigh, workspace-write, root cause and fix), `code_reviewer` (`gpt-6.1-sol` xhigh, read-only), `design_reviewer` (`gpt-6.1-sol` xhigh, read-only), `qa` (`gpt-6.1-terra` xhigh, workspace-write), `explorer` (`gpt-6-luna` low, read-only), `designer` (`gpt-6-astra` high, workspace-write)|

The table gives role defaults; dispatch overrides and independent reviews follow [workflow](instructions/workflow.md#models-and-ownership). Set model and effort explicitly on every dispatch. Track these agent definitions; keep other local tool settings ignored.

The primary assigns a lane and exact owned paths to each implementer brief. Design proposals belong to the designer; QA owns proofs, display evidence, release checks, and persona dogfooding, never product fixes.

|Lane|Owned paths|Never|
|-|-|-|
|backend|`apps/server/`, `packages/schema/` sources and Go output, `docs/api/`, migrations, MCP|Edits web UI files or production infrastructure|
|frontend|`apps/web/`: pages, editor, renderer, i18n, render workers, web tests and display fixtures|Edits Go, migrations, or production infrastructure|
|devops|`deploy/` (`vn/` host, `caddy/`, `observer/`, and the kept `aws/` reference), `.github/workflows/`, OpenTofu, Cloudflare, release scripts|Reads secret values or applies infrastructure unreviewed|

A path outside these lanes belongs to the primary, which assigns it in a brief. Renderer ownership details live in [web rules](instructions/web.md).

## How we work

- **Pre-1.0 compatibility.** Before application `v1.0.0`, API contracts, migration history, released document schemas and their manifest, and older-client support may change without backwards compatibility. Do not require fallback machinery solely for older v0 releases. Application SemVer and resume document schema versions are separate. The first `v1.0.0` candidate records supported contracts and adds root `.v1-baseline`; trusted comparison bases containing it freeze existing migrations and released schemas. See [ADR 0004](docs/adr/0004-resume-document-contract.md), [ADR 0005](docs/adr/0005-database-migrations.md), and [release rules](instructions/releases.md).
- **Small releases.** One feature per patch release (`v0.x.y`). Ship a feature as soon as it is done and green; do not batch features.
- **GitHub CI is the release gate.** Each commit runs the pre-commit secret and file-length checks; full builds and test suites run in GitHub CI. The owner may land after a passing `make ci-local` under the resource rules using the admin bypass; other contributors need a pull request and green CI. `ci.yml` runs checks per job, including browser proofs; `contracts` runs full-history gitleaks. Only allowlisted prose pull requests may omit the jobs named in the [selection design](docs/design/ci-path-selection.md). The aggregate `ci` job is the only required check on `main`. Hosted CI never calls `make ci`. A red run is fixed forward at once. A tag and a deploy need green CI on that exact commit.
- Branch CI and `main` CI differ; follow [GitHub CI](instructions/verification.md#github-ci) when running CI on a branch.
- **Merge locally.** Keep pull requests draft while iterating and mark them ready when complete. Landing needs independent Codex review and either green pull request CI or the owner local gate. Follow [workflow](instructions/workflow.md#flow) for rulesets, signed local merges, trains, and branch cleanup.
- **Coordinated parallel work:** no numeric agent-job ceiling. Each concurrent writer has its own worktree; coordinate shared fixture writes. Managed local checks remain serialized under [resource rules](instructions/resources.md). Do not spawn workers just to repeat verification.
- **Build and verify.** Review the diff and run the static `make pre-push` check before pushing. The owner may also run `make ci-local` as the landing gate under [resource rules](instructions/resources.md); GitHub CI runs the full gate on every main push. There is no hosted UAT until about 500 users. Local browser proofs follow [resource rules](instructions/resources.md), including the pending opt-in native proof path; other exceptions need behavior CI cannot cover. The owner tests in production.
- **Accepted tooling transition.** [Local tooling](docs/design/local-tooling-simplification.md) and [display assertions](docs/design/display-assertions.md) govern the pending replacements for RSS/nice enforcement, dependency borrowing, native browser execution, pixel comparisons, and the added `server-runtime-smoke` CI job. Preserve existing commands and gates until their replacements land; remove pixel comparisons only after acceptance. Exact-commit release CI is unchanged.
- Match review and checks to risk. Do not invent extra gates.

## Briefs and reports

A brief names the objective, authorities, owned paths, forbidden actions, definition of done, hosted checks, any bounded local exception, and report contract. It repeats the writing rules below, so plan and task IDs never reach code.

Start every task with `git status --short`: existing changes belong to someone else unless the brief says otherwise. Read the named authorities, inspect the code and tests, and write the failing regression test before fixing. Observe its failure in CI or an approved local exception; otherwise report it as unrun. Stop at a document conflict, overlapping ownership, or missing decision, and report the boundary; do not invent a contract.

A report gives: the exact file set (new, changed, deleted; for a file shared with another worker, the hunks that are yours), checks run with results, checks not run with the exact command and reason, screenshots or evidence paths, and open items. Never claim a check that did not run.

The Codex launcher saves only a job's final message as its report. Put the complete report or review in that final message; never rely on writing the report file yourself.

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
