# Shared instructions

Read first: AGENTS.md, docs/standards/engineering.md, instructions/workflow.md,
instructions/documentation.md, instructions/resources.md,
instructions/verification.md, the component's AGENTS.md when present, and the
design docs and accepted ADRs the brief names. Read instructions/go.md for
backend work, instructions/web.md for frontend work, browser proofs, or display
evidence, instructions/deploy.md for devops work or deployment checks,
instructions/testing.md when writing or reviewing tests, selecting checks, or
reviewing check evidence, instructions/gotchas.md when running repository tools
or reading their behavior, and instructions/releases.md for release work.

Work only from the primary's brief. Start with git status --short; leave
existing changes alone. Stay inside the brief's owned paths. Never add, commit,
push, stash, reset, switch branches, tag, deploy, or create worktrees. Never
read or print secret values or personal data. Use plain words and active voice,
no em dashes, no plan, task, or phase IDs in code, comments, tests, or living
docs. Keep Markdown at or under 450 lines, non-test Go under apps/server at or
under 400, and other non-test code at or under 700; a file with a generated
header is exempt. Temporary files and reports go to the scratchpad outside the
repository.

Codex sandboxes cannot run local checks. The primary prefetches dependencies and
runs make pre-push and approved bounded checks under instructions/resources.md.
Read its output and exact-commit CI evidence; never invent results. Stop and
report conflicting authorities, overlapping ownership, or a missing decision
instead of inventing a contract. Do not dispatch other jobs; the primary owns
concurrency and verification.

Report file changes, if any, as exact new, changed, and deleted paths, one line
each, and identify your hunks in shared files. Give checks run with commands and
results, checks not run with exact commands and reasons, evidence paths, and
open items and assumptions. Never claim a check ran when it awaits CI or the
primary.
