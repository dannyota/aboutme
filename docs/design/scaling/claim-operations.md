# Fixed claim operations and results

Status: Accepted detail of [shared claims](shared-claims.md) under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). R1 owns
database operations and store transport; R5 owns the private operation object
and admission adapter. Runtime implementation and local proof remain pending.

This contract fixes result rows, error codes, role checks and the Go boundary.
It preserves the stored schema, function names and argument order, lock order,
policy limits, [identity and retry rules](claim-identities.md), and receipts.
The result type and functions belong in a later operation migration; migration
17's schema remains unchanged. No result-only outcome is persisted.

## SQL result

Create owner-owned `public.runtime_claim_result` and revoke PUBLIC type usage.
Its attributes have this exact order. A stored result means running, waiting or
released; its outcome equals its stored state.

| Attribute                  | Type        | Presence and value                                                   |
| -------------------------- | ----------- | -------------------------------------------------------------------- |
| outcome                    | text        | Always running, waiting, released, denied or absent                  |
| claim_id                   | uuid        | Always nonnull and non-nil                                           |
| policy_id                  | text        | Exact policy; null only for absent                                   |
| replica_id                 | uuid        | Non-nil; null only for absent                                        |
| work_id                    | uuid        | Non-nil exactly for non-absent C01, including denied                 |
| state                      | text        | Stored state; null for denied/absent                                 |
| admitted_at                | timestamptz | Present exactly for stored results                                   |
| deadline_at                | timestamptz | Present exactly for stored C01; original admitted_at plus 20 seconds |
| released_at                | timestamptz | Present exactly for released                                         |
| release_reason             | text        | Present exactly for released: joined, canceled, expired or fenced    |
| request_digest             | bytea       | 32 bytes; null only for absent                                       |
| scope_count                | smallint    | One or two; null only for absent                                     |
| scope_1_kind               | text        | First accepted kind; null only for absent                            |
| scope_1_digest             | bytea       | 32 bytes; null only for absent                                       |
| scope_1_allocation_ordinal | bigint      | Positive for stored results; null for denied/absent                  |
| scope_2_kind               | text        | Present exactly for a non-absent two-scope request                   |
| scope_2_digest             | bytea       | 32 bytes exactly when scope_2_kind is present                        |
| scope_2_allocation_ordinal | bigint      | Positive for stored two-scope results; otherwise null                |

Composite attributes cannot carry CHECK/NOT NULL constraints. Every definer and
the store decoder validate the complete matrix, policy/work shape, ordered
scopes, digest lengths and state consistency. C05 always has IP first and only
an optional account second. SQL recomputes request identity from exact inputs
and stored children; acquire never accepts a supplied request digest.

- Running, waiting and released are exact durable results. Only running may
  start work; waiting retains only the accepted local queue authority. Released
  replay never reacquires capacity.
- Denied means acquire found no atomic capacity. It returns validated input
  identity, computed digest and requested scopes, without state, timestamps,
  release data or allocation ordinals. It leaves no parent, child, summary
  charge or partial C05 reservation. A definitive denial consumes the private
  operation's first acquisition; it grants no same-UUID retry.
- Absent means resolve/promote/release found no parent. Only outcome=absent and
  claim_id are nonnull; all other attributes are null. It grants no work and
  echoes no unverified identity as fact. Only confirmed absence after an
  ambiguous acquire can enter the existing one-time, under-five-minute retry
  path while the original context and deadlines remain live.
- Resolve never returns denied; acquire never returns absent. A found but
  mismatched or corrupt row never becomes absent or denied.
- Promotion blocked by capacity or queue order returns the unchanged waiting
  row. Only C01/C02 may promote. An expired C01 waiter may atomically become
  released/expired; no timestamp alone reclaims running work.

## Fixed signatures

Use p_ parameter names to avoid input/output name collisions. All five claim
functions return exactly one `public.runtime_claim_result`:

```sql
public.runtime_acquire_single_claim(
  p_claim_id uuid, p_policy_id text, p_replica_id uuid, p_work_id uuid,
  p_scope_kind text, p_scope_digest bytea
) RETURNS public.runtime_claim_result

public.runtime_acquire_sse_claim(
  p_claim_id uuid, p_replica_id uuid, p_ip_digest bytea,
  p_account_digest bytea
) RETURNS public.runtime_claim_result

public.runtime_promote_claim(
  p_claim_id uuid, p_expected_replica_id uuid, p_request_digest bytea
) RETURNS public.runtime_claim_result

public.runtime_release_claim(
  p_claim_id uuid, p_expected_replica_id uuid, p_request_digest bytea,
  p_reason text
) RETURNS public.runtime_claim_result

public.runtime_resolve_claim(
  p_claim_id uuid, p_expected_replica_id uuid, p_request_digest bytea
) RETURNS public.runtime_claim_result

public.runtime_gc_released_claim_receipts()
RETURNS TABLE(deleted_claim_count integer,
              deleted_scope_summary_count integer)
```

GC always returns one row, including (0,0). Counts are nonnegative and count
parents and summaries actually deleted. Its fixed 24-hour/256-parent selection,
canonical locks, child-before-parent deletion and unreferenced-zero-summary rule
remain those in shared claims. No caller chooses a cutoff, page or policy.

Acquire preserves capacity, replica, parent, catalog and bytewise-summary lock
order. Exact replay returns current durable claim state after identity checks.
Promote/release preserve their applicable lock order. Release remains available
during drain, updates all scopes and counts atomically, and requires exact
terminal-reason replay. The proof definer alone invokes owner-only fenced
cleanup; that helper takes no reason argument and hardcodes fenced.

## Roles and errors

Check the invoking direct login through `session_user`, never definer
`current_user`, before reading mutable state. App and maintenance have distinct
login roles, with no role-membership or SET ROLE fallback. Real-role tests use
the existing direct-login/SET SESSION AUTHORIZATION harness.

| Function                   | App EXECUTE | Maintenance EXECUTE       |
| -------------------------- | ----------- | ------------------------- |
| acquire_single_claim       | yes         | yes, C03 only             |
| acquire_sse_claim          | yes         | no                        |
| promote_claim              | yes         | no                        |
| release_claim              | yes         | yes, maintenance C03 only |
| resolve_claim              | yes         | yes, maintenance C03 only |
| gc_released_claim_receipts | no          | yes                       |

Each name has the runtime_ prefix. Maintenance calls to SSE acquire or promote
fail with 42501 at the function ACL. C03 has no waiting state and no accepted
maintenance flow needs either function. Shared reachable functions still apply
the in-function role, policy and replica-kind checks below.

- App acquires C01-C05 only against an active serving replica.
- Maintenance acquires only C03 mail.send against an active maintenance replica.
- For every found resolve/promote/release row, check stored replica kind and
  policy against the invoking role before returning or mutating it. App cannot
  operate on maintenance claims; maintenance operates only on maintenance C03.
  Maintenance receives no promotion privilege because C03 has no waiting state.
- Expected replica and digest remain separate request-consistency checks. Shared
  app credentials do not authenticate an individual process. R5/R8 bind that
  private identity.
- PUBLIC, lifecycle-command and fencing-proof receive no execution on the five
  claim functions. Maintenance alone executes GC. No login executes the fenced
  helper or directly mutates these tables.

Every definer is runtime_owner-owned, fixes search_path=pg_catalog and uses
qualified static SQL. Return fixed messages without supplied or stored identity,
ARN, digest or evidence in message, detail, hint, schema, table or column.

| SQLSTATE | Meaning                                                                                                                                                  |
| -------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| AM001    | Marker, catalog, assertion or stored durable-state corruption; existing physical connection retirement applies                                           |
| AM002    | Supplied immutable identity, replay arguments or terminal reason conflict with valid durable identity                                                    |
| 55000    | Valid request unavailable due to closed admission, replica/lifecycle state, inapplicable promotion, missing/out-of-order predecessor or stale generation |
| 22023    | Caller UUID/digest/enum/bounds/work/scope shape is invalid or requests an unsupported policy/kind                                                        |
| 42501    | Wrong invoking role or forbidden fenced release                                                                                                          |

Stored catalog drift is AM001. Native constraints remain backstops; definers
validate first where this taxonomy applies. AM002 is the replay-conflict code
for these claim operations and the newly specified membership/lifecycle
operations. Existing public-transition AM001 mismatch rules remain unchanged.
Other callback errors allow connection reuse only after confirmed rollback; none
adds retry permission.

## Membership and lifecycle replay

[Registration](replica-membership.md) alone permits current-state replay after
activation, leave, termination or fence. Verify the immutable tuple and three
children, then return current replica/capacity state with replayed=true and no
mutation. Mark-ready retains its joining prerequisite; exact replay is
idempotent only while that prerequisite holds.

[Lifecycle replay](lifecycle-replay.md) returns immutable historical step
columns and digest. It never reconstructs that result from current capacity,
replica, partition or write-state rows. Supplied operation/workflow/action or
argument conflicts and reuse of a consumed replacement predecessor are AM002.
Every supplied canonical argument-digest mismatch is AM002; the ledger cannot
reconstruct every original argument to distinguish changed input from a
well-shaped stored digest change. A recomputed stored result-digest mismatch,
independently provable retained-field corruption or impossible ledger row is
AM001. Missing/out-of-order predecessor and stale generation are 55000.

## Query transport and Go ownership

The pinned sqlc 1.31.1 cannot infer these nullable function-result columns. Each
private query uses one materialized function result, then projects fields with
explicit casts. Nullable fields have a concrete fallback value and an adjacent
presence boolean. The presence bit controls nullability; the fallback value
grants no authority. For example:

```sql
WITH result AS MATERIALIZED (
  SELECT public.runtime_promote_claim(
    sqlc.arg(claim_id)::uuid,
    sqlc.arg(expected_replica_id)::uuid,
    sqlc.arg(request_digest)::bytea
  ) AS r
)
SELECT
  (result.r).outcome::text AS outcome,
  COALESCE((result.r).released_at,
    '1970-01-01 00:00:00+00'::timestamptz)::timestamptz AS released_at_value,
  ((result.r).released_at IS NOT NULL)::boolean AS released_at_present
FROM result;
```

The example shows the transport pattern; production queries project and validate
every result field. Nullable inputs use sqlc.narg. Do not expand
`(function_call).*`, which can repeat a volatile call, or rely on `r.*`
generation. No view, domain, broad override or generated-file edit is needed.

R1's internal/store owns generated scalar rows and a narrow
RuntimeClaimTransport API with scalar inputs and decoded RuntimeClaimRow values.
It exposes no ClaimOperation, encoder, key/version, retry state, callback or raw
connection. It runs each fixed operation through WriteTxRunner, calls one
generated function, validates the entire presence/value matrix and copies data
before the callback ends. Copy every 32-byte digest into an array; return owned
timestamps and fixed ordered scope slots. No driver buffer, row handle, Queries
or transaction escapes.

Only successful runner completion exposes a decoded row. Every runner error,
including finish failure or commit ambiguity after decoding, returns the zero
RuntimeClaimRow plus error. Running, waiting, denied, absent and released are
never returned alongside an error. No retry occurs in this transport.

R5 imports internal/store, owns ClaimOperation and its private claimStore
interface, converts operation identity to scalar inputs, and translates a
committed row into domain types. It checks every expected field and owns the
monotonic retry lifetime. Internal/store never imports R5. GC stays on the
separate maintenance scheduler store; the fenced helper has no Go entry point.

An isolated PostgreSQL 18.4/generated-pgx probe verified eight scalar families:
UUID, text, int2, int4, int8, boolean, timestamptz and bytea. Presence
distinguishes NULL from present zero/empty/false. The volatile function ran
once; rollback persisted zero invocations and commit one. This is transport
evidence only.

## Required implementation proof

- Exact result order/types, every presence matrix and invalid field, denied C01
  work identity, stored outcome/state equality, absent shape and digest vector.
  Definitive denial grants no same-UUID retry.
- App/serving and maintenance/maintenance+C03 pairing; forbidden cross-kind
  access, promotion and fenced requests; direct DML/helper denial; fixed errors
  without identity disclosure.
- Atomic C05 denial/release, queue order, expired waiting versus live running,
  drain/acquire, fence/acquire and fence/release races; exact replay/conflict
  and current durable resolution without partial scope authority.
- GC at 24h-minus-epsilon and 24h, 256-parent bound, live-row exclusion, child
  deletion order, retained referenced summaries and zero-result row.
- Entry/function/finish/commit order, rollback on error/panic, physical
  retirement for AM001, null versus zero/empty decoding, one function call,
  stable copied results after connection reuse and zero authority on every
  runner error.
- Registration current-state replay after later states; mark-ready joining
  prerequisite; lifecycle historical replay after later mutations and fixed
  argument/result vectors. AM002 remains scoped; public transitions keep AM001.

These real operation checks have not run. R1 owns migration/store proof, R5 owns
caller operation state, and R8 owns trusted composition and local join.
