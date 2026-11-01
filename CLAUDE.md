@AGENTS.md

## Claude Code specifics

- The primary session runs as `manager` in `.claude/settings.json`; its duties are in AGENTS.md "AI agent definitions".
- The owner chooses the primary session's model; workers and subagents never run on Sonnet or Opus.
- Claude runs only the primary session. Codex runs Astra high for design and research, Terra xhigh for implementation, troubleshooting, and QA, and Luna low for exploration and mechanical reading.
- Every review and fix confirmation runs on Codex Sol xhigh as a separate job from the author. Load the role's model and effort.
- Run Codex: `scripts/codex-agent.sh [--model <m>] [--effort <e>] [--search] <role> <worktree> <brief-file> <report-file>`. The script loads model, effort, sandbox, and developer instructions from `<worktree>/.codex/agents/<role>.toml` and redirects stdin from `/dev/null`.
- Codex sandboxes have no network and no user systemd bus. Prefetch dependencies before workers need them. The primary runs `make pre-push` and approved capped checks; reviewers read the output and exact-commit CI evidence.
- Codex sandboxes mount `.codex/` read-only, so workers cannot edit `.codex/agents/*.toml`; the primary applies those edits.
- Keep temporary files and review reports in the scratchpad outside the repository. Findings not fixed at once become lines in `docs/plans/backlog.md`.

## Commands

- Before every push: `make pre-push` (static checks only, capped).
- Land: `gh pr create --draft --base main` while iterating; `gh pr ready <n>` when complete. Follow [workflow](instructions/workflow.md#flow) for green exact-head CI, independent Codex review, and the signed local merge or train.
- Close notifications after landing: list with `gh api 'notifications?all=true&per_page=100' --paginate --jq '.[] | select(.repository.full_name=="dannyota/aboutme") | "\(.id) \(.subject.title)"'`, review, then `gh api -X DELETE notifications/threads/<id>` for each handled thread.
- Troubleshoot: save each failing job's whole log with `gh run view <run> --job <job> --log > <scratchpad>/run-<run>-<job>.log` and the run's artifacts with `gh run download <run> -D <scratchpad>/run-<run>`, then dispatch `troubleshooter` with those paths.
- For a finished job while its run is still in progress, get its log with `gh api --allow-escape-sequences repos/dannyota/aboutme/actions/jobs/<job-id>/logs` and strip ANSI codes; `gh run view --log` refuses until the run completes.
- Pixel baselines: `gh workflow run baselines.yml --ref <branch>`, then download `candidate-baselines`.
- Deploy: `DEPLOY_HOST=<host> bash deploy/vn/scripts/deploy.sh vX.Y.Z` after green `main` CI and release-images ([releases](instructions/releases.md)).

## Gotchas

- The Bash tool runs zsh: `$var` does not word-split and a word starting with `=` expands to a command path. Claude Code may refuse inline `bash -c` scripts; write multi-step scripts to the scratchpad and run `bash <file>`.
- Commands that read stdin, such as `op` and `ssh`, consume the input of a surrounding `while read` loop. Redirect their stdin from `/dev/null`.
- A wait loop that greps a process list with `pgrep -f` or `ps | grep` matches its own command line and can never end. Wait on a PID, a file, or the run status.
