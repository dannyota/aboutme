package store

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestRuntimeClaimReceiptStoreRejectsNilInputsWithoutQuery(t *testing.T) {
	result, err := NewRuntimeClaimReceiptStore(nil).GCReleasedClaimReceipts(context.Background())
	if err == nil || result != (RuntimeClaimReceiptGCResult{}) {
		t.Fatalf("nil pool result=%+v error=%v", result, err)
	}
	runner := &registrationRunnerStub{}
	result, err = (&runtimeClaimReceiptStore{runner: runner}).GCReleasedClaimReceipts(nil) //nolint:staticcheck // Explicit nil-context rejection is part of the boundary.
	if err == nil || result != (RuntimeClaimReceiptGCResult{}) || runner.calls != 0 {
		t.Fatalf("nil context result=%+v error=%v calls=%d", result, err, runner.calls)
	}
	result, err = (&runtimeClaimReceiptStore{}).GCReleasedClaimReceipts(context.Background())
	if err == nil || result != (RuntimeClaimReceiptGCResult{}) {
		t.Fatalf("nil runner result=%+v error=%v", result, err)
	}
}

func TestRuntimeClaimReceiptStoreDecodesCountsAndZerosEveryFailure(t *testing.T) {
	db := &registrationDBStub{values: []any{int32(2), int32(1)}}
	runner := &registrationRunnerStub{queries: New(db)}
	result, err := (&runtimeClaimReceiptStore{runner: runner}).GCReleasedClaimReceipts(context.Background())
	if err != nil || result != (RuntimeClaimReceiptGCResult{DeletedClaimCount: 2, DeletedScopeSummaryCount: 1}) || runner.calls != 1 || db.calls != 1 {
		t.Fatalf("result=%+v error=%v runner=%d queries=%d", result, err, runner.calls, db.calls)
	}
	driverErr := errors.New("driver failed")
	for _, test := range []struct {
		name   string
		runner *registrationRunnerStub
	}{
		{"runner", &registrationRunnerStub{err: driverErr}},
		{"finish_or_commit", &registrationRunnerStub{queries: New(&registrationDBStub{values: []any{int32(2), int32(1)}}), afterErr: driverErr}},
		{"query", &registrationRunnerStub{queries: New(&registrationDBStub{err: driverErr})}},
		{"negative_claims", &registrationRunnerStub{queries: New(&registrationDBStub{values: []any{int32(-1), int32(0)}})}},
		{"negative_summaries", &registrationRunnerStub{queries: New(&registrationDBStub{values: []any{int32(0), int32(-1)}})}},
		{"null_count", &registrationRunnerStub{queries: New(&registrationDBStub{values: []any{nil, int32(0)}})}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := (&runtimeClaimReceiptStore{runner: test.runner}).GCReleasedClaimReceipts(context.Background())
			if err == nil || result != (RuntimeClaimReceiptGCResult{}) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			if (test.name == "runner" || test.name == "query" || test.name == "finish_or_commit") && !errors.Is(err, driverErr) {
				t.Fatalf("driver cause not wrapped: %v", err)
			}
		})
	}
}

func TestRuntimeClaimReceiptStoreOrderAndSharedNothingWithClaims(t *testing.T) {
	runner, lease := claimOrderRunner([]any{int32(0), int32(0)})
	result, err := (&runtimeClaimReceiptStore{runner: runner}).GCReleasedClaimReceipts(context.Background())
	if err != nil || result != (RuntimeClaimReceiptGCResult{}) || lease.tx.functions != 1 {
		t.Fatalf("result=%+v error=%v functions=%d", result, err, lease.tx.functions)
	}
	assertEvents(t, lease.events, "acquire", "begin", "entry", "function:runtime_gc_released_claim_receipts", "finish", "commit", "release")
	var _ RuntimeClaimReceiptStore = (*runtimeClaimReceiptStore)(nil)
	var _ RuntimeClaimTransport = (*runtimeClaimTransport)(nil)
}

func TestRuntimeClaimReceiptStoreLiveMaintenanceRoleAndAmbiguity(t *testing.T) {
	dsn, admin := claimLiveDatabase(t)
	appResult, err := NewRuntimeClaimReceiptStore(newWriteRunnerAppPool(t, dsn, nil)).GCReleasedClaimReceipts(context.Background())
	if !isSQLState(err, "42501") || appResult != (RuntimeClaimReceiptGCResult{}) {
		t.Fatalf("app result=%+v error=%v", appResult, err)
	}
	wantErr := errors.New("simulated lost receipt commit response")
	ambiguous := &runtimeClaimReceiptStore{runner: liveWrappedRunner(newMaintenancePool(t, dsn), func(tx pgx.Tx) pgx.Tx {
		return &commitResponseLostTx{Tx: tx, err: wantErr}
	})}
	result, err := ambiguous.GCReleasedClaimReceipts(context.Background())
	if !errors.Is(err, wantErr) || result != (RuntimeClaimReceiptGCResult{}) {
		t.Fatalf("ambiguous result=%+v error=%v", result, err)
	}
	store := NewRuntimeClaimReceiptStore(newMaintenancePool(t, dsn))
	result, err = store.GCReleasedClaimReceipts(context.Background())
	if err != nil || result != (RuntimeClaimReceiptGCResult{}) {
		t.Fatalf("empty gc result=%+v error=%v", result, err)
	}
	replica := registrationStoreIdentity().ReplicaID
	receipt := "77777777-7777-4777-8777-777777777777"
	claimLiveOwnerWrite(t, admin,
		`INSERT INTO public.shared_claim_scope_summaries(policy_id,scope_kind,scope_digest,running_count,waiting_count,next_ordinal,updated_at) VALUES('mail.send','global',decode(repeat('44',32),'hex'),0,0,2,clock_timestamp())`,
		fmt.Sprintf(`INSERT INTO public.shared_claim_requests(claim_id,policy_id,replica_id,work_id,state,admitted_at,deadline_at,released_at,release_reason,scope_count,request_digest) VALUES('%s','mail.send','%s',NULL,'released',clock_timestamp()-interval '26 hours',NULL,clock_timestamp()-interval '25 hours','joined',1,public.runtime_claim_input_digest('%s','mail.send','%s',NULL,1::smallint,'global',decode(repeat('44',32),'hex'),NULL,NULL))`, receipt, replica, receipt, replica),
		fmt.Sprintf(`INSERT INTO public.shared_claim_scopes(claim_id,request_ordinal,policy_id,scope_kind,scope_digest,allocation_ordinal,state) VALUES('%s',1,'mail.send','global',decode(repeat('44',32),'hex'),1,'released')`, receipt),
	)
	result, err = store.GCReleasedClaimReceipts(context.Background())
	if err != nil || result != (RuntimeClaimReceiptGCResult{DeletedClaimCount: 1, DeletedScopeSummaryCount: 1}) {
		t.Fatalf("gc result=%+v error=%v", result, err)
	}
	var remaining int
	if err := admin.QueryRowContext(context.Background(), `SELECT (SELECT count(*) FROM public.shared_claim_requests)+(SELECT count(*) FROM public.shared_claim_scopes)+(SELECT count(*) FROM public.shared_claim_scope_summaries)`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("remaining rows=%d error=%v", remaining, err)
	}
}
