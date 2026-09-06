-- +goose Up
-- +goose StatementBegin
SELECT public.runtime_begin_migration_write('migration-00017');

RESET ROLE;
GRANT CREATE ON SCHEMA public TO aboutme_runtime_owner;
SET LOCAL ROLE aboutme_runtime_owner;

CREATE TABLE public.shared_claim_policies (
 policy_id text NOT NULL,
 scope_kind text NOT NULL,
 running_limit integer NOT NULL,
 waiting_limit integer NOT NULL,
 queue_enabled boolean NOT NULL,
 deadline_mode text NOT NULL,
 CONSTRAINT shared_claim_policies_pkey PRIMARY KEY(policy_id,scope_kind),
 CONSTRAINT shared_claim_policies_identity_check CHECK (policy_id IN ('render.global_claim','password.hash','mail.send','mcp.user_concurrent','sse.fleet_account_ip') AND scope_kind IN ('global','user','ip','account')),
 CONSTRAINT shared_claim_policies_limits_check CHECK (running_limit>0 AND waiting_limit>=0),
 CONSTRAINT shared_claim_policies_deadline_mode_check CHECK (deadline_mode IN ('none','render_20s')),
 CONSTRAINT shared_claim_policies_exact_catalog_check CHECK ((
  (policy_id='render.global_claim' AND scope_kind='global' AND running_limit=1 AND waiting_limit=8 AND queue_enabled AND deadline_mode='render_20s') OR
  (policy_id='password.hash' AND scope_kind='global' AND running_limit=2 AND waiting_limit=16 AND queue_enabled AND deadline_mode='none') OR
  (policy_id='mail.send' AND scope_kind='global' AND running_limit=2 AND waiting_limit=0 AND NOT queue_enabled AND deadline_mode='none') OR
  (policy_id='mcp.user_concurrent' AND scope_kind='user' AND running_limit=4 AND waiting_limit=0 AND NOT queue_enabled AND deadline_mode='none') OR
  (policy_id='sse.fleet_account_ip' AND scope_kind='ip' AND running_limit=100 AND waiting_limit=0 AND NOT queue_enabled AND deadline_mode='none') OR
  (policy_id='sse.fleet_account_ip' AND scope_kind='account' AND running_limit=20 AND waiting_limit=0 AND NOT queue_enabled AND deadline_mode='none')) IS TRUE)
);

CREATE TABLE public.shared_claim_scope_summaries (
 policy_id text NOT NULL,
 scope_kind text NOT NULL,
 scope_digest bytea NOT NULL,
 running_count integer NOT NULL DEFAULT 0,
 waiting_count integer NOT NULL DEFAULT 0,
 next_ordinal bigint NOT NULL DEFAULT 1,
 updated_at timestamptz NOT NULL,
 CONSTRAINT shared_claim_scope_summaries_pkey PRIMARY KEY(policy_id,scope_kind,scope_digest),
 CONSTRAINT shared_claim_scope_summaries_policy_fkey FOREIGN KEY(policy_id,scope_kind) REFERENCES public.shared_claim_policies(policy_id,scope_kind) ON UPDATE RESTRICT ON DELETE RESTRICT,
 CONSTRAINT shared_claim_scope_summaries_digest_check CHECK (octet_length(scope_digest)=32),
 CONSTRAINT shared_claim_scope_summaries_counts_check CHECK (running_count>=0 AND waiting_count>=0),
 CONSTRAINT shared_claim_scope_summaries_next_ordinal_check CHECK (next_ordinal>0)
);

CREATE TABLE public.shared_claim_requests (
 claim_id uuid NOT NULL,
 policy_id text NOT NULL,
 replica_id uuid NOT NULL,
 work_id uuid,
 state text NOT NULL,
 admitted_at timestamptz NOT NULL,
 deadline_at timestamptz,
 released_at timestamptz,
 release_reason text,
 scope_count smallint NOT NULL,
 request_digest bytea NOT NULL,
 CONSTRAINT shared_claim_requests_pkey PRIMARY KEY(claim_id),
 CONSTRAINT shared_claim_requests_id_non_nil CHECK (claim_id<>'00000000-0000-0000-0000-000000000000'::uuid),
 CONSTRAINT shared_claim_requests_replica_non_nil CHECK (replica_id<>'00000000-0000-0000-0000-000000000000'::uuid),
 CONSTRAINT shared_claim_requests_work_non_nil CHECK (work_id IS NULL OR work_id<>'00000000-0000-0000-0000-000000000000'::uuid),
 CONSTRAINT shared_claim_requests_policy_check CHECK (policy_id IN ('render.global_claim','password.hash','mail.send','mcp.user_concurrent','sse.fleet_account_ip')),
 CONSTRAINT shared_claim_requests_state_check CHECK (state IN ('waiting','running','released')),
 CONSTRAINT shared_claim_requests_work_deadline_check CHECK ((
  (policy_id='render.global_claim' AND work_id IS NOT NULL AND deadline_at=admitted_at+interval '20 seconds') OR
  (policy_id<>'render.global_claim' AND work_id IS NULL AND deadline_at IS NULL)) IS TRUE),
 CONSTRAINT shared_claim_requests_release_shape_check CHECK ((
  (state IN ('waiting','running') AND released_at IS NULL AND release_reason IS NULL) OR
  (state='released' AND released_at IS NOT NULL AND release_reason IN ('joined','canceled','expired','fenced'))) IS TRUE),
 CONSTRAINT shared_claim_requests_scope_count_check CHECK (scope_count IN (1,2) AND (policy_id='sse.fleet_account_ip' OR scope_count=1)),
 CONSTRAINT shared_claim_requests_digest_check CHECK (octet_length(request_digest)=32),
 CONSTRAINT shared_claim_requests_replica_fkey FOREIGN KEY(replica_id) REFERENCES public.runtime_replicas(replica_id) ON UPDATE RESTRICT ON DELETE RESTRICT,
 CONSTRAINT shared_claim_requests_claim_policy_key UNIQUE(claim_id,policy_id)
);

CREATE TABLE public.shared_claim_scopes (
 claim_id uuid NOT NULL,
 request_ordinal smallint NOT NULL,
 policy_id text NOT NULL,
 scope_kind text NOT NULL,
 scope_digest bytea NOT NULL,
 allocation_ordinal bigint NOT NULL,
 state text NOT NULL,
 CONSTRAINT shared_claim_scopes_pkey PRIMARY KEY(claim_id,request_ordinal),
 CONSTRAINT shared_claim_scopes_parent_fkey FOREIGN KEY(claim_id,policy_id) REFERENCES public.shared_claim_requests(claim_id,policy_id) ON UPDATE RESTRICT ON DELETE RESTRICT,
 CONSTRAINT shared_claim_scopes_summary_fkey FOREIGN KEY(policy_id,scope_kind,scope_digest) REFERENCES public.shared_claim_scope_summaries(policy_id,scope_kind,scope_digest) ON UPDATE RESTRICT ON DELETE RESTRICT,
 CONSTRAINT shared_claim_scopes_request_ordinal_check CHECK (request_ordinal IN (1,2)),
 CONSTRAINT shared_claim_scopes_digest_check CHECK (octet_length(scope_digest)=32),
 CONSTRAINT shared_claim_scopes_allocation_ordinal_check CHECK (allocation_ordinal>0),
 CONSTRAINT shared_claim_scopes_state_check CHECK (state IN ('waiting','running','released')),
 CONSTRAINT shared_claim_scopes_claim_kind_key UNIQUE(claim_id,scope_kind),
 CONSTRAINT shared_claim_scopes_allocation_key UNIQUE(policy_id,scope_kind,scope_digest,allocation_ordinal)
);

INSERT INTO public.shared_claim_policies(policy_id,scope_kind,running_limit,waiting_limit,queue_enabled,deadline_mode) VALUES
 ('render.global_claim','global',1,8,true,'render_20s'),
 ('password.hash','global',2,16,true,'none'),
 ('mail.send','global',2,0,false,'none'),
 ('mcp.user_concurrent','user',4,0,false,'none'),
 ('sse.fleet_account_ip','ip',100,0,false,'none'),
 ('sse.fleet_account_ip','account',20,0,false,'none');

CREATE FUNCTION public.runtime_shared_claim_request_digest(target_claim_id uuid) RETURNS bytea
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE encoded bytea; parent record; child record;
BEGIN
 SELECT claim_id,policy_id,replica_id,work_id,scope_count INTO parent FROM public.shared_claim_requests WHERE claim_id=target_claim_id;
 IF NOT FOUND THEN RETURN NULL; END IF;
 encoded:=convert_to('aboutme.shared-claim.request.v1','UTF8')||decode('00','hex')||decode('01','hex')||uuid_send(parent.claim_id)
  ||decode(lpad(to_hex(octet_length(convert_to(parent.policy_id,'UTF8'))),2,'0'),'hex')||convert_to(parent.policy_id,'UTF8')||uuid_send(parent.replica_id)
  ||CASE WHEN parent.work_id IS NULL THEN decode('00','hex') ELSE decode('01','hex')||uuid_send(parent.work_id) END
  ||decode(lpad(to_hex(parent.scope_count::integer),2,'0'),'hex');
 FOR child IN SELECT request_ordinal,scope_kind,scope_digest FROM public.shared_claim_scopes WHERE claim_id=target_claim_id ORDER BY request_ordinal LOOP
  encoded:=encoded||decode(lpad(to_hex(child.request_ordinal::integer),2,'0'),'hex')
   ||decode(lpad(to_hex(octet_length(convert_to(child.scope_kind,'UTF8'))),2,'0'),'hex')||convert_to(child.scope_kind,'UTF8')||child.scope_digest;
 END LOOP;
 RETURN sha256(encoded);
END $$;

CREATE FUNCTION public.runtime_assert_shared_claim_request() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE target_id uuid:=COALESCE(NEW.claim_id,OLD.claim_id); parent record; child_count integer; shape_ok boolean; states_ok boolean;
BEGIN
 SELECT * INTO parent FROM public.shared_claim_requests WHERE claim_id=target_id;
 IF NOT FOUND THEN RETURN NULL; END IF;
 SELECT count(*),COALESCE(bool_and(state=parent.state),false),COALESCE(bool_and(
  (parent.policy_id='sse.fleet_account_ip' AND ((request_ordinal=1 AND scope_kind='ip') OR (parent.scope_count=2 AND request_ordinal=2 AND scope_kind='account'))) OR
  (parent.policy_id<>'sse.fleet_account_ip' AND request_ordinal=1 AND
   ((parent.policy_id IN ('render.global_claim','password.hash','mail.send') AND scope_kind='global') OR (parent.policy_id='mcp.user_concurrent' AND scope_kind='user')))),false)
 INTO child_count,states_ok,shape_ok FROM public.shared_claim_scopes WHERE claim_id=target_id;
 IF child_count<>parent.scope_count OR NOT states_ok OR NOT shape_ok OR public.runtime_shared_claim_request_digest(target_id)<>parent.request_digest THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='shared_claim_request_complete',MESSAGE='shared claim request is incomplete';
 END IF;
 RETURN NULL;
END $$;

CREATE FUNCTION public.runtime_assert_shared_claim_summary() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE p text:=COALESCE(NEW.policy_id,OLD.policy_id); k text:=COALESCE(NEW.scope_kind,OLD.scope_kind); d bytea:=COALESCE(NEW.scope_digest,OLD.scope_digest); summary record; actual_running integer; actual_waiting integer; greatest_allocation bigint;
BEGIN
 SELECT s.*,c.running_limit,c.waiting_limit INTO summary FROM public.shared_claim_scope_summaries s JOIN public.shared_claim_policies c USING(policy_id,scope_kind) WHERE s.policy_id=p AND s.scope_kind=k AND s.scope_digest=d;
 IF NOT FOUND THEN RETURN NULL; END IF;
 SELECT count(*) FILTER(WHERE state='running'),count(*) FILTER(WHERE state='waiting'),COALESCE(max(allocation_ordinal),0) INTO actual_running,actual_waiting,greatest_allocation FROM public.shared_claim_scopes WHERE policy_id=p AND scope_kind=k AND scope_digest=d;
 IF summary.running_count<>actual_running OR summary.waiting_count<>actual_waiting OR summary.running_count>summary.running_limit OR summary.waiting_count>summary.waiting_limit OR summary.next_ordinal<=greatest_allocation THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='shared_claim_summary_exact_counts',MESSAGE='shared claim summary counts are invalid';
 END IF;
 RETURN NULL;
END $$;

CREATE FUNCTION public.runtime_validate_shared_claim_policy() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$ BEGIN RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='shared claim policy catalog is immutable'; END $$;

CREATE FUNCTION public.runtime_validate_shared_claim_request() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  IF OLD.state<>'released' THEN RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='live shared claim cannot be deleted'; END IF;
  RETURN OLD;
 END IF;
 IF NEW.claim_id<>OLD.claim_id OR NEW.policy_id<>OLD.policy_id OR NEW.replica_id<>OLD.replica_id OR NEW.work_id IS DISTINCT FROM OLD.work_id OR NEW.admitted_at<>OLD.admitted_at OR NEW.deadline_at IS DISTINCT FROM OLD.deadline_at OR NEW.scope_count<>OLD.scope_count OR NEW.request_digest<>OLD.request_digest
  OR OLD.state='released' OR (OLD.state='waiting' AND NEW.state NOT IN ('running','released')) OR (OLD.state='running' AND NEW.state<>'released') THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='shared claim request mutation rejected';
 END IF;
 RETURN NEW;
END $$;

CREATE FUNCTION public.runtime_validate_shared_claim_scope() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  IF OLD.state<>'released' THEN RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='live shared claim scope cannot be deleted'; END IF;
  RETURN OLD;
 END IF;
 IF TG_OP='UPDATE' AND (NEW.claim_id<>OLD.claim_id OR NEW.request_ordinal<>OLD.request_ordinal OR NEW.policy_id<>OLD.policy_id OR NEW.scope_kind<>OLD.scope_kind OR NEW.scope_digest<>OLD.scope_digest OR NEW.allocation_ordinal<>OLD.allocation_ordinal OR OLD.state='released' OR (OLD.state='waiting' AND NEW.state NOT IN ('running','released')) OR (OLD.state='running' AND NEW.state<>'released')) THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='shared claim scope mutation rejected';
 END IF;
 RETURN NEW;
END $$;

CREATE FUNCTION public.runtime_validate_shared_claim_summary() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  IF OLD.running_count<>0 OR OLD.waiting_count<>0 OR EXISTS(SELECT 1 FROM public.shared_claim_scopes WHERE policy_id=OLD.policy_id AND scope_kind=OLD.scope_kind AND scope_digest=OLD.scope_digest) THEN
   RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='referenced shared claim summary cannot be deleted';
  END IF;
  RETURN OLD;
 END IF;
 IF TG_OP='UPDATE' AND (NEW.policy_id<>OLD.policy_id OR NEW.scope_kind<>OLD.scope_kind OR NEW.scope_digest<>OLD.scope_digest OR NEW.next_ordinal<OLD.next_ordinal) THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='shared claim summary mutation rejected';
 END IF;
 RETURN NEW;
END $$;

CREATE FUNCTION public.runtime_reject_shared_claim_truncate() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$ BEGIN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared claim truncate rejected'; END $$;

CREATE TRIGGER shared_claim_policies_immutable BEFORE UPDATE OR DELETE ON public.shared_claim_policies FOR EACH ROW EXECUTE FUNCTION public.runtime_validate_shared_claim_policy();
CREATE TRIGGER shared_claim_requests_validate BEFORE UPDATE OR DELETE ON public.shared_claim_requests FOR EACH ROW EXECUTE FUNCTION public.runtime_validate_shared_claim_request();
CREATE TRIGGER shared_claim_scopes_validate BEFORE INSERT OR UPDATE OR DELETE ON public.shared_claim_scopes FOR EACH ROW EXECUTE FUNCTION public.runtime_validate_shared_claim_scope();
CREATE TRIGGER shared_claim_scope_summaries_validate BEFORE UPDATE OR DELETE ON public.shared_claim_scope_summaries FOR EACH ROW EXECUTE FUNCTION public.runtime_validate_shared_claim_summary();

CREATE CONSTRAINT TRIGGER shared_claim_requests_complete AFTER INSERT OR UPDATE OR DELETE ON public.shared_claim_requests DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.runtime_assert_shared_claim_request();
CREATE CONSTRAINT TRIGGER shared_claim_scopes_request_complete AFTER INSERT OR UPDATE OR DELETE ON public.shared_claim_scopes DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.runtime_assert_shared_claim_request();
CREATE CONSTRAINT TRIGGER shared_claim_scope_summaries_exact AFTER INSERT OR UPDATE OR DELETE ON public.shared_claim_scope_summaries DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.runtime_assert_shared_claim_summary();
CREATE CONSTRAINT TRIGGER shared_claim_scopes_summary_exact AFTER INSERT OR UPDATE OR DELETE ON public.shared_claim_scopes DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.runtime_assert_shared_claim_summary();

CREATE TRIGGER shared_claim_policies_write_entry BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON public.shared_claim_policies FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_write_entry();
CREATE TRIGGER shared_claim_scope_summaries_write_entry BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON public.shared_claim_scope_summaries FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_write_entry();
CREATE TRIGGER shared_claim_requests_write_entry BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON public.shared_claim_requests FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_write_entry();
CREATE TRIGGER shared_claim_scopes_write_entry BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON public.shared_claim_scopes FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_write_entry();
CREATE TRIGGER shared_claim_policies_no_truncate BEFORE TRUNCATE ON public.shared_claim_policies FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_reject_shared_claim_truncate();
CREATE TRIGGER shared_claim_scope_summaries_no_truncate BEFORE TRUNCATE ON public.shared_claim_scope_summaries FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_reject_shared_claim_truncate();
CREATE TRIGGER shared_claim_requests_no_truncate BEFORE TRUNCATE ON public.shared_claim_requests FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_reject_shared_claim_truncate();
CREATE TRIGGER shared_claim_scopes_no_truncate BEFORE TRUNCATE ON public.shared_claim_scopes FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_reject_shared_claim_truncate();

REVOKE ALL ON TABLE public.shared_claim_policies,public.shared_claim_scope_summaries,public.shared_claim_requests,public.shared_claim_scopes FROM PUBLIC,aboutme_app,aboutme_maintenance,aboutme_lifecycle_command,aboutme_fencing_proof,aboutme_restore_verify;
REVOKE ALL ON FUNCTION public.runtime_shared_claim_request_digest(uuid),public.runtime_assert_shared_claim_request(),public.runtime_assert_shared_claim_summary(),public.runtime_validate_shared_claim_policy(),public.runtime_validate_shared_claim_request(),public.runtime_validate_shared_claim_scope(),public.runtime_validate_shared_claim_summary(),public.runtime_reject_shared_claim_truncate() FROM PUBLIC,aboutme_app,aboutme_maintenance,aboutme_lifecycle_command,aboutme_fencing_proof,aboutme_restore_verify;

RESET ROLE;
REVOKE CREATE ON SCHEMA public FROM aboutme_runtime_owner;
SELECT public.runtime_finish_write();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 1;
-- +goose StatementEnd
