package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RuntimeClaimScope is one ordered scope in a durable claim result.
type RuntimeClaimScope struct {
	Kind              string
	Digest            [32]byte
	AllocationOrdinal *int64
}

// RuntimeClaimRow is the owned, validated result of one fixed claim operation.
// Outcome and ClaimID are always present; every other field keeps explicit
// presence and holds a copied value that no driver buffer can change.
type RuntimeClaimRow struct {
	Outcome       string
	ClaimID       uuid.UUID
	PolicyID      *string
	ReplicaID     *uuid.UUID
	WorkID        *uuid.UUID
	State         *string
	AdmittedAt    *time.Time
	DeadlineAt    *time.Time
	ReleasedAt    *time.Time
	ReleaseReason *string
	RequestDigest *[32]byte
	ScopeCount    *int16
	Scope1        *RuntimeClaimScope
	Scope2        *RuntimeClaimScope
}

// RuntimeClaimTransport executes the five fixed shared-claim operations. Each
// call runs one generated function inside the write barrier and returns a
// decoded row only after the transaction finished and committed.
type RuntimeClaimTransport interface {
	AcquireSingle(context.Context, uuid.UUID, string, uuid.UUID, *uuid.UUID, string, [32]byte) (RuntimeClaimRow, error)
	AcquireSSE(context.Context, uuid.UUID, uuid.UUID, [32]byte, *[32]byte) (RuntimeClaimRow, error)
	Promote(context.Context, uuid.UUID, uuid.UUID, [32]byte) (RuntimeClaimRow, error)
	Release(context.Context, uuid.UUID, uuid.UUID, [32]byte, string) (RuntimeClaimRow, error)
	Resolve(context.Context, uuid.UUID, uuid.UUID, [32]byte) (RuntimeClaimRow, error)
}

// runtimeClaimProjection is the shared generated result shape of all five
// claim queries.
type runtimeClaimProjection = RuntimeAcquireSingleClaimRow

type runtimeClaimTransport struct{ runner WriteTxRunner }

// NewRuntimeClaimTransport constructs a claim transport backed by pool.
func NewRuntimeClaimTransport(pool *pgxpool.Pool) RuntimeClaimTransport {
	return &runtimeClaimTransport{runner: NewWriteTxRunner(pool)}
}

var (
	runtimeClaimSinglePolicies = map[string]bool{"render.global_claim": true, "password.hash": true, "mail.send": true, "mcp.user_concurrent": true}
	runtimeClaimScopeKinds     = map[string]bool{"global": true, "user": true, "ip": true, "account": true}
	runtimeClaimReleaseReasons = map[string]bool{"joined": true, "canceled": true, "expired": true}
)

// AcquireSingle acquires or replays a fixed single-scope C01-C04 claim.
func (s *runtimeClaimTransport) AcquireSingle(ctx context.Context, claimID uuid.UUID, policy string, replicaID uuid.UUID, workID *uuid.UUID, kind string, digest [32]byte) (RuntimeClaimRow, error) {
	if err := validateClaimCall(ctx, claimID, replicaID); err != nil {
		return RuntimeClaimRow{}, err
	}
	if !runtimeClaimSinglePolicies[policy] || !runtimeClaimScopeKinds[kind] || (workID != nil && *workID == uuid.Nil) {
		return RuntimeClaimRow{}, errors.New("store: invalid claim request shape")
	}
	var work *uuid.UUID
	if workID != nil {
		copied := *workID
		work = &copied
	}
	return s.run(ctx, func(q *Queries) (runtimeClaimProjection, error) {
		return q.RuntimeAcquireSingleClaim(ctx, RuntimeAcquireSingleClaimParams{ClaimID: claimID, PolicyID: policy, ReplicaID: replicaID, WorkID: work, ScopeKind: kind, ScopeDigest: digest[:]})
	})
}

// AcquireSSE atomically acquires or replays the fixed IP and optional account
// scopes of one C05 stream.
func (s *runtimeClaimTransport) AcquireSSE(ctx context.Context, claimID, replicaID uuid.UUID, ip [32]byte, account *[32]byte) (RuntimeClaimRow, error) {
	if err := validateClaimCall(ctx, claimID, replicaID); err != nil {
		return RuntimeClaimRow{}, err
	}
	var accountBytes []byte
	if account != nil {
		accountBytes = append([]byte(nil), account[:]...)
	}
	return s.run(ctx, func(q *Queries) (runtimeClaimProjection, error) {
		row, err := q.RuntimeAcquireSSEClaim(ctx, RuntimeAcquireSSEClaimParams{ClaimID: claimID, ReplicaID: replicaID, IpDigest: ip[:], AccountDigest: accountBytes})
		return runtimeClaimProjection(row), err
	})
}

// Promote attempts to promote the exact queued claim into free capacity.
func (s *runtimeClaimTransport) Promote(ctx context.Context, claimID, replicaID uuid.UUID, digest [32]byte) (RuntimeClaimRow, error) {
	if err := validateClaimCall(ctx, claimID, replicaID); err != nil {
		return RuntimeClaimRow{}, err
	}
	return s.run(ctx, func(q *Queries) (runtimeClaimProjection, error) {
		row, err := q.RuntimePromoteClaim(ctx, RuntimePromoteClaimParams{ClaimID: claimID, ExpectedReplicaID: replicaID, RequestDigest: digest[:]})
		return runtimeClaimProjection(row), err
	})
}

// Release atomically releases the exact claim with an allowed caller reason.
func (s *runtimeClaimTransport) Release(ctx context.Context, claimID, replicaID uuid.UUID, digest [32]byte, reason string) (RuntimeClaimRow, error) {
	if err := validateClaimCall(ctx, claimID, replicaID); err != nil {
		return RuntimeClaimRow{}, err
	}
	if !runtimeClaimReleaseReasons[reason] {
		return RuntimeClaimRow{}, errors.New("store: invalid claim release reason")
	}
	return s.run(ctx, func(q *Queries) (runtimeClaimProjection, error) {
		row, err := q.RuntimeReleaseClaim(ctx, RuntimeReleaseClaimParams{ClaimID: claimID, ExpectedReplicaID: replicaID, RequestDigest: digest[:], Reason: reason})
		return runtimeClaimProjection(row), err
	})
}

// Resolve reads the exact current durable claim state.
func (s *runtimeClaimTransport) Resolve(ctx context.Context, claimID, replicaID uuid.UUID, digest [32]byte) (RuntimeClaimRow, error) {
	if err := validateClaimCall(ctx, claimID, replicaID); err != nil {
		return RuntimeClaimRow{}, err
	}
	return s.run(ctx, func(q *Queries) (runtimeClaimProjection, error) {
		row, err := q.RuntimeResolveClaim(ctx, RuntimeResolveClaimParams{ClaimID: claimID, ExpectedReplicaID: replicaID, RequestDigest: digest[:]})
		return runtimeClaimProjection(row), err
	})
}

// run executes one generated claim function inside the write runner. The row
// is validated and copied before the callback returns, so an invalid result
// rolls the transaction back, and no value escapes unless finish and commit
// both succeed.
func (s *runtimeClaimTransport) run(ctx context.Context, call func(*Queries) (runtimeClaimProjection, error)) (RuntimeClaimRow, error) {
	if s == nil || s.runner == nil {
		return RuntimeClaimRow{}, errors.New("store: nil claim transport runner")
	}
	var decoded RuntimeClaimRow
	err := s.runner.ExecWrite(ctx, func(q *Queries) error {
		projected, err := call(q)
		if err != nil {
			return err
		}
		decoded, err = decodeRuntimeClaimRow(projected)
		return err
	})
	if err != nil {
		return RuntimeClaimRow{}, err
	}
	return decoded, nil
}

func validateClaimCall(ctx context.Context, claimID, replicaID uuid.UUID) error {
	if ctx == nil {
		return errors.New("store: nil claim context")
	}
	if claimID == uuid.Nil || replicaID == uuid.Nil {
		return errors.New("store: invalid claim identity")
	}
	return nil
}

func decodeRuntimeClaimRow(p runtimeClaimProjection) (RuntimeClaimRow, error) {
	if p.ClaimID == uuid.Nil {
		return RuntimeClaimRow{}, errors.New("store: invalid claim result ID")
	}
	result := RuntimeClaimRow{Outcome: p.Outcome, ClaimID: p.ClaimID}
	if p.Outcome == "absent" {
		if anyClaimPresence(p) {
			return RuntimeClaimRow{}, errors.New("store: invalid absent claim result")
		}
		return result, nil
	}
	if p.Outcome != "running" && p.Outcome != "waiting" && p.Outcome != "released" && p.Outcome != "denied" {
		return RuntimeClaimRow{}, errors.New("store: invalid claim outcome")
	}
	if !p.PolicyIDPresent || !p.ReplicaIDPresent || !p.RequestDigestPresent || !p.ScopeCountPresent || !p.Scope1KindPresent || !p.Scope1DigestPresent || p.ReplicaIDValue == uuid.Nil || len(p.RequestDigestValue) != 32 || len(p.Scope1DigestValue) != 32 || p.ScopeCountValue < 1 || p.ScopeCountValue > 2 || !runtimeClaimScopeKinds[p.Scope1KindValue] {
		return RuntimeClaimRow{}, errors.New("store: invalid claim identity result")
	}
	policy := p.PolicyIDValue
	if !runtimeClaimSinglePolicies[policy] && policy != "sse.fleet_account_ip" {
		return RuntimeClaimRow{}, errors.New("store: invalid claim policy result")
	}
	result.PolicyID = &policy
	replica := p.ReplicaIDValue
	result.ReplicaID = &replica
	requestDigest := new([32]byte)
	copy(requestDigest[:], p.RequestDigestValue)
	result.RequestDigest = requestDigest
	scopeCount := p.ScopeCountValue
	result.ScopeCount = &scopeCount
	s1 := &RuntimeClaimScope{Kind: p.Scope1KindValue}
	copy(s1.Digest[:], p.Scope1DigestValue)
	result.Scope1 = s1
	if scopeCount == 2 {
		if policy != "sse.fleet_account_ip" || p.Scope1KindValue != "ip" || !p.Scope2KindPresent || p.Scope2KindValue != "account" || !p.Scope2DigestPresent || len(p.Scope2DigestValue) != 32 {
			return RuntimeClaimRow{}, errors.New("store: invalid second claim scope")
		}
		s2 := &RuntimeClaimScope{Kind: p.Scope2KindValue}
		copy(s2.Digest[:], p.Scope2DigestValue)
		result.Scope2 = s2
	} else if p.Scope2KindPresent || p.Scope2DigestPresent || p.Scope2AllocationOrdinalPresent {
		return RuntimeClaimRow{}, errors.New("store: unexpected second claim scope")
	}
	if p.WorkIDPresent {
		if p.WorkIDValue == uuid.Nil {
			return RuntimeClaimRow{}, errors.New("store: invalid claim work ID")
		}
		work := p.WorkIDValue
		result.WorkID = &work
	}
	if (policy == "render.global_claim") != p.WorkIDPresent {
		return RuntimeClaimRow{}, errors.New("store: invalid claim work presence")
	}
	if p.Outcome == "denied" {
		if p.StatePresent || p.AdmittedAtPresent || p.DeadlineAtPresent || p.ReleasedAtPresent || p.ReleaseReasonPresent || p.Scope1AllocationOrdinalPresent || p.Scope2AllocationOrdinalPresent {
			return RuntimeClaimRow{}, errors.New("store: invalid denied claim result")
		}
		return result, nil
	}
	if !p.StatePresent || p.StateValue != p.Outcome || !p.AdmittedAtPresent || !p.Scope1AllocationOrdinalPresent || p.Scope1AllocationOrdinalValue <= 0 {
		return RuntimeClaimRow{}, errors.New("store: invalid stored claim result")
	}
	state := p.StateValue
	result.State = &state
	admitted := p.AdmittedAtValue
	result.AdmittedAt = &admitted
	a1 := p.Scope1AllocationOrdinalValue
	s1.AllocationOrdinal = &a1
	if scopeCount == 2 {
		if !p.Scope2AllocationOrdinalPresent || p.Scope2AllocationOrdinalValue <= 0 {
			return RuntimeClaimRow{}, errors.New("store: invalid second allocation")
		}
		a2 := p.Scope2AllocationOrdinalValue
		result.Scope2.AllocationOrdinal = &a2
	} else if p.Scope2AllocationOrdinalPresent {
		return RuntimeClaimRow{}, errors.New("store: unexpected second allocation")
	}
	if policy == "render.global_claim" {
		if !p.DeadlineAtPresent || !p.DeadlineAtValue.Equal(admitted.Add(20*time.Second)) {
			return RuntimeClaimRow{}, errors.New("store: invalid render claim deadline")
		}
		deadline := p.DeadlineAtValue
		result.DeadlineAt = &deadline
	} else if p.DeadlineAtPresent {
		return RuntimeClaimRow{}, errors.New("store: invalid non-render claim deadline")
	}
	if p.Outcome == "released" {
		if !p.ReleasedAtPresent || !p.ReleaseReasonPresent || (p.ReleaseReasonValue != "fenced" && !runtimeClaimReleaseReasons[p.ReleaseReasonValue]) {
			return RuntimeClaimRow{}, errors.New("store: invalid released claim result")
		}
		released := p.ReleasedAtValue
		reason := p.ReleaseReasonValue
		result.ReleasedAt = &released
		result.ReleaseReason = &reason
	} else if p.ReleasedAtPresent || p.ReleaseReasonPresent {
		return RuntimeClaimRow{}, errors.New("store: invalid live claim result")
	}
	return result, nil
}

func anyClaimPresence(p runtimeClaimProjection) bool {
	return p.PolicyIDPresent || p.ReplicaIDPresent || p.WorkIDPresent || p.StatePresent || p.AdmittedAtPresent || p.DeadlineAtPresent || p.ReleasedAtPresent || p.ReleaseReasonPresent || p.RequestDigestPresent || p.ScopeCountPresent || p.Scope1KindPresent || p.Scope1DigestPresent || p.Scope1AllocationOrdinalPresent || p.Scope2KindPresent || p.Scope2DigestPresent || p.Scope2AllocationOrdinalPresent
}
