-- +goose Up
-- +goose StatementBegin
SELECT public.runtime_begin_migration_write('migration-00021');

RESET ROLE;
GRANT CREATE ON SCHEMA public TO aboutme_runtime_owner;
SET LOCAL ROLE aboutme_runtime_owner;

CREATE TYPE public.runtime_rate_decision_result AS (
 allowed boolean, retry_after_seconds integer, bucket_kind text, partition smallint, effective_at timestamptz
);
CREATE TYPE public.runtime_password_failure_result AS (
 exhausted boolean, retry_after_seconds integer, bucket_kind text, partition smallint, effective_at timestamptz,
 allocated boolean, activity_refreshed boolean
);
CREATE TYPE public.runtime_password_clear_result AS (
 cleared boolean, bucket_kind text, partition smallint
);
CREATE TYPE public.runtime_failed_grant_reserve_result AS (
 allowed boolean, retry_after_seconds integer, bucket_kind text, partition smallint, replayed boolean
);
CREATE TYPE public.runtime_admission_attempt_finish_result AS (
 resolution text, stored_outcome text
);
CREATE TYPE public.runtime_admission_receipt_cleanup_result AS (
 deleted_count integer
);
CREATE TYPE public.runtime_rate_cleanup_result AS (
 deleted_count integer, effective_at timestamptz, policy_idle boolean
);

-- The only production rate time source. It takes no argument, has no login
-- grant, and its body stays exactly clock_timestamp().
CREATE FUNCTION public.runtime_sample_rate_time() RETURNS timestamptz
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT clock_timestamp()
$$;

-- PostgreSQL multiplies an interval only by double precision, and the one
-- exact alternative, (text)::interval, routes through the STABLE interval_in
-- and would make this per-row helper depend on the caller's DateStyle. The
-- double stays and the magnitude is bounded instead: every value within 2^53
-- converts exactly, so a larger one fails closed rather than rounding.
CREATE FUNCTION public.runtime_rate_interval(p_microseconds bigint) RETURNS interval
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF p_microseconds>9007199254740992 OR p_microseconds<(-9007199254740992) THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate interval is invalid';
 END IF;
 RETURN interval '1 microsecond'*p_microseconds::double precision;
END $$;

CREATE FUNCTION public.runtime_rate_ceil_div(p_value bigint,p_divisor bigint) RETURNS bigint
LANGUAGE sql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT p_value/p_divisor+CASE WHEN p_value%p_divisor>0 THEN 1 ELSE 0 END
$$;

-- Saturates before multiplying: a long forward jump reaches the threshold
-- comparison and assigns the full numerator without converting a huge
-- elapsed interval to microseconds.
CREATE FUNCTION public.runtime_rate_token_refill(p_capacity integer,p_window bigint,p_numerator bigint,p_refill_at timestamptz,p_now timestamptz) RETURNS bigint
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE full_value bigint; missing bigint; threshold bigint; elapsed bigint;
BEGIN
 IF p_capacity IS NULL OR p_capacity<=0 OR p_window IS NULL OR p_window<=0 OR p_numerator IS NULL OR p_numerator<0 OR p_refill_at IS NULL OR p_now IS NULL THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate token state is invalid';
 END IF;
 full_value:=p_capacity::bigint*p_window;
 IF p_numerator>full_value THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate token state is invalid'; END IF;
 missing:=full_value-p_numerator;
 IF missing=0 OR p_now<=p_refill_at THEN RETURN p_numerator; END IF;
 threshold:=public.runtime_rate_ceil_div(missing,p_capacity::bigint);
 IF p_now>=p_refill_at+public.runtime_rate_interval(threshold) THEN RETURN full_value; END IF;
 elapsed:=floor(EXTRACT(EPOCH FROM (p_now-p_refill_at))*1000000)::bigint;
 IF elapsed<0 THEN elapsed:=0; END IF;
 RETURN LEAST(full_value,p_numerator+elapsed*p_capacity::bigint);
END $$;

CREATE FUNCTION public.runtime_rate_token_retry(p_capacity integer,p_window bigint,p_numerator bigint) RETURNS integer
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE deficit bigint; micro bigint; seconds bigint;
BEGIN
 IF p_capacity IS NULL OR p_capacity<=0 OR p_window IS NULL OR p_window<=0 OR p_numerator IS NULL OR p_numerator<0 THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate token state is invalid';
 END IF;
 deficit:=p_window-p_numerator;
 IF deficit<=0 THEN RETURN 0; END IF;
 micro:=public.runtime_rate_ceil_div(deficit,p_capacity::bigint);
 seconds:=public.runtime_rate_ceil_div(micro,1000000);
 IF seconds<1 THEN seconds:=1; END IF;
 IF seconds>2147483647 THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate retry is invalid'; END IF;
 RETURN seconds::integer;
END $$;

CREATE FUNCTION public.runtime_rate_window_retry(p_started timestamptz,p_window bigint,p_now timestamptz) RETURNS integer
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE micro bigint; seconds bigint;
BEGIN
 IF p_started IS NULL OR p_window IS NULL OR p_now IS NULL THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate window state is invalid';
 END IF;
 micro:=floor(EXTRACT(EPOCH FROM (p_started+public.runtime_rate_interval(p_window)-p_now))*1000000)::bigint;
 IF micro<=0 THEN RETURN 1; END IF;
 seconds:=public.runtime_rate_ceil_div(micro,1000000);
 IF seconds<1 THEN seconds:=1; END IF;
 IF seconds>2147483647 THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate retry is invalid'; END IF;
 RETURN seconds::integer;
END $$;

-- Loads one closed catalog row. A caller-supplied unknown policy or an
-- algorithm outside the fixed function is 55000; stored drift is AM001.
CREATE FUNCTION public.runtime_rate_catalog(p_policy_id text,p_algorithm text,p_caller_supplied boolean) RETURNS public.shared_rate_policies
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE catalog_row public.shared_rate_policies; located boolean;
BEGIN
 SELECT * INTO catalog_row FROM public.shared_rate_policies WHERE policy_id=p_policy_id;
 located:=FOUND;
 IF NOT located OR (p_algorithm IS NOT NULL AND catalog_row.algorithm<>p_algorithm) THEN
  IF p_caller_supplied THEN RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='shared rate policy is unavailable'; END IF;
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate catalog is invalid';
 END IF;
 -- shared_rate_policies_exact_catalog_check (00018) pins every per-policy
 -- literal on the stored row and no trigger state can disable it, so this
 -- function asserts only the cross-cutting constants its own arithmetic and
 -- routing read instead of keeping a second, driftable copy of the catalog.
 IF catalog_row.capacity<=0 OR catalog_row.window_microseconds<=0
  OR catalog_row.ordinary_idle_microseconds<>86400000000 OR catalog_row.max_keys_per_partition<>10000 THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate catalog is invalid';
 END IF;
 RETURN catalog_row;
END $$;

-- Locks the policy clock, samples the owner helper exactly once, clamps the
-- sample to the durable high-water value and records the raw observation.
CREATE FUNCTION public.runtime_rate_sample(p_policy_id text) RETURNS timestamptz
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE high timestamptz; raw_now timestamptz; effective timestamptz;
BEGIN
 SELECT c.high_water_at INTO high FROM public.shared_policy_clocks c WHERE c.policy_id=p_policy_id FOR UPDATE;
 IF NOT FOUND OR high IS NULL THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate clock is invalid'; END IF;
 raw_now:=public.runtime_sample_rate_time();
 IF raw_now IS NULL THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate clock is invalid'; END IF;
 effective:=GREATEST(high,raw_now);
 UPDATE public.shared_policy_clocks SET high_water_at=effective,last_raw_at=raw_now,
  anomaly_count=CASE WHEN raw_now<high AND anomaly_count<9223372036854775807 THEN anomaly_count+1 ELSE anomaly_count END
  WHERE policy_id=p_policy_id;
 RETURN effective;
END $$;

CREATE FUNCTION public.runtime_rate_client_digest(p_client_id uuid) RETURNS bytea
LANGUAGE sql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT sha256(convert_to('aboutme.oauth.failed_grant.v1','UTF8')||uuid_send(p_client_id))
$$;

-- Legitimate expiry only: a full token bucket, an empty fixed window, an
-- empty rolling window, or a 24-hour idle row with no effective P22 debt.
CREATE FUNCTION public.runtime_rate_bucket_eligible(b public.shared_rate_buckets,p public.shared_rate_policies,p_now timestamptz) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT CASE b.algorithm
  WHEN 'token_bucket' THEN
   public.runtime_rate_token_refill(p.capacity,p.window_microseconds,b.token_numerator,b.refill_at,p_now)=p.capacity::bigint*p.window_microseconds
   OR p_now>=b.last_seen+public.runtime_rate_interval(p.ordinary_idle_microseconds)
  WHEN 'fixed_window' THEN
   (b.count=0
    OR (b.window_started_at IS NOT NULL AND p_now>=b.window_started_at+public.runtime_rate_interval(p.window_microseconds))
    OR p_now>=b.last_seen+public.runtime_rate_interval(p.ordinary_idle_microseconds))
   AND NOT EXISTS(SELECT 1 FROM public.shared_admission_attempts a
     WHERE a.policy_id=b.policy_id AND a.state='pending' AND a.bucket_kind='private'
       AND a.key_digest=b.key_digest AND a.partition=b.partition AND a.effective_until>p_now)
  WHEN 'rolling_slug' THEN
   NOT EXISTS(SELECT 1 FROM unnest(b.rolling_events) AS e(value) WHERE e.value>p_now-public.runtime_rate_interval(p.window_microseconds))
   OR p_now>=b.last_seen+public.runtime_rate_interval(p.ordinary_idle_microseconds)
  ELSE false END
$$;

CREATE FUNCTION public.runtime_rate_overflow_neutral(p public.shared_rate_policies) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT CASE p.algorithm
  WHEN 'token_bucket' THEN o.token_numerator=p.capacity::bigint*p.window_microseconds
  WHEN 'fixed_window' THEN o.count=0 AND o.window_started_at IS NULL
   AND NOT EXISTS(SELECT 1 FROM public.shared_admission_attempts a WHERE a.policy_id=p.policy_id AND a.state='pending' AND a.bucket_kind='overflow')
  WHEN 'rolling_slug' THEN cardinality(o.rolling_events)=0
  ELSE false END
 FROM public.shared_rate_overflow o WHERE o.policy_id=p.policy_id
$$;

-- Resolves the selected bucket's expired P22 reservations as neutral, clears
-- an expired committed window and restores an absent window only when both
-- committed count and stored pending debt are zero.
CREATE FUNCTION public.runtime_rate_normalize_window(p public.shared_rate_policies,p_kind text,p_digest bytea,p_partition smallint,p_now timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE started timestamptz; stored_count integer; next_started timestamptz; next_count integer; pending integer:=0; located boolean;
BEGIN
 IF p.algorithm<>'fixed_window' THEN RETURN; END IF;
 IF p.policy_id='oauth.failed_grant' THEN
  PERFORM 1 FROM public.shared_admission_attempts a
   WHERE a.policy_id=p.policy_id AND a.state='pending' AND a.bucket_kind=p_kind
     AND (p_kind='overflow' OR (a.key_digest=p_digest AND a.partition=p_partition))
   ORDER BY a.attempt_id FOR UPDATE;
  UPDATE public.shared_admission_attempts
   SET state='terminal',outcome='neutral',terminal_reason='window_expired',terminal_at=p_now
   WHERE policy_id=p.policy_id AND state='pending' AND bucket_kind=p_kind
     AND (p_kind='overflow' OR (key_digest=p_digest AND partition=p_partition))
     AND effective_until<=p_now;
  SELECT count(*) INTO pending FROM public.shared_admission_attempts a
   WHERE a.policy_id=p.policy_id AND a.state='pending' AND a.bucket_kind=p_kind
     AND (p_kind='overflow' OR (a.key_digest=p_digest AND a.partition=p_partition));
 END IF;
 IF p_kind='private' THEN
  SELECT b.window_started_at,b.count INTO started,stored_count FROM public.shared_rate_buckets b
   WHERE b.policy_id=p.policy_id AND b.key_digest=p_digest;
 ELSE
  SELECT o.window_started_at,o.count INTO started,stored_count FROM public.shared_rate_overflow o WHERE o.policy_id=p.policy_id;
 END IF;
 located:=FOUND;
 IF NOT located OR stored_count IS NULL THEN RETURN; END IF;
 next_count:=stored_count; next_started:=started;
 IF started IS NOT NULL AND p_now>=started+public.runtime_rate_interval(p.window_microseconds) THEN next_count:=0; END IF;
 IF next_count=0 AND pending=0 THEN next_started:=NULL; END IF;
 IF next_count IS DISTINCT FROM stored_count OR next_started IS DISTINCT FROM started THEN
  IF p_kind='private' THEN
   UPDATE public.shared_rate_buckets SET count=next_count,window_started_at=next_started
    WHERE policy_id=p.policy_id AND key_digest=p_digest;
  ELSE
   UPDATE public.shared_rate_overflow SET count=next_count,window_started_at=next_started WHERE policy_id=p.policy_id;
  END IF;
 END IF;
END $$;

-- Deletes at most one legitimately expired oldest row from one full enabled
-- partition. The partition row is already locked by the caller.
CREATE FUNCTION public.runtime_rate_expire_one(p public.shared_rate_policies,p_partition smallint,p_now timestamptz) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE candidate bytea; locked public.shared_rate_buckets;
BEGIN
 SELECT b.key_digest INTO candidate FROM public.shared_rate_buckets b
  WHERE b.policy_id=p.policy_id AND b.partition=p_partition AND public.runtime_rate_bucket_eligible(b,p,p_now)
  ORDER BY b.last_seen,b.key_digest LIMIT 1;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT * INTO locked FROM public.shared_rate_buckets b
  WHERE b.policy_id=p.policy_id AND b.key_digest=candidate FOR UPDATE;
 IF NOT FOUND OR locked.partition<>p_partition THEN RETURN false; END IF;
 PERFORM public.runtime_rate_normalize_window(p,'private',candidate,p_partition,p_now);
 SELECT * INTO locked FROM public.shared_rate_buckets b WHERE b.policy_id=p.policy_id AND b.key_digest=candidate;
 IF NOT FOUND OR NOT public.runtime_rate_bucket_eligible(locked,p,p_now) THEN RETURN false; END IF;
 DELETE FROM public.shared_rate_buckets WHERE policy_id=p.policy_id AND key_digest=candidate;
 UPDATE public.shared_rate_partitions SET active_keys=active_keys-1,updated_at=p_now
  WHERE policy_id=p.policy_id AND partition=p_partition;
 RETURN true;
END $$;

-- Nonlocking routing read, then clock-ordered locks: an existing key locks
-- only its bucket; an absent key locks partitions 1 then 2 before the bucket
-- or the shared overflow row.
CREATE FUNCTION public.runtime_rate_route(p public.shared_rate_policies,p_digest bytea,p_now timestamptz,p_allocate boolean,
 OUT o_kind text,OUT o_partition smallint,OUT o_allocated boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE routed smallint; locked smallint; target smallint; item record; partition_count integer;
BEGIN
 o_allocated:=false;
 SELECT b.partition INTO routed FROM public.shared_rate_buckets b WHERE b.policy_id=p.policy_id AND b.key_digest=p_digest;
 IF FOUND THEN
  SELECT b.partition INTO locked FROM public.shared_rate_buckets b
   WHERE b.policy_id=p.policy_id AND b.key_digest=p_digest FOR UPDATE;
  IF NOT FOUND OR locked IS DISTINCT FROM routed THEN
   RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate routing is invalid';
  END IF;
  o_kind:='private'; o_partition:=locked; RETURN;
 END IF;
 PERFORM 1 FROM public.shared_rate_partitions s WHERE s.policy_id=p.policy_id ORDER BY s.partition FOR UPDATE;
 SELECT count(*) INTO partition_count FROM public.shared_rate_partitions s WHERE s.policy_id=p.policy_id;
 IF partition_count<>2 THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate partitions are invalid'; END IF;
 IF p_allocate THEN
  FOR item IN SELECT s.partition FROM public.shared_rate_partitions s
   WHERE s.policy_id=p.policy_id AND s.enabled AND s.active_keys>=p.max_keys_per_partition ORDER BY s.partition LOOP
   PERFORM public.runtime_rate_expire_one(p,item.partition,p_now);
  END LOOP;
 END IF;
 SELECT s.partition INTO target FROM public.shared_rate_partitions s
  WHERE s.policy_id=p.policy_id AND s.enabled AND s.active_keys<p.max_keys_per_partition ORDER BY s.partition LIMIT 1;
 IF FOUND AND NOT p_allocate THEN o_kind:='private'; o_partition:=NULL; RETURN; END IF;
 IF NOT FOUND THEN
  PERFORM 1 FROM public.shared_rate_overflow o WHERE o.policy_id=p.policy_id FOR UPDATE;
  IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate overflow is missing'; END IF;
  o_kind:='overflow'; o_partition:=NULL; RETURN;
 END IF;
 INSERT INTO public.shared_rate_buckets(policy_id,key_digest,partition,algorithm,last_seen,token_numerator,refill_at,window_started_at,count,rolling_events)
 VALUES(p.policy_id,p_digest,target,p.algorithm,p_now,
  CASE WHEN p.algorithm='token_bucket' THEN p.capacity::bigint*p.window_microseconds END,
  CASE WHEN p.algorithm='token_bucket' THEN p_now END,
  NULL,
  CASE WHEN p.algorithm IN ('fixed_window','rolling_slug') THEN 0 END,
  CASE WHEN p.algorithm='rolling_slug' THEN ARRAY[]::timestamptz[] END);
 UPDATE public.shared_rate_partitions SET active_keys=active_keys+1,updated_at=p_now
  WHERE policy_id=p.policy_id AND partition=target;
 o_kind:='private'; o_partition:=target; o_allocated:=true;
END $$;

CREATE FUNCTION public.runtime_rate_apply_token(p public.shared_rate_policies,p_kind text,p_digest bytea,p_now timestamptz,
 OUT o_allowed boolean,OUT o_retry integer)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE numerator bigint; refill timestamptz;
BEGIN
 IF p_kind='private' THEN
  SELECT b.token_numerator,b.refill_at INTO numerator,refill FROM public.shared_rate_buckets b
   WHERE b.policy_id=p.policy_id AND b.key_digest=p_digest;
 ELSE
  SELECT o.token_numerator,o.refill_at INTO numerator,refill FROM public.shared_rate_overflow o WHERE o.policy_id=p.policy_id;
 END IF;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate bucket is missing'; END IF;
 numerator:=public.runtime_rate_token_refill(p.capacity,p.window_microseconds,numerator,refill,p_now);
 IF numerator>=p.window_microseconds THEN
  o_allowed:=true; o_retry:=0; numerator:=numerator-p.window_microseconds;
 ELSE
  o_allowed:=false; o_retry:=public.runtime_rate_token_retry(p.capacity,p.window_microseconds,numerator);
 END IF;
 IF p_kind='private' THEN
  UPDATE public.shared_rate_buckets SET token_numerator=numerator,refill_at=p_now,last_seen=p_now
   WHERE policy_id=p.policy_id AND key_digest=p_digest;
 ELSE
  UPDATE public.shared_rate_overflow SET token_numerator=numerator,refill_at=p_now,last_seen=p_now WHERE policy_id=p.policy_id;
 END IF;
END $$;

CREATE FUNCTION public.runtime_rate_apply_rolling(p public.shared_rate_policies,p_kind text,p_digest bytea,p_now timestamptz,
 OUT o_allowed boolean,OUT o_retry integer)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE events timestamptz[]; retained timestamptz[];
BEGIN
 IF p_kind='private' THEN
  SELECT b.rolling_events INTO events FROM public.shared_rate_buckets b
   WHERE b.policy_id=p.policy_id AND b.key_digest=p_digest;
 ELSE
  SELECT o.rolling_events INTO events FROM public.shared_rate_overflow o WHERE o.policy_id=p.policy_id;
 END IF;
 IF NOT FOUND OR events IS NULL THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate bucket is missing'; END IF;
 SELECT COALESCE(array_agg(e.value ORDER BY e.value),ARRAY[]::timestamptz[]) INTO retained
  FROM unnest(events) AS e(value) WHERE e.value>p_now-public.runtime_rate_interval(p.window_microseconds);
 IF cardinality(retained)>p.capacity THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate bucket is invalid'; END IF;
 IF cardinality(retained)<p.capacity THEN
  retained:=retained||p_now; o_allowed:=true; o_retry:=0;
 ELSE
  retained:=retained[2:]||p_now; o_allowed:=false; o_retry:=1;
 END IF;
 IF p_kind='private' THEN
  UPDATE public.shared_rate_buckets SET rolling_events=retained,count=cardinality(retained),last_seen=p_now
   WHERE policy_id=p.policy_id AND key_digest=p_digest;
 ELSE
  UPDATE public.shared_rate_overflow SET rolling_events=retained,count=cardinality(retained),last_seen=p_now WHERE policy_id=p.policy_id;
 END IF;
END $$;

-- Evaluates fixed-window exhaustion without persisting expiry, so P07 state
-- never sweeps, resets a window, or changes committed debt.
CREATE FUNCTION public.runtime_rate_failure_view(p public.shared_rate_policies,p_kind text,p_digest bytea,p_now timestamptz,
 OUT o_exhausted boolean,OUT o_retry integer)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE started timestamptz; stored_count integer; effective_count integer;
BEGIN
 IF p_kind='private' THEN
  SELECT b.window_started_at,b.count INTO started,stored_count FROM public.shared_rate_buckets b
   WHERE b.policy_id=p.policy_id AND b.key_digest=p_digest;
 ELSE
  SELECT o.window_started_at,o.count INTO started,stored_count FROM public.shared_rate_overflow o WHERE o.policy_id=p.policy_id;
 END IF;
 IF NOT FOUND OR stored_count IS NULL THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate bucket is missing'; END IF;
 effective_count:=stored_count;
 IF started IS NOT NULL AND p_now>=started+public.runtime_rate_interval(p.window_microseconds) THEN effective_count:=0; END IF;
 IF effective_count>=p.capacity THEN
  o_exhausted:=true; o_retry:=public.runtime_rate_window_retry(started,p.window_microseconds,p_now);
 ELSE
  o_exhausted:=false; o_retry:=0;
 END IF;
END $$;

CREATE FUNCTION public.runtime_admit_token_rate(p_policy_id text,p_key_digest bytea) RETURNS public.runtime_rate_decision_result
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE catalog_row public.shared_rate_policies; effective timestamptz; route record; applied record;
BEGIN
 IF session_user<>'aboutme_app' THEN RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='shared rate role is forbidden'; END IF;
 PERFORM public.runtime_require_write_entry();
 IF p_policy_id IS NULL OR p_key_digest IS NULL OR octet_length(p_key_digest)<>32 THEN
  RAISE EXCEPTION USING ERRCODE='22023',MESSAGE='shared rate input is invalid';
 END IF;
 catalog_row:=public.runtime_rate_catalog(p_policy_id,'token_bucket',true);
 effective:=public.runtime_rate_sample(catalog_row.policy_id);
 SELECT * INTO route FROM public.runtime_rate_route(catalog_row,p_key_digest,effective,true);
 SELECT * INTO applied FROM public.runtime_rate_apply_token(catalog_row,route.o_kind,p_key_digest,effective);
 RETURN ROW(applied.o_allowed,applied.o_retry,route.o_kind,route.o_partition,effective)::public.runtime_rate_decision_result;
END $$;

CREATE FUNCTION public.runtime_admit_slug_change(p_key_digest bytea) RETURNS public.runtime_rate_decision_result
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE catalog_row public.shared_rate_policies; effective timestamptz; route record; applied record;
BEGIN
 IF session_user<>'aboutme_app' THEN RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='shared rate role is forbidden'; END IF;
 PERFORM public.runtime_require_write_entry();
 IF p_key_digest IS NULL OR octet_length(p_key_digest)<>32 THEN
  RAISE EXCEPTION USING ERRCODE='22023',MESSAGE='shared rate input is invalid';
 END IF;
 catalog_row:=public.runtime_rate_catalog('resume.slug_change','rolling_slug',false);
 effective:=public.runtime_rate_sample(catalog_row.policy_id);
 SELECT * INTO route FROM public.runtime_rate_route(catalog_row,p_key_digest,effective,true);
 SELECT * INTO applied FROM public.runtime_rate_apply_rolling(catalog_row,route.o_kind,p_key_digest,effective);
 RETURN ROW(applied.o_allowed,applied.o_retry,route.o_kind,route.o_partition,effective)::public.runtime_rate_decision_result;
END $$;

CREATE FUNCTION public.runtime_password_failure_state(p_key_digest bytea) RETURNS public.runtime_password_failure_result
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE catalog_row public.shared_rate_policies; effective timestamptz; route record; evaluated record;
BEGIN
 IF session_user<>'aboutme_app' THEN RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='shared rate role is forbidden'; END IF;
 PERFORM public.runtime_require_write_entry();
 IF p_key_digest IS NULL OR octet_length(p_key_digest)<>32 THEN
  RAISE EXCEPTION USING ERRCODE='22023',MESSAGE='shared rate input is invalid';
 END IF;
 catalog_row:=public.runtime_rate_catalog('password.login_failure_email','fixed_window',false);
 effective:=public.runtime_rate_sample(catalog_row.policy_id);
 SELECT * INTO route FROM public.runtime_rate_route(catalog_row,p_key_digest,effective,false);
 IF route.o_kind='private' AND route.o_partition IS NULL THEN
  RETURN ROW(false,0,'private',NULL::smallint,effective,false,false)::public.runtime_password_failure_result;
 END IF;
 SELECT * INTO evaluated FROM public.runtime_rate_failure_view(catalog_row,route.o_kind,p_key_digest,effective);
 IF route.o_kind='private' THEN
  UPDATE public.shared_rate_buckets SET last_seen=effective WHERE policy_id=catalog_row.policy_id AND key_digest=p_key_digest;
 ELSE
  UPDATE public.shared_rate_overflow SET last_seen=effective WHERE policy_id=catalog_row.policy_id;
 END IF;
 RETURN ROW(evaluated.o_exhausted,evaluated.o_retry,route.o_kind,route.o_partition,effective,false,true)::public.runtime_password_failure_result;
END $$;

CREATE FUNCTION public.runtime_record_password_failure(p_key_digest bytea) RETURNS public.runtime_password_failure_result
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE catalog_row public.shared_rate_policies; effective timestamptz; route record;
 started timestamptz; stored_count integer; exhausted boolean; retry integer;
BEGIN
 IF session_user<>'aboutme_app' THEN RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='shared rate role is forbidden'; END IF;
 PERFORM public.runtime_require_write_entry();
 IF p_key_digest IS NULL OR octet_length(p_key_digest)<>32 THEN
  RAISE EXCEPTION USING ERRCODE='22023',MESSAGE='shared rate input is invalid';
 END IF;
 catalog_row:=public.runtime_rate_catalog('password.login_failure_email','fixed_window',false);
 effective:=public.runtime_rate_sample(catalog_row.policy_id);
 SELECT * INTO route FROM public.runtime_rate_route(catalog_row,p_key_digest,effective,true);
 PERFORM public.runtime_rate_normalize_window(catalog_row,route.o_kind,p_key_digest,route.o_partition,effective);
 IF route.o_kind='private' THEN
  SELECT b.window_started_at,b.count INTO started,stored_count FROM public.shared_rate_buckets b
   WHERE b.policy_id=catalog_row.policy_id AND b.key_digest=p_key_digest;
 ELSE
  SELECT o.window_started_at,o.count INTO started,stored_count FROM public.shared_rate_overflow o WHERE o.policy_id=catalog_row.policy_id;
 END IF;
 IF NOT FOUND OR stored_count IS NULL THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate bucket is missing'; END IF;
 IF stored_count=0 THEN started:=effective; stored_count:=1; ELSE stored_count:=LEAST(stored_count+1,catalog_row.capacity); END IF;
 IF route.o_kind='private' THEN
  UPDATE public.shared_rate_buckets SET count=stored_count,window_started_at=started,last_seen=effective
   WHERE policy_id=catalog_row.policy_id AND key_digest=p_key_digest;
 ELSE
  UPDATE public.shared_rate_overflow SET count=stored_count,window_started_at=started,last_seen=effective
   WHERE policy_id=catalog_row.policy_id;
 END IF;
 exhausted:=stored_count>=catalog_row.capacity;
 retry:=CASE WHEN exhausted THEN public.runtime_rate_window_retry(started,catalog_row.window_microseconds,effective) ELSE 0 END;
 RETURN ROW(exhausted,retry,route.o_kind,route.o_partition,effective,route.o_allocated,true)::public.runtime_password_failure_result;
END $$;

CREATE FUNCTION public.runtime_clear_password_failure(p_key_digest bytea) RETURNS public.runtime_password_clear_result
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE catalog_row public.shared_rate_policies; effective timestamptz; routed smallint; locked smallint;
BEGIN
 IF session_user<>'aboutme_app' THEN RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='shared rate role is forbidden'; END IF;
 PERFORM public.runtime_require_write_entry();
 IF p_key_digest IS NULL OR octet_length(p_key_digest)<>32 THEN
  RAISE EXCEPTION USING ERRCODE='22023',MESSAGE='shared rate input is invalid';
 END IF;
 catalog_row:=public.runtime_rate_catalog('password.login_failure_email','fixed_window',false);
 effective:=public.runtime_rate_sample(catalog_row.policy_id);
 SELECT b.partition INTO routed FROM public.shared_rate_buckets b
  WHERE b.policy_id=catalog_row.policy_id AND b.key_digest=p_key_digest;
 IF NOT FOUND THEN RETURN ROW(false,'private',NULL::smallint)::public.runtime_password_clear_result; END IF;
 PERFORM 1 FROM public.shared_rate_partitions s WHERE s.policy_id=catalog_row.policy_id AND s.partition=routed FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate partitions are invalid'; END IF;
 SELECT b.partition INTO locked FROM public.shared_rate_buckets b
  WHERE b.policy_id=catalog_row.policy_id AND b.key_digest=p_key_digest FOR UPDATE;
 IF NOT FOUND OR locked IS DISTINCT FROM routed THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate routing is invalid';
 END IF;
 DELETE FROM public.shared_rate_buckets WHERE policy_id=catalog_row.policy_id AND key_digest=p_key_digest;
 UPDATE public.shared_rate_partitions SET active_keys=active_keys-1,updated_at=effective
  WHERE policy_id=catalog_row.policy_id AND partition=locked;
 RETURN ROW(true,'private',locked)::public.runtime_password_clear_result;
END $$;

CREATE FUNCTION public.runtime_reserve_oauth_failed_grant(p_attempt_id uuid,p_client_id uuid) RETURNS public.runtime_failed_grant_reserve_result
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE catalog_row public.shared_rate_policies; effective timestamptz; digest bytea;
 attempt public.shared_admission_attempts; route record; started timestamptz; stored_count integer; pending integer; occupied integer;
BEGIN
 IF session_user<>'aboutme_app' THEN RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='shared rate role is forbidden'; END IF;
 PERFORM public.runtime_require_write_entry();
 IF p_attempt_id IS NULL OR p_attempt_id='00000000-0000-0000-0000-000000000000'::uuid
  OR p_client_id IS NULL OR p_client_id='00000000-0000-0000-0000-000000000000'::uuid THEN
  RAISE EXCEPTION USING ERRCODE='22023',MESSAGE='shared rate input is invalid';
 END IF;
 catalog_row:=public.runtime_rate_catalog('oauth.failed_grant','fixed_window',false);
 effective:=public.runtime_rate_sample(catalog_row.policy_id);
 digest:=public.runtime_rate_client_digest(p_client_id);
 SELECT * INTO attempt FROM public.shared_admission_attempts a WHERE a.attempt_id=p_attempt_id;
 IF FOUND THEN
  IF attempt.policy_id<>catalog_row.policy_id OR attempt.client_id<>p_client_id THEN
   RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='shared rate attempt identity conflicts';
  END IF;
  IF attempt.bucket_kind='private' THEN
   PERFORM 1 FROM public.shared_rate_buckets b WHERE b.policy_id=catalog_row.policy_id AND b.key_digest=attempt.key_digest FOR UPDATE;
  ELSE
   PERFORM 1 FROM public.shared_rate_overflow o WHERE o.policy_id=catalog_row.policy_id FOR UPDATE;
  END IF;
  SELECT * INTO attempt FROM public.shared_admission_attempts a WHERE a.attempt_id=p_attempt_id FOR UPDATE;
  IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate attempt is missing'; END IF;
  IF attempt.client_id<>p_client_id THEN
   RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='shared rate attempt identity conflicts';
  END IF;
  RETURN ROW(true,0,attempt.bucket_kind,attempt.partition,true)::public.runtime_failed_grant_reserve_result;
 END IF;
 SELECT * INTO route FROM public.runtime_rate_route(catalog_row,digest,effective,true);
 PERFORM public.runtime_rate_normalize_window(catalog_row,route.o_kind,digest,route.o_partition,effective);
 IF route.o_kind='private' THEN
  SELECT b.window_started_at,b.count INTO started,stored_count FROM public.shared_rate_buckets b
   WHERE b.policy_id=catalog_row.policy_id AND b.key_digest=digest;
 ELSE
  SELECT o.window_started_at,o.count INTO started,stored_count FROM public.shared_rate_overflow o WHERE o.policy_id=catalog_row.policy_id;
 END IF;
 IF NOT FOUND OR stored_count IS NULL THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate bucket is missing'; END IF;
 SELECT count(*) INTO pending FROM public.shared_admission_attempts a
  WHERE a.policy_id=catalog_row.policy_id AND a.state='pending' AND a.bucket_kind=route.o_kind
    AND (route.o_kind='overflow' OR (a.key_digest=digest AND a.partition=route.o_partition));
 occupied:=stored_count+pending;
 IF occupied>catalog_row.capacity THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate bucket is invalid'; END IF;
 IF occupied>=catalog_row.capacity THEN
  IF started IS NULL THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate bucket is invalid'; END IF;
  IF route.o_kind='private' THEN
   UPDATE public.shared_rate_buckets SET last_seen=effective WHERE policy_id=catalog_row.policy_id AND key_digest=digest;
  ELSE
   UPDATE public.shared_rate_overflow SET last_seen=effective WHERE policy_id=catalog_row.policy_id;
  END IF;
  RETURN ROW(false,public.runtime_rate_window_retry(started,catalog_row.window_microseconds,effective),NULL::text,NULL::smallint,false)::public.runtime_failed_grant_reserve_result;
 END IF;
 IF started IS NULL THEN
  started:=effective;
  IF route.o_kind='private' THEN
   UPDATE public.shared_rate_buckets SET window_started_at=started WHERE policy_id=catalog_row.policy_id AND key_digest=digest;
  ELSE
   UPDATE public.shared_rate_overflow SET window_started_at=started WHERE policy_id=catalog_row.policy_id;
  END IF;
 END IF;
 INSERT INTO public.shared_admission_attempts(attempt_id,policy_id,client_id,bucket_kind,partition,key_digest,window_started_at,reserved_at,effective_until,state)
 VALUES(p_attempt_id,catalog_row.policy_id,p_client_id,route.o_kind,route.o_partition,
  CASE WHEN route.o_kind='private' THEN digest END,started,effective,
  started+public.runtime_rate_interval(catalog_row.window_microseconds),'pending');
 IF route.o_kind='private' THEN
  UPDATE public.shared_rate_buckets SET last_seen=effective WHERE policy_id=catalog_row.policy_id AND key_digest=digest;
 ELSE
  UPDATE public.shared_rate_overflow SET last_seen=effective WHERE policy_id=catalog_row.policy_id;
 END IF;
 RETURN ROW(true,0,route.o_kind,route.o_partition,false)::public.runtime_failed_grant_reserve_result;
END $$;

CREATE FUNCTION public.runtime_finish_admission_attempt(p_attempt_id uuid,p_outcome text) RETURNS public.runtime_admission_attempt_finish_result
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE catalog_row public.shared_rate_policies; effective timestamptz; attempt public.shared_admission_attempts;
 started timestamptz; stored_count integer;
BEGIN
 IF session_user<>'aboutme_app' THEN RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='shared rate role is forbidden'; END IF;
 PERFORM public.runtime_require_write_entry();
 IF p_attempt_id IS NULL OR p_attempt_id='00000000-0000-0000-0000-000000000000'::uuid
  OR p_outcome IS NULL OR p_outcome NOT IN ('failure','neutral','success') THEN
  RAISE EXCEPTION USING ERRCODE='22023',MESSAGE='shared rate input is invalid';
 END IF;
 catalog_row:=public.runtime_rate_catalog('oauth.failed_grant','fixed_window',false);
 effective:=public.runtime_rate_sample(catalog_row.policy_id);
 SELECT * INTO attempt FROM public.shared_admission_attempts a WHERE a.attempt_id=p_attempt_id;
 IF NOT FOUND THEN RETURN ROW('absent_noop',NULL::text)::public.runtime_admission_attempt_finish_result; END IF;
 IF attempt.policy_id<>catalog_row.policy_id THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate attempt is invalid'; END IF;
 IF attempt.bucket_kind='private' THEN
  PERFORM 1 FROM public.shared_rate_partitions s WHERE s.policy_id=catalog_row.policy_id ORDER BY s.partition FOR UPDATE;
  PERFORM 1 FROM public.shared_rate_buckets b WHERE b.policy_id=catalog_row.policy_id AND b.key_digest=attempt.key_digest FOR UPDATE;
 ELSE
  PERFORM 1 FROM public.shared_rate_overflow o WHERE o.policy_id=catalog_row.policy_id FOR UPDATE;
 END IF;
 PERFORM public.runtime_rate_normalize_window(catalog_row,attempt.bucket_kind,attempt.key_digest,attempt.partition,effective);
 SELECT * INTO attempt FROM public.shared_admission_attempts a WHERE a.attempt_id=p_attempt_id FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate attempt is missing'; END IF;
 IF attempt.state='terminal' THEN
  IF attempt.terminal_reason='caller_finish' THEN
   IF attempt.outcome IS DISTINCT FROM p_outcome THEN
    RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='shared rate attempt outcome conflicts';
   END IF;
   RETURN ROW('caller_replay',attempt.outcome)::public.runtime_admission_attempt_finish_result;
  END IF;
  RETURN ROW('system_noop',attempt.outcome)::public.runtime_admission_attempt_finish_result;
 END IF;
 IF attempt.bucket_kind='private' THEN
  SELECT b.window_started_at,b.count INTO started,stored_count FROM public.shared_rate_buckets b
   WHERE b.policy_id=catalog_row.policy_id AND b.key_digest=attempt.key_digest;
 ELSE
  SELECT o.window_started_at,o.count INTO started,stored_count FROM public.shared_rate_overflow o WHERE o.policy_id=catalog_row.policy_id;
 END IF;
 IF NOT FOUND OR stored_count IS NULL OR started IS DISTINCT FROM attempt.window_started_at THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate bucket is invalid';
 END IF;
 IF p_outcome='success' AND attempt.bucket_kind='private' THEN
  UPDATE public.shared_admission_attempts
   SET state='terminal',outcome='neutral',terminal_reason='private_success_clear',terminal_at=effective
   WHERE policy_id=catalog_row.policy_id AND state='pending' AND bucket_kind='private'
     AND key_digest=attempt.key_digest AND partition=attempt.partition AND attempt_id<>p_attempt_id;
 END IF;
 UPDATE public.shared_admission_attempts
  SET state='terminal',outcome=p_outcome,terminal_reason='caller_finish',terminal_at=effective
  WHERE attempt_id=p_attempt_id;
 IF p_outcome='failure' THEN
  IF stored_count+1>catalog_row.capacity THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate bucket is invalid'; END IF;
  IF attempt.bucket_kind='private' THEN
   UPDATE public.shared_rate_buckets SET count=stored_count+1,last_seen=effective
    WHERE policy_id=catalog_row.policy_id AND key_digest=attempt.key_digest;
  ELSE
   UPDATE public.shared_rate_overflow SET count=stored_count+1,last_seen=effective WHERE policy_id=catalog_row.policy_id;
  END IF;
 ELSIF p_outcome='success' AND attempt.bucket_kind='private' THEN
  DELETE FROM public.shared_rate_buckets WHERE policy_id=catalog_row.policy_id AND key_digest=attempt.key_digest;
  UPDATE public.shared_rate_partitions SET active_keys=active_keys-1,updated_at=effective
   WHERE policy_id=catalog_row.policy_id AND partition=attempt.partition;
 ELSIF attempt.bucket_kind='private' THEN
  UPDATE public.shared_rate_buckets SET last_seen=effective WHERE policy_id=catalog_row.policy_id AND key_digest=attempt.key_digest;
 ELSE
  UPDATE public.shared_rate_overflow SET last_seen=effective WHERE policy_id=catalog_row.policy_id;
 END IF;
 PERFORM public.runtime_rate_normalize_window(catalog_row,attempt.bucket_kind,attempt.key_digest,attempt.partition,effective);
 RETURN ROW('caller_finished',p_outcome)::public.runtime_admission_attempt_finish_result;
END $$;

CREATE FUNCTION public.runtime_cleanup_admission_attempt_receipts(p_page_size integer) RETURNS public.runtime_admission_receipt_cleanup_result
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE catalog_row public.shared_rate_policies; effective timestamptz; cutoff timestamptz; ids uuid[]; deleted integer:=0;
BEGIN
 IF session_user<>'aboutme_maintenance' THEN RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='shared rate cleanup role is forbidden'; END IF;
 PERFORM public.runtime_require_write_entry();
 IF p_page_size IS NULL OR p_page_size<1 OR p_page_size>256 THEN
  RAISE EXCEPTION USING ERRCODE='22023',MESSAGE='shared rate input is invalid';
 END IF;
 catalog_row:=public.runtime_rate_catalog('oauth.failed_grant','fixed_window',false);
 effective:=public.runtime_rate_sample(catalog_row.policy_id);
 cutoff:=effective-interval '24 hours';
 SELECT array_agg(q.attempt_id ORDER BY q.terminal_at,q.attempt_id) INTO ids FROM (
  SELECT a.attempt_id,a.terminal_at FROM public.shared_admission_attempts a
   WHERE a.policy_id=catalog_row.policy_id AND a.state='terminal' AND a.terminal_at<=cutoff
   ORDER BY a.terminal_at,a.attempt_id LIMIT p_page_size FOR UPDATE) q;
 IF ids IS NULL THEN RETURN ROW(0)::public.runtime_admission_receipt_cleanup_result; END IF;
 DELETE FROM public.shared_admission_attempts
  WHERE attempt_id=ANY(ids) AND state='terminal' AND terminal_at<=cutoff;
 GET DIAGNOSTICS deleted=ROW_COUNT;
 RETURN ROW(deleted)::public.runtime_admission_receipt_cleanup_result;
END $$;

CREATE FUNCTION public.runtime_cleanup_rate_buckets(p_policy_id text,p_page_size integer) RETURNS public.runtime_rate_cleanup_result
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE catalog_row public.shared_rate_policies; effective timestamptz; candidates bytea[]; item record;
 locked public.shared_rate_buckets; deleted integer:=0; removed_1 integer:=0; removed_2 integer:=0;
 numerator bigint; refill timestamptz; events timestamptz[]; retained timestamptz[]; idle boolean;
BEGIN
 IF session_user<>'aboutme_maintenance' THEN RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='shared rate cleanup role is forbidden'; END IF;
 PERFORM public.runtime_require_write_entry();
 IF p_policy_id IS NULL OR p_page_size IS NULL OR p_page_size<1 OR p_page_size>256 THEN
  RAISE EXCEPTION USING ERRCODE='22023',MESSAGE='shared rate input is invalid';
 END IF;
 catalog_row:=public.runtime_rate_catalog(p_policy_id,NULL::text,true);
 effective:=public.runtime_rate_sample(catalog_row.policy_id);
 SELECT array_agg(q.key_digest ORDER BY q.last_seen,q.key_digest) INTO candidates FROM (
  SELECT b.key_digest,b.last_seen FROM public.shared_rate_buckets b
   WHERE b.policy_id=catalog_row.policy_id AND public.runtime_rate_bucket_eligible(b,catalog_row,effective)
   ORDER BY b.last_seen,b.key_digest LIMIT p_page_size) q;
 PERFORM 1 FROM public.shared_rate_partitions s WHERE s.policy_id=catalog_row.policy_id ORDER BY s.partition FOR UPDATE;
 IF candidates IS NOT NULL THEN
  PERFORM 1 FROM public.shared_rate_buckets b
   WHERE b.policy_id=catalog_row.policy_id AND b.key_digest=ANY(candidates) ORDER BY b.key_digest FOR UPDATE;
 END IF;
 PERFORM 1 FROM public.shared_rate_overflow o WHERE o.policy_id=catalog_row.policy_id FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate overflow is missing'; END IF;
 IF catalog_row.policy_id='oauth.failed_grant' THEN
  PERFORM 1 FROM public.shared_admission_attempts a
   WHERE a.policy_id=catalog_row.policy_id AND a.state='pending'
     AND (a.bucket_kind='overflow' OR (candidates IS NOT NULL AND a.key_digest=ANY(candidates)))
   ORDER BY a.attempt_id FOR UPDATE;
 END IF;
 IF candidates IS NOT NULL THEN
  FOR item IN SELECT b.key_digest,b.partition FROM public.shared_rate_buckets b
   WHERE b.policy_id=catalog_row.policy_id AND b.key_digest=ANY(candidates) ORDER BY b.key_digest LOOP
   PERFORM public.runtime_rate_normalize_window(catalog_row,'private',item.key_digest,item.partition,effective);
   SELECT * INTO locked FROM public.shared_rate_buckets b
    WHERE b.policy_id=catalog_row.policy_id AND b.key_digest=item.key_digest;
   IF FOUND AND public.runtime_rate_bucket_eligible(locked,catalog_row,effective) THEN
    DELETE FROM public.shared_rate_buckets WHERE policy_id=catalog_row.policy_id AND key_digest=item.key_digest;
    deleted:=deleted+1;
    IF item.partition=1 THEN removed_1:=removed_1+1; ELSE removed_2:=removed_2+1; END IF;
   END IF;
  END LOOP;
 END IF;
 IF removed_1>0 THEN
  UPDATE public.shared_rate_partitions SET active_keys=active_keys-removed_1,updated_at=effective
   WHERE policy_id=catalog_row.policy_id AND partition=1;
 END IF;
 IF removed_2>0 THEN
  UPDATE public.shared_rate_partitions SET active_keys=active_keys-removed_2,updated_at=effective
   WHERE policy_id=catalog_row.policy_id AND partition=2;
 END IF;
 IF catalog_row.algorithm='token_bucket' THEN
  SELECT o.token_numerator,o.refill_at INTO numerator,refill FROM public.shared_rate_overflow o WHERE o.policy_id=catalog_row.policy_id;
  numerator:=public.runtime_rate_token_refill(catalog_row.capacity,catalog_row.window_microseconds,numerator,refill,effective);
  UPDATE public.shared_rate_overflow SET token_numerator=numerator,refill_at=effective WHERE policy_id=catalog_row.policy_id;
 ELSIF catalog_row.algorithm='fixed_window' THEN
  PERFORM public.runtime_rate_normalize_window(catalog_row,'overflow',NULL::bytea,NULL::smallint,effective);
 ELSE
  SELECT o.rolling_events INTO events FROM public.shared_rate_overflow o WHERE o.policy_id=catalog_row.policy_id;
  SELECT COALESCE(array_agg(e.value ORDER BY e.value),ARRAY[]::timestamptz[]) INTO retained
   FROM unnest(events) AS e(value) WHERE e.value>effective-public.runtime_rate_interval(catalog_row.window_microseconds);
  IF cardinality(retained)<>cardinality(events) THEN
   UPDATE public.shared_rate_overflow SET rolling_events=retained,count=cardinality(retained) WHERE policy_id=catalog_row.policy_id;
  END IF;
 END IF;
 idle:=NOT EXISTS(SELECT 1 FROM public.shared_rate_buckets b WHERE b.policy_id=catalog_row.policy_id)
  AND public.runtime_rate_overflow_neutral(catalog_row)
  AND NOT EXISTS(SELECT 1 FROM public.shared_admission_attempts a WHERE a.policy_id=catalog_row.policy_id AND a.state='pending');
 RETURN ROW(deleted,effective,idle)::public.runtime_rate_cleanup_result;
END $$;

REVOKE ALL ON TYPE public.runtime_rate_decision_result,public.runtime_password_failure_result,public.runtime_password_clear_result,
 public.runtime_failed_grant_reserve_result,public.runtime_admission_attempt_finish_result,
 public.runtime_admission_receipt_cleanup_result,public.runtime_rate_cleanup_result FROM PUBLIC;
REVOKE ALL ON FUNCTION public.runtime_sample_rate_time(),public.runtime_rate_interval(bigint),public.runtime_rate_ceil_div(bigint,bigint),
 public.runtime_rate_token_refill(integer,bigint,bigint,timestamptz,timestamptz),public.runtime_rate_token_retry(integer,bigint,bigint),
 public.runtime_rate_window_retry(timestamptz,bigint,timestamptz),public.runtime_rate_catalog(text,text,boolean),
 public.runtime_rate_sample(text),public.runtime_rate_client_digest(uuid),
 public.runtime_rate_bucket_eligible(public.shared_rate_buckets,public.shared_rate_policies,timestamptz),
 public.runtime_rate_overflow_neutral(public.shared_rate_policies),
 public.runtime_rate_normalize_window(public.shared_rate_policies,text,bytea,smallint,timestamptz),
 public.runtime_rate_expire_one(public.shared_rate_policies,smallint,timestamptz),
 public.runtime_rate_route(public.shared_rate_policies,bytea,timestamptz,boolean),
 public.runtime_rate_apply_token(public.shared_rate_policies,text,bytea,timestamptz),
 public.runtime_rate_apply_rolling(public.shared_rate_policies,text,bytea,timestamptz),
 public.runtime_rate_failure_view(public.shared_rate_policies,text,bytea,timestamptz)
 FROM PUBLIC,aboutme_app,aboutme_maintenance,aboutme_lifecycle_command,aboutme_fencing_proof,aboutme_restore_verify,aboutme_migrator;
REVOKE ALL ON FUNCTION public.runtime_admit_token_rate(text,bytea),public.runtime_password_failure_state(bytea),
 public.runtime_record_password_failure(bytea),public.runtime_clear_password_failure(bytea),public.runtime_admit_slug_change(bytea),
 public.runtime_reserve_oauth_failed_grant(uuid,uuid),public.runtime_finish_admission_attempt(uuid,text),
 public.runtime_cleanup_admission_attempt_receipts(integer),public.runtime_cleanup_rate_buckets(text,integer)
 FROM PUBLIC,aboutme_app,aboutme_maintenance,aboutme_lifecycle_command,aboutme_fencing_proof,aboutme_restore_verify,aboutme_migrator;
GRANT USAGE ON TYPE public.runtime_rate_decision_result,public.runtime_password_failure_result,public.runtime_password_clear_result,
 public.runtime_failed_grant_reserve_result,public.runtime_admission_attempt_finish_result TO aboutme_app;
GRANT USAGE ON TYPE public.runtime_admission_receipt_cleanup_result,public.runtime_rate_cleanup_result TO aboutme_maintenance;
GRANT EXECUTE ON FUNCTION public.runtime_admit_token_rate(text,bytea),public.runtime_password_failure_state(bytea),
 public.runtime_record_password_failure(bytea),public.runtime_clear_password_failure(bytea),public.runtime_admit_slug_change(bytea),
 public.runtime_reserve_oauth_failed_grant(uuid,uuid),public.runtime_finish_admission_attempt(uuid,text) TO aboutme_app;
GRANT EXECUTE ON FUNCTION public.runtime_cleanup_admission_attempt_receipts(integer),public.runtime_cleanup_rate_buckets(text,integer) TO aboutme_maintenance;

RESET ROLE;
REVOKE CREATE ON SCHEMA public FROM aboutme_runtime_owner;
SELECT public.runtime_finish_write();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 1;
-- +goose StatementEnd
