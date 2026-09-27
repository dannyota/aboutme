# 0005: Goose migrations as the schema source, one baseline, deploy-step migrator

Status: Accepted (2026-08-11, 2026-08-12, 2026-09-08, 2026-09-17).

## Context

A declarative `schema.sql` diffed by Atlas into goose migrations gave one
authoring source but two derived artifacts that could disagree. Keeping them
aligned needed a pinned Atlas, a checksum file, a generator wrapper, and a drift
script, because Atlas's PostgreSQL differ silently drops functions, triggers,
and other objects this schema depends on. sqlc parses goose-format migration
files directly, so the migration directory can feed both goose and sqlc.

Migration history also needs an exact point after which files are immutable.
Before the first release, history was disposable; after it, a durable database
depends on the bytes.

Running migrations from inside a running fleet needs a recorded wake, a write
gate, and a separate migrator session. Running them as a deploy step before the
new version starts needs no coordination.

## Decision

**One schema source.** `apps/server/migrations/*.sql` is the single source of
truth for the database schema. goose applies migrations through the embedded
`cmd/migrate` binary, and sqlc reads the same directory to generate the typed
data layer. Migration DDL is hand-written.

- `make sqlc-check` is the drift gate: generated Go against the migrations. It
  uses `git status --porcelain`, so a new untracked generated file is caught.
- `sqlc.yaml` carries an explicit `public.citext` type override, because sqlc
  degrades the schema-qualified spelling to `interface{}` without it.

**One baseline.** `00001_baseline.sql` holds the schema as of the first release,
keeping every table, column order, constraint, index, function, and trigger
name. It grants `aboutme_app` exactly SELECT, INSERT, UPDATE, and DELETE on each
business table. Every later migration grants explicitly, and a catalog test
enforces the grants.

**Immutable after the marker.** `apps/server/migrations/.uat-baseline` marks the
release baseline. Once a comparison base contains it, the marker cannot change
or be removed, every migration on that base is immutable, and schema changes use
new forward migrations. Local and hosted guards compare the two trees directly
and fail closed when a baselined migration or the marker changes, including
across rewritten, non-ancestor history. Rollback never edits old files; it uses
a forward corrective migration and keeps the candidate and prior server
compatible for the rollback window.

**Plain migrator, deploy step.** The migrator is plain goose under a PostgreSQL
session advisory lock. It always runs as `aboutme_migrator`, which owns every
schema object; there are no identity modes, provisioning stage, or history
adoption. Two database roles exist, `aboutme_migrator` and `aboutme_app`. One
idempotent `db-setup` command, run as the database owner, creates them, sets
database and schema grants, and can store their SCRAM verifiers.

Migrations run as a deployment step, from a task that runs to completion before
the service updates. The application does not migrate at startup. That step is
the only migrator; the advisory lock guarantees it if two ever run.

## Consequences

- No diff engine checks that a migration expresses the intended change, so
  review of migration SQL matters more.
- Reading the current schema means reading the migrations in order. If that
  hurts, the remedy is a generated, non-authoritative `pg_dump -s` snapshot, not
  a second authoring source.
- Frozen migrations keep their original comments; the
  [engineering standard](../standards/engineering.md) exempts them from prose
  edits.
- The hosted app connects as `aboutme_app` and can reach its tables, which a
  test proves.
- A second serving replica needs its coordination runtime back as new migrations
  with explicit grants (ADR 0026).

## History

- Former ADR 0010 (2026-08-11): goose-only migrations; removed `schema.sql`,
  Atlas, and the data-drift gate. Its unconditional append-only timing was
  replaced by former ADR 0020.
- Former ADR 0020 (2026-08-12): the `.uat-baseline` marker freezes history. Its
  first marker was reset once by former ADR 0038.
- Former ADR 0036 (2026-09-08), in part: migrations as a deploy step. Its wake
  and protected-migrator machinery was retired.
- Former ADR 0038 (2026-09-17): one baseline, explicit grants, plain goose
  migrator, two roles, and `db-setup`. It removed migrations 00013 to 00023 (the
  replica coordination runtime and staged migrator). The one-time guard
  exemption for the reset and the withdrawn `v0.1.0` tag were one-off steps.
