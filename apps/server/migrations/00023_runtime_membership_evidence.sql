-- +goose Up
-- +goose StatementBegin
SELECT public.runtime_begin_migration_write('migration-00023');

RESET ROLE;
GRANT CREATE ON SCHEMA public TO aboutme_runtime_owner;
SET LOCAL ROLE aboutme_runtime_owner;

-- Migration 00015 shipped runtime_fencing_proofs without the two fields exact
-- replay needs. No proof writer exists before this migration, so the table
-- must still be empty. A nonempty legacy table fails closed with a value-free
-- diagnostic; nothing is deleted and no request ID or reclaimed count is
-- fabricated, because neither can be reconstructed after claims change or GC.
DO $upgrade$
BEGIN
 IF EXISTS(SELECT 1 FROM public.runtime_fencing_proofs) THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='runtime fencing proof upgrade requires an empty evidence table';
 END IF;
END $upgrade$;

ALTER TABLE public.runtime_fencing_proofs
 ADD COLUMN request_id text NOT NULL,
 ADD COLUMN reclaimed_claim_count integer NOT NULL;
ALTER TABLE public.runtime_fencing_proofs
 ADD CONSTRAINT runtime_fencing_proofs_request_id_check
  CHECK (octet_length(request_id) BETWEEN 1 AND 128 AND request_id~'^[ -~]+$'),
 ADD CONSTRAINT runtime_fencing_proofs_request_id_key UNIQUE(request_id),
 ADD CONSTRAINT runtime_fencing_proofs_reclaimed_claim_count_check
  CHECK (reclaimed_claim_count>=0);

-- The only production membership-evidence time source. It takes no argument,
-- has no login grant, and its body stays exactly clock_timestamp(). It is
-- distinct from the lifecycle and rate samplers so the three test seams cannot
-- interfere.
CREATE FUNCTION public.runtime_sample_membership_time() RETURNS timestamptz
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT clock_timestamp()
$$;

CREATE FUNCTION public.runtime_membership_validate_replica_input(p_replica_id uuid,p_instance_id text,p_release_digest text) RETURNS void
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF p_replica_id IS NULL OR p_replica_id='00000000-0000-0000-0000-000000000000'::uuid
  OR p_instance_id IS NULL OR octet_length(p_instance_id)>19 OR p_instance_id!~'^i-[0-9a-f]{17}$'
  OR p_release_digest IS NULL OR p_release_digest!~'^sha256:[0-9a-f]{64}$' THEN
  RAISE EXCEPTION USING ERRCODE='22023',MESSAGE='membership evidence input is invalid';
 END IF;
END $$;

CREATE FUNCTION public.runtime_membership_validate_evidence_text(p_value text) RETURNS void
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF p_value IS NULL OR octet_length(p_value) NOT BETWEEN 1 AND 128 OR p_value!~'^[ -~]+$' THEN
  RAISE EXCEPTION USING ERRCODE='22023',MESSAGE='membership evidence input is invalid';
 END IF;
END $$;

CREATE FUNCTION public.runtime_membership_validate_generation(p_value bigint) RETURNS void
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF p_value IS NULL OR p_value<=0 THEN
  RAISE EXCEPTION USING ERRCODE='22023',MESSAGE='membership evidence input is invalid';
 END IF;
END $$;

-- External EC2 evidence shape only. The two times are stored raw and impose no
-- database-clock skew ceiling; only their mutual order is required.
CREATE FUNCTION public.runtime_membership_validate_proof_evidence(p_requested_at timestamptz,
 p_observed_terminated_at timestamptz,p_observed_state text) RETURNS void
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF p_requested_at IS NULL OR p_observed_terminated_at IS NULL
  OR p_observed_terminated_at<p_requested_at
  OR p_observed_state IS DISTINCT FROM 'terminated' THEN
  RAISE EXCEPTION USING ERRCODE='22023',MESSAGE='membership evidence input is invalid';
 END IF;
END $$;

-- Locks the public singleton, then the capacity singleton. Every evidence
-- operation takes these two row locks first and in this order.
CREATE FUNCTION public.runtime_membership_lock_singletons() RETURNS public.runtime_capacity
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE capacity_row public.runtime_capacity;
BEGIN
 PERFORM 1 FROM public.public_state WHERE singleton FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='runtime singleton state is missing'; END IF;
 SELECT * INTO capacity_row FROM public.runtime_capacity WHERE singleton FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='runtime capacity is missing'; END IF;
 RETURN capacity_row;
END $$;

CREATE FUNCTION public.runtime_membership_load_target(p_replica_id uuid,p_instance_id text,p_release_digest text)
RETURNS public.runtime_replicas LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE target public.runtime_replicas;
BEGIN
 SELECT * INTO target FROM public.runtime_replicas r WHERE r.replica_id=p_replica_id FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='membership replica is unavailable'; END IF;
 IF target.instance_id<>p_instance_id OR target.release_digest<>p_release_digest THEN
  RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='membership replica identity conflicts';
 END IF;
 RETURN target;
END $$;

CREATE FUNCTION public.runtime_membership_replica_high(p_replica public.runtime_replicas) RETURNS timestamptz
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT GREATEST(p_replica.joined_at,p_replica.join_ready_at,p_replica.activated_at,p_replica.draining_at,
  p_replica.left_at,p_replica.termination_requested_at,p_replica.fenced_at)
$$;

CREATE FUNCTION public.runtime_membership_operation_high(p_operation_id text) RETURNS timestamptz
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT GREATEST(
  (SELECT o.created_at FROM public.runtime_lifecycle_operations o WHERE o.operation_id=p_operation_id),
  (SELECT max(s.recorded_at) FROM public.runtime_lifecycle_operation_steps s WHERE s.operation_id=p_operation_id))
$$;

-- Prior database-clock evidence for one replica. The proof's raw external
-- requested_at and observed_terminated_at are deliberately excluded: they are
-- diagnostic input, not ordering authority.
CREATE FUNCTION public.runtime_membership_evidence_high(p_replica_id uuid) RETURNS timestamptz
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT GREATEST(
  (SELECT i.requested_at FROM public.runtime_termination_intents i WHERE i.replica_id=p_replica_id),
  (SELECT r.recorded_at FROM public.runtime_leave_receipts r WHERE r.replica_id=p_replica_id),
  (SELECT f.recorded_at FROM public.runtime_fencing_proofs f WHERE f.replica_id=p_replica_id))
$$;

CREATE FUNCTION public.runtime_membership_termination_intent(p_replica_id uuid)
RETURNS public.runtime_termination_intents LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE intent public.runtime_termination_intents;
BEGIN
 SELECT * INTO intent FROM public.runtime_termination_intents i WHERE i.replica_id=p_replica_id FOR UPDATE;
 RETURN intent;
END $$;

CREATE FUNCTION public.runtime_membership_leave_receipt(p_replica_id uuid)
RETURNS public.runtime_leave_receipts LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE receipt public.runtime_leave_receipts;
BEGIN
 SELECT * INTO receipt FROM public.runtime_leave_receipts r WHERE r.replica_id=p_replica_id FOR UPDATE;
 RETURN receipt;
END $$;

CREATE FUNCTION public.runtime_membership_fencing_proof(p_replica_id uuid)
RETURNS public.runtime_fencing_proofs LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE proof public.runtime_fencing_proofs;
BEGIN
 SELECT * INTO proof FROM public.runtime_fencing_proofs f WHERE f.replica_id=p_replica_id FOR UPDATE;
 RETURN proof;
END $$;

-- Only the capacity generation moves here, so only its headroom is checked.
CREATE FUNCTION public.runtime_membership_assert_generation_room(p_capacity public.runtime_capacity) RETURNS void
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF p_capacity.generation>=9223372036854775807 THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='membership generation is exhausted';
 END IF;
END $$;

-- Advances the membership generation exactly once. Controller generation,
-- controller operation, desired capacity, admission and lifecycle phase are
-- untouched, and no rate partition flag or debt row is read or written.
CREATE FUNCTION public.runtime_membership_advance_capacity(p_updated_by text,p_effective timestamptz)
RETURNS public.runtime_capacity LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE capacity_row public.runtime_capacity;
BEGIN
 UPDATE public.runtime_capacity SET generation=generation+1,updated_at=p_effective,updated_by=p_updated_by
  WHERE singleton RETURNING * INTO capacity_row;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='runtime capacity is missing'; END IF;
 RETURN capacity_row;
END $$;

-- Locks this replica's live claim parents in ascending claim UUID order and
-- returns how many it locked.
CREATE FUNCTION public.runtime_membership_lock_live_claims(p_replica_id uuid) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE live integer;
BEGIN
 SELECT count(*)::integer INTO live FROM (
  SELECT r.claim_id FROM public.shared_claim_requests r
   WHERE r.replica_id=p_replica_id AND r.state IN ('waiting','running')
   ORDER BY r.claim_id FOR UPDATE) locked;
 RETURN live;
END $$;

-- One nonlocking visibility predicate. A transition parent is never locked and
-- never waited on; a nonzero count is a fail-closed leave precondition.
CREATE FUNCTION public.runtime_membership_owned_transition_count(p_replica_id uuid) RETURNS integer
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT count(*)::integer FROM public.public_transitions t
  WHERE t.state IN ('closing','unresolved')
   AND (t.initiator_replica_id=p_replica_id
    OR EXISTS(SELECT 1 FROM public.public_transition_replicas p
      WHERE p.transition_id=t.transition_id AND p.replica_id=p_replica_id))
$$;

-- The immutable prepare step is read as durable selection evidence. It takes
-- no ledger row lock, so leave creates no replica-to-ledger wait edge.
CREATE FUNCTION public.runtime_membership_prepare_step(p_operation_id text,p_workflow text,p_action text,p_replica_id uuid)
RETURNS public.runtime_lifecycle_operation_steps LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE step public.runtime_lifecycle_operation_steps;
BEGIN
 SELECT * INTO step FROM public.runtime_lifecycle_operation_steps s
  WHERE s.operation_id=p_operation_id AND s.action=p_action AND s.workflow_kind=p_workflow
    AND s.replica_id=p_replica_id;
 IF NOT FOUND THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='membership leave evidence is unavailable';
 END IF;
 RETURN step;
END $$;

-- A globally unique request ID may name only this replica's intent or proof.
CREATE FUNCTION public.runtime_membership_assert_request_identity(p_request_id text,p_replica_id uuid) RETURNS void
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM public.runtime_termination_intents i
   WHERE i.request_id=p_request_id AND i.replica_id<>p_replica_id)
  OR EXISTS(SELECT 1 FROM public.runtime_fencing_proofs f
   WHERE f.request_id=p_request_id AND f.replica_id<>p_replica_id) THEN
  RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='membership evidence identity conflicts';
 END IF;
END $$;

CREATE FUNCTION public.runtime_membership_assert_evidence_identity(p_evidence_id text,p_replica_id uuid) RETURNS void
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM public.runtime_fencing_proofs f
   WHERE f.evidence_id=p_evidence_id AND f.replica_id<>p_replica_id) THEN
  RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='membership evidence identity conflicts';
 END IF;
END $$;

-- Stored receipt integrity, proven before any caller comparison. A receipt
-- whose retained shape or member relation is impossible is AM001, never a
-- caller conflict.
CREATE FUNCTION public.runtime_membership_assert_receipt_shape(p_receipt public.runtime_leave_receipts,
 p_replica public.runtime_replicas) RETURNS void
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF p_receipt.replica_id IS DISTINCT FROM p_replica.replica_id
  OR p_receipt.instance_id IS DISTINCT FROM p_replica.instance_id
  OR p_receipt.release_digest IS DISTINCT FROM p_replica.release_digest
  OR p_receipt.operation_id IS NULL
  OR p_receipt.controller_generation IS NULL OR p_receipt.controller_generation<=0
  OR p_receipt.joined_transition_count IS DISTINCT FROM 0
  OR p_receipt.joined_claim_count IS DISTINCT FROM 0
  OR p_receipt.recorded_at IS NULL
  OR p_replica.state<>'left' THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='membership leave receipt is corrupt';
 END IF;
END $$;

CREATE FUNCTION public.runtime_membership_assert_proof_shape(p_proof public.runtime_fencing_proofs,
 p_replica public.runtime_replicas) RETURNS void
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF p_proof.replica_id IS DISTINCT FROM p_replica.replica_id
  OR p_proof.instance_id IS DISTINCT FROM p_replica.instance_id
  OR p_proof.release_digest IS DISTINCT FROM p_replica.release_digest
  OR p_proof.adapter IS DISTINCT FROM 'ec2_terminated_v1'
  OR p_proof.observed_state IS DISTINCT FROM 'terminated'
  OR p_proof.request_id IS NULL OR p_proof.evidence_id IS NULL
  OR p_proof.requested_at IS NULL OR p_proof.observed_terminated_at IS NULL
  OR p_proof.observed_terminated_at<p_proof.requested_at
  OR p_proof.recorded_at IS NULL
  OR p_proof.reclaimed_claim_count IS NULL OR p_proof.reclaimed_claim_count<0
  OR p_replica.state NOT IN ('left','fenced')
  OR (p_replica.state='left' AND p_proof.reclaimed_claim_count<>0) THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='membership fencing proof is corrupt';
 END IF;
END $$;

CREATE FUNCTION public.runtime_membership_leave_value(p_replica_id uuid,p_state text,p_operation_id text,
 p_controller_generation bigint,p_recorded_at timestamptz,p_replayed boolean) RETURNS public.runtime_leave_result
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT ROW(p_replica_id,p_state,p_operation_id,p_controller_generation,0,0,p_recorded_at,
  p_replayed)::public.runtime_leave_result
$$;

CREATE FUNCTION public.runtime_membership_fence_value(p_replica_id uuid,p_state text,p_evidence_id text,
 p_reclaimed_claim_count integer,p_recorded_at timestamptz,p_replayed boolean) RETURNS public.runtime_fence_result
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT ROW(p_replica_id,p_state,p_evidence_id,p_reclaimed_claim_count,p_recorded_at,
  p_replayed)::public.runtime_fence_result
$$;

CREATE FUNCTION public.runtime_membership_record_receipt(p_replica_id uuid,p_instance_id text,p_release_digest text,
 p_controller_generation bigint,p_operation_id text,p_recorded_at timestamptz)
RETURNS public.runtime_leave_receipts LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE receipt public.runtime_leave_receipts;
BEGIN
 INSERT INTO public.runtime_leave_receipts(replica_id,instance_id,release_digest,controller_generation,
  operation_id,joined_transition_count,joined_claim_count,recorded_at)
 VALUES(p_replica_id,p_instance_id,p_release_digest,p_controller_generation,p_operation_id,0,0,p_recorded_at)
 RETURNING * INTO receipt;
 RETURN receipt;
EXCEPTION
 WHEN unique_violation OR foreign_key_violation THEN
  RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='membership leave receipt conflicts';
 WHEN check_violation OR not_null_violation THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='membership leave receipt is corrupt';
END $$;

CREATE FUNCTION public.runtime_membership_record_proof(p_replica_id uuid,p_instance_id text,p_release_digest text,
 p_request_id text,p_evidence_id text,p_requested_at timestamptz,p_observed_terminated_at timestamptz,
 p_observed_state text,p_reclaimed_claim_count integer,p_recorded_at timestamptz)
RETURNS public.runtime_fencing_proofs LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE proof public.runtime_fencing_proofs;
BEGIN
 INSERT INTO public.runtime_fencing_proofs(replica_id,instance_id,release_digest,adapter,request_id,evidence_id,
  requested_at,observed_terminated_at,recorded_at,observed_state,reclaimed_claim_count)
 VALUES(p_replica_id,p_instance_id,p_release_digest,'ec2_terminated_v1',p_request_id,p_evidence_id,
  p_requested_at,p_observed_terminated_at,p_recorded_at,p_observed_state,p_reclaimed_claim_count)
 RETURNING * INTO proof;
 RETURN proof;
EXCEPTION
 WHEN unique_violation OR foreign_key_violation THEN
  RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='membership evidence identity conflicts';
 WHEN check_violation OR not_null_violation THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='membership fencing proof is corrupt';
END $$;

CREATE FUNCTION public.runtime_membership_set_state(p_replica_id uuid,p_state text,p_effective timestamptz)
RETURNS public.runtime_replicas LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE target public.runtime_replicas;
BEGIN
 UPDATE public.runtime_replicas
  SET state=p_state,
      left_at=CASE WHEN p_state='left' THEN p_effective ELSE left_at END,
      fenced_at=CASE WHEN p_state='fenced' THEN p_effective ELSE fenced_at END
  WHERE replica_id=p_replica_id RETURNING * INTO target;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='membership replica is missing'; END IF;
 RETURN target;
END $$;

-- Owner-only fenced cleanup. It hard-codes the fenced reason, takes no reason
-- argument, has no login grant and no Go entry point, and
-- runtime_record_ec2_termination is its only caller. It neither enters nor
-- finishes a transaction. There is no fixed row or work bound: the caller's
-- bounded context governs, every live parent of the exact replica is released
-- in this one transaction, and no age, page, cap or partial commit exists.
CREATE FUNCTION public.runtime_release_fenced_replica_claims(p_replica_id uuid,p_effective_at timestamptz) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE ids uuid[]; item record; released integer;
BEGIN
 IF p_replica_id IS NULL OR p_effective_at IS NULL THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='fenced claim release input is invalid';
 END IF;
 SELECT array_agg(locked.claim_id ORDER BY locked.claim_id) INTO ids FROM (
  SELECT r.claim_id FROM public.shared_claim_requests r
   WHERE r.replica_id=p_replica_id AND r.state IN ('waiting','running')
   ORDER BY r.claim_id FOR UPDATE) locked;
 IF ids IS NULL THEN RETURN 0; END IF;
 FOR item IN SELECT DISTINCT c.policy_id,c.scope_kind FROM public.shared_claim_scopes c
   WHERE c.claim_id=ANY(ids) ORDER BY c.policy_id,c.scope_kind LOOP
  PERFORM 1 FROM public.shared_claim_policies p
   WHERE p.policy_id=item.policy_id AND p.scope_kind=item.scope_kind FOR UPDATE;
  IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='claim policy catalog is invalid'; END IF;
 END LOOP;
 FOR item IN SELECT DISTINCT c.policy_id,c.scope_kind,c.scope_digest FROM public.shared_claim_scopes c
   WHERE c.claim_id=ANY(ids) ORDER BY c.policy_id,c.scope_kind,c.scope_digest LOOP
  PERFORM 1 FROM public.shared_claim_scope_summaries s
   WHERE s.policy_id=item.policy_id AND s.scope_kind=item.scope_kind AND s.scope_digest=item.scope_digest FOR UPDATE;
  IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='stored claim summary is invalid'; END IF;
 END LOOP;
 IF EXISTS(SELECT 1 FROM public.shared_claim_requests r WHERE r.claim_id=ANY(ids)
    AND (SELECT count(*) FROM public.shared_claim_scopes c WHERE c.claim_id=r.claim_id)<>r.scope_count)
  OR EXISTS(SELECT 1 FROM public.shared_claim_requests r JOIN public.shared_claim_scopes c ON c.claim_id=r.claim_id
    WHERE r.claim_id=ANY(ids) AND (c.state<>r.state OR c.policy_id<>r.policy_id)) THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='stored claim identity is invalid';
 END IF;
 FOR item IN SELECT c.policy_id,c.scope_kind,c.scope_digest,
    count(*) FILTER (WHERE c.state='running') AS running_scopes,
    count(*) FILTER (WHERE c.state='waiting') AS waiting_scopes
   FROM public.shared_claim_scopes c WHERE c.claim_id=ANY(ids)
   GROUP BY c.policy_id,c.scope_kind,c.scope_digest
   ORDER BY c.policy_id,c.scope_kind,c.scope_digest LOOP
  UPDATE public.shared_claim_scope_summaries s
   SET running_count=s.running_count-item.running_scopes,
       waiting_count=s.waiting_count-item.waiting_scopes,
       updated_at=p_effective_at
   WHERE s.policy_id=item.policy_id AND s.scope_kind=item.scope_kind AND s.scope_digest=item.scope_digest;
 END LOOP;
 UPDATE public.shared_claim_scopes SET state='released' WHERE claim_id=ANY(ids);
 UPDATE public.shared_claim_requests
  SET state='released',released_at=p_effective_at,release_reason='fenced' WHERE claim_id=ANY(ids);
 GET DIAGNOSTICS released=ROW_COUNT;
 IF released IS DISTINCT FROM array_length(ids,1) OR released<0 THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='fenced claim release is inconsistent';
 END IF;
 RETURN released;
EXCEPTION
 WHEN check_violation OR not_null_violation THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='fenced claim release is inconsistent';
END $$;

-- One owner-only fixed-kind graceful-leave implementation behind the two
-- public wrappers. It inserts no lifecycle row, locks no transition parent and
-- releases nothing: SQL proves durable ownership counts while the caller
-- proves its local join.
CREATE FUNCTION public.runtime_membership_finish_graceful_leave(
 p_replica_kind text,p_wrapper text,p_workflow text,p_action text,
 p_replica_id uuid,p_instance_id text,p_release_digest text,
 p_expected_controller_generation bigint,p_operation_id text
) RETURNS public.runtime_leave_result
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE capacity_row public.runtime_capacity; target public.runtime_replicas;
 intent public.runtime_termination_intents; receipt public.runtime_leave_receipts;
 prepared public.runtime_lifecycle_operation_steps; live_claims integer; owned_transitions integer;
 effective_at timestamptz;
BEGIN
 PERFORM public.runtime_require_write_entry();
 PERFORM public.runtime_membership_validate_generation(p_expected_controller_generation);
 PERFORM public.runtime_membership_validate_evidence_text(p_operation_id);
 PERFORM public.runtime_membership_validate_replica_input(p_replica_id,p_instance_id,p_release_digest);
 capacity_row:=public.runtime_membership_lock_singletons();
 target:=public.runtime_membership_load_target(p_replica_id,p_instance_id,p_release_digest);
 IF target.replica_kind<>p_replica_kind THEN
  RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='membership leave kind is forbidden';
 END IF;
 intent:=public.runtime_membership_termination_intent(p_replica_id);
 receipt:=public.runtime_membership_leave_receipt(p_replica_id);
 IF receipt.replica_id IS NOT NULL THEN
  PERFORM public.runtime_membership_assert_receipt_shape(receipt,target);
  IF receipt.instance_id<>p_instance_id OR receipt.release_digest<>p_release_digest
   OR receipt.operation_id<>p_operation_id
   OR receipt.controller_generation<>p_expected_controller_generation THEN
   RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='membership leave receipt conflicts';
  END IF;
  RETURN public.runtime_membership_leave_value(target.replica_id,target.state,receipt.operation_id,
   receipt.controller_generation,receipt.recorded_at,true);
 END IF;
 IF target.state<>'draining' OR intent.replica_id IS NOT NULL THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='membership leave state is unavailable';
 END IF;
 live_claims:=public.runtime_membership_lock_live_claims(p_replica_id);
 owned_transitions:=public.runtime_membership_owned_transition_count(p_replica_id);
 IF live_claims<>0 OR owned_transitions<>0 THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='membership leave work is unjoined';
 END IF;
 prepared:=public.runtime_membership_prepare_step(p_operation_id,p_workflow,p_action,p_replica_id);
 IF prepared.replica_kind IS DISTINCT FROM p_replica_kind OR prepared.replica_state IS DISTINCT FROM 'draining' THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='membership leave evidence is unavailable';
 END IF;
 IF p_expected_controller_generation<>prepared.result_generation THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='membership leave generation is stale';
 END IF;
 IF capacity_row.controller_generation<prepared.result_generation THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='membership controller generation is corrupt';
 END IF;
 PERFORM public.runtime_membership_assert_generation_room(capacity_row);
 effective_at:=GREATEST(public.runtime_sample_membership_time(),capacity_row.updated_at,
  public.runtime_membership_replica_high(target),public.runtime_membership_operation_high(p_operation_id),
  public.runtime_membership_evidence_high(p_replica_id));
 receipt:=public.runtime_membership_record_receipt(p_replica_id,p_instance_id,p_release_digest,
  prepared.result_generation,p_operation_id,effective_at);
 target:=public.runtime_membership_set_state(p_replica_id,'left',effective_at);
 capacity_row:=public.runtime_membership_advance_capacity(p_wrapper,effective_at);
 RETURN public.runtime_membership_leave_value(target.replica_id,target.state,receipt.operation_id,
  receipt.controller_generation,receipt.recorded_at,false);
END $$;

-- Serving graceful leave binds the immutable prepare_scale_in step.
CREATE FUNCTION public.runtime_finish_serving_graceful_leave(uuid,text,text,bigint,text)
RETURNS public.runtime_leave_result LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF session_user<>'aboutme_app' THEN
  RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='membership leave role is forbidden';
 END IF;
 RETURN public.runtime_membership_finish_graceful_leave('serving','runtime_finish_serving_graceful_leave',
  'scale_in','prepare_scale_in',$1,$2,$3,$4,$5);
END $$;

-- Maintenance graceful leave binds the immutable prepare_maintenance_drain step.
CREATE FUNCTION public.runtime_finish_maintenance_graceful_leave(uuid,text,text,bigint,text)
RETURNS public.runtime_leave_result LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF session_user<>'aboutme_maintenance' THEN
  RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='membership leave role is forbidden';
 END IF;
 RETURN public.runtime_membership_finish_graceful_leave('maintenance','runtime_finish_maintenance_graceful_leave',
  'maintenance_wake','prepare_maintenance_drain',$1,$2,$3,$4,$5);
END $$;

-- Exact EC2 termination proof. It records the caller's authenticated external
-- evidence, never calls AWS, and never infers death from a heartbeat, lock,
-- deadline, ECS, ALB or database loss. It takes no lifecycle lock and neither
-- locks, waits on, reads as a predicate nor changes any public transition.
CREATE FUNCTION public.runtime_record_ec2_termination(
 p_replica_id uuid,p_instance_id text,p_release_digest text,p_request_id text,p_evidence_id text,
 p_requested_at timestamptz,p_observed_terminated_at timestamptz,p_observed_state text
) RETURNS public.runtime_fence_result
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE capacity_row public.runtime_capacity; target public.runtime_replicas;
 intent public.runtime_termination_intents; proof public.runtime_fencing_proofs;
 reclaimed integer; effective_at timestamptz;
BEGIN
 IF session_user<>'aboutme_fencing_proof' THEN
  RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='membership evidence role is forbidden';
 END IF;
 PERFORM public.runtime_require_write_entry();
 PERFORM public.runtime_membership_validate_replica_input(p_replica_id,p_instance_id,p_release_digest);
 PERFORM public.runtime_membership_validate_evidence_text(p_request_id);
 PERFORM public.runtime_membership_validate_evidence_text(p_evidence_id);
 PERFORM public.runtime_membership_validate_proof_evidence(p_requested_at,p_observed_terminated_at,p_observed_state);
 capacity_row:=public.runtime_membership_lock_singletons();
 target:=public.runtime_membership_load_target(p_replica_id,p_instance_id,p_release_digest);
 intent:=public.runtime_membership_termination_intent(p_replica_id);
 proof:=public.runtime_membership_fencing_proof(p_replica_id);
 PERFORM public.runtime_membership_assert_request_identity(p_request_id,p_replica_id);
 PERFORM public.runtime_membership_assert_evidence_identity(p_evidence_id,p_replica_id);
 IF intent.replica_id IS NOT NULL THEN
  IF target.state='left' THEN
   RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='membership termination intent is contradictory';
  END IF;
  IF intent.request_id<>p_request_id THEN
   RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='membership evidence identity conflicts';
  END IF;
 END IF;
 IF proof.replica_id IS NOT NULL THEN
  PERFORM public.runtime_membership_assert_proof_shape(proof,target);
  IF proof.instance_id<>p_instance_id OR proof.release_digest<>p_release_digest
   OR proof.request_id<>p_request_id OR proof.evidence_id<>p_evidence_id
   OR proof.requested_at<>p_requested_at OR proof.observed_terminated_at<>p_observed_terminated_at
   OR proof.observed_state<>p_observed_state THEN
   RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='membership fencing proof conflicts';
  END IF;
  RETURN public.runtime_membership_fence_value(target.replica_id,target.state,proof.evidence_id,
   proof.reclaimed_claim_count,proof.recorded_at,true);
 END IF;
 IF target.state NOT IN ('joining','active','draining','terminating','left') THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='membership fenced state is contradictory';
 END IF;
 PERFORM public.runtime_membership_assert_generation_room(capacity_row);
 effective_at:=GREATEST(public.runtime_sample_membership_time(),capacity_row.updated_at,
  public.runtime_membership_replica_high(target),public.runtime_membership_evidence_high(p_replica_id));
 IF target.state='left' THEN
  reclaimed:=0;
 ELSE
  reclaimed:=public.runtime_release_fenced_replica_claims(p_replica_id,effective_at);
  target:=public.runtime_membership_set_state(p_replica_id,'fenced',effective_at);
 END IF;
 IF reclaimed IS NULL OR reclaimed<0 THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='fenced claim release is inconsistent';
 END IF;
 proof:=public.runtime_membership_record_proof(p_replica_id,p_instance_id,p_release_digest,p_request_id,
  p_evidence_id,p_requested_at,p_observed_terminated_at,p_observed_state,reclaimed,effective_at);
 capacity_row:=public.runtime_membership_advance_capacity('runtime_record_ec2_termination',effective_at);
 RETURN public.runtime_membership_fence_value(target.replica_id,target.state,proof.evidence_id,
  proof.reclaimed_claim_count,proof.recorded_at,false);
END $$;

REVOKE ALL ON FUNCTION
 public.runtime_sample_membership_time(),
 public.runtime_membership_validate_replica_input(uuid,text,text),
 public.runtime_membership_validate_evidence_text(text),
 public.runtime_membership_validate_generation(bigint),
 public.runtime_membership_validate_proof_evidence(timestamptz,timestamptz,text),
 public.runtime_membership_lock_singletons(),
 public.runtime_membership_load_target(uuid,text,text),
 public.runtime_membership_replica_high(public.runtime_replicas),
 public.runtime_membership_operation_high(text),
 public.runtime_membership_evidence_high(uuid),
 public.runtime_membership_termination_intent(uuid),
 public.runtime_membership_leave_receipt(uuid),
 public.runtime_membership_fencing_proof(uuid),
 public.runtime_membership_assert_generation_room(public.runtime_capacity),
 public.runtime_membership_advance_capacity(text,timestamptz),
 public.runtime_membership_lock_live_claims(uuid),
 public.runtime_membership_owned_transition_count(uuid),
 public.runtime_membership_prepare_step(text,text,text,uuid),
 public.runtime_membership_assert_request_identity(text,uuid),
 public.runtime_membership_assert_evidence_identity(text,uuid),
 public.runtime_membership_assert_receipt_shape(public.runtime_leave_receipts,public.runtime_replicas),
 public.runtime_membership_assert_proof_shape(public.runtime_fencing_proofs,public.runtime_replicas),
 public.runtime_membership_leave_value(uuid,text,text,bigint,timestamptz,boolean),
 public.runtime_membership_fence_value(uuid,text,text,integer,timestamptz,boolean),
 public.runtime_membership_record_receipt(uuid,text,text,bigint,text,timestamptz),
 public.runtime_membership_record_proof(uuid,text,text,text,text,timestamptz,timestamptz,text,integer,timestamptz),
 public.runtime_membership_set_state(uuid,text,timestamptz),
 public.runtime_membership_finish_graceful_leave(text,text,text,text,uuid,text,text,bigint,text),
 public.runtime_release_fenced_replica_claims(uuid,timestamptz)
 FROM PUBLIC,aboutme_app,aboutme_maintenance,aboutme_lifecycle_command,aboutme_fencing_proof,aboutme_restore_verify,aboutme_migrator;

REVOKE ALL ON FUNCTION
 public.runtime_finish_serving_graceful_leave(uuid,text,text,bigint,text),
 public.runtime_finish_maintenance_graceful_leave(uuid,text,text,bigint,text),
 public.runtime_record_ec2_termination(uuid,text,text,text,text,timestamptz,timestamptz,text)
 FROM PUBLIC,aboutme_app,aboutme_maintenance,aboutme_lifecycle_command,aboutme_fencing_proof,aboutme_restore_verify,aboutme_migrator;

GRANT USAGE ON TYPE public.runtime_leave_result TO aboutme_app,aboutme_maintenance;
GRANT USAGE ON TYPE public.runtime_fence_result TO aboutme_fencing_proof;
GRANT EXECUTE ON FUNCTION public.runtime_finish_serving_graceful_leave(uuid,text,text,bigint,text) TO aboutme_app;
GRANT EXECUTE ON FUNCTION public.runtime_finish_maintenance_graceful_leave(uuid,text,text,bigint,text) TO aboutme_maintenance;
GRANT EXECUTE ON FUNCTION public.runtime_record_ec2_termination(uuid,text,text,text,text,timestamptz,timestamptz,text) TO aboutme_fencing_proof;

RESET ROLE;
REVOKE CREATE ON SCHEMA public FROM aboutme_runtime_owner;
SELECT public.runtime_finish_write();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 1;
-- +goose StatementEnd
