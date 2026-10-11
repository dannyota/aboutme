# HTTPS browser proofs

## Ownership and source map

QA owns proof specs and evidence. Devops owns `Dockerfile` and `run.sh`, the browser image and container runner. Keep a brief within the assigned ownership. QA fixes defects in the assigned proof specs; product defects found by proofs go to the product owner.

|Path|Role|
|-|-|
|`playwright.config.ts`, `production.config.ts`|Local and explicitly scoped production proof configuration|
|`auth.spec.ts`, `password-auth.spec.ts`, `second-factor.spec.ts`, `totp.spec.ts`|Authentication proof groups|
|`editor.spec.ts`, `entry.spec.ts`, `publish.spec.ts`, `exports.spec.ts`|Editor, entry, publication, and export proofs|
|`public.spec.ts`, `transport.spec.ts`, `privacy.spec.ts`|Public and transport boundaries|
|`mcp.spec.ts`, `mcp-sdk.spec.ts`, `mcp-ts-sdk.spec.ts`|Agent transport and SDK proofs|
|`spec-sources.txt`, `proof-shards.mjs`|Closed staged source set and shard membership|
|`verify-evidence.mjs`, `check-shard-coverage.mjs`|Evidence schema and complete shard coverage|
|`static-test.sh`|Runner, mount, evidence, and shard regression tests|

## Sources, outputs, and cleanup

`scripts/lib/dev-https-browser-stage.sh` stages the allowlisted proof sources from `spec-sources.txt` for read-only mounting. `run.sh` verifies image identity and input mounts, starts the disposable browser, validates bounded evidence, and removes temporary container state. Specs must clean up their exact disposable accounts and resumes, including failure paths.

Evidence contains only the closed fields accepted by `verify-evidence.mjs`; never retain credentials, cookies, resume content, or unrestricted browser logs. `proof-shards.mjs` feeds specs and coverage validators. QA reports evidence paths; the primary owns stack teardown.

## Authorities and checks

Read [testing](../../instructions/testing.md), [resource rules](../../instructions/resources.md), [deployment rules](../../instructions/deploy.md), [local check runbook](../../docs/runbooks/local-uat.md), and [native development runbook](../../docs/runbooks/native-development.md). Production proofs require the exact account and scope named by the brief.

`static-test.sh` checks the runner without real services. Hosted HTTPS proof groups and evidence validators check browser behavior; [verification](../../instructions/verification.md) owns exact-commit results.
