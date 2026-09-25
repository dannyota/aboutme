# AGENTS.md

## Scope and authority

This file is the repository's single source of coding-agent instructions, for Claude Code and Codex alike. `CLAUDE.md` is a tracked bootstrap that points here and adds no rules. Role agents live in `.claude/agents/` and `.codex/agents/`; each is a thin wrapper that names its role and the [instruction files](#instruction-files) it reads.

Direct user instructions and platform safety rules come first. Then:

| Question | Authority |
|-|-|
| Intended product and architecture | `docs/design/` |
| Rationale for one decision | `docs/adr/` |
| Current implemented behavior | Code, deployment configuration, and `docs/api/openapi.yaml` |
| Current architecture narrative | `docs/architecture.md` and `docs/runbooks/` |
| Work order for a multi-step goal | The active plan in `docs/plans/`, when one exists |
| Acceptance-criterion ownership | `docs/plans/traceability/` |

Design wins over a plan. An accepted ADR wins over design text it contradicts; fix the text. Disagreement between code, deployment config, and OpenAPI is a defect: repair them together rather than picking one silently.

`aboutme` is a public AGPL-3.0 resume builder and hosted display service: Go API, Nuxt/Vue web app, PostgreSQL, deferred Flutter app. Resumes, not users, have public URLs. Production runs at `https://aboutme.vn` on one AWS Singapore host behind Amazon CloudFront ([ADR 0037](docs/adr/0037-single-host-production-without-hosted-uat.md), [ADR 0054](docs/adr/0054-cloudfront-edge-for-single-host-production.md)). **The repository and its CI logs are public.** Never commit secrets, personal data, credentials, or internal notes.

## Instruction files

This file holds the rules every agent needs on every task. Topic files in `instructions/` hold the rest; each role file in `.claude/agents/` and `.codex/agents/` names the ones its role must read.

| File | Read when |
|-|-|
| [`instructions/roles.md`](instructions/roles.md) | Managing, leading, or checking another role's duty |
| [`instructions/resources.md`](instructions/resources.md) | Before any local command beyond reading files |
| [`instructions/verification.md`](instructions/verification.md) | Writing, testing, or reviewing code |
| [`instructions/releases.md`](instructions/releases.md) | Reviewing, committing, tagging, or deploying |
| [`instructions/gotchas.md`](instructions/gotchas.md) | Running repository tools |

## Roles

Every agent works in exactly one role. The owner (Danny) talks to the manager. Other roles take work only from a manager brief and report back to it.

| Role | Owns | Never |
|-|-|-|
| manager | Plan, briefs, file ownership, Git, releases, the answer to the owner | Writes feature code that another role owns |
| leads | One lane's plan, worker briefs, and verification (`*-lead`) | Commits, pushes, tags, deploys, or leaves its lane |
| architect | `docs/design/`, `docs/adr/`, schema and API contract proposals | Implements the slice it designed |
| backend | `apps/server/`, `packages/schema/` sources and Go output, `docs/api/`, migrations, MCP | Edits web UI files or production infrastructure |
| frontend | `apps/web/`: pages, editor, renderer, i18n, render workers, web tests and baselines | Edits Go, migrations, or production infrastructure |
| designer | Visual direction, `DESIGN.md`, renderer CSS and template tokens | Changes behavior or data contracts without a brief |
| qa | E2E and dev-https proofs, pixel baselines, exploratory and production checks | Fixes the product code it tests |
| devops | `deploy/`, `.github/workflows/`, Caddy, OpenTofu, Cloudflare, AWS, release scripts | Reads secret values or applies infrastructure unreviewed |
| reviewer | Read-only review of a diff, plan, or release candidate | Edits files, or reviews work it authored |
| user | Dogfooding as a job seeker: sign-up, editing, publishing, sharing, export | Edits files, or uses the owner's production account |
| recruiter | Reading as a recruiter: public pages, PDFs, gallery samples, ATS text | Edits files, or signs in |

A path outside every row belongs to the manager, which assigns it in a brief.

Every session starts as the manager (`"agent": "manager"` in `.claude/settings.json`). Leads and sub-managers run as subagents. Role duties, leads, and the model for each kind of work are in [`instructions/roles.md`](instructions/roles.md).

## How we work

- **Small releases.** One feature per patch release (`v0.x.y`). Ship a feature as soon as it is done and green; do not batch features.
- **GitHub CI is the full gate.** Locally, each commit runs only the pre-commit `gitleaks` scan; builds and test suites run in GitHub CI. CI runs `make ci`, Semgrep, and full-history gitleaks. A red run is fixed forward at once. A tag and a deploy need green CI on that exact commit.
- Branch CI and `main` CI differ; follow [GitHub CI](instructions/verification.md#github-ci) when running CI on a branch.
- **Merge to `main` locally and push; no pull requests.** Delete a merged branch locally and on the remote.
- **Bounded parallel work:** at most four workers across all managers and worktrees. Reading and editing may overlap; local check execution is globally serialized under [resource rules](instructions/resources.md). Do not spawn workers just to repeat verification.
- **Build and verify in GitHub CI.** Do not repeat the CI gates locally before pushing; review your diff carefully and run the static `make pre-push` check instead ([resources](instructions/resources.md)). There is no hosted UAT until about 500 users; use an approved bounded local browser proof only for behavior CI cannot cover. The owner tests in production.
- Match review and checks to risk. Do not invent extra gates.

## Briefs and reports

A brief names the objective, authorities, owned paths, forbidden actions, definition of done, hosted checks, any bounded local exception, and report contract. It repeats the writing rules below, so plan and task IDs never reach code.

Start every task with `git status --short`: existing changes belong to someone else unless the brief says otherwise. Read the named authorities, inspect the code and tests, and write the failing regression test before fixing. Observe its failure in CI or an approved local exception; otherwise report it as unrun. Stop at a document conflict, overlapping ownership, or missing decision, and report the boundary; do not invent a contract.

A report gives: the exact file set (new, changed, deleted; for a file shared with another role, the hunks that are yours), checks run with results, checks not run with the exact command and reason, screenshots or evidence paths, and open items. Never claim a check that did not run.

## Git

Only the manager touches Git. Workers never add, commit, stash, reset, checkout, switch branches, or create worktrees unless the brief authorizes a worktree, and then they verify its base commit.

- Commit exactly the file list a report gives. Never commit "everything except X". Stage with `git add -- <paths>`; when a file also holds another role's uncommitted hunks, stage a blob without them (`git hash-object -w` plus `git update-index --cacheinfo`) and leave the working copy untouched.
- Never use `git add .`, `git add -A`, `git commit -a`, or force-add. Never stage `.env`.
- Reorder or amend only commits that are not pushed. Rebuild the chain in a scratch worktree, check that the new tip's tree equals the old one, then `git reset --soft` to it.
- Conventional Commits. Messages describe the change only and never mention agents, AI, or automated assistance.
- Root `Makefile`, manifests, workflows, lockfiles, migrations, schema heads, and generated snapshots are shared: the owning role edits them only when its brief says so and reports every changed line. Serialize migrations and schema versions.
- Keep local-only paths in `.git/info/exclude`, never in the committed `.gitignore`. Of `.claude/` and `.codex/`, only `agents/` (and `.claude/settings.json`) are tracked.

## Writing docs and code comments

[`docs/standards/engineering.md`](docs/standards/engineering.md) holds the full rules. The ones most often broken:

- Keep text short and plain. Say each fact once. No em dashes.
- Never cite plans, phases, tasks, review findings, or their IDs in code, comments, tests, or living docs. Cite the design doc, ADR, or `AC-*` ID, or state the rule. Plans are deleted when their work ends; design stays.
- Describe the current state, not history.
- Fix stale text in files your work touches, in the same change. No sweeps.
- Markdown stays at or under 450 lines and non-test code at or under 700 (`scripts/check-lengths.sh`).
- Files only agents read (`AGENTS.md`, `CLAUDE.md`, `instructions/`, `.claude/`, `.codex/`, `docs/plans/`) stay minified: one line per paragraph or list item, tables without padding. Prettier and markdownlint skip them. Markdown that people read keeps Prettier's 80-column wrap.
