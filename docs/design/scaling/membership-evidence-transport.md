# Membership evidence transport

[Membership evidence operations](membership-evidence.md) fix durable rows,
replay, locks and grants. These signatures preserve one fixed database call per
store method and return authority only after successful transaction completion.

## SQL signatures

Use p_ names in definitions to avoid input/output collisions while preserving
the accepted public argument order and types:

```sql
public.runtime_finish_serving_graceful_leave(
  p_replica_id uuid,
  p_instance_id text,
  p_release_digest text,
  p_expected_controller_generation bigint,
  p_operation_id text
) RETURNS public.runtime_leave_result

public.runtime_finish_maintenance_graceful_leave(
  p_replica_id uuid,
  p_instance_id text,
  p_release_digest text,
  p_expected_controller_generation bigint,
  p_operation_id text
) RETURNS public.runtime_leave_result

public.runtime_record_ec2_termination(
  p_replica_id uuid,
  p_instance_id text,
  p_release_digest text,
  p_request_id text,
  p_evidence_id text,
  p_requested_at timestamptz,
  p_observed_terminated_at timestamptz,
  p_observed_state text
) RETURNS public.runtime_fence_result
```

All three are ordinary-write functions. They run inside exactly one
WriteTxRunner transaction. They never enter or finish the barrier themselves. No
function retries internally.

## Go transport

Place the transport in internal/store. It owns WriteTxRunner, generated query
rows, value-plus-presence decoding, and copied scalar result values:

```go
type RuntimeMembershipEvidenceTransport interface {
  FinishServingGracefulLeave(context.Context, uuid.UUID, string, string,
    int64, string) (RuntimeLeaveResult, error)
  FinishMaintenanceGracefulLeave(context.Context, uuid.UUID, string, string,
    int64, string) (RuntimeLeaveResult, error)
  RecordEC2Termination(context.Context, uuid.UUID, string, string, string,
    string, time.Time, time.Time, string) (RuntimeFenceResult, error)
}

func NewRuntimeMembershipEvidenceTransport(
  *pgxpool.Pool,
) RuntimeMembershipEvidenceTransport
```

Arguments follow SQL order after context. There are no nullable inputs in these
three signatures; do not use sqlc.narg for them. Validate all strings, UUIDs,
generations, and times before the transaction. The generated query uses one
MATERIALIZED function result and explicit scalar casts. Every result attribute
is required. Either project a value plus presence boolean for each attribute and
reject any false presence, or use direct cast scans whose NULL scan is an error.
Never COALESCE a missing required reclaimed count, replayed flag, timestamp,
text, UUID, generation, or count into a valid zero/false/empty sentinel. The
decoder validates UUID/nonempty enums, nonnegative counts, positive controller
generation, and nonzero recorded_at. Copy values inside the callback and expose
them only after commit. Return zero RuntimeLeaveResult/RuntimeFenceResult on any
error, including ambiguous commit.

Server composition owns exact local identity, local join proof, lifecycle
selection inputs, external EC2 DescribeInstances evidence, and the single
same-evidence ambiguity retry. Store transport exposes no callback, Queries,
transaction, connection, raw proof row, AWS client, fencing helper, transition
recovery, or retry loop.

RuntimeLeaveResult uses `ReplicaID uuid.UUID`; `State` and `ReceiptOperationID`
as `string`; `ControllerGeneration int64`; `JoinedTransitionCount` and
`JoinedClaimCount` as `int32`; `RecordedAt time.Time`; and `Replayed bool`.
RuntimeFenceResult uses `ReplicaID uuid.UUID`; `State` and `EvidenceID` as
`string`; `ReclaimedClaimCount int32`; `RecordedAt time.Time`; and
`Replayed bool`.
