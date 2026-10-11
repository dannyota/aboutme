---
name: code-review
description:
  Review checklist for aboutme pull requests. Use when reviewing any change to
  this repository.
---

# Reviewing aboutme changes

aboutme is a public AGPL repository with public CI logs, so a leaked value is
published. Read `AGENTS.md` for the full rules. Report concrete defects with the
file, the line, and how the change fails; skip style the linters already check.

## Always check

- Secrets and personal data: no keys, tokens, passwords, account or zone IDs,
  private addresses, or personal emails in code, tests, fixtures, docs, or logs
  a test prints.
- Code, comments, tests, and living docs never cite plans, phases, tasks, or
  review findings; they cite a design doc, an ADR, or an `AC-*` ID.
- US spelling. Markdown files stay at or under 450 lines and non-test code at or
  under 700 (`scripts/check-lengths.sh`).
- Behavior changes come with tests that fail without the change. Removed or
  weakened assertions need a stated reason.
- Vietnamese and English strings change together, with matching keys.

## Production paths

- `deploy/vn/`: host scripts run as root. Look for unquoted expansions, unsafe
  temporary files, symlink races, secrets reaching logs or command lines, and
  units that start before the files or rules they need.
- `deploy/caddy/`: the CrowdSec bouncer and WAF stay ahead of every route, and
  every response, including errors, keeps the security headers.
- `deploy/observer/` and `apps/web/app/utils/deploymentDocument.ts`: the
  deployment document never claims `verified` without a signature check bound to
  the exact running digest.
- Database migrations are append-only; schema and API changes keep released
  versions readable.
- Workflows: pinned action SHAs, least `permissions`, and no untrusted input
  interpolated into `run:` steps.
