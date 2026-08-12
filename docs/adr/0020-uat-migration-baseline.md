# 0020 — First UAT freezes migration history

Status: Accepted (2026-08-12)

## Context

[ADR 0010](0010-goose-only-migrations.md) made goose migration files the sole
relational schema source and declared every committed migration append-only.
That rule treated an untested local-development history as if another
environment already depended on its bytes.

No local user acceptance testing (UAT), staging deployment, or production
deployment has occurred. The development databases are disposable and can be
recreated in a coordinated idle window. A defect in migration `00005` showed
that freezing this history before its first UAT made a safe correction require
an extra production-style migration even though no durable environment had
applied the faulty bytes.

The repository needs an exact, reviewable point at which development history
becomes release history. Relying on branch age, commit location, or an
operator's memory would not provide that boundary.

## Decision

Before the first local UAT baseline, migration history is development-only. The
integration owner may correct, replace, reorder, or remove migration files. A
correction remains high-risk work: it needs author tests, independent review,
the migration gates, and coordinated recreation of the shared development
database after every live-database worker is idle.

The first UAT candidate adds `apps/server/migrations/.uat-baseline`. That marker
records the transition. Once a comparison base contains it:

- the marker cannot be changed or removed;
- every migration present on that base is immutable; and
- later schema changes use new forward migrations.

Local and hosted guards validate the comparison commits and compare their trees
directly. They fail closed when a baselined migration or the marker changes,
including across rewritten, non-ancestor history.

This decision supersedes only ADR 0010's unconditional append-only timing and
its statement that every applied development migration is immutable. ADR 0010
continues to control the relational source of truth, goose application, sqlc
input, and hand-written DDL.

## Consequences

The exact migration history remains mutable during pre-UAT development, so a
developer database may no longer match the current files after a correction. The
integration owner must recreate the one shared database in an idle window and
verify both databases plus a real host-port migration test.

The marker must be present in the exact candidate before the first UAT begins.
The successful UAT then exercises the history whose bytes become immutable when
that candidate lands.

After the baseline, rollback never edits old files. It uses a new forward
corrective migration and keeps the candidate and prior server compatible for the
stated rollback window.
