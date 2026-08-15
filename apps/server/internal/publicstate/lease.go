package publicstate

import (
	"context"
	"sync"
)

// Lease keeps a public response admitted until it is released or canceled.
type Lease struct {
	ctx            context.Context
	cancel         context.CancelFunc
	fence          *fence
	set            *leaseSet
	representation Representation
	metrics        *fenceMetrics

	mu       sync.Mutex
	hooks    []func()
	canceled bool
	released bool
}

func newLease(parent context.Context, fence *fence, set *leaseSet, representation Representation) *Lease {
	ctx, cancel := context.WithCancel(parent)
	return &Lease{ctx: ctx, cancel: cancel, fence: fence, set: set, representation: representation}
}

// Context is canceled when the fence revokes this lease.
func (l *Lease) Context() context.Context { return l.ctx }

// OnCancel registers hook to run once if the fence revokes this lease.
func (l *Lease) OnCancel(hook func()) error {
	if hook == nil {
		return errorsNewNilCancelHook()
	}
	l.mu.Lock()
	if l.canceled {
		l.mu.Unlock()
		hook()
		return nil
	}
	l.hooks = append(l.hooks, hook)
	l.mu.Unlock()
	return nil
}

func (l *Lease) cancelLease() {
	l.mu.Lock()
	if l.canceled {
		l.mu.Unlock()
		return
	}
	l.canceled = true
	hooks := append([]func(){}, l.hooks...)
	l.mu.Unlock()
	l.cancel()
	for _, hook := range hooks {
		hook()
	}
}

// Release ends the lease and is safe to call more than once.
func (l *Lease) Release() {
	l.mu.Lock()
	if l.released {
		l.mu.Unlock()
		return
	}
	l.released = true
	l.mu.Unlock()
	l.cancel()
	l.fence.release(l, l.metrics)
}
