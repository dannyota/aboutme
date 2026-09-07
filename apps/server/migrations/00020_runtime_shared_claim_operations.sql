-- +goose Up
-- +goose StatementBegin
SELECT public.runtime_begin_migration_write('migration-00020');

RESET ROLE;
GRANT CREATE ON SCHEMA public TO aboutme_runtime_owner;
SET LOCAL ROLE aboutme_runtime_owner;

CREATE TYPE public.runtime_claim_result AS (
 outcome text, claim_id uuid, policy_id text, replica_id uuid, work_id uuid,
 state text, admitted_at timestamptz, deadline_at timestamptz,
 released_at timestamptz, release_reason text, request_digest bytea,
 scope_count smallint, scope_1_kind text, scope_1_digest bytea,
 scope_1_allocation_ordinal bigint, scope_2_kind text, scope_2_digest bytea,
 scope_2_allocation_ordinal bigint
);

CREATE FUNCTION public.runtime_claim_input_digest(
 p_claim_id uuid,p_policy_id text,p_replica_id uuid,p_work_id uuid,
 p_scope_count smallint,p_scope_1_kind text,p_scope_1_digest bytea,
 p_scope_2_kind text,p_scope_2_digest bytea
) RETURNS bytea LANGUAGE sql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT sha256(convert_to('aboutme.shared-claim.request.v1','UTF8')||decode('00','hex')||decode('01','hex')||uuid_send(p_claim_id)
  ||decode(lpad(to_hex(octet_length(convert_to(p_policy_id,'UTF8'))),2,'0'),'hex')||convert_to(p_policy_id,'UTF8')||uuid_send(p_replica_id)
  ||CASE WHEN p_work_id IS NULL THEN decode('00','hex') ELSE decode('01','hex')||uuid_send(p_work_id) END
  ||decode(lpad(to_hex(p_scope_count::integer),2,'0'),'hex')
  ||decode('01','hex')||decode(lpad(to_hex(octet_length(convert_to(p_scope_1_kind,'UTF8'))),2,'0'),'hex')||convert_to(p_scope_1_kind,'UTF8')||p_scope_1_digest
  ||CASE WHEN p_scope_count=2 THEN decode('02','hex')||decode(lpad(to_hex(octet_length(convert_to(p_scope_2_kind,'UTF8'))),2,'0'),'hex')||convert_to(p_scope_2_kind,'UTF8')||p_scope_2_digest ELSE ''::bytea END)
$$;

CREATE FUNCTION public.runtime_claim_validate_role(p_policy_id text,p_replica_kind text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$ BEGIN
 IF session_user='aboutme_app' THEN
  IF p_replica_kind IS DISTINCT FROM 'serving' THEN RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='claim role is forbidden'; END IF;
 ELSIF session_user='aboutme_maintenance' THEN
  IF p_replica_kind IS DISTINCT FROM 'maintenance' OR p_policy_id IS DISTINCT FROM 'mail.send' THEN RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='claim role is forbidden'; END IF;
 ELSE RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='claim role is forbidden';
 END IF;
END $$;

CREATE FUNCTION public.runtime_claim_validate_input(p_claim_id uuid,p_replica_id uuid,p_digest bytea) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$ BEGIN
 IF p_claim_id IS NULL OR p_claim_id='00000000-0000-0000-0000-000000000000'::uuid OR p_replica_id IS NULL OR p_replica_id='00000000-0000-0000-0000-000000000000'::uuid OR p_digest IS NULL OR octet_length(p_digest)<>32 THEN
  RAISE EXCEPTION USING ERRCODE='22023',MESSAGE='claim input is invalid';
 END IF;
END $$;

CREATE FUNCTION public.runtime_claim_absent(p_claim_id uuid) RETURNS public.runtime_claim_result
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT ROW('absent',p_claim_id,NULL::text,NULL::uuid,NULL::uuid,NULL::text,NULL::timestamptz,NULL::timestamptz,NULL::timestamptz,NULL::text,NULL::bytea,NULL::smallint,NULL::text,NULL::bytea,NULL::bigint,NULL::text,NULL::bytea,NULL::bigint)::public.runtime_claim_result
$$;

-- Locks the parent and proves its stored identity before any caller comparison.
-- Returns NULL for an absent parent and raises AM001 for stored corruption.
CREATE FUNCTION public.runtime_claim_load(p_claim_id uuid) RETURNS public.shared_claim_requests
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE p public.shared_claim_requests; stored_kind text; child_count integer; children_ok boolean;
BEGIN
 SELECT * INTO p FROM public.shared_claim_requests WHERE claim_id=p_claim_id FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL; END IF;
 SELECT r.replica_kind INTO stored_kind FROM public.runtime_replicas r WHERE r.replica_id=p.replica_id;
 IF stored_kind IS NULL THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='stored claim replica is invalid'; END IF;
 PERFORM public.runtime_claim_validate_role(p.policy_id,stored_kind);
 SELECT count(*),COALESCE(bool_and(state=p.state AND policy_id=p.policy_id AND request_ordinal BETWEEN 1 AND p.scope_count),false)
  INTO child_count,children_ok FROM public.shared_claim_scopes WHERE claim_id=p.claim_id;
 IF child_count<>p.scope_count OR NOT children_ok OR public.runtime_shared_claim_request_digest(p.claim_id) IS DISTINCT FROM p.request_digest THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='stored claim identity is invalid';
 END IF;
 RETURN p;
END $$;

CREATE FUNCTION public.runtime_claim_result_for(p public.shared_claim_requests) RETURNS public.runtime_claim_result
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE s1 public.shared_claim_scopes; s2 public.shared_claim_scopes;
BEGIN
 SELECT * INTO s1 FROM public.shared_claim_scopes WHERE claim_id=p.claim_id AND request_ordinal=1;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='stored claim scope set is invalid'; END IF;
 IF p.scope_count=2 THEN
  SELECT * INTO s2 FROM public.shared_claim_scopes WHERE claim_id=p.claim_id AND request_ordinal=2;
  IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='stored claim scope set is invalid'; END IF;
 END IF;
 RETURN ROW(p.state,p.claim_id,p.policy_id,p.replica_id,p.work_id,p.state,p.admitted_at,p.deadline_at,p.released_at,p.release_reason,p.request_digest,p.scope_count,
  s1.scope_kind,s1.scope_digest,s1.allocation_ordinal,s2.scope_kind,s2.scope_digest,s2.allocation_ordinal)::public.runtime_claim_result;
END $$;

-- Loads and validates the stored parent, then checks the caller's replica and
-- request digest. Stored corruption is AM001; a caller conflict is AM002.
CREATE FUNCTION public.runtime_claim_assert_request(p_claim_id uuid,p_replica_id uuid,p_digest bytea) RETURNS public.shared_claim_requests
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE p public.shared_claim_requests;
BEGIN
 PERFORM public.runtime_claim_validate_input(p_claim_id,p_replica_id,p_digest);
 p:=public.runtime_claim_load(p_claim_id);
 IF p.claim_id IS NULL THEN RETURN NULL; END IF;
 IF p.replica_id<>p_replica_id OR p.request_digest<>p_digest THEN RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='claim identity conflicts'; END IF;
 RETURN p;
END $$;

CREATE FUNCTION public.runtime_claim_acquire(
 p_claim_id uuid,p_policy_id text,p_replica_id uuid,p_work_id uuid,
 p_scope_count smallint,p_scope_1_kind text,p_scope_1_digest bytea,
 p_scope_2_kind text,p_scope_2_digest bytea
) RETURNS public.runtime_claim_result LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE cap public.runtime_capacity; replica public.runtime_replicas; replica_found boolean; existing public.shared_claim_requests;
 c1 public.shared_claim_policies; c2 public.shared_claim_policies; s1 public.shared_claim_scope_summaries; s2 public.shared_claim_scope_summaries;
 scope record; shape_ok boolean; digest bytea; admitted timestamptz; chosen text;
BEGIN
 PERFORM public.runtime_require_write_entry();
 IF session_user NOT IN ('aboutme_app','aboutme_maintenance') THEN RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='claim role is forbidden'; END IF;
 shape_ok:=p_claim_id IS NOT NULL AND p_claim_id<>'00000000-0000-0000-0000-000000000000'::uuid
  AND p_policy_id IS NOT NULL AND p_replica_id IS NOT NULL AND p_replica_id<>'00000000-0000-0000-0000-000000000000'::uuid
  AND p_scope_count IN (1,2) AND p_scope_1_kind IS NOT NULL AND p_scope_1_digest IS NOT NULL AND octet_length(p_scope_1_digest)=32
  AND ((p_scope_count=1 AND p_scope_2_kind IS NULL AND p_scope_2_digest IS NULL)
   OR (p_scope_count=2 AND p_scope_2_kind IS NOT NULL AND p_scope_2_digest IS NOT NULL AND octet_length(p_scope_2_digest)=32))
  AND ((p_policy_id='render.global_claim' AND p_work_id IS NOT NULL AND p_work_id<>'00000000-0000-0000-0000-000000000000'::uuid)
   OR (p_policy_id<>'render.global_claim' AND p_work_id IS NULL))
  AND ((p_policy_id IN ('render.global_claim','password.hash','mail.send') AND p_scope_count=1 AND p_scope_1_kind='global')
   OR (p_policy_id='mcp.user_concurrent' AND p_scope_count=1 AND p_scope_1_kind='user')
   OR (p_policy_id='sse.fleet_account_ip' AND p_scope_1_kind='ip' AND (p_scope_count=1 OR p_scope_2_kind='account')));
 IF shape_ok IS DISTINCT FROM true THEN RAISE EXCEPTION USING ERRCODE='22023',MESSAGE='claim input is invalid'; END IF;
 IF session_user='aboutme_maintenance' AND p_policy_id<>'mail.send' THEN RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='claim role is forbidden'; END IF;
 digest:=public.runtime_claim_input_digest(p_claim_id,p_policy_id,p_replica_id,p_work_id,p_scope_count,p_scope_1_kind,p_scope_1_digest,p_scope_2_kind,p_scope_2_digest);
 SELECT * INTO cap FROM public.runtime_capacity WHERE singleton FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='runtime capacity is missing'; END IF;
 SELECT * INTO replica FROM public.runtime_replicas WHERE replica_id=p_replica_id FOR UPDATE;
 replica_found:=FOUND;
 existing:=public.runtime_claim_load(p_claim_id);
 IF existing.claim_id IS NOT NULL THEN
  IF existing.policy_id<>p_policy_id OR existing.replica_id<>p_replica_id OR existing.work_id IS DISTINCT FROM p_work_id OR existing.scope_count<>p_scope_count OR existing.request_digest<>digest THEN
   RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='claim identity conflicts';
  END IF;
  RETURN public.runtime_claim_result_for(existing);
 END IF;
 IF NOT replica_found THEN RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='claim replica is unavailable'; END IF;
 PERFORM public.runtime_claim_validate_role(p_policy_id,replica.replica_kind);
 IF NOT cap.admission_enabled OR cap.lifecycle_phase<>'online' OR replica.state<>'active' THEN RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='claim admission is unavailable'; END IF;
 FOR scope IN SELECT v.kind FROM (VALUES (p_scope_1_kind),(p_scope_2_kind)) AS v(kind) WHERE v.kind IS NOT NULL ORDER BY v.kind LOOP
  PERFORM 1 FROM public.shared_claim_policies WHERE policy_id=p_policy_id AND scope_kind=scope.kind FOR UPDATE;
  IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='claim policy catalog is invalid'; END IF;
 END LOOP;
 SELECT * INTO c1 FROM public.shared_claim_policies WHERE policy_id=p_policy_id AND scope_kind=p_scope_1_kind;
 IF p_scope_count=2 THEN SELECT * INTO c2 FROM public.shared_claim_policies WHERE policy_id=p_policy_id AND scope_kind=p_scope_2_kind; END IF;
 FOR scope IN SELECT v.kind,v.digest FROM (VALUES (p_scope_1_kind,p_scope_1_digest),(p_scope_2_kind,p_scope_2_digest)) AS v(kind,digest) WHERE v.kind IS NOT NULL ORDER BY v.kind,v.digest LOOP
  PERFORM 1 FROM public.shared_claim_scope_summaries WHERE policy_id=p_policy_id AND scope_kind=scope.kind AND scope_digest=scope.digest FOR UPDATE;
 END LOOP;
 SELECT * INTO s1 FROM public.shared_claim_scope_summaries WHERE policy_id=p_policy_id AND scope_kind=p_scope_1_kind AND scope_digest=p_scope_1_digest;
 IF p_scope_count=2 THEN SELECT * INTO s2 FROM public.shared_claim_scope_summaries WHERE policy_id=p_policy_id AND scope_kind=p_scope_2_kind AND scope_digest=p_scope_2_digest; END IF;
 IF COALESCE(s1.next_ordinal,1)=9223372036854775807 OR (p_scope_count=2 AND COALESCE(s2.next_ordinal,1)=9223372036854775807) THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='claim allocation is unavailable';
 END IF;
 IF COALESCE(s1.running_count,0)<c1.running_limit AND (p_scope_count=1 OR COALESCE(s2.running_count,0)<c2.running_limit) THEN chosen:='running';
 ELSIF c1.queue_enabled AND COALESCE(s1.waiting_count,0)<c1.waiting_limit AND (p_scope_count=1 OR (c2.queue_enabled AND COALESCE(s2.waiting_count,0)<c2.waiting_limit)) THEN chosen:='waiting';
 ELSE
  RETURN ROW('denied',p_claim_id,p_policy_id,p_replica_id,p_work_id,NULL::text,NULL::timestamptz,NULL::timestamptz,NULL::timestamptz,NULL::text,digest,p_scope_count,p_scope_1_kind,p_scope_1_digest,NULL::bigint,p_scope_2_kind,p_scope_2_digest,NULL::bigint)::public.runtime_claim_result;
 END IF;
 admitted:=clock_timestamp();
 FOR scope IN SELECT v.kind,v.digest FROM (VALUES (p_scope_1_kind,p_scope_1_digest),(p_scope_2_kind,p_scope_2_digest)) AS v(kind,digest) WHERE v.kind IS NOT NULL ORDER BY v.kind,v.digest LOOP
  INSERT INTO public.shared_claim_scope_summaries(policy_id,scope_kind,scope_digest,running_count,waiting_count,next_ordinal,updated_at)
   VALUES(p_policy_id,scope.kind,scope.digest,0,0,1,admitted) ON CONFLICT (policy_id,scope_kind,scope_digest) DO NOTHING;
  PERFORM 1 FROM public.shared_claim_scope_summaries WHERE policy_id=p_policy_id AND scope_kind=scope.kind AND scope_digest=scope.digest FOR UPDATE;
 END LOOP;
 SELECT * INTO s1 FROM public.shared_claim_scope_summaries WHERE policy_id=p_policy_id AND scope_kind=p_scope_1_kind AND scope_digest=p_scope_1_digest;
 IF p_scope_count=2 THEN SELECT * INTO s2 FROM public.shared_claim_scope_summaries WHERE policy_id=p_policy_id AND scope_kind=p_scope_2_kind AND scope_digest=p_scope_2_digest; END IF;
 INSERT INTO public.shared_claim_requests(claim_id,policy_id,replica_id,work_id,state,admitted_at,deadline_at,scope_count,request_digest)
  VALUES(p_claim_id,p_policy_id,p_replica_id,p_work_id,chosen,admitted,CASE WHEN p_policy_id='render.global_claim' THEN admitted+interval '20 seconds' END,p_scope_count,digest);
 INSERT INTO public.shared_claim_scopes(claim_id,request_ordinal,policy_id,scope_kind,scope_digest,allocation_ordinal,state)
  VALUES(p_claim_id,1,p_policy_id,p_scope_1_kind,p_scope_1_digest,s1.next_ordinal,chosen);
 IF p_scope_count=2 THEN
  INSERT INTO public.shared_claim_scopes(claim_id,request_ordinal,policy_id,scope_kind,scope_digest,allocation_ordinal,state)
   VALUES(p_claim_id,2,p_policy_id,p_scope_2_kind,p_scope_2_digest,s2.next_ordinal,chosen);
 END IF;
 UPDATE public.shared_claim_scope_summaries SET running_count=running_count+CASE WHEN chosen='running' THEN 1 ELSE 0 END,waiting_count=waiting_count+CASE WHEN chosen='waiting' THEN 1 ELSE 0 END,next_ordinal=next_ordinal+1,updated_at=admitted
  WHERE policy_id=p_policy_id AND scope_kind=p_scope_1_kind AND scope_digest=p_scope_1_digest;
 IF p_scope_count=2 THEN
  UPDATE public.shared_claim_scope_summaries SET running_count=running_count+CASE WHEN chosen='running' THEN 1 ELSE 0 END,waiting_count=waiting_count+CASE WHEN chosen='waiting' THEN 1 ELSE 0 END,next_ordinal=next_ordinal+1,updated_at=admitted
   WHERE policy_id=p_policy_id AND scope_kind=p_scope_2_kind AND scope_digest=p_scope_2_digest;
 END IF;
 SELECT * INTO existing FROM public.shared_claim_requests WHERE claim_id=p_claim_id;
 RETURN public.runtime_claim_result_for(existing);
END $$;

CREATE FUNCTION public.runtime_acquire_single_claim(uuid,text,uuid,uuid,text,bytea) RETURNS public.runtime_claim_result
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$ BEGIN
 IF $2 IS NULL OR $2 NOT IN ('render.global_claim','password.hash','mail.send','mcp.user_concurrent') THEN RAISE EXCEPTION USING ERRCODE='22023',MESSAGE='claim input is invalid'; END IF;
 RETURN public.runtime_claim_acquire($1,$2,$3,$4,1::smallint,$5,$6,NULL::text,NULL::bytea);
END $$;

CREATE FUNCTION public.runtime_acquire_sse_claim(uuid,uuid,bytea,bytea) RETURNS public.runtime_claim_result
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT public.runtime_claim_acquire($1,'sse.fleet_account_ip'::text,$2,NULL::uuid,(CASE WHEN $4 IS NULL THEN 1 ELSE 2 END)::smallint,'ip'::text,$3,(CASE WHEN $4 IS NULL THEN NULL ELSE 'account' END)::text,$4)
$$;

CREATE FUNCTION public.runtime_resolve_claim(uuid,uuid,bytea) RETURNS public.runtime_claim_result
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE p public.shared_claim_requests;
BEGIN
 PERFORM public.runtime_require_write_entry();
 IF session_user NOT IN ('aboutme_app','aboutme_maintenance') THEN RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='claim role is forbidden'; END IF;
 p:=public.runtime_claim_assert_request($1,$2,$3);
 IF p.claim_id IS NULL THEN RETURN public.runtime_claim_absent($1); END IF;
 RETURN public.runtime_claim_result_for(p);
END $$;

CREATE FUNCTION public.runtime_promote_claim(uuid,uuid,bytea) RETURNS public.runtime_claim_result
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE cap public.runtime_capacity; replica public.runtime_replicas; p public.shared_claim_requests; s public.shared_claim_scopes; c public.shared_claim_policies; summary public.shared_claim_scope_summaries; first_waiting uuid; now_at timestamptz;
BEGIN
 PERFORM public.runtime_require_write_entry();
 IF session_user<>'aboutme_app' THEN RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='claim role is forbidden'; END IF;
 PERFORM public.runtime_claim_validate_input($1,$2,$3);
 SELECT * INTO cap FROM public.runtime_capacity WHERE singleton FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='runtime capacity is missing'; END IF;
 SELECT * INTO replica FROM public.runtime_replicas WHERE replica_id=$2 FOR UPDATE;
 p:=public.runtime_claim_assert_request($1,$2,$3);
 IF p.claim_id IS NULL THEN RETURN public.runtime_claim_absent($1); END IF;
 IF NOT cap.admission_enabled OR cap.lifecycle_phase<>'online' OR replica.state IS DISTINCT FROM 'active' OR p.state<>'waiting' THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='claim promotion is unavailable';
 END IF;
 SELECT * INTO s FROM public.shared_claim_scopes WHERE claim_id=p.claim_id AND request_ordinal=1 FOR UPDATE;
 SELECT * INTO c FROM public.shared_claim_policies WHERE policy_id=s.policy_id AND scope_kind=s.scope_kind FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='claim policy catalog is invalid'; END IF;
 IF NOT c.queue_enabled THEN RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='claim promotion is unavailable'; END IF;
 SELECT * INTO summary FROM public.shared_claim_scope_summaries WHERE policy_id=s.policy_id AND scope_kind=s.scope_kind AND scope_digest=s.scope_digest FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='stored claim summary is invalid'; END IF;
 now_at:=clock_timestamp();
 IF p.deadline_at IS NOT NULL AND now_at>=p.deadline_at THEN
  UPDATE public.shared_claim_scopes SET state='released' WHERE claim_id=p.claim_id;
  UPDATE public.shared_claim_requests SET state='released',released_at=now_at,release_reason='expired' WHERE claim_id=p.claim_id;
  UPDATE public.shared_claim_scope_summaries SET waiting_count=waiting_count-1,updated_at=now_at WHERE policy_id=s.policy_id AND scope_kind=s.scope_kind AND scope_digest=s.scope_digest;
 ELSE
  SELECT claim_id INTO first_waiting FROM public.shared_claim_scopes WHERE policy_id=s.policy_id AND scope_kind=s.scope_kind AND scope_digest=s.scope_digest AND state='waiting' ORDER BY allocation_ordinal LIMIT 1;
  IF summary.running_count<c.running_limit AND first_waiting=p.claim_id THEN
   UPDATE public.shared_claim_scopes SET state='running' WHERE claim_id=p.claim_id;
   UPDATE public.shared_claim_requests SET state='running' WHERE claim_id=p.claim_id;
   UPDATE public.shared_claim_scope_summaries SET running_count=running_count+1,waiting_count=waiting_count-1,updated_at=now_at WHERE policy_id=s.policy_id AND scope_kind=s.scope_kind AND scope_digest=s.scope_digest;
  END IF;
 END IF;
 SELECT * INTO p FROM public.shared_claim_requests WHERE claim_id=p.claim_id;
 RETURN public.runtime_claim_result_for(p);
END $$;

CREATE FUNCTION public.runtime_release_claim(uuid,uuid,bytea,text) RETURNS public.runtime_claim_result
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE cap public.runtime_capacity; p public.shared_claim_requests; s record; now_at timestamptz;
BEGIN
 PERFORM public.runtime_require_write_entry();
 IF session_user NOT IN ('aboutme_app','aboutme_maintenance') THEN RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='claim role is forbidden'; END IF;
 IF $4 IS NOT DISTINCT FROM 'fenced' THEN RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='fenced claim release is forbidden'; END IF;
 IF $4 IS NULL OR $4 NOT IN ('joined','canceled','expired') THEN RAISE EXCEPTION USING ERRCODE='22023',MESSAGE='claim release input is invalid'; END IF;
 PERFORM public.runtime_claim_validate_input($1,$2,$3);
 SELECT * INTO cap FROM public.runtime_capacity WHERE singleton FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='runtime capacity is missing'; END IF;
 PERFORM 1 FROM public.runtime_replicas WHERE replica_id=$2 FOR UPDATE;
 p:=public.runtime_claim_assert_request($1,$2,$3);
 IF p.claim_id IS NULL THEN RETURN public.runtime_claim_absent($1); END IF;
 IF p.state='released' THEN
  IF p.release_reason<>$4 THEN RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='claim terminal reason conflicts'; END IF;
  RETURN public.runtime_claim_result_for(p);
 END IF;
 FOR s IN SELECT DISTINCT policy_id,scope_kind FROM public.shared_claim_scopes WHERE claim_id=p.claim_id ORDER BY policy_id,scope_kind LOOP
  PERFORM 1 FROM public.shared_claim_policies WHERE policy_id=s.policy_id AND scope_kind=s.scope_kind FOR UPDATE;
  IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='claim policy catalog is invalid'; END IF;
 END LOOP;
 now_at:=clock_timestamp();
 FOR s IN SELECT policy_id,scope_kind,scope_digest,state FROM public.shared_claim_scopes WHERE claim_id=p.claim_id ORDER BY policy_id,scope_kind,scope_digest LOOP
  PERFORM 1 FROM public.shared_claim_scope_summaries WHERE policy_id=s.policy_id AND scope_kind=s.scope_kind AND scope_digest=s.scope_digest FOR UPDATE;
  IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='stored claim summary is invalid'; END IF;
  UPDATE public.shared_claim_scope_summaries SET running_count=running_count-CASE WHEN s.state='running' THEN 1 ELSE 0 END,waiting_count=waiting_count-CASE WHEN s.state='waiting' THEN 1 ELSE 0 END,updated_at=now_at
   WHERE policy_id=s.policy_id AND scope_kind=s.scope_kind AND scope_digest=s.scope_digest;
 END LOOP;
 UPDATE public.shared_claim_scopes SET state='released' WHERE claim_id=p.claim_id;
 UPDATE public.shared_claim_requests SET state='released',released_at=now_at,release_reason=$4 WHERE claim_id=p.claim_id;
 SELECT * INTO p FROM public.shared_claim_requests WHERE claim_id=p.claim_id;
 RETURN public.runtime_claim_result_for(p);
END $$;

CREATE FUNCTION public.runtime_gc_released_claim_receipts() RETURNS TABLE(deleted_claim_count integer,deleted_scope_summary_count integer)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE ids uuid[]; affected public.shared_claim_scope_summaries[]; item record; claims integer:=0; summaries integer:=0; cutoff timestamptz;
BEGIN
 PERFORM public.runtime_require_write_entry();
 IF session_user<>'aboutme_maintenance' THEN RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='claim receipt cleanup role is forbidden'; END IF;
 cutoff:=clock_timestamp()-interval '24 hours';
 SELECT array_agg(q.claim_id ORDER BY q.claim_id) INTO ids FROM (SELECT claim_id FROM public.shared_claim_requests WHERE state='released' AND released_at<=cutoff ORDER BY claim_id LIMIT 256 FOR UPDATE) q;
 IF ids IS NULL THEN RETURN QUERY SELECT 0,0; RETURN; END IF;
 SELECT array_agg(claim_id ORDER BY claim_id) INTO ids FROM public.shared_claim_requests WHERE claim_id=ANY(ids) AND state='released' AND released_at<=cutoff;
 IF ids IS NULL THEN RETURN QUERY SELECT 0,0; RETURN; END IF;
 FOR item IN SELECT DISTINCT policy_id,scope_kind FROM public.shared_claim_scopes WHERE claim_id=ANY(ids) ORDER BY policy_id,scope_kind LOOP
  PERFORM 1 FROM public.shared_claim_policies WHERE policy_id=item.policy_id AND scope_kind=item.scope_kind FOR UPDATE;
 END LOOP;
 FOR item IN SELECT DISTINCT policy_id,scope_kind,scope_digest FROM public.shared_claim_scopes WHERE claim_id=ANY(ids) ORDER BY policy_id,scope_kind,scope_digest LOOP
  PERFORM 1 FROM public.shared_claim_scope_summaries WHERE policy_id=item.policy_id AND scope_kind=item.scope_kind AND scope_digest=item.scope_digest FOR UPDATE;
 END LOOP;
 SELECT array_agg(s ORDER BY s.policy_id,s.scope_kind,s.scope_digest) INTO affected FROM public.shared_claim_scope_summaries s
  WHERE (s.policy_id,s.scope_kind,s.scope_digest) IN (SELECT c.policy_id,c.scope_kind,c.scope_digest FROM public.shared_claim_scopes c WHERE c.claim_id=ANY(ids));
 DELETE FROM public.shared_claim_scopes WHERE claim_id=ANY(ids);
 DELETE FROM public.shared_claim_requests WHERE claim_id=ANY(ids) AND state='released' AND released_at<=cutoff;
 GET DIAGNOSTICS claims=ROW_COUNT;
 IF affected IS NOT NULL THEN
  DELETE FROM public.shared_claim_scope_summaries s
   WHERE (s.policy_id,s.scope_kind,s.scope_digest) IN (SELECT a.policy_id,a.scope_kind,a.scope_digest FROM unnest(affected) AS a)
    AND s.running_count=0 AND s.waiting_count=0
    AND NOT EXISTS (SELECT 1 FROM public.shared_claim_scopes c WHERE c.policy_id=s.policy_id AND c.scope_kind=s.scope_kind AND c.scope_digest=s.scope_digest);
  GET DIAGNOSTICS summaries=ROW_COUNT;
 END IF;
 RETURN QUERY SELECT claims,summaries;
END $$;

REVOKE ALL ON TYPE public.runtime_claim_result FROM PUBLIC;
REVOKE ALL ON FUNCTION public.runtime_claim_input_digest(uuid,text,uuid,uuid,smallint,text,bytea,text,bytea),public.runtime_claim_validate_role(text,text),public.runtime_claim_validate_input(uuid,uuid,bytea),public.runtime_claim_absent(uuid),public.runtime_claim_load(uuid),public.runtime_claim_result_for(public.shared_claim_requests),public.runtime_claim_assert_request(uuid,uuid,bytea),public.runtime_claim_acquire(uuid,text,uuid,uuid,smallint,text,bytea,text,bytea) FROM PUBLIC,aboutme_app,aboutme_maintenance,aboutme_lifecycle_command,aboutme_fencing_proof,aboutme_restore_verify;
REVOKE ALL ON FUNCTION public.runtime_acquire_single_claim(uuid,text,uuid,uuid,text,bytea),public.runtime_acquire_sse_claim(uuid,uuid,bytea,bytea),public.runtime_promote_claim(uuid,uuid,bytea),public.runtime_release_claim(uuid,uuid,bytea,text),public.runtime_resolve_claim(uuid,uuid,bytea),public.runtime_gc_released_claim_receipts() FROM PUBLIC,aboutme_app,aboutme_maintenance,aboutme_lifecycle_command,aboutme_fencing_proof,aboutme_restore_verify;
GRANT USAGE ON TYPE public.runtime_claim_result TO aboutme_app,aboutme_maintenance;
GRANT EXECUTE ON FUNCTION public.runtime_acquire_single_claim(uuid,text,uuid,uuid,text,bytea),public.runtime_acquire_sse_claim(uuid,uuid,bytea,bytea),public.runtime_promote_claim(uuid,uuid,bytea),public.runtime_release_claim(uuid,uuid,bytea,text),public.runtime_resolve_claim(uuid,uuid,bytea) TO aboutme_app;
GRANT EXECUTE ON FUNCTION public.runtime_acquire_single_claim(uuid,text,uuid,uuid,text,bytea),public.runtime_release_claim(uuid,uuid,bytea,text),public.runtime_resolve_claim(uuid,uuid,bytea),public.runtime_gc_released_claim_receipts() TO aboutme_maintenance;

RESET ROLE;
REVOKE CREATE ON SCHEMA public FROM aboutme_runtime_owner;
SELECT public.runtime_finish_write();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 1;
-- +goose StatementEnd
