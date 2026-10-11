@AGENTS.md

## Claude Code specifics

- The primary session runs as `manager` in `.claude/settings.json`; its duties are in AGENTS.md "AI agent definitions".
- The owner chooses the primary session's model; workers and subagents never run on Sonnet or Opus.
- Claude does design and research only through the Fable `designer`. When Fable is unavailable, dispatch the Codex `designer` role (`gpt-6-astra` at `high`).
- Implementation, troubleshooting, and review run only on Codex. Load the role's model and effort; follow `instructions/workflow.md` for routing and fix confirmations.
- Run Codex: `scripts/codex-agent.sh [--model <m>] [--effort <e>] [--search] <role> <worktree> <brief-file> <report-file>`. The script loads model, effort, sandbox, and developer instructions from `<worktree>/.codex/agents/<role>.toml` and redirects stdin from `/dev/null`.
- Override the reviewer model for cross-model review, for example `--model gpt-6-astra` when Sol wrote the work under design review.
- Codex sandboxes have no network and no user systemd bus. Prefetch dependencies before workers need them. The primary runs `make pre-push` and approved capped checks; reviewers read the output and exact-commit CI evidence.
- Codex sandboxes mount `.codex/` read-only, so workers cannot edit `.codex/agents/*.toml`; the primary applies those edits.
- Keep temporary files and review reports in the scratchpad outside the repository. Findings not fixed at once become lines in `docs/plans/backlog.md`.

## Commands

- Before every push: `make pre-push` (static checks only, capped).
- Land: `gh pr create --draft --base main` while iterating (CI runs); `gh pr ready <n>` when complete starts the one Copilot review; after material fixes `gh pr edit <n> --add-reviewer @copilot`. Then a signed local `git merge -S --no-ff` and `git push`.
- Close notifications after landing: list with `gh api 'notifications?all=true&per_page=100' --paginate --jq '.[] | select(.repository.full_name=="dannyota/aboutme") | "\(.id) \(.subject.title)"'`, review, then `gh api -X DELETE notifications/threads/<id>` for each handled thread.
- Troubleshoot: save the failing run's log with `gh run view <id> --log-failed > <scratchpad>/run-<id>.log` and its artifacts with `gh run download <id> -D <scratchpad>/run-<id>`, then dispatch `troubleshooter` with those paths.
- Pixel baselines: `gh workflow run baselines.yml --ref <branch>`, then download `candidate-baselines`.
- Deploy: `DEPLOY_HOST=<host> bash deploy/vn/scripts/deploy.sh vX.Y.Z` after green `main` CI and release-images ([releases](instructions/releases.md)).

## Gotchas

- The Bash tool runs zsh: `$var` does not word-split and a word starting with `=` expands to a command path. Claude Code may refuse inline `bash -c` scripts; write multi-step scripts to the scratchpad and run `bash <file>`.
- A wait loop on `pgrep -f '<pattern>'` matches its own command line and never ends. Wait on a PID, a file, or `gh run watch <id>`.
