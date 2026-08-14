# Phase 4 Mutation Contract

Status: **Approved — ready for implementation planning.**

## Purpose

This contract defines the editor's local commands, one-second autosave queue,
idempotent attempts, accepted-state adoption, stale reconciliation, explicit
conflicts, and the shared rules used by grouped children.

It uses the state terms defined in [the editor design](design.md#state-model).
Endpoint bodies and errors remain governed by the current OpenAPI.

## Command capture

A user action creates an immutable command from optimistic `current`
**immediately before** applying that action locally. The command captures:

- its exact canonical target;
- the target's base value or exact absence;
- its intended target value or operation;
- its base-state non-target context;
- its intended-state non-target context;
- IDs of earlier local commands whose optimistic effects it read; and
- a stable local sequence number.

The store then applies the command to `current`. It never waits until dispatch
to capture or replace the comparison base.

A later command can read optimistic effects from an in-flight or pending local
command. It cannot dispatch until those named dependencies are acknowledged. A
dependency's acknowledged server value may advance the later command's captured
target or non-target context when that value represents the same earlier local
effect. No remote or unrelated response may advance it. Server sanitizing and
canonicalization of an acknowledged local dependency are valid advances.

At dispatch, the coordinator materializes the endpoint's full payload from
accepted state plus this command's intent. Materialization never overwrites its
target base, intended target, or non-target context.

## Target identity

Only adjacent unsent value-setting commands with the same canonical target and
reducer may coalesce. Coalescing retains the first command's target base,
base-state non-target context, dependencies, and sequence position. It takes the
last intended target and derives intended-state non-target context from that
last intent. In-flight, create, upload, and destructive commands never coalesce.

| Edit                             | Canonical target                       |
| -------------------------------- | -------------------------------------- |
| Resume title                     | `metadata:title`                       |
| Resume language                  | `metadata:lng`                         |
| Full name                        | `personal:fullName`                    |
| Headline                         | `personal:headline`                    |
| Contact list                     | `personal:details`                     |
| Existing entry field             | `entry:<sectionKey>:<entryId>:<field>` |
| New, deleted, or whole entry     | `entry:<sectionKey>:<entryId>`         |
| Section display name             | `section:<sectionKey>:displayName`     |
| Section icon                     | `section:<sectionKey>:iconKey`         |
| Entry order                      | `section:<sectionKey>:entryOrder`      |
| Customization leaf               | `customization:<allowlistedPath>`      |
| Photo crop                       | `photo:crop`                           |
| Photo upload, replace, or delete | `photo:object`                         |
| Resume delete                    | `resume:<id>`                          |

Every structure action gets a unique `structure:<sequence>` target and never
coalesces. A template apply is one non-coalescible group governed by the
[template group contract](template-group-contract.md).

## Target and context projections

Target state is never a prerequisite. Each command has a base target, intended
target, base-state non-target context, and intended-state non-target context.
The context protects identity or assumptions outside the value the command owns.
Matching the base-state non-target context is a prerequisite for dispatch,
retry, or rebase; it is never part of target equality.

| Command                    | Target transition                                                                          | Base-state non-target context and intended-state non-target context |
| -------------------------- | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------- |
| Metadata or personal field | Exact field or list: base value/absence → intended value/absence                           | Same resume ID and current schema version                           |
| Existing entry field       | Exact field: base → intended                                                               | Same parent section key/type and entry ID membership                |
| Whole entry replace        | Complete entry: base → intended                                                            | Same parent section key/type and membership                         |
| New entry                  | Document-wide entry binding: absent → exact section key and complete entry                 | Same parent section key/type                                        |
| Entry delete               | Exact section membership plus complete entry: base → absent                                | Same parent section key/type                                        |
| Entry reorder              | Exact ordered entry-ID array: base → intended order                                        | Same parent section key/type                                        |
| Section name or icon       | Exact metadata field: base/absence → intended value/absence                                | Same section key/type                                               |
| Customization leaf         | Exact allowlisted path: base/absence → intended value/absence                              | Same resume ID and current schema version                           |
| Photo crop                 | Crop value/absence: base → intended                                                        | Same exact photo key in both contexts                               |
| Photo replacement          | Complete photo reference: base/absence → opaque server-returned reference with crop absent | Same resume ID                                                      |
| Photo delete               | Complete photo reference: base → absent                                                    | Same resume ID                                                      |
| Resume delete              | Complete loaded resume and metadata: base → absent owner route                             | Same authenticated owner identity                                   |

A structure command list owns one structural target projection. Its base and
intended projections contain complete placement plus the complete values and
presence of every created or deleted section. A section created or deleted by
the list remains target state, not context. Move and reorder targets contain the
complete base and intended placement. Their base-state non-target context and
intended-state non-target context contain the exact key and `sectionType` of
every pre-existing section moved or reordered, plus the key/type projection of
every untouched section. The ordered command reducer derives both projections
without reading unordered map order.

Create resume has no addressable base target, and photo replacement has no
predictable intended target. Their same-key success responses are the only way
to bind those opaque results.

Structural value equality distinguishes absence, null, `""`, and array order.
Rich text compares the accepted sanitized string. Numeric crop values compare as
JSON numbers without tolerance.

Every reconciliation uses this order:

1. Target equals intended and non-target context equals intended-state
   non-target context: the command is satisfied.
2. Otherwise, target equals base and non-target context equals base-state
   non-target context: retry or rebase is safe.
3. Otherwise, the command conflicts.

## Queue and debounce

Each resume has one queue. Different resumes may save independently. All writes
within one resume are serialized, including metadata, document, structure,
photo, and template-group children.

The debounce timer resets after each local edit to that resume. After one second
of inactivity, the head command may dispatch when:

- no request is in flight;
- all named local dependencies are acknowledged;
- session and CSRF state are available; and
- captured target and non-target context have been advanced only by permitted
  acknowledged local dependencies.

The coordinator then applies the shared comparison order. Intended target plus
intended-state non-target context is satisfied without a write. Base target plus
base-state non-target context may dispatch. Any other combination conflicts
before network I/O.

Later commands remain local while the head is unknown, failed, partial, or
conflicted. They are never reported as saved.

## Attempt construction

An attempt freezes:

- method, registered operation, and concrete path targets;
- resolved current schema version;
- exact `If-Match` revision, if required;
- exact JSON body, zero-length body, or file bytes;
- UUID idempotency key; and
- first-dispatch time and retry cutoff.

Normal existing-resume attempts derive `If-Match` from validated
`acceptedRevision`. Create carries no precondition. The coordinator never
regenerates a key for a retry of the same frozen attempt.

Whole-entry and whole-personal-details endpoints remain safe because the
coordinator applies only the command intent to accepted state, then serializes
the resulting complete entry or client-owned personal-details object. It never
serializes stale optimistic sibling values.

## Accepted-state adoption

Accepted revision never decreases for any response. An idempotent replay may
return a stored success older than a complete owner read already adopted. That
replay closes the old attempt but does not replace or transform newer state. The
coordinator retains or fetches the newer winner and resolves the command against
it. If the winner no longer equals the accepted intent, it shows that the
accepted edit was later superseded instead of resubmitting it automatically.

### Complete JSON success

A `200` or `201` resume response must contain a current-version document,
decimal body revision, and response ETag for the same revision. After validation
the store adopts its document, revision, and summary metadata, marks metadata
fresh, removes the command, and replays later commands.

Create adopts the returned server ID only from a validated `201`. It never
guesses an ID from list contents.

### Bodyless child success

Entry delete and photo delete return `204`, the new parent ETag, and emitted
schema version, but no complete resume body. The store:

1. validates the success ETag and schema header;
2. applies the command's acknowledged transformation to `acceptedDocument`;
3. adopts the revision from that success ETag;
4. preserves known summary fields but marks `updatedAt` unknown; and
5. schedules a complete owner read after the queue drains.

This state is acknowledged but is not called a complete server response. Later
commands may use its document and revision. They do not use a fabricated
timestamp.

Whole-resume `204` clears that resume store and returns to the list. An unknown
delete outcome follows the unknown-outcome rules before the store is cleared.

## CSRF and closed responses

On `csrf_rejected`, the coordinator refreshes `/api/v1/me` and retries once.
Only the CSRF token changes. Method, path, body, schema version, precondition,
and idempotency key stay byte-for-value identical. A second rejection is final.

Session loss follows [the design session-loss flow](design.md#session-loss). The
queue remains mounted and stopped.

`409 idempotency_key_reuse` is an invariant failure. The coordinator stops the
queue and performs a complete owner read. It does not generate another key for
the rejected bytes.

Validation and bounds errors remain attached to their command. Rate limits and
`media_busy` honor `Retry-After` and require explicit retry. A server `5xx` may
retry only as the same frozen attempt and under the cutoff below.

## Unknown transport outcomes

The idempotency record expires after 24 hours. Each attempt sets a conservative
resolution cutoff 23 hours after first dispatch. Tests inject the clock. A clock
jump, restored suspended tab, or other uncertainty that could put the attempt
beyond that cutoff stops automatic retry.

Before the cutoff, the coordinator performs at most one automatic exact replay
after a bounded delay. It uses the same key, precondition, and bytes. An
explicit Retry may repeat that exact attempt before the cutoff, but there is no
background retry loop.

For an existing-resume command, a complete owner read supplies the observed
winner. The coordinator applies the shared target/context order:

1. **Winner target equals intended and winner context equals intended-state
   non-target context:** the command is satisfied. Adopt the winner and remove
   the command. This correctly closes a successful delete whose old target no
   longer exists.
2. **Winner target equals base and winner context equals base-state non-target
   context:** reapplying is safe. While the old outcome is unresolved, reapply
   means the exact same-key attempt. Once an exact replay returns a definitive
   `412`, the normal stale path may create a new attempt at the validated
   winning revision.
3. **Any other target/context combination:** preserve the intent and create a
   conflict.

The read does not by itself prove that an earlier server handler has stopped.
Therefore a base match never authorizes a second idempotency key while the old
attempt remains unresolved.

Opaque-result operations need stronger proof:

- A replacement upload's intended photo reference is not predictable. A changed
  photo target is not treated as intended; only a successful same-key replay can
  bind and adopt the returned reference. An unchanged base photo with matching
  resume context remains safe for same-key replay.
- Create has no prior ID or unique client marker. Only its same-key `201` closes
  the outcome.

At or after the cutoff, no unresolved attempt is retried automatically and its
old key is not reused. The editor performs a read and shows the three-way state
for an explicit decision. An unresolved create is **never** retried
automatically after the cutoff; the list is refreshed and the owner decides
whether to create again. An opaque photo upload also remains unresolved until
the owner explicitly replaces or keeps the observed photo.

## `412` reconciliation

A valid `412 revision_mismatch` must contain:

- the exact error code;
- `details.revision` matching `^[1-9][0-9]*$` and no greater than signed 64-bit
  maximum; and
- `details.document` as a current-version document.

The next `If-Match` is derived from validated `details.revision`. A `412` does
not claim or require a response ETag. Any malformed or inconsistent detail stops
reconciliation and triggers a complete owner read.

Metadata commands use a complete owner read because `details.document` has no
title or language. Document and photo commands may use validated details as the
winner. Before rebase or satisfaction, the store adopts that winning document
and revision without decreasing accepted revision; summary metadata is then
marked stale. A details revision older than already accepted state requires a
complete owner read instead.

Reconciliation is three-way:

1. Winner target equals intended and winner context equals intended-state
   non-target context: command is satisfied.
2. Winner target equals base and winner context equals base-state non-target
   context: apply intent to the winner, assign a new key, and retry once against
   the derived winning revision.
3. Any other target/context combination: create a visible conflict.

A rebased request has a new precondition or body and always a new idempotency
key. A second `412` stops automatic rebase even when the target still equals its
base.

Unrelated winning values are retained. The target/context table is the single
safe-rebase authority. In particular, customization compares its path as the
target, crop compares crop as the target and photo key as context, entry delete
compares its complete entry/membership target and parent identity as context,
and structural operations compare their derived structural target separately
from untouched-section context.

## Conflict actions

**Accept latest** drops the local intent and rebuilds `current` from the winner
plus later commands.

**Apply mine** is never a blind replay. On selection, the editor performs a
complete owner read immediately before creating the replacement command. It may
override the target only when current non-target context can form valid
base-state non-target context and the operation can derive its intended-state
non-target context. The latest target becomes the new command base; the intended
target is recomputed, and a new key is generated. A race after that read remains
guarded by the new request's `If-Match`.

If current state cannot produce both projections, generic Apply mine is absent.
The [editor conflict controls](editor-contract.md#conflict-controls) own the
explicit recreate, reorder, photo, and destructive-confirmation paths. Template
partial recovery remains in the template group contract.

## Acceptance scenarios

1. Capture and coalescing preserve the first target/context base; only an
   acknowledged local dependency advances it.
2. One resume has at most one in-flight mutation, and every retry preserves its
   frozen key and bytes.
3. Complete responses, child `204`, and whole-resume `204` adopt distinct,
   monotonic state shapes.
4. Unknown outcomes and `412` use intended target/context, then base
   target/context, then conflict. Create and opaque upload require same-key
   success, and no retry crosses 23 hours.
5. `412` uses validated `details.revision`; a second `412` stops.
6. Apply mine enforces current context, explicit recreate/reorder paths,
   photo-key binding, and destructive reconfirmation.
