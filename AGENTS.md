# AGENTS.md

## Scope and authority

This file is the repository's single source of coding-agent instructions, for Claude Code and Codex alike. `CLAUDE.md` imports it and adds Claude Code specifics. Agent definitions live in `.claude/agents/` and `.codex/agents/`; each is a thin wrapper that names its duty and the [instruction files](#instruction-files) it reads.

Direct user instructions and platform safety rules come first.

## Map

|Question|File|
|-|-|
|Intended product and architecture|`docs/design/`|
|Rationale for one decision|`docs/adr/`|
|Current implemented behavior|Code, deployment configuration, and `docs/api/openapi.yaml`|
|Current architecture narrative|`docs/architecture.md` and `docs/runbooks/`|
|Work order for a multi-step goal|The active plan in `docs/plans/`, when one exists|
|Open follow-ups|`docs/plans/backlog.md`|
|Acceptance-criterion ownership|`docs/plans/traceability/`|
|Writing rules|`docs/standards/engineering.md`|

|Doing|Read first|
|-|-|
|Design or research|`instructions/workflow.md`, accepted ADRs, and the domain's design docs|
|Writing docs or comments|`docs/standards/engineering.md`|
|Implementing or reviewing|`instructions/workflow.md`, `instructions/verification.md`, and the brief's authorities|
|Running local commands|`instructions/resources.md` and `instructions/gotchas.md`|
|Committing, tagging, or deploying|`instructions/workflow.md` and `instructions/releases.md`|

Design wins over a plan. An accepted ADR wins over design text it contradicts; fix the text. Disagreement between code, deployment config, and OpenAPI is a defect: repair them together rather than picking one silently.

`aboutme` is a public AGPL-3.0 resume builder and hosted display service: Go API, Nuxt/Vue web app, PostgreSQL, deferred Flutter app. Resumes, not users, have public URLs. Production runs at `https://aboutme.vn` on one GreenNode host in Vietnam with Cloudflare DNS-only ([ADR 0027](docs/adr/0027-vietnam-hosted-production.md)). **The repository and its CI logs are public.** Never commit secrets, personal data, credentials, or internal notes.

## Instruction files

This file holds the rules every agent needs on every task. Topic files in `instructions/` hold the rest; each agent file in `.claude/agents/` and `.codex/agents/` names the ones its role must read.

| File | Read when |
|-|-|
| [`instructions/workflow.md`](instructions/workflow.md) | Planning, briefing, implementing, or reviewing |
| [`instructions/resources.md`](instructions/resources.md) | Before any local command beyond reading files |
| [`instructions/verification.md`](instructions/verification.md) | Writing, testing, or reviewing code |
| [`instructions/releases.md`](instructions/releases.md) | Reviewing, committing, tagging, or deploying |
| [`instructions/gotchas.md`](instructions/gotchas.md) | Running repository tools |

## AI agent definitions

The owner talks to the primary agent, which runs as `manager` in `.claude/settings.json`. It plans, briefs, verifies each worker itself, runs capped local checks, and owns Git, tags, deploys, and the answer to the owner. Workers take one bounded brief and report to the primary. Claude models do design and research only; the manager coordinates that work and Codex jobs. No implementation or review runs on a Claude model.

|Tool|Where|Agents with model and effort|
|-|-|-|
|Claude Code|`.claude/agents/`|`manager` (primary; the owner chooses its model), `designer` (Fable high, design and first-party research)|
|Codex|`.codex/agents/`|`implementer` (`gpt-6.1-sol` medium, workspace-write), `code_reviewer` (`gpt-6-astra` xhigh, read-only), `design_reviewer` (`gpt-6.1-sol` xhigh, read-only), `qa` (`gpt-6.1-sol` medium, workspace-write), `explorer` (`gpt-6-luna` low, read-only), `designer` (`gpt-6-astra` high, workspace-write, only when Fable is unavailable)|

When Fable is unavailable, the Codex `designer` role (`gpt-6-astra` at `high`) writes designs, and Sol reviews them. Set model and effort explicitly on every dispatch. Review routing and fix confirmations follow [workflow](instructions/workflow.md#reviews); a reviewer never reviews work written by the same model or implements what it reviews. Track these agent definitions; keep other local tool settings ignored.

The primary assigns a lane and exact owned paths to each implementer brief. Design proposals belong to the designer; QA owns proofs, baselines, release checks, and persona dogfooding, never product fixes.

|Lane|Owned paths|Never|
|-|-|-|
|backend|`apps/server/`, `packages/schema/` sources and Go output, `docs/api/`, migrations, MCP|Edits web UI files or production infrastructure|
|frontend|`apps/web/`: pages, editor, renderer, i18n, render workers, web tests and baselines|Edits Go, migrations, or production infrastructure|
|devops|`deploy/` (`vn/` host, `caddy/`, `observer/`, and the kept `aws/` reference), `.github/workflows/`, OpenTofu, Cloudflare, release scripts|Reads secret values or applies infrastructure unreviewed|

A path outside these lanes belongs to the primary, which assigns it in a brief. Renderer CSS and template tokens are frontend implementation; the designer supplies visual direction and UI specs.

## How we work

- **Small releases.** One feature per patch release (`v0.x.y`). Ship a feature as soon as it is done and green; do not batch features.
- **GitHub CI is the full gate.** Locally, each commit runs only the pre-commit `gitleaks` scan; builds and test suites run in GitHub CI. CI runs `make ci`, Semgrep, and full-history gitleaks. A red run is fixed forward at once. A tag and a deploy need green CI on that exact commit.
- Branch CI and `main` CI differ; follow [GitHub CI](instructions/verification.md#github-ci) when running CI on a branch.
- **Land through a pull request, merge locally.** Push the branch and open a pull request against `main`. The pull request runs CI, and the `copilot-review` ruleset requests a GitHub Copilot review when it opens and on every push. Fix each Copilot finding on the branch or reply in its thread with the reason it does not apply. If `main` moved since the last run, merge `origin/main` into the branch and push first, so CI and the review cover what lands. Then merge to `main` locally with a signed `--no-ff` merge and push; GitHub marks the pull request merged. Delete the branch locally and on the remote. Copilot's review adds to the independent Codex review and never replaces it.
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

[`docs/standards/engineering.md`](docs/standards/engineering.md) holds the full rules. The ones most often broken:

- Keep text short and plain. Say each fact once. No em dashes.
- Never cite plans, phases, tasks, review findings, or their IDs in code, comments, tests, or living docs. Cite the design doc, ADR, or `AC-*` ID, or state the rule. Plans are deleted when their work ends; design stays.
- Describe the current state, not history.
- Fix stale text in files your work touches, in the same change. No sweeps.
- Markdown stays at or under 450 lines and non-test code at or under 700 (`scripts/check-lengths.sh`).
- Files only agents read (`AGENTS.md`, `CLAUDE.md`, `instructions/`, `.claude/`, `.codex/`, `docs/plans/`) stay minified: one line per paragraph or list item, tables without padding. Prettier and markdownlint skip them. Markdown that people read keeps Prettier's 80-column wrap.
