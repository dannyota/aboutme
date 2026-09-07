package migrations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// rateOperationSignatures lists the nine fixed operations with their exact
// argument types, in the accepted order.
var rateOperationSignatures = []struct{ name, signature, result string }{
	{"runtime_admit_token_rate", "(text,bytea)", "runtime_rate_decision_result"},
	{"runtime_password_failure_state", "(bytea)", "runtime_password_failure_result"},
	{"runtime_record_password_failure", "(bytea)", "runtime_password_failure_result"},
	{"runtime_clear_password_failure", "(bytea)", "runtime_password_clear_result"},
	{"runtime_admit_slug_change", "(bytea)", "runtime_rate_decision_result"},
	{"runtime_reserve_oauth_failed_grant", "(uuid,uuid)", "runtime_failed_grant_reserve_result"},
	{"runtime_finish_admission_attempt", "(uuid,text)", "runtime_admission_attempt_finish_result"},
	{"runtime_cleanup_admission_attempt_receipts", "(integer)", "runtime_admission_receipt_cleanup_result"},
	{"runtime_cleanup_rate_buckets", "(text,integer)", "runtime_rate_cleanup_result"},
}

// rateResultComposites fixes each accepted composite's field order and type.
var rateResultComposites = []struct{ name, attributes string }{
	{"runtime_rate_decision_result", "allowed:boolean,retry_after_seconds:integer,bucket_kind:text,partition:smallint,effective_at:timestamp with time zone"},
	{"runtime_password_failure_result", "exhausted:boolean,retry_after_seconds:integer,bucket_kind:text,partition:smallint,effective_at:timestamp with time zone,allocated:boolean,activity_refreshed:boolean"},
	{"runtime_password_clear_result", "cleared:boolean,bucket_kind:text,partition:smallint"},
	{"runtime_failed_grant_reserve_result", "allowed:boolean,retry_after_seconds:integer,bucket_kind:text,partition:smallint,replayed:boolean"},
	{"runtime_admission_attempt_finish_result", "resolution:text,stored_outcome:text"},
	{"runtime_admission_receipt_cleanup_result", "deleted_count:integer"},
	{"runtime_rate_cleanup_result", "deleted_count:integer,effective_at:timestamp with time zone,policy_idle:boolean"},
}

// rateHelperNames lists the owner-only helpers that carry no login grant.
var rateHelperNames = []string{
	"runtime_sample_rate_time", "runtime_rate_interval", "runtime_rate_ceil_div",
	"runtime_rate_token_refill", "runtime_rate_token_retry", "runtime_rate_window_retry",
	"runtime_rate_catalog", "runtime_rate_sample", "runtime_rate_client_digest",
	"runtime_rate_bucket_eligible", "runtime_rate_overflow_neutral",
	"runtime_rate_normalize_window", "runtime_rate_expire_one", "runtime_rate_route",
	"runtime_rate_apply_token", "runtime_rate_apply_rolling", "runtime_rate_failure_view",
}

const (
	ratePolicyToken   = "api.outer_request"
	ratePolicyFailure = "password.login_failure_email"
	ratePolicySlug    = "resume.slug_change"
	ratePolicyAttempt = "oauth.failed_grant"
	ratePolicyAlloc   = "resume.photo_upload_rate"
	rateNilUUID       = "00000000-0000-0000-0000-000000000000"
	rateFutureClock   = "2030-01-01 00:00:00+00"
	rateClockCounter  = "public.runtime_rate_clock_calls"

	// rateAttemptClockSQL is the durable P22 effective time, so a fixture
	// window stays live no matter how far the policy high-water has advanced.
	rateAttemptClockSQL = "(SELECT high_water_at FROM public.shared_policy_clocks WHERE policy_id='oauth.failed_grant')"
)

const (
	rateDecisionProjection = `(r).allowed,(r).retry_after_seconds,(r).bucket_kind,(r).partition,(r).effective_at`
	rateFailureProjection  = `(r).exhausted,(r).retry_after_seconds,(r).bucket_kind,(r).partition,(r).effective_at,(r).allocated,(r).activity_refreshed`
	rateClearProjection    = `(r).cleared,(r).bucket_kind,(r).partition`
	rateReserveProjection  = `(r).allowed,(r).retry_after_seconds,(r).bucket_kind,(r).partition,(r).replayed`
	rateFinishProjection   = `(r).resolution,(r).stored_outcome`
	rateReceiptProjection  = `(r).deleted_count`
	rateCleanupProjection  = `(r).deleted_count,(r).effective_at,(r).policy_idle`
)

type rateDecisionResult struct {
	allowed     bool
	retry       int
	kind        string
	partition   sql.NullInt16
	effectiveAt time.Time
}

type ratePasswordResult struct {
	exhausted   bool
	retry       int
	kind        string
	partition   sql.NullInt16
	effectiveAt time.Time
	allocated   bool
	refreshed   bool
}

type rateClearResult struct {
	cleared   bool
	kind      string
	partition sql.NullInt16
}

type rateReserveResult struct {
	allowed   bool
	retry     int
	kind      sql.NullString
	partition sql.NullInt16
	replayed  bool
}

type rateFinishResult struct {
	resolution    string
	storedOutcome sql.NullString
}

type rateCleanupResult struct {
	deleted     int
	effectiveAt time.Time
	policyIdle  bool
}

func rateStatement(expression, projection string) string {
	return `WITH result AS MATERIALIZED (SELECT ` + expression + ` AS r) SELECT ` + projection + ` FROM result`
}

// rateCall runs one fixed operation inside a bounded app or maintenance write
// transaction and scans its single composite row into dest.
func rateCall(t *testing.T, db *sql.DB, role, expression, projection string, dest ...any) error {
	t.Helper()
	return rateCallWithSearchPath(t, db, role, "pg_temp,public", expression, projection, dest...)
}

// rateCallWithSearchPath runs one fixed operation under an arbitrary caller
// search path, so a hostile path can be proven inert.
func rateCallWithSearchPath(t *testing.T, db *sql.DB, role, searchPath, expression, projection string, dest ...any) (resultErr error) {
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
	// A hostile search path must not reach any object the definers use.
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
	if err = conn.QueryRowContext(ctx, rateStatement(expression, projection)).Scan(dest...); err != nil {
		return err
	}
	if _, err = conn.ExecContext(ctx, `SELECT public.runtime_finish_write(); COMMIT`); err != nil {
		return err
	}
	txActive = false
	return nil
}

func callRateDecision(t *testing.T, db *sql.DB, role, expression string) (rateDecisionResult, error) {
	t.Helper()
	var r rateDecisionResult
	err := rateCall(t, db, role, expression, rateDecisionProjection, &r.allowed, &r.retry, &r.kind, &r.partition, &r.effectiveAt)
	return r, err
}

func callRatePassword(t *testing.T, db *sql.DB, role, expression string) (ratePasswordResult, error) {
	t.Helper()
	var r ratePasswordResult
	err := rateCall(t, db, role, expression, rateFailureProjection, &r.exhausted, &r.retry, &r.kind, &r.partition, &r.effectiveAt, &r.allocated, &r.refreshed)
	return r, err
}

func callRateClear(t *testing.T, db *sql.DB, role, expression string) (rateClearResult, error) {
	t.Helper()
	var r rateClearResult
	err := rateCall(t, db, role, expression, rateClearProjection, &r.cleared, &r.kind, &r.partition)
	return r, err
}

func callRateReserve(t *testing.T, db *sql.DB, role, expression string) (rateReserveResult, error) {
	t.Helper()
	var r rateReserveResult
	err := rateCall(t, db, role, expression, rateReserveProjection, &r.allowed, &r.retry, &r.kind, &r.partition, &r.replayed)
	return r, err
}

func callRateFinish(t *testing.T, db *sql.DB, role, expression string) (rateFinishResult, error) {
	t.Helper()
	var r rateFinishResult
	err := rateCall(t, db, role, expression, rateFinishProjection, &r.resolution, &r.storedOutcome)
	return r, err
}

func callRateReceiptCleanup(t *testing.T, db *sql.DB, role, expression string) (int, error) {
	t.Helper()
	var deleted int
	err := rateCall(t, db, role, expression, rateReceiptProjection, &deleted)
	return deleted, err
}

func callRateCleanup(t *testing.T, db *sql.DB, role, expression string) (rateCleanupResult, error) {
	t.Helper()
	var r rateCleanupResult
	err := rateCall(t, db, role, expression, rateCleanupProjection, &r.deleted, &r.effectiveAt, &r.policyIdle)
	return r, err
}

func rateTokenExpression(policy, digestHex string) string {
	return fmt.Sprintf(`public.runtime_admit_token_rate('%s',decode('%s','hex'))`, policy, digestHex)
}

func rateSlugExpression(digestHex string) string {
	return fmt.Sprintf(`public.runtime_admit_slug_change(decode('%s','hex'))`, digestHex)
}

func rateStateExpression(digestHex string) string {
	return fmt.Sprintf(`public.runtime_password_failure_state(decode('%s','hex'))`, digestHex)
}

func rateRecordExpression(digestHex string) string {
	return fmt.Sprintf(`public.runtime_record_password_failure(decode('%s','hex'))`, digestHex)
}

func rateClearExpression(digestHex string) string {
	return fmt.Sprintf(`public.runtime_clear_password_failure(decode('%s','hex'))`, digestHex)
}

func rateReserveExpression(attemptID, clientID string) string {
	return fmt.Sprintf(`public.runtime_reserve_oauth_failed_grant('%s','%s')`, attemptID, clientID)
}

func rateFinishExpression(attemptID, outcome string) string {
	return fmt.Sprintf(`public.runtime_finish_admission_attempt('%s','%s')`, attemptID, outcome)
}

func rateReceiptCleanupExpression(pageSize int) string {
	return fmt.Sprintf(`public.runtime_cleanup_admission_attempt_receipts(%d)`, pageSize)
}

func rateCleanupExpression(policy string, pageSize int) string {
	return fmt.Sprintf(`public.runtime_cleanup_rate_buckets('%s',%d)`, policy, pageSize)
}

// rateDigest builds a distinct 32-byte digest hex string from a small index.
func rateDigest(index int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("aboutme.rate.test.digest.%d", index)))
	return hex.EncodeToString(sum[:])
}

func rateUUID(prefix string, index int) string {
	return fmt.Sprintf("%s-0000-4000-8000-%012x", prefix, index)
}

// goRateClientDigest mirrors the accepted unkeyed P22 SHA-256 bucket key so
// the Go and SQL encodings can be compared on a pinned vector.
func goRateClientDigest(t *testing.T, clientID string) string {
	t.Helper()
	raw, err := hex.DecodeString(strings.ReplaceAll(clientID, "-", ""))
	if err != nil || len(raw) != 16 {
		t.Fatalf("client uuid %q: %v", clientID, err)
	}
	sum := sha256.Sum256(append([]byte("aboutme.oauth.failed_grant.v1"), raw...))
	return hex.EncodeToString(sum[:])
}

func enableRatePartitions(t *testing.T, db *sql.DB, policy string, partitions ...int) {
	t.Helper()
	statements := make([]string, 0, len(partitions))
	for _, partition := range partitions {
		statements = append(statements, fmt.Sprintf(`UPDATE public.shared_rate_partitions SET enabled=true,updated_at=clock_timestamp() WHERE policy_id='%s' AND partition=%d`, policy, partition))
	}
	if err := membershipWrite(t, db, statements...); err != nil {
		t.Fatal(err)
	}
}

func disableRatePartitions(t *testing.T, db *sql.DB, policy string, partitions ...int) {
	t.Helper()
	statements := make([]string, 0, len(partitions))
	for _, partition := range partitions {
		statements = append(statements, fmt.Sprintf(`UPDATE public.shared_rate_partitions SET enabled=false,updated_at=clock_timestamp() WHERE policy_id='%s' AND partition=%d`, policy, partition))
	}
	if err := membershipWrite(t, db, statements...); err != nil {
		t.Fatal(err)
	}
}

type ratePartitionState struct {
	enabled    bool
	activeKeys int
}

func ratePartition(t *testing.T, db *sql.DB, policy string, partition int) ratePartitionState {
	t.Helper()
	var state ratePartitionState
	if err := db.QueryRowContext(context.Background(), `SELECT enabled,active_keys FROM public.shared_rate_partitions WHERE policy_id=$1 AND partition=$2`, policy, partition).Scan(&state.enabled, &state.activeKeys); err != nil {
		t.Fatal(err)
	}
	return state
}

type rateBucketState struct {
	exists     bool
	partition  int
	numerator  sql.NullInt64
	refillAt   sql.NullTime
	startedAt  sql.NullTime
	count      sql.NullInt32
	eventCount sql.NullInt32
	lastSeen   time.Time
}

func rateBucket(t *testing.T, db *sql.DB, policy, digestHex string) rateBucketState {
	t.Helper()
	var state rateBucketState
	err := db.QueryRowContext(context.Background(), `SELECT partition,token_numerator,refill_at,window_started_at,count,cardinality(rolling_events),last_seen FROM public.shared_rate_buckets WHERE policy_id=$1 AND key_digest=decode($2,'hex')`, policy, digestHex).
		Scan(&state.partition, &state.numerator, &state.refillAt, &state.startedAt, &state.count, &state.eventCount, &state.lastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return state
	}
	if err != nil {
		t.Fatal(err)
	}
	state.exists = true
	return state
}

func rateOverflow(t *testing.T, db *sql.DB, policy string) rateBucketState {
	t.Helper()
	var state rateBucketState
	if err := db.QueryRowContext(context.Background(), `SELECT token_numerator,refill_at,window_started_at,count,cardinality(rolling_events),last_seen FROM public.shared_rate_overflow WHERE policy_id=$1`, policy).
		Scan(&state.numerator, &state.refillAt, &state.startedAt, &state.count, &state.eventCount, &state.lastSeen); err != nil {
		t.Fatal(err)
	}
	state.exists = true
	return state
}

type rateClockState struct {
	highWater time.Time
	lastRaw   time.Time
	anomalies int64
}

func rateClock(t *testing.T, db *sql.DB, policy string) rateClockState {
	t.Helper()
	var state rateClockState
	if err := db.QueryRowContext(context.Background(), `SELECT high_water_at,last_raw_at,anomaly_count FROM public.shared_policy_clocks WHERE policy_id=$1`, policy).Scan(&state.highWater, &state.lastRaw, &state.anomalies); err != nil {
		t.Fatal(err)
	}
	return state
}

func rateBucketCount(t *testing.T, db *sql.DB, policy string) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM public.shared_rate_buckets WHERE policy_id=$1`, policy).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

type rateAttemptState struct {
	exists                      bool
	kind, state                 string
	partition                   sql.NullInt16
	digest                      []byte
	outcome, terminalReason     sql.NullString
	windowStartedAt, reservedAt time.Time
	effectiveUntil              time.Time
	terminalAt                  sql.NullTime
	clientID                    string
	policyID                    string
}

func rateAttempt(t *testing.T, db *sql.DB, attemptID string) rateAttemptState {
	t.Helper()
	var attempt rateAttemptState
	err := db.QueryRowContext(context.Background(), `SELECT policy_id,client_id::text,bucket_kind,partition,key_digest,window_started_at,reserved_at,effective_until,state,outcome,terminal_reason,terminal_at FROM public.shared_admission_attempts WHERE attempt_id=$1`, attemptID).
		Scan(&attempt.policyID, &attempt.clientID, &attempt.kind, &attempt.partition, &attempt.digest, &attempt.windowStartedAt, &attempt.reservedAt, &attempt.effectiveUntil, &attempt.state, &attempt.outcome, &attempt.terminalReason, &attempt.terminalAt)
	if errors.Is(err, sql.ErrNoRows) {
		return attempt
	}
	if err != nil {
		t.Fatal(err)
	}
	attempt.exists = true
	return attempt
}

func rateAttemptCounts(t *testing.T, db *sql.DB) (pending, terminal int) {
	t.Helper()
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FILTER (WHERE state='pending'),count(*) FILTER (WHERE state='terminal') FROM public.shared_admission_attempts`).Scan(&pending, &terminal); err != nil {
		t.Fatal(err)
	}
	return pending, terminal
}

// setRateClock replaces the owner-only sampling helper in this test's own
// disposable database with a fixed literal and a call counter, so forward and
// backward boundaries are deterministic. The database is dropped in cleanup,
// so no other test or process can observe the replacement.
func setRateClock(t *testing.T, db *sql.DB, literal string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, `CREATE SEQUENCE IF NOT EXISTS `+rateClockCounter); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `ALTER SEQUENCE `+rateClockCounter+` OWNER TO aboutme_runtime_owner`); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`SELECT (SELECT '%s'::timestamptz) FROM (SELECT pg_catalog.nextval('%s')) AS sampled`, literal, rateClockCounter)
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE OR REPLACE FUNCTION public.runtime_sample_rate_time() RETURNS timestamptz LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog AS $body$ %s $body$`, body)); err != nil {
		t.Fatal(err)
	}
}

// restoreRateClock restores the exact production clock_timestamp body.
func restoreRateClock(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, `CREATE OR REPLACE FUNCTION public.runtime_sample_rate_time() RETURNS timestamptz LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog AS $body$
 SELECT clock_timestamp()
$body$`); err != nil {
		t.Fatal(err)
	}
}

func rateClockCalls(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var calls int64
	if err := db.QueryRowContext(context.Background(), `SELECT CASE WHEN is_called THEN last_value ELSE 0 END FROM `+rateClockCounter).Scan(&calls); err != nil {
		t.Fatal(err)
	}
	return calls
}

// rateTime parses one of the fixed literals used by the clock fixture.
func rateTime(t *testing.T, literal string) time.Time {
	t.Helper()
	parsed, err := time.Parse("2006-01-02 15:04:05-07", literal)
	if err != nil {
		t.Fatalf("clock literal %q: %v", literal, err)
	}
	return parsed
}

func requireRateOperationError(t *testing.T, err error, code string) {
	t.Helper()
	requireRegistrationError(t, err, code)
}

func rateOperationErrorOf(t *testing.T, err error, code string) *pgconn.PgError {
	t.Helper()
	requireRegistrationError(t, err, code)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("rate error is not a database error: %v", err)
	}
	return pgErr
}

// requireNoRateIdentityLeak proves a fixed error carries no private value.
func requireNoRateIdentityLeak(t *testing.T, pgErr *pgconn.PgError, secrets ...string) {
	t.Helper()
	requireNoClaimIdentityLeak(t, pgErr, secrets...)
}

// rateFillBuckets inserts count complete, ineligible buckets into one
// partition and sets its active_keys to the exact committed row count. The
// deferred per-row partition assertion makes this slow, so it runs in its own
// bounded owner transaction rather than the short membership helper.
func rateFillBuckets(t *testing.T, db *sql.DB, policy string, partition, count int, idleOldest bool) {
	t.Helper()
	columns := "policy_id,key_digest,partition,algorithm,last_seen,token_numerator,refill_at"
	values := fmt.Sprintf(`'%s',sha256(convert_to('fill-%d-'||g,'UTF8')),%d,'token_bucket',transaction_timestamp(),0,transaction_timestamp()`, policy, partition, partition)
	switch policy {
	case ratePolicyFailure, ratePolicyAttempt:
		columns = "policy_id,key_digest,partition,algorithm,last_seen,window_started_at,count"
		values = fmt.Sprintf(`'%s',sha256(convert_to('fill-%d-'||g,'UTF8')),%d,'fixed_window',transaction_timestamp(),transaction_timestamp(),1`, policy, partition, partition)
	case ratePolicySlug:
		columns = "policy_id,key_digest,partition,algorithm,last_seen,rolling_events,count"
		values = fmt.Sprintf(`'%s',sha256(convert_to('fill-%d-'||g,'UTF8')),%d,'rolling_slug',transaction_timestamp(),ARRAY[transaction_timestamp()],1`, policy, partition, partition)
	}
	statements := []string{
		fmt.Sprintf(`INSERT INTO public.shared_rate_buckets(%s) SELECT %s FROM generate_series(1,%d) g`, columns, values, count),
		fmt.Sprintf(`UPDATE public.shared_rate_partitions SET active_keys=(SELECT count(*) FROM public.shared_rate_buckets WHERE policy_id='%s' AND partition=%d),updated_at=clock_timestamp() WHERE policy_id='%s' AND partition=%d`, policy, partition, policy, partition),
	}
	if idleOldest {
		statements = append(statements, fmt.Sprintf(`UPDATE public.shared_rate_buckets SET last_seen=transaction_timestamp()-interval '48 hours' WHERE policy_id='%s' AND partition=%d AND key_digest=sha256(convert_to('fill-%d-1','UTF8'))`, policy, partition, partition))
	}
	_, tx := beginRateWrite(t, db, 180*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.ExecContext(ctx, `RESET ROLE; SELECT public.runtime_finish_write()`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func rateSeedBucket(t *testing.T, db *sql.DB, policy, digestByte string, partition int, overrides map[string]string) {
	t.Helper()
	if err := membershipWrite(t, db, rateBucketSQL(policy, digestByte, partition, overrides)); err != nil {
		t.Fatal(err)
	}
}

// rateByteDigest expands one hex byte into the 32-byte digest shape shared
// with the accepted schema fixtures.
func rateByteDigest(b string) string { return strings.Repeat(b, 32) }

func rateCeilDiv(value, divisor int64) int64 {
	quotient := value / divisor
	if value%divisor > 0 {
		quotient++
	}
	return quotient
}

// rateTokenRetry mirrors the accepted ceiling retry for an empty token bucket.
func rateTokenRetry(capacity, window int64) int {
	seconds := rateCeilDiv(rateCeilDiv(window, capacity), 1000000)
	if seconds < 1 {
		seconds = 1
	}
	return int(seconds)
}

type rateTokenPolicy struct {
	policy   string
	capacity int64
	window   int64
}

func rateTokenPolicies(t *testing.T, db *sql.DB) []rateTokenPolicy {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `SELECT policy_id,capacity,window_microseconds FROM public.shared_rate_policies WHERE algorithm='token_bucket' ORDER BY policy_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	policies := make([]rateTokenPolicy, 0, 24)
	for rows.Next() {
		var policy rateTokenPolicy
		if scanErr := rows.Scan(&policy.policy, &policy.capacity, &policy.window); scanErr != nil {
			t.Fatal(scanErr)
		}
		policies = append(policies, policy)
	}
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	if len(policies) != 21 {
		t.Fatalf("token policies=%d want=21", len(policies))
	}
	return policies
}

func rateFillDigest(t *testing.T, db *sql.DB, partition, index int) string {
	t.Helper()
	var digest string
	if err := db.QueryRowContext(context.Background(), `SELECT encode(sha256(convert_to($1,'UTF8')),'hex')`, fmt.Sprintf("fill-%d-%d", partition, index)).Scan(&digest); err != nil {
		t.Fatal(err)
	}
	return digest
}

var rateTriggerTables = map[string]string{
	"shared_policy_clocks_validate":         "shared_policy_clocks",
	"shared_rate_partitions_validate":       "shared_rate_partitions",
	"shared_rate_partitions_exact":          "shared_rate_partitions",
	"shared_rate_buckets_partition_exact":   "shared_rate_buckets",
	"shared_rate_buckets_state_exact":       "shared_rate_buckets",
	"shared_admission_attempts_state_exact": "shared_admission_attempts",
}

func rateTriggerStatements(enable bool) []string {
	verb := "DISABLE"
	if enable {
		verb = "ENABLE"
	}
	names := make([]string, 0, len(rateTriggerTables))
	for name := range rateTriggerTables {
		names = append(names, name)
	}
	sort.Strings(names)
	statements := make([]string, 0, len(names))
	for _, name := range names {
		statements = append(statements, fmt.Sprintf(`ALTER TABLE public.%s %s TRIGGER %s`, rateTriggerTables[name], verb, name))
	}
	return statements
}

// corruptRateRows installs stored corruption that the accepted schema
// constraints would otherwise reject, so the fail-closed AM001 paths can be
// proven. It always restores the triggers.
func corruptRateRows(t *testing.T, db *sql.DB, statements ...string) {
	t.Helper()
	if err := membershipWrite(t, db, rateTriggerStatements(false)...); err != nil {
		t.Fatal(err)
	}
	mutationErr := membershipWrite(t, db, statements...)
	if err := membershipWrite(t, db, rateTriggerStatements(true)...); err != nil {
		t.Fatal(err)
	}
	if mutationErr != nil {
		t.Fatal(mutationErr)
	}
}
