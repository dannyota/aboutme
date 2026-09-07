-- name: RuntimePrepareScaleOut :one
WITH result AS MATERIALIZED (
 SELECT public.runtime_prepare_scale_out(sqlc.arg(expected_controller_generation)::bigint,sqlc.arg(operation_id)::text) AS r
)
SELECT (r).desired_replicas::smallint AS desired_replicas,
 (r).active_serving_replicas::smallint AS active_serving_replicas,
 (r).active_maintenance_replicas::smallint AS active_maintenance_replicas,
 (r).partition_1_enabled::boolean AS partition_1_enabled,(r).partition_2_enabled::boolean AS partition_2_enabled,
 (r).capacity_generation::bigint AS capacity_generation,(r).controller_generation::bigint AS controller_generation,
 (r).controller_operation_id::text AS controller_operation_id,(r).admission_enabled::boolean AS admission_enabled,
 (r).lifecycle_phase::text AS lifecycle_phase,
 COALESCE((r).write_gate,''::text)::text AS write_gate_value,((r).write_gate IS NOT NULL)::boolean AS write_gate_present,
 COALESCE((r).write_generation,0::bigint)::bigint AS write_generation_value,((r).write_generation IS NOT NULL)::boolean AS write_generation_present,
 (r).replayed::boolean AS replayed FROM result;

-- name: RuntimeActivateReplicaCapacity :one
WITH result AS MATERIALIZED (
 SELECT public.runtime_activate_replica_capacity(sqlc.arg(expected_controller_generation)::bigint,sqlc.arg(operation_id)::text,sqlc.arg(replica_id)::uuid,sqlc.arg(instance_id)::text,sqlc.arg(container_instance_arn)::text,sqlc.arg(caddy_task_arn)::text,sqlc.arg(go_task_arn)::text,sqlc.arg(nuxt_task_arn)::text,sqlc.arg(release_digest)::text,sqlc.narg(replaced_replica_id)::uuid) AS r
)
SELECT (r).replica_id::uuid AS replica_id,(r).replica_kind::text AS replica_kind,(r).state::text AS state,
 COALESCE((r).desired_replicas,0::smallint)::smallint AS desired_replicas_value,((r).desired_replicas IS NOT NULL)::boolean AS desired_replicas_present,
 COALESCE((r).active_serving_replicas,0::smallint)::smallint AS active_serving_replicas_value,((r).active_serving_replicas IS NOT NULL)::boolean AS active_serving_replicas_present,
 COALESCE((r).active_maintenance_replicas,0::smallint)::smallint AS active_maintenance_replicas_value,((r).active_maintenance_replicas IS NOT NULL)::boolean AS active_maintenance_replicas_present,
 COALESCE((r).partition_1_enabled,false)::boolean AS partition_1_enabled_value,((r).partition_1_enabled IS NOT NULL)::boolean AS partition_1_enabled_present,
 COALESCE((r).partition_2_enabled,false)::boolean AS partition_2_enabled_value,((r).partition_2_enabled IS NOT NULL)::boolean AS partition_2_enabled_present,
 (r).capacity_generation::bigint AS capacity_generation,(r).controller_generation::bigint AS controller_generation,
 (r).controller_operation_id::text AS controller_operation_id,(r).admission_enabled::boolean AS admission_enabled,
 (r).lifecycle_phase::text AS lifecycle_phase,(r).replayed::boolean AS replayed FROM result;

-- name: RuntimePrepareScaleIn :one
WITH result AS MATERIALIZED (
 SELECT public.runtime_prepare_scale_in(sqlc.arg(expected_controller_generation)::bigint,sqlc.arg(operation_id)::text,sqlc.arg(replica_id)::uuid,sqlc.arg(instance_id)::text,sqlc.arg(release_digest)::text) AS r
)
SELECT (r).replica_id::uuid AS replica_id,(r).replica_kind::text AS replica_kind,(r).state::text AS state,
 COALESCE((r).desired_replicas,0::smallint)::smallint AS desired_replicas_value,((r).desired_replicas IS NOT NULL)::boolean AS desired_replicas_present,
 COALESCE((r).active_serving_replicas,0::smallint)::smallint AS active_serving_replicas_value,((r).active_serving_replicas IS NOT NULL)::boolean AS active_serving_replicas_present,
 COALESCE((r).active_maintenance_replicas,0::smallint)::smallint AS active_maintenance_replicas_value,((r).active_maintenance_replicas IS NOT NULL)::boolean AS active_maintenance_replicas_present,
 COALESCE((r).partition_1_enabled,false)::boolean AS partition_1_enabled_value,((r).partition_1_enabled IS NOT NULL)::boolean AS partition_1_enabled_present,
 COALESCE((r).partition_2_enabled,false)::boolean AS partition_2_enabled_value,((r).partition_2_enabled IS NOT NULL)::boolean AS partition_2_enabled_present,
 (r).capacity_generation::bigint AS capacity_generation,(r).controller_generation::bigint AS controller_generation,
 (r).controller_operation_id::text AS controller_operation_id,(r).admission_enabled::boolean AS admission_enabled,
 (r).lifecycle_phase::text AS lifecycle_phase,(r).replayed::boolean AS replayed FROM result;

-- name: RuntimeFinishScaleIn :one
WITH result AS MATERIALIZED (
 SELECT public.runtime_finish_scale_in(sqlc.arg(expected_controller_generation)::bigint,sqlc.arg(operation_id)::text,sqlc.arg(replica_id)::uuid,sqlc.arg(instance_id)::text,sqlc.arg(release_digest)::text,sqlc.narg(leave_operation_id)::text,sqlc.arg(fencing_evidence_id)::text) AS r
)
SELECT (r).replica_id::uuid AS replica_id,(r).replica_kind::text AS replica_kind,(r).state::text AS state,
 COALESCE((r).desired_replicas,0::smallint)::smallint AS desired_replicas_value,((r).desired_replicas IS NOT NULL)::boolean AS desired_replicas_present,
 COALESCE((r).active_serving_replicas,0::smallint)::smallint AS active_serving_replicas_value,((r).active_serving_replicas IS NOT NULL)::boolean AS active_serving_replicas_present,
 COALESCE((r).active_maintenance_replicas,0::smallint)::smallint AS active_maintenance_replicas_value,((r).active_maintenance_replicas IS NOT NULL)::boolean AS active_maintenance_replicas_present,
 COALESCE((r).partition_1_enabled,false)::boolean AS partition_1_enabled_value,((r).partition_1_enabled IS NOT NULL)::boolean AS partition_1_enabled_present,
 COALESCE((r).partition_2_enabled,false)::boolean AS partition_2_enabled_value,((r).partition_2_enabled IS NOT NULL)::boolean AS partition_2_enabled_present,
 (r).capacity_generation::bigint AS capacity_generation,(r).controller_generation::bigint AS controller_generation,
 (r).controller_operation_id::text AS controller_operation_id,(r).admission_enabled::boolean AS admission_enabled,
 (r).lifecycle_phase::text AS lifecycle_phase,(r).replayed::boolean AS replayed FROM result;

-- name: RuntimePrepareMaintenanceDrain :one
WITH result AS MATERIALIZED (
 SELECT public.runtime_prepare_maintenance_drain(sqlc.arg(expected_controller_generation)::bigint,sqlc.arg(operation_id)::text,sqlc.arg(replica_id)::uuid,sqlc.arg(instance_id)::text,sqlc.arg(release_digest)::text) AS r
)
SELECT (r).replica_id::uuid AS replica_id,(r).replica_kind::text AS replica_kind,(r).state::text AS state,
 COALESCE((r).desired_replicas,0::smallint)::smallint AS desired_replicas_value,((r).desired_replicas IS NOT NULL)::boolean AS desired_replicas_present,
 COALESCE((r).active_serving_replicas,0::smallint)::smallint AS active_serving_replicas_value,((r).active_serving_replicas IS NOT NULL)::boolean AS active_serving_replicas_present,
 COALESCE((r).active_maintenance_replicas,0::smallint)::smallint AS active_maintenance_replicas_value,((r).active_maintenance_replicas IS NOT NULL)::boolean AS active_maintenance_replicas_present,
 COALESCE((r).partition_1_enabled,false)::boolean AS partition_1_enabled_value,((r).partition_1_enabled IS NOT NULL)::boolean AS partition_1_enabled_present,
 COALESCE((r).partition_2_enabled,false)::boolean AS partition_2_enabled_value,((r).partition_2_enabled IS NOT NULL)::boolean AS partition_2_enabled_present,
 (r).capacity_generation::bigint AS capacity_generation,(r).controller_generation::bigint AS controller_generation,
 (r).controller_operation_id::text AS controller_operation_id,(r).admission_enabled::boolean AS admission_enabled,
 (r).lifecycle_phase::text AS lifecycle_phase,(r).replayed::boolean AS replayed FROM result;

-- name: RuntimeBeginReplicaTermination :one
WITH result AS MATERIALIZED (
 SELECT public.runtime_begin_replica_termination(sqlc.arg(expected_controller_generation)::bigint,sqlc.arg(operation_id)::text,sqlc.arg(request_id)::text,sqlc.arg(replica_id)::uuid,sqlc.arg(instance_id)::text,sqlc.arg(release_digest)::text,sqlc.arg(reason)::text) AS r
)
SELECT (r).replica_id::uuid AS replica_id,(r).replica_kind::text AS replica_kind,(r).state::text AS state,
 COALESCE((r).desired_replicas,0::smallint)::smallint AS desired_replicas_value,((r).desired_replicas IS NOT NULL)::boolean AS desired_replicas_present,
 COALESCE((r).active_serving_replicas,0::smallint)::smallint AS active_serving_replicas_value,((r).active_serving_replicas IS NOT NULL)::boolean AS active_serving_replicas_present,
 COALESCE((r).active_maintenance_replicas,0::smallint)::smallint AS active_maintenance_replicas_value,((r).active_maintenance_replicas IS NOT NULL)::boolean AS active_maintenance_replicas_present,
 COALESCE((r).partition_1_enabled,false)::boolean AS partition_1_enabled_value,((r).partition_1_enabled IS NOT NULL)::boolean AS partition_1_enabled_present,
 COALESCE((r).partition_2_enabled,false)::boolean AS partition_2_enabled_value,((r).partition_2_enabled IS NOT NULL)::boolean AS partition_2_enabled_present,
 (r).capacity_generation::bigint AS capacity_generation,(r).controller_generation::bigint AS controller_generation,
 (r).controller_operation_id::text AS controller_operation_id,(r).admission_enabled::boolean AS admission_enabled,
 (r).lifecycle_phase::text AS lifecycle_phase,(r).replayed::boolean AS replayed FROM result;
