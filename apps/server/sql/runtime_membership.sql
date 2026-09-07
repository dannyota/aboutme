-- name: RuntimeRegisterServingReplica :one
WITH result AS MATERIALIZED (
 SELECT public.runtime_register_serving_replica(sqlc.arg(replica_id)::uuid,sqlc.arg(instance_id)::text,sqlc.arg(container_instance_arn)::text,sqlc.arg(caddy_task_arn)::text,sqlc.arg(go_task_arn)::text,sqlc.arg(nuxt_task_arn)::text,sqlc.arg(release_digest)::text) AS r
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

-- name: RuntimeRegisterMaintenanceReplica :one
WITH result AS MATERIALIZED (
 SELECT public.runtime_register_maintenance_replica(sqlc.arg(replica_id)::uuid,sqlc.arg(instance_id)::text,sqlc.arg(container_instance_arn)::text,sqlc.arg(caddy_task_arn)::text,sqlc.arg(go_task_arn)::text,sqlc.arg(nuxt_task_arn)::text,sqlc.arg(release_digest)::text) AS r
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

-- name: RuntimeMarkServingReplicaJoinReady :one
WITH result AS MATERIALIZED (
 SELECT public.runtime_mark_serving_replica_join_ready(sqlc.arg(replica_id)::uuid,sqlc.arg(instance_id)::text,sqlc.arg(container_instance_arn)::text,sqlc.arg(caddy_task_arn)::text,sqlc.arg(go_task_arn)::text,sqlc.arg(nuxt_task_arn)::text,sqlc.arg(release_digest)::text) AS r
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

-- name: RuntimeMarkMaintenanceReplicaJoinReady :one
WITH result AS MATERIALIZED (
 SELECT public.runtime_mark_maintenance_replica_join_ready(sqlc.arg(replica_id)::uuid,sqlc.arg(instance_id)::text,sqlc.arg(container_instance_arn)::text,sqlc.arg(caddy_task_arn)::text,sqlc.arg(go_task_arn)::text,sqlc.arg(nuxt_task_arn)::text,sqlc.arg(release_digest)::text) AS r
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
