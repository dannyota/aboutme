# 0010 — Migrations are goose-only; the migration directory is the schema source

Status: Accepted (2026-08-11)

## Context

The project adopted a declarative-schema pattern: `apps/server/sql/schema.sql`
was the single declared source of truth, `atlas migrate diff` generated
goose-format migrations from it, goose applied them, and sqlc generated the
typed data layer from the same `schema.sql`.

That gives one authoring source but two derived artifacts that can disagree, so
it needs a convergence gate. The repository grew one: `make data-drift` and
`scripts/check-data-drift.sh` rebuilt a throwaway database and asked Atlas
whether `schema.sql` and `migrations/` still described the same schema. Keeping
that gate honest required pinning Atlas to v1.2.0, installing it in two CI jobs,
committing `migrations/atlas.sum`, and maintaining `cmd/migrate/gen` — a
generator wrapper carrying a `checkUndiffableObjects` cross-check, because
Atlas's Postgres differ silently drops functions, triggers, procedures, views,
sequences, rules, and policies from a generated migration.

The cost was concentrated in exactly the objects this schema depends on. The
resume cap trigger in migration `00005` had to be hand-written and then
cross-checked against `schema.sql` byte-for-byte after comment and whitespace
normalization, because Atlas could not generate it. `CREATE EXTENSION citext` in
`00001` had the same problem. So the two statements Atlas could not diff needed
bespoke machinery to prove they had not drifted — machinery whose only purpose
was to compensate for having two sources in the first place.

The alternative is to delete one source. sqlc parses goose-format migration
files directly, so `migrations/` can feed both goose and sqlc.

This was verified before adoption, not assumed: pointing sqlc's `schema:` at
`migrations` produces **byte-identical** `db.go`, `models.go`, `querier.go`, and
`queries.sql.go` compared to the committed `internal/store`, given one
configuration addition described below.

## Decision

`apps/server/migrations/*.sql` is the single source of truth for the database
schema. goose applies migrations through the embedded `cmd/migrate` binary, and
sqlc reads the same directory to generate the typed data layer. Migration DDL is
hand-written and append-only.

Removed: `sql/schema.sql`, `cmd/migrate/gen`, `migrations/atlas.sum`,
`scripts/check-data-drift.sh`, the `data-drift` and `migrate-gen` make targets,
and the pinned Atlas CLI from both CI jobs.

`make sqlc-check` becomes the only drift gate, and it checks the one thing that
can still drift — generated Go against the migrations that produced it. It now
uses `git status --porcelain` rather than `git diff --exit-code`, because the
latter does not see a newly generated untracked file.

`sqlc.yaml` gains an explicit `public.citext` type override. Migration `00002`
declares the users email column as `public.citext`, schema-qualified, because
Atlas generated it that way. sqlc maps bare `citext` to `string` by default but
does not recognize the qualified spelling, and silently degrades it to
`interface{}` — which compiles and then fails at the call sites. The override is
required for the byte-identical result above.

This supersedes the `sql/schema.sql` and `make migrate-gen` rows of the design
spec's "Schema management (declarative-schema pattern)" table (§3). The rest of
that table stands: `cmd/migrate` still applies embedded migrations goose-only at
runtime, migration immutability is still enforced by the CI append-only check
rather than by content checksums, and the production migration sequence is
unchanged.

## Consequences

Migration DDL is written by hand. There is no diff engine to catch a migration
that does not express the intended schema change, so review of migration SQL
matters more; migrations remain in the high-risk tier of ADR 0011, which
requires an independently derived test suite and a separate defect review.

Reading the current schema means reading the migration files in order rather
than one declarative file. At five migrations this is cheap. If the count grows
enough for that to hurt, the remedy is a generated non-authoritative
`pg_dump -s` reference snapshot, not a second authoring source.

Deleted: one gate, one pinned external tool, two CI installs, one shell script,
one Go package, one checksum file, and the class of failure where two sources of
truth disagree. Local CI no longer needs Atlas at all, which is part of why
`make ci` can run the full gate on a laptop.

Applied migrations are immutable, so the long comments in `00001` and `00005`
explaining what Atlas could and could not diff stay in the files. They describe
a workflow that no longer exists. `AGENTS.md` records this so a reader does not
try to follow them.

`~/src/banhmi` remains a useful reference for the sqlc, goose, and embedded
`cmd/migrate` parts of the pattern, but no longer for its Atlas half.
