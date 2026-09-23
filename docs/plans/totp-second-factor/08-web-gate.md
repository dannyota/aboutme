# TOTP web source and unit gate brief

Role: frontend. Model: Sonnet (Codex: `gpt-5.6-terra`).

## Objective and authority

Regenerate the web source manifest and pin generated-client TOTP shapes after
both UI tasks are accepted. Read `AGENTS.md`, the accepted TOTP contract and
key-management design, accepted ADR 0049, both frontend reports, and the
source-manifest scripts.

## Owned paths

- Modify generated `scripts/web-e2e-source.manifest` through
  `make web-source-manifest-update`.
- Modify `apps/web/test/nuxt/api-contract.test.ts`.

Do not edit product source, OpenAPI, package files, scripts, browser specs,
baselines, backend, infrastructure, or design. Limit the contract test to the
generated TOTP capability, state, enrollment, completion, verification, and
removal shapes.

## Required verification

Start with `git status --short`. Add the generated-client contract case, then
confirm the tree contains only accepted release files before regenerating the
manifest. The update is an authoring step. Do not run local tests, builds, lint,
installs, database writes, browsers, or stacks. Report these as unrun pending
exact-candidate GitHub CI:

```bash
(cd apps/web && npx vitest run test/nuxt/api-contract.test.ts)
make web-source-manifest-check
make web-lint web-typecheck web-test web-build
```

Do not update pixel baselines because TOTP does not change public rendering.
Definition of done: the manifest includes every new source, generated types stay
pinned by a focused test, and hosted Nuxt gates pass. Report exact manifest
lines, checks and results, skipped commands and reason, and open items. Do not
perform Git operations. Use short plain text with no em dash. Code, tests,
comments, and living docs never cite plans, tasks, phases, or review findings;
cite the design, ADR, or `AC-*` ID instead.
