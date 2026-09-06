-- +goose Up
-- +goose StatementBegin
SELECT public.runtime_begin_migration_write('migration-00016');

RESET ROLE;
GRANT CREATE ON SCHEMA public TO aboutme_runtime_owner;
SET LOCAL ROLE aboutme_runtime_owner;

CREATE TABLE public.public_transitions (
 transition_id uuid PRIMARY KEY CONSTRAINT public_transitions_transition_id_non_nil CHECK (transition_id<>'00000000-0000-0000-0000-000000000000'::uuid),
 initiator_replica_id uuid NOT NULL, initiator_instance_id text NOT NULL, initiator_release_digest text NOT NULL,
 operation text NOT NULL CONSTRAINT public_transitions_operation_check CHECK (operation IN ('resume_update','resume_publication','resume_retire','account_retire')),
 state text NOT NULL CONSTRAINT public_transitions_state_check CHECK (state IN ('closing','committed','rolled_back','unresolved')),
 created_at timestamptz NOT NULL, deadline_at timestamptz NOT NULL,
 terminal_at timestamptz, target_digest bytea NOT NULL,
 terminal_error_code text, recovery_fencing_evidence_id text,
 CONSTRAINT public_transitions_deadline_check CHECK (deadline_at=created_at+interval '5 seconds'),
 CONSTRAINT public_transitions_target_digest_check CHECK (octet_length(target_digest)=32),
 CONSTRAINT public_transitions_terminal_shape_check CHECK ((
  (state='closing' AND terminal_at IS NULL AND terminal_error_code IS NULL AND recovery_fencing_evidence_id IS NULL) OR
  (state='committed' AND terminal_at IS NOT NULL AND terminal_error_code IS NULL AND recovery_fencing_evidence_id IS NULL) OR
  (state='rolled_back' AND terminal_at IS NOT NULL AND terminal_error_code IN ('canceled','deadline','drain_failed','ack_missing','business_not_started') AND recovery_fencing_evidence_id IS NULL) OR
  (state='rolled_back' AND terminal_at IS NOT NULL AND terminal_error_code='initiator_fenced' AND recovery_fencing_evidence_id IS NOT NULL) OR
  (state='unresolved' AND terminal_at IS NOT NULL AND terminal_error_code='recovery_evidence_conflict' AND recovery_fencing_evidence_id IS NULL)) IS TRUE),
 CONSTRAINT public_transitions_initiator_fkey FOREIGN KEY(initiator_replica_id,initiator_instance_id,initiator_release_digest)
  REFERENCES public.runtime_replicas(replica_id,instance_id,release_digest) ON UPDATE RESTRICT ON DELETE RESTRICT,
 CONSTRAINT public_transitions_fencing_evidence_fkey FOREIGN KEY(recovery_fencing_evidence_id)
  REFERENCES public.runtime_fencing_proofs(evidence_id) ON UPDATE RESTRICT ON DELETE RESTRICT,
 CONSTRAINT public_transitions_id_digest_key UNIQUE(transition_id,target_digest)
);

CREATE TABLE public.public_transition_targets (
 transition_id uuid NOT NULL, ordinal integer NOT NULL, kind text NOT NULL, resume_id uuid,
 expected_generation bigint NOT NULL, class text NOT NULL,
 result_kind text, result_generation bigint, result_recorded_at timestamptz,
 CONSTRAINT public_transition_targets_pkey PRIMARY KEY(transition_id,ordinal),
 CONSTRAINT public_transition_targets_parent_fkey FOREIGN KEY(transition_id) REFERENCES public.public_transitions(transition_id) ON UPDATE RESTRICT ON DELETE RESTRICT,
 CONSTRAINT public_transition_targets_ordinal_check CHECK (ordinal>=0),
 CONSTRAINT public_transition_targets_kind_check CHECK (kind IN ('discovery','resume')),
 CONSTRAINT public_transition_targets_identity_check CHECK ((kind='discovery' AND resume_id IS NULL) OR (kind='resume' AND resume_id IS NOT NULL AND resume_id<>'00000000-0000-0000-0000-000000000000'::uuid)),
 CONSTRAINT public_transition_targets_generation_check CHECK (expected_generation>0),
 CONSTRAINT public_transition_targets_class_check CHECK (class IN ('non_draining','revoking') AND (kind<>'discovery' OR class='revoking')),
 CONSTRAINT public_transition_targets_result_kind_check CHECK (result_kind IS NULL OR result_kind IN ('generation','retired')),
 CONSTRAINT public_transition_targets_result_shape_check CHECK ((
  (result_kind IS NULL AND result_generation IS NULL AND result_recorded_at IS NULL) OR
  (result_kind='generation' AND result_generation>0 AND result_recorded_at IS NOT NULL) OR
  (result_kind='retired' AND result_generation IS NULL AND result_recorded_at IS NOT NULL AND kind='resume')) IS TRUE),
 CONSTRAINT public_transition_targets_transition_resume_key UNIQUE NULLS DISTINCT(transition_id,resume_id)
);
CREATE UNIQUE INDEX public_transition_targets_discovery_idx ON public.public_transition_targets(transition_id) WHERE kind='discovery';
CREATE INDEX public_transition_targets_resume_idx ON public.public_transition_targets(resume_id) WHERE resume_id IS NOT NULL;

CREATE TABLE public.public_transition_replicas (
 transition_id uuid NOT NULL, replica_id uuid NOT NULL, snapshot_state text NOT NULL,
 instance_id text NOT NULL, release_digest text NOT NULL, target_digest bytea NOT NULL,
 CONSTRAINT public_transition_replicas_pkey PRIMARY KEY(transition_id,replica_id),
 CONSTRAINT public_transition_replicas_replica_non_nil CHECK (replica_id<>'00000000-0000-0000-0000-000000000000'::uuid),
 CONSTRAINT public_transition_replicas_snapshot_state_check CHECK (snapshot_state='required'),
 CONSTRAINT public_transition_replicas_target_digest_check CHECK (octet_length(target_digest)=32),
 CONSTRAINT public_transition_replicas_membership_fkey FOREIGN KEY(replica_id,instance_id,release_digest) REFERENCES public.runtime_replicas(replica_id,instance_id,release_digest) ON UPDATE RESTRICT ON DELETE RESTRICT,
 CONSTRAINT public_transition_replicas_parent_digest_fkey FOREIGN KEY(transition_id,target_digest) REFERENCES public.public_transitions(transition_id,target_digest) ON UPDATE RESTRICT ON DELETE RESTRICT,
 CONSTRAINT public_transition_replicas_id_digest_key UNIQUE(transition_id,replica_id,target_digest)
);
CREATE INDEX public_transition_replicas_replica_idx ON public.public_transition_replicas(replica_id);

CREATE TABLE public.public_transition_acks (
 transition_id uuid NOT NULL, replica_id uuid NOT NULL, target_digest bytea NOT NULL,
 acked_at timestamptz NOT NULL, local_result text NOT NULL,
 revoking_count integer NOT NULL, non_draining_count integer NOT NULL,
 CONSTRAINT public_transition_acks_pkey PRIMARY KEY(transition_id,replica_id),
 CONSTRAINT public_transition_acks_replica_non_nil CHECK (replica_id<>'00000000-0000-0000-0000-000000000000'::uuid),
 CONSTRAINT public_transition_acks_target_digest_check CHECK (octet_length(target_digest)=32),
 CONSTRAINT public_transition_acks_local_result_check CHECK (local_result='closed'),
 CONSTRAINT public_transition_acks_counts_check CHECK (revoking_count>=0 AND non_draining_count>=0),
 CONSTRAINT public_transition_acks_required_fkey FOREIGN KEY(transition_id,replica_id,target_digest) REFERENCES public.public_transition_replicas(transition_id,replica_id,target_digest) ON UPDATE RESTRICT ON DELETE RESTRICT
);

CREATE INDEX public_transitions_open_idx ON public.public_transitions(state) WHERE state IN ('closing','unresolved');

CREATE FUNCTION public.runtime_public_transition_target_digest(target_transition_id uuid) RETURNS bytea
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE encoded bytea; target_count integer; target record;
BEGIN
 SELECT count(*) INTO target_count FROM public.public_transition_targets WHERE transition_id=target_transition_id;
 encoded:=convert_to('aboutme.public-transition-targets.v1','UTF8')||decode('00','hex')||int4send(target_count);
 FOR target IN SELECT ordinal,kind,resume_id,expected_generation,class FROM public.public_transition_targets WHERE transition_id=target_transition_id ORDER BY ordinal LOOP
  encoded:=encoded||int4send(target.ordinal)||CASE target.kind WHEN 'discovery' THEN decode('00','hex') ELSE decode('01','hex') END
   ||CASE target.kind WHEN 'discovery' THEN decode(repeat('00',16),'hex') ELSE uuid_send(target.resume_id) END
   ||int8send(target.expected_generation)||CASE target.class WHEN 'non_draining' THEN decode('00','hex') ELSE decode('01','hex') END;
 END LOOP;
 RETURN sha256(encoded);
END $$;

CREATE FUNCTION public.runtime_assert_public_transition_parent() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE parent_id uuid:=COALESCE(NEW.transition_id,OLD.transition_id);
 target_count integer; required_count integer; ack_count integer; valid_order boolean; valid_results boolean; parent_state text; parent_digest bytea;
BEGIN
 SELECT state,target_digest INTO parent_state,parent_digest FROM public.public_transitions WHERE transition_id=parent_id;
 IF NOT FOUND THEN RETURN NULL; END IF;
 SELECT count(*),COALESCE(bool_and(ordinal=row_number-1 AND (kind<>'discovery' OR ordinal=0) AND
   (kind<>'resume' OR previous_resume IS NULL OR uuid_send(resume_id)>uuid_send(previous_resume))),false)
 INTO target_count,valid_order FROM (
  SELECT *,row_number() OVER (ORDER BY ordinal) AS row_number,lag(resume_id) OVER (ORDER BY ordinal) AS previous_resume
  FROM public.public_transition_targets WHERE transition_id=parent_id) ordered_targets;
 SELECT count(*) INTO required_count FROM public.public_transition_replicas WHERE transition_id=parent_id;
 SELECT count(*) INTO ack_count FROM public.public_transition_acks WHERE transition_id=parent_id;
 SELECT COALESCE(bool_and(
  (parent_state='committed' AND result_kind IS NOT NULL) OR
  (parent_state IN ('closing','rolled_back','unresolved') AND result_kind IS NULL AND result_generation IS NULL AND result_recorded_at IS NULL)),false)
 INTO valid_results FROM public.public_transition_targets WHERE transition_id=parent_id;
 IF target_count NOT BETWEEN 1 AND 4 OR NOT valid_order OR public.runtime_public_transition_target_digest(parent_id)<>parent_digest
    OR required_count=0 OR NOT valid_results OR (parent_state='committed' AND ack_count<>required_count) THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='public_transition_parent_complete',MESSAGE='public transition parent is incomplete';
 END IF;
 RETURN NULL;
END $$;

CREATE FUNCTION public.runtime_validate_public_transition_parent() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF TG_OP='DELETE' OR OLD.state<>'closing' OR NEW.transition_id<>OLD.transition_id OR NEW.initiator_replica_id<>OLD.initiator_replica_id
  OR NEW.initiator_instance_id<>OLD.initiator_instance_id OR NEW.initiator_release_digest<>OLD.initiator_release_digest
  OR NEW.operation<>OLD.operation OR NEW.created_at<>OLD.created_at OR NEW.deadline_at<>OLD.deadline_at OR NEW.target_digest<>OLD.target_digest
  OR NEW.state NOT IN ('committed','rolled_back','unresolved') THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='public transition parent mutation rejected';
 END IF;
 RETURN NEW;
END $$;

CREATE FUNCTION public.runtime_validate_public_transition_child() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE parent_state text;
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='public transition child is immutable'; END IF;
 IF NEW.transition_id IS NULL THEN RETURN NEW; END IF;
 SELECT state INTO parent_state FROM public.public_transitions WHERE transition_id=NEW.transition_id FOR UPDATE;
 IF parent_state IS DISTINCT FROM 'closing' THEN RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='public transition child requires closing parent'; END IF;
 RETURN NEW;
END $$;

CREATE FUNCTION public.runtime_validate_public_transition_target() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE parent_state text;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='public transition target is immutable'; END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.transition_id IS NULL THEN RETURN NEW; END IF;
  SELECT state INTO parent_state FROM public.public_transitions WHERE transition_id=NEW.transition_id FOR UPDATE;
  IF parent_state IS DISTINCT FROM 'closing' THEN RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='public transition child requires closing parent'; END IF;
  RETURN NEW;
 END IF;
 SELECT state INTO parent_state FROM public.public_transitions WHERE transition_id=OLD.transition_id FOR UPDATE;
 IF parent_state<>'closing' OR NEW.transition_id<>OLD.transition_id OR NEW.ordinal<>OLD.ordinal OR NEW.kind<>OLD.kind
  OR NEW.resume_id IS DISTINCT FROM OLD.resume_id OR NEW.expected_generation<>OLD.expected_generation OR NEW.class<>OLD.class
  OR OLD.result_kind IS NOT NULL OR NEW.result_kind IS NULL THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='public transition target mutation rejected';
 END IF;
 RETURN NEW;
END $$;

CREATE FUNCTION public.runtime_reject_public_transition_truncate() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='public transition truncate rejected'; END $$;

CREATE TRIGGER public_transitions_immutable BEFORE UPDATE OR DELETE ON public.public_transitions FOR EACH ROW EXECUTE FUNCTION public.runtime_validate_public_transition_parent();
CREATE TRIGGER public_transition_targets_immutable BEFORE INSERT OR UPDATE OR DELETE ON public.public_transition_targets FOR EACH ROW EXECUTE FUNCTION public.runtime_validate_public_transition_target();
CREATE TRIGGER public_transition_replicas_immutable BEFORE INSERT OR UPDATE OR DELETE ON public.public_transition_replicas FOR EACH ROW EXECUTE FUNCTION public.runtime_validate_public_transition_child();
CREATE TRIGGER public_transition_acks_immutable BEFORE INSERT OR UPDATE OR DELETE ON public.public_transition_acks FOR EACH ROW EXECUTE FUNCTION public.runtime_validate_public_transition_child();

CREATE CONSTRAINT TRIGGER public_transitions_parent_complete AFTER INSERT OR UPDATE ON public.public_transitions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.runtime_assert_public_transition_parent();
CREATE CONSTRAINT TRIGGER public_transition_targets_parent_complete AFTER INSERT OR UPDATE ON public.public_transition_targets DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.runtime_assert_public_transition_parent();
CREATE CONSTRAINT TRIGGER public_transition_replicas_parent_complete AFTER INSERT ON public.public_transition_replicas DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.runtime_assert_public_transition_parent();
CREATE CONSTRAINT TRIGGER public_transition_acks_parent_complete AFTER INSERT ON public.public_transition_acks DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.runtime_assert_public_transition_parent();

CREATE TRIGGER public_transitions_write_entry BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON public.public_transitions FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_write_entry();
CREATE TRIGGER public_transition_targets_write_entry BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON public.public_transition_targets FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_write_entry();
CREATE TRIGGER public_transition_replicas_write_entry BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON public.public_transition_replicas FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_write_entry();
CREATE TRIGGER public_transition_acks_write_entry BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON public.public_transition_acks FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_write_entry();
CREATE TRIGGER public_transitions_no_truncate BEFORE TRUNCATE ON public.public_transitions FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_reject_public_transition_truncate();
CREATE TRIGGER public_transition_targets_no_truncate BEFORE TRUNCATE ON public.public_transition_targets FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_reject_public_transition_truncate();
CREATE TRIGGER public_transition_replicas_no_truncate BEFORE TRUNCATE ON public.public_transition_replicas FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_reject_public_transition_truncate();
CREATE TRIGGER public_transition_acks_no_truncate BEFORE TRUNCATE ON public.public_transition_acks FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_reject_public_transition_truncate();

REVOKE ALL ON TABLE public.public_transitions,public.public_transition_targets,public.public_transition_replicas,public.public_transition_acks FROM PUBLIC,aboutme_app,aboutme_maintenance,aboutme_lifecycle_command,aboutme_fencing_proof,aboutme_restore_verify;
REVOKE ALL ON FUNCTION public.runtime_public_transition_target_digest(uuid),public.runtime_assert_public_transition_parent(),public.runtime_validate_public_transition_parent(),public.runtime_validate_public_transition_child(),public.runtime_validate_public_transition_target(),public.runtime_reject_public_transition_truncate() FROM PUBLIC,aboutme_app,aboutme_maintenance,aboutme_lifecycle_command,aboutme_fencing_proof,aboutme_restore_verify;

RESET ROLE;
REVOKE CREATE ON SCHEMA public FROM aboutme_runtime_owner;
SELECT public.runtime_finish_write();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 1;
-- +goose StatementEnd
