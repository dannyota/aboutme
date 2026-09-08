package store

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

const (
	membershipEvidenceOperationID = "op-scale-in-1"
	membershipEvidenceExpected    = int64(9)
)

// membershipEvidenceRecordedAt is the fixed stored evidence time every stub
// projection carries, so a decoded result is compared against a pinned value
// rather than a clock.
var membershipEvidenceRecordedAt = time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

// membershipLeaveProjection builds the one shape a recorded graceful leave may
// carry: the exact target, state left, the supplied operation and historical
// prepare generation, and both joined counts zero.
func membershipLeaveProjection(id uuid.UUID, operationID string, expected int64) runtimeMembershipLeaveProjection {
	return runtimeMembershipLeaveProjection{
		ReplicaID: id, State: "left", ReceiptOperationID: operationID,
		ControllerGeneration: expected, JoinedTransitionCount: 0, JoinedClaimCount: 0,
		RecordedAt: membershipEvidenceRecordedAt,
	}
}

func membershipLeaveValues(p runtimeMembershipLeaveProjection) []any {
	return []any{p.ReplicaID, p.State, p.ReceiptOperationID, p.ControllerGeneration,
		p.JoinedTransitionCount, p.JoinedClaimCount, p.RecordedAt, p.Replayed}
}

func membershipStubTransport(values []any) (*runtimeMembershipEvidenceTransport, *registrationRunnerStub, *registrationDBStub) {
	db := &registrationDBStub{values: values}
	runner := &registrationRunnerStub{queries: New(db)}
	return &runtimeMembershipEvidenceTransport{runner: runner}, runner, db
}

// membershipLeaveMethod names one fixed-kind graceful-leave wrapper. Both
// wrappers share one result shape and one decode rule set, so every leave
// matrix runs over this table.
type membershipLeaveMethod struct {
	name, sqlName string
	call          func(RuntimeMembershipEvidenceTransport, context.Context, uuid.UUID, string, string, int64, string) (RuntimeLeaveResult, error)
}

func membershipLeaveMethods() []membershipLeaveMethod {
	return []membershipLeaveMethod{
		{"serving", "runtime_finish_serving_graceful_leave", func(s RuntimeMembershipEvidenceTransport, ctx context.Context,
			id uuid.UUID, instance, release string, expected int64, operation string) (RuntimeLeaveResult, error) {
			return s.FinishServingGracefulLeave(ctx, id, instance, release, expected, operation)
		}},
		{"maintenance", "runtime_finish_maintenance_graceful_leave", func(s RuntimeMembershipEvidenceTransport, ctx context.Context,
			id uuid.UUID, instance, release string, expected int64, operation string) (RuntimeLeaveResult, error) {
			return s.FinishMaintenanceGracefulLeave(ctx, id, instance, release, expected, operation)
		}},
	}
}

func TestRuntimeMembershipEvidenceLeaveCallsOneFunction(t *testing.T) {
	identity := registrationStoreIdentity()
	for _, method := range membershipLeaveMethods() {
		for _, replayed := range []bool{false, true} {
			name := method.name + "_fresh"
			if replayed {
				name = method.name + "_replayed"
			}
			t.Run(name, func(t *testing.T) {
				projection := membershipLeaveProjection(identity.ReplicaID, membershipEvidenceOperationID, membershipEvidenceExpected)
				projection.Replayed = replayed
				transport, runner, db := membershipStubTransport(membershipLeaveValues(projection))
				result, err := method.call(transport, context.Background(), identity.ReplicaID, identity.InstanceID,
					identity.ReleaseDigest, membershipEvidenceExpected, membershipEvidenceOperationID)
				if err != nil || runner.calls != 1 || db.calls != 1 || !strings.Contains(db.sql, method.sqlName) {
					t.Fatalf("result=%+v error=%v runner=%d queries=%d sql=%q", result, err, runner.calls, db.calls, db.sql)
				}
				if result.ReplicaID != identity.ReplicaID || result.State != "left" ||
					result.ReceiptOperationID != membershipEvidenceOperationID ||
					result.ControllerGeneration != membershipEvidenceExpected ||
					result.JoinedTransitionCount != 0 || result.JoinedClaimCount != 0 ||
					!result.RecordedAt.Equal(membershipEvidenceRecordedAt) || result.Replayed != replayed {
					t.Fatalf("decoded result=%+v", result)
				}
			})
		}
	}
}

func TestRuntimeMembershipEvidenceZerosEveryInvalidLeaveRow(t *testing.T) {
	identity := registrationStoreIdentity()
	mutate := func(name string, change func(*runtimeMembershipLeaveProjection)) struct {
		name       string
		projection runtimeMembershipLeaveProjection
	} {
		p := membershipLeaveProjection(identity.ReplicaID, membershipEvidenceOperationID, membershipEvidenceExpected)
		change(&p)
		return struct {
			name       string
			projection runtimeMembershipLeaveProjection
		}{name, p}
	}
	rows := []struct {
		name       string
		projection runtimeMembershipLeaveProjection
	}{
		mutate("other_replica", func(p *runtimeMembershipLeaveProjection) {
			p.ReplicaID = uuid.MustParse("22222222-2222-4222-8222-222222222222")
		}),
		mutate("nil_replica", func(p *runtimeMembershipLeaveProjection) { p.ReplicaID = uuid.Nil }),
		mutate("draining_state", func(p *runtimeMembershipLeaveProjection) { p.State = "draining" }),
		mutate("fenced_state", func(p *runtimeMembershipLeaveProjection) { p.State = "fenced" }),
		mutate("empty_state", func(p *runtimeMembershipLeaveProjection) { p.State = "" }),
		mutate("other_operation", func(p *runtimeMembershipLeaveProjection) { p.ReceiptOperationID = "op-other" }),
		mutate("empty_operation", func(p *runtimeMembershipLeaveProjection) { p.ReceiptOperationID = "" }),
		mutate("stale_generation", func(p *runtimeMembershipLeaveProjection) {
			p.ControllerGeneration = membershipEvidenceExpected - 1
		}),
		mutate("advanced_generation", func(p *runtimeMembershipLeaveProjection) {
			p.ControllerGeneration = membershipEvidenceExpected + 1
		}),
		mutate("zero_generation", func(p *runtimeMembershipLeaveProjection) { p.ControllerGeneration = 0 }),
		mutate("joined_transition_count", func(p *runtimeMembershipLeaveProjection) { p.JoinedTransitionCount = 1 }),
		mutate("negative_transition_count", func(p *runtimeMembershipLeaveProjection) { p.JoinedTransitionCount = -1 }),
		mutate("joined_claim_count", func(p *runtimeMembershipLeaveProjection) { p.JoinedClaimCount = 1 }),
		mutate("negative_claim_count", func(p *runtimeMembershipLeaveProjection) { p.JoinedClaimCount = -1 }),
		mutate("zero_recorded_at", func(p *runtimeMembershipLeaveProjection) { p.RecordedAt = time.Time{} }),
	}
	for _, method := range membershipLeaveMethods() {
		for _, test := range rows {
			t.Run(method.name+"/"+test.name, func(t *testing.T) {
				transport, runner, _ := membershipStubTransport(membershipLeaveValues(test.projection))
				runner.afterErr = errors.New("must not reach finish")
				result, err := method.call(transport, context.Background(), identity.ReplicaID, identity.InstanceID,
					identity.ReleaseDigest, membershipEvidenceExpected, membershipEvidenceOperationID)
				if err == nil || result != (RuntimeLeaveResult{}) || errors.Is(err, runner.afterErr) {
					t.Fatalf("result=%+v error=%v", result, err)
				}
			})
		}
		driverErr := errors.New("driver failed")
		valid := membershipLeaveValues(membershipLeaveProjection(identity.ReplicaID, membershipEvidenceOperationID, membershipEvidenceExpected))
		for _, test := range []struct {
			name   string
			runner *registrationRunnerStub
		}{
			{"runner", &registrationRunnerStub{err: driverErr}},
			{"finish_or_commit_after_decode", &registrationRunnerStub{queries: New(&registrationDBStub{values: valid}), afterErr: driverErr}},
			{"query", &registrationRunnerStub{queries: New(&registrationDBStub{err: driverErr})}},
			{"required_null", &registrationRunnerStub{queries: New(&registrationDBStub{values: append([]any{nil}, valid[1:]...)})}},
		} {
			t.Run(method.name+"/"+test.name, func(t *testing.T) {
				result, err := method.call(&runtimeMembershipEvidenceTransport{runner: test.runner}, context.Background(),
					identity.ReplicaID, identity.InstanceID, identity.ReleaseDigest, membershipEvidenceExpected, membershipEvidenceOperationID)
				if err == nil || result != (RuntimeLeaveResult{}) {
					t.Fatalf("result=%+v error=%v", result, err)
				}
				if test.name != "required_null" && !errors.Is(err, driverErr) {
					t.Fatalf("driver cause not wrapped: %v", err)
				}
			})
		}
	}
}

func TestRuntimeMembershipEvidenceRejectsLeaveInputsWithoutQuery(t *testing.T) {
	identity := registrationStoreIdentity()
	for _, method := range membershipLeaveMethods() {
		for _, test := range []struct {
			name       string
			replicaID  uuid.UUID
			instance   string
			release    string
			expected   int64
			operation  string
			nilContext bool
		}{
			{name: "nil_context", replicaID: identity.ReplicaID, instance: identity.InstanceID, release: identity.ReleaseDigest,
				expected: membershipEvidenceExpected, operation: membershipEvidenceOperationID, nilContext: true},
			{name: "nil_replica", replicaID: uuid.Nil, instance: identity.InstanceID, release: identity.ReleaseDigest,
				expected: membershipEvidenceExpected, operation: membershipEvidenceOperationID},
			{name: "empty_instance", replicaID: identity.ReplicaID, instance: "", release: identity.ReleaseDigest,
				expected: membershipEvidenceExpected, operation: membershipEvidenceOperationID},
			{name: "short_instance", replicaID: identity.ReplicaID, instance: "i-1111111111111111", release: identity.ReleaseDigest,
				expected: membershipEvidenceExpected, operation: membershipEvidenceOperationID},
			{name: "upper_case_instance", replicaID: identity.ReplicaID, instance: "i-1111111111111111A", release: identity.ReleaseDigest,
				expected: membershipEvidenceExpected, operation: membershipEvidenceOperationID},
			{name: "empty_release", replicaID: identity.ReplicaID, instance: identity.InstanceID, release: "",
				expected: membershipEvidenceExpected, operation: membershipEvidenceOperationID},
			{name: "unprefixed_release", replicaID: identity.ReplicaID, instance: identity.InstanceID, release: strings.Repeat("1", 64),
				expected: membershipEvidenceExpected, operation: membershipEvidenceOperationID},
			{name: "zero_generation", replicaID: identity.ReplicaID, instance: identity.InstanceID, release: identity.ReleaseDigest,
				expected: 0, operation: membershipEvidenceOperationID},
			{name: "negative_generation", replicaID: identity.ReplicaID, instance: identity.InstanceID, release: identity.ReleaseDigest,
				expected: -1, operation: membershipEvidenceOperationID},
			{name: "empty_operation", replicaID: identity.ReplicaID, instance: identity.InstanceID, release: identity.ReleaseDigest,
				expected: membershipEvidenceExpected, operation: ""},
			{name: "long_operation", replicaID: identity.ReplicaID, instance: identity.InstanceID, release: identity.ReleaseDigest,
				expected: membershipEvidenceExpected, operation: strings.Repeat("o", 129)},
			{name: "control_character_operation", replicaID: identity.ReplicaID, instance: identity.InstanceID, release: identity.ReleaseDigest,
				expected: membershipEvidenceExpected, operation: "op\n1"},
			{name: "high_byte_operation", replicaID: identity.ReplicaID, instance: identity.InstanceID, release: identity.ReleaseDigest,
				expected: membershipEvidenceExpected, operation: "opé"},
		} {
			t.Run(method.name+"/"+test.name, func(t *testing.T) {
				runner := &registrationRunnerStub{}
				ctx := context.Background()
				if test.nilContext {
					ctx = nil
				}
				//nolint:staticcheck // Explicit nil-context rejection is part of the boundary.
				result, err := method.call(&runtimeMembershipEvidenceTransport{runner: runner}, ctx, test.replicaID,
					test.instance, test.release, test.expected, test.operation)
				if err == nil || result != (RuntimeLeaveResult{}) || runner.calls != 0 {
					t.Fatalf("result=%+v error=%v calls=%d", result, err, runner.calls)
				}
			})
		}
		t.Run(method.name+"/nil_pool", func(t *testing.T) {
			result, err := method.call(NewRuntimeMembershipEvidenceTransport(nil), context.Background(), identity.ReplicaID,
				identity.InstanceID, identity.ReleaseDigest, membershipEvidenceExpected, membershipEvidenceOperationID)
			if err == nil || result != (RuntimeLeaveResult{}) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
		t.Run(method.name+"/nil_runner", func(t *testing.T) {
			result, err := method.call(&runtimeMembershipEvidenceTransport{}, context.Background(), identity.ReplicaID,
				identity.InstanceID, identity.ReleaseDigest, membershipEvidenceExpected, membershipEvidenceOperationID)
			if err == nil || result != (RuntimeLeaveResult{}) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
	var _ RuntimeMembershipEvidenceTransport = (*runtimeMembershipEvidenceTransport)(nil)
}

const (
	membershipEvidenceRequestID  = "req-terminate-99"
	membershipEvidenceEvidenceID = "ec2-evidence-99"
)

// The two external EC2 timestamps are stored raw and never enter ordering
// authority, so they deliberately sit far ahead of the stored evidence time.
var (
	membershipEvidenceRequestedAt = time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	membershipEvidenceObservedAt  = time.Date(2030, 1, 2, 3, 9, 5, 0, time.UTC)
)

// membershipFenceProjection builds one recorded termination proof.
func membershipFenceProjection(id uuid.UUID, state, evidenceID string, reclaimed int32) RuntimeRecordEC2TerminationRow {
	return RuntimeRecordEC2TerminationRow{ReplicaID: id, State: state, EvidenceID: evidenceID,
		ReclaimedClaimCount: reclaimed, RecordedAt: membershipEvidenceRecordedAt}
}

func membershipFenceValues(p RuntimeRecordEC2TerminationRow) []any {
	return []any{p.ReplicaID, p.State, p.EvidenceID, p.ReclaimedClaimCount, p.RecordedAt, p.Replayed}
}

// membershipRecordTermination calls the proof with the accepted registration
// identity and the fixed external evidence.
func membershipRecordTermination(ctx context.Context, s RuntimeMembershipEvidenceTransport) (RuntimeFenceResult, error) {
	i := registrationStoreIdentity()
	return s.RecordEC2Termination(ctx, i.ReplicaID, i.InstanceID, i.ReleaseDigest, membershipEvidenceRequestID,
		membershipEvidenceEvidenceID, membershipEvidenceRequestedAt, membershipEvidenceObservedAt, "terminated")
}

func TestRuntimeMembershipEvidenceTerminationProofCallsOneFunction(t *testing.T) {
	identity := registrationStoreIdentity()
	for _, test := range []struct {
		name, state string
		reclaimed   int32
		replayed    bool
	}{
		{name: "fresh_fence_without_claims", state: "fenced"},
		{name: "fresh_fence_reclaiming_claims", state: "fenced", reclaimed: 5},
		{name: "fresh_audit_proof_of_left", state: "left"},
		{name: "replayed_fence", state: "fenced", reclaimed: 5, replayed: true},
		{name: "replayed_audit_proof", state: "left", replayed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			projection := membershipFenceProjection(identity.ReplicaID, test.state, membershipEvidenceEvidenceID, test.reclaimed)
			projection.Replayed = test.replayed
			transport, runner, db := membershipStubTransport(membershipFenceValues(projection))
			result, err := membershipRecordTermination(context.Background(), transport)
			if err != nil || runner.calls != 1 || db.calls != 1 || !strings.Contains(db.sql, "runtime_record_ec2_termination") {
				t.Fatalf("result=%+v error=%v runner=%d queries=%d sql=%q", result, err, runner.calls, db.calls, db.sql)
			}
			if result.ReplicaID != identity.ReplicaID || result.State != test.state ||
				result.EvidenceID != membershipEvidenceEvidenceID || result.ReclaimedClaimCount != test.reclaimed ||
				!result.RecordedAt.Equal(membershipEvidenceRecordedAt) || result.Replayed != test.replayed {
				t.Fatalf("decoded result=%+v", result)
			}
			// The raw external evidence never becomes ordering authority: the
			// stored time stays behind both supplied timestamps.
			if !result.RecordedAt.Before(membershipEvidenceRequestedAt) {
				t.Fatalf("stored recorded_at %v was dragged forward by external evidence", result.RecordedAt)
			}
		})
	}
}

func TestRuntimeMembershipEvidenceZerosEveryInvalidFenceRow(t *testing.T) {
	identity := registrationStoreIdentity()
	mutate := func(name string, change func(*RuntimeRecordEC2TerminationRow)) struct {
		name       string
		projection RuntimeRecordEC2TerminationRow
	} {
		p := membershipFenceProjection(identity.ReplicaID, "fenced", membershipEvidenceEvidenceID, 2)
		change(&p)
		return struct {
			name       string
			projection RuntimeRecordEC2TerminationRow
		}{name, p}
	}
	for _, test := range []struct {
		name       string
		projection RuntimeRecordEC2TerminationRow
	}{
		mutate("other_replica", func(p *RuntimeRecordEC2TerminationRow) {
			p.ReplicaID = uuid.MustParse("22222222-2222-4222-8222-222222222222")
		}),
		mutate("nil_replica", func(p *RuntimeRecordEC2TerminationRow) { p.ReplicaID = uuid.Nil }),
		mutate("joining_state", func(p *RuntimeRecordEC2TerminationRow) { p.State = "joining" }),
		mutate("active_state", func(p *RuntimeRecordEC2TerminationRow) { p.State = "active" }),
		mutate("draining_state", func(p *RuntimeRecordEC2TerminationRow) { p.State = "draining" }),
		mutate("terminating_state", func(p *RuntimeRecordEC2TerminationRow) { p.State = "terminating" }),
		mutate("empty_state", func(p *RuntimeRecordEC2TerminationRow) { p.State = "" }),
		mutate("other_evidence", func(p *RuntimeRecordEC2TerminationRow) { p.EvidenceID = "ec2-evidence-other" }),
		mutate("empty_evidence", func(p *RuntimeRecordEC2TerminationRow) { p.EvidenceID = "" }),
		mutate("negative_reclaimed", func(p *RuntimeRecordEC2TerminationRow) { p.ReclaimedClaimCount = -1 }),
		mutate("left_reclaiming_claims", func(p *RuntimeRecordEC2TerminationRow) {
			p.State, p.ReclaimedClaimCount = "left", 3
		}),
		mutate("zero_recorded_at", func(p *RuntimeRecordEC2TerminationRow) { p.RecordedAt = time.Time{} }),
	} {
		t.Run(test.name, func(t *testing.T) {
			transport, runner, _ := membershipStubTransport(membershipFenceValues(test.projection))
			runner.afterErr = errors.New("must not reach finish")
			result, err := membershipRecordTermination(context.Background(), transport)
			if err == nil || result != (RuntimeFenceResult{}) || errors.Is(err, runner.afterErr) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
	driverErr := errors.New("driver failed")
	valid := membershipFenceValues(membershipFenceProjection(identity.ReplicaID, "fenced", membershipEvidenceEvidenceID, 2))
	for _, test := range []struct {
		name   string
		runner *registrationRunnerStub
	}{
		{"runner", &registrationRunnerStub{err: driverErr}},
		{"finish_or_commit_after_decode", &registrationRunnerStub{queries: New(&registrationDBStub{values: valid}), afterErr: driverErr}},
		{"query", &registrationRunnerStub{queries: New(&registrationDBStub{err: driverErr})}},
		{"required_null", &registrationRunnerStub{queries: New(&registrationDBStub{values: append([]any{nil}, valid[1:]...)})}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := membershipRecordTermination(context.Background(), &runtimeMembershipEvidenceTransport{runner: test.runner})
			if err == nil || result != (RuntimeFenceResult{}) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			if test.name != "required_null" && !errors.Is(err, driverErr) {
				t.Fatalf("driver cause not wrapped: %v", err)
			}
		})
	}
}

// membershipProofInput is one complete argument set for the termination
// proof, so a rejection case names only the field it corrupts.
type membershipProofInput struct {
	replicaID                              uuid.UUID
	instance, release, requestID, evidence string
	requested, observed                    time.Time
	observedState                          string
}

func membershipValidProofInput() membershipProofInput {
	i := registrationStoreIdentity()
	return membershipProofInput{replicaID: i.ReplicaID, instance: i.InstanceID, release: i.ReleaseDigest,
		requestID: membershipEvidenceRequestID, evidence: membershipEvidenceEvidenceID,
		requested: membershipEvidenceRequestedAt, observed: membershipEvidenceObservedAt, observedState: "terminated"}
}

func TestRuntimeMembershipEvidenceRejectsProofInputsWithoutQuery(t *testing.T) {
	for _, test := range []struct {
		name       string
		corrupt    func(*membershipProofInput)
		nilContext bool
	}{
		{name: "nil_context", corrupt: func(*membershipProofInput) {}, nilContext: true},
		{name: "nil_replica", corrupt: func(i *membershipProofInput) { i.replicaID = uuid.Nil }},
		{name: "empty_instance", corrupt: func(i *membershipProofInput) { i.instance = "" }},
		{name: "malformed_instance", corrupt: func(i *membershipProofInput) { i.instance = "i-not-an-instanceX" }},
		{name: "short_instance", corrupt: func(i *membershipProofInput) { i.instance = "i-1111111111111111" }},
		{name: "empty_release", corrupt: func(i *membershipProofInput) { i.release = "" }},
		{name: "malformed_release", corrupt: func(i *membershipProofInput) { i.release = "sha256:" + strings.Repeat("z", 64) }},
		{name: "empty_request", corrupt: func(i *membershipProofInput) { i.requestID = "" }},
		{name: "long_request", corrupt: func(i *membershipProofInput) { i.requestID = strings.Repeat("r", 129) }},
		{name: "control_character_request", corrupt: func(i *membershipProofInput) { i.requestID = "req\t1" }},
		{name: "high_byte_request", corrupt: func(i *membershipProofInput) { i.requestID = "req\u00e91" }},
		{name: "empty_evidence", corrupt: func(i *membershipProofInput) { i.evidence = "" }},
		{name: "long_evidence", corrupt: func(i *membershipProofInput) { i.evidence = strings.Repeat("e", 129) }},
		{name: "control_character_evidence", corrupt: func(i *membershipProofInput) { i.evidence = "ec2\n1" }},
		{name: "zero_requested_at", corrupt: func(i *membershipProofInput) { i.requested = time.Time{} }},
		{name: "zero_observed_at", corrupt: func(i *membershipProofInput) { i.observed = time.Time{} }},
		{name: "observed_before_requested", corrupt: func(i *membershipProofInput) {
			i.observed = i.requested.Add(-time.Nanosecond)
		}},
		{name: "empty_observed_state", corrupt: func(i *membershipProofInput) { i.observedState = "" }},
		{name: "running_observed_state", corrupt: func(i *membershipProofInput) { i.observedState = "running" }},
		{name: "shutting_down_observed_state", corrupt: func(i *membershipProofInput) { i.observedState = "shutting-down" }},
		{name: "padded_observed_state", corrupt: func(i *membershipProofInput) { i.observedState = "terminated " }},
		{name: "upper_case_observed_state", corrupt: func(i *membershipProofInput) { i.observedState = "TERMINATED" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := membershipValidProofInput()
			test.corrupt(&input)
			ctx := context.Background()
			if test.nilContext {
				ctx = nil
			}
			runner := &registrationRunnerStub{}
			//nolint:staticcheck // Explicit nil-context rejection is part of the boundary.
			result, err := (&runtimeMembershipEvidenceTransport{runner: runner}).RecordEC2Termination(ctx, input.replicaID,
				input.instance, input.release, input.requestID, input.evidence, input.requested, input.observed, input.observedState)
			if err == nil || result != (RuntimeFenceResult{}) || runner.calls != 0 {
				t.Fatalf("result=%+v error=%v calls=%d", result, err, runner.calls)
			}
		})
	}
	result, err := membershipRecordTermination(context.Background(), NewRuntimeMembershipEvidenceTransport(nil))
	if err == nil || result != (RuntimeFenceResult{}) {
		t.Fatalf("nil pool result=%+v error=%v", result, err)
	}
	result, err = membershipRecordTermination(context.Background(), &runtimeMembershipEvidenceTransport{})
	if err == nil || result != (RuntimeFenceResult{}) {
		t.Fatalf("nil runner result=%+v error=%v", result, err)
	}
}

func TestRuntimeMembershipEvidenceEntryFunctionFinishCommitOrder(t *testing.T) {
	identity := registrationStoreIdentity()
	leave := membershipLeaveProjection(identity.ReplicaID, membershipEvidenceOperationID, membershipEvidenceExpected)
	fence := membershipFenceProjection(identity.ReplicaID, "fenced", membershipEvidenceEvidenceID, 4)
	call := func(runner WriteTxRunner, method membershipLeaveMethod) (RuntimeLeaveResult, error) {
		return method.call(&runtimeMembershipEvidenceTransport{runner: runner}, context.Background(), identity.ReplicaID,
			identity.InstanceID, identity.ReleaseDigest, membershipEvidenceExpected, membershipEvidenceOperationID)
	}
	serving, maintenance := membershipLeaveMethods()[0], membershipLeaveMethods()[1]

	runner, lease := claimOrderRunner(membershipLeaveValues(leave))
	result, err := call(runner, serving)
	if err != nil || result.State != "left" || lease.tx.functions != 1 {
		t.Fatalf("serving leave result=%+v error=%v functions=%d", result, err, lease.tx.functions)
	}
	assertEvents(t, lease.events, "acquire", "begin", "entry", "function:runtime_finish_serving_graceful_leave", "finish", "commit", "release")

	invalid := membershipLeaveProjection(identity.ReplicaID, membershipEvidenceOperationID, membershipEvidenceExpected)
	invalid.State = "draining"
	runner, lease = claimOrderRunner(membershipLeaveValues(invalid))
	result, err = call(runner, maintenance)
	if err == nil || result != (RuntimeLeaveResult{}) {
		t.Fatalf("invalid leave result=%+v error=%v", result, err)
	}
	assertEvents(t, lease.events, "acquire", "begin", "entry", "function:runtime_finish_maintenance_graceful_leave", "rollback", "release")

	runner, lease = claimOrderRunner(membershipLeaveValues(leave))
	lease.tx.finishErr = errors.New("finish lost")
	result, err = call(runner, maintenance)
	if !errors.Is(err, lease.tx.finishErr) || result != (RuntimeLeaveResult{}) {
		t.Fatalf("finish failure result=%+v error=%v", result, err)
	}
	assertEvents(t, lease.events, "acquire", "begin", "entry", "function:runtime_finish_maintenance_graceful_leave", "finish", "rollback", "destroy")

	runner, lease = claimOrderRunner(membershipFenceValues(fence))
	proof, err := membershipRecordTermination(context.Background(), &runtimeMembershipEvidenceTransport{runner: runner})
	if err != nil || proof.State != "fenced" || proof.ReclaimedClaimCount != 4 || lease.tx.functions != 1 {
		t.Fatalf("proof result=%+v error=%v functions=%d", proof, err, lease.tx.functions)
	}
	assertEvents(t, lease.events, "acquire", "begin", "entry", "function:runtime_record_ec2_termination", "finish", "commit", "release")

	runner, lease = claimOrderRunner(membershipFenceValues(fence))
	lease.tx.commitErr = errors.New("commit ambiguous")
	proof, err = membershipRecordTermination(context.Background(), &runtimeMembershipEvidenceTransport{runner: runner})
	if !errors.Is(err, lease.tx.commitErr) || proof != (RuntimeFenceResult{}) {
		t.Fatalf("commit failure result=%+v error=%v", proof, err)
	}
	assertEvents(t, lease.events, "acquire", "begin", "entry", "function:runtime_record_ec2_termination", "finish", "commit", "destroy")

	runner, lease = claimOrderRunner(nil)
	lease.tx.queryErr = &pgconn.PgError{Code: "AM001", Message: "membership fencing proof is corrupt"}
	proof, err = membershipRecordTermination(context.Background(), &runtimeMembershipEvidenceTransport{runner: runner})
	if !isSQLState(err, "AM001") || proof != (RuntimeFenceResult{}) {
		t.Fatalf("AM001 result=%+v error=%v", proof, err)
	}
	assertEvents(t, lease.events, "acquire", "begin", "entry", "function:runtime_record_ec2_termination", "rollback", "destroy")

	runner, lease = claimOrderRunner(nil)
	lease.tx.queryErr = &pgconn.PgError{Code: "55000", Message: "membership leave work is unjoined"}
	result, err = call(runner, serving)
	if !isSQLState(err, "55000") || result != (RuntimeLeaveResult{}) {
		t.Fatalf("55000 result=%+v error=%v", result, err)
	}
	// A definite 55000 rejection is not durable corruption, so the connection
	// returns to the pool instead of being retired.
	assertEvents(t, lease.events, "acquire", "begin", "entry", "function:runtime_finish_serving_graceful_leave", "rollback", "release")
}

// TestRuntimeMembershipEvidenceInterfaceExposesOnlyScalars pins the fixed
// transport surface: three context-first scalar methods returning one of two
// owned result types, with no callback, Queries, transaction, connection or
// row handle anywhere in the signature, and no reference field inside either
// result.
func TestRuntimeMembershipEvidenceInterfaceExposesOnlyScalars(t *testing.T) {
	surface := reflect.TypeOf((*RuntimeMembershipEvidenceTransport)(nil)).Elem()
	if surface.NumMethod() != 3 {
		t.Fatalf("transport exposes %d methods", surface.NumMethod())
	}
	contextType := reflect.TypeOf((*context.Context)(nil)).Elem()
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	valueTypes := map[reflect.Type]bool{reflect.TypeOf(uuid.UUID{}): true, reflect.TypeOf(time.Time{}): true}
	results := map[reflect.Type]bool{reflect.TypeOf(RuntimeLeaveResult{}): true, reflect.TypeOf(RuntimeFenceResult{}): true}
	scalars := map[reflect.Kind]bool{reflect.Int64: true, reflect.String: true}
	for i := 0; i < surface.NumMethod(); i++ {
		method := surface.Method(i)
		if method.Type.NumIn() < 3 || method.Type.In(0) != contextType {
			t.Fatalf("%s is not context-first: %v", method.Name, method.Type)
		}
		for argument := 1; argument < method.Type.NumIn(); argument++ {
			parameter := method.Type.In(argument)
			if !valueTypes[parameter] && !scalars[parameter.Kind()] {
				t.Fatalf("%s argument %d is %v", method.Name, argument, parameter)
			}
		}
		if method.Type.NumOut() != 2 || !results[method.Type.Out(0)] || method.Type.Out(1) != errorType {
			t.Fatalf("%s returns %v", method.Name, method.Type)
		}
	}
	for result := range results {
		for field := 0; field < result.NumField(); field++ {
			switch result.Field(field).Type.Kind() {
			case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func, reflect.Interface, reflect.UnsafePointer:
				t.Fatalf("%s field %s is a reference: %v", result, result.Field(field).Name, result.Field(field).Type)
			default:
			}
		}
	}
}

const (
	membershipLeaveOperationID       = "op-scale-in"
	membershipMaintenanceOperationID = "op-maintenance"
)

// membershipFencingProofPool returns a single-connection pool authenticated as
// aboutme_fencing_proof, the only login allowed to execute the exact EC2
// termination proof.
func membershipFencingProofPool(t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 1
	cfg.MinConns = 0
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, authErr := conn.Exec(ctx, `SET SESSION AUTHORIZATION aboutme_fencing_proof`)
		return authErr
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// membershipLiveEvidence reports the durable membership evidence one live
// database holds for the exact replica: its state, the capacity and controller
// generations, and the receipt and proof counts.
func membershipLiveEvidence(t *testing.T, admin *sql.DB, replicaID uuid.UUID) (string, int64, int64, int, int) {
	t.Helper()
	var state string
	var generation, controller int64
	var receipts, proofs int
	if err := admin.QueryRowContext(context.Background(),
		`SELECT (SELECT state FROM public.runtime_replicas WHERE replica_id=$1),`+
			`(SELECT generation FROM public.runtime_capacity WHERE singleton),`+
			`(SELECT controller_generation FROM public.runtime_capacity WHERE singleton),`+
			`(SELECT count(*) FROM public.runtime_leave_receipts WHERE replica_id=$1),`+
			`(SELECT count(*) FROM public.runtime_fencing_proofs WHERE replica_id=$1)`,
		replicaID).Scan(&state, &generation, &controller, &receipts, &proofs); err != nil {
		t.Fatal(err)
	}
	return state, generation, controller, receipts, proofs
}

// membershipLiveDrainingServing builds a live two-replica fleet and drains the
// second serving incarnation through the accepted controller path, which
// leaves exactly the immutable prepare_scale_in evidence a serving graceful
// leave requires. It returns the database, the draining incarnation and the
// prepare step's stored result generation.
func membershipLiveDrainingServing(t *testing.T, n int) (string, *sql.DB, RuntimeReplicaIdentity, int64) {
	t.Helper()
	dsn, admin := testutil.NewMigratedTestDatabase(t)
	ctx := context.Background()
	registration := NewRuntimeReplicaRegistrationStore(newWriteRunnerAppPool(t, dsn, nil))
	lifecycle := NewRuntimeLifecycleTransport(lifecycleCommandPool(t, dsn))
	first, second := lifecycleLiveIdentity(n), lifecycleLiveIdentity(n+1)

	lifecycleLiveJoin(t, registration, first, "serving")
	if activated, err := lifecycle.ActivateReplicaCapacity(ctx, 1, "op-initial", first.ReplicaID, first.InstanceID,
		first.ContainerInstanceARN, first.CaddyTaskARN, first.GoTaskARN, first.NuxtTaskARN, first.ReleaseDigest, nil); err != nil || activated.State != "active" {
		t.Fatalf("initial activation=%+v error=%v", activated, err)
	}
	if booked, err := lifecycle.PrepareScaleOut(ctx, 2, "op-scale-out"); err != nil || booked.ControllerGeneration != 3 {
		t.Fatalf("scale out=%+v error=%v", booked, err)
	}
	lifecycleLiveJoin(t, registration, second, "serving")
	if grown, err := lifecycle.ActivateReplicaCapacity(ctx, 3, "op-scale-out", second.ReplicaID, second.InstanceID,
		second.ContainerInstanceARN, second.CaddyTaskARN, second.GoTaskARN, second.NuxtTaskARN, second.ReleaseDigest, nil); err != nil || grown.State != "active" {
		t.Fatalf("second activation=%+v error=%v", grown, err)
	}
	drained, err := lifecycle.PrepareScaleIn(ctx, 4, membershipLeaveOperationID, second.ReplicaID, second.InstanceID, second.ReleaseDigest)
	if err != nil || drained.State != "draining" || drained.ControllerGeneration != 5 {
		t.Fatalf("scale in=%+v error=%v", drained, err)
	}
	return dsn, admin, second, drained.ControllerGeneration
}

func TestRuntimeMembershipEvidenceLiveServingGracefulLeaveAndReplay(t *testing.T) {
	dsn, admin, identity, expected := membershipLiveDrainingServing(t, 41)
	ctx := context.Background()
	evidence := NewRuntimeMembershipEvidenceTransport(newWriteRunnerAppPool(t, dsn, nil))
	_, beforeGeneration, _, _, _ := membershipLiveEvidence(t, admin, identity.ReplicaID)

	fresh, err := evidence.FinishServingGracefulLeave(ctx, identity.ReplicaID, identity.InstanceID,
		identity.ReleaseDigest, expected, membershipLeaveOperationID)
	if err != nil || fresh.ReplicaID != identity.ReplicaID || fresh.State != "left" ||
		fresh.ReceiptOperationID != membershipLeaveOperationID || fresh.ControllerGeneration != expected ||
		fresh.JoinedTransitionCount != 0 || fresh.JoinedClaimCount != 0 || fresh.RecordedAt.IsZero() || fresh.Replayed {
		t.Fatalf("fresh leave=%+v error=%v", fresh, err)
	}
	retained := fresh
	state, generation, controller, receipts, proofs := membershipLiveEvidence(t, admin, identity.ReplicaID)
	if state != "left" || generation != beforeGeneration+1 || controller != expected || receipts != 1 || proofs != 0 {
		t.Fatalf("after leave state=%q generation=%d controller=%d receipts=%d proofs=%d", state, generation, controller, receipts, proofs)
	}

	replay, err := evidence.FinishServingGracefulLeave(ctx, identity.ReplicaID, identity.InstanceID,
		identity.ReleaseDigest, expected, membershipLeaveOperationID)
	if err != nil || !replay.Replayed || replay.State != "left" || replay.ReceiptOperationID != fresh.ReceiptOperationID ||
		replay.ControllerGeneration != fresh.ControllerGeneration || !replay.RecordedAt.Equal(fresh.RecordedAt) {
		t.Fatalf("replayed leave=%+v fresh=%+v error=%v", replay, fresh, err)
	}
	if _, replayGeneration, _, _, _ := membershipLiveEvidence(t, admin, identity.ReplicaID); replayGeneration != generation {
		t.Fatalf("replay advanced the capacity generation to %d", replayGeneration)
	}

	for _, test := range []struct {
		name      string
		expected  int64
		operation string
	}{
		{"changed_generation", expected + 1, membershipLeaveOperationID},
		{"changed_operation", expected, "op-other-scale-in"},
	} {
		t.Run(test.name, func(t *testing.T) {
			conflict, conflictErr := evidence.FinishServingGracefulLeave(ctx, identity.ReplicaID, identity.InstanceID,
				identity.ReleaseDigest, test.expected, test.operation)
			if !isSQLState(conflictErr, "AM002") || conflict != (RuntimeLeaveResult{}) {
				t.Fatalf("conflict=%+v error=%v", conflict, conflictErr)
			}
		})
	}
	if retained != fresh || retained.ReceiptOperationID != membershipLeaveOperationID {
		t.Fatalf("retained result changed after connection reuse: retained=%+v fresh=%+v", retained, fresh)
	}
}

func TestRuntimeMembershipEvidenceLiveMaintenanceGracefulLeaveAndReplay(t *testing.T) {
	dsn, admin := testutil.NewMigratedTestDatabase(t)
	ctx := context.Background()
	serving, maintenance := lifecycleLiveIdentity(51), lifecycleLiveIdentity(52)
	lifecycle := NewRuntimeLifecycleTransport(lifecycleCommandPool(t, dsn))
	maintenancePool := newMaintenancePool(t, dsn)

	lifecycleLiveJoin(t, NewRuntimeReplicaRegistrationStore(newWriteRunnerAppPool(t, dsn, nil)), serving, "serving")
	if activated, err := lifecycle.ActivateReplicaCapacity(ctx, 1, "op-initial", serving.ReplicaID, serving.InstanceID,
		serving.ContainerInstanceARN, serving.CaddyTaskARN, serving.GoTaskARN, serving.NuxtTaskARN, serving.ReleaseDigest, nil); err != nil || activated.State != "active" {
		t.Fatalf("initial activation=%+v error=%v", activated, err)
	}
	lifecycleMaintenanceWakeFixture(t, admin, membershipMaintenanceOperationID, 2)
	lifecycleLiveJoin(t, NewRuntimeReplicaRegistrationStore(maintenancePool), maintenance, "maintenance")
	if joined, err := lifecycle.ActivateReplicaCapacity(ctx, 4, membershipMaintenanceOperationID, maintenance.ReplicaID,
		maintenance.InstanceID, maintenance.ContainerInstanceARN, maintenance.CaddyTaskARN, maintenance.GoTaskARN,
		maintenance.NuxtTaskARN, maintenance.ReleaseDigest, nil); err != nil || joined.ReplicaKind != "maintenance" {
		t.Fatalf("maintenance activation=%+v error=%v", joined, err)
	}
	drained, err := lifecycle.PrepareMaintenanceDrain(ctx, 5, membershipMaintenanceOperationID, maintenance.ReplicaID,
		maintenance.InstanceID, maintenance.ReleaseDigest)
	if err != nil || drained.State != "draining" || drained.ControllerGeneration != 6 {
		t.Fatalf("maintenance drain=%+v error=%v", drained, err)
	}

	evidence := NewRuntimeMembershipEvidenceTransport(maintenancePool)
	_, beforeGeneration, _, _, _ := membershipLiveEvidence(t, admin, maintenance.ReplicaID)
	fresh, err := evidence.FinishMaintenanceGracefulLeave(ctx, maintenance.ReplicaID, maintenance.InstanceID,
		maintenance.ReleaseDigest, drained.ControllerGeneration, membershipMaintenanceOperationID)
	if err != nil || fresh.ReplicaID != maintenance.ReplicaID || fresh.State != "left" ||
		fresh.ReceiptOperationID != membershipMaintenanceOperationID ||
		fresh.ControllerGeneration != drained.ControllerGeneration || fresh.JoinedTransitionCount != 0 ||
		fresh.JoinedClaimCount != 0 || fresh.RecordedAt.IsZero() || fresh.Replayed {
		t.Fatalf("fresh maintenance leave=%+v error=%v", fresh, err)
	}
	state, generation, controller, receipts, _ := membershipLiveEvidence(t, admin, maintenance.ReplicaID)
	if state != "left" || generation != beforeGeneration+1 || controller != drained.ControllerGeneration || receipts != 1 {
		t.Fatalf("after maintenance leave state=%q generation=%d controller=%d receipts=%d", state, generation, controller, receipts)
	}
	replay, err := evidence.FinishMaintenanceGracefulLeave(ctx, maintenance.ReplicaID, maintenance.InstanceID,
		maintenance.ReleaseDigest, drained.ControllerGeneration, membershipMaintenanceOperationID)
	if err != nil || !replay.Replayed || !replay.RecordedAt.Equal(fresh.RecordedAt) || replay.State != "left" {
		t.Fatalf("replayed maintenance leave=%+v error=%v", replay, err)
	}
	// The serving replica the maintenance campaign never named is untouched.
	if servingState, _, _, servingReceipts, _ := membershipLiveEvidence(t, admin, serving.ReplicaID); servingState != "active" || servingReceipts != 0 {
		t.Fatalf("maintenance leave changed the serving replica: state=%q receipts=%d", servingState, servingReceipts)
	}
}

func TestRuntimeMembershipEvidenceLiveTerminationProofReclaimsClaims(t *testing.T) {
	dsn, admin := claimLiveDatabase(t)
	ctx := context.Background()
	identity := registrationStoreIdentity()
	claims := NewRuntimeClaimTransport(newWriteRunnerAppPool(t, dsn, nil))
	work, scope := claimStoreWorkID, claimStoreDigest(0x44)
	waitingID := uuid.MustParse("44444444-4444-4444-8444-444444444444")

	if running, err := claims.AcquireSingle(ctx, claimStoreClaimID, "render.global_claim", identity.ReplicaID, &work, "global", scope); err != nil || running.Outcome != "running" {
		t.Fatalf("running claim=%+v error=%v", running, err)
	}
	if waiting, err := claims.AcquireSingle(ctx, waitingID, "render.global_claim", identity.ReplicaID, &work, "global", scope); err != nil || waiting.Outcome != "waiting" {
		t.Fatalf("waiting claim=%+v error=%v", waiting, err)
	}

	evidence := NewRuntimeMembershipEvidenceTransport(membershipFencingProofPool(t, dsn))
	_, beforeGeneration, _, _, _ := membershipLiveEvidence(t, admin, identity.ReplicaID)
	fresh, err := evidence.RecordEC2Termination(ctx, identity.ReplicaID, identity.InstanceID, identity.ReleaseDigest,
		membershipEvidenceRequestID, membershipEvidenceEvidenceID, membershipEvidenceRequestedAt,
		membershipEvidenceObservedAt, "terminated")
	if err != nil || fresh.ReplicaID != identity.ReplicaID || fresh.State != "fenced" ||
		fresh.EvidenceID != membershipEvidenceEvidenceID || fresh.ReclaimedClaimCount != 2 ||
		fresh.RecordedAt.IsZero() || fresh.Replayed {
		t.Fatalf("fresh proof=%+v error=%v", fresh, err)
	}
	// The raw external evidence is stored but never becomes ordering
	// authority, so the durable proof time stays behind both 2030 timestamps.
	if !fresh.RecordedAt.Before(membershipEvidenceRequestedAt) {
		t.Fatalf("external evidence dragged recorded_at forward to %v", fresh.RecordedAt)
	}
	state, generation, _, receipts, proofs := membershipLiveEvidence(t, admin, identity.ReplicaID)
	if state != "fenced" || generation != beforeGeneration+1 || receipts != 0 || proofs != 1 {
		t.Fatalf("after proof state=%q generation=%d receipts=%d proofs=%d", state, generation, receipts, proofs)
	}
	var released, fenced int
	if countErr := admin.QueryRowContext(ctx,
		`SELECT count(*) FILTER (WHERE state='released'),count(*) FILTER (WHERE release_reason='fenced')`+
			` FROM public.shared_claim_requests WHERE replica_id=$1`, identity.ReplicaID).Scan(&released, &fenced); countErr != nil || released != 2 || fenced != 2 {
		t.Fatalf("released=%d fenced=%d error=%v", released, fenced, countErr)
	}

	replay, err := evidence.RecordEC2Termination(ctx, identity.ReplicaID, identity.InstanceID, identity.ReleaseDigest,
		membershipEvidenceRequestID, membershipEvidenceEvidenceID, membershipEvidenceRequestedAt,
		membershipEvidenceObservedAt, "terminated")
	if err != nil || !replay.Replayed || replay.State != "fenced" || replay.ReclaimedClaimCount != 2 ||
		!replay.RecordedAt.Equal(fresh.RecordedAt) || replay.EvidenceID != fresh.EvidenceID {
		t.Fatalf("replayed proof=%+v fresh=%+v error=%v", replay, fresh, err)
	}
	if _, replayGeneration, _, _, replayProofs := membershipLiveEvidence(t, admin, identity.ReplicaID); replayGeneration != generation || replayProofs != 1 {
		t.Fatalf("replay advanced generation to %d with %d proofs", replayGeneration, replayProofs)
	}
	conflict, err := evidence.RecordEC2Termination(ctx, identity.ReplicaID, identity.InstanceID, identity.ReleaseDigest,
		membershipEvidenceRequestID, "ec2-evidence-other", membershipEvidenceRequestedAt,
		membershipEvidenceObservedAt, "terminated")
	if !isSQLState(err, "AM002") || conflict != (RuntimeFenceResult{}) {
		t.Fatalf("changed evidence conflict=%+v error=%v", conflict, err)
	}
}

func TestRuntimeMembershipEvidenceLiveRoleBoundaryInBothDirections(t *testing.T) {
	dsn, admin, identity, expected := membershipLiveDrainingServing(t, 61)
	ctx := context.Background()
	appPool := newWriteRunnerAppPool(t, dsn, nil)
	pools := map[string]*pgxpool.Pool{
		"app":           appPool,
		"maintenance":   newMaintenancePool(t, dsn),
		"fencing_proof": membershipFencingProofPool(t, dsn),
		"lifecycle":     lifecycleCommandPool(t, dsn),
	}
	// Every role is denied every wrapper it does not own, including the role
	// that owns one of the other two. The data is leave-ready throughout, so a
	// denial can only come from the login.
	for _, test := range []struct {
		role, wrapper string
		call          func(RuntimeMembershipEvidenceTransport) error
	}{
		{"app", "maintenance_leave", func(s RuntimeMembershipEvidenceTransport) error {
			result, err := s.FinishMaintenanceGracefulLeave(ctx, identity.ReplicaID, identity.InstanceID, identity.ReleaseDigest, expected, membershipLeaveOperationID)
			if result != (RuntimeLeaveResult{}) {
				t.Fatalf("denied call returned %+v", result)
			}
			return err
		}},
		{"maintenance", "serving_leave", func(s RuntimeMembershipEvidenceTransport) error {
			result, err := s.FinishServingGracefulLeave(ctx, identity.ReplicaID, identity.InstanceID, identity.ReleaseDigest, expected, membershipLeaveOperationID)
			if result != (RuntimeLeaveResult{}) {
				t.Fatalf("denied call returned %+v", result)
			}
			return err
		}},
		{"lifecycle", "serving_leave", func(s RuntimeMembershipEvidenceTransport) error {
			_, err := s.FinishServingGracefulLeave(ctx, identity.ReplicaID, identity.InstanceID, identity.ReleaseDigest, expected, membershipLeaveOperationID)
			return err
		}},
		{"lifecycle", "maintenance_leave", func(s RuntimeMembershipEvidenceTransport) error {
			_, err := s.FinishMaintenanceGracefulLeave(ctx, identity.ReplicaID, identity.InstanceID, identity.ReleaseDigest, expected, membershipLeaveOperationID)
			return err
		}},
		{"fencing_proof", "serving_leave", func(s RuntimeMembershipEvidenceTransport) error {
			_, err := s.FinishServingGracefulLeave(ctx, identity.ReplicaID, identity.InstanceID, identity.ReleaseDigest, expected, membershipLeaveOperationID)
			return err
		}},
		{"fencing_proof", "maintenance_leave", func(s RuntimeMembershipEvidenceTransport) error {
			_, err := s.FinishMaintenanceGracefulLeave(ctx, identity.ReplicaID, identity.InstanceID, identity.ReleaseDigest, expected, membershipLeaveOperationID)
			return err
		}},
		{"app", "termination_proof", func(s RuntimeMembershipEvidenceTransport) error {
			result, err := s.RecordEC2Termination(ctx, identity.ReplicaID, identity.InstanceID, identity.ReleaseDigest,
				membershipEvidenceRequestID, membershipEvidenceEvidenceID, membershipEvidenceRequestedAt, membershipEvidenceObservedAt, "terminated")
			if result != (RuntimeFenceResult{}) {
				t.Fatalf("denied call returned %+v", result)
			}
			return err
		}},
		{"maintenance", "termination_proof", func(s RuntimeMembershipEvidenceTransport) error {
			_, err := s.RecordEC2Termination(ctx, identity.ReplicaID, identity.InstanceID, identity.ReleaseDigest,
				membershipEvidenceRequestID, membershipEvidenceEvidenceID, membershipEvidenceRequestedAt, membershipEvidenceObservedAt, "terminated")
			return err
		}},
		{"lifecycle", "termination_proof", func(s RuntimeMembershipEvidenceTransport) error {
			_, err := s.RecordEC2Termination(ctx, identity.ReplicaID, identity.InstanceID, identity.ReleaseDigest,
				membershipEvidenceRequestID, membershipEvidenceEvidenceID, membershipEvidenceRequestedAt, membershipEvidenceObservedAt, "terminated")
			return err
		}},
	} {
		t.Run(test.role+"_denied_"+test.wrapper, func(t *testing.T) {
			if err := test.call(NewRuntimeMembershipEvidenceTransport(pools[test.role])); !isSQLState(err, "42501") {
				t.Fatalf("error=%v", err)
			}
		})
	}
	state, generation, _, receipts, proofs := membershipLiveEvidence(t, admin, identity.ReplicaID)
	if state != "draining" || receipts != 0 || proofs != 0 {
		t.Fatalf("denied roles wrote evidence: state=%q receipts=%d proofs=%d", state, receipts, proofs)
	}

	// The other direction: the same data the denied roles could not touch is
	// accepted from the one login that owns the serving wrapper.
	fresh, err := NewRuntimeMembershipEvidenceTransport(appPool).FinishServingGracefulLeave(ctx, identity.ReplicaID,
		identity.InstanceID, identity.ReleaseDigest, expected, membershipLeaveOperationID)
	if err != nil || fresh.State != "left" || fresh.Replayed {
		t.Fatalf("owner call=%+v error=%v", fresh, err)
	}
	if leftState, leftGeneration, _, leftReceipts, _ := membershipLiveEvidence(t, admin, identity.ReplicaID); leftState != "left" ||
		leftGeneration != generation+1 || leftReceipts != 1 {
		t.Fatalf("owner call state=%q generation=%d receipts=%d", leftState, leftGeneration, leftReceipts)
	}
}

func TestRuntimeMembershipEvidenceLiveFinishFailureRollsBackAndReturnsZero(t *testing.T) {
	dsn, admin, identity, expected := membershipLiveDrainingServing(t, 71)
	wantErr := errors.New("simulated lost membership finish response")
	runner := liveWrappedRunner(newWriteRunnerAppPool(t, dsn, nil), func(tx pgx.Tx) pgx.Tx {
		return &registrationFinishResponseLostTx{Tx: tx, err: wantErr}
	})
	_, beforeGeneration, _, _, _ := membershipLiveEvidence(t, admin, identity.ReplicaID)
	result, err := (&runtimeMembershipEvidenceTransport{runner: runner}).FinishServingGracefulLeave(context.Background(),
		identity.ReplicaID, identity.InstanceID, identity.ReleaseDigest, expected, membershipLeaveOperationID)
	if !errors.Is(err, wantErr) || result != (RuntimeLeaveResult{}) {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if state, generation, _, receipts, _ := membershipLiveEvidence(t, admin, identity.ReplicaID); state != "draining" ||
		generation != beforeGeneration || receipts != 0 {
		t.Fatalf("rolled-back state=%q generation=%d receipts=%d", state, generation, receipts)
	}
}

func TestRuntimeMembershipEvidenceLiveCommitAmbiguityReturnsZeroWithoutAuthority(t *testing.T) {
	dsn, admin, identity, expected := membershipLiveDrainingServing(t, 81)
	wantErr := errors.New("simulated lost membership commit response")
	pool := newWriteRunnerAppPool(t, dsn, nil)
	runner := liveWrappedRunner(pool, func(tx pgx.Tx) pgx.Tx { return &commitResponseLostTx{Tx: tx, err: wantErr} })
	_, beforeGeneration, _, _, _ := membershipLiveEvidence(t, admin, identity.ReplicaID)
	var oldPID int32
	if err := NewWriteTxRunner(pool).ExecWrite(context.Background(), func(q *Queries) error {
		return q.db.QueryRow(context.Background(), `SELECT pg_backend_pid()`).Scan(&oldPID)
	}); err != nil {
		t.Fatal(err)
	}
	result, err := (&runtimeMembershipEvidenceTransport{runner: runner}).FinishServingGracefulLeave(context.Background(),
		identity.ReplicaID, identity.InstanceID, identity.ReleaseDigest, expected, membershipLeaveOperationID)
	if !errors.Is(err, wantErr) || result != (RuntimeLeaveResult{}) {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	// The leave committed. The caller receives no authority for it, the
	// transport neither retries nor replays, and the ambiguous connection is
	// retired.
	if state, generation, _, receipts, _ := membershipLiveEvidence(t, admin, identity.ReplicaID); state != "left" ||
		generation != beforeGeneration+1 || receipts != 1 {
		t.Fatalf("committed state=%q generation=%d receipts=%d", state, generation, receipts)
	}
	assertBackendGone(t, admin, oldPID)
}
