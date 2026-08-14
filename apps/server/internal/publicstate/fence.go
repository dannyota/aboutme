package publicstate

import (
	"context"
	"errors"
	"sync"
)

type fenceState uint8

const (
	fenceOpen fenceState = iota
	fenceClosing
	fenceClosed
	fenceRetired
)

type leaseSet struct {
	generation int64
	leases     map[*Lease]struct{}
	sealed     bool
	done       chan struct{}
}

type fence struct {
	mu         sync.Mutex
	state      fenceState
	generation int64
	current    *leaseSet
	sets       map[*leaseSet]struct{}
	owner      bool
	ownerDone  chan struct{}
	unresolved bool
}

func newFence(generation int64) *fence {
	set := &leaseSet{generation: generation, leases: make(map[*Lease]struct{}), done: make(chan struct{})}
	return &fence{
		state:      fenceOpen,
		generation: generation,
		current:    set,
		sets:       map[*leaseSet]struct{}{set: {}},
		ownerDone:  make(chan struct{}),
	}
}

func (f *fence) acquire(ctx context.Context, expected int64, rep Representation, metrics *fenceMetrics) (*Lease, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.state != fenceOpen {
		return nil, ErrAdmissionClosed
	}
	if f.generation != expected {
		metrics.recordMismatch()
		return nil, &GenerationMismatchError{Expected: expected, Actual: f.generation}
	}
	lease := newLease(ctx, f, f.current, rep)
	lease.metrics = metrics
	f.current.leases[lease] = struct{}{}
	metrics.recordLease(rep, 1)
	return lease, nil
}

func (f *fence) release(lease *Lease, metrics *fenceMetrics) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := lease.set.leases[lease]; !ok {
		return
	}
	delete(lease.set.leases, lease)
	if len(lease.set.leases) == 0 && lease.set != f.current {
		delete(f.sets, lease.set)
	}
	if lease.set.sealed && len(lease.set.leases) == 0 {
		select {
		case <-lease.set.done:
		default:
			close(lease.set.done)
		}
	}
	metrics.recordLease(lease.representation, -1)
}

func (f *fence) claim(ctx context.Context, expected int64) error {
	for {
		f.mu.Lock()
		if !f.owner {
			if f.state != fenceOpen {
				f.mu.Unlock()
				return ErrAdmissionClosed
			}
			if f.generation != expected {
				actual := f.generation
				f.mu.Unlock()
				return &GenerationMismatchError{Expected: expected, Actual: actual}
			}
			f.owner = true
			f.ownerDone = make(chan struct{})
			f.mu.Unlock()
			return nil
		}
		done := f.ownerDone
		f.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
		}
	}
}

func (f *fence) releaseOwner() {
	f.mu.Lock()
	if f.owner {
		f.owner = false
		close(f.ownerDone)
	}
	f.mu.Unlock()
}

func (f *fence) close(drain bool) []*Lease {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = fenceClosing
	if !drain {
		return nil
	}
	leases := make([]*Lease, 0)
	for set := range f.sets {
		set.sealed = true
		if len(set.leases) == 0 {
			select {
			case <-set.done:
			default:
				close(set.done)
			}
		}
		for lease := range set.leases {
			leases = append(leases, lease)
		}
	}
	return leases
}

func (f *fence) drainSets() []*leaseSet {
	f.mu.Lock()
	defer f.mu.Unlock()
	sets := make([]*leaseSet, 0, len(f.sets))
	for set := range f.sets {
		if set.sealed {
			sets = append(sets, set)
		}
	}
	return sets
}

func (f *fence) markClosed() {
	f.mu.Lock()
	f.state = fenceClosed
	f.mu.Unlock()
}

func (f *fence) open(generation int64) {
	f.mu.Lock()
	for set := range f.sets {
		if len(set.leases) == 0 {
			delete(f.sets, set)
		}
	}
	set := &leaseSet{generation: generation, leases: make(map[*Lease]struct{}), done: make(chan struct{})}
	f.generation = generation
	f.current = set
	f.sets[set] = struct{}{}
	f.state = fenceOpen
	f.unresolved = false
	f.mu.Unlock()
}

func (f *fence) retire() {
	f.mu.Lock()
	f.state = fenceRetired
	f.unresolved = false
	f.mu.Unlock()
}

func (f *fence) markUnresolved() {
	f.mu.Lock()
	f.state = fenceClosed
	f.unresolved = true
	f.mu.Unlock()
}

func (f *fence) ready() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.unresolved {
		return errors.New("publicstate: recovery unresolved")
	}
	if f.generation <= 0 || f.current == nil {
		return errors.New("publicstate: invalid generation fence")
	}
	if (f.state == fenceClosing || f.state == fenceClosed) && !f.owner {
		return errors.New("publicstate: invalid generation fence")
	}
	if f.state == fenceRetired && f.owner {
		return errors.New("publicstate: invalid generation fence")
	}
	return nil
}
