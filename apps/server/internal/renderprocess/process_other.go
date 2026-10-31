//go:build !linux

package renderprocess

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Local non-Linux supervision rechecks group and start time before signaling,
// but lacks Linux pidfds' atomic protection against reuse after the last check.
var realProcessOS = portableProcessOS(readIdentity, processEntries, syscall.Kill)

func portableProcessOS(read func(int) (identity, error), entries func() ([]string, error), kill func(int, syscall.Signal) error) processOS {
	bound := map[int]identity{}
	next := 0
	return processOS{
		readDir:      entries,
		readIdentity: read,
		pidfdOpen: func(pid int) (int, error) {
			observed, err := read(pid)
			if errors.Is(err, os.ErrNotExist) {
				return 0, syscall.ESRCH
			}
			if err != nil {
				return 0, err
			}
			next++
			bound[next] = observed
			return next, nil
		},
		pidfdSignal: func(handle int, number syscall.Signal) error {
			before, ok := bound[handle]
			if !ok {
				return syscall.EBADF
			}
			after, err := read(before.pid)
			if errors.Is(err, os.ErrNotExist) || (err == nil && after != before) {
				return syscall.ESRCH
			}
			if err != nil {
				return err
			}
			return kill(before.pid, number)
		},
		poll: func(handle int, timeout time.Duration) (int16, error) {
			before, ok := bound[handle]
			if !ok {
				return unix.POLLNVAL, nil
			}
			after, err := read(before.pid)
			if errors.Is(err, os.ErrNotExist) || (err == nil && after != before) {
				return unix.POLLIN, nil
			}
			if err != nil {
				return 0, err
			}
			if timeout > 0 {
				time.Sleep(timeout)
			}
			return 0, nil
		},
		close: func(handle int) error {
			delete(bound, handle)
			return nil
		},
		waitCadence: func(ctx context.Context) error {
			timer := time.NewTimer(observationCadence)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return context.Cause(ctx)
			case <-timer.C:
				return nil
			}
		},
	}
}
