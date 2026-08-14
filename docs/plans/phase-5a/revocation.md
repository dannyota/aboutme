# Phase 5A generation transition and revocation protocol

Status: **Approved — ready for implementation planning.**

This document owns every in-process resume/discovery generation transition,
public lease, lock order, drain, database outcome, and recovery rule. The
[core design](design.md) owns domain state. The
[public contract](public-contract.md) owns wire responses and validators.

## Safety property

Every public admission presents the revision read from PostgreSQL to the
per-resume fence. The fence admits it only when that expected revision is the
open generation. Every revision-changing mutation closes new admission before
its database transaction and opens only the transaction's definite outcome.

An ordinary mutation does not cancel readers already admitted under an older
revision. They may finish. A revoking publish-state change or deletion cancels
and drains every active older Go origin reader before its transaction begins.
Thus:

- no public request first admitted after a successful revision change can use an
  older generation;
- no public request first admitted after successful unpublish, rename, route
  disable, or delete can receive the revoked representation; and
- mutation success never depends on edge invalidation or cache expiry.

The fence covers Go's origin admission and response connection. A CDN viewer
delivery whose origin validation completed before close is already admitted and
may finish after mutation success; bytes already delivered cannot be retracted.
Any first origin validation or revalidation after success uses only new state.

The v1 fence is in-process. PostgreSQL owns durable revision and discovery
generation. Scaling Go past one task requires a distributed-fence ADR.

## Fence and lease model

A resume fence tracks:

- current state: `open(revision)`, `closing(revision)`, `closed(revision)`, or
  `retired`;
- active lease sets for current and older non-drained revisions;
- one transition owner and its expected revision; and
- cancellation and completion for each lease.

On first mutation or admission, a missing in-memory fence initializes from the
exact database revision read for that operation. It never guesses revision 1.

A non-draining commit may open revision N+1 while an admitted revision N lease
finishes. The old set remains tracked until its last release. A later revoking
transition cancels and drains every active set, not only the current revision.

The global discovery fence has the same model keyed by durable
`discovery_generation`. Its leases cover the complete sitemap or `llms.txt`
origin response.

Each lease owns its generation, representation, request context, active object
read or Nuxt call, viewer response connection, and idempotent release. Canceling
it must interrupt blocked I/O, rendering, cache fill, and slow or aborted viewer
bodies. A timer without cancel-and-join is not a drain.

## Public admission

Resume admission is exact:

1. Read the row by requested slug and check current slug, `live`, route flag,
   photo reference when applicable, and revision.
2. Call `Acquire(resumeID, expectedRevision, representation)`.
3. `open(expectedRevision)` grants a lease. `closed`, `closing`, or `retired`
   returns route-family `503` without bytes.
4. An open revision mismatch releases without admission, rereads PostgreSQL, and
   retries once from step 1.
5. A second mismatch returns route-family `503`, records transition contention,
   and sends no validator or body.

The gate precedes cache selection, `If-None-Match` parsing, object access, and
Nuxt. A read that saw old state but arrives after close cannot enter. A read
admitted just before close owns a tracked old lease.

Discovery admission reads the durable generation and one eligible-slug snapshot
in the same database transaction, then acquires exactly that expected global
generation. A mismatch rereads and retries once. It holds the lease through the
complete aggregate response.

## Transition classes

### Non-draining resume transition

All revision-changing mutations use this base transition:

1. Wait for any prior transition owner, subject to request cancellation.
2. Claim transition ownership without closing admission. In a short transaction
   at the normal lock-order position, serialize the user and probe idempotency
   again. An exact record replays and a reused key conflicts; both release
   transition ownership without closing admission.
3. Require the fence's open revision to equal the mutation's preflight revision.
   A mismatch returns through the normal fresh-row `412` path.
4. Change current state to `closed(expectedRevision)`. New public admissions
   return `503`; existing leases remain active and uncanceled.
5. Run the mutation transaction.
6. Definite commit opens the returned new revision. Definite rollback reopens
   expected revision. Ambiguity stays closed for recovery.

Mutation contenders wait for the transition owner rather than returning a new
write error. After wake, the second idempotency probe wins first; otherwise an
old precondition becomes the existing `412 revision_mismatch`. Public readers do
not wait.

### Revoking resume transition

Use the base close plus cancellation and drain when a fresh mutation would:

- change one non-null stored slug to another;
- change `live` from true to false;
- change `seo_geo_enabled` from true to false;
- change `download_enabled` from true to false; or
- delete a slug-bearing resume.

The transition closes admission, cancels every active lease generation for that
resume, and waits for all handlers to exit. Enabling a representation, initial
publish, discovery opt-in, download opt-in, repeated-value publish, and ordinary
content changes use the non-draining transition.

### Discovery transition

Any transaction that changes slug, `live`, or `seo_geo_enabled`, or deletes a
slug-bearing resume, advances durable discovery generation as defined by the
core design. It closes the global generation around the database transaction.

Every such generation change cancels and drains all active global generations,
including a private-state change whose eligible URL set remains byte-identical.
Content, language, owner title, photo, crop, and download-only mutations do not
take the global fence because they do not advance discovery generation.

## Existing mutation integration

The transition coordinator wraps the transaction boundary of every existing P2B
path that increments revision:

| Mutation surface                                      | Resume transition                             |
| ----------------------------------------------------- | --------------------------------------------- |
| `PATCH /resumes/{id}` metadata                        | Non-draining                                  |
| Entry upsert and delete                               | Non-draining                                  |
| Section display/order metadata                        | Non-draining                                  |
| Atomic structure create/delete/move/reorder           | Non-draining                                  |
| Personal-details replacement                          | Non-draining                                  |
| Customization deltas                                  | Non-draining                                  |
| Photo upload/replace, crop, and delete                | Non-draining                                  |
| `POST /resumes/{id}/publish`                          | Non-draining or revoking from resolved state  |
| `DELETE /resumes/{id}`                                | Revoking when slug-bearing; retires on commit |
| Any internal complete-document CAS used by those APIs | Same class as its registered operation        |

`POST /resumes` has no prior revision or public generation. Definite commit
initializes a private `open(1)` fence lazily; no transition is needed. The
system backfill does not change revision and does not transition.

This integration is mandatory even when an edited field is owner-only. Public
generation is the resume revision, so metadata mutation still closes admission
around commit and opens the returned revision.

Photo decode, normalization, and proved object creation happen before closing
the resume fence. Only the short database mutation is inside the transition.
Existing compensation and ambiguous-object rules remain. Public photo reads
already admitted before a non-draining replace/delete may finish; new admission
after commit checks the new reference and revision.

## Cheap preflight before cancellation

No transition closes or cancels a lease until cheap rejection work is complete.
In order, fresh execution performs:

1. route-rate, session, CSRF/Origin, singleton-header, path, body, schema
   version, idempotency-key, and precondition syntax checks;
2. exact idempotency replay or reuse detection;
3. owner-scoped row read and preliminary revision check;
4. body bounds, projection, command application, sanitizer, document validation,
   and publish completeness when applicable;
5. recent reauthentication for fresh slug release;
6. dedicated slug-attempt admission before availability detail;
7. slug grammar, public-root reservation, current claim, and tombstone
   preflight; and
8. transition classification and required fence set.

Photo processing retains its existing permit and storage ordering before step 8.
Preflight is advisory under concurrency. It prevents needless cancellation but
grants no write authority.

After close/drain, the database transaction rechecks the authenticated session,
owner authorization, CAS, current document, applied command,
sanitizer/validation, publish completeness, recent reauthentication, publish
flags, current photo key, slug locks, claims, tombstone age, and durable
discovery generation. A changed result rolls back and reopens the old
generation. Rate admission, CSRF/Origin evidence, and pure syntax are not
repeated inside PostgreSQL.

The final transaction also rechecks the idempotency record under the existing
user serialization. A newly visible exact result ends without mutation, reopens
the old generation, and replays it. Reuse still returns `409`. Preflight
releases that serialization before taking any transition ownership; the
transaction reacquires it only at the lock-order position below.

## Lock order

When required, acquire in this order:

1. global discovery transition ownership;
2. resume transition ownership in ascending resume UUID byte order;
3. PostgreSQL transaction;
4. existing per-user idempotency serialization and record recheck;
5. per-slug transaction advisory locks in bytewise slug order; and
6. public-state, resume rows in ascending UUID order, session/reauth, tombstone,
   claim, and media-job rows in the query's fixed order.

No code acquires a global or earlier UUID fence while holding a later one. The
same order is reserved for later account deletion across up to three resumes.
Advisory-lock hash collisions only add serialization.

## Shared five-second revocation bound

One wall-clock deadline covers cancellation and join of every global and resume
lease set in one mutation. It is not five seconds per fence or generation.

If any handler remains at the deadline:

- no database transaction has begun;
- all transition ownership is released;
- unchanged resume and discovery generations reopen;
- canceled old handlers stay canceled and tracked until release;
- fresh reads may again enter unchanged public state; and
- the mutation returns `503 public_state_busy` with `Retry-After: 1`.

The request never reports success and never proceeds after a drain timeout.

## Transaction outcomes

After successful close or drain, the transaction rechecks all state, performs
the registered mutation, advances revisions/generation, and writes the
idempotency result atomically.

- A begin failure, validation/CAS rejection, statement failure, explicit
  rollback, cancellation before commit, or definite commit failure reopens the
  old committed generations.
- A definite commit opens only the returned resume revision and committed
  discovery generation. Delete retires the resume fence.
- Request cancellation after commit starts does not classify the outcome.
- An ambiguous commit leaves every affected fence closed.

No cache fill or edge invalidation occurs inside the transaction. Post-commit
invalidation is defense in depth and cannot change the fence result.

## Ambiguous outcome recovery

Under closed fences, use a new database connection to read the exact retained
idempotency result, resume row, tombstone, media job when applicable, and
discovery generation.

- Exact result plus intended row and generation proves commit; open the proved
  generations and replay the stored result.
- No result plus unchanged old row and generation proves non-commit; reopen old
  generations and return a safe failure.
- Delete is committed only when the row is absent and the exact tombstone, media
  job when a photo existed, bodyless result, and discovery generation agree.
- Any mixed, unavailable, or unverifiable state remains closed and makes Go
  unready.

Recovery may repeat the read but never reruns the mutation. Photo ambiguity
retains the candidate under ADR 0019. Startup eagerly reconstructs the global
discovery fence from the exact `public_state` generation and completes any
unresolved outcome recovery before readiness. It does not scan the resume table
or prebuild per-resume fences. Those fences initialize lazily from the exact
database revision read by the first admission or mutation. Process restart is
safe because old in-process sockets, leases, and transition owners die; the
committed database state is then definitive.

## Resume delete

An exact stored DELETE replay is returned before a now-stale reauth check, as
defined in the public contract. Fresh execution preflights current owner,
revision, reauth, slug, photo key, and transition sets.

A slug-bearing delete uses a revoking resume transition. It also uses a global
transition because deletion advances discovery generation. It cancels and drains
every active global generation under the shared five-second deadline, even when
the deleted private slug was absent from eligible bytes. A never-slugged delete
still uses a non-draining resume transition and retires it at commit.

The transaction rechecks everything, inserts the 180-day tombstone, enqueues the
exact current photo key, advances discovery generation when required, deletes
under CAS, and stores exact `204`. Rollback reopens the row and old generations.
Ambiguous recovery applies the stricter delete proof above. No physical object
delete runs before commit.

## Readiness and failure behavior

Go is unready while durable public state is missing or invalid, any outcome is
unresolved, a transition invariant is broken, PostgreSQL is unavailable, or the
bounded direct-Nuxt readiness check fails. `/healthz` remains independent.

A closed transition produces route-family `503`, never stale bytes. Nuxt failure
aborts HTML. Object-store failure aborts photo. Viewer abort releases its lease.
A Go process loss closes its origin sockets, so a replacement cannot report
mutation success while an old response from that process continues.

Metrics include open/closed state, active leases by generation and
representation, transition duration, drain cancellation and timeout, retry
mismatch, ambiguous-recovery age, and readiness cause. Logs remain content- and
key-free.

## Acceptance and race matrix

| #   | Scenario and required result                                                                                                                   |
| --- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | Every metadata, entry, section, structure, personal-details, customization, photo, publish, and delete CAS closes new admission around commit. |
| 2   | A stale expected revision retries one database read; a second mismatch or closed fence returns `503` without cache or bytes.                   |
| 3   | An ordinary edit commits N+1 while an admitted N reader finishes; no new reader enters N after close.                                          |
| 4   | A later unpublish cancels and drains current and retired active sets left by non-draining edits.                                               |
| 5   | Unpublish, rename, discovery disable, download disable, and delete cancel every affected resume representation before transaction.             |
| 6   | Every discovery-generation change drains all active global generations, even when a private change leaves eligible URLs byte-identical.        |
| 7   | Preflight auth, CAS, completeness, reauth, reserved slug, current claim, and tombstone failures cancel no lease and write nothing.             |
| 8   | A race after preflight fails the transactional recheck and reopens the exact old generations.                                                  |
| 9   | One five-second deadline covers stalled Nuxt, object, discovery, slow-viewer, and aborted-viewer handlers across all affected fences.          |
| 10  | Drain timeout begins no transaction, restores unchanged readability, and leaves no transition owner.                                           |
| 11  | Definite rollback reopens old state; definite commit opens only the returned revision and generation.                                          |
| 12  | Ambiguous commit stays closed until exact idempotency and row state prove one outcome; mixed state fails readiness.                            |
| 13  | Fresh delete requires reauth; exact committed replay after reauth expiry remains byte-identical `204` and runs no fence work.                  |
| 14  | Delete rollback restores row, slug access, and media reference; commit proves tombstone, media job, generation, and replay together.           |
| 15  | Concurrent content writes serialize at transition ownership; the loser receives the existing `412` current document.                           |
| 16  | Concurrent reclaim has one winner; failed claim rolls back expired-tombstone consumption; later release creates a new release time.            |
| 17  | Cross-renames lock slugs bytewise and neither deadlock nor overwrite a current claim or tombstone.                                             |
| 18  | Global-first and ascending-UUID order remains deadlock-free for the later three-resume account-delete caller.                                  |
| 19  | Cache hit and exact conditional match acquire the current lease before returning `304`.                                                        |
| 20  | Restart loads committed generation and cannot validate an older aggregate; invalidation failure cannot restore access.                         |
| 21  | A CDN delivery validated before close may finish; origin validation or revalidation first admitted after success cannot select old bytes.      |
| 22  | A same-key contender behind a committed mutation re-probes under transition ownership and replays or conflicts before stale CAS handling.      |
