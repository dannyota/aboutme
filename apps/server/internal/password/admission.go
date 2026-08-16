package password

import (
	"context"
	"errors"
)

// ErrHashAdmission is the closed error returned when the hash/verify admission
// queue is already full.
var ErrHashAdmission = errors.New("password hash admission unavailable")

// admission running/waiting budget from D2: at most two hash/verify jobs run
// concurrently and at most sixteen wait for a slot; the seventeenth waiter
// fails immediately.
const (
	admissionRunning = 2
	admissionWaiting = 16
)

// Admission bounds concurrent Argon2id hash/verify work. A process runs at most
// two jobs and queues at most sixteen; the seventeenth waiter fails
// immediately with ErrHashAdmission. Release must be called exactly once for
// every successful Acquire, on both the result and every failure path.
type Admission struct {
	// slots is the running permit: two buffered tokens, one consumed by each
	// Acquire that gets a slot and returned by the matching Release.
	slots chan struct{}
	// queue bounds the number of waiters: a token is held only while blocked
	// waiting for a slot, then returned as soon as Acquire returns.
	queue chan struct{}
}

// NewAdmission returns an idle admission controller.
func NewAdmission() *Admission {
	a := &Admission{
		slots: make(chan struct{}, admissionRunning),
		queue: make(chan struct{}, admissionWaiting),
	}
	for range admissionRunning {
		a.slots <- struct{}{}
	}
	return a
}

// Acquire waits for a running slot, honoring ctx cancellation. It returns
// ErrHashAdmission immediately when the waiting queue is full, and ctx.Err()
// when the context is canceled or has already expired.
func (a *Admission) Acquire(ctx context.Context) error {
	select {
	case a.queue <- struct{}{}:
	default:
		return ErrHashAdmission
	}
	defer func() { <-a.queue }()

	if ctx.Err() != nil {
		return ctx.Err()
	}
	select {
	case <-a.slots:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release returns one running slot. It must be called exactly once per
// successful Acquire; the slot is not released on an Acquire error.
func (a *Admission) Release() {
	a.slots <- struct{}{}
}
