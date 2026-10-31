//go:build !linux

package renderprocess

import (
	"errors"
	"os"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestPortableBindingRevalidatesBeforeEverySignal(t *testing.T) {
	for _, change := range []string{"start", "group", "exit"} {
		t.Run(change, func(t *testing.T) {
			observed := identity{pid: 20, pgrp: 10, startTime: 1}
			var readErr error
			calls := 0
			system := portableProcessOS(func(int) (identity, error) {
				return observed, readErr
			}, func() ([]string, error) {
				return []string{"20"}, nil
			}, func(int, syscall.Signal) error {
				calls++
				return nil
			})
			handle, err := system.pidfdOpen(20)
			if err != nil {
				t.Fatal(err)
			}
			if err := system.pidfdSignal(handle, 0); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "start":
				observed.startTime++
			case "group":
				observed.pgrp++
			case "exit":
				readErr = os.ErrNotExist
			}
			if err := system.pidfdSignal(handle, syscall.SIGKILL); !errors.Is(err, syscall.ESRCH) || calls != 1 {
				t.Fatalf("err=%v signals=%d", err, calls)
			}
			if events, err := system.poll(handle, time.Duration(0)); err != nil || events != unix.POLLIN {
				t.Fatalf("events=%d err=%v", events, err)
			}
			if err := system.close(handle); err != nil {
				t.Fatal(err)
			}
		})
	}
}
