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
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	claimStoreClaimID   = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	claimStoreReplicaID = uuid.MustParse("22222222-2222-4222-8222-222222222222")
	claimStoreWorkID    = uuid.MustParse("33333333-3333-4333-8333-333333333333")
)

func claimStoreDigest(fill byte) [32]byte {
	var digest [32]byte
	for i := range digest {
		digest[i] = fill
	}
	return digest
}

func claimStoreDigestBytes(fill byte) []byte {
	digest := claimStoreDigest(fill)
	return digest[:]
}

func claimProjectionValues(p runtimeClaimProjection) []any {
	return []any{p.Outcome, p.ClaimID, p.PolicyIDValue, p.PolicyIDPresent, p.ReplicaIDValue, p.ReplicaIDPresent, p.WorkIDValue, p.WorkIDPresent, p.StateValue, p.StatePresent, p.AdmittedAtValue, p.AdmittedAtPresent, p.DeadlineAtValue, p.DeadlineAtPresent, p.ReleasedAtValue, p.ReleasedAtPresent, p.ReleaseReasonValue, p.ReleaseReasonPresent, p.RequestDigestValue, p.RequestDigestPresent, p.ScopeCountValue, p.ScopeCountPresent, p.Scope1KindValue, p.Scope1KindPresent, p.Scope1DigestValue, p.Scope1DigestPresent, p.Scope1AllocationOrdinalValue, p.Scope1AllocationOrdinalPresent, p.Scope2KindValue, p.Scope2KindPresent, p.Scope2DigestValue, p.Scope2DigestPresent, p.Scope2AllocationOrdinalValue, p.Scope2AllocationOrdinalPresent}
}

func storedClaimProjection(outcome, policy string) runtimeClaimProjection {
	admitted := time.Date(2026, 9, 7, 8, 0, 0, 0, time.UTC)
	digest := claimStoreDigest(0x11)
	p := runtimeClaimProjection{
		Outcome: outcome, ClaimID: claimStoreClaimID,
		PolicyIDValue: policy, PolicyIDPresent: true,
		ReplicaIDValue: claimStoreReplicaID, ReplicaIDPresent: true,
		StateValue: outcome, StatePresent: true,
		AdmittedAtValue: admitted, AdmittedAtPresent: true,
		RequestDigestValue: digest[:], RequestDigestPresent: true,
		ScopeCountValue: 1, ScopeCountPresent: true,
		Scope1KindValue: "global", Scope1KindPresent: true,
		Scope1DigestValue: claimStoreDigestBytes(0x22), Scope1DigestPresent: true,
		Scope1AllocationOrdinalValue: 3, Scope1AllocationOrdinalPresent: true,
	}
	if policy == "render.global_claim" {
		p.WorkIDValue, p.WorkIDPresent = claimStoreWorkID, true
		p.DeadlineAtValue, p.DeadlineAtPresent = admitted.Add(20*time.Second), true
	}
	if policy == "sse.fleet_account_ip" {
		p.Scope1KindValue = "ip"
		p.ScopeCountValue = 2
		p.Scope2KindValue, p.Scope2KindPresent = "account", true
		p.Scope2DigestValue, p.Scope2DigestPresent = claimStoreDigestBytes(0x33), true
		p.Scope2AllocationOrdinalValue, p.Scope2AllocationOrdinalPresent = 5, true
	}
	if policy == "mcp.user_concurrent" {
		p.Scope1KindValue = "user"
	}
	if outcome == "released" {
		p.ReleasedAtValue, p.ReleasedAtPresent = admitted.Add(time.Minute), true
		p.ReleaseReasonValue, p.ReleaseReasonPresent = "joined", true
	}
	if outcome == "denied" {
		p.StateValue, p.StatePresent = "", false
		p.AdmittedAtValue, p.AdmittedAtPresent = time.Time{}, false
		p.DeadlineAtValue, p.DeadlineAtPresent = time.Time{}, false
		p.Scope1AllocationOrdinalValue, p.Scope1AllocationOrdinalPresent = 0, false
		p.Scope2AllocationOrdinalValue, p.Scope2AllocationOrdinalPresent = 0, false
	}
	return p
}

func absentClaimProjection() runtimeClaimProjection {
	return runtimeClaimProjection{Outcome: "absent", ClaimID: claimStoreClaimID}
}

func claimStubTransport(values []any) (*runtimeClaimTransport, *registrationRunnerStub, *registrationDBStub) {
	db := &registrationDBStub{values: values}
	runner := &registrationRunnerStub{queries: New(db)}
	return &runtimeClaimTransport{runner: runner}, runner, db
}

func TestRuntimeClaimTransportFiveOperationsCallOneFunctionEach(t *testing.T) {
	digest := claimStoreDigest(0x11)
	account := claimStoreDigest(0x33)
	for _, test := range []struct {
		name, sqlName, policy string
		call                  func(RuntimeClaimTransport) (RuntimeClaimRow, error)
	}{
		{"acquire_single", "runtime_acquire_single_claim", "render.global_claim", func(s RuntimeClaimTransport) (RuntimeClaimRow, error) {
			work := claimStoreWorkID
			return s.AcquireSingle(context.Background(), claimStoreClaimID, "render.global_claim", claimStoreReplicaID, &work, "global", digest)
		}},
		{"acquire_sse", "runtime_acquire_sse_claim", "sse.fleet_account_ip", func(s RuntimeClaimTransport) (RuntimeClaimRow, error) {
			return s.AcquireSSE(context.Background(), claimStoreClaimID, claimStoreReplicaID, digest, &account)
		}},
		{"promote", "runtime_promote_claim", "password.hash", func(s RuntimeClaimTransport) (RuntimeClaimRow, error) {
			return s.Promote(context.Background(), claimStoreClaimID, claimStoreReplicaID, digest)
		}},
		{"release", "runtime_release_claim", "mail.send", func(s RuntimeClaimTransport) (RuntimeClaimRow, error) {
			return s.Release(context.Background(), claimStoreClaimID, claimStoreReplicaID, digest, "joined")
		}},
		{"resolve", "runtime_resolve_claim", "mcp.user_concurrent", func(s RuntimeClaimTransport) (RuntimeClaimRow, error) {
			return s.Resolve(context.Background(), claimStoreClaimID, claimStoreReplicaID, digest)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			projection := storedClaimProjection("running", test.policy)
			transport, runner, db := claimStubTransport(claimProjectionValues(projection))
			row, err := test.call(transport)
			if err != nil || runner.calls != 1 || db.calls != 1 || !strings.Contains(db.sql, test.sqlName) {
				t.Fatalf("row=%+v error=%v runner=%d queries=%d sql=%q", row, err, runner.calls, db.calls, db.sql)
			}
			if row.Outcome != "running" || row.ClaimID != claimStoreClaimID || row.PolicyID == nil || *row.PolicyID != test.policy || row.ReplicaID == nil || *row.ReplicaID != claimStoreReplicaID || row.State == nil || *row.State != "running" || row.AdmittedAt == nil || !row.AdmittedAt.Equal(projection.AdmittedAtValue) || row.RequestDigest == nil || *row.RequestDigest != claimStoreDigest(0x11) || row.ScopeCount == nil || row.Scope1 == nil || row.Scope1.Digest != claimStoreDigest(0x22) || row.Scope1.AllocationOrdinal == nil || *row.Scope1.AllocationOrdinal != 3 || row.ReleasedAt != nil || row.ReleaseReason != nil {
				t.Fatalf("decoded row=%+v", row)
			}
			switch test.policy {
			case "render.global_claim":
				if row.WorkID == nil || *row.WorkID != claimStoreWorkID || row.DeadlineAt == nil || !row.DeadlineAt.Equal(projection.AdmittedAtValue.Add(20*time.Second)) {
					t.Fatalf("render row=%+v", row)
				}
			case "sse.fleet_account_ip":
				if *row.ScopeCount != 2 || row.Scope2 == nil || row.Scope2.Kind != "account" || row.Scope2.Digest != claimStoreDigest(0x33) || row.Scope2.AllocationOrdinal == nil || *row.Scope2.AllocationOrdinal != 5 {
					t.Fatalf("sse row=%+v", row)
				}
			default:
				if row.WorkID != nil || row.DeadlineAt != nil || row.Scope2 != nil || *row.ScopeCount != 1 {
					t.Fatalf("single row=%+v", row)
				}
			}
			projection.RequestDigestValue[0] = 0xEE
			projection.Scope1DigestValue[0] = 0xEE
			if *row.RequestDigest != claimStoreDigest(0x11) || row.Scope1.Digest != claimStoreDigest(0x22) {
				t.Fatal("decoded digests share driver buffers")
			}
		})
	}
}

func TestRuntimeClaimTransportDecodesEveryOutcome(t *testing.T) {
	for _, test := range []struct {
		name       string
		projection runtimeClaimProjection
		check      func(RuntimeClaimRow) bool
	}{
		{"waiting_render", storedClaimProjection("waiting", "render.global_claim"), func(r RuntimeClaimRow) bool {
			return r.Outcome == "waiting" && r.State != nil && *r.State == "waiting" && r.WorkID != nil && r.DeadlineAt != nil && r.ReleasedAt == nil
		}},
		{"released_mail", storedClaimProjection("released", "mail.send"), func(r RuntimeClaimRow) bool {
			return r.Outcome == "released" && r.ReleasedAt != nil && r.ReleaseReason != nil && *r.ReleaseReason == "joined" && r.Scope1.AllocationOrdinal != nil
		}},
		{"released_fenced", func() runtimeClaimProjection {
			p := storedClaimProjection("released", "mail.send")
			p.ReleaseReasonValue = "fenced"
			return p
		}(), func(r RuntimeClaimRow) bool {
			return r.ReleaseReason != nil && *r.ReleaseReason == "fenced"
		}},
		{"denied_render", storedClaimProjection("denied", "render.global_claim"), func(r RuntimeClaimRow) bool {
			return r.Outcome == "denied" && r.State == nil && r.AdmittedAt == nil && r.DeadlineAt == nil && r.WorkID != nil && r.RequestDigest != nil && r.Scope1 != nil && r.Scope1.AllocationOrdinal == nil
		}},
		{"denied_sse", storedClaimProjection("denied", "sse.fleet_account_ip"), func(r RuntimeClaimRow) bool {
			return r.Outcome == "denied" && r.Scope2 != nil && r.Scope2.AllocationOrdinal == nil && r.WorkID == nil
		}},
		{"absent", absentClaimProjection(), func(r RuntimeClaimRow) bool {
			return r == RuntimeClaimRow{Outcome: "absent", ClaimID: claimStoreClaimID}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport, _, _ := claimStubTransport(claimProjectionValues(test.projection))
			row, err := transport.Resolve(context.Background(), claimStoreClaimID, claimStoreReplicaID, claimStoreDigest(0x11))
			if err != nil || !test.check(row) {
				t.Fatalf("row=%+v error=%v", row, err)
			}
		})
	}
}

func TestRuntimeClaimTransportRejectsInputsWithoutQuery(t *testing.T) {
	digest := claimStoreDigest(0x11)
	nilWork := uuid.Nil
	for _, test := range []struct {
		name string
		call func(RuntimeClaimTransport) (RuntimeClaimRow, error)
	}{
		{"nil_context", func(s RuntimeClaimTransport) (RuntimeClaimRow, error) {
			return s.AcquireSingle(nil, claimStoreClaimID, "mail.send", claimStoreReplicaID, nil, "global", digest) //nolint:staticcheck // Explicit nil-context rejection is part of the boundary.
		}},
		{"nil_claim", func(s RuntimeClaimTransport) (RuntimeClaimRow, error) {
			return s.AcquireSingle(context.Background(), uuid.Nil, "mail.send", claimStoreReplicaID, nil, "global", digest)
		}},
		{"nil_replica", func(s RuntimeClaimTransport) (RuntimeClaimRow, error) {
			return s.AcquireSingle(context.Background(), claimStoreClaimID, "mail.send", uuid.Nil, nil, "global", digest)
		}},
		{"unknown_policy", func(s RuntimeClaimTransport) (RuntimeClaimRow, error) {
			return s.AcquireSingle(context.Background(), claimStoreClaimID, "bogus", claimStoreReplicaID, nil, "global", digest)
		}},
		{"sse_via_single", func(s RuntimeClaimTransport) (RuntimeClaimRow, error) {
			return s.AcquireSingle(context.Background(), claimStoreClaimID, "sse.fleet_account_ip", claimStoreReplicaID, nil, "ip", digest)
		}},
		{"unknown_kind", func(s RuntimeClaimTransport) (RuntimeClaimRow, error) {
			return s.AcquireSingle(context.Background(), claimStoreClaimID, "mail.send", claimStoreReplicaID, nil, "tenant", digest)
		}},
		{"nil_work_value", func(s RuntimeClaimTransport) (RuntimeClaimRow, error) {
			return s.AcquireSingle(context.Background(), claimStoreClaimID, "render.global_claim", claimStoreReplicaID, &nilWork, "global", digest)
		}},
		{"sse_nil_claim", func(s RuntimeClaimTransport) (RuntimeClaimRow, error) {
			return s.AcquireSSE(context.Background(), uuid.Nil, claimStoreReplicaID, digest, nil)
		}},
		{"sse_nil_context", func(s RuntimeClaimTransport) (RuntimeClaimRow, error) {
			return s.AcquireSSE(nil, claimStoreClaimID, claimStoreReplicaID, digest, nil) //nolint:staticcheck // Explicit nil-context rejection is part of the boundary.
		}},
		{"promote_nil_replica", func(s RuntimeClaimTransport) (RuntimeClaimRow, error) {
			return s.Promote(context.Background(), claimStoreClaimID, uuid.Nil, digest)
		}},
		{"release_bad_reason", func(s RuntimeClaimTransport) (RuntimeClaimRow, error) {
			return s.Release(context.Background(), claimStoreClaimID, claimStoreReplicaID, digest, "fenced")
		}},
		{"release_empty_reason", func(s RuntimeClaimTransport) (RuntimeClaimRow, error) {
			return s.Release(context.Background(), claimStoreClaimID, claimStoreReplicaID, digest, "")
		}},
		{"resolve_nil_claim", func(s RuntimeClaimTransport) (RuntimeClaimRow, error) {
			return s.Resolve(context.Background(), uuid.Nil, claimStoreReplicaID, digest)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &registrationRunnerStub{}
			row, err := test.call(&runtimeClaimTransport{runner: runner})
			if err == nil || row != (RuntimeClaimRow{}) || runner.calls != 0 {
				t.Fatalf("row=%+v error=%v calls=%d", row, err, runner.calls)
			}
		})
	}
	row, err := NewRuntimeClaimTransport(nil).AcquireSingle(context.Background(), claimStoreClaimID, "mail.send", claimStoreReplicaID, nil, "global", digest)
	if err == nil || row != (RuntimeClaimRow{}) {
		t.Fatalf("nil pool row=%+v error=%v", row, err)
	}
	row, err = (&runtimeClaimTransport{}).Resolve(context.Background(), claimStoreClaimID, claimStoreReplicaID, digest)
	if err == nil || row != (RuntimeClaimRow{}) {
		t.Fatalf("nil runner row=%+v error=%v", row, err)
	}
}

func TestRuntimeClaimTransportReturnsZeroOnRunnerQueryAndDecodeFailures(t *testing.T) {
	driverErr := errors.New("driver failed")
	running := storedClaimProjection("running", "mail.send")
	mutate := func(name string, change func(*runtimeClaimProjection)) struct {
		name       string
		projection runtimeClaimProjection
	} {
		p := storedClaimProjection("running", "mail.send")
		change(&p)
		return struct {
			name       string
			projection runtimeClaimProjection
		}{name, p}
	}
	invalid := []struct {
		name       string
		projection runtimeClaimProjection
	}{
		mutate("unknown_outcome", func(p *runtimeClaimProjection) { p.Outcome = "bogus" }),
		mutate("nil_claim_id", func(p *runtimeClaimProjection) { p.ClaimID = uuid.Nil }),
		mutate("absent_with_presence", func(p *runtimeClaimProjection) { *p = absentClaimProjection(); p.PolicyIDPresent = true }),
		mutate("policy_absent", func(p *runtimeClaimProjection) { p.PolicyIDPresent = false }),
		mutate("policy_unknown", func(p *runtimeClaimProjection) { p.PolicyIDValue = "bogus" }),
		mutate("replica_absent", func(p *runtimeClaimProjection) { p.ReplicaIDPresent = false }),
		mutate("replica_nil", func(p *runtimeClaimProjection) { p.ReplicaIDValue = uuid.Nil }),
		mutate("digest_absent", func(p *runtimeClaimProjection) { p.RequestDigestPresent = false }),
		mutate("digest_short", func(p *runtimeClaimProjection) { p.RequestDigestValue = p.RequestDigestValue[:31] }),
		mutate("digest_present_empty", func(p *runtimeClaimProjection) { p.RequestDigestValue = []byte{} }),
		mutate("scope_count_absent", func(p *runtimeClaimProjection) { p.ScopeCountPresent = false }),
		mutate("scope_count_zero", func(p *runtimeClaimProjection) { p.ScopeCountValue = 0 }),
		mutate("scope_count_three", func(p *runtimeClaimProjection) { p.ScopeCountValue = 3 }),
		mutate("scope1_kind_absent", func(p *runtimeClaimProjection) { p.Scope1KindPresent = false }),
		mutate("scope1_kind_unknown", func(p *runtimeClaimProjection) { p.Scope1KindValue = "tenant" }),
		mutate("scope1_digest_short", func(p *runtimeClaimProjection) { p.Scope1DigestValue = p.Scope1DigestValue[:31] }),
		mutate("scope2_kind_on_single", func(p *runtimeClaimProjection) { p.Scope2KindPresent = true; p.Scope2KindValue = "account" }),
		mutate("scope2_digest_on_single", func(p *runtimeClaimProjection) { p.Scope2DigestPresent = true; p.Scope2DigestValue = make([]byte, 32) }),
		mutate("scope2_ordinal_on_single", func(p *runtimeClaimProjection) {
			p.Scope2AllocationOrdinalPresent = true
			p.Scope2AllocationOrdinalValue = 1
		}),
		mutate("two_scopes_non_sse", func(p *runtimeClaimProjection) { p.ScopeCountValue = 2 }),
		mutate("sse_missing_scope2_kind", func(p *runtimeClaimProjection) {
			*p = storedClaimProjection("running", "sse.fleet_account_ip")
			p.Scope2KindPresent = false
		}),
		mutate("sse_wrong_scope2_kind", func(p *runtimeClaimProjection) {
			*p = storedClaimProjection("running", "sse.fleet_account_ip")
			p.Scope2KindValue = "ip"
		}),
		mutate("sse_wrong_scope1_kind", func(p *runtimeClaimProjection) {
			*p = storedClaimProjection("running", "sse.fleet_account_ip")
			p.Scope1KindValue = "account"
		}),
		mutate("sse_scope2_digest_short", func(p *runtimeClaimProjection) {
			*p = storedClaimProjection("running", "sse.fleet_account_ip")
			p.Scope2DigestValue = p.Scope2DigestValue[:31]
		}),
		mutate("sse_scope2_ordinal_absent", func(p *runtimeClaimProjection) {
			*p = storedClaimProjection("running", "sse.fleet_account_ip")
			p.Scope2AllocationOrdinalPresent = false
		}),
		mutate("sse_scope2_ordinal_zero", func(p *runtimeClaimProjection) {
			*p = storedClaimProjection("running", "sse.fleet_account_ip")
			p.Scope2AllocationOrdinalValue = 0
		}),
		mutate("work_on_mail", func(p *runtimeClaimProjection) { p.WorkIDPresent = true; p.WorkIDValue = claimStoreWorkID }),
		mutate("render_without_work", func(p *runtimeClaimProjection) {
			*p = storedClaimProjection("running", "render.global_claim")
			p.WorkIDPresent = false
		}),
		mutate("render_nil_work", func(p *runtimeClaimProjection) {
			*p = storedClaimProjection("running", "render.global_claim")
			p.WorkIDValue = uuid.Nil
		}),
		mutate("render_without_deadline", func(p *runtimeClaimProjection) {
			*p = storedClaimProjection("running", "render.global_claim")
			p.DeadlineAtPresent = false
		}),
		mutate("render_wrong_deadline", func(p *runtimeClaimProjection) {
			*p = storedClaimProjection("running", "render.global_claim")
			p.DeadlineAtValue = p.DeadlineAtValue.Add(time.Second)
		}),
		mutate("mail_with_deadline", func(p *runtimeClaimProjection) { p.DeadlineAtPresent = true; p.DeadlineAtValue = p.AdmittedAtValue }),
		mutate("state_absent", func(p *runtimeClaimProjection) { p.StatePresent = false }),
		mutate("state_mismatch", func(p *runtimeClaimProjection) { p.StateValue = "waiting" }),
		mutate("admitted_absent", func(p *runtimeClaimProjection) { p.AdmittedAtPresent = false }),
		mutate("ordinal_absent", func(p *runtimeClaimProjection) { p.Scope1AllocationOrdinalPresent = false }),
		mutate("ordinal_zero", func(p *runtimeClaimProjection) { p.Scope1AllocationOrdinalValue = 0 }),
		mutate("ordinal_negative", func(p *runtimeClaimProjection) { p.Scope1AllocationOrdinalValue = -1 }),
		mutate("live_with_released_at", func(p *runtimeClaimProjection) { p.ReleasedAtPresent = true }),
		mutate("live_with_reason", func(p *runtimeClaimProjection) { p.ReleaseReasonPresent = true; p.ReleaseReasonValue = "joined" }),
		mutate("released_without_time", func(p *runtimeClaimProjection) {
			*p = storedClaimProjection("released", "mail.send")
			p.ReleasedAtPresent = false
		}),
		mutate("released_without_reason", func(p *runtimeClaimProjection) {
			*p = storedClaimProjection("released", "mail.send")
			p.ReleaseReasonPresent = false
		}),
		mutate("released_unknown_reason", func(p *runtimeClaimProjection) {
			*p = storedClaimProjection("released", "mail.send")
			p.ReleaseReasonValue = "bogus"
		}),
		mutate("denied_with_state", func(p *runtimeClaimProjection) {
			*p = storedClaimProjection("denied", "mail.send")
			p.StatePresent = true
			p.StateValue = "running"
		}),
		mutate("denied_with_admitted", func(p *runtimeClaimProjection) {
			*p = storedClaimProjection("denied", "mail.send")
			p.AdmittedAtPresent = true
		}),
		mutate("denied_with_ordinal", func(p *runtimeClaimProjection) {
			*p = storedClaimProjection("denied", "mail.send")
			p.Scope1AllocationOrdinalPresent = true
			p.Scope1AllocationOrdinalValue = 1
		}),
		mutate("denied_with_release", func(p *runtimeClaimProjection) {
			*p = storedClaimProjection("denied", "mail.send")
			p.ReleaseReasonPresent = true
			p.ReleaseReasonValue = "joined"
		}),
	}
	for _, test := range invalid {
		t.Run("decode_"+test.name, func(t *testing.T) {
			transport, runner, _ := claimStubTransport(claimProjectionValues(test.projection))
			runner.afterErr = errors.New("must not reach finish")
			row, err := transport.Resolve(context.Background(), claimStoreClaimID, claimStoreReplicaID, claimStoreDigest(0x11))
			if err == nil || row != (RuntimeClaimRow{}) || errors.Is(err, runner.afterErr) {
				t.Fatalf("row=%+v error=%v", row, err)
			}
		})
	}
	for _, test := range []struct {
		name   string
		runner *registrationRunnerStub
	}{
		{"runner", &registrationRunnerStub{err: driverErr}},
		{"finish_or_commit_after_decode", &registrationRunnerStub{queries: New(&registrationDBStub{values: claimProjectionValues(running)}), afterErr: driverErr}},
		{"query", &registrationRunnerStub{queries: New(&registrationDBStub{err: driverErr})}},
		{"required_null", &registrationRunnerStub{queries: New(&registrationDBStub{values: append([]any{nil}, claimProjectionValues(running)[1:]...)})}},
	} {
		t.Run(test.name, func(t *testing.T) {
			row, err := (&runtimeClaimTransport{runner: test.runner}).Resolve(context.Background(), claimStoreClaimID, claimStoreReplicaID, claimStoreDigest(0x11))
			if err == nil || row != (RuntimeClaimRow{}) {
				t.Fatalf("row=%+v error=%v", row, err)
			}
			if test.name != "required_null" && !errors.Is(err, driverErr) {
				t.Fatalf("driver cause not wrapped: %v", err)
			}
		})
	}
}

type claimOrderLease struct {
	events             []string
	tx                 *claimOrderTx
	releases, destroys int
}

func (l *claimOrderLease) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	l.events = append(l.events, "begin")
	return l.tx, nil
}

func (l *claimOrderLease) Release(context.Context) error {
	l.events = append(l.events, "release")
	l.releases++
	return nil
}

func (l *claimOrderLease) Destroy(context.Context) error {
	l.events = append(l.events, "destroy")
	l.destroys++
	return nil
}

type claimOrderTx struct {
	pgx.Tx
	lease                          *claimOrderLease
	values                         []any
	queryErr, finishErr, commitErr error
	functions                      int
}

func (tx *claimOrderTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	switch sql {
	case "SELECT public.runtime_enter_write()":
		tx.lease.events = append(tx.lease.events, "entry")
		return pgconn.CommandTag{}, nil
	case "SELECT public.runtime_finish_write()":
		tx.lease.events = append(tx.lease.events, "finish")
		return pgconn.CommandTag{}, tx.finishErr
	}
	tx.lease.events = append(tx.lease.events, "unexpected-exec")
	return pgconn.CommandTag{}, errors.New("unexpected exec")
}

func (tx *claimOrderTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	tx.functions++
	name := "unknown"
	for _, candidate := range []string{"runtime_acquire_single_claim", "runtime_acquire_sse_claim", "runtime_promote_claim", "runtime_release_claim", "runtime_resolve_claim", "runtime_gc_released_claim_receipts"} {
		if strings.Contains(sql, candidate) {
			name = candidate
		}
	}
	tx.lease.events = append(tx.lease.events, "function:"+name)
	return registrationRowStub{values: tx.values, err: tx.queryErr}
}

func (tx *claimOrderTx) Commit(context.Context) error {
	tx.lease.events = append(tx.lease.events, "commit")
	return tx.commitErr
}

func (tx *claimOrderTx) Rollback(context.Context) error {
	tx.lease.events = append(tx.lease.events, "rollback")
	return nil
}

func claimOrderRunner(values []any) (WriteTxRunner, *claimOrderLease) {
	lease := &claimOrderLease{}
	lease.tx = &claimOrderTx{lease: lease, values: values}
	return &writeTxRunner{acquire: func(context.Context) (writeTxLease, error) {
		lease.events = append(lease.events, "acquire")
		return lease, nil
	}}, lease
}

func TestRuntimeClaimTransportEntryFunctionFinishCommitOrder(t *testing.T) {
	running := storedClaimProjection("running", "mail.send")
	runner, lease := claimOrderRunner(claimProjectionValues(running))
	row, err := (&runtimeClaimTransport{runner: runner}).AcquireSingle(context.Background(), claimStoreClaimID, "mail.send", claimStoreReplicaID, nil, "global", claimStoreDigest(0x22))
	if err != nil || row.Outcome != "running" || lease.tx.functions != 1 {
		t.Fatalf("row=%+v error=%v functions=%d", row, err, lease.tx.functions)
	}
	assertEvents(t, lease.events, "acquire", "begin", "entry", "function:runtime_acquire_single_claim", "finish", "commit", "release")

	invalid := storedClaimProjection("running", "mail.send")
	invalid.StateValue = "waiting"
	runner, lease = claimOrderRunner(claimProjectionValues(invalid))
	row, err = (&runtimeClaimTransport{runner: runner}).Resolve(context.Background(), claimStoreClaimID, claimStoreReplicaID, claimStoreDigest(0x22))
	if err == nil || row != (RuntimeClaimRow{}) {
		t.Fatalf("invalid row=%+v error=%v", row, err)
	}
	assertEvents(t, lease.events, "acquire", "begin", "entry", "function:runtime_resolve_claim", "rollback", "release")

	runner, lease = claimOrderRunner(claimProjectionValues(running))
	lease.tx.finishErr = errors.New("finish lost")
	row, err = (&runtimeClaimTransport{runner: runner}).Release(context.Background(), claimStoreClaimID, claimStoreReplicaID, claimStoreDigest(0x22), "joined")
	if !errors.Is(err, lease.tx.finishErr) || row != (RuntimeClaimRow{}) {
		t.Fatalf("finish failure row=%+v error=%v", row, err)
	}
	assertEvents(t, lease.events, "acquire", "begin", "entry", "function:runtime_release_claim", "finish", "rollback", "destroy")

	runner, lease = claimOrderRunner(claimProjectionValues(running))
	lease.tx.commitErr = errors.New("commit ambiguous")
	row, err = (&runtimeClaimTransport{runner: runner}).Promote(context.Background(), claimStoreClaimID, claimStoreReplicaID, claimStoreDigest(0x22))
	if !errors.Is(err, lease.tx.commitErr) || row != (RuntimeClaimRow{}) {
		t.Fatalf("commit failure row=%+v error=%v", row, err)
	}
	assertEvents(t, lease.events, "acquire", "begin", "entry", "function:runtime_promote_claim", "finish", "commit", "destroy")

	runner, lease = claimOrderRunner(nil)
	lease.tx.queryErr = &pgconn.PgError{Code: "AM001", Message: "stored claim identity is invalid"}
	row, err = (&runtimeClaimTransport{runner: runner}).AcquireSSE(context.Background(), claimStoreClaimID, claimStoreReplicaID, claimStoreDigest(0x22), nil)
	if !isSQLState(err, "AM001") || row != (RuntimeClaimRow{}) {
		t.Fatalf("AM001 row=%+v error=%v", row, err)
	}
	assertEvents(t, lease.events, "acquire", "begin", "entry", "function:runtime_acquire_sse_claim", "rollback", "destroy")
}

var claimLiveTriggerStatements = []string{
	`ALTER TABLE public.shared_claim_requests %s TRIGGER shared_claim_requests_validate`,
	`ALTER TABLE public.shared_claim_scopes %s TRIGGER shared_claim_scopes_validate`,
	`ALTER TABLE public.shared_claim_scope_summaries %s TRIGGER shared_claim_scope_summaries_validate`,
	`ALTER TABLE public.shared_claim_requests %s TRIGGER shared_claim_requests_complete`,
	`ALTER TABLE public.shared_claim_scopes %s TRIGGER shared_claim_scopes_request_complete`,
	`ALTER TABLE public.shared_claim_scope_summaries %s TRIGGER shared_claim_scope_summaries_exact`,
	`ALTER TABLE public.shared_claim_scopes %s TRIGGER shared_claim_scopes_summary_exact`,
}

func claimLiveOwnerWrite(t *testing.T, admin *sql.DB, statements ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := admin.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, resetErr := conn.ExecContext(cleanup, `SELECT public.runtime_exit_migrator(); RESET SESSION AUTHORIZATION`); resetErr != nil {
			t.Errorf("reset claim owner write: %v", resetErr)
		}
		if closeErr := conn.Close(); closeErr != nil {
			t.Errorf("close claim owner write: %v", closeErr)
		}
	}()
	if _, err := conn.ExecContext(ctx, `SET SESSION AUTHORIZATION aboutme_migrator; SELECT public.runtime_enter_migrator(); BEGIN; SELECT public.runtime_begin_migration_write('migration-100015'); SET LOCAL ROLE aboutme_runtime_owner`); err != nil {
		t.Fatal(err)
	}
	for _, statement := range statements {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			if _, rollbackErr := conn.ExecContext(ctx, `ROLLBACK`); rollbackErr != nil {
				t.Error(rollbackErr)
			}
			t.Fatalf("%s: %v", statement, err)
		}
	}
	if _, err := conn.ExecContext(ctx, `RESET ROLE; SELECT public.runtime_finish_write(); COMMIT`); err != nil {
		t.Fatal(err)
	}
}

func claimLiveDatabase(t *testing.T) (string, *sql.DB) {
	t.Helper()
	dsn, admin := registrationLiveDatabase(t)
	identity := registrationStoreIdentity()
	claimLiveOwnerWrite(t, admin,
		fmt.Sprintf(`INSERT INTO public.runtime_replicas(replica_id,replica_kind,instance_id,container_instance_arn,caddy_task_arn,go_task_arn,nuxt_task_arn,release_digest,state,joined_at,join_ready_at,activated_at) VALUES('%s','serving','%s','%s','%s','%s','%s','%s','active',clock_timestamp(),clock_timestamp(),clock_timestamp())`, identity.ReplicaID, identity.InstanceID, identity.ContainerInstanceARN, identity.CaddyTaskARN, identity.GoTaskARN, identity.NuxtTaskARN, identity.ReleaseDigest),
		fmt.Sprintf(`INSERT INTO public.runtime_replica_tasks(task_arn,replica_id,task_role) VALUES('%s','%s','caddy'),('%s','%s','go'),('%s','%s','nuxt')`, identity.CaddyTaskARN, identity.ReplicaID, identity.GoTaskARN, identity.ReplicaID, identity.NuxtTaskARN, identity.ReplicaID),
	)
	return dsn, admin
}

func claimLiveCorrupt(t *testing.T, admin *sql.DB, statement string) {
	t.Helper()
	disable := make([]string, 0, len(claimLiveTriggerStatements))
	enable := make([]string, 0, len(claimLiveTriggerStatements))
	for _, template := range claimLiveTriggerStatements {
		disable = append(disable, fmt.Sprintf(template, "DISABLE"))
		enable = append(enable, fmt.Sprintf(template, "ENABLE"))
	}
	claimLiveOwnerWrite(t, admin, disable...)
	claimLiveOwnerWrite(t, admin, statement)
	claimLiveOwnerWrite(t, admin, enable...)
}

func sameClaimRow(a, b RuntimeClaimRow) bool {
	ptrString := func(x, y *string) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	ptrUUID := func(x, y *uuid.UUID) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	ptrTime := func(x, y *time.Time) bool { return (x == nil) == (y == nil) && (x == nil || x.Equal(*y)) }
	ptrInt := func(x, y *int64) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	scope := func(x, y *RuntimeClaimScope) bool {
		return (x == nil) == (y == nil) && (x == nil || (x.Kind == y.Kind && x.Digest == y.Digest && ptrInt(x.AllocationOrdinal, y.AllocationOrdinal)))
	}
	return a.Outcome == b.Outcome && a.ClaimID == b.ClaimID && ptrString(a.PolicyID, b.PolicyID) && ptrUUID(a.ReplicaID, b.ReplicaID) && ptrUUID(a.WorkID, b.WorkID) && ptrString(a.State, b.State) && ptrTime(a.AdmittedAt, b.AdmittedAt) && ptrTime(a.DeadlineAt, b.DeadlineAt) && ptrTime(a.ReleasedAt, b.ReleasedAt) && ptrString(a.ReleaseReason, b.ReleaseReason) &&
		(a.RequestDigest == nil) == (b.RequestDigest == nil) && (a.RequestDigest == nil || *a.RequestDigest == *b.RequestDigest) && (a.ScopeCount == nil) == (b.ScopeCount == nil) && (a.ScopeCount == nil || *a.ScopeCount == *b.ScopeCount) && scope(a.Scope1, b.Scope1) && scope(a.Scope2, b.Scope2)
}

func TestRuntimeClaimTransportLiveOperationsCommitOnceAndSurviveReuse(t *testing.T) {
	dsn, admin := claimLiveDatabase(t)
	pool := newWriteRunnerAppPool(t, dsn, nil)
	transport := NewRuntimeClaimTransport(pool)
	replica := registrationStoreIdentity().ReplicaID
	work := claimStoreWorkID
	scope := claimStoreDigest(0x44)
	ctx := context.Background()

	fresh, err := transport.AcquireSingle(ctx, claimStoreClaimID, "render.global_claim", replica, &work, "global", scope)
	if err != nil {
		t.Fatal(err)
	}
	retained := fresh
	retainedDigest := *fresh.RequestDigest
	if fresh.Outcome != "running" || fresh.WorkID == nil || *fresh.WorkID != work || fresh.DeadlineAt == nil || fresh.Scope1 == nil || fresh.Scope1.Digest != scope || fresh.Scope1.AllocationOrdinal == nil || *fresh.Scope1.AllocationOrdinal != 1 {
		t.Fatalf("fresh=%+v", fresh)
	}
	var parents int
	var nextOrdinal int64
	if countErr := admin.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM public.shared_claim_requests WHERE claim_id=$1),(SELECT next_ordinal FROM public.shared_claim_scope_summaries WHERE policy_id='render.global_claim' AND scope_digest=$2)`, claimStoreClaimID, scope[:]).Scan(&parents, &nextOrdinal); countErr != nil || parents != 1 || nextOrdinal != 2 {
		t.Fatalf("parents=%d next_ordinal=%d error=%v", parents, nextOrdinal, countErr)
	}
	replay, err := transport.AcquireSingle(ctx, claimStoreClaimID, "render.global_claim", replica, &work, "global", scope)
	if err != nil || !sameClaimRow(replay, fresh) {
		t.Fatalf("replay=%+v fresh=%+v error=%v", replay, fresh, err)
	}
	resolved, err := transport.Resolve(ctx, claimStoreClaimID, replica, *fresh.RequestDigest)
	if err != nil || !sameClaimRow(resolved, fresh) {
		t.Fatalf("resolved=%+v error=%v", resolved, err)
	}
	second := uuid.MustParse("44444444-4444-4444-8444-444444444444")
	waiting, err := transport.AcquireSingle(ctx, second, "render.global_claim", replica, &work, "global", scope)
	if err != nil || waiting.Outcome != "waiting" {
		t.Fatalf("waiting=%+v error=%v", waiting, err)
	}
	blocked, err := transport.Promote(ctx, second, replica, *waiting.RequestDigest)
	if err != nil || !sameClaimRow(blocked, waiting) {
		t.Fatalf("blocked=%+v error=%v", blocked, err)
	}
	released, err := transport.Release(ctx, claimStoreClaimID, replica, *fresh.RequestDigest, "joined")
	if err != nil || released.Outcome != "released" || released.ReleaseReason == nil || *released.ReleaseReason != "joined" {
		t.Fatalf("released=%+v error=%v", released, err)
	}
	promoted, err := transport.Promote(ctx, second, replica, *waiting.RequestDigest)
	if err != nil || promoted.Outcome != "running" || !promoted.DeadlineAt.Equal(*waiting.DeadlineAt) {
		t.Fatalf("promoted=%+v error=%v", promoted, err)
	}
	if !sameClaimRow(retained, fresh) || *retained.RequestDigest != retainedDigest || retained.Scope1.Digest != scope {
		t.Fatalf("retained fresh result changed after connection reuse: retained=%+v fresh=%+v", retained, fresh)
	}
	account := claimStoreDigest(0x55)
	stream, err := transport.AcquireSSE(ctx, uuid.MustParse("55555555-5555-4555-8555-555555555555"), replica, claimStoreDigest(0x66), &account)
	if err != nil || stream.Outcome != "running" || stream.Scope2 == nil || stream.Scope2.Digest != account || stream.Scope1.Kind != "ip" {
		t.Fatalf("stream=%+v error=%v", stream, err)
	}
	absent, err := transport.Resolve(ctx, uuid.MustParse("66666666-6666-4666-8666-666666666666"), replica, scope)
	if err != nil || absent != (RuntimeClaimRow{Outcome: "absent", ClaimID: uuid.MustParse("66666666-6666-4666-8666-666666666666")}) {
		t.Fatalf("absent=%+v error=%v", absent, err)
	}
	conflict, err := transport.Resolve(ctx, claimStoreClaimID, replica, claimStoreDigest(0x77))
	if !isSQLState(err, "AM002") || conflict != (RuntimeClaimRow{}) {
		t.Fatalf("conflict=%+v error=%v", conflict, err)
	}
}

func TestRuntimeClaimTransportLiveCommitAmbiguityReturnsZeroWithoutReplay(t *testing.T) {
	dsn, admin := claimLiveDatabase(t)
	pool := newWriteRunnerAppPool(t, dsn, nil)
	wantErr := errors.New("simulated lost claim commit response")
	transport := &runtimeClaimTransport{runner: liveWrappedRunner(pool, func(tx pgx.Tx) pgx.Tx {
		return &commitResponseLostTx{Tx: tx, err: wantErr}
	})}
	row, err := transport.AcquireSingle(context.Background(), claimStoreClaimID, "mail.send", registrationStoreIdentity().ReplicaID, nil, "global", claimStoreDigest(0x44))
	if !errors.Is(err, wantErr) || row != (RuntimeClaimRow{}) {
		t.Fatalf("row=%+v error=%v", row, err)
	}
	var rows int
	if err := admin.QueryRowContext(context.Background(), `SELECT count(*) FROM public.shared_claim_requests WHERE claim_id=$1 AND state='running'`, claimStoreClaimID).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("committed rows=%d error=%v", rows, err)
	}
}

func TestRuntimeClaimTransportLiveFinishFailureRollsBackAndReturnsZero(t *testing.T) {
	dsn, admin := claimLiveDatabase(t)
	pool := newWriteRunnerAppPool(t, dsn, nil)
	wantErr := errors.New("simulated lost claim finish response")
	transport := &runtimeClaimTransport{runner: liveWrappedRunner(pool, func(tx pgx.Tx) pgx.Tx {
		return &registrationFinishResponseLostTx{Tx: tx, err: wantErr}
	})}
	row, err := transport.AcquireSingle(context.Background(), claimStoreClaimID, "mail.send", registrationStoreIdentity().ReplicaID, nil, "global", claimStoreDigest(0x44))
	if !errors.Is(err, wantErr) || row != (RuntimeClaimRow{}) {
		t.Fatalf("row=%+v error=%v", row, err)
	}
	var rows, summaries int
	if err := admin.QueryRowContext(context.Background(), `SELECT (SELECT count(*) FROM public.shared_claim_requests),(SELECT count(*) FROM public.shared_claim_scope_summaries)`).Scan(&rows, &summaries); err != nil || rows != 0 || summaries != 0 {
		t.Fatalf("rolled-back rows=%d summaries=%d error=%v", rows, summaries, err)
	}
}

func TestRuntimeClaimTransportLiveAM001RetiresBackendAndReturnsZero(t *testing.T) {
	dsn, admin := claimLiveDatabase(t)
	pool := newWriteRunnerAppPool(t, dsn, nil)
	transport := NewRuntimeClaimTransport(pool)
	replica := registrationStoreIdentity().ReplicaID
	ctx := context.Background()
	fresh, err := transport.AcquireSingle(ctx, claimStoreClaimID, "mail.send", replica, nil, "global", claimStoreDigest(0x44))
	if err != nil {
		t.Fatal(err)
	}
	var oldPID int32
	if pidErr := NewWriteTxRunner(pool).ExecWrite(ctx, func(q *Queries) error {
		return q.db.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&oldPID)
	}); pidErr != nil {
		t.Fatal(pidErr)
	}
	claimLiveCorrupt(t, admin, fmt.Sprintf(`UPDATE public.shared_claim_requests SET request_digest=decode('%s','hex') WHERE claim_id='%s'`, strings.Repeat("ab", 32), claimStoreClaimID))
	row, err := transport.Resolve(ctx, claimStoreClaimID, replica, *fresh.RequestDigest)
	if !isSQLState(err, "AM001") || row != (RuntimeClaimRow{}) {
		t.Fatalf("row=%+v error=%v", row, err)
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

func TestRuntimeClaimTransportLiveRequiredNullReturnsZero(t *testing.T) {
	dsn, admin := claimLiveDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := admin.ExecContext(ctx, `GRANT CREATE ON SCHEMA public TO aboutme_runtime_owner`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, revokeErr := admin.ExecContext(cleanup, `REVOKE CREATE ON SCHEMA public FROM aboutme_runtime_owner`); revokeErr != nil {
			t.Errorf("revoke runtime owner schema create: %v", revokeErr)
		}
	})
	if _, err := admin.ExecContext(ctx, `SET ROLE aboutme_runtime_owner; CREATE OR REPLACE FUNCTION public.runtime_resolve_claim(uuid,uuid,bytea) RETURNS public.runtime_claim_result LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog AS $$ SELECT ROW(NULL::text,$1,NULL::text,NULL::uuid,NULL::uuid,NULL::text,NULL::timestamptz,NULL::timestamptz,NULL::timestamptz,NULL::text,NULL::bytea,NULL::smallint,NULL::text,NULL::bytea,NULL::bigint,NULL::text,NULL::bytea,NULL::bigint)::public.runtime_claim_result $$; RESET ROLE`); err != nil {
		t.Fatal(err)
	}
	pool := newWriteRunnerAppPool(t, dsn, nil)
	row, err := NewRuntimeClaimTransport(pool).Resolve(ctx, claimStoreClaimID, registrationStoreIdentity().ReplicaID, claimStoreDigest(0x44))
	if err == nil || row != (RuntimeClaimRow{}) {
		t.Fatalf("row=%+v error=%v", row, err)
	}
}

func newMaintenancePool(t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 1
	cfg.MinConns = 0
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, authErr := conn.Exec(ctx, `SET SESSION AUTHORIZATION aboutme_maintenance`)
		return authErr
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}
