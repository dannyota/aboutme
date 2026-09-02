# Phase PF exit criteria

The integration owner checks every item at one unchanged candidate commit.
Failed or unsatisfiable items are corrected and rerun under ADR 0024. When every
item passes and the review is clean, the exit commit deletes
`docs/plans/phase-pf/`.

## Authorities

- [ ] ADR 0027 and ADR 0028 are accepted and indexed in
      `docs/design/decisions.md`; product, web, security, and API design pages
      describe the flag, the capabilities read, the landing page, the shell by
      state, and the no-operator-surface rule without contradiction.
- [ ] `.env.example` lists `PROVIDER_LOGIN_ENABLED=` with an empty value and a
      comment naming the design section.
- [ ] Traceability rows AC-AUTH-017, AC-AUTH-018, AC-SEC-006, AC-OPS-021, and
      AC-OPS-022 are PROVEN with exact test and proof references.

## Server

- [ ] `PROVIDER_LOGIN_ENABLED` parses exactly `true`, `false`, blank; an invalid
      value fails startup and the error names the variable, never the value.
- [ ] With the flag false, every provider start and callback path returns the
      uniform not-found envelope for every method; `/api/v1/me`,
      `/api/v1/sessions`, logout, and every password route stay registered.
- [ ] With the flag true, the existing provider suites pass unchanged.
- [ ] `GET /api/v1/capabilities` is unauthenticated, returns exactly the two
      required booleans reflecting configuration, rejects other methods with
      405, and carries `Cache-Control: no-store` through the router chain.
- [ ] `dev-seed seed` is idempotent across two runs, refuses a database not
      named `aboutme_dev` or not on loopback, fails loudly when the seed email
      exists under another ID, never overwrites an existing document or
      credential, and `cleanup` removes exactly its two rows. The embedded
      fixture matches `packages/schema/fixtures/full.json` byte for byte.

## Web

- [ ] The shell shows Sign in, Create account, and the theme toggle while logged
      out or loading, and Resumes, Settings, and the account control when
      authenticated; the editor route keeps its own top bar.
- [ ] `/` renders exactly the D5 copy and links; no app navigation is present
      for a logged-out visitor; `PlaceholderHero` no longer exists.
- [ ] `/login` renders provider links only when `providerLogin` is true; a
      failed capabilities read shows none.
- [ ] `/app/settings/sessions` shows the provider block only when
      `providerLogin` is true and Connected agents only when `agentAccess` is
      true, and issues no request to `/api/v1/me/agents` when it is false.
- [ ] `make web-lint web-typecheck web-test web-build` pass.

## Harness and proof

- [ ] The HTTPS harness server environment contains
      `PROVIDER_LOGIN_ENABLED=true`; `scripts/dev-https-test.sh --static`
      asserts it.
- [ ] `make dev-https-entry-check` passes end to end after
      `make dev-https-browser-image` with the updated `run.sh`; evidence is
      `entry-proof.json`, mode 0600, at most 4,096 bytes, containing only the
      scenario, origin, five step booleans, and four zero error counters.
- [ ] The existing auth, transport, editor, public, password, and MCP checks
      still pass with the harness flag on.
- [ ] `make operational-test` passes, including the static browser and Makefile
      safety tests.

## Records and gate

- [ ] `docs/architecture.md`, the roadmap, and the native development runbook
      describe the landing page, the flag, the capabilities read, and the seed
      credentials.
- [ ] One fresh non-author review confirms the route-absence,
      capabilities-no-store, seed-database-guard, no-operator-surface, and
      no-SSR-fetch invariants by name; findings are fixed and confirmed.
- [ ] `make ci` (foreground chunks) and connected `make scan` pass at the
      candidate.
