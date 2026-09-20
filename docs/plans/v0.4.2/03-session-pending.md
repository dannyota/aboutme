# Session and pending authority brief

Role: backend. Model: `gpt-5.6-terra`.

## Objective and authority

Implement current-epoch browser sessions, separate pending authentication, and
the shared recent-primary and recent-factor policy. Read `AGENTS.md`,
`docs/design/second-factor-authentication.md`,
`docs/design/passkey-second-factor-contract.md`, ADRs 0014, 0015, 0025, 0027,
and 0048, and the verified storage report before editing.

## Owned paths

- Modify `apps/server/internal/auth/session.go`.
- Modify `apps/server/internal/auth/session_test.go`.
- Modify `apps/server/internal/auth/session_adversarial_test.go`.
- Modify `apps/server/internal/auth/context.go`.
- Modify `apps/server/internal/auth/session_cookie.go`.
- Modify `apps/server/internal/auth/session_cookie_test.go`.
- Create `apps/server/internal/auth/pending_authentication.go`.
- Create `apps/server/internal/auth/pending_authentication_test.go`.
- Create `apps/server/internal/auth/second_factor_policy.go`.
- Create `apps/server/internal/auth/second_factor_policy_test.go`.

Do not edit password or provider handlers, factor cryptography, OAuth, mail,
OpenAPI, web files, migrations, or infrastructure.

## Required behavior

- Session issue, authentication, rotation, and replacement copy and compare the
  user's current authentication epoch while holding the accepted locks.
- Ordinary age-based rotation inherits the accepted primary and factor proof
  timestamps. Epoch-changing deliberate replacement writes the exact accepted
  timestamps for the completed action.
- A stale epoch authenticates nothing and cannot rotate.
- Pending login and reauthentication use the accepted digest-only cookie,
  separate CSRF secret, purpose, return path, account epoch, expiry, attempt
  count, and optional current-session binding.
- A pending credential authorizes only the pending second-factor routes.
- Creating a sixth live pending row expires the oldest under the user lock.
- Each admitted pending creation first makes the accepted best-effort bounded
  cleanup calls. Fixed cleanup warnings do not fail the request or readiness.
- Success, `auth_required`, and fifth-attempt exhaustion clear the pending
  cookie according to the accepted response contract. Other closed failures keep
  the cookie and pending row available for a valid retry.
- Recent sensitive-action checks require primary and factor timestamps for an
  enrolled account, but keep existing unenrolled behavior.
- Epoch change replaces the deliberate current session with a fresh non-lineage
  session and revokes every other browser session.

## Test-first cycle and checks

Start with `git status --short`. Add failing unit and live-database race tests
for stale epochs, pending isolation, sixth-row expiry, wrong binding, concurrent
completion, exhaustion, and session replacement. Do not run local tests, builds,
lint, installs, database writes, browsers, or development stacks. Report these
checks as unrun pending GitHub CI on the exact candidate:

```bash
(cd apps/server && go test -count=1 ./internal/auth)
(cd apps/server && golangci-lint run ./internal/auth/...)
make server-build server-vet server-test
```

Definition of done: session and pending primitives enforce the accepted epoch,
cookie, CSRF, expiry, attempt, binding, and lock rules without integrating login
routes yet. Report exact files, checks and results, skipped checks with command
and reason, race evidence, and open items. Do not perform Git operations. Use
short plain text with no em dash.
