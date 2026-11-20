# Agent workflow

The primary session runs as `manager`. It plans, briefs, verifies each worker itself, runs bounded local checks, and owns Git, tags, deploys, and the answer to the owner. It manages the work and does not investigate or debug itself; it fetches inputs workers cannot reach, such as CI logs and artifacts. It asks the owner only for decisions the owner must make, one question at a time, with a recommendation.

## Models and ownership

|Work|Model|
|-|-|
|Design, documents, and decisions|The primary|
|Implementation, troubleshooting with its fix, and QA proofs|Claude Sonnet as a subagent of the primary, or Codex Terra (`implementer`, `troubleshooter`, `qa`)|
|Independent review and fix confirmation: code, architecture and contract designs, adversarial passes|A fresh Claude Opus subagent of the primary, or Codex Sol (`code_reviewer`, `design_reviewer`)|
|Hard and complex work: security, concurrency or migration code, or a task that failed twice|Claude Fable or Codex Astra, never a default|
|UI design and visual direction (owner, 2026-11-15)|Claude Opus or Fable as a subagent|
|Exploration, searches, summaries, mechanical reading|An Explore subagent|

Model routing is the owner's (2026-11-20). When Codex is out, Opus reviews, Sonnet implements, and Fable takes the hard and complex work. The reviewing model is never the one that implemented the work. Subagents each work in their own worktree, run bounded build, vet, lint, and test commands under the managed lock, and never change Git state or dispatch other agents.

The primary (owner, 2026-11-10) writes designs, ADR edits, and documentation itself, reviews them, and makes the decisions. Its documentation and prose need no independent review. An architecture or contract design and all code still get one independent review (owner, 2026-11-10). The reviewer is a separate agent with fresh context: it gets the code or design, the authorities and the check output, never the author's conclusions, and it never implements what it reviews.

The owner chooses the primary session's model. Implementers work in any lane within the brief's owned paths. When the primary dispatches Codex, it uses `scripts/codex-agent.sh [--model <m>] [--effort <e>] [--search] <role> <worktree> <brief-file> <report-file>` with the role TOML defaults in `.codex/agents/`.

Design proposals cover benefits, trade-offs, dependencies, limits, alternatives, migration and loss rules, older-client behavior, security, size, and release order. The owner approves product-visible choices before code starts. UI specs give exact values, breakpoints, and copy in both languages. Research cites first-party sources and marks claims supported only by secondary sources as needing verification.

## Flow

1. Read the brief's authorities and the domain's design docs and accepted ADRs. Design wins over a plan; an accepted ADR wins over conflicting design text.
2. For a contract or architecture change, the primary writes the proposal, and an independent reviewer reviews it before implementation. The author revises and the reviewer confirms. Small changes skip design.
3. Brief one implementer per bounded task with exact paths. Write the failing regression test first, then the smallest correct change. Observe failure in CI or an approved local exception; otherwise report it as unrun. Adversarial cases for write safety, races, bounds, hostile input, authorization, and CSRF belong to the author.
4. Run the independent review once after the last code task. One-line changes, including production DNS, skip the reviewer pass; the primary shows plan or diff evidence instead under [ADR 0024](../docs/adr/0024-delivery-gates.md#amendment-one-line-review-exemption-2026-10-10). Local-only and test-only changes, and documentation or prose the primary wrote, also skip the fresh review under the delivery rules. Other security-relevant changes still require the adversarial pass before merge.
5. Return findings to the author for fixes, then request the original reviewer to confirm each fix on the same route.
6. The primary inspects each diff and report, runs `make pre-push` and approved bounded checks under the resource rules, and reads exact-commit CI evidence. It never repeats checks just to confirm a report. Before landing, read the local gate output for the exact head. On the owner fast path, a change to shared scripts, helpers, or test fixtures runs `make operational-test-fast` (or `make ci-local`) before push; a targeted test alone is not enough.
7. Push the branch. Do not open a pull request for the owner's work: a pull request starts hosted CI. Landing needs independent review where step 4 requires it and a passing [local gate](verification.md#local-gate) on the exact head. Finish and land one change before starting the next (owner, 2026-11-10).
8. The primary asks the Ubuntu host session to run the [local gate](verification.md#local-gate) on the exact head, reads its output, then merges with a signed `--no-ff` merge onto current `origin/main` and pushes `main` with the admin bypass on `main-required-ci`. Gate only commits the owner wrote or has reviewed, because the gate executes the checkout's scripts and pnpm lifecycle hooks with the owner's privileges; review changes to the Makefile, workflows, or dependency manifests before running them. For several ready branches, merge each head in order in a fresh worktree from `origin/main`, gate the combined head once, then push. Changes under `deploy/vn/` also dispatch `gh workflow run vn-infra.yml --ref <branch>`, because those tests need a hosted root runner. Contributors' pull requests land only after their `ci` check passed in a `pull_request` run; for those, a local merge of an up-to-date pull request whose checks passed is accepted. `main-required-ci` has an always-on bypass for the repository admin role; the separate `main-history` ruleset blocks force pushes and deletion without a bypass. A `workflow_dispatch` run never satisfies the ruleset ([GitHub](https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/collaborating-on-repositories-with-code-quality-features/troubleshooting-required-status-checks)). Every `main` push runs the full hosted gate; do not wait for it after landing, and check it before a tag under [releases](releases.md). Delete merged branches locally and remotely.
9. Close the GitHub notifications the work produced: review each thread for this repository (pull request, review, and failed-run threads), then mark it done with `gh api -X DELETE notifications/threads/<id>`. Close, never just mark read. A failed-run thread closes only after its cause is fixed or explained in the report.

## Reviews

Each independent review and fix confirmation follows the job's [route](#models-and-ownership) as a separate read-only job from the author. No agent reviews its own work. A worker's self-review never counts. A reviewer never implements what it reviews. The original reviewer confirms each fix on that route.

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

Reviews stay read-only. Reviewers read the primary's check output and exact-commit CI runs; they never run local checks or delegate check execution. Evidence handling follows [documentation instructions](documentation.md#evidence).

## Concurrency and blocked workers

There is no numeric agent-job ceiling. Each concurrent writer has its own worktree. The primary creates worktrees, verifies their base commits, and coordinates shared fixtures. Managed local checks remain serialized under [resources](resources.md); ordinary commands need no mandatory wrapper or lock after the tooling change lands.

Implementing subagents run the brief's bounded checks through the managed lock under the resource rules. Read-only reviewers and explorers run none. The primary runs `make pre-push` before pushing and gives reviewers the check output.

When a worker stops with a question, a design conflict, overlapping ownership, or a missing decision, the primary answers from the reviewed design or returns the conflict to the designer and independent reviewer. Record an approved contract change in the design before resuming the worker with a new brief that states the answer. Never invent a contract.

When CI fails or a defect needs diagnosis, the primary downloads the failing logs and artifacts to the scratchpad and dispatches a troubleshooting subagent with that evidence. The worker follows [verification](verification.md#when-a-check-fails), finds the root cause, and fixes it; an independent reviewer reviews the fix. The worker reads the whole log and fixes every reported error together. Count red runs and their causes; two from the same cause, or a failure covered by `make pre-push`, means stop dispatching and repair the process before the next push. After two failed attempts, diagnose and rebrief instead of retrying unchanged work. Watch CI with a background wait that ends when the run completes and act on its result; never leave a landing half done.

## Lane instructions

Implementers read their own diff line by line, update tests, specs, snapshots, and docs that assert changed behavior, and follow nearby code patterns. Report needed edits outside the brief's paths instead of making them.

- Backend: follow [Go and database rules](go.md) and the change-area gates in [testing](testing.md).
- Frontend: follow [web rules](web.md) for source manifests, renderer boundaries, and display assertions, and [resources](resources.md) for active browser proofs.
- Devops: follow [deployment rules](deploy.md), the production runbook, and release authorities linked there.
- Shared generated files: regenerate schema Go, sqlc output, OpenAPI clients, web source manifests, and public roots from their sources when affected. Never hand-edit generated output. The primary runs generators under [resources](resources.md), using existing callers until wrapper integration lands. The source's assigned implementer reports each changed generated file and line. Serialize migrations and schema versions.

## QA and personas

QA writes scripted headless Playwright proofs, inspects display evidence, and
checks production releases. It never fixes product code. Follow
[testing](testing.md) for Playwright authoring and [web rules](web.md) for
display assertions, review images, and separate product images. The primary runs
or dispatches approved proofs; QA inspects the optional review images under
[display assertions](../docs/design/display-assertions.md); hosted Linux CI is
the display gate. Native HTTPS evidence stays partial and cannot replace Linux
CI.

Persona dogfooding reads `PRODUCT.md` and judges only the visible product at phone (390 px) and desktop (1440 px) widths. A Vietnamese job seeker signs up, starts from a template or sample, edits, publishes, shares, and exports in Vietnamese or English. Create accounts and resumes only on the local stack. In production stay signed out unless the brief names a test account; never use the owner's account or create, publish, or delete production resumes. A recruiter holds no account and stays signed out while reading public resumes, gallery samples, PDFs, and applicant tracking system text. Check content, order, credibility, and scan speed.

Persona evidence and defect reports follow [documentation instructions](documentation.md#evidence). Local proofs need an approved bounded primary-session command; the primary stops the stack when the proof ends.

## Reporting

Use [AGENTS.md](../AGENTS.md#briefs-and-reports) for briefs and exact file reports. Report checks run with results, checks not run with the exact command and reason, evidence paths, and open items. Never claim a failed or skipped check passed.

Review output: Verdict (`merge`, `fix first`, or `redesign`); Findings ordered blocker, major, minor; Checks read (CI runs and the primary's output); Not checked. Code findings use `R-nn. [severity] [category] file:line`; design findings use `S-nn. [severity] [category] file:line`; adversarial findings use `A-nn. [severity] [attacker] file:line`. Quote the code or text, state the failure scenario, and give one fix sentence. Mark an inference and cite its first-party source. Do not write the fix.
