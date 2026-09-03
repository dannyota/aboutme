# P5B T03 — native HTTPS publish proof

## Contract

Extend the existing native HTTPS Playwright harness with a `publish` mode and
`make dev-https-publish-check`. Before the browser starts,
`scripts/dev-https-check.sh publish` builds `apps/server/cmd/dev-seed`, runs its
idempotent `seed` command against `aboutme_dev`, and uses the fixed local-only
password account already pinned by `entry.spec.ts`. It does not run dev-seed
cleanup because that would delete the shared native development account and
sample resume. The browser creates its own uniquely named complete resume and a
unique valid slug, then deletes that disposable resume in a `finally` cleanup
using its current session, CSRF token, revision, and a fresh delete idempotency
key. Do not introduce a second database container, browser image, server
process, account, or evidence format.

The browser scenario must:

1. Sign in, create a disposable resume through the UI, set a nonblank full name,
   and add one visible work entry with nonblank job title and employer so it
   satisfies the current publish-completeness policy without changing the fixed
   sample resume.
2. Make an unsaved edit, open Publish, and prove the accepted edit precedes the
   publish request.
3. Publish with a unique valid slug, PDF download on, and SEO/GEO off.
4. Verify exact mutation headers from browser network observation without
   recording their secret values.
5. Open the canonical public link and prove the live HTML response and the
   expected `noindex, noarchive` discovery state.
6. Reopen the dialog, enable SEO/GEO, and prove the updated public response.
7. Unpublish, prove the slug remains in owner metadata, and prove the public URL
   returns the uniform `404` inside the five-second revocation budget.
8. Delete the disposable resume, sign out, and leave no committed or unbounded
   evidence. A failed assertion must still attempt exact-resume cleanup.

The scenario must also prove keyboard dialog operation, no serious accessibility
violations under the harness's existing policy, no unexpected console error, and
no request outside the harness origin. Unit tests own forced reauth and every
error family because the deterministic recent login should not be aged or
bypassed in the browser proof.

Write bounded secret-free `publish-proof.json` under the existing ignored
evidence directory. Store booleans, response status, bounded elapsed
milliseconds, and header-presence flags only. Never store cookies, CSRF values,
idempotency keys, passwords, response bodies, or personal data.

## Ownership and checks

Worker-owned paths:

- `deploy/dev-https-browser/publish.spec.ts`
- `deploy/dev-https-browser/playwright.config.ts`
- `deploy/dev-https-browser/run.sh`
- `deploy/dev-https-browser/static-test.sh`
- `scripts/dev-https-check.sh`

Integration-owner paths:

- `Makefile`
- `docs/runbooks/local-uat.md`

Acceptance: `AC-PUB-006`, `AC-PUB-007`, `AC-PUB-010`. Budget: public revocation
completes within five seconds. `AC-MCP-007` remains unchanged and must still
have no publish/unpublish/public-read tool.

Start by changing `deploy/dev-https-browser/static-test.sh` to require
`publish.spec.ts` in the staged source list, reject missing or extra sources,
accept only the closed `publish` mode, give publish the existing 120-second
heavy-flow timeout, compile/list only `publish.spec.ts`, validate the bounded
`publish-proof.json` schema and mode-specific invocation, and exercise the outer
cleanup trap. Observe the static test fail before the implementation changes.
Run:

```sh
bash deploy/dev-https-browser/static-test.sh
```

The integration owner then applies the reported `.PHONY`, help, target, and
safety-test updates in `Makefile`, confirms `scripts/dev-https-check.sh` stages
the exact source list and seeds publish like entry, and runs:

```sh
make dev-https-publish-check
```

Do not edit Git state, application UI/transport, generated files, or phase
records. Report the first observed failing test, changed files, exact commands
and results, evidence path and bounded summary, resource state after teardown,
and the exact shared-file edits needed from the integration owner.
