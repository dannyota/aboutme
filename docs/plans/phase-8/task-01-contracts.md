# Task 8.1 — Contracts and lifecycle schema

**Owner:** integration owner. **Acceptance:** AC-PRIV-001–005,
AC-MEDIA-003/006/007. Read the phase index and its named authorities.

**Owned paths:** phase plan, design clarifications, traceability, OpenAPI and
generated client, `apps/server/migrations/00011_privacy_lifecycle.sql`,
migration tests, `apps/server/sqlc.yaml`, generated store files, and shared
queries needed by the task authors.

## Contract

Add terminal/lease/overdue fields to the existing exact-key media queue.
Completed jobs remain for 180 days so existing ambiguous-commit recovery can
still prove their existence. They are excluded from due work.

Add a closed lifecycle audit table with UUID event ID, fixed event kind,
occurrence time, and optional media-job UUID. It has no account identity, email,
document, raw object key, request body, or credential fields. Retain audit
records for 180 days. Account deletion's audit event is transactional.

Persist the orphan sweep cursor independently of accounts. Limit it to the
backend's canonical cursor bound. A dry run never changes the cursor or queue.

Keep the existing cascade and tombstone rules. Add indexes for bounded oldest
session redaction, audit expiry, pending queue claims, and completed-job expiry.
SQL query files under `sql/` feed the same generated store package.

- [ ] Write migration tests for constraints, terminal-state retention, queue
      survival across account deletion, and a fresh/prior-head migration.
- [ ] Observe the tests fail on the prior schema.
- [ ] Add the migration and regenerate; never edit generated files directly.
- [ ] Set exact HTTP schemas, examples, response headers and error vocabulary.
- [ ] Run `make server-migration-test api-check`; inspect generated diffs and
      run `make sqlc-check` after committing the source and generated result.

**Done:** authors can use named schema and HTTP contracts without guessing. The
owner serializes subsequent query generation.
