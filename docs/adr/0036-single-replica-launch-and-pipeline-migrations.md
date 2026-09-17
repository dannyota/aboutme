# 0036: Single-replica launch and pipeline migrations

Status: Accepted (2026-09-08), by the human owner's direction. Superseded in
part by [ADR 0038](0038-single-baseline-and-plain-migrator.md).

## Context

[ADR 0034](0034-scheduled-uat-and-production-autoscaling.md) set a production
target of one to two automatically scaled replicas.
[ADR 0035](0035-replica-coordination-and-uat-lifecycle.md) then specified the
coordination a second replica requires, because the publication coordinator,
render authority and admission limits were process-local and a second copy could
bypass revocation or multiply a limit.

That coordination was implemented as migrations 13 through 23: the write barrier
and protected migrator, membership, transition, claim and rate schemas, then
registration, claim, rate and lifecycle operations and membership evidence. Each
landed with a fresh review and local proof. That work is sound and is not in
question here.

What changed is the estimate of what the first release needs. The remaining work
is larger than the part completed: exclusive wake and protected wake migration,
the public transition operations, live entry, caller integration for
publication, render, realtime, and rate-limit callers, then composition and
readiness, and only then a local multi-replica proof. Almost all of it exists to
make a second replica safe.

The product has no users yet. A second replica buys capacity that is not needed,
plus survival of a node failure and deploys without downtime. The second and
third are worth having, but not at the cost of blocking a launch.

Two mechanisms deserve separate attention because they are the most expensive
and the least necessary. Applying migrations from inside a running fleet
requires a recorded wake, a closing write gate that suspends ordinary writers,
and a separate migrator session that can act while that gate is closed. The
standard alternative is to run migrations as a deployment step, before the new
application version starts, where no coordination is required at all.

## Decision

The first release runs one serving replica. The growth path is a larger instance
before a second replica.

Database migrations run as a deployment step, not from a running application
node. The deployment applies pending migrations with the existing protected
migrator, from a task that runs to completion before the service is updated. The
application no longer migrates at startup and no longer needs a path that
migrates while the fleet is awake.

The scheduled UAT stop and start remains, and becomes an operational sequence
rather than a database protocol: start the database, run the migration step,
start the service; reverse it to stop. It no longer requires a recorded wake
operation, an exclusive wake barrier or a closed-gate migrator session.

Cross-replica revocation reverts to the existing in-process coordinator, which
is correct while exactly one replica serves. The shared publication transition
schema stays in the database, unused, so a second replica does not require a
schema change.

Shared rate limits stay in PostgreSQL. They are correct with one replica and
stay correct with several, so nothing is gained by moving them back into process
memory and correctness would be lost later.

Exclusive work continues to use the durable claims already built where its
caller is already integrated. New exclusive work does not require the claim
protocol: `SELECT ... FOR UPDATE SKIP LOCKED` is sufficient for queued work
whose worker may die, because an aborted transaction releases the row without
any fencing step.

Membership registration, lifecycle actions and termination evidence remain
installed. A single replica still registers, and verified termination is still
the only thing that may reclaim its claims.

The single-replica constraint is enforced by deployment configuration, not by
the database. Raising the service maximum above one is a decision that requires
the deferred work in the consequences below to be completed first.

## Consequences

Retired for this release, with their plans deleted and the reasoning kept here:

- Exclusive wake, which reserved migration 24.
- Protected wake migration, which reserved migration 25.

Deferred until a second replica is actually wanted, in this order: the public
transition operations, live entry, caller integration for render, realtime and
policy callers, and composition and readiness. Their contracts remain accepted
and are not withdrawn.

Retained and still required: the write barrier and protected migrator, all four
runtime schemas, registration, claim, rate and lifecycle operations, membership
evidence, and the typed store transports for each. The migration test template
harness stays, since it serves every migration test.

The deployment gains a migration step that must complete before the service
updates, and that step must be the only migrator. The existing advisory lock
already guarantees this if two ever run.

Deploys briefly interrupt service, because one replica cannot hand over. This is
accepted for the first release and is the main thing a second replica would
later buy.

The exit criterion recording that rate candidate selection still evaluates its
predicate under the policy clock remains open and is unaffected by this
decision.

## Compatibility and review

This decision changes deployment sequence and internal coordination scope. It
preserves public HTTP and SSE schemas, mutation ordering, CSRF and session
rules, privacy deadlines, print capability authority, and every database
invariant already proved.

This record supersedes these parts of earlier decisions:

- ADR 0034: the one-to-two replica production target, for the first release
  only. Its cost, region, and topology decisions stand.
- ADR 0035: migration during a recorded wake, the exclusive wake barrier, and
  the requirement that a second replica be proved before launch. Its
  coordination contracts stand and are the basis for any later second replica.

The scaling contract and deployment design record this decision together.
Production activation remains a separate decision requiring owner approval.
