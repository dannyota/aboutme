package media

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrMediaBusy reports that the task-wide decoder permit was not acquired.
var ErrMediaBusy = errors.New("media: photo intake busy")

// PhotoAdmission limits image decoding and its pixel buffers to one request
// per server task.
type PhotoAdmission struct {
	permit chan struct{}
	wait   time.Duration
}

// NewPhotoAdmission creates the production one-at-a-time photo admission gate.
func NewPhotoAdmission() *PhotoAdmission {
	return newPhotoAdmission(time.Second)
}

func newPhotoAdmission(wait time.Duration) *PhotoAdmission {
	return &PhotoAdmission{permit: make(chan struct{}, 1), wait: wait}
}

// Acquire waits for the task-wide photo permit. The returned release function
// is safe to call more than once.
func (a *PhotoAdmission) Acquire(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	timer := time.NewTimer(a.wait)
	defer timer.Stop()

	select {
	case a.permit <- struct{}{}:
		var once sync.Once
		return func() {
			once.Do(func() { <-a.permit })
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, ErrMediaBusy
	}
}
