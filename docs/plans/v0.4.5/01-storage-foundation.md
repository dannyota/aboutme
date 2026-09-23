# TOTP storage foundation brief

Role: backend. Model: Sonnet (Codex: `gpt-5.6-terra`).

## Objective and authority

Add the additive TOTP credential and enrollment storage contract plus generated
sqlc access. Read `AGENTS.md`, the accepted TOTP contract and key-management
design, accepted ADR 0049, the data and budget designs, ADR 0038, migration
00004, and the v0.4.2 storage code and tests on main:
`apps/server/sql/queries.sql` (second-factor queries),
`apps/server/internal/store/second_factor_store_test.go`, and
`apps/server/migrations/second_factor_passkeys_test.go`.

## Owned paths

- Create `apps/server/migrations/00005_totp_second_factor.sql`.
- Create `apps/server/migrations/second_factor_totp_test.go`.
- Modify `apps/server/migrations/migrations_test.go` and
  `apps/server/migrations/status_test.go` only if they pin the migration count
  or latest version.
- Create `apps/server/sql/second_factor_totp.sql` for every TOTP query. Edit
  `apps/server/sql/queries.sql` only when an existing v0.4.2 query must change
  (for example the policy row gaining `attempt_mail_at`); report each hunk.
- Regenerate `apps/server/internal/store/models.go`,
  `apps/server/internal/store/querier.go`, the new
  `apps/server/internal/store/second_factor_totp.sql.go`, and
  `apps/server/internal/store/queries.sql.go` if it changes, with
  `cd apps/server && sqlc generate` (sqlc 1.31.1 from `.tool-versions`).
- Create `apps/server/internal/store/totp_store_test.go`.

Do not edit cryptography, factor services, mail, config, commands, OpenAPI, web,
infrastructure, or design. Never hand-edit generated Go.

## Required behavior

- Add exactly one credential and one enrollment row per account (plain unique
  `totp_enrollments.user_id`, no `consumed_at`) with the accepted UUID, digest,
  session, epoch, issuer, key, nonce, ciphertext, step, timestamp, format,
  check, index, and cascade rules. Every enrollment exit (completion,
  supersession, removal, disabled completion, expiry cleanup) deletes the row.
- Provide user, session, credential, and enrollment lock queries in the shared
  order: user, current session when present, policy, credential, enrollment,
  then pending authentication when applicable. Provide atomic supersession
  (delete any existing enrollment row, expired or not, then insert), claim by
  delete, step compare-and-advance, install, replace, removal, and bounded
  cleanup queries.
- Accept application-generated UUIDv7 IDs on credential and enrollment insert.
- Add `failed_attempts` from 0 through 1,000 and nullable `cooldown_until` to
  `totp_credentials`, with atomic cool-down check, increment, and reset queries.
  Add nullable `second_factor_policies.attempt_mail_at` with a conditional claim
  query for the one-per-hour mail cap. Change no existing row value.
- Check the 26-byte `tk1_` key-ID shape. Provide the bounded three-ID key query,
  bounded selection of rows off the active key, compare-before-update
  re-encryption, and bounded count queries.
- Provide one active-factor count query per factor type for the shared count.
- Re-encryption selection locks credentials, then enrollments, 200 rows per
  batch in `id` order with `FOR UPDATE SKIP LOCKED`, and deletes expired
  enrollments without decrypting (key-management design).
- Prove account and session cascades, one-enrollment-row enforcement,
  supersession, concurrent step advance, replacement and removal races, and
  every database bound.
- Grant `aboutme_app` only required privileges. Keep the migration additive and
  forward-only with no data deletion.
- Replace both `auth_email_jobs_kind_check` and `auth_email_jobs_scope_check`.
  Permit `totp_added`, `totp_replaced`, and `totp_removed` with required user
  scope, no registration or reset scope, and the existing token-digest rule.
  Down first raises an exception while any `totp_credentials` row exists, then
  deletes only those job kinds, restores the exact v0.4.2 constraints, and drops
  both tables and `attempt_mail_at`.
- Test migration up and down for all existing and TOTP mail kinds and scopes.
  Keep mail insertion inside the lifecycle factor transaction.

## Hosted checks and report

Write failing migration and live-store tests first. Do not run local tests,
installs, database writes, browsers, or stacks. Locally you may run only
`sqlc generate`, `go build ./...`, `go vet` and `golangci-lint run` over
`./internal/store/... ./migrations/...` under `apps/server`, one at a time, each
wrapped exactly as:

```bash
flock -o "$(git rev-parse --path-format=absolute --git-common-dir)/aboutme-local-check.lock" \
  timeout 600 systemd-run --user --scope -p MemoryMax=2G -p MemorySwapMax=0 \
  -p CPUQuota=200% <command>
```

Report the rest as unrun pending exact-candidate GitHub CI:

```bash
make sqlc-check
make server-test-db server-test-integration server-migration-test
make server-build server-vet server-test
(cd apps/server && golangci-lint run ./internal/store/... ./migrations/...)
```

Definition of done: migration, mail constraints, generated access, locks,
bounds, indexes, cascades, cleanup, and rotation queries match the accepted
contract. Report exact files, generated diffs, checks and results, skipped
commands and reason, race evidence, and open items. Do not perform Git
operations. Use short plain text with no em dash. Code, tests, comments, and
living docs never cite plans, tasks, phases, or review findings; cite the
design, ADR, or `AC-*` ID instead.
