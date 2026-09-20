# Primary authentication integration brief

Role: backend. Model: `gpt-5.6-terra`.

## Objective and authority

Connect password and provider primary authentication, reauthentication, and
auth-owned sensitive actions to the verified pending and factor service. Read
`AGENTS.md`, `docs/design/second-factor-authentication.md`,
`docs/design/passkey-second-factor-contract.md`, ADRs 0014, 0025, 0027, 0039,
and 0048, and the verified authority and passkey reports before editing.

## Owned paths

- Modify `apps/server/internal/auth/handlers.go`.
- Modify `apps/server/internal/auth/handlers_test.go`.
- Modify `apps/server/internal/auth/password_login.go`.
- Modify `apps/server/internal/auth/password_handlers.go`.
- Modify `apps/server/internal/auth/password_service.go`.
- Modify `apps/server/internal/auth/password_service_test.go`.
- Modify `apps/server/internal/auth/password_account.go`.
- Modify `apps/server/internal/auth/password_adversarial_test.go`.
- Modify `apps/server/internal/auth/password_race_test.go`.
- Modify `apps/server/internal/auth/provider_identity.go`.
- Modify `apps/server/internal/auth/provider_identity_race_test.go`.
- Modify `apps/server/internal/auth/google.go`.
- Modify `apps/server/internal/auth/google_test.go`.
- Modify `apps/server/internal/auth/google_adversarial_test.go`.
- Modify `apps/server/internal/auth/github.go`.
- Modify `apps/server/internal/auth/github_test.go`.
- Modify `apps/server/internal/auth/github_adversarial_test.go`.
- Modify `apps/server/internal/auth/linkedin.go`.
- Modify `apps/server/internal/auth/linkedin_test.go`.
- Modify `apps/server/internal/auth/linkedin_adversarial_test.go`.
- Modify `apps/server/internal/auth/provider_email_access_test.go`.
- Modify `apps/server/internal/auth/start.go`.
- Modify `apps/server/internal/auth/start_test.go`.
- Modify `apps/server/internal/auth/link.go`.
- Modify `apps/server/internal/auth/link_test.go`.
- Modify `apps/server/internal/auth/link_adversarial_test.go`.
- Modify `apps/server/internal/auth/identity_unlink.go`.
- Modify `apps/server/internal/auth/identity_unlink_test.go`.
- Modify `apps/server/internal/auth/sessions_handlers.go`.
- Modify `apps/server/internal/auth/sessions_handlers_test.go`.

Do not edit session primitives, factor service files, OAuth, account API, mail,
OpenAPI, web files, migrations, or infrastructure.

## Required behavior

- Password and every enabled provider issue the existing full session for an
  unenrolled account and the accepted pending login for an enrolled account.
- Neither pending path writes or returns a session cookie.
- Password and provider reauthentication bind pending purpose `reauth` to the
  current live session and do not update verification timestamps before factor
  completion.
- Password reset revokes sessions but preserves factor policy, credentials, and
  recovery codes.
- Password add or change, provider link or unlink, session revoke, and logout
  everywhere enforce current epoch and both recent proofs when enrolled.
- Provider reauthentication creates the accepted session-bound pending row and
  does not update primary proof until factor completion. Provider linking
  rechecks the concrete current session, epoch, and both proofs under the
  accepted mutation locks before it changes identity state.
- Login and reauthentication preserve closed errors, return-path validation,
  exact Origin, CSRF, rate limits, and provider enablement.
- Concurrent password, provider, factor, and session mutations follow the
  accepted lock order and cannot issue stale authority.

## Test-first cycle and checks

Start with `git status --short`. Add failing password and provider matrices for
unenrolled and enrolled accounts, stale epoch, callback replay, reset bypass,
reauth binding, and concurrent factor changes. Do not run local tests, builds,
lint, installs, database writes, browsers, or development stacks. Report these
checks as unrun pending GitHub CI on the exact candidate:

```bash
(cd apps/server && go test -count=1 ./internal/auth)
(cd apps/server && golangci-lint run ./internal/auth/...)
make server-build server-vet server-test
```

Definition of done: every primary and auth-owned sensitive path respects the
pending and epoch boundary with unchanged unenrolled behavior. Report exact
files and hunks, checks and results, skipped checks with command and reason,
race evidence, and open items. Do not perform Git operations. Use short plain
text with no em dash.
