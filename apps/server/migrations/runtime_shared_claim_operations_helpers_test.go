package migrations

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type claimOperationResult struct {
	outcome, claimID                            string
	policy, replica, work, state, releaseReason sql.NullString
	admitted, deadline, released                sql.NullTime
	digest, scope1Digest, scope2Digest          []byte
	scopeCount                                  sql.NullInt16
	scope1Kind, scope2Kind                      sql.NullString
	allocation1, allocation2                    sql.NullInt64
}

const claimResultProjection = `(r).outcome,(r).claim_id::text,(r).policy_id,(r).replica_id::text,(r).work_id::text,(r).state,(r).admitted_at,(r).deadline_at,(r).released_at,(r).release_reason,(r).request_digest,(r).scope_count,(r).scope_1_kind,(r).scope_1_digest,(r).scope_1_allocation_ordinal,(r).scope_2_kind,(r).scope_2_digest,(r).scope_2_allocation_ordinal`

var claimTriggerNames = []string{
	"shared_claim_requests_validate", "shared_claim_scopes_validate", "shared_claim_scope_summaries_validate",
	"shared_claim_requests_complete", "shared_claim_scopes_request_complete", "shared_claim_scope_summaries_exact", "shared_claim_scopes_summary_exact",
}

var claimTriggerTables = map[string]string{
	"shared_claim_requests_validate":        "shared_claim_requests",
	"shared_claim_scopes_validate":          "shared_claim_scopes",
	"shared_claim_scope_summaries_validate": "shared_claim_scope_summaries",
	"shared_claim_requests_complete":        "shared_claim_requests",
	"shared_claim_scopes_request_complete":  "shared_claim_scopes",
	"shared_claim_scope_summaries_exact":    "shared_claim_scope_summaries",
	"shared_claim_scopes_summary_exact":     "shared_claim_scopes",
}

func claimHex(value int) string {
	return fmt.Sprintf("%064x", value)
}

func claimUUID(prefix string, index int) string {
	return fmt.Sprintf("%s-0000-4000-8000-%012x", prefix, index)
}

func claimReplicaIdentity(replicaID, suffix string) registrationIdentity {
	id := validRegistrationIdentity(suffix)
	id.replicaID = replicaID
	return id
}

func activeClaimReplica(t *testing.T, db *sql.DB, id registrationIdentity, kind string) {
	t.Helper()
	fixture := []string{
		fmt.Sprintf(`INSERT INTO public.runtime_replicas(replica_id,replica_kind,instance_id,container_instance_arn,caddy_task_arn,go_task_arn,nuxt_task_arn,release_digest,state,joined_at,join_ready_at,activated_at) VALUES('%s','%s','%s','%s','%s','%s','%s','%s','active',clock_timestamp(),clock_timestamp(),clock_timestamp())`, id.replicaID, kind, id.instanceID, id.containerARN, id.caddyARN, id.goARN, id.nuxtARN, id.release),
		fmt.Sprintf(`INSERT INTO public.runtime_replica_tasks(task_arn,replica_id,task_role) VALUES('%s','%s','caddy'),('%s','%s','go'),('%s','%s','nuxt')`, id.caddyARN, id.replicaID, id.goARN, id.replicaID, id.nuxtARN, id.replicaID),
	}
	if err := membershipWrite(t, db, fixture...); err != nil {
		t.Fatal(err)
	}
}

func claimSingleExpression(claimID, policy, replica, work, kind, digest string) string {
	return fmt.Sprintf(`public.runtime_acquire_single_claim('%s','%s','%s',%s,'%s',decode('%s','hex'))`, claimID, policy, replica, work, kind, digest)
}

func claimSSEExpression(claimID, replica, ipHex string, accountHex *string) string {
	account := "NULL::bytea"
	if accountHex != nil {
		account = fmt.Sprintf(`decode('%s','hex')`, *accountHex)
	}
	return fmt.Sprintf(`public.runtime_acquire_sse_claim('%s','%s',decode('%s','hex'),%s)`, claimID, replica, ipHex, account)
}

func claimResolveExpression(claimID, replica, digestHex string) string {
	return fmt.Sprintf(`public.runtime_resolve_claim('%s','%s',decode('%s','hex'))`, claimID, replica, digestHex)
}

func claimPromoteExpression(claimID, replica, digestHex string) string {
	return fmt.Sprintf(`public.runtime_promote_claim('%s','%s',decode('%s','hex'))`, claimID, replica, digestHex)
}

func claimReleaseExpression(claimID, replica, digestHex, reason string) string {
	return fmt.Sprintf(`public.runtime_release_claim('%s','%s',decode('%s','hex'),'%s')`, claimID, replica, digestHex, reason)
}

func scanClaimResult(ctx context.Context, conn *sql.Conn, expression string) (result claimOperationResult, err error) {
	statement := `WITH result AS MATERIALIZED (SELECT ` + expression + ` AS r) SELECT ` + claimResultProjection + ` FROM result`
	err = conn.QueryRowContext(ctx, statement).Scan(&result.outcome, &result.claimID, &result.policy, &result.replica, &result.work, &result.state, &result.admitted, &result.deadline, &result.released, &result.releaseReason, &result.digest, &result.scopeCount, &result.scope1Kind, &result.scope1Digest, &result.allocation1, &result.scope2Kind, &result.scope2Digest, &result.allocation2)
	return result, err
}

func callClaimOperation(t *testing.T, db *sql.DB, role, expression string) (result claimOperationResult, resultErr error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		return result, err
	}
	authenticated, txActive := false, false
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if txActive {
			_, e := conn.ExecContext(cleanup, `ROLLBACK`)
			resultErr = errors.Join(resultErr, e)
		}
		if authenticated {
			_, e := conn.ExecContext(cleanup, `RESET SESSION AUTHORIZATION`)
			resultErr = errors.Join(resultErr, e)
		}
		resultErr = errors.Join(resultErr, conn.Close())
	}()
	if _, err = conn.ExecContext(ctx, `SET SESSION AUTHORIZATION `+role); err != nil {
		return result, err
	}
	authenticated = true
	if _, err = conn.ExecContext(ctx, `BEGIN; SELECT public.runtime_enter_write()`); err != nil {
		return result, err
	}
	txActive = true
	if result, err = scanClaimResult(ctx, conn, expression); err != nil {
		return result, err
	}
	if _, err = conn.ExecContext(ctx, `SELECT public.runtime_finish_write(); COMMIT`); err != nil {
		return result, err
	}
	txActive = false
	return result, nil
}

func mustClaim(t *testing.T, db *sql.DB, role, expression string) claimOperationResult {
	t.Helper()
	result, err := callClaimOperation(t, db, role, expression)
	if err != nil {
		t.Fatalf("%s: %v", expression, err)
	}
	return result
}

func requireClaimOutcome(t *testing.T, db *sql.DB, role, expression, outcome string) claimOperationResult {
	t.Helper()
	result := mustClaim(t, db, role, expression)
	if result.outcome != outcome {
		t.Fatalf("%s outcome=%q want=%q result=%+v", expression, result.outcome, outcome, result)
	}
	return result
}

func requireClaimError(t *testing.T, db *sql.DB, role, expression, code string) {
	t.Helper()
	_, err := callClaimOperation(t, db, role, expression)
	requireRegistrationError(t, err, code)
}

func claimErrorOf(t *testing.T, db *sql.DB, role, expression, code string) *pgconn.PgError {
	t.Helper()
	_, err := callClaimOperation(t, db, role, expression)
	requireRegistrationError(t, err, code)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("claim error is not a database error: %v", err)
	}
	return pgErr
}

func callClaimGC(t *testing.T, db *sql.DB) (claims, summaries int, resultErr error) {
	t.Helper()
	return callClaimGCWithCommit(t, db, true)
}

func callClaimGCWithCommit(t *testing.T, db *sql.DB, commit bool) (claims, summaries int, resultErr error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		return 0, 0, err
	}
	active := false
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if active {
			_, e := conn.ExecContext(cleanup, `ROLLBACK`)
			resultErr = errors.Join(resultErr, e)
		}
		_, e := conn.ExecContext(cleanup, `RESET SESSION AUTHORIZATION`)
		resultErr = errors.Join(resultErr, e, conn.Close())
	}()
	if _, err = conn.ExecContext(ctx, `SET SESSION AUTHORIZATION aboutme_maintenance; BEGIN; SELECT public.runtime_enter_write()`); err != nil {
		return 0, 0, err
	}
	active = true
	if err = conn.QueryRowContext(ctx, `SELECT * FROM public.runtime_gc_released_claim_receipts()`).Scan(&claims, &summaries); err != nil {
		return 0, 0, err
	}
	if !commit {
		return claims, summaries, nil
	}
	if _, err = conn.ExecContext(ctx, `SELECT public.runtime_finish_write(); COMMIT`); err != nil {
		return 0, 0, err
	}
	active = false
	return claims, summaries, nil
}

type claimTableCounts struct {
	parents, scopes, summaries int
}

func countClaimTables(t *testing.T, db *sql.DB) claimTableCounts {
	t.Helper()
	var counts claimTableCounts
	if err := db.QueryRowContext(context.Background(), `SELECT (SELECT count(*) FROM public.shared_claim_requests),(SELECT count(*) FROM public.shared_claim_scopes),(SELECT count(*) FROM public.shared_claim_scope_summaries)`).Scan(&counts.parents, &counts.scopes, &counts.summaries); err != nil {
		t.Fatal(err)
	}
	return counts
}

type claimSummaryState struct {
	exists           bool
	running, waiting int
	nextOrdinal      int64
}

func claimSummary(t *testing.T, db *sql.DB, policy, kind, digestHex string) claimSummaryState {
	t.Helper()
	var state claimSummaryState
	err := db.QueryRowContext(context.Background(), `SELECT running_count,waiting_count,next_ordinal FROM public.shared_claim_scope_summaries WHERE policy_id=$1 AND scope_kind=$2 AND scope_digest=decode($3,'hex')`, policy, kind, digestHex).Scan(&state.running, &state.waiting, &state.nextOrdinal)
	if errors.Is(err, sql.ErrNoRows) {
		return state
	}
	if err != nil {
		t.Fatal(err)
	}
	state.exists = true
	return state
}

func requireClaimSummary(t *testing.T, db *sql.DB, policy, kind, digestHex string, running, waiting int) {
	t.Helper()
	state := claimSummary(t, db, policy, kind, digestHex)
	if !state.exists || state.running != running || state.waiting != waiting {
		t.Fatalf("summary %s/%s exists=%t running=%d waiting=%d want %d/%d", policy, kind, state.exists, state.running, state.waiting, running, waiting)
	}
}

func claimTriggerStatements(enable bool) []string {
	verb := "DISABLE"
	if enable {
		verb = "ENABLE"
	}
	statements := make([]string, 0, len(claimTriggerNames))
	for _, name := range claimTriggerNames {
		statements = append(statements, fmt.Sprintf(`ALTER TABLE public.%s %s TRIGGER %s`, claimTriggerTables[name], verb, name))
	}
	return statements
}

func corruptClaimRows(t *testing.T, db *sql.DB, statements ...string) {
	t.Helper()
	if err := membershipWrite(t, db, claimTriggerStatements(false)...); err != nil {
		t.Fatal(err)
	}
	mutationErr := membershipWrite(t, db, statements...)
	if err := membershipWrite(t, db, claimTriggerStatements(true)...); err != nil {
		t.Fatal(err)
	}
	if mutationErr != nil {
		t.Fatal(mutationErr)
	}
}

func backdateClaim(t *testing.T, db *sql.DB, claimID string, seconds int) {
	t.Helper()
	corruptClaimRows(t, db, fmt.Sprintf(`UPDATE public.shared_claim_requests SET admitted_at=admitted_at-interval '%d seconds',deadline_at=deadline_at-interval '%d seconds' WHERE claim_id='%s'`, seconds, seconds, claimID))
}

func releasedReceiptFixture(claimID, replicaID, policy, kind, digestHex, releasedAgo string, allocation int) []string {
	digestSQL := fmt.Sprintf(`decode('%s','hex')`, digestHex)
	var workID *string
	workValue, deadline := "NULL", "NULL"
	if policy == "render.global_claim" {
		value := claimUUID("77777777", allocation)
		workID = &value
		workValue = "'" + value + "'"
		deadline = fmt.Sprintf(`transaction_timestamp()-interval '%s'-interval '1 second'+interval '20 seconds'`, releasedAgo)
	}
	requestDigest := singleClaimDigestSQL(claimID, replicaID, policy, kind, digestSQL, workID)
	return []string{
		fmt.Sprintf(`INSERT INTO public.shared_claim_scope_summaries(policy_id,scope_kind,scope_digest,running_count,waiting_count,next_ordinal,updated_at) VALUES('%s','%s',%s,0,0,%d,clock_timestamp()) ON CONFLICT (policy_id,scope_kind,scope_digest) DO UPDATE SET next_ordinal=GREATEST(public.shared_claim_scope_summaries.next_ordinal,EXCLUDED.next_ordinal)`, policy, kind, digestSQL, allocation+1),
		fmt.Sprintf(`INSERT INTO public.shared_claim_requests(claim_id,policy_id,replica_id,work_id,state,admitted_at,deadline_at,released_at,release_reason,scope_count,request_digest) VALUES('%s','%s','%s',%s,'released',transaction_timestamp()-interval '%s'-interval '1 second',%s,transaction_timestamp()-interval '%s','joined',1,%s)`, claimID, policy, replicaID, workValue, releasedAgo, deadline, releasedAgo, requestDigest),
		fmt.Sprintf(`INSERT INTO public.shared_claim_scopes(claim_id,request_ordinal,policy_id,scope_kind,scope_digest,allocation_ordinal,state) VALUES('%s',1,'%s','%s',%s,%d,'released')`, claimID, policy, kind, digestSQL, allocation),
	}
}

func requireNoClaimIdentityLeak(t *testing.T, pgErr *pgconn.PgError, secrets ...string) {
	t.Helper()
	if pgErr == nil {
		t.Fatal("missing database error")
	}
	if pgErr.Detail != "" || pgErr.Hint != "" || pgErr.SchemaName != "" || pgErr.TableName != "" || pgErr.ColumnName != "" || pgErr.ConstraintName != "" {
		t.Fatalf("fixed error carries context: %+v", pgErr)
	}
	for _, secret := range secrets {
		if strings.Contains(strings.ToLower(pgErr.Message), strings.ToLower(secret)) || strings.Contains(strings.ToLower(pgErr.Where), strings.ToLower(secret)) {
			t.Fatalf("fixed error discloses %q: %+v", secret, pgErr)
		}
	}
}

type claimRaceHold struct {
	conn *sql.Conn
	pid  int
}

func openClaimHolder(t *testing.T, db *sql.DB, role, expression string) claimRaceHold {
	t.Helper()
	conn, pid := openRegistrationTx(t, db, role)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := scanClaimResult(ctx, conn, expression); err != nil {
		if rollbackErr := rollbackRegistrationTx(conn); rollbackErr != nil {
			t.Error(rollbackErr)
		}
		t.Fatal(err)
	}
	return claimRaceHold{conn: conn, pid: pid}
}

func (h claimRaceHold) commit() error   { return finishRegistrationTx(h.conn) }
func (h claimRaceHold) rollback() error { return rollbackRegistrationTx(h.conn) }

// raceClaimOperation starts waiterExpression on a fresh app or maintenance
// transaction, proves it blocks on a database lock, settles the holder, then
// returns the waiter's result after committing its transaction.
func raceClaimOperation(t *testing.T, db *sql.DB, settle func() error, waiterRole, waiterExpression string) (claimOperationResult, error) {
	t.Helper()
	waiter, waiterPID := openRegistrationTx(t, db, waiterRole)
	raceCtx, cancelRace := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelRace()
	type outcome struct {
		result claimOperationResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := scanClaimResult(raceCtx, waiter, waiterExpression)
		done <- outcome{result, err}
	}()
	waitErr := waitForTransitionCondition(db, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND NOT granted)`, waiterPID)
	settleErr := settle()
	var result outcome
	select {
	case result = <-done:
	case <-time.After(10 * time.Second):
		cancelRace()
		result = <-done
		if rollbackErr := rollbackRegistrationTx(waiter); rollbackErr != nil {
			t.Error(rollbackErr)
		}
		t.Fatal("timed out joining claim race")
	}
	if waitErr != nil || settleErr != nil {
		if rollbackErr := rollbackRegistrationTx(waiter); rollbackErr != nil {
			t.Error(rollbackErr)
		}
		t.Fatalf("race setup wait=%v settle=%v result=%+v/%v", waitErr, settleErr, result.result, result.err)
	}
	if result.err != nil {
		if rollbackErr := rollbackRegistrationTx(waiter); rollbackErr != nil {
			t.Error(rollbackErr)
		}
		return result.result, result.err
	}
	if err := finishRegistrationTx(waiter); err != nil {
		t.Fatal(err)
	}
	return result.result, nil
}

func claimResultsEqual(a, b claimOperationResult) bool {
	nullTimeEqual := func(x, y sql.NullTime) bool { return x.Valid == y.Valid && (!x.Valid || x.Time.Equal(y.Time)) }
	return a.outcome == b.outcome && a.claimID == b.claimID && a.policy == b.policy && a.replica == b.replica && a.work == b.work && a.state == b.state && a.releaseReason == b.releaseReason &&
		nullTimeEqual(a.admitted, b.admitted) && nullTimeEqual(a.deadline, b.deadline) && nullTimeEqual(a.released, b.released) &&
		bytes.Equal(a.digest, b.digest) && bytes.Equal(a.scope1Digest, b.scope1Digest) && bytes.Equal(a.scope2Digest, b.scope2Digest) &&
		a.scopeCount == b.scopeCount && a.scope1Kind == b.scope1Kind && a.scope2Kind == b.scope2Kind && a.allocation1 == b.allocation1 && a.allocation2 == b.allocation2
}
