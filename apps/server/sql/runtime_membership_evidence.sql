-- name: RuntimeFinishServingGracefulLeave :one
WITH result AS MATERIALIZED (
 SELECT public.runtime_finish_serving_graceful_leave(sqlc.arg(replica_id)::uuid,sqlc.arg(instance_id)::text,sqlc.arg(release_digest)::text,sqlc.arg(expected_controller_generation)::bigint,sqlc.arg(operation_id)::text) AS r
)
SELECT (r).replica_id::uuid AS replica_id,(r).state::text AS state,
 (r).receipt_operation_id::text AS receipt_operation_id,
 (r).controller_generation::bigint AS controller_generation,
 (r).joined_transition_count::integer AS joined_transition_count,
 (r).joined_claim_count::integer AS joined_claim_count,
 (r).recorded_at::timestamptz AS recorded_at,(r).replayed::boolean AS replayed FROM result;

-- name: RuntimeFinishMaintenanceGracefulLeave :one
WITH result AS MATERIALIZED (
 SELECT public.runtime_finish_maintenance_graceful_leave(sqlc.arg(replica_id)::uuid,sqlc.arg(instance_id)::text,sqlc.arg(release_digest)::text,sqlc.arg(expected_controller_generation)::bigint,sqlc.arg(operation_id)::text) AS r
)
SELECT (r).replica_id::uuid AS replica_id,(r).state::text AS state,
 (r).receipt_operation_id::text AS receipt_operation_id,
 (r).controller_generation::bigint AS controller_generation,
 (r).joined_transition_count::integer AS joined_transition_count,
 (r).joined_claim_count::integer AS joined_claim_count,
 (r).recorded_at::timestamptz AS recorded_at,(r).replayed::boolean AS replayed FROM result;

-- name: RuntimeRecordEC2Termination :one
WITH result AS MATERIALIZED (
 SELECT public.runtime_record_ec2_termination(sqlc.arg(replica_id)::uuid,sqlc.arg(instance_id)::text,sqlc.arg(release_digest)::text,sqlc.arg(request_id)::text,sqlc.arg(evidence_id)::text,sqlc.arg(requested_at)::timestamptz,sqlc.arg(observed_terminated_at)::timestamptz,sqlc.arg(observed_state)::text) AS r
)
SELECT (r).replica_id::uuid AS replica_id,(r).state::text AS state,
 (r).evidence_id::text AS evidence_id,
 (r).reclaimed_claim_count::integer AS reclaimed_claim_count,
 (r).recorded_at::timestamptz AS recorded_at,(r).replayed::boolean AS replayed FROM result;
