# TOTP contract acceptance brief

Role: architect. Model: `gpt-5.6-sol`.

## Objective and authority

After the owner decides every marked product choice, make the accepted TOTP
contract part of the repository's design, ADR, budget, deployment, and
acceptance authorities. Read `AGENTS.md`, the owner decision, the complete
v0.4.2 factor authorities, `docs/design/totp-second-factor-contract.md`, ADR
0049, and this release plan.

## Owned paths

- Modify `docs/design/totp-second-factor-contract.md` only to record accepted
  choices or the owner's exact revisions.
- Modify `docs/adr/0049-totp-second-factor-authentication.md` and
  `docs/adr/README.md`.
- Modify `docs/design/README.md`, `docs/design/decisions.md`,
  `docs/design/second-factor-authentication.md`, `docs/design/security.md`,
  `docs/design/data.md`, `docs/design/api.md`, `docs/design/budgets.md`, and
  `docs/design/deployment.md`.
- Modify `docs/plans/traceability/README.md`,
  `docs/plans/traceability/ac-auth.md`, and `docs/plans/traceability/ac-sec.md`.
- Modify `docs/plans/v0.4.3-totp.md` only to mark its contract prerequisite
  satisfied and record the accepted base commit.

Do not edit product code, OpenAPI, migrations, generated files, infrastructure,
runbooks, or implementation briefs. Do not mark a rejected or unanswered choice
accepted.

## Required contract

- Resolve every `OWNER APPROVAL REQUIRED` marker from the recorded decision.
- Add every count, byte, lifetime, skew, key, batch, body, URI, and cleanup
  limit to the budget table.
- Make API and security design name the new fields, methods, routes, key ring,
  cache groups, readiness latch, mail events, older-client behavior, and v0.4.3
  floor without weakening v0.4.2.
- Add exact data relations, cascades, indexes, export absence, rotation, and
  migration-loss rules, including both closed mail constraints.
- Record the canonical lock order, bounded first-policy handle creation and
  reuse, app-execution-role-only key reads, and dedicated one-shot rotation
  path.
- Add acceptance rows for TOTP profile and replay, encrypted storage and
  rotation, lifecycle races, pending and recovery composition, flags and mixed
  versions, bilingual UI and mail, and floor 4003.
- Keep current-state design text free of plan and task IDs.

## Checks and report

Run only the repository's small Markdown formatting checks on touched files:

```bash
node_modules/.bin/prettier --check <exact-touched-markdown-files>
npx markdownlint-cli2 <exact-touched-markdown-files>
scripts/check-lengths.sh <exact-touched-markdown-files>
git diff --check -- <exact-touched-markdown-files>
```

Do not install a missing tool. Definition of done: no owner choice remains and
no implementer must invent a wire, storage, security, budget, migration, or
release rule. Obtain a fresh Sol review. Report exact files, checks and results,
skipped checks with exact reason, owner decisions, review findings and
confirmation, and open items. Do not perform Git operations. Use short plain
text with no em dash.
