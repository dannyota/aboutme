# Agent workflow

The primary session runs as `manager`. It plans, briefs, verifies each worker itself, runs capped local checks, and owns Git, tags, deploys, and the answer to the owner. It manages the work and does not investigate or debug itself; it fetches inputs workers cannot reach, such as CI logs and artifacts. It asks the owner only for decisions the owner must make, one question at a time, with a recommendation.

## Models and ownership

|Work|Model|Effort|
|-|-|-|
|Design and first-party research|Fable through `designer`|high|
|Review and improve each Fable design; design when Fable is unavailable|`gpt-6-astra` through the Codex `designer` role|high|
|Implementation on Codex|`gpt-6.1-sol`|medium|
|Troubleshooting, flaky tests, and root-cause debugging, fix included|`gpt-6.1-sol` through `troubleshooter`|xhigh|
|Every review and fix confirmation|`gpt-6.1-sol`, as a separate job from the author|xhigh|
|QA proofs and persona dogfooding|`gpt-6.1-sol`|medium|
|Exploration, searches, summaries, mechanical reading|`gpt-6-luna`|low|

Dispatch with `scripts/codex-agent.sh [--model <m>] [--effort <e>] [--search] <role> <worktree> <brief-file> <report-file>`, which loads model, effort, sandbox, and developer instructions from the role TOML in that worktree. The owner chooses the primary session's model. Workers and subagents never run on Sonnet or Opus. Claude models do design and research only; the primary coordinates the work. No Claude model implements or reviews. Codex implementers work in any lane within the brief's owned paths. The designer writes design notes, ADRs, schema and API contract proposals, visual direction, and UI specs; it never implements or reviews.

Design proposals cover benefits, trade-offs, dependencies, limits, alternatives, migration and loss rules, older-client behavior, security, size, and release order. The owner approves product-visible choices before code starts. UI specs give exact values, breakpoints, and copy in both languages. Research cites first-party sources and marks claims supported only by secondary sources as needing verification.

## Flow

1. Read the brief's authorities and the domain's design docs and accepted ADRs. Design wins over a plan; an accepted ADR wins over conflicting design text.
2. For a contract or architecture change, the Fable designer writes the proposal, the Codex `designer` role (Astra high) reviews and improves it in place, and `design_reviewer` reviews the result independently before implementation. Small changes skip design.
3. Brief one implementer per bounded task with exact paths. Write the failing regression test first, then the smallest correct change. Observe failure in CI or an approved local exception; otherwise report it as unrun. Adversarial cases for write safety, races, bounds, hostile input, authorization, and CSRF belong to the author.
4. Run `code_reviewer` once after the last code task. One-line changes, including production DNS, skip the reviewer pass; the primary shows plan or diff evidence instead under [ADR 0024](../docs/adr/0024-delivery-gates.md#amendment-one-line-review-exemption-2026-10-10). Local-only and test-only changes also skip the fresh review under the delivery rules. Other security-relevant changes still require the adversarial pass before merge.
5. Return findings to the author for fixes, then request the original reviewer to confirm each fix at `xhigh`.
6. The primary inspects each diff and report, runs `make pre-push` and approved capped checks, and reads exact-commit CI evidence. It never repeats checks just to confirm a report.
7. Push the branch and open a draft pull request against `main` (`gh pr create --draft`); mark it ready (`gh pr ready`) when the change is complete, which requests the Copilot review. CI and GitHub Copilot review must cover the change. Fix each Copilot finding or reply in its thread with the reason it does not apply. After material fixes, request the next review with `gh pr edit <n> --add-reviewer @copilot`; Copilot adds to independent review and never replaces it.
8. If `main` moved, merge `origin/main` into the branch and push so CI and review cover the combination. The primary merges locally with a signed `--no-ff` merge, pushes `main`, and deletes the branch locally and remotely. Follow [releases](releases.md) for tags and deploys.
9. Close the GitHub notifications the work produced: review each thread for this repository (pull request, review, and failed-run threads), then mark it done with `gh api -X DELETE notifications/threads/<id>`. Close, never just mark read. A failed-run thread closes only after its cause is fixed or explained in the report.

## Reviews

Every review and fix confirmation runs on `gpt-6.1-sol` at `xhigh` as its own job, separate from the job that wrote the work; it may share the author's model. A worker's self-review never counts. A reviewer never implements what it reviews. The original reviewer confirms each fix at `xhigh`.

Security-relevant work beyond one-line changes needs an adversarial pass before merge: auth, sessions, CSRF, sanitizing, media privacy, publish revocation, credentials, keys, signing, production writes, deploys, and release workflows. Also check concurrency, idempotency, and migrations when touched. Name the invariants confirmed and trace each relevant attack end to end.

Model these attackers:

- An anonymous visitor to public resume pages and the API.
- A signed-in user attacking another user's resumes, media, or sessions.
- Hostile resume content (HTML, links, images, imports) reaching the renderer, link previews, print workers, or PDFs.
- A stolen session or MCP token.
- A network position between the visitor and the edge.
- A compromised container on the Vietnam host.
- A database writer without the application keys.
- A leaked backup.

Reviews stay read-only. Reviewers read the primary's check output and exact-commit CI runs; they never run local checks or delegate check execution. Reports go to the scratchpad outside the repository. Findings not fixed at once become lines in `docs/plans/backlog.md`.

## Concurrency and blocked workers

At most four Codex jobs run at once across all work, counting design, implementation, troubleshooting, review, QA, and exploration. Each concurrent writer has its own worktree. The primary creates worktrees and verifies their base commits. Local check execution is globally serialized under [resources](resources.md).

Codex sandboxes have no network or user systemd bus and cannot create capped scopes. The primary prefetches dependencies under the resource rules before a worker needs them, runs `make pre-push` and approved capped checks, and gives reviewers the output.

When a worker stops with a question, a design conflict, overlapping ownership, or a missing decision, the primary answers from the reviewed design or returns the conflict to the designer and independent reviewer. Record an approved contract change in the design before resuming the worker with a new brief that states the answer. Never invent a contract.

When CI fails or a defect needs diagnosis, the primary downloads the failing logs and artifacts to the scratchpad and dispatches `troubleshooter` with that evidence. The worker follows [verification](verification.md#when-a-check-fails), finds the root cause, and fixes it; `code_reviewer` reviews the fix. The worker reads the whole log and fixes every reported error together. Count red runs and their causes; two from the same cause, or a failure covered by `make pre-push`, means stop dispatching and repair the process before the next push. After two failed attempts, diagnose and rebrief instead of retrying unchanged work. Wait on CI in the foreground; do not end with a waiting message.

## Lane instructions

Implementers read their own diff line by line, update tests, specs, snapshots, and docs that assert changed behavior, and follow nearby code patterns. Report needed edits outside the brief's paths instead of making them.

- Backend: use the Go, store, schema, and OpenAPI gates in [verification](verification.md). Migrations are append-only; preserve frozen migrations and grant `aboutme_app` under ADR 0005.
- Frontend: adding a web file needs the source manifest regenerated through the primary. Never run Nuxt tooling in a checkout with an active browser proof. Report every pixel baseline a renderer change moves; generate it through hosted Baselines.
- Devops: follow the [production runbook](../docs/runbooks/production.md) and prefer infrastructure as code. Production infrastructure and release paths beyond one-line changes need adversarial review before merge. The primary applies and deploys already reviewed commits under standing approval without a new review, and stops on unexpected destroys. Check that secrets exist; never read their values.
- Shared generated files: regenerate schema Go, sqlc output, OpenAPI clients, web source manifests, and public roots from their sources when affected. Never hand-edit generated output. The primary runs generators needing capped scopes. The source's assigned implementer reports each changed generated file and line. Serialize migrations and schema versions.

## QA and personas

QA writes scripted headless Playwright proofs, inspects hosted pixel baselines, and checks production releases. It never fixes product code. Use the Playwright MCP server in `.codex/config.toml` to inspect selectors, requests, and state, then write `@playwright/test` specs; MCP never runs recorded automation. Report missing configuration or access to the primary. The primary dispatches Baselines at the exact candidate and downloads `candidate-baselines`; QA reports every changed PNG.

Persona dogfooding reads `PRODUCT.md` and judges only the visible product at phone (390 px) and desktop (1440 px) widths. A Vietnamese job seeker signs up, starts from a template or sample, edits, publishes, shares, and exports in Vietnamese or English. Create accounts and resumes only on the local stack. In production stay signed out unless the brief names a test account; never use the owner's account or create, publish, or delete production resumes. A recruiter holds no account and stays signed out while reading public resumes, gallery samples, PDFs, and applicant tracking system text. Check content, order, credibility, and scan speed.

Save assigned persona evidence under `.dev/personas/`. Persona work never edits product files. Report each defect with steps, URL, expected and actual behavior, severity, screenshot or other evidence, and owning lane. Local proofs need an approved bounded primary-session command; the primary stops the stack when the proof ends.

## Reporting

Use [AGENTS.md](../AGENTS.md#briefs-and-reports) for briefs and exact file reports. Report checks run with results, checks not run with the exact command and reason, evidence paths, and open items. Never claim a failed or skipped check passed.

Review output: Verdict (`merge`, `fix first`, or `redesign`); Findings ordered blocker, major, minor; Checks read (CI runs and the primary's output); Not checked. Code findings use `R-nn. [severity] [category] file:line`; design findings use `S-nn. [severity] [category] file:line`; adversarial findings use `A-nn. [severity] [attacker] file:line`. Quote the code or text, state the failure scenario, and give one fix sentence. Mark an inference and cite its first-party source. Do not write the fix.
