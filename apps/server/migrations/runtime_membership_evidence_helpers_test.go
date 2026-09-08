package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// membershipEvidenceSignatures lists the three fixed evidence operations with
// their exact argument types, in the accepted signature order, and the
// composite each returns.
var membershipEvidenceSignatures = []struct{ name, signature, result, role string }{
	{"runtime_finish_serving_graceful_leave", "(uuid,text,text,bigint,text)", "runtime_leave_result", membershipAppRole},
	{"runtime_finish_maintenance_graceful_leave", "(uuid,text,text,bigint,text)", "runtime_leave_result", membershipMaintenanceRole},
	{"runtime_record_ec2_termination", "(uuid,text,text,text,text,timestamptz,timestamptz,text)", "runtime_fence_result", membershipProofRole},
}

// membershipEvidenceComposites fixes each returned composite's field order and
// type. Migration 00015 owns both types; migration 00023 must not reshape
// them.
var membershipEvidenceComposites = []struct{ name, attributes string }{
	{
		"runtime_leave_result",
		"replica_id:uuid,state:text,receipt_operation_id:text,controller_generation:bigint," +
			"joined_transition_count:integer,joined_claim_count:integer," +
			"recorded_at:timestamp with time zone,replayed:boolean",
	},
	{
		"runtime_fence_result",
		"replica_id:uuid,state:text,evidence_id:text,reclaimed_claim_count:integer," +
			"recorded_at:timestamp with time zone,replayed:boolean",
	},
}

// membershipProofColumns fixes the two retained proof fields migration 00023
// adds, in the order it adds them, with their exact type and NOT NULL flag.
const membershipProofColumns = "request_id:text:true,reclaimed_claim_count:integer:true"

// membershipProofConstraints fixes the constraints that guard those two
// fields. c is a CHECK and u is a UNIQUE constraint.
var membershipProofConstraints = []struct{ name, kind string }{
	{"runtime_fencing_proofs_reclaimed_claim_count_check", "c"},
	{"runtime_fencing_proofs_request_id_check", "c"},
	{"runtime_fencing_proofs_request_id_key", "u"},
}

// membershipEvidenceHelperSignatures lists the owner-only helpers migration
// 00023 installs. No login role receives EXECUTE on any of them, and the
// fenced claim release helper in particular is reachable only through
// runtime_record_ec2_termination.
var membershipEvidenceHelperSignatures = []struct{ name, signature string }{
	{"runtime_sample_membership_time", "()"},
	{"runtime_membership_validate_replica_input", "(uuid,text,text)"},
	{"runtime_membership_validate_evidence_text", "(text)"},
	{"runtime_membership_validate_generation", "(bigint)"},
	{"runtime_membership_validate_proof_evidence", "(timestamptz,timestamptz,text)"},
	{"runtime_membership_lock_singletons", "()"},
	{"runtime_membership_load_target", "(uuid,text,text)"},
	{"runtime_membership_replica_high", "(public.runtime_replicas)"},
	{"runtime_membership_operation_high", "(text)"},
	{"runtime_membership_evidence_high", "(uuid)"},
	{"runtime_membership_termination_intent", "(uuid)"},
	{"runtime_membership_leave_receipt", "(uuid)"},
	{"runtime_membership_fencing_proof", "(uuid)"},
	{"runtime_membership_assert_generation_room", "(public.runtime_capacity)"},
	{"runtime_membership_advance_capacity", "(text,timestamptz)"},
	{"runtime_membership_lock_live_claims", "(uuid)"},
	{"runtime_membership_owned_transition_count", "(uuid)"},
	{"runtime_membership_prepare_step", "(text,text,text,uuid)"},
	{"runtime_membership_assert_request_identity", "(text,uuid)"},
	{"runtime_membership_assert_evidence_identity", "(text,uuid)"},
	{"runtime_membership_assert_receipt_shape", "(public.runtime_leave_receipts,public.runtime_replicas)"},
	{"runtime_membership_assert_proof_shape", "(public.runtime_fencing_proofs,public.runtime_replicas)"},
	{"runtime_membership_leave_value", "(uuid,text,text,bigint,timestamptz,boolean)"},
	{"runtime_membership_fence_value", "(uuid,text,text,integer,timestamptz,boolean)"},
	{"runtime_membership_record_receipt", "(uuid,text,text,bigint,text,timestamptz)"},
	{"runtime_membership_record_proof", "(uuid,text,text,text,text,timestamptz,timestamptz,text,integer,timestamptz)"},
	{"runtime_membership_set_state", "(uuid,text,timestamptz)"},
	{"runtime_membership_finish_graceful_leave", "(text,text,text,text,uuid,text,text,bigint,text)"},
	{"runtime_release_fenced_replica_claims", "(uuid,timestamptz)"},
}

// membershipEvidenceVolatility pins each installed function's declared
// volatility. Anything that samples, locks or reads database state is STABLE
// or VOLATILE; only argument-pure scalar validation stays IMMUTABLE.
var membershipEvidenceVolatility = map[string]string{
	"runtime_sample_membership_time":              "v",
	"runtime_membership_validate_replica_input":   "i",
	"runtime_membership_validate_evidence_text":   "i",
	"runtime_membership_validate_generation":      "i",
	"runtime_membership_validate_proof_evidence":  "s",
	"runtime_membership_lock_singletons":          "v",
	"runtime_membership_load_target":              "v",
	"runtime_membership_replica_high":             "s",
	"runtime_membership_operation_high":           "s",
	"runtime_membership_evidence_high":            "s",
	"runtime_membership_termination_intent":       "v",
	"runtime_membership_leave_receipt":            "v",
	"runtime_membership_fencing_proof":            "v",
	"runtime_membership_assert_generation_room":   "i",
	"runtime_membership_advance_capacity":         "v",
	"runtime_membership_lock_live_claims":         "v",
	"runtime_membership_owned_transition_count":   "s",
	"runtime_membership_prepare_step":             "s",
	"runtime_membership_assert_request_identity":  "s",
	"runtime_membership_assert_evidence_identity": "s",
	"runtime_membership_assert_receipt_shape":     "s",
	"runtime_membership_assert_proof_shape":       "s",
	"runtime_membership_leave_value":              "s",
	"runtime_membership_fence_value":              "s",
	"runtime_membership_record_receipt":           "v",
	"runtime_membership_record_proof":             "v",
	"runtime_membership_set_state":                "v",
	"runtime_membership_finish_graceful_leave":    "v",
	"runtime_release_fenced_replica_claims":       "v",
	"runtime_finish_serving_graceful_leave":       "v",
	"runtime_finish_maintenance_graceful_leave":   "v",
	"runtime_record_ec2_termination":              "v",
}

const (
	membershipAppRole         = "aboutme_app"
	membershipMaintenanceRole = "aboutme_maintenance"
	membershipProofRole       = "aboutme_fencing_proof"
	membershipNilUUID         = "00000000-0000-0000-0000-000000000000"
	membershipLeaveProjection = `(r).replica_id::text,(r).state,(r).receipt_operation_id,(r).controller_generation,` +
		`(r).joined_transition_count,(r).joined_claim_count,(r).recorded_at,(r).replayed`
	membershipFenceProjection = `(r).replica_id::text,(r).state,(r).evidence_id,(r).reclaimed_claim_count,` +
		`(r).recorded_at,(r).replayed`
	// The external EC2 times are deliberately far in the future: they are
	// stored raw and must never drag database ordering forward.
	membershipRequestedAt = `'2030-01-01 00:00:00+00'::timestamptz`
	membershipObservedAt  = `'2030-01-01 00:00:05+00'::timestamptz`
)

type membershipLeaveResult struct {
	replicaID, state, operationID string
	controllerGeneration          int64
	transitionCount, claimCount   int32
	recordedAt                    time.Time
	replayed                      bool
}

type membershipFenceResult struct {
	replicaID, state, evidenceID string
	reclaimedClaimCount          int32
	recordedAt                   time.Time
	replayed                     bool
}

func membershipEvidenceDB(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	db := newMigratedTestDatabase(t, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return db, ctx
}

func membershipLeaveExpr(function string, id registrationIdentity, expected int64, operation string) string {
	return fmt.Sprintf(`public.%s('%s','%s','%s',%d,'%s')`, function, id.replicaID, id.instanceID, id.release, expected, operation)
}

func membershipServingLeaveExpr(id registrationIdentity, expected int64, operation string) string {
	return membershipLeaveExpr("runtime_finish_serving_graceful_leave", id, expected, operation)
}

func membershipMaintenanceLeaveExpr(id registrationIdentity, expected int64, operation string) string {
	return membershipLeaveExpr("runtime_finish_maintenance_graceful_leave", id, expected, operation)
}

// membershipProofExpr renders one proof call. The two time arguments are raw
// SQL expressions so a case can supply NULL or an inverted order.
func membershipProofExpr(id registrationIdentity, request, evidence, requestedAt, observedAt, observedState string) string {
	return fmt.Sprintf(`public.runtime_record_ec2_termination('%s','%s','%s','%s','%s',%s,%s,'%s')`,
		id.replicaID, id.instanceID, id.release, request, evidence, requestedAt, observedAt, observedState)
}

func membershipValidProofExpr(id registrationIdentity, request, evidence string) string {
	return membershipProofExpr(id, request, evidence, membershipRequestedAt, membershipObservedAt, "terminated")
}

func callMembershipLeave(t *testing.T, db *sql.DB, role, expression string) (membershipLeaveResult, error) {
	t.Helper()
	var r membershipLeaveResult
	err := lifecycleCall(t, db, role, expression, membershipLeaveProjection,
		&r.replicaID, &r.state, &r.operationID, &r.controllerGeneration, &r.transitionCount,
		&r.claimCount, &r.recordedAt, &r.replayed)
	return r, err
}

func callMembershipProof(t *testing.T, db *sql.DB, expression string) (membershipFenceResult, error) {
	t.Helper()
	var r membershipFenceResult
	err := lifecycleCall(t, db, membershipProofRole, expression, membershipFenceProjection,
		&r.replicaID, &r.state, &r.evidenceID, &r.reclaimedClaimCount, &r.recordedAt, &r.replayed)
	return r, err
}

func mustMembershipLeave(t *testing.T, db *sql.DB, role, expression string) membershipLeaveResult {
	t.Helper()
	result, err := callMembershipLeave(t, db, role, expression)
	if err != nil {
		t.Fatalf("%s: %v", expression, err)
	}
	return result
}

func mustMembershipProof(t *testing.T, db *sql.DB, expression string) membershipFenceResult {
	t.Helper()
	result, err := callMembershipProof(t, db, expression)
	if err != nil {
		t.Fatalf("%s: %v", expression, err)
	}
	return result
}

func requireMembershipLeaveError(t *testing.T, db *sql.DB, role, expression, code string) {
	t.Helper()
	_, err := callMembershipLeave(t, db, role, expression)
	requireRegistrationError(t, err, code)
}

func requireMembershipProofError(t *testing.T, db *sql.DB, expression, code string) {
	t.Helper()
	_, err := callMembershipProof(t, db, expression)
	requireRegistrationError(t, err, code)
}

// membershipLeaveErrorOf and membershipProofErrorOf return the rejected call's
// database error so a case can prove the fixed message discloses nothing.
func membershipLeaveErrorOf(t *testing.T, db *sql.DB, role, expression, code string) *pgconn.PgError {
	t.Helper()
	_, err := callMembershipLeave(t, db, role, expression)
	return membershipErrorOf(t, err, code)
}

func membershipProofErrorOf(t *testing.T, db *sql.DB, expression, code string) *pgconn.PgError {
	t.Helper()
	_, err := callMembershipProof(t, db, expression)
	return membershipErrorOf(t, err, code)
}

func membershipErrorOf(t *testing.T, err error, code string) *pgconn.PgError {
	t.Helper()
	requireRegistrationError(t, err, code)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("membership evidence error is not a database error: %v", err)
	}
	return pgErr
}

// membershipServingDrained drives the accepted two-node fleet and the real
// prepare-scale-in action, leaving the first replica draining with a stored
// prepare step whose result generation is 5.
func membershipServingDrained(t *testing.T, db *sql.DB) (first, second registrationIdentity) {
	t.Helper()
	first, second = lifecycleTwoNodeFleet(t, db)
	drained, err := callLifecycleReplica(t, db, lifecycleScaleInExpr(4, "op-scale-in", first))
	if err != nil {
		t.Fatal(err)
	}
	if drained.state != "draining" || drained.controllerGeneration != 5 {
		t.Fatalf("prepare scale in=%+v", drained)
	}
	return first, second
}

// membershipMaintenanceDrained drives the accepted maintenance wake, real
// activation and real prepare-maintenance-drain, leaving the maintenance
// replica draining with a stored prepare step whose result generation is 5.
func membershipMaintenanceDrained(t *testing.T, db *sql.DB) registrationIdentity {
	t.Helper()
	node := validRegistrationIdentity("3")
	lifecycleWrite(t, db, append(lifecycleWakeFixture("op-wake", "maintenance_wake"), lifecycleSetControllerGenerationSQL(3))...)
	lifecycleRegisterReady(t, db, "maintenance", node)
	if _, err := callLifecycleReplica(t, db, lifecycleActivateExpr(3, "op-wake", node, "")); err != nil {
		t.Fatal(err)
	}
	drained, err := callLifecycleReplica(t, db, lifecycleDrainExpr(4, "op-wake", node))
	if err != nil {
		t.Fatal(err)
	}
	if drained.state != "draining" || drained.controllerGeneration != 5 {
		t.Fatalf("prepare maintenance drain=%+v", drained)
	}
	return node
}

// membershipProofRow reads the stored proof for one replica.
type membershipProofRow struct {
	exists                               bool
	instanceID, releaseDigest, adapter   string
	requestID, evidenceID, observedState string
	requestedAt, observedTerminatedAt    time.Time
	recordedAt                           time.Time
	reclaimedClaimCount                  int32
}

func membershipProof(t *testing.T, db *sql.DB, replicaID string) membershipProofRow {
	t.Helper()
	var row membershipProofRow
	err := db.QueryRowContext(context.Background(),
		`SELECT instance_id,release_digest,adapter,request_id,evidence_id,requested_at,observed_terminated_at,recorded_at,observed_state,reclaimed_claim_count FROM public.runtime_fencing_proofs WHERE replica_id=$1`, replicaID).
		Scan(&row.instanceID, &row.releaseDigest, &row.adapter, &row.requestID, &row.evidenceID,
			&row.requestedAt, &row.observedTerminatedAt, &row.recordedAt, &row.observedState, &row.reclaimedClaimCount)
	if errors.Is(err, sql.ErrNoRows) {
		return row
	}
	if err != nil {
		t.Fatal(err)
	}
	row.exists = true
	return row
}

// membershipReceiptRow reads the stored leave receipt for one replica.
type membershipReceiptRow struct {
	exists                                 bool
	instanceID, releaseDigest, operationID string
	controllerGeneration                   int64
	transitionCount, claimCount            int32
	recordedAt                             time.Time
}

func membershipReceipt(t *testing.T, db *sql.DB, replicaID string) membershipReceiptRow {
	t.Helper()
	var row membershipReceiptRow
	err := db.QueryRowContext(context.Background(),
		`SELECT instance_id,release_digest,operation_id,controller_generation,joined_transition_count,joined_claim_count,recorded_at FROM public.runtime_leave_receipts WHERE replica_id=$1`, replicaID).
		Scan(&row.instanceID, &row.releaseDigest, &row.operationID, &row.controllerGeneration,
			&row.transitionCount, &row.claimCount, &row.recordedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return row
	}
	if err != nil {
		t.Fatal(err)
	}
	row.exists = true
	return row
}

// membershipClaimStates returns one replica's claim parents with their state
// and release reason, ordered by claim UUID.
func membershipClaimStates(t *testing.T, db *sql.DB, replicaID string) []string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(),
		`SELECT r.claim_id::text||':'||r.state||':'||COALESCE(r.release_reason,'-')||':'||COALESCE((SELECT string_agg(c.state||'/'||c.allocation_ordinal,',' ORDER BY c.request_ordinal) FROM public.shared_claim_scopes c WHERE c.claim_id=r.claim_id),'-') FROM public.shared_claim_requests r WHERE r.replica_id=$1 ORDER BY r.claim_id`, replicaID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	states := []string{}
	for rows.Next() {
		var value string
		if scanErr := rows.Scan(&value); scanErr != nil {
			t.Fatal(scanErr)
		}
		states = append(states, value)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		t.Fatal(rowsErr)
	}
	return states
}

// membershipLiveClaims builds an app claim of every accepted single-scope
// policy plus one two-scope SSE claim for the given replica, so a fence has
// several policies and scopes to release at once.
func membershipLiveClaims(t *testing.T, db *sql.DB, replicaID string, index int) []string {
	t.Helper()
	renderWork := claimUUID("77777777", index)
	claims := []string{
		claimUUID("41111111", index), claimUUID("42222222", index),
		claimUUID("43333333", index), claimUUID("44444444", index),
	}
	requireClaimOutcome(t, db, membershipAppRole,
		claimSingleExpression(claims[0], "render.global_claim", replicaID, "'"+renderWork+"'", "global", claimHex(index)), "running")
	requireClaimOutcome(t, db, membershipAppRole,
		claimSingleExpression(claims[1], "password.hash", replicaID, "NULL", "global", claimHex(index)), "running")
	requireClaimOutcome(t, db, membershipAppRole,
		claimSingleExpression(claims[2], "mcp.user_concurrent", replicaID, "NULL", "user", claimHex(index)), "running")
	account := claimHex(index + 512)
	requireClaimOutcome(t, db, membershipAppRole,
		claimSSEExpression(claims[3], replicaID, claimHex(index+256), &account), "running")
	return claims
}

// membershipWaitingRenderClaim adds one queued render claim so a fence has a
// waiting parent as well as running ones.
func membershipWaitingRenderClaim(t *testing.T, db *sql.DB, replicaID string, index int) string {
	t.Helper()
	claimID := claimUUID("45555555", index)
	requireClaimOutcome(t, db, membershipAppRole,
		claimSingleExpression(claimID, "render.global_claim", replicaID, "'"+claimUUID("78888888", index)+"'", "global", claimHex(index)), "waiting")
	return claimID
}

// membershipLegacyProofSQL owner-inserts a proof shaped exactly as migration
// 00015 defined it, without the two fields migration 00023 adds.
func membershipLegacyProofSQL(id registrationIdentity, evidence string) string {
	// Deliberately the version-22 column list. This row is inserted before
	// migration 23 runs, so it must not follow lifecycleProofSQL, which now
	// carries the two columns migration 23 adds.
	return fmt.Sprintf(`INSERT INTO public.runtime_fencing_proofs(replica_id,instance_id,release_digest,adapter,evidence_id,requested_at,observed_terminated_at,observed_state) VALUES('%s','%s','%s','ec2_terminated_v1','%s',clock_timestamp(),clock_timestamp(),'terminated')`,
		id.replicaID, id.instanceID, id.release, evidence)
}

// membershipCountsStatement reads every populated table this migration must
// preserve plus both generations, so an upgrade can be proven lossless.
const membershipCountsStatement = `SELECT s.generation,c.generation,c.controller_generation,` +
	`(SELECT count(*) FROM public.runtime_replicas),(SELECT count(*) FROM public.runtime_replica_tasks),` +
	`(SELECT count(*) FROM public.public_transitions),(SELECT count(*) FROM public.public_transition_acks),` +
	`(SELECT count(*) FROM public.shared_claim_requests),(SELECT count(*) FROM public.shared_claim_scopes),` +
	`(SELECT count(*) FROM public.shared_claim_scope_summaries),(SELECT count(*) FROM public.shared_rate_policies),` +
	`(SELECT count(*) FROM public.shared_rate_partitions),(SELECT count(*) FROM public.shared_rate_buckets),` +
	`(SELECT count(*) FROM public.shared_admission_attempts),(SELECT count(*) FROM public.runtime_lifecycle_operations),` +
	`(SELECT count(*) FROM public.runtime_lifecycle_operation_steps),(SELECT count(*) FROM public.users),` +
	`(SELECT count(*) FROM public.runtime_fencing_proofs),(SELECT count(*) FROM public.runtime_leave_receipts),` +
	`(SELECT count(*) FROM public.runtime_termination_intents) ` +
	`FROM public.runtime_write_state s CROSS JOIN public.runtime_capacity c WHERE s.singleton AND c.singleton`

type membershipCounts [20]int64

func scanMembershipCounts(ctx context.Context, t *testing.T, db *sql.DB) membershipCounts {
	t.Helper()
	var counts membershipCounts
	targets := make([]any, len(counts))
	for index := range counts {
		targets[index] = &counts[index]
	}
	if err := db.QueryRowContext(ctx, membershipCountsStatement).Scan(targets...); err != nil {
		t.Fatal(err)
	}
	return counts
}

// membershipOwnerExec runs owner statements through the migrator write path.
func membershipOwnerExec(t *testing.T, db *sql.DB, statements ...string) {
	t.Helper()
	if err := membershipWrite(t, db, statements...); err != nil {
		t.Fatal(err)
	}
}

// membershipHold keeps one evidence transaction open so a race can prove a
// real database lock wait.
type membershipHold struct {
	conn *sql.Conn
	pid  int
}

func (h membershipHold) commit() error { return finishRegistrationTx(h.conn) }

// openMembershipHolder runs one evidence call inside an open write
// transaction and keeps its locks until the caller settles it.
func openMembershipHolder(t *testing.T, db *sql.DB, role, expression, projection string) membershipHold {
	t.Helper()
	conn, pid := openRegistrationTx(t, db, role)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var sink [16]any
	if err := conn.QueryRowContext(ctx, lifecycleStatement(expression, projection)).Scan(lifecycleSinkTargets(&sink, projection)...); err != nil {
		if rollbackErr := rollbackRegistrationTx(conn); rollbackErr != nil {
			t.Error(rollbackErr)
		}
		t.Fatal(err)
	}
	return membershipHold{conn: conn, pid: pid}
}

// raceMembershipOperation starts one evidence or claim call on a fresh
// transaction, proves it blocks on a real database lock, settles the holder,
// then joins and commits the waiter.
func raceMembershipOperation(t *testing.T, db *sql.DB, settle func() error, role, expression, projection string) error {
	t.Helper()
	waiter, waiterPID := openRegistrationTx(t, db, role)
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
		t.Fatalf("timed out joining membership evidence race: %v", queryErr)
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

// membershipClockProbe replaces the owner-only membership clock with a fixed
// literal and runs one evidence call, all inside a single owner transaction
// that is always rolled back.
func membershipClockProbe(t *testing.T, db *sql.DB, literal, role, expression, projection string) (capacityAt, recordedAt time.Time, resultErr error) {
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
		`CREATE OR REPLACE FUNCTION public.runtime_sample_membership_time() RETURNS timestamptz LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog AS $probe$ SELECT '` + literal + `'::timestamptz $probe$`,
		`SET SESSION AUTHORIZATION ` + role,
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
	err = conn.QueryRowContext(ctx, `SELECT c.updated_at,GREATEST((SELECT max(r.recorded_at) FROM public.runtime_leave_receipts r),(SELECT max(f.recorded_at) FROM public.runtime_fencing_proofs f)) FROM public.runtime_capacity c WHERE c.singleton`).Scan(&capacityAt, &recordedAt)
	return capacityAt, recordedAt, err
}

// membershipQueryNames maps each owned sqlc query to the single login role
// that may execute the function it names.
var membershipQueryNames = map[string]string{
	"RuntimeFinishServingGracefulLeave":     membershipAppRole,
	"RuntimeFinishMaintenanceGracefulLeave": membershipMaintenanceRole,
	"RuntimeRecordEC2Termination":           membershipProofRole,
}

// membershipEvidenceNames returns every function name migration 00023
// installs, helpers first.
func membershipEvidenceNames() []string {
	names := make([]string, 0, len(membershipEvidenceHelperSignatures)+len(membershipEvidenceSignatures))
	for _, helper := range membershipEvidenceHelperSignatures {
		names = append(names, helper.name)
	}
	for _, operation := range membershipEvidenceSignatures {
		names = append(names, operation.name)
	}
	return names
}

// membershipRoles lists every real login role the grant matrix must cover.
var membershipRoles = []string{
	"aboutme_app", "aboutme_maintenance", "aboutme_lifecycle_command",
	"aboutme_fencing_proof", "aboutme_restore_verify", "aboutme_migrator",
}

// membershipLongText builds an over-length printable identifier.
func membershipLongText() string { return strings.Repeat("x", 129) }

// membershipClaimDigest returns one stored claim's request digest as hex, so a
// test can call the accepted claim functions with an exact request identity.
func membershipClaimDigest(t *testing.T, db *sql.DB, claimID string) string {
	t.Helper()
	var digest string
	if err := db.QueryRowContext(context.Background(),
		`SELECT encode(request_digest,'hex') FROM public.shared_claim_requests WHERE claim_id=$1`, claimID).Scan(&digest); err != nil {
		t.Fatal(err)
	}
	return digest
}

// membershipTransitionFixture builds one closing public transition initiated
// by an existing replica, with the accepted two-target set whose digest is the
// shared published digest, plus one required participant.
func membershipTransitionFixture(initiator registrationIdentity, transition, participant string) []string {
	return []string{
		fmt.Sprintf(`INSERT INTO public.public_transitions(transition_id,initiator_replica_id,initiator_instance_id,initiator_release_digest,operation,state,created_at,deadline_at,target_digest) VALUES('%s','%s','%s','%s','resume_publication','closing',transaction_timestamp(),transaction_timestamp()+interval '5 seconds',decode('%s','hex'))`,
			transition, initiator.replicaID, initiator.instanceID, initiator.release, publishedDigest),
		fmt.Sprintf(`INSERT INTO public.public_transition_targets(transition_id,ordinal,kind,resume_id,expected_generation,class) VALUES('%s',0,'discovery',NULL,7,'revoking'),('%s',1,'resume','00112233-4455-6677-8899-aabbccddeeff',42,'non_draining')`,
			transition, transition),
		fmt.Sprintf(`INSERT INTO public.public_transition_replicas(transition_id,replica_id,snapshot_state,instance_id,release_digest,target_digest) SELECT '%s',r.replica_id,'required',r.instance_id,r.release_digest,decode('%s','hex') FROM public.runtime_replicas r WHERE r.replica_id='%s'`,
			transition, publishedDigest, participant),
	}
}

// membershipUnresolveTransitionSQL moves one closing transition to the
// unresolved terminal state through the accepted parent shape.
func membershipUnresolveTransitionSQL(transition string) string {
	return fmt.Sprintf(`UPDATE public.public_transitions SET state='unresolved',terminal_at=clock_timestamp(),terminal_error_code='recovery_evidence_conflict' WHERE transition_id='%s'`, transition)
}

// membershipTransitionState reads one transition's durable state and terminal
// fields, so a proof can be proven to leave it untouched.
func membershipTransitionState(t *testing.T, db *sql.DB, transition string) string {
	t.Helper()
	var state string
	if err := db.QueryRowContext(context.Background(),
		`SELECT state||':'||COALESCE(terminal_error_code,'-')||':'||COALESCE(recovery_fencing_evidence_id,'-')||':'||(terminal_at IS NULL)::text FROM public.public_transitions WHERE transition_id=$1`, transition).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}

// membershipIntentSQL owner-inserts one termination intent for a replica in
// the state its reason requires.
func membershipIntentSQL(id registrationIdentity, request, reason string) string {
	return fmt.Sprintf(`INSERT INTO public.runtime_termination_intents(replica_id,instance_id,release_digest,request_id,reason,requested_at) VALUES('%s','%s','%s','%s','%s',clock_timestamp())`,
		id.replicaID, id.instanceID, id.release, request, reason)
}

// membershipReceiptSQL owner-inserts one leave receipt directly, so a fixture
// can build a receipt that contradicts its member row.
func membershipReceiptSQL(id registrationIdentity, generation int64, operation string) string {
	return fmt.Sprintf(`INSERT INTO public.runtime_leave_receipts(replica_id,instance_id,release_digest,controller_generation,operation_id,joined_transition_count,joined_claim_count) VALUES('%s','%s','%s',%d,'%s',0,0)`,
		id.replicaID, id.instanceID, id.release, generation, operation)
}

// membershipCapacityFields reads the capacity fields an evidence operation
// must leave alone alongside the generation it advances.
type membershipCapacityFields struct {
	desired                          int16
	generation, controllerGeneration int64
	controllerOperation, updatedBy   string
	phase                            string
	admission                        bool
}

func membershipCapacity(t *testing.T, db *sql.DB) membershipCapacityFields {
	t.Helper()
	state := lifecycleCapacity(t, db)
	return membershipCapacityFields{
		desired: state.desired, generation: state.generation, controllerGeneration: state.controllerGeneration,
		controllerOperation: state.controllerOperation, updatedBy: state.updatedBy,
		phase: state.phase, admission: state.admission,
	}
}
