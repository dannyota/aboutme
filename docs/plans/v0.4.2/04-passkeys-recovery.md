# Passkeys and recovery brief

Role: backend. Model: `gpt-5.6-terra`.

## Objective and authority

Implement passkey enrollment, assertion, management, and recovery-code lifecycle
on the verified authority and storage primitives. Read `AGENTS.md`,
`docs/design/second-factor-authentication.md`,
`docs/design/passkey-second-factor-contract.md`, `docs/design/budgets.md`, ADR
0048, and the verified storage, session, and mail reports before editing.

## Owned paths

- Modify `apps/server/go.mod`.
- Modify `apps/server/go.sum`.
- Create `apps/server/internal/secondfactor/webauthn.go`.
- Create `apps/server/internal/secondfactor/webauthn_test.go`.
- Create `apps/server/internal/secondfactor/recovery.go`.
- Create `apps/server/internal/secondfactor/recovery_test.go`.
- Create `apps/server/internal/secondfactor/service.go`.
- Create `apps/server/internal/secondfactor/service_test.go`.
- Create `apps/server/internal/auth/second_factor_handlers.go`.
- Create `apps/server/internal/auth/second_factor_handlers_test.go`.
- Create `apps/server/internal/auth/second_factor_adversarial_test.go`.

Do not edit existing login, provider, OAuth, account, mail, config, composition,
OpenAPI, web, migration, or infrastructure files. Do not implement TOTP.

## Required behavior

- Pin `github.com/go-webauthn/webauthn` v0.18.2. Do not add a second WebAuthn or
  base64 package.
- Start and complete registration with the accepted pre-policy user-handle
  binding. Competing first completions have one winner.
- Verify exact ceremony type, challenge, origin, relying-party hash, user
  presence, user verification, account and credential binding, signature,
  optional user handle, cross-origin state, requested extensions, and counter.
- A non-increasing nonzero counter consumes the ceremony, increments the pending
  failure count, leaves the credential unchanged, and inserts the bounded event
  in one transaction. Event failure rolls every write back and returns the
  accepted unavailable error.
- Enforce accepted credential, key, body, field, transport, count, timeout,
  attempt, and cleanup bounds before cryptographic or database work where the
  contract requires it.
- Store only credential public material and display hints. Transport hints never
  grant authority.
- Each admitted ceremony creation first makes the accepted best-effort bounded
  cleanup calls. Fixed cleanup warnings do not fail the request or readiness.
- Generate accepted recovery codes once, return them only on first enrollment or
  regeneration, store digests only, and consume one matching digest under the
  user lock.
- Enrollment, addition, regeneration, removal, and final disablement advance the
  epoch, revoke other sessions and connected-agent authority, rotate the current
  session, and enqueue the accepted mail in the same transaction.
- Registration options return the accepted disabled response while the flag is
  off. Disabled registration completion atomically consumes its matching
  ceremony, stores no credential, and returns the same uniform 404. State,
  assertion, recovery, removal, and regeneration remain registered.

## Test-first cycle and checks

Start with `git status --short`. Add failing unit, HTTP, and live-database race
tests before implementation. Include a frozen hostile assertion corpus and
counter, replay, wrong-binding, notification rollback, and recovery races. Do
not run local tests, builds, lint, installs, database writes, browsers, or
development stacks. Report these checks as unrun pending GitHub CI on the exact
candidate:

```bash
(cd apps/server && go mod tidy && go mod verify)
(cd apps/server && go test -count=1 ./internal/secondfactor ./internal/auth)
(cd apps/server && golangci-lint run ./internal/secondfactor/... ./internal/auth/...)
make server-build server-vet server-test
```

Definition of done: the passkey and recovery service is complete behind its
handlers and flag, with no TOTP code or route. Report exact files, dependency
version and review source, checks and results, skipped checks with command and
reason, race evidence, and open items. Do not perform Git operations. Use short
plain text with no em dash.
