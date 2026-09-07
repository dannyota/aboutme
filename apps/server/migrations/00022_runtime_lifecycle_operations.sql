-- +goose Up
-- +goose StatementBegin
SELECT public.runtime_begin_migration_write('migration-00022');

RESET ROLE;
GRANT CREATE ON SCHEMA public TO aboutme_runtime_owner;
SET LOCAL ROLE aboutme_runtime_owner;

-- The only production lifecycle time source. It takes no argument, has no
-- login grant, and its body stays exactly clock_timestamp(). It is distinct
-- from runtime_sample_rate_time so the two test seams cannot interfere.
CREATE FUNCTION public.runtime_sample_lifecycle_time() RETURNS timestamptz
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT clock_timestamp()
$$;

-- One canonical field: type byte, presence byte, four-byte big-endian length
-- and payload. A null payload emits null presence and zero length; a present
-- empty text payload still emits present.
CREATE FUNCTION public.runtime_lifecycle_digest_field(p_field_type smallint,p_payload bytea) RETURNS bytea
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE payload_length integer;
BEGIN
 IF p_field_type IS NULL OR p_field_type NOT IN (1,2,3,4) THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='lifecycle digest field is invalid';
 END IF;
 IF p_payload IS NULL THEN
  RETURN set_byte(decode('0000','hex'),0,p_field_type::integer)||decode('00000000','hex');
 END IF;
 payload_length:=octet_length(p_payload);
 IF (p_field_type=1 AND payload_length<>16)
  OR (p_field_type=2 AND payload_length<>8)
  OR (p_field_type=4 AND (payload_length<>1 OR get_byte(p_payload,0) NOT IN (0,1))) THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='lifecycle digest field is invalid';
 END IF;
 RETURN set_byte(set_byte(decode('0000','hex'),0,p_field_type::integer),1,1)||int4send(payload_length)||p_payload;
END $$;

-- SHA-256 over the fixed ASCII domain, one NUL, the kind byte and the framed
-- field concatenation. Kind 1 is arguments and kind 2 is a result.
CREATE FUNCTION public.runtime_lifecycle_digest(p_kind smallint,p_framed bytea) RETURNS bytea
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF p_kind IS NULL OR p_kind NOT IN (1,2) OR p_framed IS NULL THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='lifecycle digest input is invalid';
 END IF;
 RETURN sha256(convert_to('aboutme.runtime-lifecycle-step.v1','UTF8')||decode('00','hex')
  ||set_byte(decode('00','hex'),0,p_kind::integer)||p_framed);
END $$;

CREATE FUNCTION public.runtime_lifecycle_field_uuid(p_value uuid) RETURNS bytea
LANGUAGE sql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT public.runtime_lifecycle_digest_field(1::smallint,CASE WHEN p_value IS NULL THEN NULL ELSE uuid_send(p_value) END)
$$;

CREATE FUNCTION public.runtime_lifecycle_field_int8(p_value bigint) RETURNS bytea
LANGUAGE sql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT public.runtime_lifecycle_digest_field(2::smallint,CASE WHEN p_value IS NULL THEN NULL ELSE int8send(p_value) END)
$$;

CREATE FUNCTION public.runtime_lifecycle_field_text(p_value text) RETURNS bytea
LANGUAGE sql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT public.runtime_lifecycle_digest_field(3::smallint,CASE WHEN p_value IS NULL THEN NULL ELSE convert_to(p_value,'UTF8') END)
$$;

CREATE FUNCTION public.runtime_lifecycle_field_bool(p_value boolean) RETURNS bytea
LANGUAGE sql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT public.runtime_lifecycle_digest_field(4::smallint,
  CASE WHEN p_value IS NULL THEN NULL WHEN p_value THEN decode('01','hex') ELSE decode('00','hex') END)
$$;

-- The nineteen canonical result fields in their fixed order. Result generation
-- and controller generation both appear, as do operation ID and stored
-- controller operation ID; the schema requires each pair equal.
CREATE FUNCTION public.runtime_lifecycle_result_fields(
 p_operation_id text,p_action text,p_result_generation bigint,p_result_kind text,
 p_replica_id uuid,p_replica_kind text,p_replica_state text,
 p_desired bigint,p_serving bigint,p_maintenance bigint,
 p_partition_1 boolean,p_partition_2 boolean,
 p_capacity_generation bigint,p_controller_generation bigint,p_controller_operation_id text,
 p_admission boolean,p_lifecycle_phase text,p_write_gate text,p_write_generation bigint
) RETURNS bytea LANGUAGE sql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT public.runtime_lifecycle_field_text(p_operation_id)||public.runtime_lifecycle_field_text(p_action)
  ||public.runtime_lifecycle_field_int8(p_result_generation)||public.runtime_lifecycle_field_text(p_result_kind)
  ||public.runtime_lifecycle_field_uuid(p_replica_id)||public.runtime_lifecycle_field_text(p_replica_kind)
  ||public.runtime_lifecycle_field_text(p_replica_state)||public.runtime_lifecycle_field_int8(p_desired)
  ||public.runtime_lifecycle_field_int8(p_serving)||public.runtime_lifecycle_field_int8(p_maintenance)
  ||public.runtime_lifecycle_field_bool(p_partition_1)||public.runtime_lifecycle_field_bool(p_partition_2)
  ||public.runtime_lifecycle_field_int8(p_capacity_generation)||public.runtime_lifecycle_field_int8(p_controller_generation)
  ||public.runtime_lifecycle_field_text(p_controller_operation_id)||public.runtime_lifecycle_field_bool(p_admission)
  ||public.runtime_lifecycle_field_text(p_lifecycle_phase)||public.runtime_lifecycle_field_text(p_write_gate)
  ||public.runtime_lifecycle_field_int8(p_write_generation)
$$;

CREATE FUNCTION public.runtime_lifecycle_result_digest(
 p_operation_id text,p_action text,p_result_generation bigint,p_result_kind text,
 p_replica_id uuid,p_replica_kind text,p_replica_state text,
 p_desired bigint,p_serving bigint,p_maintenance bigint,
 p_partition_1 boolean,p_partition_2 boolean,
 p_capacity_generation bigint,p_controller_generation bigint,p_controller_operation_id text,
 p_admission boolean,p_lifecycle_phase text,p_write_gate text,p_write_generation bigint
) RETURNS bytea LANGUAGE sql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT public.runtime_lifecycle_digest(2::smallint,public.runtime_lifecycle_result_fields(
  p_operation_id,p_action,p_result_generation,p_result_kind,p_replica_id,p_replica_kind,p_replica_state,
  p_desired,p_serving,p_maintenance,p_partition_1,p_partition_2,p_capacity_generation,p_controller_generation,
  p_controller_operation_id,p_admission,p_lifecycle_phase,p_write_gate,p_write_generation))
$$;

CREATE FUNCTION public.runtime_lifecycle_assert_vector(p_kind smallint,p_framed bytea,p_bytes integer,p_digest text) RETURNS void
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF octet_length(p_framed)+35<>p_bytes OR public.runtime_lifecycle_digest(p_kind,p_framed)<>decode(p_digest,'hex') THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='lifecycle digest vector mismatch';
 END IF;
END $$;

-- The ten published vectors, computed from literal SQL values. Both extra
-- variants (null and non-null activation replacement, serving and maintenance
-- begin-wake) are included, and the two wake encodings are pinned here without
-- installing any wake action.
CREATE FUNCTION public.runtime_lifecycle_assert_digest_vectors() RETURNS void
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE
 v_replica uuid:='11111111-2222-4333-8444-555555555555';
 v_replaced uuid:='aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee';
 v_instance text:='i-0123456789abcdef0';
 v_container text:='arn:aws:ecs:ap-southeast-1:123456789012:container-instance/aboutme/fixture';
 v_caddy text:='arn:aws:ecs:ap-southeast-1:123456789012:task/aboutme/caddy-fixture';
 v_go text:='arn:aws:ecs:ap-southeast-1:123456789012:task/aboutme/go-fixture';
 v_nuxt text:='arn:aws:ecs:ap-southeast-1:123456789012:task/aboutme/nuxt-fixture';
 v_release text:='sha256:'||repeat('a',64);
 v_expected bigint:=40;
 v_result bigint:=41;
 v_identity bytea;
BEGIN
 v_identity:=public.runtime_lifecycle_field_uuid(v_replica)||public.runtime_lifecycle_field_text(v_instance)
  ||public.runtime_lifecycle_field_text(v_container)||public.runtime_lifecycle_field_text(v_caddy)
  ||public.runtime_lifecycle_field_text(v_go)||public.runtime_lifecycle_field_text(v_nuxt)
  ||public.runtime_lifecycle_field_text(v_release);

 PERFORM public.runtime_lifecycle_assert_vector(1::smallint,
  public.runtime_lifecycle_field_text('op-scale-out')||public.runtime_lifecycle_field_text('prepare_scale_out')
  ||public.runtime_lifecycle_field_int8(v_expected),
  90,'cf000ef3bf307c8cdf230c419a40430e53390fe2cae60111951cc08a28a344f3');
 PERFORM public.runtime_lifecycle_assert_vector(2::smallint,
  public.runtime_lifecycle_result_fields('op-scale-out','prepare_scale_out',v_result,'capacity',
   NULL::uuid,NULL::text,NULL::text,2,1,0,true,false,71,v_result,'op-scale-out',true,'online',NULL::text,NULL::bigint),
  255,'97583a4d21b893730e66b7862246cc28922a0227c3cded0674bf10bd83d5414c');

 PERFORM public.runtime_lifecycle_assert_vector(1::smallint,
  public.runtime_lifecycle_field_text('op-activate-new')||public.runtime_lifecycle_field_text('activate_replica_capacity')
  ||public.runtime_lifecycle_field_int8(v_expected)||v_identity||public.runtime_lifecycle_field_uuid(NULL::uuid),
  523,'a0cea0d8c5c4b701e413c009af40bdc1d09a696fb160494fe01f3a45e7a6e1b9');
 PERFORM public.runtime_lifecycle_assert_vector(2::smallint,
  public.runtime_lifecycle_result_fields('op-activate-new','activate_replica_capacity',v_result,'replica',
   v_replica,'serving','active',NULL::bigint,NULL::bigint,NULL::bigint,NULL::boolean,NULL::boolean,
   71,v_result,'op-activate-new',true,'online',NULL::text,NULL::bigint),
  271,'bd3d991093d7ad39d56858eafbab2afb3aa27b7dd224059d6d37474741a5a8a2');

 PERFORM public.runtime_lifecycle_assert_vector(1::smallint,
  public.runtime_lifecycle_field_text('op-activate-replacement')||public.runtime_lifecycle_field_text('activate_replica_capacity')
  ||public.runtime_lifecycle_field_int8(v_expected)||v_identity||public.runtime_lifecycle_field_uuid(v_replaced),
  547,'22cabd95d5d83122cbec4395f2e1b0256190af5dbaff7eb7e14160b3d7a6c5c2');
 PERFORM public.runtime_lifecycle_assert_vector(2::smallint,
  public.runtime_lifecycle_result_fields('op-activate-replacement','activate_replica_capacity',v_result,'replica',
   v_replica,'serving','active',NULL::bigint,NULL::bigint,NULL::bigint,NULL::boolean,NULL::boolean,
   71,v_result,'op-activate-replacement',true,'online',NULL::text,NULL::bigint),
  287,'01ae3ed8ce5342cfcf18f2ebe547bbf1c63a1f0b9ea64c1b920669767409ab7f');

 PERFORM public.runtime_lifecycle_assert_vector(1::smallint,
  public.runtime_lifecycle_field_text('op-scale-in')||public.runtime_lifecycle_field_text('prepare_scale_in')
  ||public.runtime_lifecycle_field_int8(v_expected)||public.runtime_lifecycle_field_uuid(v_replica)
  ||public.runtime_lifecycle_field_text(v_instance)||public.runtime_lifecycle_field_text(v_release),
  212,'39c457fd9b3c30904396ce70488a72b21a559649a55f90569c4bc767271ba5b8');
 PERFORM public.runtime_lifecycle_assert_vector(2::smallint,
  public.runtime_lifecycle_result_fields('op-scale-in','prepare_scale_in',v_result,'replica',
   v_replica,'serving','draining',NULL::bigint,NULL::bigint,NULL::bigint,NULL::boolean,NULL::boolean,
   71,v_result,'op-scale-in',true,'online',NULL::text,NULL::bigint),
  256,'775a098df0a51ea8eadc869fe5ff36b8c0e04c811401250b45ace66ad3c52f04');

 PERFORM public.runtime_lifecycle_assert_vector(1::smallint,
  public.runtime_lifecycle_field_text('op-scale-in-finish')||public.runtime_lifecycle_field_text('finish_scale_in')
  ||public.runtime_lifecycle_field_int8(v_expected)||public.runtime_lifecycle_field_uuid(v_replica)
  ||public.runtime_lifecycle_field_text(v_instance)||public.runtime_lifecycle_field_text(v_release)
  ||public.runtime_lifecycle_field_text('leave-fixture')||public.runtime_lifecycle_field_text('proof-fixture'),
  256,'dbf2187ecde3122476847d63589c3ff6ab15c72b10477fbf3b51d13ac1c9943a');
 PERFORM public.runtime_lifecycle_assert_vector(2::smallint,
  public.runtime_lifecycle_result_fields('op-scale-in-finish','finish_scale_in',v_result,'replica',
   v_replica,'serving','left',1,1,0,true,false,71,v_result,'op-scale-in-finish',true,'online',NULL::text,NULL::bigint),
  291,'92cad6bfc24856d63242f115a379671a39724417a8335476564d809bd04eb64a');

 PERFORM public.runtime_lifecycle_assert_vector(1::smallint,
  public.runtime_lifecycle_field_text('op-maint-drain')||public.runtime_lifecycle_field_text('prepare_maintenance_drain')
  ||public.runtime_lifecycle_field_int8(v_expected)||public.runtime_lifecycle_field_uuid(v_replica)
  ||public.runtime_lifecycle_field_text(v_instance)||public.runtime_lifecycle_field_text(v_release),
  224,'66beb1e1fa2549e66550ba9a9a898b225451293d89020eb6aac2640d0177c9ef');
 PERFORM public.runtime_lifecycle_assert_vector(2::smallint,
  public.runtime_lifecycle_result_fields('op-maint-drain','prepare_maintenance_drain',v_result,'replica',
   v_replica,'maintenance','draining',NULL::bigint,NULL::bigint,NULL::bigint,NULL::boolean,NULL::boolean,
   71,v_result,'op-maint-drain',true,'online',NULL::text,NULL::bigint),
  275,'ba98747923b9791adfb3af8a8f057891a952ede94edcbcb660f5ab53b7b6c584');

 PERFORM public.runtime_lifecycle_assert_vector(1::smallint,
  public.runtime_lifecycle_field_text('op-terminate')||public.runtime_lifecycle_field_text('begin_replica_termination')
  ||public.runtime_lifecycle_field_int8(v_expected)||public.runtime_lifecycle_field_text('request-fixture')
  ||public.runtime_lifecycle_field_uuid(v_replica)||public.runtime_lifecycle_field_text(v_instance)
  ||public.runtime_lifecycle_field_text(v_release)||public.runtime_lifecycle_field_text('readiness_failed'),
  265,'9b40c99609c143fa0ddcdd4a43383da128719b027178648a4531a2058b84836b');
 PERFORM public.runtime_lifecycle_assert_vector(2::smallint,
  public.runtime_lifecycle_result_fields('op-terminate','begin_replica_termination',v_result,'replica',
   v_replica,'serving','terminating',NULL::bigint,NULL::bigint,NULL::bigint,NULL::boolean,NULL::boolean,
   71,v_result,'op-terminate',true,'online',NULL::text,NULL::bigint),
  270,'7cd11ad109ec65a6de4075f6d59786fbeb984ad6d36dc63009d58fb65c2ff2eb');

 PERFORM public.runtime_lifecycle_assert_vector(1::smallint,
  public.runtime_lifecycle_field_text('op-wake-serving')||public.runtime_lifecycle_field_text('begin_wake')
  ||public.runtime_lifecycle_field_int8(v_expected)||public.runtime_lifecycle_field_text('serving')
  ||public.runtime_lifecycle_field_int8(100),
  113,'4c2165a2ee539b0c45ba52f4605461547ea966ca12f5ef57f02659ae689a5119');
 PERFORM public.runtime_lifecycle_assert_vector(2::smallint,
  public.runtime_lifecycle_result_fields('op-wake-serving','begin_wake',v_result,'capacity',
   NULL::uuid,NULL::text,NULL::text,1,0,0,false,false,71,v_result,'op-wake-serving',false,'starting','closing',101),
  271,'a97a896208cf389d17f85eabead18af5a388df346b618797cabb1a3760226dd9');

 PERFORM public.runtime_lifecycle_assert_vector(1::smallint,
  public.runtime_lifecycle_field_text('op-wake-maintenance')||public.runtime_lifecycle_field_text('begin_wake')
  ||public.runtime_lifecycle_field_int8(v_expected)||public.runtime_lifecycle_field_text('maintenance')
  ||public.runtime_lifecycle_field_int8(100),
  121,'6117c2414121a2755b65160441f5146a3a3712eaa983cd1bf469a01162c8840a');
 PERFORM public.runtime_lifecycle_assert_vector(2::smallint,
  public.runtime_lifecycle_result_fields('op-wake-maintenance','begin_wake',v_result,'capacity',
   NULL::uuid,NULL::text,NULL::text,1,0,0,false,false,71,v_result,'op-wake-maintenance',false,'starting','closing',101),
  279,'95bfcd2e8956df38433cbe7cf3395e94f564c433e5ab2b121a1818566722ae2d');

 PERFORM public.runtime_lifecycle_assert_vector(1::smallint,
  public.runtime_lifecycle_field_text('op-wake-complete')||public.runtime_lifecycle_field_text('complete_wake')
  ||public.runtime_lifecycle_field_int8(v_expected)||public.runtime_lifecycle_field_int8(101),
  104,'c380d0ac76ef9e143cc8d49f54f214509e37bf1ef42596402d264a3821101abb');
 PERFORM public.runtime_lifecycle_assert_vector(2::smallint,
  public.runtime_lifecycle_result_fields('op-wake-complete','complete_wake',v_result,'capacity',
   NULL::uuid,NULL::text,NULL::text,1,0,0,false,false,72,v_result,'op-wake-complete',true,'online','open',102),
  271,'b7e07a8ca2aaaccfcb247b21b6976c3b77f9ed7c474a5b15eab3f13ccb75973d');
END $$;

CREATE FUNCTION public.runtime_lifecycle_validate_scalars(p_expected bigint,p_operation_id text) RETURNS void
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF p_expected IS NULL OR p_expected<=0 OR p_operation_id IS NULL
  OR octet_length(p_operation_id) NOT BETWEEN 1 AND 128 OR p_operation_id!~'^[ -~]+$' THEN
  RAISE EXCEPTION USING ERRCODE='22023',MESSAGE='lifecycle input is invalid';
 END IF;
END $$;

CREATE FUNCTION public.runtime_lifecycle_validate_evidence(p_value text,p_required boolean) RETURNS void
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF p_value IS NULL THEN
  IF p_required THEN RAISE EXCEPTION USING ERRCODE='22023',MESSAGE='lifecycle input is invalid'; END IF;
  RETURN;
 END IF;
 IF octet_length(p_value) NOT BETWEEN 1 AND 128 OR p_value!~'^[ -~]+$' THEN
  RAISE EXCEPTION USING ERRCODE='22023',MESSAGE='lifecycle input is invalid';
 END IF;
END $$;

CREATE FUNCTION public.runtime_lifecycle_validate_replica_input(p_replica_id uuid,p_instance_id text,p_release_digest text) RETURNS void
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF p_replica_id IS NULL OR p_replica_id='00000000-0000-0000-0000-000000000000'::uuid
  OR p_instance_id IS NULL OR octet_length(p_instance_id)>19 OR p_instance_id!~'^i-[0-9a-f]{17}$'
  OR p_release_digest IS NULL OR p_release_digest!~'^sha256:[0-9a-f]{64}$' THEN
  RAISE EXCEPTION USING ERRCODE='22023',MESSAGE='lifecycle input is invalid';
 END IF;
END $$;

CREATE FUNCTION public.runtime_lifecycle_validate_reason(p_reason text) RETURNS void
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF p_reason IS NULL OR p_reason NOT IN ('startup_failed','readiness_failed','drain_failed') THEN
  RAISE EXCEPTION USING ERRCODE='22023',MESSAGE='lifecycle input is invalid';
 END IF;
END $$;

-- Locks the public singleton, then the capacity singleton. Every lifecycle
-- action takes these two row locks first and in this order.
CREATE FUNCTION public.runtime_lifecycle_lock_singletons() RETURNS public.runtime_capacity
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE capacity_row public.runtime_capacity;
BEGIN
 PERFORM 1 FROM public.public_state WHERE singleton FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='runtime singleton state is missing'; END IF;
 SELECT * INTO capacity_row FROM public.runtime_capacity WHERE singleton FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='runtime capacity is missing'; END IF;
 RETURN capacity_row;
END $$;

CREATE FUNCTION public.runtime_lifecycle_parent_kind(p_operation_id text) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE kind text;
BEGIN
 SELECT o.workflow_kind INTO kind FROM public.runtime_lifecycle_operations o
  WHERE o.operation_id=p_operation_id FOR UPDATE;
 RETURN kind;
END $$;

CREATE FUNCTION public.runtime_lifecycle_create_parent(p_operation_id text,p_workflow text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 INSERT INTO public.runtime_lifecycle_operations(operation_id,workflow_kind) VALUES(p_operation_id,p_workflow);
EXCEPTION WHEN unique_violation THEN
 RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='lifecycle operation identity conflicts';
END $$;

CREATE FUNCTION public.runtime_lifecycle_step(p_operation_id text,p_action text,
 OUT o_found boolean,OUT o_step public.runtime_lifecycle_operation_steps) RETURNS record
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 SELECT * INTO o_step FROM public.runtime_lifecycle_operation_steps s
  WHERE s.operation_id=p_operation_id AND s.action=p_action;
 o_found:=FOUND;
END $$;

CREATE FUNCTION public.runtime_lifecycle_require_first_action(p_operation_id text) RETURNS void
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM public.runtime_lifecycle_operation_steps s WHERE s.operation_id=p_operation_id) THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle action order is invalid';
 END IF;
END $$;

-- A predecessor must exist and its result generation must not exceed the next
-- action's expected generation. An intervening controller action may advance
-- the generation between two steps of one workflow.
CREATE FUNCTION public.runtime_lifecycle_require_predecessor(p_operation_id text,p_action text,p_expected bigint)
RETURNS public.runtime_lifecycle_operation_steps
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE step public.runtime_lifecycle_operation_steps;
BEGIN
 SELECT * INTO step FROM public.runtime_lifecycle_operation_steps s
  WHERE s.operation_id=p_operation_id AND s.action=p_action;
 IF NOT FOUND OR step.result_generation>p_expected THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle predecessor is unavailable';
 END IF;
 RETURN step;
END $$;

-- Replay integrity. A supplied argument-digest difference is AM002 because
-- schema 00015 does not retain every canonical argument, so a changed request
-- and a well-shaped stored argument-digest tamper are indistinguishable. Only
-- independently provable retained-field corruption is AM001.
CREATE FUNCTION public.runtime_lifecycle_assert_step_integrity(
 p_step public.runtime_lifecycle_operation_steps,p_argument_digest bytea) RETURNS void
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE recomputed bytea; stored_kind text;
BEGIN
 IF p_step.argument_digest IS DISTINCT FROM p_argument_digest THEN
  RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='lifecycle operation arguments conflict';
 END IF;
 IF p_step.controller_operation_id IS DISTINCT FROM p_step.operation_id
  OR p_step.controller_generation IS DISTINCT FROM p_step.result_generation
  OR p_step.result_generation IS DISTINCT FROM p_step.expected_generation+1
  OR p_step.admission_enabled IS DISTINCT FROM (p_step.lifecycle_phase='online')
  OR p_step.capacity_generation IS NULL OR p_step.capacity_generation<=0
  OR p_step.controller_generation IS NULL OR p_step.controller_generation<=0
  OR p_step.result_digest IS NULL OR octet_length(p_step.result_digest)<>32
  OR (p_step.result_kind='capacity') IS DISTINCT FROM (p_step.replica_id IS NULL)
  OR (p_step.result_kind='replica') IS DISTINCT FROM (p_step.replica_kind IS NOT NULL)
  OR (p_step.result_kind='replica') IS DISTINCT FROM (p_step.replica_state IS NOT NULL) THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='lifecycle step is corrupt';
 END IF;
 IF p_step.result_kind='replica' THEN
  SELECT r.replica_kind INTO stored_kind FROM public.runtime_replicas r WHERE r.replica_id=p_step.replica_id;
  IF NOT FOUND OR stored_kind IS DISTINCT FROM p_step.replica_kind THEN
   RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='lifecycle step is corrupt';
  END IF;
 END IF;
 recomputed:=public.runtime_lifecycle_result_digest(p_step.operation_id,p_step.action,p_step.result_generation,
  p_step.result_kind,p_step.replica_id,p_step.replica_kind,p_step.replica_state,
  p_step.desired_replicas::bigint,p_step.active_serving_replicas::bigint,p_step.active_maintenance_replicas::bigint,
  p_step.partition_1_enabled,p_step.partition_2_enabled,p_step.capacity_generation,p_step.controller_generation,
  p_step.controller_operation_id,p_step.admission_enabled,p_step.lifecycle_phase,p_step.write_gate,p_step.write_generation);
 IF recomputed IS DISTINCT FROM p_step.result_digest THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='lifecycle step is corrupt';
 END IF;
END $$;

CREATE FUNCTION public.runtime_lifecycle_replay_capacity(
 p_step public.runtime_lifecycle_operation_steps,p_argument_digest bytea) RETURNS public.runtime_capacity_result
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 PERFORM public.runtime_lifecycle_assert_step_integrity(p_step,p_argument_digest);
 IF p_step.result_kind<>'capacity' THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='lifecycle step is corrupt';
 END IF;
 RETURN ROW(p_step.desired_replicas,p_step.active_serving_replicas,p_step.active_maintenance_replicas,
  p_step.partition_1_enabled,p_step.partition_2_enabled,p_step.capacity_generation,p_step.controller_generation,
  p_step.controller_operation_id,p_step.admission_enabled,p_step.lifecycle_phase,
  p_step.write_gate,p_step.write_generation,true)::public.runtime_capacity_result;
END $$;

CREATE FUNCTION public.runtime_lifecycle_replay_replica(
 p_step public.runtime_lifecycle_operation_steps,p_argument_digest bytea) RETURNS public.runtime_replica_result
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 PERFORM public.runtime_lifecycle_assert_step_integrity(p_step,p_argument_digest);
 IF p_step.result_kind<>'replica' THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='lifecycle step is corrupt';
 END IF;
 RETURN ROW(p_step.replica_id,p_step.replica_kind,p_step.replica_state,p_step.desired_replicas,
  p_step.active_serving_replicas,p_step.active_maintenance_replicas,p_step.partition_1_enabled,
  p_step.partition_2_enabled,p_step.capacity_generation,p_step.controller_generation,
  p_step.controller_operation_id,p_step.admission_enabled,p_step.lifecycle_phase,true)::public.runtime_replica_result;
END $$;

-- Locks all 48 partition rows in partition then policy order. Rate operations
-- lock clock then partition and never capacity, so this capacity-to-partition
-- wait has no reverse capacity edge.
CREATE FUNCTION public.runtime_lifecycle_lock_partitions() RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 PERFORM 1 FROM public.shared_rate_partitions s ORDER BY s.partition,s.policy_id FOR UPDATE;
END $$;

-- Requires the exact 24-policy catalog with rows 1 and 2, and uniform enabled,
-- capacity generation and operation evidence within each ordinal. active_keys
-- may differ and is never read here.
CREATE FUNCTION public.runtime_lifecycle_partition_flags(OUT o_partition_1 boolean,OUT o_partition_2 boolean)
RETURNS record LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE total integer; policies integer; catalog_count integer; matched integer;
 count_1 integer; count_2 integer; distinct_1 integer; distinct_2 integer;
BEGIN
 SELECT count(*),count(DISTINCT s.policy_id),
  count(*) FILTER (WHERE s.partition=1),count(*) FILTER (WHERE s.partition=2),
  count(DISTINCT (s.enabled,s.capacity_generation,s.operation_id)) FILTER (WHERE s.partition=1),
  count(DISTINCT (s.enabled,s.capacity_generation,s.operation_id)) FILTER (WHERE s.partition=2),
  bool_and(s.enabled) FILTER (WHERE s.partition=1),bool_and(s.enabled) FILTER (WHERE s.partition=2)
  INTO total,policies,count_1,count_2,distinct_1,distinct_2,o_partition_1,o_partition_2
  FROM public.shared_rate_partitions s;
 SELECT count(*) INTO catalog_count FROM public.shared_rate_policies p;
 SELECT count(*) INTO matched FROM public.shared_rate_policies p
  WHERE EXISTS(SELECT 1 FROM public.shared_rate_partitions s WHERE s.policy_id=p.policy_id AND s.partition=1)
    AND EXISTS(SELECT 1 FROM public.shared_rate_partitions s WHERE s.policy_id=p.policy_id AND s.partition=2);
 IF total<>48 OR policies<>24 OR catalog_count<>24 OR matched<>24 OR count_1<>24 OR count_2<>24
  OR distinct_1<>1 OR distinct_2<>1 OR o_partition_1 IS NULL OR o_partition_2 IS NULL THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate partition state is invalid';
 END IF;
END $$;

CREATE FUNCTION public.runtime_lifecycle_set_partition(p_partition smallint,p_enabled boolean,
 p_generation bigint,p_operation_id text,p_effective timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE changed integer;
BEGIN
 UPDATE public.shared_rate_partitions
  SET enabled=p_enabled,capacity_generation=p_generation,operation_id=p_operation_id,updated_at=p_effective
  WHERE partition=p_partition;
 GET DIAGNOSTICS changed=ROW_COUNT;
 IF changed<>24 THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='shared rate partition state is invalid';
 END IF;
END $$;

CREATE FUNCTION public.runtime_lifecycle_replica_high(p_replica public.runtime_replicas) RETURNS timestamptz
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT GREATEST(p_replica.joined_at,p_replica.join_ready_at,p_replica.activated_at,p_replica.draining_at,
  p_replica.left_at,p_replica.termination_requested_at,p_replica.fenced_at)
$$;

CREATE FUNCTION public.runtime_lifecycle_operation_high(p_operation_id text) RETURNS timestamptz
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT GREATEST(
  (SELECT o.created_at FROM public.runtime_lifecycle_operations o WHERE o.operation_id=p_operation_id),
  (SELECT max(s.recorded_at) FROM public.runtime_lifecycle_operation_steps s WHERE s.operation_id=p_operation_id))
$$;

CREATE FUNCTION public.runtime_lifecycle_evidence_high(p_replica_id uuid) RETURNS timestamptz
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT GREATEST(
  (SELECT i.requested_at FROM public.runtime_termination_intents i WHERE i.replica_id=p_replica_id),
  (SELECT r.recorded_at FROM public.runtime_leave_receipts r WHERE r.replica_id=p_replica_id),
  (SELECT GREATEST(f.requested_at,f.observed_terminated_at,f.recorded_at)
     FROM public.runtime_fencing_proofs f WHERE f.replica_id=p_replica_id))
$$;

CREATE FUNCTION public.runtime_lifecycle_serving_count() RETURNS smallint
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT count(*)::smallint FROM public.runtime_replicas r WHERE r.replica_kind='serving' AND r.state='active'
$$;

CREATE FUNCTION public.runtime_lifecycle_maintenance_count() RETURNS smallint
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT count(*)::smallint FROM public.runtime_replicas r WHERE r.replica_kind='maintenance' AND r.state='active'
$$;

CREATE FUNCTION public.runtime_lifecycle_assert_open_state(p_capacity public.runtime_capacity) RETURNS void
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF p_capacity.lifecycle_phase<>'online' OR NOT p_capacity.admission_enabled THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle state is unavailable';
 END IF;
END $$;

-- Nonlocking visibility predicates: an unfenced terminating incarnation or a
-- visible closing or unresolved public transition. A transition parent is
-- never locked and never waited on.
CREATE FUNCTION public.runtime_lifecycle_assert_no_blockers() RETURNS void
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM public.runtime_replicas r WHERE r.state='terminating')
  OR EXISTS(SELECT 1 FROM public.public_transitions t WHERE t.state IN ('closing','unresolved')) THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle state is unavailable';
 END IF;
END $$;

CREATE FUNCTION public.runtime_lifecycle_assert_generation_room(p_capacity public.runtime_capacity) RETURNS void
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF p_capacity.generation>=9223372036854775807 OR p_capacity.controller_generation>=9223372036854775807 THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle generation is exhausted';
 END IF;
END $$;

CREATE FUNCTION public.runtime_lifecycle_advance_capacity(p_operation_id text,p_effective timestamptz,p_desired smallint)
RETURNS public.runtime_capacity LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE capacity_row public.runtime_capacity;
BEGIN
 UPDATE public.runtime_capacity
  SET generation=generation+1,controller_generation=controller_generation+1,
      controller_operation_id=p_operation_id,updated_by=p_operation_id,updated_at=p_effective,
      desired_replicas=COALESCE(p_desired,desired_replicas)
  WHERE singleton RETURNING * INTO capacity_row;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='runtime capacity is missing'; END IF;
 RETURN capacity_row;
END $$;

CREATE FUNCTION public.runtime_lifecycle_lock_replicas(p_first uuid,p_second uuid) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 PERFORM 1 FROM public.runtime_replicas r WHERE r.replica_id IN (p_first,p_second) ORDER BY r.replica_id FOR UPDATE;
END $$;

CREATE FUNCTION public.runtime_lifecycle_load_target(p_replica_id uuid,p_instance_id text,p_release_digest text)
RETURNS public.runtime_replicas LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE target public.runtime_replicas;
BEGIN
 SELECT * INTO target FROM public.runtime_replicas r WHERE r.replica_id=p_replica_id FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle replica is unavailable'; END IF;
 IF target.instance_id<>p_instance_id OR target.release_digest<>p_release_digest THEN
  RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='lifecycle replica identity conflicts';
 END IF;
 RETURN target;
END $$;

CREATE FUNCTION public.runtime_lifecycle_assert_task_identity(p_replica public.runtime_replicas,
 p_container_instance_arn text,p_caddy_task_arn text,p_go_task_arn text,p_nuxt_task_arn text) RETURNS void
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF p_replica.container_instance_arn<>p_container_instance_arn OR p_replica.caddy_task_arn<>p_caddy_task_arn
  OR p_replica.go_task_arn<>p_go_task_arn OR p_replica.nuxt_task_arn<>p_nuxt_task_arn THEN
  RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='lifecycle replica identity conflicts';
 END IF;
END $$;

CREATE FUNCTION public.runtime_lifecycle_set_replica_state(p_replica_id uuid,p_state text,p_effective timestamptz)
RETURNS public.runtime_replicas LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE target public.runtime_replicas;
BEGIN
 UPDATE public.runtime_replicas
  SET state=p_state,
      activated_at=CASE WHEN p_state='active' THEN p_effective ELSE activated_at END,
      draining_at=CASE WHEN p_state='draining' THEN p_effective ELSE draining_at END,
      termination_requested_at=CASE WHEN p_state='terminating' THEN p_effective ELSE termination_requested_at END
  WHERE replica_id=p_replica_id RETURNING * INTO target;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='lifecycle replica is missing'; END IF;
 RETURN target;
END $$;

CREATE FUNCTION public.runtime_lifecycle_capacity_value(p_capacity public.runtime_capacity,
 p_serving smallint,p_maintenance smallint,p_partition_1 boolean,p_partition_2 boolean,p_replayed boolean)
RETURNS public.runtime_capacity_result LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT ROW(p_capacity.desired_replicas,p_serving,p_maintenance,p_partition_1,p_partition_2,
  p_capacity.generation,p_capacity.controller_generation,p_capacity.controller_operation_id,
  p_capacity.admission_enabled,p_capacity.lifecycle_phase,NULL::text,NULL::bigint,p_replayed)::public.runtime_capacity_result
$$;

CREATE FUNCTION public.runtime_lifecycle_replica_value(p_replica public.runtime_replicas,p_capacity public.runtime_capacity,
 p_desired smallint,p_serving smallint,p_maintenance smallint,p_partition_1 boolean,p_partition_2 boolean,p_replayed boolean)
RETURNS public.runtime_replica_result LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT ROW(p_replica.replica_id,p_replica.replica_kind,p_replica.state,p_desired,p_serving,p_maintenance,
  p_partition_1,p_partition_2,p_capacity.generation,p_capacity.controller_generation,
  p_capacity.controller_operation_id,p_capacity.admission_enabled,p_capacity.lifecycle_phase,
  p_replayed)::public.runtime_replica_result
$$;

CREATE FUNCTION public.runtime_lifecycle_assert_no_intent(p_replica_id uuid) RETURNS void
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM public.runtime_termination_intents i WHERE i.replica_id=p_replica_id) THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle state is unavailable';
 END IF;
END $$;

CREATE FUNCTION public.runtime_lifecycle_lock_intent(p_replica_id uuid) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 PERFORM 1 FROM public.runtime_termination_intents i WHERE i.replica_id=p_replica_id FOR UPDATE;
 IF FOUND THEN RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle state is unavailable'; END IF;
END $$;

CREATE FUNCTION public.runtime_lifecycle_assert_request_identity(p_request_id text,p_replica_id uuid) RETURNS void
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM public.runtime_termination_intents i
   WHERE i.request_id=p_request_id AND i.replica_id<>p_replica_id) THEN
  RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='lifecycle evidence identity conflicts';
 END IF;
END $$;

-- A replacement names an exact fenced serving predecessor that no earlier
-- successful step consumed, and no scale-in workflow may be unfinished.
CREATE FUNCTION public.runtime_lifecycle_assert_replacement(p_replaced uuid) RETURNS void
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE predecessor_state text; predecessor_kind text;
BEGIN
 SELECT r.state,r.replica_kind INTO predecessor_state,predecessor_kind
  FROM public.runtime_replicas r WHERE r.replica_id=p_replaced;
 IF NOT FOUND OR predecessor_state<>'fenced' OR predecessor_kind<>'serving'
  OR EXISTS(SELECT 1 FROM public.runtime_lifecycle_operation_steps s WHERE s.argument_replaced_replica_id=p_replaced)
  OR EXISTS(SELECT 1 FROM public.runtime_lifecycle_operation_steps s
     WHERE s.action='prepare_scale_in'
       AND NOT EXISTS(SELECT 1 FROM public.runtime_lifecycle_operation_steps f
         WHERE f.operation_id=s.operation_id AND f.action='finish_scale_in')) THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle replacement is unavailable';
 END IF;
END $$;

-- Maintenance activation requires no other maintenance incarnation in
-- joining, active, draining or terminating state. Retained left or fenced
-- maintenance history does not block a later campaign.
CREATE FUNCTION public.runtime_lifecycle_assert_single_maintenance(p_replica_id uuid) RETURNS void
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM public.runtime_replicas r
   WHERE r.replica_kind='maintenance' AND r.replica_id<>p_replica_id
     AND r.state IN ('joining','active','draining','terminating')) THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle state is unavailable';
 END IF;
END $$;

CREATE FUNCTION public.runtime_lifecycle_assert_no_other_drain(p_replica_kind text,p_replica_id uuid) RETURNS void
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM public.runtime_replicas r
   WHERE r.state='draining' AND r.replica_id<>p_replica_id
     AND (p_replica_kind IS NULL OR r.replica_kind=p_replica_kind)) THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle state is unavailable';
 END IF;
END $$;

-- Every other serving incarnation must be terminal, except zero or one active
-- survivor. The returned count is the post-action active serving count.
CREATE FUNCTION public.runtime_lifecycle_assert_scale_in_survivors(p_replica_id uuid) RETURNS smallint
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE nonterminal integer; active integer;
BEGIN
 SELECT count(*),count(*) FILTER (WHERE r.state='active') INTO nonterminal,active
  FROM public.runtime_replicas r
  WHERE r.replica_kind='serving' AND r.replica_id<>p_replica_id AND r.state NOT IN ('left','fenced');
 IF nonterminal>1 OR nonterminal<>active THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle state is unavailable';
 END IF;
 RETURN active::smallint;
END $$;

CREATE FUNCTION public.runtime_lifecycle_assert_leave_receipt(p_replica_id uuid,p_instance_id text,
 p_release_digest text,p_operation_id text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE receipt public.runtime_leave_receipts;
BEGIN
 SELECT * INTO receipt FROM public.runtime_leave_receipts r WHERE r.replica_id=p_replica_id FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle evidence is unavailable'; END IF;
 IF receipt.instance_id<>p_instance_id OR receipt.release_digest<>p_release_digest
  OR receipt.operation_id<>p_operation_id THEN
  RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='lifecycle evidence identity conflicts';
 END IF;
END $$;

CREATE FUNCTION public.runtime_lifecycle_assert_intent_evidence(p_replica_id uuid,p_instance_id text,
 p_release_digest text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE intent public.runtime_termination_intents;
BEGIN
 SELECT * INTO intent FROM public.runtime_termination_intents i WHERE i.replica_id=p_replica_id FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle evidence is unavailable'; END IF;
 IF intent.instance_id<>p_instance_id OR intent.release_digest<>p_release_digest THEN
  RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='lifecycle evidence identity conflicts';
 END IF;
END $$;

CREATE FUNCTION public.runtime_lifecycle_assert_fencing_proof(p_replica_id uuid,p_instance_id text,
 p_release_digest text,p_evidence_id text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE proof public.runtime_fencing_proofs;
BEGIN
 SELECT * INTO proof FROM public.runtime_fencing_proofs f WHERE f.replica_id=p_replica_id FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle evidence is unavailable'; END IF;
 IF proof.instance_id<>p_instance_id OR proof.release_digest<>p_release_digest
  OR proof.evidence_id<>p_evidence_id THEN
  RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='lifecycle evidence identity conflicts';
 END IF;
END $$;

CREATE FUNCTION public.runtime_lifecycle_assert_drain_predecessor(p_replica_id uuid) RETURNS void
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM public.runtime_lifecycle_operation_steps s
   WHERE s.action IN ('prepare_scale_in','prepare_maintenance_drain') AND s.replica_id=p_replica_id) THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle predecessor is unavailable';
 END IF;
END $$;

CREATE FUNCTION public.runtime_lifecycle_record_step(
 p_operation_id text,p_action text,p_workflow text,p_expected bigint,p_argument_digest bytea,
 p_replaced uuid,p_result_kind text,p_replica_id uuid,p_replica_kind text,p_replica_state text,
 p_desired bigint,p_serving bigint,p_maintenance bigint,p_partition_1 boolean,p_partition_2 boolean,
 p_capacity public.runtime_capacity,p_effective timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 INSERT INTO public.runtime_lifecycle_operation_steps(
  operation_id,action,workflow_kind,expected_generation,result_generation,argument_digest,result_digest,
  argument_replaced_replica_id,result_kind,replica_id,replica_kind,replica_state,
  desired_replicas,active_serving_replicas,active_maintenance_replicas,partition_1_enabled,partition_2_enabled,
  capacity_generation,controller_generation,controller_operation_id,admission_enabled,lifecycle_phase,
  write_gate,write_generation,recorded_at)
 VALUES(p_operation_id,p_action,p_workflow,p_expected,p_capacity.controller_generation,p_argument_digest,
  public.runtime_lifecycle_result_digest(p_operation_id,p_action,p_capacity.controller_generation,p_result_kind,
   p_replica_id,p_replica_kind,p_replica_state,p_desired,p_serving,p_maintenance,p_partition_1,p_partition_2,
   p_capacity.generation,p_capacity.controller_generation,p_capacity.controller_operation_id,
   p_capacity.admission_enabled,p_capacity.lifecycle_phase,NULL::text,NULL::bigint),
  p_replaced,p_result_kind,p_replica_id,p_replica_kind,p_replica_state,
  p_desired::smallint,p_serving::smallint,p_maintenance::smallint,p_partition_1,p_partition_2,
  p_capacity.generation,p_capacity.controller_generation,p_capacity.controller_operation_id,
  p_capacity.admission_enabled,p_capacity.lifecycle_phase,NULL::text,NULL::bigint,p_effective);
EXCEPTION
 WHEN unique_violation THEN
  RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='lifecycle operation identity conflicts';
 WHEN check_violation OR foreign_key_violation OR not_null_violation THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='lifecycle step is corrupt';
END $$;

-- Prepare scale out. Sets desired two only; it locks every partition row for
-- the consistency check but changes no flag.
CREATE FUNCTION public.runtime_prepare_scale_out(
 expected_controller_generation bigint,operation_id text
) RETURNS public.runtime_capacity_result
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE capacity_row public.runtime_capacity; parent_kind text; located record; flags record;
 argument_digest bytea; serving smallint; maintenance smallint; effective_at timestamptz;
BEGIN
 IF session_user<>'aboutme_lifecycle_command' THEN
  RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='lifecycle role is forbidden';
 END IF;
 PERFORM public.runtime_require_write_entry();
 PERFORM public.runtime_lifecycle_validate_scalars(expected_controller_generation,operation_id);
 argument_digest:=public.runtime_lifecycle_digest(1::smallint,
  public.runtime_lifecycle_field_text(operation_id)
  ||public.runtime_lifecycle_field_text('prepare_scale_out')
  ||public.runtime_lifecycle_field_int8(expected_controller_generation));
 capacity_row:=public.runtime_lifecycle_lock_singletons();
 parent_kind:=public.runtime_lifecycle_parent_kind(operation_id);
 IF parent_kind IS NULL THEN
  PERFORM public.runtime_lifecycle_create_parent(operation_id,'scale_out');
 ELSIF parent_kind<>'scale_out' THEN
  RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='lifecycle operation identity conflicts';
 END IF;
 SELECT * INTO located FROM public.runtime_lifecycle_step(operation_id,'prepare_scale_out');
 IF located.o_found THEN
  RETURN public.runtime_lifecycle_replay_capacity(located.o_step,argument_digest);
 END IF;
 PERFORM public.runtime_lifecycle_require_first_action(operation_id);
 IF expected_controller_generation<>capacity_row.controller_generation THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle generation is stale';
 END IF;
 PERFORM public.runtime_lifecycle_assert_open_state(capacity_row);
 PERFORM public.runtime_lifecycle_assert_no_blockers();
 serving:=public.runtime_lifecycle_serving_count();
 maintenance:=public.runtime_lifecycle_maintenance_count();
 IF capacity_row.desired_replicas<>1 OR serving<>1 THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle state is unavailable';
 END IF;
 PERFORM public.runtime_lifecycle_lock_partitions();
 SELECT * INTO flags FROM public.runtime_lifecycle_partition_flags();
 IF NOT flags.o_partition_1 OR flags.o_partition_2 THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle state is unavailable';
 END IF;
 PERFORM public.runtime_lifecycle_assert_generation_room(capacity_row);
 effective_at:=GREATEST(public.runtime_sample_lifecycle_time(),capacity_row.updated_at,
  public.runtime_lifecycle_operation_high(operation_id));
 capacity_row:=public.runtime_lifecycle_advance_capacity(operation_id,effective_at,2::smallint);
 PERFORM public.runtime_lifecycle_record_step(operation_id,'prepare_scale_out','scale_out',
  expected_controller_generation,argument_digest,NULL::uuid,'capacity',NULL::uuid,NULL::text,NULL::text,
  capacity_row.desired_replicas::bigint,serving::bigint,maintenance::bigint,
  flags.o_partition_1,flags.o_partition_2,capacity_row,effective_at);
 RETURN public.runtime_lifecycle_capacity_value(capacity_row,serving,maintenance,
  flags.o_partition_1,flags.o_partition_2,false);
END $$;

-- Activate one registered, join-ready replica. The operation parent fixes the
-- workflow: with no parent a null replacement creates initial_serving and a
-- non-null replacement creates replacement_serving.
CREATE FUNCTION public.runtime_activate_replica_capacity(
 expected_controller_generation bigint,operation_id text,replica_id uuid,instance_id text,
 container_instance_arn text,caddy_task_arn text,go_task_arn text,nuxt_task_arn text,
 release_digest text,replaced_replica_id uuid
) RETURNS public.runtime_replica_result
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE capacity_row public.runtime_capacity; parent_kind text; workflow text; located record; flags record;
 argument_digest bytea; target public.runtime_replicas; serving smallint; maintenance smallint;
 effective_at timestamptz; enable_partition smallint;
BEGIN
 IF session_user<>'aboutme_lifecycle_command' THEN
  RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='lifecycle role is forbidden';
 END IF;
 PERFORM public.runtime_require_write_entry();
 PERFORM public.runtime_lifecycle_validate_scalars(expected_controller_generation,operation_id);
 PERFORM public.runtime_validate_replica_identity_input(replica_id,instance_id,container_instance_arn,
  caddy_task_arn,go_task_arn,nuxt_task_arn,release_digest);
 IF replaced_replica_id IS NOT NULL
  AND (replaced_replica_id='00000000-0000-0000-0000-000000000000'::uuid OR replaced_replica_id=replica_id) THEN
  RAISE EXCEPTION USING ERRCODE='22023',MESSAGE='lifecycle input is invalid';
 END IF;
 argument_digest:=public.runtime_lifecycle_digest(1::smallint,
  public.runtime_lifecycle_field_text(operation_id)
  ||public.runtime_lifecycle_field_text('activate_replica_capacity')
  ||public.runtime_lifecycle_field_int8(expected_controller_generation)
  ||public.runtime_lifecycle_field_uuid(replica_id)
  ||public.runtime_lifecycle_field_text(instance_id)
  ||public.runtime_lifecycle_field_text(container_instance_arn)
  ||public.runtime_lifecycle_field_text(caddy_task_arn)
  ||public.runtime_lifecycle_field_text(go_task_arn)
  ||public.runtime_lifecycle_field_text(nuxt_task_arn)
  ||public.runtime_lifecycle_field_text(release_digest)
  ||public.runtime_lifecycle_field_uuid(replaced_replica_id));
 capacity_row:=public.runtime_lifecycle_lock_singletons();
 parent_kind:=public.runtime_lifecycle_parent_kind(operation_id);
 IF parent_kind IS NULL THEN
  workflow:=CASE WHEN replaced_replica_id IS NULL THEN 'initial_serving' ELSE 'replacement_serving' END;
  PERFORM public.runtime_lifecycle_create_parent(operation_id,workflow);
 ELSE
  workflow:=parent_kind;
  IF workflow NOT IN ('initial_serving','scale_out','replacement_serving','uat_serving_wake','maintenance_wake')
   OR (workflow='replacement_serving')<>(replaced_replica_id IS NOT NULL) THEN
   RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='lifecycle operation identity conflicts';
  END IF;
 END IF;
 SELECT * INTO located FROM public.runtime_lifecycle_step(operation_id,'activate_replica_capacity');
 IF located.o_found THEN
  RETURN public.runtime_lifecycle_replay_replica(located.o_step,argument_digest);
 END IF;
 IF workflow='scale_out' THEN
  PERFORM public.runtime_lifecycle_require_predecessor(operation_id,'prepare_scale_out',expected_controller_generation);
 ELSIF workflow IN ('uat_serving_wake','maintenance_wake') THEN
  PERFORM public.runtime_lifecycle_require_predecessor(operation_id,'complete_wake',expected_controller_generation);
 ELSE
  PERFORM public.runtime_lifecycle_require_first_action(operation_id);
 END IF;
 IF expected_controller_generation<>capacity_row.controller_generation THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle generation is stale';
 END IF;
 PERFORM public.runtime_lifecycle_assert_open_state(capacity_row);
 PERFORM public.runtime_lifecycle_assert_no_blockers();
 PERFORM public.runtime_lifecycle_lock_replicas(replica_id,replaced_replica_id);
 target:=public.runtime_lifecycle_load_target(replica_id,instance_id,release_digest);
 PERFORM public.runtime_lifecycle_assert_task_identity(target,container_instance_arn,caddy_task_arn,
  go_task_arn,nuxt_task_arn);
 IF target.state<>'joining' OR target.join_ready_at IS NULL THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle replica is unavailable';
 END IF;
 PERFORM public.runtime_assert_replica_registration_children(target);
 PERFORM public.runtime_lifecycle_assert_no_intent(replica_id);
 serving:=public.runtime_lifecycle_serving_count();
 maintenance:=public.runtime_lifecycle_maintenance_count();
 IF workflow='maintenance_wake' THEN
  IF target.replica_kind<>'maintenance' THEN
   RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle replica is unavailable';
  END IF;
  PERFORM public.runtime_lifecycle_assert_single_maintenance(replica_id);
 ELSE
  IF target.replica_kind<>'serving' THEN
   RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle replica is unavailable';
  END IF;
  PERFORM public.runtime_lifecycle_lock_partitions();
  SELECT * INTO flags FROM public.runtime_lifecycle_partition_flags();
  IF workflow IN ('initial_serving','uat_serving_wake') THEN
   IF capacity_row.desired_replicas<>1 OR serving<>0 OR flags.o_partition_1 OR flags.o_partition_2 THEN
    RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle state is unavailable';
   END IF;
   enable_partition:=1::smallint;
  ELSIF workflow='scale_out' THEN
   IF capacity_row.desired_replicas<>2 OR serving<>1 OR NOT flags.o_partition_1 OR flags.o_partition_2 THEN
    RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle state is unavailable';
   END IF;
   enable_partition:=2::smallint;
  ELSE
   IF serving>=capacity_row.desired_replicas THEN
    RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle state is unavailable';
   END IF;
   PERFORM public.runtime_lifecycle_assert_replacement(replaced_replica_id);
  END IF;
 END IF;
 PERFORM public.runtime_lifecycle_assert_generation_room(capacity_row);
 effective_at:=GREATEST(public.runtime_sample_lifecycle_time(),capacity_row.updated_at,
  public.runtime_lifecycle_operation_high(operation_id),public.runtime_lifecycle_replica_high(target));
 target:=public.runtime_lifecycle_set_replica_state(replica_id,'active',effective_at);
 capacity_row:=public.runtime_lifecycle_advance_capacity(operation_id,effective_at,NULL::smallint);
 IF enable_partition IS NOT NULL THEN
  PERFORM public.runtime_lifecycle_set_partition(enable_partition,true,capacity_row.generation,
   operation_id,effective_at);
 END IF;
 PERFORM public.runtime_lifecycle_record_step(operation_id,'activate_replica_capacity',workflow,
  expected_controller_generation,argument_digest,replaced_replica_id,'replica',
  target.replica_id,target.replica_kind,target.state,
  NULL::bigint,NULL::bigint,NULL::bigint,NULL::boolean,NULL::boolean,capacity_row,effective_at);
 RETURN public.runtime_lifecycle_replica_value(target,capacity_row,NULL::smallint,NULL::smallint,
  NULL::smallint,NULL::boolean,NULL::boolean,false);
END $$;

-- Select one exact active serving replica for scale in. Desired and both
-- logical partition flags are unchanged while the target drains.
CREATE FUNCTION public.runtime_prepare_scale_in(
 expected_controller_generation bigint,operation_id text,replica_id uuid,instance_id text,release_digest text
) RETURNS public.runtime_replica_result
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE capacity_row public.runtime_capacity; parent_kind text; located record; flags record;
 argument_digest bytea; target public.runtime_replicas; serving smallint; effective_at timestamptz;
BEGIN
 IF session_user<>'aboutme_lifecycle_command' THEN
  RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='lifecycle role is forbidden';
 END IF;
 PERFORM public.runtime_require_write_entry();
 PERFORM public.runtime_lifecycle_validate_scalars(expected_controller_generation,operation_id);
 PERFORM public.runtime_lifecycle_validate_replica_input(replica_id,instance_id,release_digest);
 argument_digest:=public.runtime_lifecycle_digest(1::smallint,
  public.runtime_lifecycle_field_text(operation_id)
  ||public.runtime_lifecycle_field_text('prepare_scale_in')
  ||public.runtime_lifecycle_field_int8(expected_controller_generation)
  ||public.runtime_lifecycle_field_uuid(replica_id)
  ||public.runtime_lifecycle_field_text(instance_id)
  ||public.runtime_lifecycle_field_text(release_digest));
 capacity_row:=public.runtime_lifecycle_lock_singletons();
 parent_kind:=public.runtime_lifecycle_parent_kind(operation_id);
 IF parent_kind IS NULL THEN
  PERFORM public.runtime_lifecycle_create_parent(operation_id,'scale_in');
 ELSIF parent_kind<>'scale_in' THEN
  RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='lifecycle operation identity conflicts';
 END IF;
 SELECT * INTO located FROM public.runtime_lifecycle_step(operation_id,'prepare_scale_in');
 IF located.o_found THEN
  RETURN public.runtime_lifecycle_replay_replica(located.o_step,argument_digest);
 END IF;
 PERFORM public.runtime_lifecycle_require_first_action(operation_id);
 IF expected_controller_generation<>capacity_row.controller_generation THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle generation is stale';
 END IF;
 PERFORM public.runtime_lifecycle_assert_open_state(capacity_row);
 PERFORM public.runtime_lifecycle_assert_no_blockers();
 serving:=public.runtime_lifecycle_serving_count();
 IF capacity_row.desired_replicas<>2 OR serving<>2 THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle state is unavailable';
 END IF;
 PERFORM public.runtime_lifecycle_lock_replicas(replica_id,NULL::uuid);
 target:=public.runtime_lifecycle_load_target(replica_id,instance_id,release_digest);
 IF target.replica_kind<>'serving' OR target.state<>'active' THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle replica is unavailable';
 END IF;
 PERFORM public.runtime_lifecycle_assert_no_intent(replica_id);
 PERFORM public.runtime_lifecycle_assert_no_other_drain(NULL::text,replica_id);
 PERFORM public.runtime_lifecycle_lock_partitions();
 SELECT * INTO flags FROM public.runtime_lifecycle_partition_flags();
 IF NOT flags.o_partition_1 OR NOT flags.o_partition_2 THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle state is unavailable';
 END IF;
 PERFORM public.runtime_lifecycle_assert_generation_room(capacity_row);
 effective_at:=GREATEST(public.runtime_sample_lifecycle_time(),capacity_row.updated_at,
  public.runtime_lifecycle_operation_high(operation_id),public.runtime_lifecycle_replica_high(target));
 target:=public.runtime_lifecycle_set_replica_state(replica_id,'draining',effective_at);
 capacity_row:=public.runtime_lifecycle_advance_capacity(operation_id,effective_at,NULL::smallint);
 PERFORM public.runtime_lifecycle_record_step(operation_id,'prepare_scale_in','scale_in',
  expected_controller_generation,argument_digest,NULL::uuid,'replica',
  target.replica_id,target.replica_kind,target.state,
  NULL::bigint,NULL::bigint,NULL::bigint,NULL::boolean,NULL::boolean,capacity_row,effective_at);
 RETURN public.runtime_lifecycle_replica_value(target,capacity_row,NULL::smallint,NULL::smallint,
  NULL::smallint,NULL::boolean,NULL::boolean,false);
END $$;

-- Finish scale in. Graceful requires target left with its exact leave receipt;
-- abrupt requires target fenced with its exact intent and a null leave ID.
-- Exact EC2 audit proof is required in both branches.
CREATE FUNCTION public.runtime_finish_scale_in(
 expected_controller_generation bigint,operation_id text,replica_id uuid,instance_id text,
 release_digest text,leave_operation_id text,fencing_evidence_id text
) RETURNS public.runtime_replica_result
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE capacity_row public.runtime_capacity; parent_kind text; located record; flags record;
 prepared public.runtime_lifecycle_operation_steps; argument_digest bytea; target public.runtime_replicas;
 serving smallint; maintenance smallint; effective_at timestamptz;
BEGIN
 IF session_user<>'aboutme_lifecycle_command' THEN
  RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='lifecycle role is forbidden';
 END IF;
 PERFORM public.runtime_require_write_entry();
 PERFORM public.runtime_lifecycle_validate_scalars(expected_controller_generation,operation_id);
 PERFORM public.runtime_lifecycle_validate_replica_input(replica_id,instance_id,release_digest);
 PERFORM public.runtime_lifecycle_validate_evidence(leave_operation_id,false);
 PERFORM public.runtime_lifecycle_validate_evidence(fencing_evidence_id,true);
 argument_digest:=public.runtime_lifecycle_digest(1::smallint,
  public.runtime_lifecycle_field_text(operation_id)
  ||public.runtime_lifecycle_field_text('finish_scale_in')
  ||public.runtime_lifecycle_field_int8(expected_controller_generation)
  ||public.runtime_lifecycle_field_uuid(replica_id)
  ||public.runtime_lifecycle_field_text(instance_id)
  ||public.runtime_lifecycle_field_text(release_digest)
  ||public.runtime_lifecycle_field_text(leave_operation_id)
  ||public.runtime_lifecycle_field_text(fencing_evidence_id));
 capacity_row:=public.runtime_lifecycle_lock_singletons();
 parent_kind:=public.runtime_lifecycle_parent_kind(operation_id);
 IF parent_kind IS NULL THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle predecessor is unavailable';
 ELSIF parent_kind<>'scale_in' THEN
  RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='lifecycle operation identity conflicts';
 END IF;
 SELECT * INTO located FROM public.runtime_lifecycle_step(operation_id,'finish_scale_in');
 IF located.o_found THEN
  RETURN public.runtime_lifecycle_replay_replica(located.o_step,argument_digest);
 END IF;
 prepared:=public.runtime_lifecycle_require_predecessor(operation_id,'prepare_scale_in',
  expected_controller_generation);
 IF prepared.replica_id IS DISTINCT FROM replica_id THEN
  RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='lifecycle replica identity conflicts';
 END IF;
 IF expected_controller_generation<>capacity_row.controller_generation THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle generation is stale';
 END IF;
 IF capacity_row.desired_replicas<>2 THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle state is unavailable';
 END IF;
 PERFORM public.runtime_lifecycle_lock_replicas(replica_id,NULL::uuid);
 target:=public.runtime_lifecycle_load_target(replica_id,instance_id,release_digest);
 IF target.replica_kind<>'serving' THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle replica is unavailable';
 END IF;
 IF leave_operation_id IS NOT NULL THEN
  IF target.state<>'left' THEN
   RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle replica is unavailable';
  END IF;
  PERFORM public.runtime_lifecycle_assert_leave_receipt(replica_id,instance_id,release_digest,leave_operation_id);
 ELSE
  IF target.state<>'fenced' THEN
   RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle replica is unavailable';
  END IF;
  PERFORM public.runtime_lifecycle_assert_intent_evidence(replica_id,instance_id,release_digest);
 END IF;
 PERFORM public.runtime_lifecycle_assert_fencing_proof(replica_id,instance_id,release_digest,fencing_evidence_id);
 serving:=public.runtime_lifecycle_assert_scale_in_survivors(replica_id);
 maintenance:=public.runtime_lifecycle_maintenance_count();
 PERFORM public.runtime_lifecycle_lock_partitions();
 SELECT * INTO flags FROM public.runtime_lifecycle_partition_flags();
 IF NOT flags.o_partition_1 OR NOT flags.o_partition_2 THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle state is unavailable';
 END IF;
 PERFORM public.runtime_lifecycle_assert_generation_room(capacity_row);
 effective_at:=GREATEST(public.runtime_sample_lifecycle_time(),capacity_row.updated_at,
  public.runtime_lifecycle_operation_high(operation_id),public.runtime_lifecycle_replica_high(target),
  public.runtime_lifecycle_evidence_high(replica_id));
 capacity_row:=public.runtime_lifecycle_advance_capacity(operation_id,effective_at,1::smallint);
 PERFORM public.runtime_lifecycle_set_partition(2::smallint,false,capacity_row.generation,
  operation_id,effective_at);
 PERFORM public.runtime_lifecycle_record_step(operation_id,'finish_scale_in','scale_in',
  expected_controller_generation,argument_digest,NULL::uuid,'replica',
  target.replica_id,target.replica_kind,target.state,
  capacity_row.desired_replicas::bigint,serving::bigint,maintenance::bigint,
  flags.o_partition_1,false,capacity_row,effective_at);
 RETURN public.runtime_lifecycle_replica_value(target,capacity_row,capacity_row.desired_replicas,
  serving,maintenance,flags.o_partition_1,false,false);
END $$;

-- Drain the one active maintenance replica. It never inspects or changes
-- desired serving capacity or the rate partitions.
CREATE FUNCTION public.runtime_prepare_maintenance_drain(
 expected_controller_generation bigint,operation_id text,replica_id uuid,instance_id text,release_digest text
) RETURNS public.runtime_replica_result
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE capacity_row public.runtime_capacity; parent_kind text; located record;
 activation public.runtime_lifecycle_operation_steps; argument_digest bytea;
 target public.runtime_replicas; maintenance smallint; effective_at timestamptz;
BEGIN
 IF session_user<>'aboutme_lifecycle_command' THEN
  RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='lifecycle role is forbidden';
 END IF;
 PERFORM public.runtime_require_write_entry();
 PERFORM public.runtime_lifecycle_validate_scalars(expected_controller_generation,operation_id);
 PERFORM public.runtime_lifecycle_validate_replica_input(replica_id,instance_id,release_digest);
 argument_digest:=public.runtime_lifecycle_digest(1::smallint,
  public.runtime_lifecycle_field_text(operation_id)
  ||public.runtime_lifecycle_field_text('prepare_maintenance_drain')
  ||public.runtime_lifecycle_field_int8(expected_controller_generation)
  ||public.runtime_lifecycle_field_uuid(replica_id)
  ||public.runtime_lifecycle_field_text(instance_id)
  ||public.runtime_lifecycle_field_text(release_digest));
 capacity_row:=public.runtime_lifecycle_lock_singletons();
 parent_kind:=public.runtime_lifecycle_parent_kind(operation_id);
 IF parent_kind IS NULL THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle predecessor is unavailable';
 ELSIF parent_kind<>'maintenance_wake' THEN
  RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='lifecycle operation identity conflicts';
 END IF;
 SELECT * INTO located FROM public.runtime_lifecycle_step(operation_id,'prepare_maintenance_drain');
 IF located.o_found THEN
  RETURN public.runtime_lifecycle_replay_replica(located.o_step,argument_digest);
 END IF;
 activation:=public.runtime_lifecycle_require_predecessor(operation_id,'activate_replica_capacity',
  expected_controller_generation);
 IF activation.replica_id IS DISTINCT FROM replica_id THEN
  RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='lifecycle replica identity conflicts';
 END IF;
 IF expected_controller_generation<>capacity_row.controller_generation THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle generation is stale';
 END IF;
 PERFORM public.runtime_lifecycle_assert_open_state(capacity_row);
 PERFORM public.runtime_lifecycle_lock_replicas(replica_id,NULL::uuid);
 target:=public.runtime_lifecycle_load_target(replica_id,instance_id,release_digest);
 IF target.replica_kind<>'maintenance' OR target.state<>'active' THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle replica is unavailable';
 END IF;
 maintenance:=public.runtime_lifecycle_maintenance_count();
 IF maintenance<>1 THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle state is unavailable';
 END IF;
 PERFORM public.runtime_lifecycle_assert_no_other_drain('maintenance',replica_id);
 PERFORM public.runtime_lifecycle_assert_no_intent(replica_id);
 PERFORM public.runtime_lifecycle_assert_generation_room(capacity_row);
 effective_at:=GREATEST(public.runtime_sample_lifecycle_time(),capacity_row.updated_at,
  public.runtime_lifecycle_operation_high(operation_id),public.runtime_lifecycle_replica_high(target));
 target:=public.runtime_lifecycle_set_replica_state(replica_id,'draining',effective_at);
 capacity_row:=public.runtime_lifecycle_advance_capacity(operation_id,effective_at,NULL::smallint);
 PERFORM public.runtime_lifecycle_record_step(operation_id,'prepare_maintenance_drain','maintenance_wake',
  expected_controller_generation,argument_digest,NULL::uuid,'replica',
  target.replica_id,target.replica_kind,target.state,
  NULL::bigint,NULL::bigint,NULL::bigint,NULL::boolean,NULL::boolean,capacity_row,effective_at);
 RETURN public.runtime_lifecycle_replica_value(target,capacity_row,NULL::smallint,NULL::smallint,
  NULL::smallint,NULL::boolean,NULL::boolean,false);
END $$;

-- Record one immutable request-bound termination intent and move the exact
-- incarnation to terminating. SQL proves only the reason and source-state
-- relation; it infers no death and releases no claim.
CREATE FUNCTION public.runtime_begin_replica_termination(
 expected_controller_generation bigint,operation_id text,request_id text,replica_id uuid,
 instance_id text,release_digest text,reason text
) RETURNS public.runtime_replica_result
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE capacity_row public.runtime_capacity; parent_kind text; located record;
 argument_digest bytea; target public.runtime_replicas; effective_at timestamptz;
BEGIN
 IF session_user<>'aboutme_lifecycle_command' THEN
  RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='lifecycle role is forbidden';
 END IF;
 PERFORM public.runtime_require_write_entry();
 PERFORM public.runtime_lifecycle_validate_scalars(expected_controller_generation,operation_id);
 PERFORM public.runtime_lifecycle_validate_evidence(request_id,true);
 PERFORM public.runtime_lifecycle_validate_replica_input(replica_id,instance_id,release_digest);
 PERFORM public.runtime_lifecycle_validate_reason(reason);
 argument_digest:=public.runtime_lifecycle_digest(1::smallint,
  public.runtime_lifecycle_field_text(operation_id)
  ||public.runtime_lifecycle_field_text('begin_replica_termination')
  ||public.runtime_lifecycle_field_int8(expected_controller_generation)
  ||public.runtime_lifecycle_field_text(request_id)
  ||public.runtime_lifecycle_field_uuid(replica_id)
  ||public.runtime_lifecycle_field_text(instance_id)
  ||public.runtime_lifecycle_field_text(release_digest)
  ||public.runtime_lifecycle_field_text(reason));
 capacity_row:=public.runtime_lifecycle_lock_singletons();
 parent_kind:=public.runtime_lifecycle_parent_kind(operation_id);
 IF parent_kind IS NULL THEN
  PERFORM public.runtime_lifecycle_create_parent(operation_id,'replica_termination');
 ELSIF parent_kind<>'replica_termination' THEN
  RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='lifecycle operation identity conflicts';
 END IF;
 SELECT * INTO located FROM public.runtime_lifecycle_step(operation_id,'begin_replica_termination');
 IF located.o_found THEN
  RETURN public.runtime_lifecycle_replay_replica(located.o_step,argument_digest);
 END IF;
 PERFORM public.runtime_lifecycle_require_first_action(operation_id);
 IF expected_controller_generation<>capacity_row.controller_generation THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle generation is stale';
 END IF;
 PERFORM public.runtime_lifecycle_lock_replicas(replica_id,NULL::uuid);
 target:=public.runtime_lifecycle_load_target(replica_id,instance_id,release_digest);
 PERFORM public.runtime_lifecycle_assert_request_identity(request_id,replica_id);
 PERFORM public.runtime_lifecycle_lock_intent(replica_id);
 IF NOT ((reason='startup_failed' AND target.state='joining')
      OR (reason='readiness_failed' AND target.state='active')
      OR (reason='drain_failed' AND target.state='draining')) THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='lifecycle replica is unavailable';
 END IF;
 IF reason='drain_failed' THEN
  PERFORM public.runtime_lifecycle_assert_drain_predecessor(replica_id);
 END IF;
 PERFORM public.runtime_lifecycle_assert_generation_room(capacity_row);
 effective_at:=GREATEST(public.runtime_sample_lifecycle_time(),capacity_row.updated_at,
  public.runtime_lifecycle_operation_high(operation_id),public.runtime_lifecycle_replica_high(target));
 PERFORM public.runtime_lifecycle_record_intent(replica_id,instance_id,release_digest,request_id,
  reason,effective_at);
 target:=public.runtime_lifecycle_set_replica_state(replica_id,'terminating',effective_at);
 capacity_row:=public.runtime_lifecycle_advance_capacity(operation_id,effective_at,NULL::smallint);
 PERFORM public.runtime_lifecycle_record_step(operation_id,'begin_replica_termination','replica_termination',
  expected_controller_generation,argument_digest,NULL::uuid,'replica',
  target.replica_id,target.replica_kind,target.state,
  NULL::bigint,NULL::bigint,NULL::bigint,NULL::boolean,NULL::boolean,capacity_row,effective_at);
 RETURN public.runtime_lifecycle_replica_value(target,capacity_row,NULL::smallint,NULL::smallint,
  NULL::smallint,NULL::boolean,NULL::boolean,false);
END $$;

-- The intent is inserted before the incarnation moves to terminating, so the
-- accepted schema trigger validates the reason against the source state.
CREATE FUNCTION public.runtime_lifecycle_record_intent(p_replica_id uuid,p_instance_id text,
 p_release_digest text,p_request_id text,p_reason text,p_effective timestamptz) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 INSERT INTO public.runtime_termination_intents(replica_id,instance_id,release_digest,request_id,reason,requested_at)
 VALUES(p_replica_id,p_instance_id,p_release_digest,p_request_id,p_reason,p_effective);
EXCEPTION
 WHEN unique_violation OR foreign_key_violation THEN
  RAISE EXCEPTION USING ERRCODE='AM002',MESSAGE='lifecycle evidence identity conflicts';
 WHEN check_violation OR not_null_violation THEN
  RAISE EXCEPTION USING ERRCODE='AM001',MESSAGE='lifecycle evidence is corrupt';
END $$;

REVOKE ALL ON FUNCTION
 public.runtime_sample_lifecycle_time(),
 public.runtime_lifecycle_digest_field(smallint,bytea),
 public.runtime_lifecycle_digest(smallint,bytea),
 public.runtime_lifecycle_field_uuid(uuid),
 public.runtime_lifecycle_field_int8(bigint),
 public.runtime_lifecycle_field_text(text),
 public.runtime_lifecycle_field_bool(boolean),
 public.runtime_lifecycle_result_fields(text,text,bigint,text,uuid,text,text,bigint,bigint,bigint,boolean,boolean,bigint,bigint,text,boolean,text,text,bigint),
 public.runtime_lifecycle_result_digest(text,text,bigint,text,uuid,text,text,bigint,bigint,bigint,boolean,boolean,bigint,bigint,text,boolean,text,text,bigint),
 public.runtime_lifecycle_assert_vector(smallint,bytea,integer,text),
 public.runtime_lifecycle_assert_digest_vectors(),
 public.runtime_lifecycle_validate_scalars(bigint,text),
 public.runtime_lifecycle_validate_evidence(text,boolean),
 public.runtime_lifecycle_validate_replica_input(uuid,text,text),
 public.runtime_lifecycle_validate_reason(text),
 public.runtime_lifecycle_lock_singletons(),
 public.runtime_lifecycle_parent_kind(text),
 public.runtime_lifecycle_create_parent(text,text),
 public.runtime_lifecycle_step(text,text),
 public.runtime_lifecycle_require_first_action(text),
 public.runtime_lifecycle_require_predecessor(text,text,bigint),
 public.runtime_lifecycle_assert_step_integrity(public.runtime_lifecycle_operation_steps,bytea),
 public.runtime_lifecycle_replay_capacity(public.runtime_lifecycle_operation_steps,bytea),
 public.runtime_lifecycle_replay_replica(public.runtime_lifecycle_operation_steps,bytea),
 public.runtime_lifecycle_lock_partitions(),
 public.runtime_lifecycle_partition_flags(),
 public.runtime_lifecycle_set_partition(smallint,boolean,bigint,text,timestamptz),
 public.runtime_lifecycle_replica_high(public.runtime_replicas),
 public.runtime_lifecycle_operation_high(text),
 public.runtime_lifecycle_evidence_high(uuid),
 public.runtime_lifecycle_serving_count(),
 public.runtime_lifecycle_maintenance_count(),
 public.runtime_lifecycle_assert_open_state(public.runtime_capacity),
 public.runtime_lifecycle_assert_no_blockers(),
 public.runtime_lifecycle_assert_generation_room(public.runtime_capacity),
 public.runtime_lifecycle_advance_capacity(text,timestamptz,smallint),
 public.runtime_lifecycle_lock_replicas(uuid,uuid),
 public.runtime_lifecycle_load_target(uuid,text,text),
 public.runtime_lifecycle_assert_task_identity(public.runtime_replicas,text,text,text,text),
 public.runtime_lifecycle_set_replica_state(uuid,text,timestamptz),
 public.runtime_lifecycle_capacity_value(public.runtime_capacity,smallint,smallint,boolean,boolean,boolean),
 public.runtime_lifecycle_replica_value(public.runtime_replicas,public.runtime_capacity,smallint,smallint,smallint,boolean,boolean,boolean),
 public.runtime_lifecycle_assert_no_intent(uuid),
 public.runtime_lifecycle_lock_intent(uuid),
 public.runtime_lifecycle_assert_request_identity(text,uuid),
 public.runtime_lifecycle_assert_replacement(uuid),
 public.runtime_lifecycle_assert_single_maintenance(uuid),
 public.runtime_lifecycle_assert_no_other_drain(text,uuid),
 public.runtime_lifecycle_assert_scale_in_survivors(uuid),
 public.runtime_lifecycle_assert_leave_receipt(uuid,text,text,text),
 public.runtime_lifecycle_assert_intent_evidence(uuid,text,text),
 public.runtime_lifecycle_assert_fencing_proof(uuid,text,text,text),
 public.runtime_lifecycle_assert_drain_predecessor(uuid),
 public.runtime_lifecycle_record_step(text,text,text,bigint,bytea,uuid,text,uuid,text,text,bigint,bigint,bigint,boolean,boolean,public.runtime_capacity,timestamptz),
 public.runtime_lifecycle_record_intent(uuid,text,text,text,text,timestamptz)
 FROM PUBLIC,aboutme_app,aboutme_maintenance,aboutme_lifecycle_command,aboutme_fencing_proof,aboutme_restore_verify,aboutme_migrator;

REVOKE ALL ON FUNCTION
 public.runtime_prepare_scale_out(bigint,text),
 public.runtime_activate_replica_capacity(bigint,text,uuid,text,text,text,text,text,text,uuid),
 public.runtime_prepare_scale_in(bigint,text,uuid,text,text),
 public.runtime_finish_scale_in(bigint,text,uuid,text,text,text,text),
 public.runtime_prepare_maintenance_drain(bigint,text,uuid,text,text),
 public.runtime_begin_replica_termination(bigint,text,text,uuid,text,text,text)
 FROM PUBLIC,aboutme_app,aboutme_maintenance,aboutme_lifecycle_command,aboutme_fencing_proof,aboutme_restore_verify,aboutme_migrator;

GRANT USAGE ON TYPE public.runtime_replica_result,public.runtime_capacity_result TO aboutme_lifecycle_command;
GRANT EXECUTE ON FUNCTION
 public.runtime_prepare_scale_out(bigint,text),
 public.runtime_activate_replica_capacity(bigint,text,uuid,text,text,text,text,text,text,uuid),
 public.runtime_prepare_scale_in(bigint,text,uuid,text,text),
 public.runtime_finish_scale_in(bigint,text,uuid,text,text,text,text),
 public.runtime_prepare_maintenance_drain(bigint,text,uuid,text,text),
 public.runtime_begin_replica_termination(bigint,text,text,uuid,text,text,text)
 TO aboutme_lifecycle_command;

SELECT public.runtime_lifecycle_assert_digest_vectors();

RESET ROLE;
REVOKE CREATE ON SCHEMA public FROM aboutme_runtime_owner;
SELECT public.runtime_finish_write();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 1;
-- +goose StatementEnd
