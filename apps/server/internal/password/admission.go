package password

import (
	"context"
	"errors"
)

var ErrHashAdmission = errors.New("password hash admission unavailable")

const (
	admissionRunning = 2
	admissionWaiting = 16
)

type Admission struct {
	slots chan struct{}
	queue chan struct{}
}

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

func (a *Admission) Release() {
	a.slots <- struct{}{}
}
