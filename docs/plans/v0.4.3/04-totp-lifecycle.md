# TOTP lifecycle brief

Role: backend. Model: Sonnet (Codex: `gpt-5.6-terra`).

## Objective and authority

Implement enrollment, replacement, pending verification, removal, recovery
composition, epoch effects, and key re-encryption on the verified v0.4.2 factor
service. Read `AGENTS.md`, the accepted TOTP contract and key-management design,
accepted ADRs 0048 and 0049, the budgets, and the verified storage,
cryptography, and mail reports.

## Owned paths

- Create `apps/server/internal/secondfactor/totp_service.go` and
  `apps/server/internal/secondfactor/totp_service_test.go`.
- Create `apps/server/internal/secondfactor/totp_rotation.go` and
  `apps/server/internal/secondfactor/totp_rotation_test.go`.
- Create `apps/server/internal/secondfactor/totp_health.go` and
  `apps/server/internal/secondfactor/totp_health_test.go`.
- Modify `apps/server/internal/secondfactor/service.go`,
  `apps/server/internal/secondfactor/service_test.go`,
  `apps/server/internal/secondfactor/recovery.go`, and
  `apps/server/internal/secondfactor/recovery_test.go`.
- Modify `apps/server/internal/auth/pending_authentication.go` and
  `apps/server/internal/auth/pending_authentication_test.go` only if the
  `WithLivePending` `beforePending` callback cannot hold the policy and
  credential locks; report each hunk.
- Modify `apps/server/internal/auth/second_factor_handlers.go`,
  `apps/server/internal/auth/second_factor_handlers_test.go`, and
  `apps/server/internal/auth/second_factor_adversarial_test.go`.
- Modify `apps/server/internal/auth/password_reset.go` and
  `apps/server/internal/auth/password_service_test.go` only to clear the TOTP
  cool-down on reset completion.
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
- Enforce the per-account failure budget on the credential row: the cool-down
  check before decryption, `429` with bounded `Retry-After`, the doubling
  cool-down every fifth consecutive failure, and reset on success, replacement,
  or removal. Prove passkey and recovery completion stay available during a TOTP
  cool-down.
- Apply the one-per-hour `attempt_mail_at` cap under the policy lock to every
  `second_factor_attempts_exhausted` job, including passkey and recovery
  exhaustion and TOTP cool-down start. The shared exhaustion hook is
  `failAttempt` in `recovery.go`, run through `auth.FailPendingVerification`.
  Lock the policy row (and TOTP credential) in the `WithLivePending`
  `beforePending` callback, before the pending row, in the canonical order. A
  suppressed mail keeps the state change.
- Clear `cooldown_until` and `failed_attempts` in the password-reset completion
  transaction. Prove a reset ends an active cool-down.
- Register the TOTP counter with the shared active-factor count in `service.go`.
  TOTP removal and passkey removal decide final versus non-final only from that
  count. Do not change passkey handler functions or passkey routes. Prove final
  passkey removal with TOTP active keeps the policy, recovery codes, factor
  proof, and `passkey_removed` mail, and prove the reverse for TOTP removal.
- Return `404 factor_not_found` for TOTP verification on an account with no
  active TOTP credential, with no failure counted.
- Generate each credential and enrollment UUIDv7 with the injected generator
  before sealing. On completion, open the enrollment ciphertext and seal again
  under the active key with a fresh nonce and the credential binding. Never copy
  enrollment ciphertext.
- Preserve password reset, provider, passkey, recovery, sensitive action,
  session, grant, and authorization-code behavior.
- Lazily re-encrypt successful previous-key verification. Implement bounded
  proactive re-encryption and zero-use count without secret output.
- Use one lock order everywhere: user, current session when present, policy,
  credential, enrollment, then pending authentication when applicable.
- Fail closed per row on an unknown key ID or authenticated-decryption failure
  with `503 authentication_unavailable` and no failure counted. Emit the fixed
  `totp_unavailable` log with its closed reason at most once a minute per
  reason. Run the bounded three-ID query at startup and every five minutes
  without decrypting. Nothing here touches `/readyz`.
- Prove start, completion, replacement, removal, same-step verification,
  recovery, session rotation, account deletion, mail rollback, and epoch races
  have one valid winner. Prove handle collisions and passkey reuse, per-row key
  failure, signal rate, transactional mail, canonical lock order, and export
  absence.

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
plain text with no em dash. Code, tests, comments, and living docs never cite
plans, tasks, phases, or review findings; cite the design, ADR, or `AC-*` ID
instead.
