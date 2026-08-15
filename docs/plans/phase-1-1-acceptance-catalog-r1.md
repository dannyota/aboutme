# P1.1 acceptance catalog — revision 1

Status: **Frozen** (2026-08-12). Corrections create a later revision; this file
does not change during a run.

> **Historical evidence note (2026-08-15):** P11-003/P11-008 browser evidence
> was captured through the Playwright MCP server as a one-off before a scripted
> headless Playwright harness existed. Reusable browser automation is now
> scripted headless Playwright (`make web-e2e`, `make dev-https-auth-check`,
> `make dev-https-transport-check`, `make dev-https-editor-check`); the
> Playwright MCP server is for agent exploration and test authoring only.

The acceptance worker edits no product code, tests, fixtures, generated output,
or criteria. It records the exact commit, commands, start and end times, state
changes, retries, expected result, observed result, evidence path and hash, and
`PASS | FAIL | BLOCKED` for every row. `BLOCKED`, missing evidence, an
undisclosed retry, or a changed candidate fails the catalog.

| ID      | Expected result                                                                                                                                       | Required evidence                                                             |
| ------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| P11-001 | Every privileged provider start is a CSRF-protected bodiless POST with query purpose; it has no `Content-Type` and no privileged GET fallback         | Focused web tests plus request capture for every provider and both purposes   |
| P11-002 | A `csrf_rejected` start refreshes `/me` and retries once; a second rejection stops; the same rule still holds for existing mutations                  | `useAuth` and settings component test output                                  |
| P11-003 | A valid `{data:{authorizeUrl}}` causes top-level navigation; malformed data, request failure, or navigation failure preserves a clear error state     | Component cases and git-ignored Playwright MCP accessibility/network evidence |
| P11-004 | Equal-time identities are ordered by `(created_at, id)` through SQL and `/me`; the settings default provider is the first item in that order          | Live database test, `/me` test, settings test, generated sqlc diff            |
| P11-005 | Login GET remains unauthenticated and login-only; a privileged purpose returns 405 without a transaction; privileged POST retains auth, CSRF, limits  | Server auth and adversarial test output                                       |
| P11-006 | Callback no-oracle behavior, GitHub's non-OIDC boundary, rotation delivery, per-session revoke, and logout-everywhere retain their current properties | Full server auth gate and static GitHub guard                                 |
| P11-007 | OpenAPI, generated TypeScript, server, and UI agree on methods, query, body, response, error prose, and stable identity order                         | `make api-check`, generated drift result, contract tests                      |
| P11-008 | A real browser performs one link and one reauthentication POST then follows the returned top-level URL; no privileged GET appears                     | Playwright MCP procedure, Chrome version, accessibility snapshot, network log |

Run, in order:

```sh
make sqlc-check
make api-check
make server-build server-vet server-test
make web-lint web-typecheck web-test web-build
make ci
make scan
```

Then run the exact Playwright MCP procedure in
[`phase-1-deferred.md`](phase-1-deferred.md#task-1--settings-page-post-flow).
