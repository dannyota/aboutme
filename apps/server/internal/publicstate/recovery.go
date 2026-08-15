package publicstate

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
)

var errTransitionState = errors.New("publicstate: invalid transition state")

type transitionTarget struct {
	fence    *fence
	id       uuid.UUID
	expected int64
	class    TransitionClass
	global   bool
}

type transitionState uint8

const (
	transitionBegun transitionState = iota
	transitionClosed
	transitionDone
)

// Transition owns the close, commit, rollback, and recovery sequence for fences.
type Transition struct {
	mu          sync.Mutex
	coordinator *Coordinator
	targets     []transitionTarget
	state       transitionState
}

// Close stops new admissions and drains required active public requests.
func (t *Transition) Close(ctx context.Context, deadline time.Time) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.state != transitionBegun {
		return errTransitionState
	}
	if err := ctx.Err(); err != nil {
		t.releaseBegun()
		return err
	}
	var leases []*Lease
	for _, target := range t.targets {
		leases = append(leases, target.fence.close(target.global || target.class == Revoking)...)
	}
	for _, lease := range leases {
		lease.cancelLease()
	}
	timer := time.NewTimer(deadline.Sub(t.coordinator.now()))
	defer timer.Stop()
	for _, target := range t.targets {
		for _, set := range target.fence.drainSets() {
			select {
			case <-set.done:
			case <-ctx.Done():
				t.releaseUnchanged()
				return ctx.Err()
			case <-timer.C:
				t.releaseUnchanged()
				return &DrainTimeoutError{Deadline: deadline}
			}
		}
	}
	for _, target := range t.targets {
		target.fence.markClosed()
	}
	t.state = transitionClosed
	return nil
}

// Commit opens fences for the durable state proven after Close.
func (t *Transition) Commit(state CommittedState) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.state != transitionClosed {
		return errTransitionState
	}
	return t.commit(state)
}

// Rollback releases a transition without changing its durable generation state.
func (t *Transition) Rollback() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.state == transitionDone {
		return errTransitionState
	}
	if t.state == transitionBegun {
		t.releaseBegun()
		return nil
	}
	t.releaseUnchanged()
	return nil
}

// Recover resolves a closed transition before it can admit public requests.
func (t *Transition) Recover(ctx context.Context, resolver RecoveryResolver) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.state != transitionClosed {
		return errTransitionState
	}
	if resolver == nil {
		t.markUnresolved()
		return &RecoveryUnresolvedError{Cause: errors.New("nil recovery resolver")}
	}
	proof, err := resolver.Resolve(ctx)
	if err != nil {
		t.markUnresolved()
		return &RecoveryUnresolvedError{Cause: err}
	}
	switch proof.Disposition {
	case RecoveryCommitted:
		if err := t.commit(proof.State); err != nil {
			t.markUnresolved()
			return &RecoveryUnresolvedError{Cause: err}
		}
		return nil
	case RecoveryNotCommitted:
		if err := t.recoverNotCommitted(proof.State); err != nil {
			t.markUnresolved()
			return &RecoveryUnresolvedError{Cause: err}
		}
		return nil
	default:
		t.markUnresolved()
		return &RecoveryUnresolvedError{Cause: errors.New("invalid recovery disposition")}
	}
}

func (t *Transition) commit(state CommittedState) error {
	retired, err := t.validateCommittedState(state)
	if err != nil {
		return err
	}
	for _, target := range t.targets {
		if target.global {
			target.fence.open(*state.DiscoveryGeneration)
			continue
		}
		if _, isRetired := retired[target.id]; isRetired {
			target.fence.retire()
			continue
		}
		target.fence.open(state.ResumeRevisions[target.id])
	}
	t.releaseOwners()
	t.state = transitionDone
	return nil
}

func (t *Transition) validateCommittedState(state CommittedState) (map[uuid.UUID]struct{}, error) {
	hasGlobal := false
	targets := make(map[uuid.UUID]struct{}, len(t.targets))
	for _, target := range t.targets {
		if target.global {
			hasGlobal = true
			continue
		}
		targets[target.id] = struct{}{}
	}
	if hasGlobal != (state.DiscoveryGeneration != nil) {
		return nil, errTransitionState
	}
	if state.DiscoveryGeneration != nil && *state.DiscoveryGeneration <= 0 {
		return nil, errTransitionState
	}
	retired := make(map[uuid.UUID]struct{}, len(state.RetiredResumes))
	for _, id := range state.RetiredResumes {
		if _, target := targets[id]; !target {
			return nil, errTransitionState
		}
		if _, duplicate := retired[id]; duplicate {
			return nil, errTransitionState
		}
		retired[id] = struct{}{}
	}
	for id, generation := range state.ResumeRevisions {
		if _, target := targets[id]; !target || generation <= 0 {
			return nil, errTransitionState
		}
		if _, isRetired := retired[id]; isRetired {
			return nil, errTransitionState
		}
	}
	for _, target := range t.targets {
		if target.global {
			continue
		}
		if _, isRetired := retired[target.id]; isRetired {
			continue
		}
		if _, ok := state.ResumeRevisions[target.id]; !ok {
			return nil, errTransitionState
		}
	}
	return retired, nil
}

func (t *Transition) releaseUnchanged() {
	for _, target := range t.targets {
		target.fence.open(target.expected)
	}
	t.releaseOwners()
	t.state = transitionDone
}

func (t *Transition) releaseBegun() {
	t.releaseOwners()
	t.state = transitionDone
}

func (t *Transition) recoverNotCommitted(state CommittedState) error {
	retired, err := t.validateCommittedState(state)
	if err != nil || len(retired) != 0 {
		return errTransitionState
	}
	for _, target := range t.targets {
		if target.global {
			if state.DiscoveryGeneration == nil || *state.DiscoveryGeneration != target.expected {
				return errTransitionState
			}
			continue
		}
		generation, ok := state.ResumeRevisions[target.id]
		if !ok || generation != target.expected {
			return errTransitionState
		}
	}
	for _, target := range t.targets {
		if target.global {
			target.fence.open(*state.DiscoveryGeneration)
			continue
		}
		target.fence.open(state.ResumeRevisions[target.id])
	}
	t.releaseOwners()
	t.state = transitionDone
	return nil
}

func (t *Transition) releaseOwners() {
	for _, target := range t.targets {
		target.fence.releaseOwner()
	}
}

func (t *Transition) markUnresolved() {
	for _, target := range t.targets {
		target.fence.markUnresolved()
	}
}
