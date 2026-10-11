@AGENTS.md

## Claude Code specifics

- The primary session runs as `manager` in `.claude/settings.json`. It plans, briefs, verifies, runs capped local checks, and owns Git, tags, deploys, and the answer to the owner.
- Claude does design and research only through the Fable `designer`. When Fable is unavailable, use a separate Codex `gpt-6-astra` design worker at `high`.
- Implementation and review run only on Codex. Load the role's model and effort; follow `instructions/workflow.md` for routing and fix confirmations.
- Run Codex: `scripts/codex-agent.sh [--model <m>] [--effort <e>] [--search] <role> <worktree> <brief-file> <report-file>`. The script loads model, effort, sandbox, and developer instructions from `<worktree>/.codex/agents/<role>.toml` and redirects stdin from `/dev/null`.
- Override the reviewer model for cross-model review, for example `--model gpt-6-astra` when Sol wrote the work under design review.
- Codex sandboxes have no network and no user systemd bus. Prefetch dependencies before workers need them. The primary runs `make pre-push` and approved capped checks; reviewers read the output and exact-commit CI evidence.
- At most four Codex jobs run at once across all work. Each concurrent writer uses its own worktree, created by the primary.
- Keep temporary files and review reports in the scratchpad outside the repository. Findings not fixed at once become lines in `docs/plans/backlog.md`.
