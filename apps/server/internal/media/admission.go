package media

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrMediaBusy = errors.New("media: photo intake busy")

type PhotoAdmission struct {
	permit chan struct{}
	wait   time.Duration
}

func NewPhotoAdmission() *PhotoAdmission {
	return newPhotoAdmission(time.Second)
}

func newPhotoAdmission(wait time.Duration) *PhotoAdmission {
	return &PhotoAdmission{permit: make(chan struct{}, 1), wait: wait}
}

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
