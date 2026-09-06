-- +goose Up
-- +goose StatementBegin
SELECT public.runtime_begin_migration_write('migration-00015');

RESET ROLE;
GRANT CREATE ON SCHEMA public TO aboutme_runtime_owner;
SET LOCAL ROLE aboutme_runtime_owner;

CREATE TYPE public.runtime_replica_result AS (
 replica_id uuid, replica_kind text, state text, desired_replicas smallint,
 active_serving_replicas smallint, active_maintenance_replicas smallint,
 partition_1_enabled boolean, partition_2_enabled boolean,
 capacity_generation bigint, controller_generation bigint,
 controller_operation_id text, admission_enabled boolean, lifecycle_phase text, replayed boolean
);
CREATE TYPE public.runtime_capacity_result AS (
 desired_replicas smallint, active_serving_replicas smallint, active_maintenance_replicas smallint,
 partition_1_enabled boolean, partition_2_enabled boolean, capacity_generation bigint,
 controller_generation bigint, controller_operation_id text, admission_enabled boolean,
 lifecycle_phase text, write_gate text, write_generation bigint, replayed boolean
);
CREATE TYPE public.runtime_leave_result AS (
 replica_id uuid, state text, receipt_operation_id text, controller_generation bigint,
 joined_transition_count integer, joined_claim_count integer, recorded_at timestamptz, replayed boolean
);
CREATE TYPE public.runtime_fence_result AS (
 replica_id uuid, state text, evidence_id text, reclaimed_claim_count integer,
 recorded_at timestamptz, replayed boolean
);

CREATE TABLE public.runtime_replicas (
 replica_id uuid PRIMARY KEY CHECK (replica_id<>'00000000-0000-0000-0000-000000000000'::uuid),
 replica_kind text NOT NULL CHECK (replica_kind IN ('serving','maintenance')),
 instance_id text NOT NULL CHECK (octet_length(instance_id)<=19 AND instance_id~'^i-[0-9a-f]{17}$'),
 container_instance_arn text NOT NULL CHECK (octet_length(container_instance_arn) BETWEEN 1 AND 512 AND container_instance_arn~'^[!-~]+$'),
 caddy_task_arn text NOT NULL CHECK (octet_length(caddy_task_arn) BETWEEN 1 AND 512 AND caddy_task_arn~'^[!-~]+$'),
 go_task_arn text NOT NULL CHECK (octet_length(go_task_arn) BETWEEN 1 AND 512 AND go_task_arn~'^[!-~]+$'),
 nuxt_task_arn text NOT NULL CHECK (octet_length(nuxt_task_arn) BETWEEN 1 AND 512 AND nuxt_task_arn~'^[!-~]+$'),
 release_digest text NOT NULL CHECK (release_digest~'^sha256:[0-9a-f]{64}$'),
 state text NOT NULL CHECK (state IN ('joining','active','draining','left','terminating','fenced')),
 joined_at timestamptz NOT NULL, join_ready_at timestamptz, activated_at timestamptz,
 draining_at timestamptz, left_at timestamptz, termination_requested_at timestamptz, fenced_at timestamptz,
 UNIQUE(instance_id,replica_id), UNIQUE(replica_id,instance_id,release_digest),
 CHECK (caddy_task_arn<>go_task_arn AND caddy_task_arn<>nuxt_task_arn AND go_task_arn<>nuxt_task_arn),
 CHECK ((state='joining' AND activated_at IS NULL AND draining_at IS NULL AND left_at IS NULL AND termination_requested_at IS NULL AND fenced_at IS NULL)
   OR (state='active' AND join_ready_at IS NOT NULL AND activated_at IS NOT NULL AND draining_at IS NULL AND left_at IS NULL AND termination_requested_at IS NULL AND fenced_at IS NULL)
   OR (state='draining' AND activated_at IS NOT NULL AND draining_at IS NOT NULL AND left_at IS NULL AND termination_requested_at IS NULL AND fenced_at IS NULL)
   OR (state='left' AND activated_at IS NOT NULL AND draining_at IS NOT NULL AND left_at IS NOT NULL AND termination_requested_at IS NULL AND fenced_at IS NULL)
   OR (state='terminating' AND termination_requested_at IS NOT NULL AND left_at IS NULL AND fenced_at IS NULL)
   OR (state='fenced' AND fenced_at IS NOT NULL AND left_at IS NULL)),
 CHECK ((join_ready_at IS NULL OR join_ready_at>=joined_at) AND (activated_at IS NULL OR activated_at>=joined_at)
   AND (draining_at IS NULL OR draining_at>=activated_at) AND (left_at IS NULL OR left_at>=draining_at)
   AND (termination_requested_at IS NULL OR termination_requested_at>=joined_at) AND (fenced_at IS NULL OR fenced_at>=joined_at))
);
CREATE UNIQUE INDEX runtime_replicas_one_live_instance_idx ON public.runtime_replicas(instance_id)
 WHERE state IN ('joining','active','draining','terminating');

CREATE TABLE public.runtime_replica_tasks (
 task_arn text PRIMARY KEY CHECK (octet_length(task_arn) BETWEEN 1 AND 512 AND task_arn~'^[!-~]+$'),
 replica_id uuid NOT NULL REFERENCES public.runtime_replicas(replica_id),
 task_role text NOT NULL CHECK (task_role IN ('caddy','go','nuxt')),
 UNIQUE(replica_id,task_role)
);

CREATE TABLE public.runtime_capacity (
 singleton boolean PRIMARY KEY CHECK (singleton), desired_replicas smallint NOT NULL CHECK (desired_replicas BETWEEN 1 AND 2),
 generation bigint NOT NULL CHECK (generation>0), controller_generation bigint NOT NULL CHECK (controller_generation>0),
 controller_operation_id text NOT NULL CHECK (octet_length(controller_operation_id) BETWEEN 1 AND 128 AND controller_operation_id~'^[ -~]+$'),
 admission_enabled boolean NOT NULL, lifecycle_phase text NOT NULL CHECK (lifecycle_phase IN ('offline','starting','online','stopping')),
 updated_at timestamptz NOT NULL, updated_by text NOT NULL CHECK (octet_length(updated_by) BETWEEN 1 AND 128 AND updated_by~'^[ -~]+$'),
 CHECK (admission_enabled=(lifecycle_phase='online'))
);

CREATE TABLE public.runtime_lifecycle_operations (
 operation_id text PRIMARY KEY CHECK (octet_length(operation_id) BETWEEN 1 AND 128 AND operation_id~'^[ -~]+$'),
 workflow_kind text NOT NULL CHECK (workflow_kind IN ('initial_serving','scale_out','replacement_serving','scale_in','uat_serving_wake','maintenance_wake','replica_termination')),
 created_at timestamptz NOT NULL DEFAULT transaction_timestamp()
);
CREATE TABLE public.runtime_lifecycle_operation_steps (
 operation_id text NOT NULL, action text NOT NULL,
 workflow_kind text NOT NULL CHECK (workflow_kind IN ('initial_serving','scale_out','replacement_serving','scale_in','uat_serving_wake','maintenance_wake','replica_termination')),
 expected_generation bigint NOT NULL CHECK (expected_generation>0), result_generation bigint NOT NULL CHECK (result_generation=expected_generation+1),
 argument_digest bytea NOT NULL CHECK (octet_length(argument_digest)=32), result_digest bytea NOT NULL CHECK (octet_length(result_digest)=32),
 argument_replaced_replica_id uuid,
 result_kind text NOT NULL CHECK (result_kind IN ('capacity','replica')),
 replica_id uuid, replica_kind text CHECK (replica_kind IN ('serving','maintenance')), replica_state text CHECK (replica_state IN ('joining','active','draining','left','terminating','fenced')),
 desired_replicas smallint CHECK (desired_replicas BETWEEN 1 AND 2), active_serving_replicas smallint CHECK (active_serving_replicas>=0), active_maintenance_replicas smallint CHECK (active_maintenance_replicas>=0),
 partition_1_enabled boolean, partition_2_enabled boolean,
 capacity_generation bigint NOT NULL CHECK (capacity_generation>0), controller_generation bigint NOT NULL CHECK (controller_generation>0),
 controller_operation_id text NOT NULL CHECK (octet_length(controller_operation_id) BETWEEN 1 AND 128 AND controller_operation_id~'^[ -~]+$'),
 admission_enabled boolean NOT NULL, lifecycle_phase text NOT NULL CHECK (lifecycle_phase IN ('offline','starting','online','stopping')),
 write_gate text CHECK (write_gate IN ('open','closing')), write_generation bigint CHECK (write_generation>0), recorded_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 PRIMARY KEY(operation_id,action), FOREIGN KEY(operation_id) REFERENCES public.runtime_lifecycle_operations(operation_id),
 CHECK (action IN ('prepare_scale_out','activate_replica_capacity','prepare_scale_in','finish_scale_in','prepare_maintenance_drain','begin_replica_termination','begin_wake','complete_wake')),
 CHECK ((workflow_kind='initial_serving' AND action='activate_replica_capacity') OR (workflow_kind='scale_out' AND action IN ('prepare_scale_out','activate_replica_capacity'))
  OR (workflow_kind='replacement_serving' AND action='activate_replica_capacity') OR (workflow_kind='scale_in' AND action IN ('prepare_scale_in','finish_scale_in'))
  OR (workflow_kind='uat_serving_wake' AND action IN ('begin_wake','complete_wake','activate_replica_capacity'))
  OR (workflow_kind='maintenance_wake' AND action IN ('begin_wake','complete_wake','activate_replica_capacity','prepare_maintenance_drain'))
  OR (workflow_kind='replica_termination' AND action='begin_replica_termination')),
 CONSTRAINT runtime_lifecycle_steps_replaced_shape_check CHECK ((action='activate_replica_capacity' AND workflow_kind='replacement_serving')=(argument_replaced_replica_id IS NOT NULL)),
	CHECK (controller_generation=result_generation),
	CHECK (controller_operation_id=operation_id),
	CHECK ((action IN ('prepare_scale_out','begin_wake','complete_wake') AND result_kind='capacity') OR
	       (action IN ('activate_replica_capacity','prepare_scale_in','finish_scale_in','prepare_maintenance_drain','begin_replica_termination') AND result_kind='replica')),
 CONSTRAINT runtime_lifecycle_steps_write_shape_check CHECK (((action='begin_wake' AND write_gate='closing' AND write_generation IS NOT NULL)
  OR (action='complete_wake' AND write_gate='open' AND write_generation IS NOT NULL)
  OR (action NOT IN ('begin_wake','complete_wake') AND write_gate IS NULL AND write_generation IS NULL)) IS TRUE),
	CONSTRAINT runtime_lifecycle_steps_result_shape_check CHECK ((result_kind='capacity' AND replica_id IS NULL AND replica_kind IS NULL AND replica_state IS NULL AND desired_replicas IS NOT NULL AND active_serving_replicas IS NOT NULL AND active_maintenance_replicas IS NOT NULL AND partition_1_enabled IS NOT NULL AND partition_2_enabled IS NOT NULL)
  OR (result_kind='replica' AND replica_id IS NOT NULL AND replica_kind IS NOT NULL AND replica_state IS NOT NULL
    AND ((action='finish_scale_in' AND desired_replicas IS NOT NULL AND active_serving_replicas IS NOT NULL AND active_maintenance_replicas IS NOT NULL AND partition_1_enabled IS NOT NULL AND partition_2_enabled IS NOT NULL)
      OR (action<>'finish_scale_in' AND desired_replicas IS NULL AND active_serving_replicas IS NULL AND active_maintenance_replicas IS NULL AND partition_1_enabled IS NULL AND partition_2_enabled IS NULL)))),
	CHECK (desired_replicas IS NULL OR active_serving_replicas<=desired_replicas),
	CHECK (partition_2_enabled IS DISTINCT FROM true OR partition_1_enabled=true),
	CHECK ((action='activate_replica_capacity' AND replica_state='active') OR
	       (action IN ('prepare_scale_in','prepare_maintenance_drain') AND replica_state='draining') OR
	       (action='begin_replica_termination' AND replica_state='terminating') OR
	       (action='finish_scale_in' AND replica_state IN ('left','fenced')) OR result_kind='capacity'),
	CHECK (action<>'prepare_maintenance_drain' OR replica_kind='maintenance'),
	CHECK (action NOT IN ('prepare_scale_in','finish_scale_in') OR replica_kind='serving'),
	CHECK (action<>'finish_scale_in' OR (desired_replicas=1 AND active_serving_replicas BETWEEN 0 AND 1 AND partition_2_enabled=false)),
 CHECK (admission_enabled=(lifecycle_phase='online'))
);
CREATE UNIQUE INDEX runtime_lifecycle_replaced_once_idx ON public.runtime_lifecycle_operation_steps(argument_replaced_replica_id) WHERE argument_replaced_replica_id IS NOT NULL;

CREATE TABLE public.runtime_termination_intents (
 replica_id uuid PRIMARY KEY, instance_id text NOT NULL, release_digest text NOT NULL,
 request_id text NOT NULL UNIQUE CHECK (octet_length(request_id) BETWEEN 1 AND 128 AND request_id~'^[ -~]+$'),
 reason text NOT NULL CHECK (reason IN ('startup_failed','readiness_failed','drain_failed')),
 requested_at timestamptz NOT NULL,
 FOREIGN KEY(replica_id,instance_id,release_digest) REFERENCES public.runtime_replicas(replica_id,instance_id,release_digest)
);
CREATE TABLE public.runtime_leave_receipts (
 replica_id uuid PRIMARY KEY, instance_id text NOT NULL, release_digest text NOT NULL,
 controller_generation bigint NOT NULL CHECK (controller_generation>0), operation_id text NOT NULL CHECK (octet_length(operation_id) BETWEEN 1 AND 128 AND operation_id~'^[ -~]+$'),
 joined_transition_count integer NOT NULL CHECK (joined_transition_count=0), joined_claim_count integer NOT NULL CHECK (joined_claim_count=0),
 recorded_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 FOREIGN KEY(replica_id,instance_id,release_digest) REFERENCES public.runtime_replicas(replica_id,instance_id,release_digest)
);
CREATE TABLE public.runtime_fencing_proofs (
 replica_id uuid PRIMARY KEY, instance_id text NOT NULL, release_digest text NOT NULL,
 adapter text NOT NULL CHECK (adapter='ec2_terminated_v1'), evidence_id text NOT NULL UNIQUE CHECK (octet_length(evidence_id) BETWEEN 1 AND 128 AND evidence_id~'^[ -~]+$'),
 requested_at timestamptz NOT NULL, observed_terminated_at timestamptz NOT NULL, recorded_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 observed_state text NOT NULL CHECK (observed_state='terminated'),
 CHECK (observed_terminated_at>=requested_at),
 FOREIGN KEY(replica_id,instance_id,release_digest) REFERENCES public.runtime_replicas(replica_id,instance_id,release_digest)
);

CREATE FUNCTION public.runtime_assert_immutable_row() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='runtime evidence is immutable'; END $$;
CREATE FUNCTION public.runtime_validate_replica_change() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF TG_OP='DELETE' OR NEW.replica_id<>OLD.replica_id OR NEW.replica_kind<>OLD.replica_kind OR NEW.instance_id<>OLD.instance_id
  OR NEW.container_instance_arn<>OLD.container_instance_arn OR NEW.caddy_task_arn<>OLD.caddy_task_arn OR NEW.go_task_arn<>OLD.go_task_arn
  OR NEW.nuxt_task_arn<>OLD.nuxt_task_arn OR NEW.release_digest<>OLD.release_digest OR OLD.state IN ('left','fenced')
  OR NOT ((OLD.state='joining' AND NEW.state IN ('joining','active','terminating','fenced')) OR (OLD.state='active' AND NEW.state IN ('active','draining','terminating','fenced'))
   OR (OLD.state='draining' AND NEW.state IN ('draining','left','terminating','fenced')) OR (OLD.state='terminating' AND NEW.state IN ('terminating','fenced'))) THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='runtime replica identity or state transition rejected'; END IF; RETURN NEW;
END $$;
CREATE FUNCTION public.runtime_assert_replica_task_trio() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE target uuid; valid boolean;
BEGIN target:=COALESCE(NEW.replica_id,OLD.replica_id); SELECT count(*)=3 AND bool_and(t.task_arn=CASE t.task_role WHEN 'caddy' THEN r.caddy_task_arn WHEN 'go' THEN r.go_task_arn WHEN 'nuxt' THEN r.nuxt_task_arn END) INTO valid FROM public.runtime_replicas r LEFT JOIN public.runtime_replica_tasks t ON t.replica_id=r.replica_id WHERE r.replica_id=target GROUP BY r.replica_id; IF NOT COALESCE(valid,false) THEN RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='runtime replica task trio mismatch'; END IF; RETURN NULL;
END $$;
CREATE FUNCTION public.runtime_validate_lifecycle_step() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE parent_kind text;
BEGIN
 SELECT workflow_kind INTO parent_kind FROM public.runtime_lifecycle_operations WHERE operation_id=NEW.operation_id;
 IF parent_kind IS DISTINCT FROM NEW.workflow_kind THEN RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='runtime lifecycle parent mismatch'; END IF;
 IF (NEW.workflow_kind='scale_out' AND NEW.action='activate_replica_capacity'
      AND NOT EXISTS (SELECT 1 FROM public.runtime_lifecycle_operation_steps WHERE operation_id=NEW.operation_id AND action='prepare_scale_out' AND result_generation<=NEW.expected_generation))
   OR (NEW.workflow_kind='scale_in' AND NEW.action='finish_scale_in'
      AND NOT EXISTS (SELECT 1 FROM public.runtime_lifecycle_operation_steps WHERE operation_id=NEW.operation_id AND action='prepare_scale_in' AND result_generation<=NEW.expected_generation))
   OR (NEW.workflow_kind IN ('uat_serving_wake','maintenance_wake') AND NEW.action='complete_wake'
      AND NOT EXISTS (SELECT 1 FROM public.runtime_lifecycle_operation_steps WHERE operation_id=NEW.operation_id AND action='begin_wake' AND result_generation<=NEW.expected_generation))
   OR (NEW.workflow_kind IN ('uat_serving_wake','maintenance_wake') AND NEW.action='activate_replica_capacity'
      AND NOT EXISTS (SELECT 1 FROM public.runtime_lifecycle_operation_steps WHERE operation_id=NEW.operation_id AND action='complete_wake' AND result_generation<=NEW.expected_generation))
   OR (NEW.workflow_kind='maintenance_wake' AND NEW.action='prepare_maintenance_drain'
      AND NOT EXISTS (SELECT 1 FROM public.runtime_lifecycle_operation_steps WHERE operation_id=NEW.operation_id AND action='activate_replica_capacity' AND result_generation<=NEW.expected_generation)) THEN
  RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='runtime lifecycle predecessor mismatch';
 END IF;
 RETURN NEW;
END $$;
CREATE FUNCTION public.runtime_validate_termination_intent() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE replica_state text;
BEGIN
 SELECT state INTO replica_state FROM public.runtime_replicas WHERE replica_id=NEW.replica_id;
 IF NOT ((NEW.reason='startup_failed' AND replica_state='joining') OR (NEW.reason='readiness_failed' AND replica_state='active') OR (NEW.reason='drain_failed' AND replica_state='draining')) THEN
  RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='runtime termination intent state mismatch';
 END IF;
 RETURN NEW;
END $$;

CREATE TRIGGER runtime_replicas_identity BEFORE UPDATE OR DELETE ON public.runtime_replicas FOR EACH ROW EXECUTE FUNCTION public.runtime_validate_replica_change();
CREATE TRIGGER runtime_replica_tasks_immutable BEFORE UPDATE OR DELETE ON public.runtime_replica_tasks FOR EACH ROW EXECUTE FUNCTION public.runtime_assert_immutable_row();
CREATE CONSTRAINT TRIGGER runtime_replica_tasks_trio AFTER INSERT OR UPDATE OR DELETE ON public.runtime_replica_tasks DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.runtime_assert_replica_task_trio();
CREATE CONSTRAINT TRIGGER runtime_replicas_trio AFTER INSERT OR UPDATE ON public.runtime_replicas DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.runtime_assert_replica_task_trio();
CREATE TRIGGER runtime_lifecycle_operations_immutable BEFORE UPDATE OR DELETE ON public.runtime_lifecycle_operations FOR EACH ROW EXECUTE FUNCTION public.runtime_assert_immutable_row();
CREATE TRIGGER runtime_lifecycle_steps_immutable BEFORE UPDATE OR DELETE ON public.runtime_lifecycle_operation_steps FOR EACH ROW EXECUTE FUNCTION public.runtime_assert_immutable_row();
CREATE TRIGGER runtime_lifecycle_step_parent BEFORE INSERT ON public.runtime_lifecycle_operation_steps FOR EACH ROW EXECUTE FUNCTION public.runtime_validate_lifecycle_step();
CREATE TRIGGER runtime_termination_intents_immutable BEFORE UPDATE OR DELETE ON public.runtime_termination_intents FOR EACH ROW EXECUTE FUNCTION public.runtime_assert_immutable_row();
CREATE TRIGGER runtime_termination_intents_shape BEFORE INSERT ON public.runtime_termination_intents FOR EACH ROW EXECUTE FUNCTION public.runtime_validate_termination_intent();
CREATE TRIGGER runtime_leave_receipts_immutable BEFORE UPDATE OR DELETE ON public.runtime_leave_receipts FOR EACH ROW EXECUTE FUNCTION public.runtime_assert_immutable_row();
CREATE TRIGGER runtime_fencing_proofs_immutable BEFORE UPDATE OR DELETE ON public.runtime_fencing_proofs FOR EACH ROW EXECUTE FUNCTION public.runtime_assert_immutable_row();
CREATE TRIGGER runtime_capacity_no_delete BEFORE DELETE ON public.runtime_capacity FOR EACH ROW EXECUTE FUNCTION public.runtime_assert_immutable_row();
CREATE TRIGGER runtime_capacity_no_truncate BEFORE TRUNCATE ON public.runtime_capacity FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_immutable_row();
CREATE TRIGGER runtime_replicas_no_truncate BEFORE TRUNCATE ON public.runtime_replicas FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_immutable_row();
CREATE TRIGGER runtime_replica_tasks_no_truncate BEFORE TRUNCATE ON public.runtime_replica_tasks FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_immutable_row();
CREATE TRIGGER runtime_lifecycle_operations_no_truncate BEFORE TRUNCATE ON public.runtime_lifecycle_operations FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_immutable_row();
CREATE TRIGGER runtime_lifecycle_steps_no_truncate BEFORE TRUNCATE ON public.runtime_lifecycle_operation_steps FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_immutable_row();
CREATE TRIGGER runtime_termination_intents_no_truncate BEFORE TRUNCATE ON public.runtime_termination_intents FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_immutable_row();
CREATE TRIGGER runtime_leave_receipts_no_truncate BEFORE TRUNCATE ON public.runtime_leave_receipts FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_immutable_row();
CREATE TRIGGER runtime_fencing_proofs_no_truncate BEFORE TRUNCATE ON public.runtime_fencing_proofs FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_immutable_row();

CREATE TRIGGER runtime_replicas_write_entry BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON public.runtime_replicas FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_write_entry();
CREATE TRIGGER runtime_replica_tasks_write_entry BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON public.runtime_replica_tasks FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_write_entry();
CREATE TRIGGER runtime_capacity_write_entry BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON public.runtime_capacity FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_write_entry();
CREATE TRIGGER runtime_lifecycle_operations_write_entry BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON public.runtime_lifecycle_operations FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_write_entry();
CREATE TRIGGER runtime_lifecycle_operation_steps_write_entry BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON public.runtime_lifecycle_operation_steps FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_write_entry();
CREATE TRIGGER runtime_termination_intents_write_entry BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON public.runtime_termination_intents FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_write_entry();
CREATE TRIGGER runtime_leave_receipts_write_entry BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON public.runtime_leave_receipts FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_write_entry();
CREATE TRIGGER runtime_fencing_proofs_write_entry BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON public.runtime_fencing_proofs FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_write_entry();

INSERT INTO public.runtime_capacity(singleton,desired_replicas,generation,controller_generation,controller_operation_id,admission_enabled,lifecycle_phase,updated_at,updated_by)
SELECT true,1,1,1,'bootstrap-uncomposed-v1',true,'online',now_at,'bootstrap-uncomposed-v1' FROM (SELECT clock_timestamp() now_at) seed;

REVOKE ALL ON TABLE public.runtime_replicas,public.runtime_replica_tasks,public.runtime_capacity,public.runtime_lifecycle_operations,public.runtime_lifecycle_operation_steps,public.runtime_termination_intents,public.runtime_leave_receipts,public.runtime_fencing_proofs FROM PUBLIC,aboutme_app,aboutme_maintenance,aboutme_lifecycle_command,aboutme_fencing_proof,aboutme_restore_verify;
GRANT SELECT ON public.runtime_capacity,public.runtime_termination_intents,public.runtime_leave_receipts,public.runtime_fencing_proofs TO aboutme_app;
REVOKE ALL ON FUNCTION public.runtime_assert_immutable_row(),public.runtime_validate_replica_change(),public.runtime_assert_replica_task_trio(),public.runtime_validate_lifecycle_step(),public.runtime_validate_termination_intent() FROM PUBLIC;
REVOKE ALL ON TYPE public.runtime_replica_result,public.runtime_capacity_result,public.runtime_leave_result,public.runtime_fence_result FROM PUBLIC;

RESET ROLE;
REVOKE CREATE ON SCHEMA public FROM aboutme_runtime_owner;
SELECT public.runtime_finish_write();
-- +goose StatementEnd

-- +goose Down
-- Forward-only before the UAT baseline.
