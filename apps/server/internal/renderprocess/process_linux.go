//go:build linux

package renderprocess

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

var realProcessOS = processOS{
	readDir: func() ([]string, error) {
		entries, err := os.ReadDir("/proc")
		if err != nil {
			return nil, err
		}
		names := make([]string, len(entries))
		for i := range entries {
			names[i] = entries[i].Name()
		}
		return names, nil
	},
	readIdentity: readIdentity,
	pidfdOpen:    func(pid int) (int, error) { return unix.PidfdOpen(pid, 0) },
	pidfdSignal: func(fd int, signal syscall.Signal) error {
		return unix.PidfdSendSignal(fd, signal, nil, 0)
	},
	poll: func(fd int, timeout time.Duration) (int16, error) {
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}} //nolint:gosec // pidfd is kernel allocated
		_, err := unix.Poll(poll, int(timeout.Milliseconds()))
		return poll[0].Revents, err
	},
	close: unix.Close,
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

func readIdentity(pid int) (identity, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return identity{}, err
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 || end+2 >= len(data) {
		return identity{}, errors.New("malformed process observation")
	}
	fields := strings.Fields(string(data[end+2:]))
	if len(fields) < 20 {
		return identity{}, errors.New("malformed process observation")
	}
	pgrp, err := strconv.Atoi(fields[2])
	if err != nil {
		return identity{}, errors.New("malformed process observation")
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return identity{}, errors.New("malformed process observation")
	}
	return identity{pid: pid, pgrp: pgrp, startTime: start}, nil
}
