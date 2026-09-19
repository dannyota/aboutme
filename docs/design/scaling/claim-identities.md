# Shared claim identities and ambiguity

Status: Accepted detail of [shared claims](shared-claims.md) under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). The
[scaling index](README.md) records whether it is built.

## Scope digest

Use HMAC-SHA-256 with one nonzero 32-byte deployment admission key. The shared
claim package is the sole encoder. The store receives a 32-byte digest, never a
raw IP or account string. The key never enters PostgreSQL, logs, error messages
or test fixtures.

The input begins with the 30 bytes `aboutme.shared-claim.scope.v1\x00`, then:

1. Version byte 0x01.
2. One-byte policy length, then its exact lowercase ASCII ID, 1..63 bytes.
3. One-byte scope-kind length, then its exact lowercase ASCII kind, 1..15 bytes.
4. Two-byte unsigned big-endian value length, then canonical value bytes.

Kinds are global, user, ip and account. Global has an empty value. User/account
use 16 UUID network-order bytes. IP uses four IPv4 or sixteen IPv6 bytes after
Unmap. Domain, policy and kind separation prevent limits from sharing rows.

Every replica verifies the same private pinned key-version tuple before
readiness. The [rate identity contract](rate-identities.md) binds both admission
and password-email key versions through trusted server composition and
controller evidence, without changing membership or lifecycle SQL. Mixed or
unverifiable versions remain unready.

Rotation closes application admission and activation, joins or exactly fences
all admitted work, and proves zero claim counts plus zero rate/pending debt
through the accepted bounded cleanup. Only then may the deployment replace a key
and recompose every replica. Claim counts alone cannot justify a switch because
the admission key also identifies rate buckets. No old-key fallback or
clock-based live-claim reclamation exists. Server composition owns loading,
readiness and the executable runbook after infrastructure exists.

## Request digest

SQL and Go compute SHA-256 over this exact frame:

1. The 32 bytes `aboutme.shared-claim.request.v1\x00`, then version byte 0x01.
2. Claim UUID as sixteen network-order bytes.
3. One-byte policy length, then exact lowercase ASCII policy ID.
4. Replica UUID as sixteen network-order bytes.
5. Work-presence byte: 0, or 1 followed by sixteen work UUID bytes.
6. Scope-count byte: 1 or 2.
7. For each child in request order: one-byte request ordinal, one-byte kind
   length, ASCII kind, then its 32-byte scope digest.

Allocation ordinals, state and timestamps are not request identity. SQL uses
pg_catalog.sha256, uuid_send and convert_to(...,'UTF8'). No text UUID, JSON,
locale or delimiter-dependent concatenation participates.

The fixed SSE vector uses claim UUID ending in 0001, replica UUID ending in
0002, all other UUID digits zero, no work UUID, 32 zero IP-digest bytes and 32
ff account-digest bytes. Its 165-byte frame hashes to
`0733ec593ac2751902ebac6d8175187ea5c2e695f47264b36b1608a2576cbbac`. Pin that
vector in SQL and Go. Also test mapped IPv4, IPv6, UUID encoding, key changes,
zero-key rejection and policy/kind separation.

## Replica binding

Composition creates the operation factory with its private local replica UUID.
Caller ClaimRequest contains claim ID, policy, work ID and scopes, but no
replica override. The factory retains the replica in private ClaimOperation
state. Resolve, Promote and Release supply that expected replica, and SQL
compares it with the stored parent before returning or changing a claim.

All replicas share the app database role. The expected-replica comparison is
request consistency, not authentication of a process-specific SQL principal. The
factory/adapter binds the trusted process identity; public callers cannot
construct a foreign operation or supply raw claim metadata.

## Operation lifetime

[Claim results](claim-operations.md) distinguish stored state, definitive denial
and confirmed absence. Denial consumes first acquisition; it grants no same-UUID
retry. Only the ambiguous-acquire/confirmed-absence path below can use the
private retry flag. A store error exposes no result authority.

ClaimOperation retains the immutable request, original context, factory
identity, createdAt and a reacquire-used flag privately. It exposes no SQL, pgx,
caller clock, limit override, deadline override or fencing action. The shared
claim package provides Acquire, Resolve, Promote and Release through narrow
domain types.

The factory captures createdAt from its injected process clock immediately
before first Acquire. Production retains time.Now's monotonic reading. Tests
inject elapsed time. Reject zero IDs, missing context/time, negative age,
duplicate first acquisition and copied/foreign operations. Neither createdAt nor
retry permission is input, serialized state or a database field. A restart or
lost operation cannot reconstruct permission.

An ambiguous acquire starts no work. Resolve must prove the exact committed
parent and ordered scopes before the caller may use a waiting/running claim.
Confirmed absence permits one same-UUID reacquisition only while:

- its private retry flag is unused;
- monotonic age is at least zero and strictly less than five minutes;
- the original context and all existing caller deadlines remain live.

Consume retry permission before sending that request. At five minutes or later,
invalid/missing age, cancellation, expired deadline, restart or lost state,
absence fails closed. Automatic retry never substitutes a new UUID. These bounds
never extend the render attempt's original 20 seconds or other caller timeouts.
UUID nonreuse is a caller obligation; deleted receipts cannot enforce it
indefinitely in SQL.

A late positive result starts no work after cancellation or deadline. Use a
bounded independent cleanup context to release only after proving local work
never started or after joining it. Unavailable or contradictory cleanup leaves
the claim charged until exact resolution, graceful joined release or verified
EC2 fencing. Time alone never reclaims a live claim.

Released replay never reacquires capacity. Promote and Release resolve unknown
outcomes by exact UUID, replica and request digest; they never infer success
from counts or replay a different mutation. Test the five-minute boundary,
consumed-before-call flag, late positive result, lost operation and restart.
