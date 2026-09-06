-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
  IF NOT pg_catalog.has_database_privilege('aboutme_runtime_owner',current_database(),'TEMPORARY') THEN
    RAISE EXCEPTION USING ERRCODE='42501', MESSAGE='runtime owner requires database TEMPORARY privilege';
  END IF;
END;
$$;

GRANT CREATE ON SCHEMA public TO aboutme_runtime_owner;

CREATE TABLE public.runtime_write_state (
  singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
  generation bigint NOT NULL CHECK (generation > 0),
  write_gate text NOT NULL CHECK (write_gate IN ('open','closing','closed')),
  last_write_at timestamptz NOT NULL,
  last_accepted_writer_at timestamptz NOT NULL,
  last_writer_kind text NOT NULL CHECK (last_writer_kind IN ('bootstrap','app','maintenance','lifecycle','proof','migrator')),
  last_writer_operation_id text NOT NULL CHECK (octet_length(last_writer_operation_id) BETWEEN 1 AND 128),
  updated_at timestamptz NOT NULL,
  migrator_enforcement_version smallint NOT NULL CHECK (migrator_enforcement_version IN (0,1)),
  migration_history_owner text NOT NULL CHECK (octet_length(migration_history_owner) BETWEEN 1 AND 63)
);

INSERT INTO public.runtime_write_state (
  singleton, generation, write_gate, last_write_at, last_accepted_writer_at,
  last_writer_kind, last_writer_operation_id, updated_at,
  migrator_enforcement_version, migration_history_owner
)
SELECT true, 1, 'open', transaction_timestamp(), transaction_timestamp(),
       'bootstrap', 'migration-00013', transaction_timestamp(), 0, owner_role.rolname
FROM pg_catalog.pg_class AS history
JOIN pg_catalog.pg_roles AS owner_role ON owner_role.oid = history.relowner
WHERE history.oid = 'public.goose_db_version'::regclass;

REVOKE ALL ON TABLE public.runtime_write_state FROM PUBLIC;
ALTER TABLE public.runtime_write_state OWNER TO aboutme_runtime_owner;

CREATE FUNCTION public.runtime_assert_write_finished()
RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_temp.runtime_write_entry_v1
    WHERE transaction_id = NEW.transaction_id AND backend_pid = pg_backend_pid()
      AND session_role_oid = (session_user::regrole)::oid AND finished
  ) THEN
    RAISE EXCEPTION USING ERRCODE='55000', MESSAGE='write transaction was not finished';
  END IF;
  RETURN NULL;
END;
$$;

CREATE FUNCTION public.runtime_validate_write_marker()
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE
  marker_oid oid;
  marker_owner oid;
  valid_columns integer;
  total_columns integer;
BEGIN
  SELECT c.oid, c.relowner INTO marker_oid, marker_owner
  FROM pg_catalog.pg_class c
  WHERE c.relnamespace = pg_my_temp_schema() AND c.relname='runtime_write_entry_v1';
  IF marker_oid IS NULL THEN
    RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='write marker is missing';
  END IF;
  IF marker_owner <> 'aboutme_runtime_owner'::regrole::oid OR
     NOT EXISTS (SELECT 1 FROM pg_catalog.pg_class WHERE oid=marker_oid AND relpersistence='t' AND relkind='r') THEN
    RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='write marker identity mismatch';
  END IF;
  SELECT count(*), count(*) FILTER (WHERE
    (attnum=1 AND attname='transaction_id' AND atttypid='xid8'::regtype AND attnotnull) OR
    (attnum=2 AND attname='backend_pid' AND atttypid='integer'::regtype AND attnotnull) OR
    (attnum=3 AND attname='session_role_oid' AND atttypid='oid'::regtype AND attnotnull) OR
    (attnum=4 AND attname='writer_kind' AND atttypid='text'::regtype AND attnotnull) OR
    (attnum=5 AND attname='entry_generation' AND atttypid='bigint'::regtype AND attnotnull) OR
    (attnum=6 AND attname='dirty' AND atttypid='boolean'::regtype AND attnotnull) OR
    (attnum=7 AND attname='accepted_write' AND atttypid='boolean'::regtype AND attnotnull) OR
    (attnum=8 AND attname='operation_id' AND atttypid='text'::regtype AND NOT attnotnull) OR
    (attnum=9 AND attname='finished' AND atttypid='boolean'::regtype AND attnotnull))
    INTO total_columns, valid_columns
  FROM pg_catalog.pg_attribute WHERE attrelid=marker_oid AND attnum>0 AND NOT attisdropped;
  IF total_columns<>9 OR valid_columns<>9 OR
     (SELECT pg_catalog.array_agg(pg_catalog.pg_get_expr(d.adbin,d.adrelid) ORDER BY a.attnum)
        FROM pg_catalog.pg_attribute a LEFT JOIN pg_catalog.pg_attrdef d
          ON d.adrelid=a.attrelid AND d.adnum=a.attnum
       WHERE a.attrelid=marker_oid AND a.attnum>0 AND NOT a.attisdropped)
       IS DISTINCT FROM ARRAY[NULL,NULL,NULL,NULL,NULL,'false'::text,'false'::text,NULL,'false'::text] OR
     (SELECT pg_catalog.array_agg(pg_catalog.pg_get_constraintdef(c.oid,true) ORDER BY c.contype,c.conname)
        FROM pg_catalog.pg_constraint c WHERE c.conrelid=marker_oid AND c.contype IN ('c','p'))
       IS DISTINCT FROM ARRAY[
         'CHECK (entry_generation > 0)',
         'CHECK (operation_id IS NULL OR octet_length(operation_id) >= 1 AND octet_length(operation_id) <= 128)',
         'CHECK (writer_kind = ANY (ARRAY[''app''::text, ''maintenance''::text, ''lifecycle''::text, ''proof''::text, ''migrator''::text]))',
         'PRIMARY KEY (transaction_id)'] OR
     (SELECT count(*) FROM pg_catalog.pg_trigger t
       JOIN pg_catalog.pg_constraint c ON c.oid=t.tgconstraint
       WHERE t.tgrelid=marker_oid AND NOT t.tgisinternal
         AND t.tgname='runtime_write_entry_finished_assert' AND t.tgenabled='O'
         AND t.tgfoid='public.runtime_assert_write_finished()'::regprocedure
         AND t.tgtype=5 AND t.tgnargs=0 AND c.contype='t'
         AND c.condeferrable AND c.condeferred)<>1 OR
     (SELECT count(*) FROM pg_catalog.pg_trigger WHERE tgrelid=marker_oid AND NOT tgisinternal)<>1 OR
     (SELECT count(*) FROM pg_catalog.pg_index i JOIN pg_catalog.pg_class x ON x.oid=i.indexrelid
       WHERE i.indrelid=marker_oid AND i.indisprimary AND i.indisunique AND i.indisvalid AND i.indisready
         AND i.indnkeyatts=1 AND i.indnatts=1 AND i.indkey::text='1' AND i.indoption::text='0'
         AND i.indcollation::text='0' AND i.indexprs IS NULL AND i.indpred IS NULL
         AND (SELECT opcname FROM pg_catalog.pg_opclass WHERE oid=i.indclass[0])='xid8_ops'
         AND x.relkind='i' AND x.relam=(SELECT oid FROM pg_catalog.pg_am WHERE amname='btree'))<>1 OR
     (SELECT count(*) FROM pg_catalog.pg_index WHERE indrelid=marker_oid)<>1 OR
     EXISTS (SELECT 1 FROM pg_catalog.aclexplode(COALESCE(
       (SELECT relacl FROM pg_catalog.pg_class WHERE oid=marker_oid),
       pg_catalog.acldefault('r',marker_owner))) acl WHERE acl.grantee<>marker_owner) OR
     EXISTS (SELECT 1 FROM pg_catalog.pg_attribute WHERE attrelid=marker_oid AND attnum>0 AND NOT attisdropped AND attacl IS NOT NULL) THEN
    RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='write marker shape mismatch';
  END IF;
END;
$$;

CREATE FUNCTION public.runtime_create_write_marker()
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_catalog.pg_class WHERE relnamespace=pg_my_temp_schema() AND relname='runtime_write_entry_v1') THEN
    PERFORM public.runtime_validate_write_marker();
    RETURN;
  END IF;
  IF EXISTS (SELECT 1 FROM pg_catalog.pg_type WHERE typnamespace=pg_my_temp_schema() AND typname='runtime_write_entry_v1') OR
     EXISTS (SELECT 1 FROM pg_catalog.pg_class WHERE relnamespace=pg_my_temp_schema() AND relname='runtime_write_entry_v1_pkey') THEN
    RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='write marker catalog name collision';
  END IF;
  CREATE TEMP TABLE pg_temp.runtime_write_entry_v1 (
    transaction_id xid8 PRIMARY KEY,
    backend_pid integer NOT NULL,
    session_role_oid oid NOT NULL,
    writer_kind text NOT NULL CHECK (writer_kind IN ('app','maintenance','lifecycle','proof','migrator')),
    entry_generation bigint NOT NULL CHECK (entry_generation > 0),
    dirty boolean NOT NULL DEFAULT false,
    accepted_write boolean NOT NULL DEFAULT false,
    operation_id text NULL CHECK (operation_id IS NULL OR octet_length(operation_id) BETWEEN 1 AND 128),
    finished boolean NOT NULL DEFAULT false
  ) ON COMMIT DELETE ROWS;
  CREATE CONSTRAINT TRIGGER runtime_write_entry_finished_assert
    AFTER INSERT ON pg_temp.runtime_write_entry_v1 DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION public.runtime_assert_write_finished();
  PERFORM public.runtime_validate_write_marker();
END;
$$;

CREATE FUNCTION public.runtime_require_write_entry()
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE kind text; gate text;
BEGIN
  PERFORM public.runtime_validate_write_marker();
  IF NOT EXISTS (
    SELECT 1 FROM pg_catalog.pg_locks WHERE locktype='advisory' AND pid=pg_backend_pid()
      AND database=(SELECT oid FROM pg_catalog.pg_database WHERE datname=current_database())
      AND classid=867785103 AND objid=2177536586 AND objsubid=1
      AND mode='ShareLock' AND granted
  ) THEN RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='runtime barrier lock missing'; END IF;
  SELECT writer_kind INTO kind FROM pg_temp.runtime_write_entry_v1
  WHERE transaction_id=pg_current_xact_id() AND backend_pid=pg_backend_pid()
    AND session_role_oid=(session_user::regrole)::oid AND NOT finished;
  IF kind IS NULL OR (SELECT count(*) FROM pg_temp.runtime_write_entry_v1)<>1 THEN
    RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='write marker mismatch';
  END IF;
  SELECT write_gate INTO gate FROM public.runtime_write_state WHERE singleton;
  IF gate IS DISTINCT FROM 'open' THEN
    RAISE EXCEPTION USING ERRCODE='55000', MESSAGE='runtime write gate unavailable';
  END IF;
  RETURN kind;
END;
$$;

CREATE FUNCTION public.runtime_enter_write()
RETURNS TABLE(generation bigint, writer_kind text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE expected_kind text; current_generation bigint;
BEGIN
  expected_kind := CASE session_user
    WHEN 'aboutme_app' THEN 'app' WHEN 'aboutme_maintenance' THEN 'maintenance'
    WHEN 'aboutme_lifecycle_command' THEN 'lifecycle' WHEN 'aboutme_fencing_proof' THEN 'proof'
    ELSE NULL END;
  IF expected_kind IS NULL THEN RAISE EXCEPTION USING ERRCODE='42501', MESSAGE='role cannot enter runtime writes'; END IF;
  PERFORM pg_catalog.pg_advisory_xact_lock_shared(3727108639518528074);
  IF EXISTS (SELECT 1 FROM pg_catalog.pg_locks WHERE pid=pg_backend_pid()
    AND database=(SELECT oid FROM pg_catalog.pg_database WHERE datname=current_database())
    AND locktype='advisory' AND classid=295306100 AND objid=pg_backend_pid()
    AND objsubid=2 AND mode='ExclusiveLock' AND granted) THEN
    RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='runtime finish guard already held';
  END IF;
  SELECT s.generation INTO current_generation FROM public.runtime_write_state s WHERE s.singleton AND s.write_gate='open';
  IF current_generation IS NULL THEN RAISE EXCEPTION USING ERRCODE='55000', MESSAGE='runtime write gate unavailable'; END IF;
  PERFORM public.runtime_create_write_marker();
  IF EXISTS (SELECT 1 FROM pg_temp.runtime_write_entry_v1 AS marker WHERE marker.finished) THEN
    RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='write marker already finished';
  END IF;
  IF EXISTS (SELECT 1 FROM pg_temp.runtime_write_entry_v1 AS marker WHERE marker.transaction_id<>pg_current_xact_id() OR marker.backend_pid<>pg_backend_pid() OR marker.session_role_oid<>(session_user::regrole)::oid OR marker.writer_kind<>expected_kind) THEN
    RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='write marker mismatch';
  END IF;
  SELECT marker.entry_generation INTO generation FROM pg_temp.runtime_write_entry_v1 AS marker
   WHERE marker.transaction_id=pg_current_xact_id() AND marker.backend_pid=pg_backend_pid()
     AND marker.session_role_oid=(session_user::regrole)::oid AND marker.writer_kind=expected_kind;
  IF FOUND THEN writer_kind := expected_kind; RETURN NEXT; RETURN; END IF;
  generation := current_generation;
  INSERT INTO pg_temp.runtime_write_entry_v1(transaction_id,backend_pid,session_role_oid,writer_kind,entry_generation)
  VALUES(pg_current_xact_id(),pg_backend_pid(),(session_user::regrole)::oid,expected_kind,generation)
  ON CONFLICT (transaction_id) DO NOTHING;
  writer_kind := expected_kind;
  RETURN NEXT;
END;
$$;

CREATE FUNCTION public.runtime_assert_write_entry()
RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
BEGIN
  PERFORM public.runtime_require_write_entry();
  UPDATE pg_temp.runtime_write_entry_v1 SET dirty=true WHERE transaction_id=pg_current_xact_id();
  RETURN NULL;
END;
$$;

CREATE FUNCTION public.runtime_assert_business_write()
RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE kind text; operation text;
BEGIN
  kind := public.runtime_require_write_entry();
  IF TG_NARGS<>1 OR octet_length(TG_ARGV[0]) NOT BETWEEN 1 AND 100 THEN
    RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='business trigger classification mismatch';
  END IF;
  operation := TG_ARGV[0] || ':' || lower(TG_OP);
  UPDATE pg_temp.runtime_write_entry_v1 SET dirty=true,
    accepted_write=accepted_write OR kind='app',
    operation_id=CASE WHEN kind='app' THEN operation ELSE operation_id END
  WHERE transaction_id=pg_current_xact_id();
  RETURN NULL;
END;
$$;

CREATE FUNCTION public.runtime_finish_write()
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE marker_dirty boolean; marker_accepted boolean; marker_finished boolean; marker_kind text; marker_operation text; now_at timestamptz; expected_kind text; guard_held boolean; guard_acquired boolean;
BEGIN
  PERFORM public.runtime_validate_write_marker();
  expected_kind := CASE session_user WHEN 'aboutme_app' THEN 'app' WHEN 'aboutme_maintenance' THEN 'maintenance' WHEN 'aboutme_lifecycle_command' THEN 'lifecycle' WHEN 'aboutme_fencing_proof' THEN 'proof' WHEN 'aboutme_migrator' THEN 'migrator' ELSE NULL END;
  SELECT dirty, accepted_write, finished, writer_kind, operation_id
    INTO marker_dirty, marker_accepted, marker_finished, marker_kind, marker_operation
    FROM pg_temp.runtime_write_entry_v1
   WHERE transaction_id=pg_current_xact_id() AND backend_pid=pg_backend_pid()
     AND session_role_oid=(session_user::regrole)::oid;
  IF NOT FOUND OR (SELECT count(*) FROM pg_temp.runtime_write_entry_v1)<>1 OR marker_kind IS DISTINCT FROM expected_kind THEN RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='write marker missing or mismatched'; END IF;
  SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_locks WHERE pid=pg_backend_pid()
    AND database=(SELECT oid FROM pg_catalog.pg_database WHERE datname=current_database())
    AND locktype='advisory' AND classid=295306100 AND objid=pg_backend_pid()
    AND objsubid=2 AND mode='ExclusiveLock' AND granted) INTO guard_held;
  IF marker_finished THEN
    IF NOT guard_held THEN RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='runtime finish guard missing'; END IF;
    RETURN;
  END IF;
  IF guard_held THEN RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='runtime finish guard held before finish'; END IF;
  PERFORM public.runtime_require_write_entry();
  SELECT pg_catalog.pg_try_advisory_xact_lock(295306100,pg_backend_pid()) INTO guard_acquired;
  IF NOT guard_acquired THEN RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='runtime finish guard unavailable'; END IF;
  IF marker_dirty THEN
    PERFORM 1 FROM public.runtime_write_state WHERE singleton FOR UPDATE;
    now_at := clock_timestamp();
    UPDATE public.runtime_write_state SET generation=generation+1, last_write_at=GREATEST(last_write_at,now_at),
      last_accepted_writer_at=CASE WHEN marker_accepted THEN GREATEST(last_accepted_writer_at,now_at) ELSE last_accepted_writer_at END,
      last_writer_kind=marker_kind,
      last_writer_operation_id=COALESCE(marker_operation,'runtime-write'), updated_at=GREATEST(updated_at,now_at)
    WHERE singleton;
  END IF;
  UPDATE pg_temp.runtime_write_entry_v1 SET finished=true WHERE transaction_id=pg_current_xact_id();
END;
$$;

CREATE FUNCTION public.runtime_validate_migrator_marker()
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE marker_oid oid; marker_owner oid; valid_columns integer; total_columns integer;
BEGIN
  SELECT c.oid,c.relowner INTO marker_oid,marker_owner FROM pg_catalog.pg_class c
   WHERE c.relnamespace=pg_my_temp_schema() AND c.relname='runtime_migrator_session_v1';
  IF marker_oid IS NULL OR marker_owner<>'aboutme_runtime_owner'::regrole::oid OR
     NOT EXISTS (SELECT 1 FROM pg_catalog.pg_class WHERE oid=marker_oid AND relpersistence='t' AND relkind='r')
    THEN RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='migrator marker missing or untrusted'; END IF;
  SELECT count(*) FILTER (WHERE
    (attnum=1 AND attname='singleton' AND atttypid='boolean'::regtype AND attnotnull) OR
    (attnum=2 AND attname='backend_pid' AND atttypid='integer'::regtype AND attnotnull) OR
    (attnum=3 AND attname='session_role_oid' AND atttypid='oid'::regtype AND attnotnull) OR
    (attnum=4 AND attname='entry_generation' AND atttypid='bigint'::regtype AND attnotnull) OR
    (attnum=5 AND attname='entered_at' AND atttypid='timestamptz'::regtype AND attnotnull)), count(*)
    INTO valid_columns, total_columns FROM pg_catalog.pg_attribute WHERE attrelid=marker_oid AND attnum>0 AND NOT attisdropped;
  IF valid_columns<>5 OR total_columns<>5 OR
     EXISTS (SELECT 1 FROM pg_catalog.pg_attrdef WHERE adrelid=marker_oid) OR
     (SELECT pg_catalog.array_agg(pg_catalog.pg_get_constraintdef(c.oid,true) ORDER BY c.contype,c.conname)
        FROM pg_catalog.pg_constraint c WHERE c.conrelid=marker_oid AND c.contype IN ('c','p'))
       IS DISTINCT FROM ARRAY['CHECK (entry_generation > 0)','CHECK (singleton)','PRIMARY KEY (singleton)'] OR
     EXISTS (SELECT 1 FROM pg_catalog.pg_trigger WHERE tgrelid=marker_oid AND NOT tgisinternal) OR
     (SELECT count(*) FROM pg_catalog.pg_index i JOIN pg_catalog.pg_class x ON x.oid=i.indexrelid
       WHERE i.indrelid=marker_oid AND i.indisprimary AND i.indisunique AND i.indisvalid AND i.indisready
         AND i.indnkeyatts=1 AND i.indnatts=1 AND i.indkey::text='1' AND i.indoption::text='0'
         AND i.indcollation::text='0' AND i.indexprs IS NULL AND i.indpred IS NULL
         AND (SELECT opcname FROM pg_catalog.pg_opclass WHERE oid=i.indclass[0])='bool_ops'
         AND x.relkind='i' AND x.relam=(SELECT oid FROM pg_catalog.pg_am WHERE amname='btree'))<>1 OR
     (SELECT count(*) FROM pg_catalog.pg_index WHERE indrelid=marker_oid)<>1 OR
     EXISTS (SELECT 1 FROM pg_catalog.aclexplode(COALESCE(
       (SELECT relacl FROM pg_catalog.pg_class WHERE oid=marker_oid),
       pg_catalog.acldefault('r',marker_owner))) acl WHERE acl.grantee<>marker_owner) OR
     EXISTS (SELECT 1 FROM pg_catalog.pg_attribute WHERE attrelid=marker_oid AND attnum>0 AND NOT attisdropped AND attacl IS NOT NULL) THEN
    RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='migrator marker shape mismatch';
  END IF;
END;
$$;

CREATE FUNCTION public.runtime_enter_migrator()
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE generation bigint; locked boolean := false;
BEGIN
  IF session_user<>'aboutme_migrator' THEN RAISE EXCEPTION USING ERRCODE='42501', MESSAGE='migrator role required'; END IF;
  IF EXISTS (SELECT 1 FROM pg_catalog.pg_class WHERE relnamespace=pg_my_temp_schema() AND relname='runtime_migrator_session_v1') THEN
    PERFORM public.runtime_validate_migrator_marker();
    IF EXISTS (SELECT 1 FROM pg_temp.runtime_migrator_session_v1 WHERE singleton AND backend_pid=pg_backend_pid() AND session_role_oid=(session_user::regrole)::oid) AND
       EXISTS (SELECT 1 FROM pg_catalog.pg_locks WHERE pid=pg_backend_pid() AND database=(SELECT oid FROM pg_catalog.pg_database WHERE datname=current_database()) AND locktype='advisory' AND classid=867785103 AND objid=2177536586 AND objsubid=1 AND mode='ShareLock' AND granted) THEN RETURN; END IF;
    IF EXISTS (SELECT 1 FROM pg_temp.runtime_migrator_session_v1) THEN RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='migrator marker mismatch'; END IF;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_class WHERE relnamespace=pg_my_temp_schema() AND relname='runtime_migrator_session_v1') AND
     (EXISTS (SELECT 1 FROM pg_catalog.pg_type WHERE typnamespace=pg_my_temp_schema() AND typname='runtime_migrator_session_v1') OR
      EXISTS (SELECT 1 FROM pg_catalog.pg_class WHERE relnamespace=pg_my_temp_schema() AND relname='runtime_migrator_session_v1_pkey')) THEN
    RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='migrator marker catalog name collision';
  END IF;
  PERFORM pg_catalog.pg_advisory_lock_shared(3727108639518528074); locked := true;
  BEGIN
    SELECT s.generation INTO generation FROM public.runtime_write_state s WHERE singleton AND write_gate='open';
    IF generation IS NULL THEN RAISE EXCEPTION USING ERRCODE='55000', MESSAGE='runtime write gate unavailable'; END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_class WHERE relnamespace=pg_my_temp_schema() AND relname='runtime_migrator_session_v1') THEN CREATE TEMP TABLE pg_temp.runtime_migrator_session_v1(singleton boolean PRIMARY KEY CHECK(singleton), backend_pid integer NOT NULL, session_role_oid oid NOT NULL, entry_generation bigint NOT NULL CHECK(entry_generation>0), entered_at timestamptz NOT NULL) ON COMMIT PRESERVE ROWS; END IF;
    INSERT INTO pg_temp.runtime_migrator_session_v1 VALUES(true,pg_backend_pid(),(session_user::regrole)::oid,generation,clock_timestamp());
    PERFORM public.runtime_validate_migrator_marker();
  EXCEPTION WHEN query_canceled OR assert_failure THEN
    IF locked THEN PERFORM pg_catalog.pg_advisory_unlock_shared(3727108639518528074); END IF;
    RAISE;
  WHEN OTHERS THEN
    IF locked THEN PERFORM pg_catalog.pg_advisory_unlock_shared(3727108639518528074); END IF;
    RAISE;
  END;
END;
$$;

CREATE FUNCTION public.runtime_begin_migration_write(operation_id text)
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE generation bigint;
BEGIN
  IF session_user<>'aboutme_migrator' OR operation_id IS NULL OR octet_length(operation_id) NOT BETWEEN 1 AND 128 THEN RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='invalid migration entry'; END IF;
  PERFORM public.runtime_validate_migrator_marker();
  IF EXISTS (SELECT 1 FROM pg_catalog.pg_locks WHERE pid=pg_backend_pid()
    AND database=(SELECT oid FROM pg_catalog.pg_database WHERE datname=current_database())
    AND locktype='advisory' AND classid=295306100 AND objid=pg_backend_pid()
    AND objsubid=2 AND mode='ExclusiveLock' AND granted) THEN
    RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='runtime finish guard already held';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_temp.runtime_migrator_session_v1 WHERE singleton AND backend_pid=pg_backend_pid() AND session_role_oid=(session_user::regrole)::oid) OR
     NOT EXISTS (SELECT 1 FROM pg_catalog.pg_locks WHERE pid=pg_backend_pid() AND database=(SELECT oid FROM pg_catalog.pg_database WHERE datname=current_database()) AND locktype='advisory' AND classid=867785103 AND objid=2177536586 AND objsubid=1 AND mode='ShareLock' AND granted) THEN RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='migrator authority missing'; END IF;
  SELECT entry_generation INTO generation FROM pg_temp.runtime_migrator_session_v1 WHERE singleton;
  PERFORM public.runtime_create_write_marker();
  IF EXISTS (SELECT 1 FROM pg_temp.runtime_write_entry_v1) THEN RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='write marker already present'; END IF;
  INSERT INTO pg_temp.runtime_write_entry_v1(transaction_id,backend_pid,session_role_oid,writer_kind,entry_generation,dirty,operation_id) VALUES(pg_current_xact_id(),pg_backend_pid(),(session_user::regrole)::oid,'migrator',generation,true,operation_id);
END;
$$;

CREATE FUNCTION public.runtime_exit_migrator()
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE unlocked boolean;
BEGIN
  IF session_user<>'aboutme_migrator' THEN RAISE EXCEPTION USING ERRCODE='42501', MESSAGE='migrator role required'; END IF;
  PERFORM public.runtime_validate_migrator_marker();
  IF EXISTS (SELECT 1 FROM pg_catalog.pg_class WHERE relnamespace=pg_my_temp_schema() AND relname='runtime_write_entry_v1') THEN
    PERFORM public.runtime_validate_write_marker();
    IF EXISTS (SELECT 1 FROM pg_temp.runtime_write_entry_v1) THEN
      RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='migration write marker still active';
    END IF;
  END IF;
  IF (SELECT count(*) FROM pg_temp.runtime_migrator_session_v1 WHERE singleton AND backend_pid=pg_backend_pid() AND session_role_oid=(session_user::regrole)::oid)<>1 THEN RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='migrator marker mismatch'; END IF;
  DELETE FROM pg_temp.runtime_migrator_session_v1;
  SELECT pg_catalog.pg_advisory_unlock_shared(3727108639518528074) INTO unlocked;
  IF NOT unlocked THEN RAISE EXCEPTION USING ERRCODE='AM001', MESSAGE='migrator lock missing'; END IF;
END;
$$;

CREATE FUNCTION public.runtime_read_migrator_metadata()
RETURNS TABLE(write_gate text, generation bigint, migrator_enforcement_version smallint, migration_history_owner text)
LANGUAGE sql SECURITY DEFINER SET search_path = pg_catalog AS $$
  SELECT s.write_gate,s.generation,s.migrator_enforcement_version,s.migration_history_owner
  FROM public.runtime_write_state AS s WHERE s.singleton
$$;

ALTER FUNCTION public.runtime_assert_write_finished() OWNER TO aboutme_runtime_owner;
ALTER FUNCTION public.runtime_validate_write_marker() OWNER TO aboutme_runtime_owner;
ALTER FUNCTION public.runtime_create_write_marker() OWNER TO aboutme_runtime_owner;
ALTER FUNCTION public.runtime_require_write_entry() OWNER TO aboutme_runtime_owner;
ALTER FUNCTION public.runtime_enter_write() OWNER TO aboutme_runtime_owner;
ALTER FUNCTION public.runtime_assert_write_entry() OWNER TO aboutme_runtime_owner;
ALTER FUNCTION public.runtime_assert_business_write() OWNER TO aboutme_runtime_owner;
ALTER FUNCTION public.runtime_finish_write() OWNER TO aboutme_runtime_owner;
ALTER FUNCTION public.runtime_validate_migrator_marker() OWNER TO aboutme_runtime_owner;
ALTER FUNCTION public.runtime_enter_migrator() OWNER TO aboutme_runtime_owner;
ALTER FUNCTION public.runtime_begin_migration_write(text) OWNER TO aboutme_runtime_owner;
ALTER FUNCTION public.runtime_exit_migrator() OWNER TO aboutme_runtime_owner;
ALTER FUNCTION public.runtime_read_migrator_metadata() OWNER TO aboutme_runtime_owner;

SET LOCAL ROLE aboutme_runtime_owner;

REVOKE ALL ON FUNCTION public.runtime_assert_write_finished() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.runtime_validate_write_marker() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.runtime_create_write_marker() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.runtime_require_write_entry() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.runtime_enter_write() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.runtime_assert_write_entry() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.runtime_assert_business_write() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.runtime_finish_write() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.runtime_validate_migrator_marker() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.runtime_enter_migrator() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.runtime_begin_migration_write(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.runtime_exit_migrator() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.runtime_read_migrator_metadata() FROM PUBLIC;

GRANT EXECUTE ON FUNCTION public.runtime_enter_write(), public.runtime_finish_write() TO aboutme_app, aboutme_maintenance, aboutme_lifecycle_command, aboutme_fencing_proof;
GRANT EXECUTE ON FUNCTION public.runtime_enter_migrator(), public.runtime_begin_migration_write(text), public.runtime_finish_write(), public.runtime_exit_migrator(), public.runtime_read_migrator_metadata() TO aboutme_migrator;

RESET ROLE;

REVOKE CREATE ON SCHEMA public FROM aboutme_runtime_owner;
-- +goose StatementEnd

-- +goose Down
