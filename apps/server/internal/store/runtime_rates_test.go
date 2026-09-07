package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

var rateStoreEffective = time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC)

func rateStoreDigest(fill byte) [32]byte {
	var digest [32]byte
	for i := range digest {
		digest[i] = fill
	}
	return digest
}

func rateDecisionProjection(allowed bool, kind string, partition int16) runtimeRateDecisionProjection {
	p := runtimeRateDecisionProjection{Allowed: allowed, BucketKind: kind, EffectiveAt: rateStoreEffective}
	if !allowed {
		p.RetryAfterSeconds = 12
	}
	if kind == "private" {
		p.PartitionValue, p.PartitionPresent = partition, true
	}
	return p
}

func rateDecisionValues(p runtimeRateDecisionProjection) []any {
	return []any{p.Allowed, p.RetryAfterSeconds, p.BucketKind, p.PartitionValue, p.PartitionPresent, p.EffectiveAt}
}

func rateStubTransport(values []any) (*runtimeRateTransport, *registrationRunnerStub, *registrationDBStub) {
	db := &registrationDBStub{values: values}
	runner := &registrationRunnerStub{queries: New(db)}
	return &runtimeRateTransport{runner: runner}, runner, db
}

func TestRuntimeRateTransportDecisionOperationsCallOneFunctionEach(t *testing.T) {
	digest := rateStoreDigest(0x11)
	for _, test := range []struct {
		name, sqlName string
		call          func(RuntimeRateTransport) (RuntimeRateDecisionResult, error)
	}{
		{"admit_token", "runtime_admit_token_rate", func(s RuntimeRateTransport) (RuntimeRateDecisionResult, error) {
			return s.AdmitToken(context.Background(), "api.outer_request", digest)
		}},
		{"admit_slug", "runtime_admit_slug_change", func(s RuntimeRateTransport) (RuntimeRateDecisionResult, error) {
			return s.AdmitChangedSlug(context.Background(), digest)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			projection := rateDecisionProjection(true, "private", 2)
			transport, runner, db := rateStubTransport(rateDecisionValues(projection))
			result, err := test.call(transport)
			if err != nil || runner.calls != 1 || db.calls != 1 || !strings.Contains(db.sql, test.sqlName) {
				t.Fatalf("result=%+v error=%v runner=%d queries=%d sql=%q", result, err, runner.calls, db.calls, db.sql)
			}
			if !result.Allowed || result.RetryAfterSeconds != 0 || result.BucketKind != "private" || result.Partition == nil || *result.Partition != 2 || !result.EffectiveAt.Equal(rateStoreEffective) {
				t.Fatalf("decoded result=%+v partition=%v", result, result.Partition)
			}
		})
	}
}

func TestRuntimeRateTransportDecodesEveryDecisionShape(t *testing.T) {
	for _, test := range []struct {
		name       string
		projection runtimeRateDecisionProjection
		check      func(RuntimeRateDecisionResult) bool
	}{
		{"allowed_private_1", rateDecisionProjection(true, "private", 1), func(r RuntimeRateDecisionResult) bool {
			return r.Allowed && r.RetryAfterSeconds == 0 && r.BucketKind == "private" && r.Partition != nil && *r.Partition == 1
		}},
		{"allowed_overflow", rateDecisionProjection(true, "overflow", 0), func(r RuntimeRateDecisionResult) bool {
			return r.Allowed && r.BucketKind == "overflow" && r.Partition == nil
		}},
		{"denied_private_2", rateDecisionProjection(false, "private", 2), func(r RuntimeRateDecisionResult) bool {
			return !r.Allowed && r.RetryAfterSeconds == 12 && r.Partition != nil && *r.Partition == 2
		}},
		{"denied_overflow", rateDecisionProjection(false, "overflow", 0), func(r RuntimeRateDecisionResult) bool {
			return !r.Allowed && r.RetryAfterSeconds == 12 && r.BucketKind == "overflow" && r.Partition == nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport, _, _ := rateStubTransport(rateDecisionValues(test.projection))
			result, err := transport.AdmitChangedSlug(context.Background(), rateStoreDigest(0x11))
			if err != nil || !test.check(result) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
}

func TestRuntimeRateTransportZerosEveryInvalidDecisionRow(t *testing.T) {
	mutate := func(name string, change func(*runtimeRateDecisionProjection)) struct {
		name       string
		projection runtimeRateDecisionProjection
	} {
		p := rateDecisionProjection(true, "private", 1)
		change(&p)
		return struct {
			name       string
			projection runtimeRateDecisionProjection
		}{name, p}
	}
	for _, test := range []struct {
		name       string
		projection runtimeRateDecisionProjection
	}{
		mutate("unknown_kind", func(p *runtimeRateDecisionProjection) { p.BucketKind = "shared" }),
		mutate("empty_kind", func(p *runtimeRateDecisionProjection) { p.BucketKind = "" }),
		mutate("allowed_with_retry", func(p *runtimeRateDecisionProjection) { p.RetryAfterSeconds = 3 }),
		mutate("denied_without_retry", func(p *runtimeRateDecisionProjection) { p.Allowed = false }),
		mutate("negative_retry", func(p *runtimeRateDecisionProjection) { p.Allowed = false; p.RetryAfterSeconds = -1 }),
		mutate("private_without_partition", func(p *runtimeRateDecisionProjection) { p.PartitionPresent = false }),
		mutate("private_partition_zero", func(p *runtimeRateDecisionProjection) { p.PartitionValue = 0 }),
		mutate("private_partition_three", func(p *runtimeRateDecisionProjection) { p.PartitionValue = 3 }),
		mutate("overflow_with_partition", func(p *runtimeRateDecisionProjection) { p.BucketKind = "overflow" }),
		mutate("zero_effective_at", func(p *runtimeRateDecisionProjection) { p.EffectiveAt = time.Time{} }),
	} {
		t.Run(test.name, func(t *testing.T) {
			transport, runner, _ := rateStubTransport(rateDecisionValues(test.projection))
			runner.afterErr = errors.New("must not reach finish")
			result, err := transport.AdmitToken(context.Background(), "api.outer_request", rateStoreDigest(0x11))
			if err == nil || result != (RuntimeRateDecisionResult{}) || errors.Is(err, runner.afterErr) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
	driverErr := errors.New("driver failed")
	allowed := rateDecisionValues(rateDecisionProjection(true, "overflow", 0))
	for _, test := range []struct {
		name   string
		runner *registrationRunnerStub
	}{
		{"runner", &registrationRunnerStub{err: driverErr}},
		{"finish_or_commit_after_decode", &registrationRunnerStub{queries: New(&registrationDBStub{values: allowed}), afterErr: driverErr}},
		{"query", &registrationRunnerStub{queries: New(&registrationDBStub{err: driverErr})}},
		{"required_null", &registrationRunnerStub{queries: New(&registrationDBStub{values: append([]any{nil}, allowed[1:]...)})}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := (&runtimeRateTransport{runner: test.runner}).AdmitToken(context.Background(), "api.outer_request", rateStoreDigest(0x11))
			if err == nil || result != (RuntimeRateDecisionResult{}) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			if test.name != "required_null" && !errors.Is(err, driverErr) {
				t.Fatalf("driver cause not wrapped: %v", err)
			}
		})
	}
}

func TestRuntimeRateTransportRejectsDecisionInputsWithoutQuery(t *testing.T) {
	digest := rateStoreDigest(0x11)
	for _, test := range []struct {
		name string
		call func(RuntimeRateTransport) (RuntimeRateDecisionResult, error)
	}{
		{"token_nil_context", func(s RuntimeRateTransport) (RuntimeRateDecisionResult, error) {
			return s.AdmitToken(nil, "api.outer_request", digest) //nolint:staticcheck // Explicit nil-context rejection is part of the boundary.
		}},
		{"token_empty_policy", func(s RuntimeRateTransport) (RuntimeRateDecisionResult, error) {
			return s.AdmitToken(context.Background(), "", digest)
		}},
		{"slug_nil_context", func(s RuntimeRateTransport) (RuntimeRateDecisionResult, error) {
			return s.AdmitChangedSlug(nil, digest) //nolint:staticcheck // Explicit nil-context rejection is part of the boundary.
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &registrationRunnerStub{}
			result, err := test.call(&runtimeRateTransport{runner: runner})
			if err == nil || result != (RuntimeRateDecisionResult{}) || runner.calls != 0 {
				t.Fatalf("result=%+v error=%v calls=%d", result, err, runner.calls)
			}
		})
	}
	result, err := NewRuntimeRateTransport(nil).AdmitChangedSlug(context.Background(), digest)
	if err == nil || result != (RuntimeRateDecisionResult{}) {
		t.Fatalf("nil pool result=%+v error=%v", result, err)
	}
	result, err = (&runtimeRateTransport{}).AdmitToken(context.Background(), "api.outer_request", digest)
	if err == nil || result != (RuntimeRateDecisionResult{}) {
		t.Fatalf("nil runner result=%+v error=%v", result, err)
	}
	var _ RuntimeRateTransport = (*runtimeRateTransport)(nil)
	var _ = uuid.Nil
}

func ratePasswordProjection(kind string, partition int16, present bool) runtimeRatePasswordProjection {
	p := runtimeRatePasswordProjection{BucketKind: kind, EffectiveAt: rateStoreEffective, ActivityRefreshed: true}
	if present {
		p.PartitionValue, p.PartitionPresent = partition, true
	}
	return p
}

func rateAbsentStateProjection() runtimeRatePasswordProjection {
	p := ratePasswordProjection("private", 0, false)
	p.ActivityRefreshed = false
	return p
}

func rateExhaustedProjection(kind string, partition int16, present bool) runtimeRatePasswordProjection {
	p := ratePasswordProjection(kind, partition, present)
	p.Exhausted, p.RetryAfterSeconds = true, 300
	return p
}

func ratePasswordValues(p runtimeRatePasswordProjection) []any {
	return []any{p.Exhausted, p.RetryAfterSeconds, p.BucketKind, p.PartitionValue, p.PartitionPresent, p.EffectiveAt, p.Allocated, p.ActivityRefreshed}
}

func rateClearProjection(cleared bool, partition int16) RuntimeClearPasswordFailureRow {
	p := RuntimeClearPasswordFailureRow{Cleared: cleared, BucketKind: "private"}
	if cleared {
		p.PartitionValue, p.PartitionPresent = partition, true
	}
	return p
}

func rateClearValues(p RuntimeClearPasswordFailureRow) []any {
	return []any{p.Cleared, p.BucketKind, p.PartitionValue, p.PartitionPresent}
}

func TestRuntimeRateTransportPasswordOperationsCallOneFunctionEach(t *testing.T) {
	digest := rateStoreDigest(0x22)
	state := ratePasswordProjection("private", 1, true)
	record := ratePasswordProjection("private", 1, true)
	record.Allocated = true
	for _, test := range []struct {
		name, sqlName string
		values        []any
		call          func(RuntimeRateTransport) (any, error)
		check         func(any) bool
	}{
		{"failure_state", "runtime_password_failure_state", ratePasswordValues(state), func(s RuntimeRateTransport) (any, error) {
			return s.FailureState(context.Background(), digest)
		}, func(v any) bool {
			r, ok := v.(RuntimePasswordFailureResult)
			return ok && !r.Exhausted && r.RetryAfterSeconds == 0 && r.BucketKind == "private" && r.Partition != nil && *r.Partition == 1 && r.EffectiveAt.Equal(rateStoreEffective) && !r.Allocated && r.ActivityRefreshed
		}},
		{"record_failure", "runtime_record_password_failure", ratePasswordValues(record), func(s RuntimeRateTransport) (any, error) {
			return s.RecordFailure(context.Background(), digest)
		}, func(v any) bool {
			r, ok := v.(RuntimePasswordFailureResult)
			return ok && !r.Exhausted && r.BucketKind == "private" && r.Partition != nil && *r.Partition == 1 && r.Allocated && r.ActivityRefreshed
		}},
		{"clear_failure", "runtime_clear_password_failure", rateClearValues(rateClearProjection(true, 2)), func(s RuntimeRateTransport) (any, error) {
			return s.ClearFailureSuccess(context.Background(), digest)
		}, func(v any) bool {
			r, ok := v.(RuntimePasswordClearResult)
			return ok && r.Cleared && r.BucketKind == "private" && r.Partition != nil && *r.Partition == 2
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport, runner, db := rateStubTransport(test.values)
			result, err := test.call(transport)
			if err != nil || runner.calls != 1 || db.calls != 1 || !strings.Contains(db.sql, test.sqlName) {
				t.Fatalf("result=%+v error=%v runner=%d queries=%d sql=%q", result, err, runner.calls, db.calls, db.sql)
			}
			if !test.check(result) {
				t.Fatalf("decoded result=%+v", result)
			}
		})
	}
}

func TestRuntimeRateTransportDecodesEveryPasswordShape(t *testing.T) {
	for _, test := range []struct {
		name       string
		projection runtimeRatePasswordProjection
		state      bool
		check      func(RuntimePasswordFailureResult) bool
	}{
		{"state_existing_private", ratePasswordProjection("private", 2, true), true, func(r RuntimePasswordFailureResult) bool {
			return !r.Exhausted && r.BucketKind == "private" && r.Partition != nil && *r.Partition == 2 && !r.Allocated && r.ActivityRefreshed
		}},
		{"state_absent_routable", rateAbsentStateProjection(), true, func(r RuntimePasswordFailureResult) bool {
			return !r.Exhausted && r.RetryAfterSeconds == 0 && r.BucketKind == "private" && r.Partition == nil && !r.Allocated && !r.ActivityRefreshed
		}},
		{"state_overflow_exhausted", rateExhaustedProjection("overflow", 0, false), true, func(r RuntimePasswordFailureResult) bool {
			return r.Exhausted && r.RetryAfterSeconds == 300 && r.BucketKind == "overflow" && r.Partition == nil && r.ActivityRefreshed
		}},
		{"record_existing_private", ratePasswordProjection("private", 1, true), false, func(r RuntimePasswordFailureResult) bool {
			return !r.Allocated && r.Partition != nil && *r.Partition == 1
		}},
		{"record_allocated_private", func() runtimeRatePasswordProjection {
			p := ratePasswordProjection("private", 2, true)
			p.Allocated = true
			return p
		}(), false, func(r RuntimePasswordFailureResult) bool {
			return r.Allocated && r.Partition != nil && *r.Partition == 2 && r.ActivityRefreshed
		}},
		{"record_overflow_exhausted", rateExhaustedProjection("overflow", 0, false), false, func(r RuntimePasswordFailureResult) bool {
			return r.Exhausted && r.RetryAfterSeconds == 300 && r.BucketKind == "overflow" && r.Partition == nil && !r.Allocated
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport, _, _ := rateStubTransport(ratePasswordValues(test.projection))
			var result RuntimePasswordFailureResult
			var err error
			if test.state {
				result, err = transport.FailureState(context.Background(), rateStoreDigest(0x22))
			} else {
				result, err = transport.RecordFailure(context.Background(), rateStoreDigest(0x22))
			}
			if err != nil || !test.check(result) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
	for _, test := range []struct {
		name       string
		projection RuntimeClearPasswordFailureRow
		check      func(RuntimePasswordClearResult) bool
	}{
		{"cleared_private", rateClearProjection(true, 1), func(r RuntimePasswordClearResult) bool {
			return r.Cleared && r.BucketKind == "private" && r.Partition != nil && *r.Partition == 1
		}},
		{"absent_private", rateClearProjection(false, 0), func(r RuntimePasswordClearResult) bool {
			return !r.Cleared && r.BucketKind == "private" && r.Partition == nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport, _, _ := rateStubTransport(rateClearValues(test.projection))
			result, err := transport.ClearFailureSuccess(context.Background(), rateStoreDigest(0x22))
			if err != nil || !test.check(result) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
}

func TestRuntimeRateTransportZerosEveryInvalidPasswordRow(t *testing.T) {
	statePassword := func(name string, change func(*runtimeRatePasswordProjection)) struct {
		name   string
		values []any
		state  bool
	} {
		p := ratePasswordProjection("private", 1, true)
		change(&p)
		return struct {
			name   string
			values []any
			state  bool
		}{name, ratePasswordValues(p), true}
	}
	recordPassword := func(name string, change func(*runtimeRatePasswordProjection)) struct {
		name   string
		values []any
		state  bool
	} {
		p := ratePasswordProjection("private", 1, true)
		change(&p)
		return struct {
			name   string
			values []any
			state  bool
		}{name, ratePasswordValues(p), false}
	}
	for _, test := range []struct {
		name   string
		values []any
		state  bool
	}{
		statePassword("state_allocated", func(p *runtimeRatePasswordProjection) { p.Allocated = true }),
		statePassword("state_private_without_refresh", func(p *runtimeRatePasswordProjection) { p.ActivityRefreshed = false }),
		statePassword("state_absent_with_refresh", func(p *runtimeRatePasswordProjection) { p.PartitionPresent = false }),
		statePassword("state_absent_exhausted", func(p *runtimeRatePasswordProjection) {
			*p = rateAbsentStateProjection()
			p.Exhausted, p.RetryAfterSeconds = true, 60
		}),
		statePassword("state_overflow_without_refresh", func(p *runtimeRatePasswordProjection) {
			*p = ratePasswordProjection("overflow", 0, false)
			p.ActivityRefreshed = false
		}),
		statePassword("state_overflow_with_partition", func(p *runtimeRatePasswordProjection) { p.BucketKind = "overflow" }),
		statePassword("state_unknown_kind", func(p *runtimeRatePasswordProjection) { p.BucketKind = "shared" }),
		statePassword("state_partition_three", func(p *runtimeRatePasswordProjection) { p.PartitionValue = 3 }),
		statePassword("state_exhausted_zero_retry", func(p *runtimeRatePasswordProjection) { p.Exhausted = true }),
		statePassword("state_positive_retry_unexhausted", func(p *runtimeRatePasswordProjection) { p.RetryAfterSeconds = 60 }),
		statePassword("state_negative_retry", func(p *runtimeRatePasswordProjection) { p.Exhausted, p.RetryAfterSeconds = true, -1 }),
		statePassword("state_zero_effective_at", func(p *runtimeRatePasswordProjection) { p.EffectiveAt = time.Time{} }),
		recordPassword("record_absent_partition", func(p *runtimeRatePasswordProjection) { p.PartitionPresent = false }),
		recordPassword("record_without_refresh", func(p *runtimeRatePasswordProjection) { p.ActivityRefreshed = false }),
		recordPassword("record_allocated_overflow", func(p *runtimeRatePasswordProjection) {
			*p = ratePasswordProjection("overflow", 0, false)
			p.Allocated = true
		}),
		recordPassword("record_overflow_with_partition", func(p *runtimeRatePasswordProjection) { p.BucketKind = "overflow" }),
		recordPassword("record_unknown_kind", func(p *runtimeRatePasswordProjection) { p.BucketKind = "" }),
		recordPassword("record_exhausted_zero_retry", func(p *runtimeRatePasswordProjection) { p.Exhausted = true }),
	} {
		t.Run(test.name, func(t *testing.T) {
			transport, runner, _ := rateStubTransport(test.values)
			runner.afterErr = errors.New("must not reach finish")
			var result RuntimePasswordFailureResult
			var err error
			if test.state {
				result, err = transport.FailureState(context.Background(), rateStoreDigest(0x22))
			} else {
				result, err = transport.RecordFailure(context.Background(), rateStoreDigest(0x22))
			}
			if err == nil || result != (RuntimePasswordFailureResult{}) || errors.Is(err, runner.afterErr) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
	clear := func(name string, change func(*RuntimeClearPasswordFailureRow)) struct {
		name       string
		projection RuntimeClearPasswordFailureRow
	} {
		p := rateClearProjection(true, 1)
		change(&p)
		return struct {
			name       string
			projection RuntimeClearPasswordFailureRow
		}{name, p}
	}
	for _, test := range []struct {
		name       string
		projection RuntimeClearPasswordFailureRow
	}{
		clear("clear_overflow_kind", func(p *RuntimeClearPasswordFailureRow) { p.BucketKind = "overflow" }),
		clear("clear_empty_kind", func(p *RuntimeClearPasswordFailureRow) { p.BucketKind = "" }),
		clear("clear_without_partition", func(p *RuntimeClearPasswordFailureRow) { p.PartitionPresent = false }),
		clear("clear_partition_zero", func(p *RuntimeClearPasswordFailureRow) { p.PartitionValue = 0 }),
		clear("absent_with_partition", func(p *RuntimeClearPasswordFailureRow) { p.Cleared = false }),
	} {
		t.Run(test.name, func(t *testing.T) {
			transport, runner, _ := rateStubTransport(rateClearValues(test.projection))
			runner.afterErr = errors.New("must not reach finish")
			result, err := transport.ClearFailureSuccess(context.Background(), rateStoreDigest(0x22))
			if err == nil || result != (RuntimePasswordClearResult{}) || errors.Is(err, runner.afterErr) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
}

func TestRuntimeRateTransportRejectsPasswordInputsWithoutQuery(t *testing.T) {
	digest := rateStoreDigest(0x22)
	runner := &registrationRunnerStub{}
	transport := &runtimeRateTransport{runner: runner}
	state, err := transport.FailureState(nil, digest) //nolint:staticcheck // Explicit nil-context rejection is part of the boundary.
	if err == nil || state != (RuntimePasswordFailureResult{}) || runner.calls != 0 {
		t.Fatalf("state=%+v error=%v calls=%d", state, err, runner.calls)
	}
	record, err := transport.RecordFailure(nil, digest) //nolint:staticcheck // Explicit nil-context rejection is part of the boundary.
	if err == nil || record != (RuntimePasswordFailureResult{}) || runner.calls != 0 {
		t.Fatalf("record=%+v error=%v calls=%d", record, err, runner.calls)
	}
	cleared, err := transport.ClearFailureSuccess(nil, digest) //nolint:staticcheck // Explicit nil-context rejection is part of the boundary.
	if err == nil || cleared != (RuntimePasswordClearResult{}) || runner.calls != 0 {
		t.Fatalf("clear=%+v error=%v calls=%d", cleared, err, runner.calls)
	}
	if state, err = (&runtimeRateTransport{}).FailureState(context.Background(), digest); err == nil || state != (RuntimePasswordFailureResult{}) {
		t.Fatalf("nil runner state=%+v error=%v", state, err)
	}
	if cleared, err = NewRuntimeRateTransport(nil).ClearFailureSuccess(context.Background(), digest); err == nil || cleared != (RuntimePasswordClearResult{}) {
		t.Fatalf("nil pool clear=%+v error=%v", cleared, err)
	}
}

var (
	rateStoreAttemptID = uuid.MustParse("aaaaaaaa-1111-4111-8111-111111111111")
	rateStoreClientID  = uuid.MustParse("bbbbbbbb-2222-4222-8222-222222222222")
)

func rateReserveProjection(allowed, replayed bool, kind string, partition int16) RuntimeReserveOAuthFailedGrantRow {
	p := RuntimeReserveOAuthFailedGrantRow{Allowed: allowed, Replayed: replayed}
	if !allowed {
		p.RetryAfterSeconds = 900
		return p
	}
	p.BucketKindValue, p.BucketKindPresent = kind, true
	if kind == "private" {
		p.PartitionValue, p.PartitionPresent = partition, true
	}
	return p
}

func rateReserveValues(p RuntimeReserveOAuthFailedGrantRow) []any {
	return []any{p.Allowed, p.RetryAfterSeconds, p.BucketKindValue, p.BucketKindPresent, p.PartitionValue, p.PartitionPresent, p.Replayed}
}

func rateFinishProjection(resolution, outcome string) RuntimeFinishAdmissionAttemptRow {
	p := RuntimeFinishAdmissionAttemptRow{Resolution: resolution}
	if resolution != "absent_noop" {
		p.StoredOutcomeValue, p.StoredOutcomePresent = outcome, true
	}
	return p
}

func rateFinishValues(p RuntimeFinishAdmissionAttemptRow) []any {
	return []any{p.Resolution, p.StoredOutcomeValue, p.StoredOutcomePresent}
}

func TestRuntimeRateTransportAttemptOperationsCallOneFunctionEach(t *testing.T) {
	transport, runner, db := rateStubTransport(rateReserveValues(rateReserveProjection(true, false, "private", 1)))
	reserved, err := transport.ReserveFailedGrant(context.Background(), rateStoreAttemptID, rateStoreClientID)
	if err != nil || runner.calls != 1 || db.calls != 1 || !strings.Contains(db.sql, "runtime_reserve_oauth_failed_grant") {
		t.Fatalf("reserved=%+v error=%v runner=%d queries=%d sql=%q", reserved, err, runner.calls, db.calls, db.sql)
	}
	if !reserved.Allowed || reserved.RetryAfterSeconds != 0 || reserved.BucketKind == nil || *reserved.BucketKind != "private" || reserved.Partition == nil || *reserved.Partition != 1 || reserved.Replayed {
		t.Fatalf("decoded reserved=%+v", reserved)
	}
	transport, runner, db = rateStubTransport(rateFinishValues(rateFinishProjection("caller_finished", "failure")))
	finished, err := transport.FinishAttempt(context.Background(), rateStoreAttemptID, "failure")
	if err != nil || runner.calls != 1 || db.calls != 1 || !strings.Contains(db.sql, "runtime_finish_admission_attempt") {
		t.Fatalf("finished=%+v error=%v runner=%d queries=%d sql=%q", finished, err, runner.calls, db.calls, db.sql)
	}
	if finished.Resolution != "caller_finished" || finished.StoredOutcome == nil || *finished.StoredOutcome != "failure" {
		t.Fatalf("decoded finished=%+v", finished)
	}
}

func TestRuntimeRateTransportDecodesEveryAttemptShape(t *testing.T) {
	for _, test := range []struct {
		name       string
		projection RuntimeReserveOAuthFailedGrantRow
		check      func(RuntimeFailedGrantReserveResult) bool
	}{
		{"fresh_private", rateReserveProjection(true, false, "private", 2), func(r RuntimeFailedGrantReserveResult) bool {
			return r.Allowed && r.RetryAfterSeconds == 0 && r.BucketKind != nil && *r.BucketKind == "private" && r.Partition != nil && *r.Partition == 2 && !r.Replayed
		}},
		{"fresh_overflow", rateReserveProjection(true, false, "overflow", 0), func(r RuntimeFailedGrantReserveResult) bool {
			return r.Allowed && r.BucketKind != nil && *r.BucketKind == "overflow" && r.Partition == nil && !r.Replayed
		}},
		{"fresh_denied", rateReserveProjection(false, false, "", 0), func(r RuntimeFailedGrantReserveResult) bool {
			return !r.Allowed && r.RetryAfterSeconds == 900 && r.BucketKind == nil && r.Partition == nil && !r.Replayed
		}},
		{"replay_private", rateReserveProjection(true, true, "private", 1), func(r RuntimeFailedGrantReserveResult) bool {
			return r.Allowed && r.Replayed && r.BucketKind != nil && *r.BucketKind == "private" && r.Partition != nil && *r.Partition == 1
		}},
		{"replay_overflow", rateReserveProjection(true, true, "overflow", 0), func(r RuntimeFailedGrantReserveResult) bool {
			return r.Allowed && r.Replayed && r.BucketKind != nil && *r.BucketKind == "overflow" && r.Partition == nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport, _, _ := rateStubTransport(rateReserveValues(test.projection))
			result, err := transport.ReserveFailedGrant(context.Background(), rateStoreAttemptID, rateStoreClientID)
			if err != nil || !test.check(result) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
	for _, test := range []struct {
		name, requested string
		projection      RuntimeFinishAdmissionAttemptRow
		check           func(RuntimeAdmissionAttemptFinishResult) bool
	}{
		{"caller_finished_success", "success", rateFinishProjection("caller_finished", "success"), func(r RuntimeAdmissionAttemptFinishResult) bool {
			return r.Resolution == "caller_finished" && r.StoredOutcome != nil && *r.StoredOutcome == "success"
		}},
		{"caller_replay_neutral", "neutral", rateFinishProjection("caller_replay", "neutral"), func(r RuntimeAdmissionAttemptFinishResult) bool {
			return r.Resolution == "caller_replay" && r.StoredOutcome != nil && *r.StoredOutcome == "neutral"
		}},
		{"system_noop_keeps_neutral", "failure", rateFinishProjection("system_noop", "neutral"), func(r RuntimeAdmissionAttemptFinishResult) bool {
			return r.Resolution == "system_noop" && r.StoredOutcome != nil && *r.StoredOutcome == "neutral"
		}},
		{"absent_noop", "failure", rateFinishProjection("absent_noop", ""), func(r RuntimeAdmissionAttemptFinishResult) bool {
			return r.Resolution == "absent_noop" && r.StoredOutcome == nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport, _, _ := rateStubTransport(rateFinishValues(test.projection))
			result, err := transport.FinishAttempt(context.Background(), rateStoreAttemptID, test.requested)
			if err != nil || !test.check(result) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
}

func TestRuntimeRateTransportZerosEveryInvalidAttemptRow(t *testing.T) {
	reserve := func(name string, change func(*RuntimeReserveOAuthFailedGrantRow)) struct {
		name       string
		projection RuntimeReserveOAuthFailedGrantRow
	} {
		p := rateReserveProjection(true, false, "private", 1)
		change(&p)
		return struct {
			name       string
			projection RuntimeReserveOAuthFailedGrantRow
		}{name, p}
	}
	for _, test := range []struct {
		name       string
		projection RuntimeReserveOAuthFailedGrantRow
	}{
		reserve("allowed_with_retry", func(p *RuntimeReserveOAuthFailedGrantRow) { p.RetryAfterSeconds = 5 }),
		reserve("allowed_without_kind", func(p *RuntimeReserveOAuthFailedGrantRow) { p.BucketKindPresent = false }),
		reserve("allowed_unknown_kind", func(p *RuntimeReserveOAuthFailedGrantRow) { p.BucketKindValue = "shared" }),
		reserve("allowed_empty_kind", func(p *RuntimeReserveOAuthFailedGrantRow) { p.BucketKindValue = "" }),
		reserve("allowed_private_without_partition", func(p *RuntimeReserveOAuthFailedGrantRow) { p.PartitionPresent = false }),
		reserve("allowed_partition_three", func(p *RuntimeReserveOAuthFailedGrantRow) { p.PartitionValue = 3 }),
		reserve("allowed_overflow_with_partition", func(p *RuntimeReserveOAuthFailedGrantRow) { p.BucketKindValue = "overflow" }),
		reserve("denied_without_retry", func(p *RuntimeReserveOAuthFailedGrantRow) {
			*p = rateReserveProjection(false, false, "", 0)
			p.RetryAfterSeconds = 0
		}),
		reserve("denied_negative_retry", func(p *RuntimeReserveOAuthFailedGrantRow) {
			*p = rateReserveProjection(false, false, "", 0)
			p.RetryAfterSeconds = -1
		}),
		reserve("denied_with_kind", func(p *RuntimeReserveOAuthFailedGrantRow) {
			*p = rateReserveProjection(false, false, "", 0)
			p.BucketKindValue, p.BucketKindPresent = "overflow", true
		}),
		reserve("denied_with_partition", func(p *RuntimeReserveOAuthFailedGrantRow) {
			*p = rateReserveProjection(false, false, "", 0)
			p.PartitionValue, p.PartitionPresent = 1, true
		}),
		reserve("denied_replay", func(p *RuntimeReserveOAuthFailedGrantRow) { *p = rateReserveProjection(false, true, "", 0) }),
	} {
		t.Run("reserve_"+test.name, func(t *testing.T) {
			transport, runner, _ := rateStubTransport(rateReserveValues(test.projection))
			runner.afterErr = errors.New("must not reach finish")
			result, err := transport.ReserveFailedGrant(context.Background(), rateStoreAttemptID, rateStoreClientID)
			if err == nil || result != (RuntimeFailedGrantReserveResult{}) || errors.Is(err, runner.afterErr) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
	finish := func(name string, change func(*RuntimeFinishAdmissionAttemptRow)) struct {
		name       string
		projection RuntimeFinishAdmissionAttemptRow
	} {
		p := rateFinishProjection("caller_finished", "failure")
		change(&p)
		return struct {
			name       string
			projection RuntimeFinishAdmissionAttemptRow
		}{name, p}
	}
	for _, test := range []struct {
		name       string
		projection RuntimeFinishAdmissionAttemptRow
	}{
		finish("unknown_resolution", func(p *RuntimeFinishAdmissionAttemptRow) { p.Resolution = "caller_done" }),
		finish("empty_resolution", func(p *RuntimeFinishAdmissionAttemptRow) { p.Resolution = "" }),
		finish("finished_without_outcome", func(p *RuntimeFinishAdmissionAttemptRow) { p.StoredOutcomePresent = false }),
		finish("finished_other_outcome", func(p *RuntimeFinishAdmissionAttemptRow) { p.StoredOutcomeValue = "success" }),
		finish("finished_unknown_outcome", func(p *RuntimeFinishAdmissionAttemptRow) { p.StoredOutcomeValue = "partial" }),
		finish("replay_other_outcome", func(p *RuntimeFinishAdmissionAttemptRow) {
			*p = rateFinishProjection("caller_replay", "neutral")
		}),
		finish("system_noop_not_neutral", func(p *RuntimeFinishAdmissionAttemptRow) {
			*p = rateFinishProjection("system_noop", "failure")
		}),
		finish("system_noop_without_outcome", func(p *RuntimeFinishAdmissionAttemptRow) {
			*p = rateFinishProjection("system_noop", "neutral")
			p.StoredOutcomePresent = false
		}),
		finish("absent_noop_with_outcome", func(p *RuntimeFinishAdmissionAttemptRow) {
			*p = rateFinishProjection("absent_noop", "")
			p.StoredOutcomeValue, p.StoredOutcomePresent = "failure", true
		}),
	} {
		t.Run("finish_"+test.name, func(t *testing.T) {
			transport, runner, _ := rateStubTransport(rateFinishValues(test.projection))
			runner.afterErr = errors.New("must not reach finish")
			result, err := transport.FinishAttempt(context.Background(), rateStoreAttemptID, "failure")
			if err == nil || result != (RuntimeAdmissionAttemptFinishResult{}) || errors.Is(err, runner.afterErr) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
}

func TestRuntimeRateTransportRejectsAttemptInputsWithoutQuery(t *testing.T) {
	for _, test := range []struct {
		name string
		call func(RuntimeRateTransport) error
	}{
		{"reserve_nil_context", func(s RuntimeRateTransport) error {
			result, err := s.ReserveFailedGrant(nil, rateStoreAttemptID, rateStoreClientID) //nolint:staticcheck // Explicit nil-context rejection is part of the boundary.
			return rateRejected(result != (RuntimeFailedGrantReserveResult{}), err)
		}},
		{"reserve_nil_attempt", func(s RuntimeRateTransport) error {
			result, err := s.ReserveFailedGrant(context.Background(), uuid.Nil, rateStoreClientID)
			return rateRejected(result != (RuntimeFailedGrantReserveResult{}), err)
		}},
		{"reserve_nil_client", func(s RuntimeRateTransport) error {
			result, err := s.ReserveFailedGrant(context.Background(), rateStoreAttemptID, uuid.Nil)
			return rateRejected(result != (RuntimeFailedGrantReserveResult{}), err)
		}},
		{"finish_nil_context", func(s RuntimeRateTransport) error {
			result, err := s.FinishAttempt(nil, rateStoreAttemptID, "failure") //nolint:staticcheck // Explicit nil-context rejection is part of the boundary.
			return rateRejected(result != (RuntimeAdmissionAttemptFinishResult{}), err)
		}},
		{"finish_nil_attempt", func(s RuntimeRateTransport) error {
			result, err := s.FinishAttempt(context.Background(), uuid.Nil, "failure")
			return rateRejected(result != (RuntimeAdmissionAttemptFinishResult{}), err)
		}},
		{"finish_empty_outcome", func(s RuntimeRateTransport) error {
			result, err := s.FinishAttempt(context.Background(), rateStoreAttemptID, "")
			return rateRejected(result != (RuntimeAdmissionAttemptFinishResult{}), err)
		}},
		{"finish_unknown_outcome", func(s RuntimeRateTransport) error {
			result, err := s.FinishAttempt(context.Background(), rateStoreAttemptID, "canceled")
			return rateRejected(result != (RuntimeAdmissionAttemptFinishResult{}), err)
		}},
		{"finish_uppercase_outcome", func(s RuntimeRateTransport) error {
			result, err := s.FinishAttempt(context.Background(), rateStoreAttemptID, "SUCCESS")
			return rateRejected(result != (RuntimeAdmissionAttemptFinishResult{}), err)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &registrationRunnerStub{}
			if err := test.call(&runtimeRateTransport{runner: runner}); err != nil || runner.calls != 0 {
				t.Fatalf("error=%v calls=%d", err, runner.calls)
			}
		})
	}
	reserved, err := NewRuntimeRateTransport(nil).ReserveFailedGrant(context.Background(), rateStoreAttemptID, rateStoreClientID)
	if err == nil || reserved != (RuntimeFailedGrantReserveResult{}) {
		t.Fatalf("nil pool reserved=%+v error=%v", reserved, err)
	}
	finished, err := (&runtimeRateTransport{}).FinishAttempt(context.Background(), rateStoreAttemptID, "neutral")
	if err == nil || finished != (RuntimeAdmissionAttemptFinishResult{}) {
		t.Fatalf("nil runner finished=%+v error=%v", finished, err)
	}
}

// rateRejected reports a rejected call that both errored and returned the
// exact zero result.
func rateRejected(nonZero bool, err error) error {
	if err == nil {
		return errors.New("call was not rejected")
	}
	if nonZero {
		return errors.New("rejected call returned a nonzero result")
	}
	return nil
}

func TestRuntimeRateTransportEntryFunctionFinishCommitOrder(t *testing.T) {
	// The shared fake transaction only names claim functions, so each rate
	// query reaches it as "function:unknown". The exact SQL function name of
	// every rate operation is asserted in the per-operation stub tests above.
	runner, lease := claimOrderRunner(rateDecisionValues(rateDecisionProjection(true, "overflow", 0)))
	decision, err := (&runtimeRateTransport{runner: runner}).AdmitToken(context.Background(), "api.outer_request", rateStoreDigest(0x11))
	if err != nil || !decision.Allowed || decision.BucketKind != "overflow" || lease.tx.functions != 1 {
		t.Fatalf("decision=%+v error=%v functions=%d", decision, err, lease.tx.functions)
	}
	assertEvents(t, lease.events, "acquire", "begin", "entry", "function:unknown", "finish", "commit", "release")

	invalid := rateDecisionProjection(true, "private", 3)
	runner, lease = claimOrderRunner(rateDecisionValues(invalid))
	decision, err = (&runtimeRateTransport{runner: runner}).AdmitChangedSlug(context.Background(), rateStoreDigest(0x11))
	if err == nil || decision != (RuntimeRateDecisionResult{}) {
		t.Fatalf("invalid decision=%+v error=%v", decision, err)
	}
	assertEvents(t, lease.events, "acquire", "begin", "entry", "function:unknown", "rollback", "release")

	runner, lease = claimOrderRunner(ratePasswordValues(ratePasswordProjection("overflow", 0, false)))
	lease.tx.finishErr = errors.New("finish lost")
	record, err := (&runtimeRateTransport{runner: runner}).RecordFailure(context.Background(), rateStoreDigest(0x22))
	if !errors.Is(err, lease.tx.finishErr) || record != (RuntimePasswordFailureResult{}) {
		t.Fatalf("finish failure record=%+v error=%v", record, err)
	}
	assertEvents(t, lease.events, "acquire", "begin", "entry", "function:unknown", "finish", "rollback", "destroy")

	runner, lease = claimOrderRunner(rateReserveValues(rateReserveProjection(true, false, "overflow", 0)))
	lease.tx.commitErr = errors.New("commit ambiguous")
	reserved, err := (&runtimeRateTransport{runner: runner}).ReserveFailedGrant(context.Background(), rateStoreAttemptID, rateStoreClientID)
	if !errors.Is(err, lease.tx.commitErr) || reserved != (RuntimeFailedGrantReserveResult{}) {
		t.Fatalf("commit failure reserved=%+v error=%v", reserved, err)
	}
	assertEvents(t, lease.events, "acquire", "begin", "entry", "function:unknown", "finish", "commit", "destroy")

	runner, lease = claimOrderRunner(nil)
	lease.tx.queryErr = &pgconn.PgError{Code: "AM001", Message: "shared rate clock is invalid"}
	finished, err := (&runtimeRateTransport{runner: runner}).FinishAttempt(context.Background(), rateStoreAttemptID, "neutral")
	if !isSQLState(err, "AM001") || finished != (RuntimeAdmissionAttemptFinishResult{}) {
		t.Fatalf("AM001 finished=%+v error=%v", finished, err)
	}
	assertEvents(t, lease.events, "acquire", "begin", "entry", "function:unknown", "rollback", "destroy")

	runner, lease = claimOrderRunner(rateClearValues(rateClearProjection(true, 1)))
	cleared, err := (&runtimeRateTransport{runner: runner}).ClearFailureSuccess(context.Background(), rateStoreDigest(0x22))
	if err != nil || !cleared.Cleared || lease.tx.functions != 1 {
		t.Fatalf("cleared=%+v error=%v functions=%d", cleared, err, lease.tx.functions)
	}
	assertEvents(t, lease.events, "acquire", "begin", "entry", "function:unknown", "finish", "commit", "release")

	runner, lease = claimOrderRunner(ratePasswordValues(rateAbsentStateProjection()))
	state, err := (&runtimeRateTransport{runner: runner}).FailureState(context.Background(), rateStoreDigest(0x22))
	if err != nil || state.Partition != nil || state.ActivityRefreshed || lease.tx.functions != 1 {
		t.Fatalf("state=%+v error=%v functions=%d", state, err, lease.tx.functions)
	}
	assertEvents(t, lease.events, "acquire", "begin", "entry", "function:unknown", "finish", "commit", "release")
}

// rateLiveDatabase returns one migrated clone at head plus its admin pool.
func rateLiveDatabase(t *testing.T) (string, *sql.DB) {
	t.Helper()
	return testutil.NewMigratedTestDatabase(t)
}

// rateLiveEnablePartitions turns on both fixed partitions of one policy, so
// live routing allocates private buckets instead of shared overflow.
func rateLiveEnablePartitions(t *testing.T, admin *sql.DB, policy string) {
	t.Helper()
	claimLiveOwnerWrite(t, admin, fmt.Sprintf(`UPDATE public.shared_rate_partitions SET enabled=true,updated_at=clock_timestamp() WHERE policy_id='%s'`, policy))
}

// rateLiveDropClock removes one policy clock behind its identity trigger, the
// stored corruption that makes every operation of that policy raise AM001.
func rateLiveDropClock(t *testing.T, admin *sql.DB, policy string) {
	t.Helper()
	claimLiveOwnerWrite(t, admin, `ALTER TABLE public.shared_policy_clocks DISABLE TRIGGER shared_policy_clocks_validate`)
	claimLiveOwnerWrite(t, admin, fmt.Sprintf(`DELETE FROM public.shared_policy_clocks WHERE policy_id='%s'`, policy))
	claimLiveOwnerWrite(t, admin, `ALTER TABLE public.shared_policy_clocks ENABLE TRIGGER shared_policy_clocks_validate`)
}

func TestRuntimeRateTransportLiveTokenAndSlugFamilies(t *testing.T) {
	dsn, _ := rateLiveDatabase(t)
	transport := NewRuntimeRateTransport(newWriteRunnerAppPool(t, dsn, nil))
	ctx := context.Background()
	digest := rateStoreDigest(0x41)

	first, err := transport.AdmitToken(ctx, "account.export", digest)
	if err != nil || !first.Allowed || first.RetryAfterSeconds != 0 || first.BucketKind != "overflow" || first.Partition != nil || first.EffectiveAt.IsZero() {
		t.Fatalf("first=%+v error=%v", first, err)
	}
	retained := first
	retainedAt := first.EffectiveAt
	for i := 1; i < 5; i++ {
		allowed, allowErr := transport.AdmitToken(ctx, "account.export", digest)
		if allowErr != nil || !allowed.Allowed || allowed.RetryAfterSeconds != 0 {
			t.Fatalf("allowed[%d]=%+v error=%v", i, allowed, allowErr)
		}
	}
	denied, err := transport.AdmitToken(ctx, "account.export", digest)
	if err != nil || denied.Allowed || denied.RetryAfterSeconds <= 0 || denied.BucketKind != "overflow" || denied.Partition != nil {
		t.Fatalf("denied=%+v error=%v", denied, err)
	}
	if retained != first || !retained.EffectiveAt.Equal(retainedAt) {
		t.Fatalf("retained token result changed after connection reuse: retained=%+v first=%+v", retained, first)
	}
	unknown, err := transport.AdmitToken(ctx, "no.such_policy", digest)
	if !isSQLState(err, "55000") || unknown != (RuntimeRateDecisionResult{}) {
		t.Fatalf("unknown policy=%+v error=%v", unknown, err)
	}
	mismatched, err := transport.AdmitToken(ctx, "password.login_failure_email", digest)
	if !isSQLState(err, "55000") || mismatched != (RuntimeRateDecisionResult{}) {
		t.Fatalf("algorithm mismatch=%+v error=%v", mismatched, err)
	}

	for i := 0; i < 30; i++ {
		allowed, allowErr := transport.AdmitChangedSlug(ctx, digest)
		if allowErr != nil || !allowed.Allowed || allowed.RetryAfterSeconds != 0 || allowed.BucketKind != "overflow" || allowed.Partition != nil {
			t.Fatalf("slug allowed[%d]=%+v error=%v", i, allowed, allowErr)
		}
	}
	slugDenied, err := transport.AdmitChangedSlug(ctx, digest)
	if err != nil || slugDenied.Allowed || slugDenied.RetryAfterSeconds != 1 || slugDenied.BucketKind != "overflow" {
		t.Fatalf("slug denied=%+v error=%v", slugDenied, err)
	}
}

func TestRuntimeRateTransportLivePasswordFamilyAllocatesAndClears(t *testing.T) {
	dsn, admin := rateLiveDatabase(t)
	rateLiveEnablePartitions(t, admin, "password.login_failure_email")
	transport := NewRuntimeRateTransport(newWriteRunnerAppPool(t, dsn, nil))
	ctx := context.Background()
	digest := rateStoreDigest(0x51)

	absent, err := transport.FailureState(ctx, digest)
	if err != nil || absent.Exhausted || absent.RetryAfterSeconds != 0 || absent.BucketKind != "private" || absent.Partition != nil || absent.Allocated || absent.ActivityRefreshed {
		t.Fatalf("absent=%+v error=%v", absent, err)
	}
	allocated, err := transport.RecordFailure(ctx, digest)
	if err != nil || !allocated.Allocated || allocated.BucketKind != "private" || allocated.Partition == nil || *allocated.Partition != 1 || !allocated.ActivityRefreshed || allocated.Exhausted {
		t.Fatalf("allocated=%+v error=%v", allocated, err)
	}
	retained := allocated
	retainedPartition := *allocated.Partition
	existing, err := transport.FailureState(ctx, digest)
	if err != nil || existing.Allocated || existing.Partition == nil || *existing.Partition != 1 || !existing.ActivityRefreshed || existing.Exhausted {
		t.Fatalf("existing=%+v error=%v", existing, err)
	}
	var recorded RuntimePasswordFailureResult
	for i := 1; i < 10; i++ {
		recorded, err = transport.RecordFailure(ctx, digest)
		if err != nil || recorded.Allocated || recorded.Partition == nil || *recorded.Partition != 1 {
			t.Fatalf("recorded[%d]=%+v error=%v", i, recorded, err)
		}
	}
	if !recorded.Exhausted || recorded.RetryAfterSeconds <= 0 {
		t.Fatalf("tenth failure did not exhaust: %+v", recorded)
	}
	exhausted, err := transport.FailureState(ctx, digest)
	if err != nil || !exhausted.Exhausted || exhausted.RetryAfterSeconds <= 0 || exhausted.Allocated {
		t.Fatalf("exhausted=%+v error=%v", exhausted, err)
	}
	if *retained.Partition != retainedPartition || retained.Allocated != true {
		t.Fatalf("retained record result changed after connection reuse: %+v", retained)
	}
	cleared, err := transport.ClearFailureSuccess(ctx, digest)
	if err != nil || !cleared.Cleared || cleared.BucketKind != "private" || cleared.Partition == nil || *cleared.Partition != 1 {
		t.Fatalf("cleared=%+v error=%v", cleared, err)
	}
	repeated, err := transport.ClearFailureSuccess(ctx, digest)
	if err != nil || repeated.Cleared || repeated.BucketKind != "private" || repeated.Partition != nil {
		t.Fatalf("repeated clear=%+v error=%v", repeated, err)
	}
	var buckets, activeKeys int
	if err := admin.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM public.shared_rate_buckets WHERE policy_id='password.login_failure_email'),(SELECT sum(active_keys) FROM public.shared_rate_partitions WHERE policy_id='password.login_failure_email')`).Scan(&buckets, &activeKeys); err != nil || buckets != 0 || activeKeys != 0 {
		t.Fatalf("buckets=%d active_keys=%d error=%v", buckets, activeKeys, err)
	}
}

func TestRuntimeRateTransportLiveAttemptFamilyReservesReplaysAndFinishes(t *testing.T) {
	dsn, _ := rateLiveDatabase(t)
	transport := NewRuntimeRateTransport(newWriteRunnerAppPool(t, dsn, nil))
	ctx := context.Background()
	attempt := uuid.MustParse("cccccccc-0000-4000-8000-000000000001")
	other := uuid.MustParse("cccccccc-0000-4000-8000-000000000002")
	client := rateStoreClientID

	fresh, err := transport.ReserveFailedGrant(ctx, attempt, client)
	if err != nil || !fresh.Allowed || fresh.RetryAfterSeconds != 0 || fresh.BucketKind == nil || *fresh.BucketKind != "overflow" || fresh.Partition != nil || fresh.Replayed {
		t.Fatalf("fresh=%+v error=%v", fresh, err)
	}
	retained := fresh
	retainedKind := *fresh.BucketKind
	replay, err := transport.ReserveFailedGrant(ctx, attempt, client)
	if err != nil || !replay.Allowed || !replay.Replayed || replay.BucketKind == nil || *replay.BucketKind != "overflow" {
		t.Fatalf("replay=%+v error=%v", replay, err)
	}
	conflict, err := transport.ReserveFailedGrant(ctx, attempt, uuid.MustParse("dddddddd-0000-4000-8000-000000000003"))
	if !isSQLState(err, "AM002") || conflict != (RuntimeFailedGrantReserveResult{}) {
		t.Fatalf("conflict=%+v error=%v", conflict, err)
	}
	finished, err := transport.FinishAttempt(ctx, attempt, "failure")
	if err != nil || finished.Resolution != "caller_finished" || finished.StoredOutcome == nil || *finished.StoredOutcome != "failure" {
		t.Fatalf("finished=%+v error=%v", finished, err)
	}
	finishReplay, err := transport.FinishAttempt(ctx, attempt, "failure")
	if err != nil || finishReplay.Resolution != "caller_replay" || finishReplay.StoredOutcome == nil || *finishReplay.StoredOutcome != "failure" {
		t.Fatalf("finish replay=%+v error=%v", finishReplay, err)
	}
	outcomeConflict, err := transport.FinishAttempt(ctx, attempt, "success")
	if !isSQLState(err, "AM002") || outcomeConflict != (RuntimeAdmissionAttemptFinishResult{}) {
		t.Fatalf("outcome conflict=%+v error=%v", outcomeConflict, err)
	}
	retainedReserve, err := transport.ReserveFailedGrant(ctx, attempt, client)
	if err != nil || !retainedReserve.Allowed || !retainedReserve.Replayed {
		t.Fatalf("terminal replay=%+v error=%v", retainedReserve, err)
	}
	absent, err := transport.FinishAttempt(ctx, other, "neutral")
	if err != nil || absent.Resolution != "absent_noop" || absent.StoredOutcome != nil {
		t.Fatalf("absent=%+v error=%v", absent, err)
	}
	if *retained.BucketKind != retainedKind || retained.Replayed {
		t.Fatalf("retained reserve result changed after connection reuse: %+v", retained)
	}
	for i := 1; i < 10; i++ {
		id := uuid.MustParse(fmt.Sprintf("cccccccc-0000-4000-8000-%012d", 100+i))
		allowed, reserveErr := transport.ReserveFailedGrant(ctx, id, client)
		if reserveErr != nil || !allowed.Allowed {
			t.Fatalf("reserve[%d]=%+v error=%v", i, allowed, reserveErr)
		}
		charged, finishErr := transport.FinishAttempt(ctx, id, "failure")
		if finishErr != nil || charged.Resolution != "caller_finished" {
			t.Fatalf("finish[%d]=%+v error=%v", i, charged, finishErr)
		}
	}
	denied, err := transport.ReserveFailedGrant(ctx, uuid.MustParse("cccccccc-0000-4000-8000-000000000999"), client)
	if err != nil || denied.Allowed || denied.RetryAfterSeconds <= 0 || denied.BucketKind != nil || denied.Partition != nil || denied.Replayed {
		t.Fatalf("denied=%+v error=%v", denied, err)
	}
}

func TestRuntimeRateTransportLiveCommitAmbiguityReturnsZeroWithoutAuthority(t *testing.T) {
	dsn, admin := rateLiveDatabase(t)
	pool := newWriteRunnerAppPool(t, dsn, nil)
	wantErr := errors.New("simulated lost rate commit response")
	transport := &runtimeRateTransport{runner: liveWrappedRunner(pool, func(tx pgx.Tx) pgx.Tx {
		return &commitResponseLostTx{Tx: tx, err: wantErr}
	})}
	result, err := transport.RecordFailure(context.Background(), rateStoreDigest(0x61))
	if !errors.Is(err, wantErr) || result != (RuntimePasswordFailureResult{}) {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	var count int
	if err := admin.QueryRowContext(context.Background(), `SELECT count FROM public.shared_rate_overflow WHERE policy_id='password.login_failure_email'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("committed count=%d error=%v", count, err)
	}
}

func TestRuntimeRateTransportLiveFinishFailureRollsBackAndReturnsZero(t *testing.T) {
	dsn, admin := rateLiveDatabase(t)
	pool := newWriteRunnerAppPool(t, dsn, nil)
	wantErr := errors.New("simulated lost rate finish response")
	transport := &runtimeRateTransport{runner: liveWrappedRunner(pool, func(tx pgx.Tx) pgx.Tx {
		return &registrationFinishResponseLostTx{Tx: tx, err: wantErr}
	})}
	result, err := transport.RecordFailure(context.Background(), rateStoreDigest(0x62))
	if !errors.Is(err, wantErr) || result != (RuntimePasswordFailureResult{}) {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	var count, attempts int
	if err := admin.QueryRowContext(context.Background(), `SELECT (SELECT count FROM public.shared_rate_overflow WHERE policy_id='password.login_failure_email'),(SELECT count(*) FROM public.shared_admission_attempts)`).Scan(&count, &attempts); err != nil || count != 0 || attempts != 0 {
		t.Fatalf("rolled-back count=%d attempts=%d error=%v", count, attempts, err)
	}
}

func TestRuntimeRateTransportLiveAM001RetiresBackendAndReturnsZero(t *testing.T) {
	dsn, admin := rateLiveDatabase(t)
	pool := newWriteRunnerAppPool(t, dsn, nil)
	transport := NewRuntimeRateTransport(pool)
	ctx := context.Background()
	if _, err := transport.AdmitToken(ctx, "account.export", rateStoreDigest(0x63)); err != nil {
		t.Fatal(err)
	}
	var oldPID int32
	if pidErr := NewWriteTxRunner(pool).ExecWrite(ctx, func(q *Queries) error {
		return q.db.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&oldPID)
	}); pidErr != nil {
		t.Fatal(pidErr)
	}
	rateLiveDropClock(t, admin, "account.export")
	result, err := transport.AdmitToken(ctx, "account.export", rateStoreDigest(0x63))
	if !isSQLState(err, "AM001") || result != (RuntimeRateDecisionResult{}) {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	assertBackendGone(t, admin, oldPID)
	var replacementPID int32
	if err := NewWriteTxRunner(pool).ExecWrite(ctx, func(q *Queries) error {
		return q.db.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&replacementPID)
	}); err != nil {
		t.Fatal(err)
	}
	if replacementPID == oldPID {
		t.Fatalf("poisoned backend PID %d was reused", oldPID)
	}
}

func TestRuntimeRateTransportLiveCanceledContextReturnsZeroWithoutWriting(t *testing.T) {
	dsn, admin := rateLiveDatabase(t)
	transport := NewRuntimeRateTransport(newWriteRunnerAppPool(t, dsn, nil))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	record, err := transport.RecordFailure(ctx, rateStoreDigest(0x64))
	if !errors.Is(err, context.Canceled) || record != (RuntimePasswordFailureResult{}) {
		t.Fatalf("record=%+v error=%v", record, err)
	}
	reserved, err := transport.ReserveFailedGrant(ctx, rateStoreAttemptID, rateStoreClientID)
	if !errors.Is(err, context.Canceled) || reserved != (RuntimeFailedGrantReserveResult{}) {
		t.Fatalf("reserved=%+v error=%v", reserved, err)
	}
	var count, attempts int
	if scanErr := admin.QueryRowContext(context.Background(), `SELECT (SELECT count FROM public.shared_rate_overflow WHERE policy_id='password.login_failure_email'),(SELECT count(*) FROM public.shared_admission_attempts)`).Scan(&count, &attempts); scanErr != nil || count != 0 || attempts != 0 {
		t.Fatalf("count=%d attempts=%d error=%v", count, attempts, scanErr)
	}
}
