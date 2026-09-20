# Passkey storage foundation brief

Role: backend. Model: `gpt-5.6-terra`.

## Objective and authority

Add the additive passkey-release storage contract and generated sqlc access
layer. Read `AGENTS.md`, `docs/design/second-factor-authentication.md`,
`docs/design/passkey-second-factor-contract.md`, `docs/design/data.md`,
`docs/design/budgets.md`, ADR 0038, and ADR 0048 before editing.

## Owned paths

- Create `apps/server/migrations/00004_passkey_second_factor.sql`.
- Create `apps/server/migrations/second_factor_passkeys_test.go`.
- Modify `apps/server/migrations/migrations_test.go`.
- Modify `apps/server/migrations/status_test.go`.
- Modify `apps/server/sql/queries.sql`.
- Modify `apps/server/sql/privacy_retention.sql`.
- Modify generated `apps/server/internal/store/models.go` through sqlc.
- Modify generated `apps/server/internal/store/queries.sql.go` through sqlc.
- Modify generated `apps/server/internal/store/privacy_retention.sql.go` through
  sqlc.
- Modify generated `apps/server/internal/store/account_deletion.sql.go` through
  sqlc only for the session epoch and factor-proof projection added by the
  migration. Do not edit its SQL source.
- Modify generated `apps/server/internal/store/querier.go` through sqlc.
- Create `apps/server/internal/store/second_factor_store_test.go`.
- Modify `apps/server/internal/privacyretention/worker.go`.
- Modify `apps/server/internal/privacyretention/worker_test.go`.

Do not edit auth services, OAuth services, OpenAPI, web files, infrastructure,
or design sources. Never hand-edit generated Go files.

## Required behavior

- Add authentication epochs and second-factor timestamps to the accepted rows.
- Add passkey policy, credential, recovery digest, pending authentication, and
  WebAuthn ceremony relations with accepted bounds and account cascades. Add the
  bounded passkey-counter security-event relation.
- Backfill existing authority at epoch zero without creating a factor policy.
- Delete outstanding short-lived OAuth authorization codes before adding the
  required grant and epoch bindings. Add the accepted insert trigger that fills
  omitted bindings for the old flag-off code issuer during mixed-version
  startup.
- Provide accepted row-lock, atomic claim, bounded cleanup, session rotation,
  grant-family revocation, and deterministic list queries.
- Extend the existing daily privacy sweep to delete counter events older than
  180 days in its existing 1,000-row pages and 10,000-row run ceiling.
- Prove first-enrollment, recovery consumption, ceremony completion, final
  removal, and cleanup races have one winner and no partial state.
- Keep the migration append-only and grant `aboutme_app` only the required
  privileges.

## Test-first cycle and checks

Start with `git status --short`. Add failing migration and live-store tests
before implementation, then implement and regenerate sqlc. Do not run local
tests, builds, lint, installs, database writes, browsers, or development stacks.
Report these checks as unrun pending GitHub CI on the exact candidate:

```bash
make sqlc-check
make server-test-db server-test-integration server-migration-test
make server-build server-vet server-test
(cd apps/server && go test -count=1 ./internal/privacyretention)
(cd apps/server && golangci-lint run ./internal/store/... ./internal/privacyretention/... ./migrations/...)
```

Definition of done: migration, generated access, bounds, locks, cleanup, and
cascades match the accepted contract. Report exact new and changed files,
generated diffs, checks and results, skipped checks with command and reason, and
open items. Do not perform Git operations. Use short plain text with no em dash.
