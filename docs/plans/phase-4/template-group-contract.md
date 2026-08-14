# Phase 4 Template Group Contract

Status: Implementation design pending review.

## Purpose

Template application changes placement through the structure endpoint and the
rest of customization through the customization endpoint. Those requests cannot
share one database transaction. This contract makes them one guarded,
client-visible group without claiming server atomicity.

The [mutation contract](mutation-contract.md) remains the authority for command
capture, target/context comparison, attempts, CSRF, unknown outcomes, `412`,
accepted-state adoption, and conflicts. The
[editor contract](editor-contract.md#template-application) owns controls,
warnings, and the deterministic diff.

## Group input

Apply calls the landed pure helper against optimistic state:

```text
applyTemplate(current.customization, preset, current.content)
```

It captures the complete pre-apply input immediately before showing the result.
The helper result is immutable for that group. Content is never modified.

The group records:

- complete pre-apply customization and placement;
- complete helper-result customization and placement;
- the resume ID and current schema version;
- the complete content key/type projection read by placement; and
- earlier local command IDs whose optimistic effects the helper read.

It waits until every named local dependency is acknowledged. Only the
acknowledged value of that dependency may advance the captured projection. A
remote or unrelated response cannot.

## Target and context

The group target is the complete pair:

```text
{ placement, customization }
```

Its base target is the pre-apply pair. Its intended target is the complete
helper-result pair. Customization paths, including optional absence, are target
state, not prerequisites.

The base-state non-target context and intended-state non-target context are the
same resume ID, current schema version, and complete content key/type
projection. A content-key or section-type change is a context change because it
can alter placement meaning. Entry field changes do not change that context.

The shared comparison order applies to the complete group:

1. Target equals the helper result and context equals intended-state non-target
   context: group is satisfied.
2. Target equals the pre-apply pair and context equals base-state non-target
   context: group may start.
3. Any other combination conflicts before the first request.

## Child expansion

The editor produces at most two children:

1. one structure request that reaches the helper-result placement; then
2. one customization request that reaches the helper-result customization.

A child is omitted when its owned target already equals its intended value. If
both are omitted, Apply reports **No changes** and creates no group or undo
record.

Children are adjacent and non-coalescible. Later local commands wait behind the
whole group. Each child is an ordinary frozen attempt under the mutation
contract and has its own idempotency key and precondition.

Each child also separates target from context:

| Child         | Target transition                                | Base-state non-target context and intended-state non-target context |
| ------------- | ------------------------------------------------ | ------------------------------------------------------------------- |
| Structure     | Base placement → helper-result placement         | Base customization plus captured content key/type projection        |
| Customization | Base customization → helper-result customization | Helper-result placement plus captured content key/type projection   |

When placement needs no request, its base already equals the helper result and
is valid customization-child context. Customization remains structure-child
context rather than its target; placement remains customization-child context
rather than its target.

## Expected intermediate state

Before the first request, expected state equals the base target and base-state
non-target context. After an accepted structure child, expected placement
advances to its intended placement while expected customization remains at base.
After an accepted customization child, expected customization advances to the
helper result.

Only an accepted child from this group may advance its owned part of expected
target. This is an acknowledged local dependency advance, not recapture.

After every complete child response, the coordinator first checks the complete
group target and intended-state non-target context. If they equal the helper
result and captured non-target context, the group proceeds directly to final
completion and omits any remaining child. Otherwise it checks:

- the acknowledged child-owned target equals that child's intent;
- every not-yet-written target still equals its expected intermediate value;
- complete content key/type context still equals captured non-target context;
- response revision is newer than the previously adopted group revision.

Unexpected canonicalization, remote target change, missing or retyped section,
changed content projection, malformed response, or revision regression stops the
group as partial. No next child is admitted.

## Unknown and stale children

An unknown child outcome uses the mutation contract's same-key resolution and
23-hour cutoff. Intended child target plus intended-state non-target context is
satisfied. Base child target plus base-state non-target context is safe for
same-key resolution. Any other combination conflicts.

A valid `412` uses `details.revision` under the mutation contract. Safe rebase
is allowed only when the child base target and base-state non-target context
still match. A second `412` stops. The group remains partial while any child
outcome is unresolved.

An opaque result does not exist in template children: placement and
customization intent are both fully known before dispatch.

## Final completion

The group becomes `saved` only when one adopted final revision contains:

- placement exactly equal to helper-result placement;
- complete customization exactly equal to helper-result customization; and
- content key/type context exactly equal to captured intended-state non-target
  context.

The comparison occurs against one complete response or complete owner read. It
does not combine placement from one revision with customization from another. A
child success or optimistic preview never reports the template saved.

Only this final state creates the template undo record. The record contains the
complete pre-apply target, complete helper-result target, final revision, and
content context.

## Partial recovery

A partial group shows accepted target, remaining intent, and the failed or
changed context. It offers exactly:

- **Retry remaining** after a complete owner read, only when target equals the
  expected intermediate target and the new attempt's base-state non-target
  context equals captured non-target context;
- **Restore pre-apply** as a new reverse group, only when the latest
  helper-owned target still equals the expected intermediate target and context
  still matches; or
- **Keep partial**, which explicitly drops remaining intent and creates no
  template undo record.

Retry and restore create attempts under the mutation contract. Neither action
may treat a changed target as a prerequisite or overwrite it silently.

## Undo

Undo is available only for the latest fully accepted template group. It creates
a guarded reverse group whose base target is the complete helper result and
whose intended target is the complete pre-apply pair. Its base-state non-target
context and intended-state non-target context are the recorded content key/type
projection.

Any later change to placement, any customization path, or content key/type
context invalidates Undo. Unrelated entry field edits do not invalidate it. Undo
is not general history or server rollback.

The reverse group is complete only when one adopted revision equals the full
pre-apply target and recorded context. A partial reverse uses the same recovery
states as an apply group.

## Acceptance scenarios

1. Group base/intended target is separate from its base-state non-target context
   and intended-state non-target context.
2. Earlier acknowledged local dependencies are the only allowed capture
   advances.
3. No child starts from a remote-changed target or context.
4. Structure success alone leaves the group partial, not saved or undoable.
5. Every child response rechecks written target, untouched target, context, and
   monotonic revision.
6. Unknown and stale children use central retry and `412` rules without a
   group-specific bypass.
7. Final saved state comes from one revision containing the complete helper
   result and context.
8. Retry, restore, keep-partial, and undo never overwrite a changed target
   silently.
