package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func rateCleanupValues(deleted int32, effective time.Time, idle bool) []any {
	return []any{deleted, effective, idle}
}

func rateMaintenanceStub(values []any) (*runtimeRateMaintenanceStore, *registrationRunnerStub, *registrationDBStub) {
	db := &registrationDBStub{values: values}
	runner := &registrationRunnerStub{queries: New(db)}
	return &runtimeRateMaintenanceStore{runner: runner}, runner, db
}

func TestRuntimeRateMaintenanceStoreOperationsCallOneFunctionEach(t *testing.T) {
	store, runner, db := rateMaintenanceStub(rateCleanupValues(3, rateStoreEffective, true))
	buckets, err := store.CleanupRateBuckets(context.Background(), "password.login_failure_email", 4)
	if err != nil || runner.calls != 1 || db.calls != 1 || !strings.Contains(db.sql, "runtime_cleanup_rate_buckets") {
		t.Fatalf("buckets=%+v error=%v runner=%d queries=%d sql=%q", buckets, err, runner.calls, db.calls, db.sql)
	}
	if buckets != (RuntimeRateCleanupResult{DeletedCount: 3, EffectiveAt: rateStoreEffective, PolicyIdle: true}) {
		t.Fatalf("decoded buckets=%+v", buckets)
	}
	store, runner, db = rateMaintenanceStub([]any{int32(2)})
	receipts, err := store.CleanupAdmissionAttemptReceipts(context.Background(), 2)
	if err != nil || runner.calls != 1 || db.calls != 1 || !strings.Contains(db.sql, "runtime_cleanup_admission_attempt_receipts") {
		t.Fatalf("receipts=%+v error=%v runner=%d queries=%d sql=%q", receipts, err, runner.calls, db.calls, db.sql)
	}
	if receipts != (RuntimeAdmissionReceiptCleanupResult{DeletedCount: 2}) {
		t.Fatalf("decoded receipts=%+v", receipts)
	}
	empty, err := rateMaintenanceStore(rateCleanupValues(0, rateStoreEffective, false)).CleanupRateBuckets(context.Background(), "resume.slug_change", 256)
	if err != nil || empty != (RuntimeRateCleanupResult{EffectiveAt: rateStoreEffective}) {
		t.Fatalf("empty page result=%+v error=%v", empty, err)
	}
	var _ RuntimeRateMaintenanceStore = (*runtimeRateMaintenanceStore)(nil)
}

func rateMaintenanceStore(values []any) *runtimeRateMaintenanceStore {
	store, _, _ := rateMaintenanceStub(values)
	return store
}

func TestRuntimeRateMaintenanceStoreZerosEveryInvalidCleanupRow(t *testing.T) {
	for _, test := range []struct {
		name   string
		values []any
	}{
		{"negative_count", rateCleanupValues(-1, rateStoreEffective, false)},
		{"count_over_page", rateCleanupValues(5, rateStoreEffective, false)},
		{"zero_effective_at", rateCleanupValues(1, time.Time{}, false)},
	} {
		t.Run("buckets_"+test.name, func(t *testing.T) {
			store, runner, _ := rateMaintenanceStub(test.values)
			runner.afterErr = errors.New("must not reach finish")
			result, err := store.CleanupRateBuckets(context.Background(), "password.login_failure_email", 4)
			if err == nil || result != (RuntimeRateCleanupResult{}) || errors.Is(err, runner.afterErr) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
	for _, test := range []struct {
		name   string
		values []any
	}{
		{"negative_count", []any{int32(-1)}},
		{"count_over_page", []any{int32(3)}},
	} {
		t.Run("receipts_"+test.name, func(t *testing.T) {
			store, runner, _ := rateMaintenanceStub(test.values)
			runner.afterErr = errors.New("must not reach finish")
			result, err := store.CleanupAdmissionAttemptReceipts(context.Background(), 2)
			if err == nil || result != (RuntimeAdmissionReceiptCleanupResult{}) || errors.Is(err, runner.afterErr) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
	driverErr := errors.New("driver failed")
	page := rateCleanupValues(1, rateStoreEffective, false)
	for _, test := range []struct {
		name   string
		runner *registrationRunnerStub
	}{
		{"runner", &registrationRunnerStub{err: driverErr}},
		{"finish_or_commit_after_decode", &registrationRunnerStub{queries: New(&registrationDBStub{values: page}), afterErr: driverErr}},
		{"query", &registrationRunnerStub{queries: New(&registrationDBStub{err: driverErr})}},
		{"required_null", &registrationRunnerStub{queries: New(&registrationDBStub{values: []any{int32(1), nil, false}})}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := (&runtimeRateMaintenanceStore{runner: test.runner}).CleanupRateBuckets(context.Background(), "password.login_failure_email", 4)
			if err == nil || result != (RuntimeRateCleanupResult{}) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			if test.name != "required_null" && !errors.Is(err, driverErr) {
				t.Fatalf("driver cause not wrapped: %v", err)
			}
		})
	}
}

func TestRuntimeRateMaintenanceStoreRejectsInputsWithoutQuery(t *testing.T) {
	for _, test := range []struct {
		name string
		call func(RuntimeRateMaintenanceStore) error
	}{
		{"buckets_nil_context", func(s RuntimeRateMaintenanceStore) error {
			result, err := s.CleanupRateBuckets(nil, "password.login_failure_email", 1) //nolint:staticcheck // Explicit nil-context rejection is part of the boundary.
			return rateRejected(result != (RuntimeRateCleanupResult{}), err)
		}},
		{"buckets_empty_policy", func(s RuntimeRateMaintenanceStore) error {
			result, err := s.CleanupRateBuckets(context.Background(), "", 1)
			return rateRejected(result != (RuntimeRateCleanupResult{}), err)
		}},
		{"buckets_page_zero", func(s RuntimeRateMaintenanceStore) error {
			result, err := s.CleanupRateBuckets(context.Background(), "password.login_failure_email", 0)
			return rateRejected(result != (RuntimeRateCleanupResult{}), err)
		}},
		{"buckets_page_negative", func(s RuntimeRateMaintenanceStore) error {
			result, err := s.CleanupRateBuckets(context.Background(), "password.login_failure_email", -1)
			return rateRejected(result != (RuntimeRateCleanupResult{}), err)
		}},
		{"buckets_page_over_limit", func(s RuntimeRateMaintenanceStore) error {
			result, err := s.CleanupRateBuckets(context.Background(), "password.login_failure_email", 257)
			return rateRejected(result != (RuntimeRateCleanupResult{}), err)
		}},
		{"receipts_nil_context", func(s RuntimeRateMaintenanceStore) error {
			result, err := s.CleanupAdmissionAttemptReceipts(nil, 1) //nolint:staticcheck // Explicit nil-context rejection is part of the boundary.
			return rateRejected(result != (RuntimeAdmissionReceiptCleanupResult{}), err)
		}},
		{"receipts_page_zero", func(s RuntimeRateMaintenanceStore) error {
			result, err := s.CleanupAdmissionAttemptReceipts(context.Background(), 0)
			return rateRejected(result != (RuntimeAdmissionReceiptCleanupResult{}), err)
		}},
		{"receipts_page_over_limit", func(s RuntimeRateMaintenanceStore) error {
			result, err := s.CleanupAdmissionAttemptReceipts(context.Background(), 257)
			return rateRejected(result != (RuntimeAdmissionReceiptCleanupResult{}), err)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &registrationRunnerStub{}
			if err := test.call(&runtimeRateMaintenanceStore{runner: runner}); err != nil || runner.calls != 0 {
				t.Fatalf("error=%v calls=%d", err, runner.calls)
			}
		})
	}
	buckets, err := NewRuntimeRateMaintenanceStore(nil).CleanupRateBuckets(context.Background(), "password.login_failure_email", 1)
	if err == nil || buckets != (RuntimeRateCleanupResult{}) {
		t.Fatalf("nil pool buckets=%+v error=%v", buckets, err)
	}
	receipts, err := (&runtimeRateMaintenanceStore{}).CleanupAdmissionAttemptReceipts(context.Background(), 1)
	if err == nil || receipts != (RuntimeAdmissionReceiptCleanupResult{}) {
		t.Fatalf("nil runner receipts=%+v error=%v", receipts, err)
	}
}

func TestRuntimeRateMaintenanceStoreEntryFunctionFinishCommitOrder(t *testing.T) {
	// The asserted sequence names the cleanup in the function position, so a
	// method wired to the wrong query fails here as well as in its stub test.
	runner, lease := claimOrderRunner([]any{int32(0)})
	receipts, err := (&runtimeRateMaintenanceStore{runner: runner}).CleanupAdmissionAttemptReceipts(context.Background(), 8)
	if err != nil || receipts != (RuntimeAdmissionReceiptCleanupResult{}) || lease.tx.functions != 1 {
		t.Fatalf("receipts=%+v error=%v functions=%d", receipts, err, lease.tx.functions)
	}
	assertEvents(t, lease.events, "acquire", "begin", "entry", "function:runtime_cleanup_admission_attempt_receipts", "finish", "commit", "release")

	runner, lease = claimOrderRunner(rateCleanupValues(1, rateStoreEffective, false))
	lease.tx.commitErr = errors.New("commit ambiguous")
	buckets, err := (&runtimeRateMaintenanceStore{runner: runner}).CleanupRateBuckets(context.Background(), "resume.slug_change", 4)
	if !errors.Is(err, lease.tx.commitErr) || buckets != (RuntimeRateCleanupResult{}) {
		t.Fatalf("commit failure buckets=%+v error=%v", buckets, err)
	}
	assertEvents(t, lease.events, "acquire", "begin", "entry", "function:runtime_cleanup_rate_buckets", "finish", "commit", "destroy")

	runner, lease = claimOrderRunner(rateCleanupValues(9, rateStoreEffective, false))
	lease.tx.finishErr = errors.New("must not reach finish")
	buckets, err = (&runtimeRateMaintenanceStore{runner: runner}).CleanupRateBuckets(context.Background(), "resume.slug_change", 4)
	if err == nil || buckets != (RuntimeRateCleanupResult{}) || errors.Is(err, lease.tx.finishErr) {
		t.Fatalf("over-page buckets=%+v error=%v", buckets, err)
	}
	assertEvents(t, lease.events, "acquire", "begin", "entry", "function:runtime_cleanup_rate_buckets", "rollback", "release")
}

func TestRuntimeRateMaintenanceStoreLiveRoleBoundaryAndBoundedPages(t *testing.T) {
	dsn, admin := rateLiveDatabase(t)
	ctx := context.Background()
	appPool := newWriteRunnerAppPool(t, dsn, nil)
	appStore := NewRuntimeRateMaintenanceStore(appPool)
	buckets, err := appStore.CleanupRateBuckets(ctx, "oauth.failed_grant", 4)
	if !isSQLState(err, "42501") || buckets != (RuntimeRateCleanupResult{}) {
		t.Fatalf("app bucket cleanup=%+v error=%v", buckets, err)
	}
	receipts, err := appStore.CleanupAdmissionAttemptReceipts(ctx, 4)
	if !isSQLState(err, "42501") || receipts != (RuntimeAdmissionReceiptCleanupResult{}) {
		t.Fatalf("app receipt cleanup=%+v error=%v", receipts, err)
	}
	maintenancePool := newMaintenancePool(t, dsn)
	admitted, err := NewRuntimeRateTransport(maintenancePool).AdmitToken(ctx, "account.export", rateStoreDigest(0x71))
	if !isSQLState(err, "42501") || admitted != (RuntimeRateDecisionResult{}) {
		t.Fatalf("maintenance admission=%+v error=%v", admitted, err)
	}

	store := NewRuntimeRateMaintenanceStore(maintenancePool)
	idle, err := store.CleanupRateBuckets(ctx, "oauth.failed_grant", 4)
	if err != nil || idle.DeletedCount != 0 || !idle.PolicyIdle || idle.EffectiveAt.IsZero() {
		t.Fatalf("idle policy=%+v error=%v", idle, err)
	}
	unknown, err := store.CleanupRateBuckets(ctx, "no.such_policy", 4)
	if !isSQLState(err, "55000") || unknown != (RuntimeRateCleanupResult{}) {
		t.Fatalf("unknown policy=%+v error=%v", unknown, err)
	}
	emptyReceipts, err := store.CleanupAdmissionAttemptReceipts(ctx, 4)
	if err != nil || emptyReceipts != (RuntimeAdmissionReceiptCleanupResult{}) {
		t.Fatalf("empty receipts=%+v error=%v", emptyReceipts, err)
	}

	rateLiveEnablePartitions(t, admin, "oauth.failed_grant")
	transport := NewRuntimeRateTransport(appPool)
	for i, client := range []uuid.UUID{
		uuid.MustParse("eeeeeeee-0000-4000-8000-000000000001"),
		uuid.MustParse("eeeeeeee-0000-4000-8000-000000000002"),
	} {
		attempt := uuid.MustParse(fmt.Sprintf("ffffffff-0000-4000-8000-%012d", i+1))
		reserved, reserveErr := transport.ReserveFailedGrant(ctx, attempt, client)
		if reserveErr != nil || !reserved.Allowed || reserved.BucketKind == nil || *reserved.BucketKind != "private" || reserved.Partition == nil || *reserved.Partition != 1 {
			t.Fatalf("reserved[%d]=%+v error=%v", i, reserved, reserveErr)
		}
		released, finishErr := transport.FinishAttempt(ctx, attempt, "neutral")
		if finishErr != nil || released.Resolution != "caller_finished" || released.StoredOutcome == nil || *released.StoredOutcome != "neutral" {
			t.Fatalf("released[%d]=%+v error=%v", i, released, finishErr)
		}
	}
	page, err := store.CleanupRateBuckets(ctx, "oauth.failed_grant", 1)
	if err != nil || page.DeletedCount != 1 || page.PolicyIdle || page.EffectiveAt.IsZero() {
		t.Fatalf("first page=%+v error=%v", page, err)
	}
	rest, err := store.CleanupRateBuckets(ctx, "oauth.failed_grant", 1)
	if err != nil || rest.DeletedCount != 1 || !rest.PolicyIdle {
		t.Fatalf("second page=%+v error=%v", rest, err)
	}
	var remaining, terminal, activeKeys int
	if countErr := admin.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM public.shared_rate_buckets WHERE policy_id='oauth.failed_grant'),(SELECT count(*) FROM public.shared_admission_attempts WHERE state='terminal'),(SELECT sum(active_keys) FROM public.shared_rate_partitions WHERE policy_id='oauth.failed_grant')`).Scan(&remaining, &terminal, &activeKeys); countErr != nil || remaining != 0 || terminal != 2 || activeKeys != 0 {
		t.Fatalf("remaining=%d terminal=%d active_keys=%d error=%v", remaining, terminal, activeKeys, countErr)
	}
	retained, err := store.CleanupAdmissionAttemptReceipts(ctx, 4)
	if err != nil || retained != (RuntimeAdmissionReceiptCleanupResult{}) {
		t.Fatalf("retained receipts=%+v error=%v", retained, err)
	}
}

func TestRuntimeRateMaintenanceStoreLiveCommitAmbiguityReturnsZero(t *testing.T) {
	dsn, _ := rateLiveDatabase(t)
	wantErr := errors.New("simulated lost rate cleanup commit response")
	store := &runtimeRateMaintenanceStore{runner: liveWrappedRunner(newMaintenancePool(t, dsn), func(tx pgx.Tx) pgx.Tx {
		return &commitResponseLostTx{Tx: tx, err: wantErr}
	})}
	buckets, err := store.CleanupRateBuckets(context.Background(), "oauth.failed_grant", 4)
	if !errors.Is(err, wantErr) || buckets != (RuntimeRateCleanupResult{}) {
		t.Fatalf("buckets=%+v error=%v", buckets, err)
	}
	receipts, err := store.CleanupAdmissionAttemptReceipts(context.Background(), 4)
	if !errors.Is(err, wantErr) || receipts != (RuntimeAdmissionReceiptCleanupResult{}) {
		t.Fatalf("receipts=%+v error=%v", receipts, err)
	}
}
