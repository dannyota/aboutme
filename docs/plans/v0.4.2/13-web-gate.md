# Passkey web source and unit gate brief

Role: frontend. Model: `gpt-5.6-terra`.

## Objective and authority

Regenerate the web source manifest and add the focused generated-client contract
case after both passkey UI tasks are accepted. Read `AGENTS.md`,
`docs/design/second-factor-authentication.md`,
`docs/design/passkey-second-factor-contract.md`, ADR 0048, both frontend
reports, and the source-manifest scripts before editing.

## Owned paths

- Modify generated `scripts/web-e2e-source.manifest` through
  `make web-source-manifest-update`.
- Modify `apps/web/test/nuxt/api-contract.test.ts`.

Do not edit product source, OpenAPI, scripts, packages, browser specs,
baselines, backend, or infrastructure. Limit the API contract test edit to
proving that the generated client exposes the accepted passkey shapes. Report
any other source or test defect to its owning frontend author.

## Required verification

Start with `git status --short`. Add the focused generated-client contract case,
then confirm the working tree contains only the accepted release files before
regenerating the manifest through `make web-source-manifest-update`. The update
is an authoring step, not verification. Do not run local tests, builds, lint,
installs, database writes, browsers, or development stacks. Report these checks
as unrun pending GitHub CI on the exact candidate:

```bash
(cd apps/web && npx vitest run test/nuxt/api-contract.test.ts)
make web-source-manifest-check
make web-lint web-typecheck web-test web-build
```

Do not update pixel baselines. Hosted `make web-e2e` remains a regression gate;
it does not replace the hosted passkey browser proof.

Definition of done: the manifest includes every new web source, exact-candidate
CI passes the complete Nuxt gate, and no public renderer or print source
changed. Report the exact generated manifest lines, checks and results or
awaiting CI, skipped checks with exact reason, client JavaScript byte delta if
the manager requests it, and open items. Do not perform Git operations. Use
short plain text with no em dash.
