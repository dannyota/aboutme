-- +goose Up
-- +goose StatementBegin
SELECT public.runtime_begin_migration_write('migration-00019');

RESET ROLE;
GRANT CREATE ON SCHEMA public TO aboutme_runtime_owner;
SET LOCAL ROLE aboutme_runtime_owner;

CREATE FUNCTION public.runtime_validate_replica_identity_input(
 p_replica_id uuid, p_instance_id text, p_container_instance_arn text,
 p_caddy_task_arn text, p_go_task_arn text, p_nuxt_task_arn text,
 p_release_digest text
) RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF p_replica_id IS NULL OR p_replica_id='00000000-0000-0000-0000-000000000000'::uuid
  OR p_instance_id IS NULL OR octet_length(p_instance_id)>19 OR p_instance_id!~'^i-[0-9a-f]{17}$'
  OR p_container_instance_arn IS NULL OR octet_length(p_container_instance_arn) NOT BETWEEN 1 AND 512 OR p_container_instance_arn!~'^[!-~]+$'
  OR p_caddy_task_arn IS NULL OR octet_length(p_caddy_task_arn) NOT BETWEEN 1 AND 512 OR p_caddy_task_arn!~'^[!-~]+$'
  OR p_go_task_arn IS NULL OR octet_length(p_go_task_arn) NOT BETWEEN 1 AND 512 OR p_go_task_arn!~'^[!-~]+$'
  OR p_nuxt_task_arn IS NULL OR octet_length(p_nuxt_task_arn) NOT BETWEEN 1 AND 512 OR p_nuxt_task_arn!~'^[!-~]+$'
  OR p_release_digest IS NULL OR p_release_digest!~'^sha256:[0-9a-f]{64}$'
  OR p_container_instance_arn IN (p_caddy_task_arn,p_go_task_arn,p_nuxt_task_arn)
  OR p_caddy_task_arn IN (p_go_task_arn,p_nuxt_task_arn) OR p_go_task_arn=p_nuxt_task_arn THEN
  RAISE EXCEPTION USING ERRCODE='22023',MESSAGE='replica identity input is invalid';
 END IF;
END $$;

CREATE FUNCTION public.runtime_replica_result_value(p_replica public.runtime_replicas, p_capacity public.runtime_capacity, p_replayed boolean)
RETURNS public.runtime_replica_result LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT ROW(p_replica.replica_id,p_replica.replica_kind,p_replica.state,NULL::smallint,NULL::smallint,NULL::smallint,
  NULL::boolean,NULL::boolean,p_capacity.generation,p_capacity.controller_generation,p_capacity.controller_operation_id,
  p_capacity.admission_enabled,p_capacity.lifecycle_phase,p_replayed)::public.runtime_replica_result
$$;

CREATE FUNCTION public.runtime_assert_replica_registration_children(p_replica public.runtime_replicas)
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE child_count integer; children_match boolean;
BEGIN
 SELECT count(*),COALESCE(bool_and(task_arn=CASE task_role WHEN 'caddy' THEN p_replica.caddy_task_arn WHEN 'go' THEN p_replica.go_task_arn WHEN 'nuxt' THEN p_replica.nuxt_task_arn END),false)
 INTO child_count,children_match FROM public.runtime_replica_tasks WHERE replica_id=p_replica.replica_id;
 IF child_count<>3 OR NOT children_match
  OR NOT EXISTS(SELECT 1 FROM public.runtime_replica_tasks WHERE replica_id=p_replica.replica_id AND task_role='caddy')
  OR NOT EXISTS(SELECT 1 FROM public.runtime_replica_tasks WHERE replica_id=p_replica.replica_id AND task_role='go')
  OR NOT EXISTS(SELECT 1 FROM public.runtime_replica_tasks WHERE replica_id=p_replica.replica_id AND task_role='nuxt') THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='stored replica task set is invalid';
 END IF;
END $$;

CREATE FUNCTION public.runtime_register_replica(
 p_replica_kind text, p_updated_by text, p_replica_id uuid, p_instance_id text,
 p_container_instance_arn text, p_caddy_task_arn text, p_go_task_arn text,
 p_nuxt_task_arn text, p_release_digest text
) RETURNS public.runtime_replica_result LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE existing public.runtime_replicas; capacity_row public.runtime_capacity; effective_at timestamptz;
BEGIN
 PERFORM public.runtime_require_write_entry();
 PERFORM public.runtime_validate_replica_identity_input(p_replica_id,p_instance_id,p_container_instance_arn,p_caddy_task_arn,p_go_task_arn,p_nuxt_task_arn,p_release_digest);
 PERFORM 1 FROM public.public_state WHERE singleton FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='runtime singleton state is missing'; END IF;
 SELECT * INTO capacity_row FROM public.runtime_capacity WHERE singleton FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='runtime capacity is missing'; END IF;
 SELECT * INTO existing FROM public.runtime_replicas WHERE replica_id=p_replica_id;
 IF FOUND THEN
  IF existing.replica_kind<>p_replica_kind OR existing.instance_id<>p_instance_id OR existing.container_instance_arn<>p_container_instance_arn
   OR existing.caddy_task_arn<>p_caddy_task_arn OR existing.go_task_arn<>p_go_task_arn OR existing.nuxt_task_arn<>p_nuxt_task_arn
   OR existing.release_digest<>p_release_digest THEN
   RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='replica registration identity conflicts';
  END IF;
  PERFORM public.runtime_assert_replica_registration_children(existing);
  RETURN public.runtime_replica_result_value(existing,capacity_row,true);
 END IF;
 IF EXISTS(SELECT 1 FROM public.runtime_replicas WHERE instance_id=p_instance_id AND state IN ('joining','active','draining','terminating'))
  OR EXISTS(SELECT 1 FROM public.runtime_replica_tasks WHERE task_arn IN (p_caddy_task_arn,p_go_task_arn,p_nuxt_task_arn)) THEN
  RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='replica registration identity conflicts';
 END IF;
 IF capacity_row.generation=9223372036854775807 THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='runtime capacity generation is exhausted';
 END IF;
 effective_at:=GREATEST(clock_timestamp(),capacity_row.updated_at);
 BEGIN
  INSERT INTO public.runtime_replicas(replica_id,replica_kind,instance_id,container_instance_arn,caddy_task_arn,go_task_arn,nuxt_task_arn,release_digest,state,joined_at)
  VALUES(p_replica_id,p_replica_kind,p_instance_id,p_container_instance_arn,p_caddy_task_arn,p_go_task_arn,p_nuxt_task_arn,p_release_digest,'joining',effective_at)
  RETURNING * INTO existing;
  INSERT INTO public.runtime_replica_tasks(task_arn,replica_id,task_role) VALUES
   (p_caddy_task_arn,p_replica_id,'caddy'),(p_go_task_arn,p_replica_id,'go'),(p_nuxt_task_arn,p_replica_id,'nuxt');
 EXCEPTION WHEN unique_violation THEN
  RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='replica registration identity conflicts';
 END;
 UPDATE public.runtime_capacity SET generation=generation+1,updated_at=effective_at,updated_by=p_updated_by WHERE singleton RETURNING * INTO capacity_row;
 RETURN public.runtime_replica_result_value(existing,capacity_row,false);
END $$;

CREATE FUNCTION public.runtime_mark_replica_join_ready(
 p_replica_kind text, p_updated_by text, p_replica_id uuid, p_instance_id text,
 p_container_instance_arn text, p_caddy_task_arn text, p_go_task_arn text,
 p_nuxt_task_arn text, p_release_digest text
) RETURNS public.runtime_replica_result LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE existing public.runtime_replicas; capacity_row public.runtime_capacity; effective_at timestamptz;
BEGIN
 PERFORM public.runtime_require_write_entry();
 PERFORM public.runtime_validate_replica_identity_input(p_replica_id,p_instance_id,p_container_instance_arn,p_caddy_task_arn,p_go_task_arn,p_nuxt_task_arn,p_release_digest);
 PERFORM 1 FROM public.public_state WHERE singleton FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='runtime singleton state is missing'; END IF;
 SELECT * INTO capacity_row FROM public.runtime_capacity WHERE singleton FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='runtime capacity is missing'; END IF;
 SELECT * INTO existing FROM public.runtime_replicas WHERE replica_id=p_replica_id FOR UPDATE;
 IF NOT FOUND OR existing.state<>'joining' THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='replica is unavailable for join readiness';
 END IF;
 IF existing.replica_kind<>p_replica_kind OR existing.instance_id<>p_instance_id OR existing.container_instance_arn<>p_container_instance_arn
  OR existing.caddy_task_arn<>p_caddy_task_arn OR existing.go_task_arn<>p_go_task_arn OR existing.nuxt_task_arn<>p_nuxt_task_arn
  OR existing.release_digest<>p_release_digest THEN
  RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='replica readiness identity conflicts';
 END IF;
 PERFORM public.runtime_assert_replica_registration_children(existing);
 IF NOT ((capacity_row.lifecycle_phase='online' AND capacity_row.admission_enabled)
      OR (capacity_row.lifecycle_phase='starting' AND NOT capacity_row.admission_enabled))
  OR EXISTS(SELECT 1 FROM public.runtime_termination_intents WHERE replica_id=p_replica_id)
  OR EXISTS(SELECT 1 FROM public.public_transitions WHERE state IN ('closing','unresolved')) THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='replica join readiness is unavailable';
 END IF;
 IF existing.join_ready_at IS NOT NULL THEN
  RETURN public.runtime_replica_result_value(existing,capacity_row,true);
 END IF;
 IF capacity_row.generation=9223372036854775807 THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='runtime capacity generation is exhausted';
 END IF;
 effective_at:=GREATEST(clock_timestamp(),capacity_row.updated_at,existing.joined_at);
 UPDATE public.runtime_replicas SET join_ready_at=effective_at WHERE replica_id=p_replica_id RETURNING * INTO existing;
 UPDATE public.runtime_capacity SET generation=generation+1,updated_at=effective_at,updated_by=p_updated_by WHERE singleton RETURNING * INTO capacity_row;
 RETURN public.runtime_replica_result_value(existing,capacity_row,false);
END $$;

CREATE FUNCTION public.runtime_register_serving_replica(uuid,text,text,text,text,text,text) RETURNS public.runtime_replica_result
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$ BEGIN
 IF session_user<>'aboutme_app' THEN RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='replica registration role is forbidden'; END IF;
 RETURN public.runtime_register_replica('serving','runtime_register_serving_replica',$1,$2,$3,$4,$5,$6,$7); END $$;
CREATE FUNCTION public.runtime_register_maintenance_replica(uuid,text,text,text,text,text,text) RETURNS public.runtime_replica_result
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$ BEGIN
 IF session_user<>'aboutme_maintenance' THEN RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='replica registration role is forbidden'; END IF;
 RETURN public.runtime_register_replica('maintenance','runtime_register_maintenance_replica',$1,$2,$3,$4,$5,$6,$7); END $$;
CREATE FUNCTION public.runtime_mark_serving_replica_join_ready(uuid,text,text,text,text,text,text) RETURNS public.runtime_replica_result
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$ BEGIN
 IF session_user<>'aboutme_app' THEN RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='replica readiness role is forbidden'; END IF;
 RETURN public.runtime_mark_replica_join_ready('serving','runtime_mark_serving_replica_join_ready',$1,$2,$3,$4,$5,$6,$7); END $$;
CREATE FUNCTION public.runtime_mark_maintenance_replica_join_ready(uuid,text,text,text,text,text,text) RETURNS public.runtime_replica_result
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$ BEGIN
 IF session_user<>'aboutme_maintenance' THEN RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='replica readiness role is forbidden'; END IF;
 RETURN public.runtime_mark_replica_join_ready('maintenance','runtime_mark_maintenance_replica_join_ready',$1,$2,$3,$4,$5,$6,$7); END $$;

REVOKE ALL ON FUNCTION public.runtime_validate_replica_identity_input(uuid,text,text,text,text,text,text),public.runtime_replica_result_value(public.runtime_replicas,public.runtime_capacity,boolean),public.runtime_assert_replica_registration_children(public.runtime_replicas),public.runtime_register_replica(text,text,uuid,text,text,text,text,text,text),public.runtime_mark_replica_join_ready(text,text,uuid,text,text,text,text,text,text) FROM PUBLIC,aboutme_app,aboutme_maintenance,aboutme_lifecycle_command,aboutme_fencing_proof,aboutme_restore_verify;
REVOKE ALL ON FUNCTION public.runtime_register_serving_replica(uuid,text,text,text,text,text,text),public.runtime_register_maintenance_replica(uuid,text,text,text,text,text,text),public.runtime_mark_serving_replica_join_ready(uuid,text,text,text,text,text,text),public.runtime_mark_maintenance_replica_join_ready(uuid,text,text,text,text,text,text) FROM PUBLIC,aboutme_app,aboutme_maintenance,aboutme_lifecycle_command,aboutme_fencing_proof,aboutme_restore_verify;
GRANT EXECUTE ON FUNCTION public.runtime_register_serving_replica(uuid,text,text,text,text,text,text),public.runtime_mark_serving_replica_join_ready(uuid,text,text,text,text,text,text) TO aboutme_app;
GRANT EXECUTE ON FUNCTION public.runtime_register_maintenance_replica(uuid,text,text,text,text,text,text),public.runtime_mark_maintenance_replica_join_ready(uuid,text,text,text,text,text,text) TO aboutme_maintenance;

RESET ROLE;
REVOKE CREATE ON SCHEMA public FROM aboutme_runtime_owner;
SELECT public.runtime_finish_write();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 1;
-- +goose StatementEnd
