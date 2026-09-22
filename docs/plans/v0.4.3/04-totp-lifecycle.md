# TOTP lifecycle brief

Role: backend. Model: `gpt-5.6-terra`.

## Objective and authority

Implement enrollment, replacement, pending verification, removal, recovery
composition, epoch effects, and key re-encryption on the verified v0.4.2 factor
service. Read `AGENTS.md`, the accepted TOTP contract, accepted ADRs 0048 and
0049, the budgets, and the verified storage, cryptography, and mail reports.

## Owned paths

- Create `apps/server/internal/secondfactor/totp_service.go` and
  `apps/server/internal/secondfactor/totp_service_test.go`.
- Create `apps/server/internal/secondfactor/totp_rotation.go` and
  `apps/server/internal/secondfactor/totp_rotation_test.go`.
- Create `apps/server/internal/secondfactor/totp_readiness.go` and
  `apps/server/internal/secondfactor/totp_readiness_test.go`.
- Modify `apps/server/internal/secondfactor/service.go` and
  `apps/server/internal/secondfactor/service_test.go`.
- Modify `apps/server/internal/auth/second_factor_handlers.go`,
  `apps/server/internal/auth/second_factor_handlers_test.go`, and
  `apps/server/internal/auth/second_factor_adversarial_test.go`.
- Modify `apps/server/internal/accountapi/export_test.go` and
  `apps/server/internal/accountapi/delete_test.go` only for TOTP absence and
  cascade cases.

Do not edit config, composition, OpenAPI, generated clients, login providers,
OAuth, mail files, SQL, migrations, web, infrastructure, or design.

## Required behavior

- Start one encrypted, token-digest-only, session and epoch-bound enrollment.
  Supersede an earlier enrollment without changing active authority.
- Complete only a matching live enrollment and valid code. Install or replace
  atomically, store the proof step, create recovery codes only for the first
  factor, advance the epoch, revoke stale authority, rotate the current session,
  and enqueue the exact mail.
- When no policy exists, generate a random unique 32-byte WebAuthn user handle.
  Retry a unique collision twice, return 503 after three candidates with no
  mutation, persist the winner, and make later passkey enrollment reuse it.
- Complete pending login or reauthentication with one greater unused step in the
  same transaction as pending consumption and session issue or update.
- Count invalid and replayed codes in the shared pending attempt budget. Do not
  count dependency, decryption, or clock failures.
- Remove TOTP while preserving passkeys and recovery, or perform accepted final
  disablement. Consume live setup and handle the credential used for recent
  proof.
- Preserve password reset, provider, passkey, recovery, sensitive action,
  session, grant, and authorization-code behavior.
- Lazily re-encrypt successful previous-key verification. Implement bounded
  proactive re-encryption and zero-use count without secret output.
- Use one lock order everywhere: user, current session when present, policy,
  credential, enrollment, then pending authentication when applicable.
- Latch the first authenticated-decryption failure by record kind and internal
  row ID. Validate only that row once per readiness probe. Clear the latch after
  successful validation or confirmed deletion, and let startup rebuild health
  from the bounded key-ID check.
- Prove start, completion, replacement, removal, same-step verification,
  recovery, session rotation, account deletion, mail rollback, and epoch races
  have one valid winner. Prove handle collisions and passkey reuse, latch set
  and clear, transactional mail, canonical lock order, and export absence.

## Hosted checks and report

Write failing service, HTTP, and live-database race tests first. Do not run
local tests, builds, lint, installs, database writes, browsers, or stacks.
Report these as unrun pending exact-candidate GitHub CI:

```bash
(cd apps/server && go test -count=1 ./internal/secondfactor ./internal/auth ./internal/accountapi)
(cd apps/server && golangci-lint run ./internal/secondfactor/... ./internal/auth/... ./internal/accountapi/...)
make server-build server-vet server-test
make server-test-db server-test-integration
```

Definition of done: all TOTP lifecycle paths compose with the v0.4.2 authority
boundary and key rotation without changing route registration. Report exact
files and hunks, checks and results, skipped commands and reason, race and
rollback evidence, and open items. Do not perform Git operations. Use short
plain text with no em dash.
