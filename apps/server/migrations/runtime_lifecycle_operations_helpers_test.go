package migrations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// lifecycleOperationSignatures lists the six fixed controller actions with
// their exact argument types, in the accepted signature order.
var lifecycleOperationSignatures = []struct{ name, signature, result string }{
	{"runtime_prepare_scale_out", "(bigint,text)", "runtime_capacity_result"},
	{"runtime_activate_replica_capacity", "(bigint,text,uuid,text,text,text,text,text,text,uuid)", "runtime_replica_result"},
	{"runtime_prepare_scale_in", "(bigint,text,uuid,text,text)", "runtime_replica_result"},
	{"runtime_finish_scale_in", "(bigint,text,uuid,text,text,text,text)", "runtime_replica_result"},
	{"runtime_prepare_maintenance_drain", "(bigint,text,uuid,text,text)", "runtime_replica_result"},
	{"runtime_begin_replica_termination", "(bigint,text,text,uuid,text,text,text)", "runtime_replica_result"},
}

// lifecycleResultComposites fixes each returned composite's field order and
// type. Migration 00015 owns both types; migration 00022 must not reshape
// them.
var lifecycleResultComposites = []struct{ name, attributes string }{
	{
		"runtime_replica_result",
		"replica_id:uuid,replica_kind:text,state:text,desired_replicas:smallint," +
			"active_serving_replicas:smallint,active_maintenance_replicas:smallint," +
			"partition_1_enabled:boolean,partition_2_enabled:boolean,capacity_generation:bigint," +
			"controller_generation:bigint,controller_operation_id:text,admission_enabled:boolean," +
			"lifecycle_phase:text,replayed:boolean",
	},
	{
		"runtime_capacity_result",
		"desired_replicas:smallint,active_serving_replicas:smallint,active_maintenance_replicas:smallint," +
			"partition_1_enabled:boolean,partition_2_enabled:boolean,capacity_generation:bigint," +
			"controller_generation:bigint,controller_operation_id:text,admission_enabled:boolean," +
			"lifecycle_phase:text,write_gate:text,write_generation:bigint,replayed:boolean",
	},
}

// lifecycleHelperSignatures lists the owner-only helpers migration 00022
// installs. No login role receives EXECUTE on any of them.
var lifecycleHelperSignatures = []struct{ name, signature string }{
	{"runtime_sample_lifecycle_time", "()"},
	{"runtime_lifecycle_digest_field", "(smallint,bytea)"},
	{"runtime_lifecycle_digest", "(smallint,bytea)"},
	{"runtime_lifecycle_field_uuid", "(uuid)"},
	{"runtime_lifecycle_field_int8", "(bigint)"},
	{"runtime_lifecycle_field_text", "(text)"},
	{"runtime_lifecycle_field_bool", "(boolean)"},
	{"runtime_lifecycle_result_fields", "(text,text,bigint,text,uuid,text,text,bigint,bigint,bigint,boolean,boolean,bigint,bigint,text,boolean,text,text,bigint)"},
	{"runtime_lifecycle_result_digest", "(text,text,bigint,text,uuid,text,text,bigint,bigint,bigint,boolean,boolean,bigint,bigint,text,boolean,text,text,bigint)"},
	{"runtime_lifecycle_assert_vector", "(smallint,bytea,integer,text)"},
	{"runtime_lifecycle_assert_digest_vectors", "()"},
	{"runtime_lifecycle_validate_scalars", "(bigint,text)"},
	{"runtime_lifecycle_validate_evidence", "(text,boolean)"},
	{"runtime_lifecycle_validate_replica_input", "(uuid,text,text)"},
	{"runtime_lifecycle_validate_reason", "(text)"},
	{"runtime_lifecycle_lock_singletons", "()"},
	{"runtime_lifecycle_parent_kind", "(text)"},
	{"runtime_lifecycle_create_parent", "(text,text)"},
	{"runtime_lifecycle_step", "(text,text)"},
	{"runtime_lifecycle_require_first_action", "(text)"},
	{"runtime_lifecycle_require_predecessor", "(text,text,bigint)"},
	{"runtime_lifecycle_assert_step_integrity", "(public.runtime_lifecycle_operation_steps,bytea)"},
	{"runtime_lifecycle_replay_capacity", "(public.runtime_lifecycle_operation_steps,bytea)"},
	{"runtime_lifecycle_replay_replica", "(public.runtime_lifecycle_operation_steps,bytea)"},
	{"runtime_lifecycle_lock_partitions", "()"},
	{"runtime_lifecycle_partition_flags", "()"},
	{"runtime_lifecycle_set_partition", "(smallint,boolean,bigint,text,timestamptz)"},
	{"runtime_lifecycle_replica_high", "(public.runtime_replicas)"},
	{"runtime_lifecycle_operation_high", "(text)"},
	{"runtime_lifecycle_evidence_high", "(uuid)"},
	{"runtime_lifecycle_serving_count", "()"},
	{"runtime_lifecycle_maintenance_count", "()"},
	{"runtime_lifecycle_assert_open_state", "(public.runtime_capacity)"},
	{"runtime_lifecycle_assert_no_blockers", "()"},
	{"runtime_lifecycle_assert_generation_room", "(public.runtime_capacity)"},
	{"runtime_lifecycle_advance_capacity", "(text,timestamptz,smallint)"},
	{"runtime_lifecycle_lock_replicas", "(uuid,uuid)"},
	{"runtime_lifecycle_load_target", "(uuid,text,text)"},
	{"runtime_lifecycle_assert_task_identity", "(public.runtime_replicas,text,text,text,text)"},
	{"runtime_lifecycle_set_replica_state", "(uuid,text,timestamptz)"},
	{"runtime_lifecycle_capacity_value", "(public.runtime_capacity,smallint,smallint,boolean,boolean,boolean)"},
	{"runtime_lifecycle_replica_value", "(public.runtime_replicas,public.runtime_capacity,smallint,smallint,smallint,boolean,boolean,boolean)"},
	{"runtime_lifecycle_assert_no_intent", "(uuid)"},
	{"runtime_lifecycle_lock_intent", "(uuid)"},
	{"runtime_lifecycle_assert_request_identity", "(text,uuid)"},
	{"runtime_lifecycle_assert_replacement", "(uuid)"},
	{"runtime_lifecycle_assert_single_maintenance", "(uuid)"},
	{"runtime_lifecycle_assert_no_other_drain", "(text,uuid)"},
	{"runtime_lifecycle_assert_scale_in_survivors", "(uuid)"},
	{"runtime_lifecycle_assert_leave_receipt", "(uuid,text,text,text)"},
	{"runtime_lifecycle_assert_intent_evidence", "(uuid,text,text)"},
	{"runtime_lifecycle_assert_fencing_proof", "(uuid,text,text,text)"},
	{"runtime_lifecycle_assert_drain_predecessor", "(uuid)"},
	{"runtime_lifecycle_record_step", "(text,text,text,bigint,bytea,uuid,text,uuid,text,text,bigint,bigint,bigint,boolean,boolean,public.runtime_capacity,timestamptz)"},
	{"runtime_lifecycle_record_intent", "(uuid,text,text,text,text,timestamptz)"},
}

// lifecycleFunctionVolatility pins each installed function's declared
// volatility. Anything that samples or compares database time is STABLE or
// VOLATILE, never IMMUTABLE.
var lifecycleFunctionVolatility = map[string]string{
	"runtime_sample_lifecycle_time":               "v",
	"runtime_lifecycle_digest_field":              "i",
	"runtime_lifecycle_digest":                    "i",
	"runtime_lifecycle_field_uuid":                "i",
	"runtime_lifecycle_field_int8":                "i",
	"runtime_lifecycle_field_text":                "i",
	"runtime_lifecycle_field_bool":                "i",
	"runtime_lifecycle_result_fields":             "i",
	"runtime_lifecycle_result_digest":             "i",
	"runtime_lifecycle_assert_vector":             "i",
	"runtime_lifecycle_assert_digest_vectors":     "i",
	"runtime_lifecycle_validate_scalars":          "i",
	"runtime_lifecycle_validate_evidence":         "i",
	"runtime_lifecycle_validate_replica_input":    "i",
	"runtime_lifecycle_validate_reason":           "i",
	"runtime_lifecycle_lock_singletons":           "v",
	"runtime_lifecycle_parent_kind":               "v",
	"runtime_lifecycle_create_parent":             "v",
	"runtime_lifecycle_step":                      "s",
	"runtime_lifecycle_require_first_action":      "s",
	"runtime_lifecycle_require_predecessor":       "s",
	"runtime_lifecycle_assert_step_integrity":     "s",
	"runtime_lifecycle_replay_capacity":           "s",
	"runtime_lifecycle_replay_replica":            "s",
	"runtime_lifecycle_lock_partitions":           "v",
	"runtime_lifecycle_partition_flags":           "s",
	"runtime_lifecycle_set_partition":             "v",
	"runtime_lifecycle_replica_high":              "s",
	"runtime_lifecycle_operation_high":            "s",
	"runtime_lifecycle_evidence_high":             "s",
	"runtime_lifecycle_serving_count":             "s",
	"runtime_lifecycle_maintenance_count":         "s",
	"runtime_lifecycle_assert_open_state":         "i",
	"runtime_lifecycle_assert_no_blockers":        "s",
	"runtime_lifecycle_assert_generation_room":    "i",
	"runtime_lifecycle_advance_capacity":          "v",
	"runtime_lifecycle_lock_replicas":             "v",
	"runtime_lifecycle_load_target":               "v",
	"runtime_lifecycle_assert_task_identity":      "i",
	"runtime_lifecycle_set_replica_state":         "v",
	"runtime_lifecycle_capacity_value":            "s",
	"runtime_lifecycle_replica_value":             "s",
	"runtime_lifecycle_assert_no_intent":          "s",
	"runtime_lifecycle_lock_intent":               "v",
	"runtime_lifecycle_assert_request_identity":   "s",
	"runtime_lifecycle_assert_replacement":        "s",
	"runtime_lifecycle_assert_single_maintenance": "s",
	"runtime_lifecycle_assert_no_other_drain":     "s",
	"runtime_lifecycle_assert_scale_in_survivors": "s",
	"runtime_lifecycle_assert_leave_receipt":      "v",
	"runtime_lifecycle_assert_intent_evidence":    "v",
	"runtime_lifecycle_assert_fencing_proof":      "v",
	"runtime_lifecycle_assert_drain_predecessor":  "s",
	"runtime_lifecycle_record_step":               "v",
	"runtime_lifecycle_record_intent":             "v",
	"runtime_prepare_scale_out":                   "v",
	"runtime_activate_replica_capacity":           "v",
	"runtime_prepare_scale_in":                    "v",
	"runtime_finish_scale_in":                     "v",
	"runtime_prepare_maintenance_drain":           "v",
	"runtime_begin_replica_termination":           "v",
}

func runtimeLifecycleDB(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	db := newMigratedTestDatabase(t, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return db, ctx
}

const (
	lifecycleRole        = "aboutme_lifecycle_command"
	lifecycleNilUUID     = "00000000-0000-0000-0000-000000000000"
	lifecycleCapacitySQL = `(r).desired_replicas,(r).active_serving_replicas,(r).active_maintenance_replicas,` +
		`(r).partition_1_enabled,(r).partition_2_enabled,(r).capacity_generation,(r).controller_generation,` +
		`(r).controller_operation_id,(r).admission_enabled,(r).lifecycle_phase,(r).write_gate,(r).write_generation,(r).replayed`
	lifecycleReplicaSQL = `(r).replica_id::text,(r).replica_kind,(r).state,(r).desired_replicas,` +
		`(r).active_serving_replicas,(r).active_maintenance_replicas,(r).partition_1_enabled,(r).partition_2_enabled,` +
		`(r).capacity_generation,(r).controller_generation,(r).controller_operation_id,(r).admission_enabled,` +
		`(r).lifecycle_phase,(r).replayed`
)

type lifecycleCapacityResult struct {
	desired, serving, maintenance       int16
	partition1, partition2              bool
	capacityGeneration, controllerGen   int64
	controllerOperation, lifecyclePhase string
	admission, replayed                 bool
	writeGate                           sql.NullString
	writeGeneration                     sql.NullInt64
}

func lifecycleStatement(expression, projection string) string {
	return `WITH result AS MATERIALIZED (SELECT ` + expression + ` AS r) SELECT ` + projection + ` FROM result`
}

// lifecycleCall runs one fixed controller action inside a bounded lifecycle
// write transaction and scans its single composite row into dest.
func lifecycleCall(t *testing.T, db *sql.DB, role, expression, projection string, dest ...any) error {
	t.Helper()
	return lifecycleCallWithSearchPath(t, db, role, "pg_temp,public", expression, projection, dest...)
}

func lifecycleCallWithSearchPath(t *testing.T, db *sql.DB, role, searchPath, expression, projection string, dest ...any) (resultErr error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	authenticated, txActive := false, false
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if txActive {
			_, rollbackErr := conn.ExecContext(cleanup, `ROLLBACK`)
			resultErr = errors.Join(resultErr, rollbackErr)
		}
		if authenticated {
			_, resetErr := conn.ExecContext(cleanup, `RESET ALL; RESET SESSION AUTHORIZATION`)
			resultErr = errors.Join(resultErr, resetErr)
		}
		resultErr = errors.Join(resultErr, conn.Close())
	}()
	if _, err = conn.ExecContext(ctx, `SET SESSION AUTHORIZATION `+role); err != nil {
		return err
	}
	authenticated = true
	if _, err = conn.ExecContext(ctx, `SET search_path=`+searchPath); err != nil {
		return err
	}
	if _, err = conn.ExecContext(ctx, `BEGIN`); err != nil {
		return err
	}
	txActive = true
	if _, err = conn.ExecContext(ctx, `SELECT public.runtime_enter_write()`); err != nil {
		return err
	}
	if err = conn.QueryRowContext(ctx, lifecycleStatement(expression, projection)).Scan(dest...); err != nil {
		return err
	}
	if _, err = conn.ExecContext(ctx, `SELECT public.runtime_finish_write(); COMMIT`); err != nil {
		return err
	}
	txActive = false
	return nil
}

func callLifecycleCapacity(t *testing.T, db *sql.DB, expression string) (lifecycleCapacityResult, error) {
	t.Helper()
	var r lifecycleCapacityResult
	err := lifecycleCall(t, db, lifecycleRole, expression, lifecycleCapacitySQL,
		&r.desired, &r.serving, &r.maintenance, &r.partition1, &r.partition2, &r.capacityGeneration,
		&r.controllerGen, &r.controllerOperation, &r.admission, &r.lifecyclePhase, &r.writeGate,
		&r.writeGeneration, &r.replayed)
	return r, err
}

func callLifecycleReplica(t *testing.T, db *sql.DB, expression string) (registrationResult, error) {
	t.Helper()
	var r registrationResult
	err := lifecycleCall(t, db, lifecycleRole, expression, lifecycleReplicaSQL,
		&r.replicaID, &r.kind, &r.state, &r.desired, &r.serving, &r.maintenance, &r.partition1, &r.partition2,
		&r.capacityGeneration, &r.controllerGeneration, &r.controllerOperation, &r.admission, &r.lifecycle, &r.replayed)
	return r, err
}

func lifecycleScaleOutExpr(expected int64, operation string) string {
	return fmt.Sprintf(`public.runtime_prepare_scale_out(%d,'%s')`, expected, operation)
}

func lifecycleActivateExpr(expected int64, operation string, id registrationIdentity, replaced string) string {
	replacement := "NULL"
	if replaced != "" {
		replacement = "'" + replaced + "'"
	}
	return fmt.Sprintf(`public.runtime_activate_replica_capacity(%d,'%s','%s','%s','%s','%s','%s','%s','%s',%s)`,
		expected, operation, id.replicaID, id.instanceID, id.containerARN, id.caddyARN, id.goARN, id.nuxtARN,
		id.release, replacement)
}

func lifecycleScaleInExpr(expected int64, operation string, id registrationIdentity) string {
	return fmt.Sprintf(`public.runtime_prepare_scale_in(%d,'%s','%s','%s','%s')`,
		expected, operation, id.replicaID, id.instanceID, id.release)
}

func lifecycleFinishExpr(expected int64, operation string, id registrationIdentity, leave, evidence string) string {
	leaveArg := "NULL"
	if leave != "" {
		leaveArg = "'" + leave + "'"
	}
	return fmt.Sprintf(`public.runtime_finish_scale_in(%d,'%s','%s','%s','%s',%s,'%s')`,
		expected, operation, id.replicaID, id.instanceID, id.release, leaveArg, evidence)
}

func lifecycleDrainExpr(expected int64, operation string, id registrationIdentity) string {
	return fmt.Sprintf(`public.runtime_prepare_maintenance_drain(%d,'%s','%s','%s','%s')`,
		expected, operation, id.replicaID, id.instanceID, id.release)
}

func lifecycleTerminateExpr(expected int64, operation, request string, id registrationIdentity, reason string) string {
	return fmt.Sprintf(`public.runtime_begin_replica_termination(%d,'%s','%s','%s','%s','%s','%s')`,
		expected, operation, request, id.replicaID, id.instanceID, id.release, reason)
}

func requireLifecycleError(t *testing.T, err error, code string) {
	t.Helper()
	requireRegistrationError(t, err, code)
}

func lifecycleErrorOf(t *testing.T, err error, code string) *pgconn.PgError {
	t.Helper()
	requireRegistrationError(t, err, code)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("lifecycle error is not a database error: %v", err)
	}
	return pgErr
}

// requireNoLifecycleIdentityLeak proves a fixed error carries no supplied
// identity or evidence value and sets no structured diagnostic field.
func requireNoLifecycleIdentityLeak(t *testing.T, pgErr *pgconn.PgError, secrets ...string) {
	t.Helper()
	requireNoClaimIdentityLeak(t, pgErr, secrets...)
}

// lifecycleCapacityState reads the durable capacity singleton.
type lifecycleCapacityState struct {
	desired                          int16
	generation, controllerGeneration int64
	controllerOperation, updatedBy   string
	phase                            string
	admission                        bool
	updatedAt                        time.Time
}

func lifecycleCapacity(t *testing.T, db *sql.DB) lifecycleCapacityState {
	t.Helper()
	var state lifecycleCapacityState
	if err := db.QueryRowContext(context.Background(), `SELECT desired_replicas,generation,controller_generation,controller_operation_id,updated_by,lifecycle_phase,admission_enabled,updated_at FROM public.runtime_capacity WHERE singleton`).
		Scan(&state.desired, &state.generation, &state.controllerGeneration, &state.controllerOperation,
			&state.updatedBy, &state.phase, &state.admission, &state.updatedAt); err != nil {
		t.Fatal(err)
	}
	return state
}

// lifecyclePartitionSummary counts the enabled rows of one ordinal and the
// distinct evidence tuples across its 24 policies.
type lifecyclePartitionSummary struct {
	rows, enabled, distinctEvidence int
	generation                      int64
	operation                       string
}

func lifecyclePartition(t *testing.T, db *sql.DB, partition int) lifecyclePartitionSummary {
	t.Helper()
	var summary lifecyclePartitionSummary
	if err := db.QueryRowContext(context.Background(), `SELECT count(*),count(*) FILTER (WHERE enabled),count(DISTINCT (enabled,capacity_generation,operation_id)),max(capacity_generation),max(operation_id) FROM public.shared_rate_partitions WHERE partition=$1`, partition).
		Scan(&summary.rows, &summary.enabled, &summary.distinctEvidence, &summary.generation, &summary.operation); err != nil {
		t.Fatal(err)
	}
	return summary
}

func lifecycleReplicaState(t *testing.T, db *sql.DB, replicaID string) string {
	t.Helper()
	var state string
	if err := db.QueryRowContext(context.Background(), `SELECT state FROM public.runtime_replicas WHERE replica_id=$1`, replicaID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}

func lifecycleStepCount(t *testing.T, db *sql.DB) (operations, steps, intents int) {
	t.Helper()
	if err := db.QueryRowContext(context.Background(), `SELECT (SELECT count(*) FROM public.runtime_lifecycle_operations),(SELECT count(*) FROM public.runtime_lifecycle_operation_steps),(SELECT count(*) FROM public.runtime_termination_intents)`).
		Scan(&operations, &steps, &intents); err != nil {
		t.Fatal(err)
	}
	return operations, steps, intents
}

// lifecycleRegisterReady drives the accepted registration and readiness
// wrappers as their real login role, so an activation fixture never fabricates
// membership rows.
func lifecycleRegisterReady(t *testing.T, db *sql.DB, kind string, id registrationIdentity) {
	t.Helper()
	role, register, ready := "aboutme_app", "runtime_register_serving_replica", "runtime_mark_serving_replica_join_ready"
	if kind == "maintenance" {
		role, register, ready = "aboutme_maintenance", "runtime_register_maintenance_replica", "runtime_mark_maintenance_replica_join_ready"
	}
	if _, err := callRegistration(t, db, role, register, id); err != nil {
		t.Fatal(err)
	}
	if _, err := callRegistration(t, db, role, ready, id); err != nil {
		t.Fatal(err)
	}
}

// lifecycleActivateServing registers, readies and activates one serving
// replica through the real wrappers and returns the resulting controller
// generation.
func lifecycleActivateServing(t *testing.T, db *sql.DB, expected int64, operation string, id registrationIdentity, replaced string) registrationResult {
	t.Helper()
	lifecycleRegisterReady(t, db, "serving", id)
	result, err := callLifecycleReplica(t, db, lifecycleActivateExpr(expected, operation, id, replaced))
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func lifecycleMarkLeftSQL(id registrationIdentity) string {
	return fmt.Sprintf(`UPDATE public.runtime_replicas SET state='left',left_at=clock_timestamp() WHERE replica_id='%s'`, id.replicaID)
}

func lifecycleMarkFencedSQL(id registrationIdentity) string {
	return fmt.Sprintf(`UPDATE public.runtime_replicas SET state='fenced',fenced_at=clock_timestamp() WHERE replica_id='%s'`, id.replicaID)
}

func lifecycleLeaveReceiptSQL(id registrationIdentity, operation string) string {
	return fmt.Sprintf(`INSERT INTO public.runtime_leave_receipts(replica_id,instance_id,release_digest,controller_generation,operation_id,joined_transition_count,joined_claim_count) VALUES('%s','%s','%s',1,'%s',0,0)`,
		id.replicaID, id.instanceID, id.release, operation)
}

func lifecycleProofSQL(id registrationIdentity, evidence string) string {
	return fmt.Sprintf(`INSERT INTO public.runtime_fencing_proofs(replica_id,instance_id,release_digest,adapter,evidence_id,requested_at,observed_terminated_at,observed_state) VALUES('%s','%s','%s','ec2_terminated_v1','%s',clock_timestamp(),clock_timestamp(),'terminated')`,
		id.replicaID, id.instanceID, id.release, evidence)
}

// lifecycleWakeFixture builds the accepted durable wake parent and its two
// completed wake steps. The wake operations themselves belong to a later
// slice, so these protected owner rows stand in for their history.
func lifecycleWakeFixture(operation, workflow string) []string {
	return []string{
		fmt.Sprintf(`INSERT INTO public.runtime_lifecycle_operations(operation_id,workflow_kind) VALUES('%s','%s')`, operation, workflow),
		lifecycleStepSQL(operation, workflow, "begin_wake", 1),
		lifecycleStepSQL(operation, workflow, "complete_wake", 2),
	}
}

// lifecycleSetControllerGenerationSQL pins the capacity singleton's controller
// generation so a fixture wake history can precede a real action.
func lifecycleSetControllerGenerationSQL(value int64) string {
	return fmt.Sprintf(`UPDATE public.runtime_capacity SET controller_generation=%d WHERE singleton`, value)
}

func lifecycleWrite(t *testing.T, db *sql.DB, statements ...string) {
	t.Helper()
	if err := membershipWrite(t, db, statements...); err != nil {
		t.Fatal(err)
	}
}

// lifecycleOwnerQuery runs one read as aboutme_runtime_owner, the only role
// that may reach the digest helpers.
func lifecycleOwnerQuery(t *testing.T, db *sql.DB, statement string, dest ...any) (resultErr error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_, resetErr := conn.ExecContext(cleanup, `RESET SESSION AUTHORIZATION`)
		resultErr = errors.Join(resultErr, resetErr, conn.Close())
	}()
	if _, err = conn.ExecContext(ctx, `SET SESSION AUTHORIZATION aboutme_runtime_owner`); err != nil {
		return err
	}
	return conn.QueryRowContext(ctx, statement).Scan(dest...)
}

// lifecycleHexBytes renders a Go byte slice as a SQL hex literal.
func lifecycleHexBytes(value []byte) string {
	return `decode('` + hex.EncodeToString(value) + `','hex')`
}

// The independent Go encoder. It repeats the accepted canonical byte stream
// without calling any SQL, so a SQL encoding change cannot silently move both
// sides together.
const (
	lifecycleDigestDomain  = "aboutme.runtime-lifecycle-step.v1"
	lifecycleKindArguments = 1
	lifecycleKindResult    = 2
	lifecycleTypeUUID      = 1
	lifecycleTypeInt8      = 2
	lifecycleTypeText      = 3
	lifecycleTypeBool      = 4
)

func goLifecycleField(fieldType byte, payload []byte, present bool) []byte {
	framed := []byte{fieldType, 0x00, 0, 0, 0, 0}
	if !present {
		return framed
	}
	framed[1] = 0x01
	binary.BigEndian.PutUint32(framed[2:6], uint32(len(payload)))
	return append(framed, payload...)
}

func goLifecycleUUID(value string) []byte {
	if value == "" {
		return goLifecycleField(lifecycleTypeUUID, nil, false)
	}
	raw, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	if err != nil || len(raw) != 16 {
		panic("lifecycle vector uuid " + value)
	}
	return goLifecycleField(lifecycleTypeUUID, raw, true)
}

func goLifecycleInt8(value int64, present bool) []byte {
	payload := make([]byte, 8)
	binary.BigEndian.PutUint64(payload, uint64(value))
	return goLifecycleField(lifecycleTypeInt8, payload, present)
}

func goLifecycleText(value string, present bool) []byte {
	return goLifecycleField(lifecycleTypeText, []byte(value), present)
}

func goLifecycleBool(value, present bool) []byte {
	payload := []byte{0x00}
	if value {
		payload[0] = 0x01
	}
	return goLifecycleField(lifecycleTypeBool, payload, present)
}

func goLifecycleStream(kind byte, fields ...[]byte) []byte {
	stream := append([]byte(lifecycleDigestDomain), 0x00, kind)
	for _, field := range fields {
		stream = append(stream, field...)
	}
	return stream
}

func goLifecycleDigest(stream []byte) string {
	sum := sha256.Sum256(stream)
	return hex.EncodeToString(sum[:])
}

// The public synthetic fixture from lifecycle-vectors.md.
const (
	vectorReplica   = "11111111-2222-4333-8444-555555555555"
	vectorReplaced  = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	vectorInstance  = "i-0123456789abcdef0"
	vectorContainer = "arn:aws:ecs:ap-southeast-1:123456789012:container-instance/aboutme/fixture"
	vectorCaddy     = "arn:aws:ecs:ap-southeast-1:123456789012:task/aboutme/caddy-fixture"
	vectorGo        = "arn:aws:ecs:ap-southeast-1:123456789012:task/aboutme/go-fixture"
	vectorNuxt      = "arn:aws:ecs:ap-southeast-1:123456789012:task/aboutme/nuxt-fixture"
	vectorExpected  = int64(40)
	vectorResult    = int64(41)
)

type lifecycleVector struct {
	name           string
	argumentFields [][]byte
	resultFields   [][]byte
	argumentBytes  int
	argumentDigest string
	resultBytes    int
	resultDigest   string
}

// goLifecycleResultFields repeats the nineteen canonical result fields in
// their fixed order, including every explicit null and both duplicated header
// values.
func goLifecycleResultFields(operation, action string, resultGeneration int64, resultKind, replica, replicaKind,
	replicaState string, desired, serving, maintenance int64, counts bool, partition1, partition2, flags bool,
	capacityGeneration int64, controllerOperation string, admission bool, phase, writeGate string,
	writeGeneration int64, wake bool) [][]byte {
	return [][]byte{
		goLifecycleText(operation, true),
		goLifecycleText(action, true),
		goLifecycleInt8(resultGeneration, true),
		goLifecycleText(resultKind, true),
		goLifecycleUUID(replica),
		goLifecycleText(replicaKind, replicaKind != ""),
		goLifecycleText(replicaState, replicaState != ""),
		goLifecycleInt8(desired, counts),
		goLifecycleInt8(serving, counts),
		goLifecycleInt8(maintenance, counts),
		goLifecycleBool(partition1, flags),
		goLifecycleBool(partition2, flags),
		goLifecycleInt8(capacityGeneration, true),
		goLifecycleInt8(resultGeneration, true),
		goLifecycleText(controllerOperation, true),
		goLifecycleBool(admission, true),
		goLifecycleText(phase, true),
		goLifecycleText(writeGate, wake),
		goLifecycleInt8(writeGeneration, wake),
	}
}

func lifecycleVectors() []lifecycleVector {
	identity := [][]byte{
		goLifecycleUUID(vectorReplica), goLifecycleText(vectorInstance, true),
		goLifecycleText(vectorContainer, true), goLifecycleText(vectorCaddy, true),
		goLifecycleText(vectorGo, true), goLifecycleText(vectorNuxt, true),
		goLifecycleText(vectorRelease(), true),
	}
	header := func(operation, action string) [][]byte {
		return [][]byte{goLifecycleText(operation, true), goLifecycleText(action, true), goLifecycleInt8(vectorExpected, true)}
	}
	join := func(parts ...[][]byte) [][]byte {
		var all [][]byte
		for _, part := range parts {
			all = append(all, part...)
		}
		return all
	}
	return []lifecycleVector{
		{
			name:           "prepare_scale_out",
			argumentFields: header("op-scale-out", "prepare_scale_out"),
			resultFields: goLifecycleResultFields("op-scale-out", "prepare_scale_out", vectorResult, "capacity",
				"", "", "", 2, 1, 0, true, true, false, true, 71, "op-scale-out", true, "online", "", 0, false),
			argumentBytes: 90, argumentDigest: "cf000ef3bf307c8cdf230c419a40430e53390fe2cae60111951cc08a28a344f3",
			resultBytes: 255, resultDigest: "97583a4d21b893730e66b7862246cc28922a0227c3cded0674bf10bd83d5414c",
		},
		{
			name:           "activate_null_replacement",
			argumentFields: join(header("op-activate-new", "activate_replica_capacity"), identity, [][]byte{goLifecycleUUID("")}),
			resultFields: goLifecycleResultFields("op-activate-new", "activate_replica_capacity", vectorResult, "replica",
				vectorReplica, "serving", "active", 0, 0, 0, false, false, false, false, 71, "op-activate-new", true, "online", "", 0, false),
			argumentBytes: 523, argumentDigest: "a0cea0d8c5c4b701e413c009af40bdc1d09a696fb160494fe01f3a45e7a6e1b9",
			resultBytes: 271, resultDigest: "bd3d991093d7ad39d56858eafbab2afb3aa27b7dd224059d6d37474741a5a8a2",
		},
		{
			name:           "activate_nonnull_replacement",
			argumentFields: join(header("op-activate-replacement", "activate_replica_capacity"), identity, [][]byte{goLifecycleUUID(vectorReplaced)}),
			resultFields: goLifecycleResultFields("op-activate-replacement", "activate_replica_capacity", vectorResult, "replica",
				vectorReplica, "serving", "active", 0, 0, 0, false, false, false, false, 71, "op-activate-replacement", true, "online", "", 0, false),
			argumentBytes: 547, argumentDigest: "22cabd95d5d83122cbec4395f2e1b0256190af5dbaff7eb7e14160b3d7a6c5c2",
			resultBytes: 287, resultDigest: "01ae3ed8ce5342cfcf18f2ebe547bbf1c63a1f0b9ea64c1b920669767409ab7f",
		},
		{
			name: "prepare_scale_in",
			argumentFields: join(header("op-scale-in", "prepare_scale_in"), [][]byte{
				goLifecycleUUID(vectorReplica), goLifecycleText(vectorInstance, true), goLifecycleText(vectorRelease(), true)}),
			resultFields: goLifecycleResultFields("op-scale-in", "prepare_scale_in", vectorResult, "replica",
				vectorReplica, "serving", "draining", 0, 0, 0, false, false, false, false, 71, "op-scale-in", true, "online", "", 0, false),
			argumentBytes: 212, argumentDigest: "39c457fd9b3c30904396ce70488a72b21a559649a55f90569c4bc767271ba5b8",
			resultBytes: 256, resultDigest: "775a098df0a51ea8eadc869fe5ff36b8c0e04c811401250b45ace66ad3c52f04",
		},
		{
			name: "finish_scale_in",
			argumentFields: join(header("op-scale-in-finish", "finish_scale_in"), [][]byte{
				goLifecycleUUID(vectorReplica), goLifecycleText(vectorInstance, true), goLifecycleText(vectorRelease(), true),
				goLifecycleText("leave-fixture", true), goLifecycleText("proof-fixture", true)}),
			resultFields: goLifecycleResultFields("op-scale-in-finish", "finish_scale_in", vectorResult, "replica",
				vectorReplica, "serving", "left", 1, 1, 0, true, true, false, true, 71, "op-scale-in-finish", true, "online", "", 0, false),
			argumentBytes: 256, argumentDigest: "dbf2187ecde3122476847d63589c3ff6ab15c72b10477fbf3b51d13ac1c9943a",
			resultBytes: 291, resultDigest: "92cad6bfc24856d63242f115a379671a39724417a8335476564d809bd04eb64a",
		},
		{
			name: "prepare_maintenance_drain",
			argumentFields: join(header("op-maint-drain", "prepare_maintenance_drain"), [][]byte{
				goLifecycleUUID(vectorReplica), goLifecycleText(vectorInstance, true), goLifecycleText(vectorRelease(), true)}),
			resultFields: goLifecycleResultFields("op-maint-drain", "prepare_maintenance_drain", vectorResult, "replica",
				vectorReplica, "maintenance", "draining", 0, 0, 0, false, false, false, false, 71, "op-maint-drain", true, "online", "", 0, false),
			argumentBytes: 224, argumentDigest: "66beb1e1fa2549e66550ba9a9a898b225451293d89020eb6aac2640d0177c9ef",
			resultBytes: 275, resultDigest: "ba98747923b9791adfb3af8a8f057891a952ede94edcbcb660f5ab53b7b6c584",
		},
		{
			name: "begin_replica_termination",
			argumentFields: join(header("op-terminate", "begin_replica_termination"), [][]byte{
				goLifecycleText("request-fixture", true), goLifecycleUUID(vectorReplica),
				goLifecycleText(vectorInstance, true), goLifecycleText(vectorRelease(), true),
				goLifecycleText("readiness_failed", true)}),
			resultFields: goLifecycleResultFields("op-terminate", "begin_replica_termination", vectorResult, "replica",
				vectorReplica, "serving", "terminating", 0, 0, 0, false, false, false, false, 71, "op-terminate", true, "online", "", 0, false),
			argumentBytes: 265, argumentDigest: "9b40c99609c143fa0ddcdd4a43383da128719b027178648a4531a2058b84836b",
			resultBytes: 270, resultDigest: "7cd11ad109ec65a6de4075f6d59786fbeb984ad6d36dc63009d58fb65c2ff2eb",
		},
		{
			name: "begin_wake_serving",
			argumentFields: join(header("op-wake-serving", "begin_wake"), [][]byte{
				goLifecycleText("serving", true), goLifecycleInt8(100, true)}),
			resultFields: goLifecycleResultFields("op-wake-serving", "begin_wake", vectorResult, "capacity",
				"", "", "", 1, 0, 0, true, false, false, true, 71, "op-wake-serving", false, "starting", "closing", 101, true),
			argumentBytes: 113, argumentDigest: "4c2165a2ee539b0c45ba52f4605461547ea966ca12f5ef57f02659ae689a5119",
			resultBytes: 271, resultDigest: "a97a896208cf389d17f85eabead18af5a388df346b618797cabb1a3760226dd9",
		},
		{
			name: "begin_wake_maintenance",
			argumentFields: join(header("op-wake-maintenance", "begin_wake"), [][]byte{
				goLifecycleText("maintenance", true), goLifecycleInt8(100, true)}),
			resultFields: goLifecycleResultFields("op-wake-maintenance", "begin_wake", vectorResult, "capacity",
				"", "", "", 1, 0, 0, true, false, false, true, 71, "op-wake-maintenance", false, "starting", "closing", 101, true),
			argumentBytes: 121, argumentDigest: "6117c2414121a2755b65160441f5146a3a3712eaa983cd1bf469a01162c8840a",
			resultBytes: 279, resultDigest: "95bfcd2e8956df38433cbe7cf3395e94f564c433e5ab2b121a1818566722ae2d",
		},
		{
			name:           "complete_wake",
			argumentFields: join(header("op-wake-complete", "complete_wake"), [][]byte{goLifecycleInt8(101, true)}),
			resultFields: goLifecycleResultFields("op-wake-complete", "complete_wake", vectorResult, "capacity",
				"", "", "", 1, 0, 0, true, false, false, true, 72, "op-wake-complete", true, "online", "open", 102, true),
			argumentBytes: 104, argumentDigest: "c380d0ac76ef9e143cc8d49f54f214509e37bf1ef42596402d264a3821101abb",
			resultBytes: 271, resultDigest: "b7e07a8ca2aaaccfcb247b21b6976c3b77f9ed7c474a5b15eab3f13ccb75973d",
		},
	}
}

func vectorRelease() string { return "sha256:" + strings.Repeat("a", 64) }

// lifecycleTwoNodeFleet drives the accepted path to desired two with two
// active serving replicas and both logical partitions enabled. It leaves the
// controller generation at 4.
func lifecycleTwoNodeFleet(t *testing.T, db *sql.DB) (registrationIdentity, registrationIdentity) {
	t.Helper()
	first := validRegistrationIdentity("1")
	second := validRegistrationIdentity("2")
	lifecycleActivateServing(t, db, 1, "op-initial", first, "")
	if _, err := callLifecycleCapacity(t, db, lifecycleScaleOutExpr(2, "op-scale-out")); err != nil {
		t.Fatal(err)
	}
	lifecycleActivateServing(t, db, 3, "op-scale-out", second, "")
	return first, second
}

// goLifecycleArgumentHex computes the canonical argument digest in Go and
// renders it as a SQL literal, so a fixture can store the exact digest the
// wrapper will recompute.
func goLifecycleArgumentHex(fields ...[]byte) string {
	sum := sha256.Sum256(goLifecycleStream(lifecycleKindArguments, fields...))
	return `decode('` + hex.EncodeToString(sum[:]) + `','hex')`
}

func goLifecycleResultHex(fields [][]byte) string {
	sum := sha256.Sum256(goLifecycleStream(lifecycleKindResult, fields...))
	return `decode('` + hex.EncodeToString(sum[:]) + `','hex')`
}

// goScaleOutArguments and goScaleInArguments repeat the two argument streams a
// fixture needs, in their fixed signature order.
func goScaleOutArguments(operation string, expected int64) [][]byte {
	return [][]byte{
		goLifecycleText(operation, true),
		goLifecycleText("prepare_scale_out", true),
		goLifecycleInt8(expected, true),
	}
}

func goScaleInArguments(operation string, expected int64, id registrationIdentity) [][]byte {
	return [][]byte{
		goLifecycleText(operation, true),
		goLifecycleText("prepare_scale_in", true),
		goLifecycleInt8(expected, true),
		goLifecycleUUID(id.replicaID),
		goLifecycleText(id.instanceID, true),
		goLifecycleText(id.release, true),
	}
}

func lifecycleParentSQL(operation, workflow string) string {
	return fmt.Sprintf(`INSERT INTO public.runtime_lifecycle_operations(operation_id,workflow_kind) VALUES('%s','%s')`, operation, workflow)
}

// lifecycleSinkTargets returns discard destinations matching a projection's
// arity, so a rejected call can still be scanned.
func lifecycleSinkTargets(sink *[16]any, projection string) []any {
	count := strings.Count(projection, ",") + 1
	targets := make([]any, 0, count)
	for index := 0; index < count; index++ {
		sink[index] = new(any)
		targets = append(targets, sink[index])
	}
	return targets
}

type lifecycleHold struct {
	conn *sql.Conn
	pid  int
}

func (h lifecycleHold) commit() error   { return finishRegistrationTx(h.conn) }
func (h lifecycleHold) rollback() error { return rollbackRegistrationTx(h.conn) }

// openLifecycleHolder runs one action inside an open lifecycle write
// transaction and keeps its locks until the caller settles it.
func openLifecycleHolder(t *testing.T, db *sql.DB, expression, projection string) lifecycleHold {
	t.Helper()
	conn, pid := openRegistrationTx(t, db, lifecycleRole)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var sink [16]any
	if err := conn.QueryRowContext(ctx, lifecycleStatement(expression, projection)).Scan(lifecycleSinkTargets(&sink, projection)...); err != nil {
		if rollbackErr := rollbackRegistrationTx(conn); rollbackErr != nil {
			t.Error(rollbackErr)
		}
		t.Fatal(err)
	}
	return lifecycleHold{conn: conn, pid: pid}
}

// raceLifecycleOperation proves the waiter blocks on a real database lock,
// settles the holder, then joins and commits the waiter.
func raceLifecycleOperation(t *testing.T, db *sql.DB, settle func() error, expression, projection string) error {
	t.Helper()
	waiter, waiterPID := openRegistrationTx(t, db, lifecycleRole)
	raceCtx, cancelRace := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelRace()
	done := make(chan error, 1)
	go func() {
		var sink [16]any
		done <- waiter.QueryRowContext(raceCtx, lifecycleStatement(expression, projection)).Scan(lifecycleSinkTargets(&sink, projection)...)
	}()
	waitErr := waitForTransitionCondition(db, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND NOT granted)`, waiterPID)
	settleErr := settle()
	var queryErr error
	select {
	case queryErr = <-done:
	case <-time.After(15 * time.Second):
		cancelRace()
		queryErr = <-done
		if rollbackErr := rollbackRegistrationTx(waiter); rollbackErr != nil {
			t.Error(rollbackErr)
		}
		t.Fatalf("timed out joining lifecycle race: %v", queryErr)
	}
	if waitErr != nil || settleErr != nil {
		if rollbackErr := rollbackRegistrationTx(waiter); rollbackErr != nil {
			t.Error(rollbackErr)
		}
		t.Fatalf("race setup wait=%v settle=%v query=%v", waitErr, settleErr, queryErr)
	}
	if queryErr != nil {
		if rollbackErr := rollbackRegistrationTx(waiter); rollbackErr != nil {
			t.Error(rollbackErr)
		}
		return queryErr
	}
	if err := finishRegistrationTx(waiter); err != nil {
		t.Fatal(err)
	}
	return nil
}

// holdTransitionParent locks one visible public transition row and keeps it,
// so a lifecycle action can be proven never to wait on it.
func holdTransitionParent(t *testing.T, db *sql.DB) func() {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	conn, err := db.Conn(ctx)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, `BEGIN`); err != nil {
		cancel()
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, `SELECT 1 FROM public.public_transitions WHERE state='closing' FOR UPDATE`); err != nil {
		cancel()
		t.Fatal(err)
	}
	return func() {
		defer cancel()
		release, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, rollbackErr := conn.ExecContext(release, `ROLLBACK`); rollbackErr != nil {
			t.Error(rollbackErr)
		}
		if closeErr := conn.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}
}

// lifecycleClockProbe replaces the owner-only lifecycle clock with a fixed
// literal and runs one action, all inside a single owner transaction that is
// always rolled back. Nothing outside this transaction can observe either the
// replacement or the action.
func lifecycleClockProbe(t *testing.T, db *sql.DB, literal, expression, projection string) (capacityAt, recordedAt time.Time, resultErr error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		return capacityAt, recordedAt, err
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_, rollbackErr := conn.ExecContext(cleanup, `ROLLBACK; RESET SESSION AUTHORIZATION`)
		resultErr = errors.Join(resultErr, rollbackErr, conn.Close())
	}()
	for _, statement := range []string{
		`BEGIN`,
		`CREATE OR REPLACE FUNCTION public.runtime_sample_lifecycle_time() RETURNS timestamptz LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog AS $probe$ SELECT '` + literal + `'::timestamptz $probe$`,
		`SET SESSION AUTHORIZATION ` + lifecycleRole,
		`SELECT public.runtime_enter_write()`,
	} {
		if _, err = conn.ExecContext(ctx, statement); err != nil {
			return capacityAt, recordedAt, err
		}
	}
	var sink [16]any
	if err = conn.QueryRowContext(ctx, lifecycleStatement(expression, projection)).Scan(lifecycleSinkTargets(&sink, projection)...); err != nil {
		return capacityAt, recordedAt, err
	}
	if _, err = conn.ExecContext(ctx, `RESET SESSION AUTHORIZATION`); err != nil {
		return capacityAt, recordedAt, err
	}
	err = conn.QueryRowContext(ctx, `SELECT c.updated_at,(SELECT max(s.recorded_at) FROM public.runtime_lifecycle_operation_steps s) FROM public.runtime_capacity c WHERE c.singleton`).Scan(&capacityAt, &recordedAt)
	return capacityAt, recordedAt, err
}
