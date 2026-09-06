-- +goose Up
-- +goose StatementBegin
SELECT public.runtime_begin_migration_write('migration-00018');

RESET ROLE;
GRANT CREATE ON SCHEMA public TO aboutme_runtime_owner;
SET LOCAL ROLE aboutme_runtime_owner;

CREATE TABLE public.shared_rate_policies (
 policy_id text PRIMARY KEY,
 algorithm text NOT NULL,
 capacity integer NOT NULL,
 window_microseconds bigint NOT NULL,
 allow_success_clear boolean NOT NULL,
 denied_attempt_adds_debt boolean NOT NULL,
 ordinary_idle_microseconds bigint NOT NULL,
 max_keys_per_partition integer NOT NULL,
 CONSTRAINT shared_rate_policies_algorithm_check CHECK (algorithm IN ('token_bucket','fixed_window','rolling_slug')),
 CONSTRAINT shared_rate_policies_exact_catalog_check CHECK ((
  (policy_id='api.outer_request' AND algorithm='token_bucket' AND capacity=300 AND window_microseconds=60000000) OR
  (policy_id='resume.read' AND algorithm='token_bucket' AND capacity=600 AND window_microseconds=60000000) OR
  (policy_id='resume.write' AND algorithm='token_bucket' AND capacity=240 AND window_microseconds=60000000) OR
  (policy_id='resume.photo_upload_rate' AND algorithm='token_bucket' AND capacity=20 AND window_microseconds=3600000000) OR
  (policy_id='auth.provider_start' AND algorithm='token_bucket' AND capacity=30 AND window_microseconds=60000000) OR
  (policy_id='password.login_ip' AND algorithm='token_bucket' AND capacity=30 AND window_microseconds=60000000) OR
  (policy_id='password.login_failure_email' AND algorithm='fixed_window' AND capacity=10 AND window_microseconds=900000000) OR
  (policy_id='password.register_forgot_ip' AND algorithm='token_bucket' AND capacity=20 AND window_microseconds=3600000000) OR
  (policy_id='password.register_forgot_email' AND algorithm='token_bucket' AND capacity=5 AND window_microseconds=3600000000) OR
  (policy_id='password.verify_reset_ip' AND algorithm='token_bucket' AND capacity=10 AND window_microseconds=3600000000) OR
  (policy_id='password.account_mutation' AND algorithm='token_bucket' AND capacity=10 AND window_microseconds=3600000000) OR
  (policy_id='resume.slug_change' AND algorithm='rolling_slug' AND capacity=30 AND window_microseconds=3600000000) OR
  (policy_id IN ('resume.owner_pdf_account','resume.owner_pdf_ip') AND algorithm='token_bucket' AND capacity=10 AND window_microseconds=60000000) OR
  (policy_id IN ('account.export','account.delete') AND algorithm='token_bucket' AND capacity=5 AND window_microseconds=60000000) OR
  (policy_id='public.artifact_request' AND algorithm='token_bucket' AND capacity=300 AND window_microseconds=60000000) OR
  (policy_id='public.render_miss' AND algorithm='token_bucket' AND capacity=20 AND window_microseconds=60000000) OR
  (policy_id='realtime.public_request' AND algorithm='token_bucket' AND capacity=300 AND window_microseconds=60000000) OR
  (policy_id='oauth.register' AND algorithm='token_bucket' AND capacity=5 AND window_microseconds=3600000000) OR
  (policy_id='oauth.token' AND algorithm='token_bucket' AND capacity=30 AND window_microseconds=60000000) OR
  (policy_id='oauth.failed_grant' AND algorithm='fixed_window' AND capacity=10 AND window_microseconds=900000000) OR
  (policy_id='mcp.token' AND algorithm='token_bucket' AND capacity=120 AND window_microseconds=60000000) OR
  (policy_id='mcp.user' AND algorithm='token_bucket' AND capacity=240 AND window_microseconds=60000000))
  AND allow_success_clear=(policy_id IN ('password.login_failure_email','oauth.failed_grant'))
  AND denied_attempt_adds_debt=(policy_id='resume.slug_change')
  AND ordinary_idle_microseconds=86400000000 AND max_keys_per_partition=10000),
 CONSTRAINT shared_rate_policies_policy_algorithm_key UNIQUE(policy_id,algorithm)
);

CREATE TABLE public.shared_rate_policy_key_shapes (
 policy_id text NOT NULL REFERENCES public.shared_rate_policies(policy_id) ON UPDATE RESTRICT ON DELETE RESTRICT,
 key_shape text NOT NULL CHECK (key_shape IN ('ip','peer_ip','account','account_ip','email_digest','oauth_client','token','user')),
 PRIMARY KEY(policy_id,key_shape),
 CONSTRAINT shared_rate_policy_key_shapes_exact_check CHECK ((
  (policy_id='api.outer_request' AND key_shape IN ('ip','peer_ip')) OR
  (policy_id IN ('resume.read','resume.write','resume.photo_upload_rate','account.export','account.delete') AND key_shape IN ('account_ip','peer_ip')) OR
  (policy_id='auth.provider_start' AND key_shape IN ('ip','account_ip','peer_ip')) OR
  (policy_id IN ('password.login_ip','password.register_forgot_ip','password.verify_reset_ip','public.artifact_request','public.render_miss','oauth.register','oauth.token') AND key_shape='ip') OR
  (policy_id IN ('password.login_failure_email','password.register_forgot_email') AND key_shape='email_digest') OR
  (policy_id='password.account_mutation' AND key_shape='account_ip') OR
  (policy_id IN ('resume.slug_change','resume.owner_pdf_account') AND key_shape='account') OR
  (policy_id='resume.owner_pdf_account' AND key_shape='peer_ip') OR
  (policy_id IN ('resume.owner_pdf_ip','realtime.public_request') AND key_shape IN ('ip','peer_ip')) OR
  (policy_id='oauth.failed_grant' AND key_shape='oauth_client') OR (policy_id='mcp.token' AND key_shape='token') OR (policy_id='mcp.user' AND key_shape='user')) IS TRUE)
);

CREATE TABLE public.shared_policy_clocks (
 policy_id text PRIMARY KEY REFERENCES public.shared_rate_policies(policy_id) ON UPDATE RESTRICT ON DELETE RESTRICT,
 high_water_at timestamptz NOT NULL,
 last_raw_at timestamptz NOT NULL,
 anomaly_count bigint NOT NULL CONSTRAINT shared_policy_clocks_anomaly_check CHECK(anomaly_count>=0)
);

CREATE TABLE public.shared_rate_partitions (
 policy_id text NOT NULL REFERENCES public.shared_rate_policies(policy_id) ON UPDATE RESTRICT ON DELETE RESTRICT,
 partition smallint NOT NULL CONSTRAINT shared_rate_partitions_number_check CHECK(partition IN (1,2)),
 enabled boolean NOT NULL,
 capacity_generation bigint NOT NULL CONSTRAINT shared_rate_partitions_generation_check CHECK(capacity_generation>0),
 operation_id text NOT NULL CONSTRAINT shared_rate_partitions_operation_check CHECK(octet_length(operation_id) BETWEEN 1 AND 128 AND operation_id~'^[ -~]+$'),
 active_keys integer NOT NULL CONSTRAINT shared_rate_partitions_active_keys_check CHECK(active_keys BETWEEN 0 AND 10000),
 updated_at timestamptz NOT NULL,
 PRIMARY KEY(policy_id,partition)
);

CREATE FUNCTION public.runtime_rate_events_sorted(events timestamptz[]) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT COALESCE(bool_and(previous IS NULL OR value>=previous),true) FROM (SELECT value,lag(value) OVER (ORDER BY ordinal) previous FROM unnest(events) WITH ORDINALITY AS e(value,ordinal)) ordered_events
$$;

CREATE TABLE public.shared_rate_buckets (
 policy_id text NOT NULL,
 key_digest bytea NOT NULL CONSTRAINT shared_rate_buckets_digest_check CHECK(octet_length(key_digest)=32),
 partition smallint NOT NULL,
 algorithm text NOT NULL,
 last_seen timestamptz NOT NULL,
 token_numerator bigint,
 refill_at timestamptz,
 window_started_at timestamptz,
 count integer,
 rolling_events timestamptz[],
 PRIMARY KEY(policy_id,key_digest),
 UNIQUE(policy_id,partition,key_digest),
 FOREIGN KEY(policy_id,partition) REFERENCES public.shared_rate_partitions(policy_id,partition) ON UPDATE RESTRICT ON DELETE RESTRICT,
 FOREIGN KEY(policy_id,algorithm) REFERENCES public.shared_rate_policies(policy_id,algorithm) ON UPDATE RESTRICT ON DELETE RESTRICT,
 CONSTRAINT shared_rate_buckets_algorithm_shape_check CHECK ((
  (algorithm='token_bucket' AND token_numerator>=0 AND refill_at IS NOT NULL AND window_started_at IS NULL AND count IS NULL AND rolling_events IS NULL) OR
  (algorithm='fixed_window' AND token_numerator IS NULL AND refill_at IS NULL AND count BETWEEN 0 AND 10 AND rolling_events IS NULL) OR
  (algorithm='rolling_slug' AND token_numerator IS NULL AND refill_at IS NULL AND window_started_at IS NULL AND count=cardinality(rolling_events)
   AND cardinality(rolling_events) BETWEEN 0 AND 30 AND CASE WHEN COALESCE(array_ndims(rolling_events),1)=1 THEN array_position(rolling_events,NULL) IS NULL AND public.runtime_rate_events_sorted(rolling_events) ELSE false END)) IS TRUE)
);

CREATE TABLE public.shared_rate_overflow (
 policy_id text PRIMARY KEY,
 algorithm text NOT NULL,
 last_seen timestamptz NOT NULL,
 token_numerator bigint,
 refill_at timestamptz,
 window_started_at timestamptz,
 count integer,
 rolling_events timestamptz[],
 FOREIGN KEY(policy_id,algorithm) REFERENCES public.shared_rate_policies(policy_id,algorithm) ON UPDATE RESTRICT ON DELETE RESTRICT,
 CONSTRAINT shared_rate_overflow_algorithm_shape_check CHECK ((
  (algorithm='token_bucket' AND token_numerator>=0 AND refill_at IS NOT NULL AND window_started_at IS NULL AND count IS NULL AND rolling_events IS NULL) OR
  (algorithm='fixed_window' AND token_numerator IS NULL AND refill_at IS NULL AND count BETWEEN 0 AND 10 AND rolling_events IS NULL) OR
  (algorithm='rolling_slug' AND token_numerator IS NULL AND refill_at IS NULL AND window_started_at IS NULL AND count=cardinality(rolling_events)
   AND cardinality(rolling_events) BETWEEN 0 AND 30 AND CASE WHEN COALESCE(array_ndims(rolling_events),1)=1 THEN array_position(rolling_events,NULL) IS NULL AND public.runtime_rate_events_sorted(rolling_events) ELSE false END)) IS TRUE)
);

CREATE TABLE public.shared_admission_attempts (
 attempt_id uuid PRIMARY KEY,
 policy_id text NOT NULL CONSTRAINT shared_admission_attempts_policy_check CHECK(policy_id='oauth.failed_grant'),
 client_id uuid NOT NULL,
 bucket_kind text NOT NULL CONSTRAINT shared_admission_attempts_bucket_kind_check CHECK(bucket_kind IN ('private','overflow')),
 partition smallint,
 key_digest bytea,
 window_started_at timestamptz NOT NULL,
 reserved_at timestamptz NOT NULL,
 effective_until timestamptz NOT NULL,
 state text NOT NULL CONSTRAINT shared_admission_attempts_state_check CHECK(state IN ('pending','terminal')),
 outcome text,
 terminal_reason text,
 terminal_at timestamptz,
 CONSTRAINT shared_admission_attempts_id_non_nil CHECK(attempt_id<>'00000000-0000-0000-0000-000000000000'::uuid),
 CONSTRAINT shared_admission_attempts_client_non_nil CHECK(client_id<>'00000000-0000-0000-0000-000000000000'::uuid),
 CONSTRAINT shared_admission_attempts_scope_shape_check CHECK (((bucket_kind='private' AND partition IN (1,2) AND octet_length(key_digest)=32) OR (bucket_kind='overflow' AND partition IS NULL AND key_digest IS NULL)) IS TRUE),
 CONSTRAINT shared_admission_attempts_time_check CHECK(window_started_at<=reserved_at AND reserved_at<effective_until AND effective_until=window_started_at+interval '15 minutes'),
 CONSTRAINT shared_admission_attempts_result_shape_check CHECK (((state='pending' AND outcome IS NULL AND terminal_reason IS NULL AND terminal_at IS NULL) OR
  (state='terminal' AND outcome IN ('failure','neutral','success') AND terminal_reason IN ('caller_finish','private_success_clear','window_expired') AND terminal_at>=reserved_at
   AND (terminal_reason<>'private_success_clear' OR (bucket_kind='private' AND outcome='neutral'))
   AND (terminal_reason<>'window_expired' OR (outcome='neutral' AND terminal_at>=effective_until)))) IS TRUE)
);
CREATE INDEX shared_admission_attempts_pending_private_idx ON public.shared_admission_attempts(policy_id,key_digest,attempt_id) WHERE state='pending' AND bucket_kind='private';
CREATE INDEX shared_admission_attempts_pending_overflow_idx ON public.shared_admission_attempts(policy_id,attempt_id) WHERE state='pending' AND bucket_kind='overflow';
CREATE INDEX shared_admission_attempts_pending_expiry_idx ON public.shared_admission_attempts(effective_until,attempt_id) WHERE state='pending';
CREATE INDEX shared_admission_attempts_terminal_idx ON public.shared_admission_attempts(terminal_at,attempt_id) WHERE state='terminal';

CREATE FUNCTION public.runtime_reject_rate_catalog_mutation() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='shared rate catalog is immutable'; END $$;
CREATE FUNCTION public.runtime_reject_rate_truncate() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate truncate rejected'; END $$;
CREATE FUNCTION public.runtime_validate_policy_clock() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='shared policy clock mutation rejected'; END IF; IF NEW.policy_id<>OLD.policy_id OR NEW.high_water_at<OLD.high_water_at THEN RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='shared policy clock mutation rejected'; END IF; RETURN NEW;
END $$;
CREATE FUNCTION public.runtime_validate_rate_partition() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='shared rate partition identity mutation rejected'; END IF; IF NEW.policy_id<>OLD.policy_id OR NEW.partition<>OLD.partition THEN RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='shared rate partition identity mutation rejected'; END IF; RETURN NEW;
END $$;
CREATE FUNCTION public.runtime_validate_rate_bucket() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF TG_OP='UPDATE' AND (NEW.policy_id<>OLD.policy_id OR NEW.key_digest<>OLD.key_digest OR NEW.partition<>OLD.partition OR NEW.algorithm<>OLD.algorithm) THEN RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='shared rate bucket identity mutation rejected'; END IF; RETURN NEW;
END $$;
CREATE FUNCTION public.runtime_validate_rate_overflow() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='shared rate overflow identity mutation rejected'; END IF; IF NEW.policy_id<>OLD.policy_id OR NEW.algorithm<>OLD.algorithm THEN RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='shared rate overflow identity mutation rejected'; END IF; RETURN NEW;
END $$;
CREATE FUNCTION public.runtime_validate_admission_attempt() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF TG_OP='DELETE' THEN IF OLD.state='pending' THEN RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='pending admission attempt cannot be deleted'; END IF; RETURN OLD; END IF;
 IF NEW.attempt_id<>OLD.attempt_id OR NEW.policy_id<>OLD.policy_id OR NEW.client_id<>OLD.client_id OR NEW.bucket_kind<>OLD.bucket_kind OR NEW.partition IS DISTINCT FROM OLD.partition OR NEW.key_digest IS DISTINCT FROM OLD.key_digest OR NEW.window_started_at<>OLD.window_started_at OR NEW.reserved_at<>OLD.reserved_at OR NEW.effective_until<>OLD.effective_until OR OLD.state='terminal' OR NEW.state<>'terminal' THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='admission attempt mutation rejected'; END IF; RETURN NEW;
END $$;
CREATE FUNCTION public.runtime_assert_rate_partition_count() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE p text:=COALESCE(NEW.policy_id,OLD.policy_id); n smallint:=COALESCE(NEW.partition,OLD.partition); stored integer; actual integer;
BEGIN
 IF TG_TABLE_NAME='shared_rate_buckets' THEN
  IF TG_OP='UPDATE' THEN RETURN NULL; END IF;
 ELSIF TG_TABLE_NAME='shared_rate_partitions' THEN
  IF TG_OP='UPDATE' AND NEW.active_keys=OLD.active_keys THEN RETURN NULL; END IF;
 END IF;
 SELECT active_keys INTO stored FROM public.shared_rate_partitions WHERE policy_id=p AND partition=n; IF NOT FOUND THEN RETURN NULL; END IF;
 SELECT count(*) INTO actual FROM public.shared_rate_buckets WHERE policy_id=p AND partition=n; IF stored<>actual THEN RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='shared_rate_partition_exact_count',MESSAGE='shared rate partition count mismatch'; END IF; RETURN NULL; END $$;
CREATE FUNCTION public.runtime_assert_rate_state() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE target_policy text; target_digest bytea; target_kind text; bad boolean;
BEGIN
 IF TG_TABLE_NAME='shared_rate_buckets' THEN target_policy:=COALESCE(NEW.policy_id,OLD.policy_id); target_digest:=COALESCE(NEW.key_digest,OLD.key_digest); target_kind:='private';
 ELSIF TG_TABLE_NAME='shared_rate_overflow' THEN target_policy:=COALESCE(NEW.policy_id,OLD.policy_id); target_kind:='overflow';
 ELSE target_policy:=COALESCE(NEW.policy_id,OLD.policy_id); target_digest:=COALESCE(NEW.key_digest,OLD.key_digest); target_kind:=COALESCE(NEW.bucket_kind,OLD.bucket_kind); END IF;
 SELECT
  EXISTS(SELECT 1 FROM public.shared_rate_buckets b JOIN public.shared_rate_policies p USING(policy_id) WHERE target_kind='private' AND b.policy_id=target_policy AND b.key_digest=target_digest AND b.algorithm='token_bucket' AND b.token_numerator>p.capacity::bigint*p.window_microseconds)
  OR EXISTS(SELECT 1 FROM public.shared_rate_overflow b JOIN public.shared_rate_policies p USING(policy_id) WHERE target_kind='overflow' AND b.policy_id=target_policy AND b.algorithm='token_bucket' AND b.token_numerator>p.capacity::bigint*p.window_microseconds)
  OR EXISTS(SELECT 1 FROM public.shared_rate_buckets b WHERE target_kind='private' AND b.policy_id='password.login_failure_email' AND b.policy_id=target_policy AND b.key_digest=target_digest AND ((b.count=0) IS DISTINCT FROM (b.window_started_at IS NULL)))
  OR EXISTS(SELECT 1 FROM public.shared_rate_overflow b WHERE target_kind='overflow' AND b.policy_id='password.login_failure_email' AND b.policy_id=target_policy AND ((b.count=0) IS DISTINCT FROM (b.window_started_at IS NULL)))
  OR EXISTS(SELECT 1 FROM public.shared_admission_attempts a WHERE a.state='pending' AND a.policy_id=target_policy AND a.bucket_kind=target_kind AND (target_kind='overflow' OR a.key_digest=target_digest) AND ((a.bucket_kind='private' AND NOT EXISTS(SELECT 1 FROM public.shared_rate_buckets b WHERE b.policy_id=a.policy_id AND b.partition=a.partition AND b.key_digest=a.key_digest AND b.window_started_at=a.window_started_at)) OR (a.bucket_kind='overflow' AND NOT EXISTS(SELECT 1 FROM public.shared_rate_overflow b WHERE b.policy_id=a.policy_id AND b.window_started_at=a.window_started_at))))
  OR EXISTS(SELECT 1 FROM public.shared_rate_buckets b WHERE target_kind='private' AND b.policy_id='oauth.failed_grant' AND b.policy_id=target_policy AND b.key_digest=target_digest AND ((b.window_started_at IS NULL) IS DISTINCT FROM (b.count=0 AND NOT EXISTS(SELECT 1 FROM public.shared_admission_attempts a WHERE a.state='pending' AND a.bucket_kind='private' AND a.policy_id=b.policy_id AND a.key_digest=b.key_digest AND a.partition=b.partition)) OR b.count+(SELECT count(*) FROM public.shared_admission_attempts a WHERE a.state='pending' AND a.bucket_kind='private' AND a.policy_id=b.policy_id AND a.key_digest=b.key_digest AND a.partition=b.partition)>10))
  OR EXISTS(SELECT 1 FROM public.shared_rate_overflow b WHERE target_kind='overflow' AND b.policy_id='oauth.failed_grant' AND b.policy_id=target_policy AND ((b.window_started_at IS NULL) IS DISTINCT FROM (b.count=0 AND NOT EXISTS(SELECT 1 FROM public.shared_admission_attempts a WHERE a.state='pending' AND a.bucket_kind='overflow' AND a.policy_id=b.policy_id)) OR b.count+(SELECT count(*) FROM public.shared_admission_attempts a WHERE a.state='pending' AND a.bucket_kind='overflow' AND a.policy_id=b.policy_id)>10)) INTO bad;
 IF bad THEN RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='shared_rate_state_exact',MESSAGE='shared rate state is invalid'; END IF; RETURN NULL;
END $$;
CREATE TRIGGER shared_rate_policies_immutable BEFORE UPDATE OR DELETE ON public.shared_rate_policies FOR EACH ROW EXECUTE FUNCTION public.runtime_reject_rate_catalog_mutation();
CREATE TRIGGER shared_rate_policy_key_shapes_immutable BEFORE UPDATE OR DELETE ON public.shared_rate_policy_key_shapes FOR EACH ROW EXECUTE FUNCTION public.runtime_reject_rate_catalog_mutation();
CREATE TRIGGER shared_policy_clocks_validate BEFORE UPDATE OR DELETE ON public.shared_policy_clocks FOR EACH ROW EXECUTE FUNCTION public.runtime_validate_policy_clock();
CREATE TRIGGER shared_rate_partitions_validate BEFORE UPDATE OR DELETE ON public.shared_rate_partitions FOR EACH ROW EXECUTE FUNCTION public.runtime_validate_rate_partition();
CREATE TRIGGER shared_rate_buckets_validate BEFORE UPDATE ON public.shared_rate_buckets FOR EACH ROW EXECUTE FUNCTION public.runtime_validate_rate_bucket();
CREATE TRIGGER shared_rate_overflow_validate BEFORE UPDATE OR DELETE ON public.shared_rate_overflow FOR EACH ROW EXECUTE FUNCTION public.runtime_validate_rate_overflow();
CREATE TRIGGER shared_admission_attempts_validate BEFORE UPDATE OR DELETE ON public.shared_admission_attempts FOR EACH ROW EXECUTE FUNCTION public.runtime_validate_admission_attempt();
CREATE CONSTRAINT TRIGGER shared_rate_partitions_exact AFTER INSERT OR UPDATE OR DELETE ON public.shared_rate_partitions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.runtime_assert_rate_partition_count();
CREATE CONSTRAINT TRIGGER shared_rate_buckets_partition_exact AFTER INSERT OR UPDATE OR DELETE ON public.shared_rate_buckets DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.runtime_assert_rate_partition_count();
CREATE CONSTRAINT TRIGGER shared_rate_buckets_state_exact AFTER INSERT OR UPDATE OR DELETE ON public.shared_rate_buckets DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.runtime_assert_rate_state();
CREATE CONSTRAINT TRIGGER shared_rate_overflow_state_exact AFTER INSERT OR UPDATE OR DELETE ON public.shared_rate_overflow DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.runtime_assert_rate_state();
CREATE CONSTRAINT TRIGGER shared_admission_attempts_state_exact AFTER INSERT OR UPDATE OR DELETE ON public.shared_admission_attempts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.runtime_assert_rate_state();

SELECT set_config('aboutme.runtime_rate_seed_time',clock_timestamp()::text,true);
INSERT INTO public.shared_rate_policies(policy_id,algorithm,capacity,window_microseconds,allow_success_clear,denied_attempt_adds_debt,ordinary_idle_microseconds,max_keys_per_partition) VALUES
('api.outer_request','token_bucket',300,60000000,false,false,86400000000,10000),('resume.read','token_bucket',600,60000000,false,false,86400000000,10000),('resume.write','token_bucket',240,60000000,false,false,86400000000,10000),('resume.photo_upload_rate','token_bucket',20,3600000000,false,false,86400000000,10000),('auth.provider_start','token_bucket',30,60000000,false,false,86400000000,10000),('password.login_ip','token_bucket',30,60000000,false,false,86400000000,10000),('password.login_failure_email','fixed_window',10,900000000,true,false,86400000000,10000),('password.register_forgot_ip','token_bucket',20,3600000000,false,false,86400000000,10000),('password.register_forgot_email','token_bucket',5,3600000000,false,false,86400000000,10000),('password.verify_reset_ip','token_bucket',10,3600000000,false,false,86400000000,10000),('password.account_mutation','token_bucket',10,3600000000,false,false,86400000000,10000),('resume.slug_change','rolling_slug',30,3600000000,false,true,86400000000,10000),('resume.owner_pdf_account','token_bucket',10,60000000,false,false,86400000000,10000),('resume.owner_pdf_ip','token_bucket',10,60000000,false,false,86400000000,10000),('account.export','token_bucket',5,60000000,false,false,86400000000,10000),('account.delete','token_bucket',5,60000000,false,false,86400000000,10000),('public.artifact_request','token_bucket',300,60000000,false,false,86400000000,10000),('public.render_miss','token_bucket',20,60000000,false,false,86400000000,10000),('realtime.public_request','token_bucket',300,60000000,false,false,86400000000,10000),('oauth.register','token_bucket',5,3600000000,false,false,86400000000,10000),('oauth.token','token_bucket',30,60000000,false,false,86400000000,10000),('oauth.failed_grant','fixed_window',10,900000000,true,false,86400000000,10000),('mcp.token','token_bucket',120,60000000,false,false,86400000000,10000),('mcp.user','token_bucket',240,60000000,false,false,86400000000,10000);
INSERT INTO public.shared_rate_policy_key_shapes(policy_id,key_shape) VALUES
('api.outer_request','ip'),('resume.read','account_ip'),('resume.write','account_ip'),('resume.photo_upload_rate','account_ip'),('auth.provider_start','ip'),('auth.provider_start','account_ip'),('password.login_ip','ip'),('password.login_failure_email','email_digest'),('password.register_forgot_ip','ip'),('password.register_forgot_email','email_digest'),('password.verify_reset_ip','ip'),('password.account_mutation','account_ip'),('resume.slug_change','account'),('resume.owner_pdf_account','account'),('resume.owner_pdf_ip','ip'),('account.export','account_ip'),('account.delete','account_ip'),('public.artifact_request','ip'),('public.render_miss','ip'),('realtime.public_request','ip'),('oauth.register','ip'),('oauth.token','ip'),('oauth.failed_grant','oauth_client'),('mcp.token','token'),('mcp.user','user'),
('api.outer_request','peer_ip'),('resume.read','peer_ip'),('resume.write','peer_ip'),('resume.photo_upload_rate','peer_ip'),('auth.provider_start','peer_ip'),('resume.owner_pdf_account','peer_ip'),('resume.owner_pdf_ip','peer_ip'),('account.export','peer_ip'),('account.delete','peer_ip'),('realtime.public_request','peer_ip');
INSERT INTO public.shared_policy_clocks(policy_id,high_water_at,last_raw_at,anomaly_count) SELECT policy_id,current_setting('aboutme.runtime_rate_seed_time')::timestamptz,current_setting('aboutme.runtime_rate_seed_time')::timestamptz,0 FROM public.shared_rate_policies;
INSERT INTO public.shared_rate_partitions(policy_id,partition,enabled,capacity_generation,operation_id,active_keys,updated_at) SELECT policy_id,p,false,1,'bootstrap-uncomposed-v1',0,current_setting('aboutme.runtime_rate_seed_time')::timestamptz FROM public.shared_rate_policies CROSS JOIN (VALUES(1::smallint),(2::smallint)) v(p);
INSERT INTO public.shared_rate_overflow(policy_id,algorithm,last_seen,token_numerator,refill_at,window_started_at,count,rolling_events)
SELECT policy_id,algorithm,current_setting('aboutme.runtime_rate_seed_time')::timestamptz,CASE WHEN algorithm='token_bucket' THEN capacity::bigint*window_microseconds END,CASE WHEN algorithm='token_bucket' THEN current_setting('aboutme.runtime_rate_seed_time')::timestamptz END,NULL,CASE WHEN algorithm IN ('fixed_window','rolling_slug') THEN 0 END,CASE WHEN algorithm='rolling_slug' THEN ARRAY[]::timestamptz[] END FROM public.shared_rate_policies;

CREATE TRIGGER shared_rate_policies_write_entry BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON public.shared_rate_policies FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_write_entry();
CREATE TRIGGER shared_rate_policy_key_shapes_write_entry BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON public.shared_rate_policy_key_shapes FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_write_entry();
CREATE TRIGGER shared_policy_clocks_write_entry BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON public.shared_policy_clocks FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_write_entry();
CREATE TRIGGER shared_rate_partitions_write_entry BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON public.shared_rate_partitions FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_write_entry();
CREATE TRIGGER shared_rate_buckets_write_entry BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON public.shared_rate_buckets FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_write_entry();
CREATE TRIGGER shared_rate_overflow_write_entry BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON public.shared_rate_overflow FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_write_entry();
CREATE TRIGGER shared_admission_attempts_write_entry BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON public.shared_admission_attempts FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_write_entry();
CREATE TRIGGER shared_rate_policies_no_truncate BEFORE TRUNCATE ON public.shared_rate_policies FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_reject_rate_truncate();
CREATE TRIGGER shared_rate_policy_key_shapes_no_truncate BEFORE TRUNCATE ON public.shared_rate_policy_key_shapes FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_reject_rate_truncate();
CREATE TRIGGER shared_policy_clocks_no_truncate BEFORE TRUNCATE ON public.shared_policy_clocks FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_reject_rate_truncate();
CREATE TRIGGER shared_rate_partitions_no_truncate BEFORE TRUNCATE ON public.shared_rate_partitions FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_reject_rate_truncate();
CREATE TRIGGER shared_rate_buckets_no_truncate BEFORE TRUNCATE ON public.shared_rate_buckets FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_reject_rate_truncate();
CREATE TRIGGER shared_rate_overflow_no_truncate BEFORE TRUNCATE ON public.shared_rate_overflow FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_reject_rate_truncate();
CREATE TRIGGER shared_admission_attempts_no_truncate BEFORE TRUNCATE ON public.shared_admission_attempts FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_reject_rate_truncate();

REVOKE ALL ON TABLE public.shared_rate_policies,public.shared_rate_policy_key_shapes,public.shared_policy_clocks,public.shared_rate_partitions,public.shared_rate_buckets,public.shared_rate_overflow,public.shared_admission_attempts FROM PUBLIC,aboutme_app,aboutme_maintenance,aboutme_lifecycle_command,aboutme_fencing_proof,aboutme_restore_verify,aboutme_migrator;
REVOKE ALL ON FUNCTION public.runtime_rate_events_sorted(timestamptz[]),public.runtime_reject_rate_catalog_mutation(),public.runtime_reject_rate_truncate(),public.runtime_validate_policy_clock(),public.runtime_validate_rate_partition(),public.runtime_validate_rate_bucket(),public.runtime_validate_rate_overflow(),public.runtime_validate_admission_attempt(),public.runtime_assert_rate_partition_count(),public.runtime_assert_rate_state() FROM PUBLIC,aboutme_app,aboutme_maintenance,aboutme_lifecycle_command,aboutme_fencing_proof,aboutme_restore_verify,aboutme_migrator;
RESET ROLE;
REVOKE CREATE ON SCHEMA public FROM aboutme_runtime_owner;
SELECT public.runtime_finish_write();
-- +goose StatementEnd

-- +goose Down
-- Forward-only migration. Intentionally inert.
