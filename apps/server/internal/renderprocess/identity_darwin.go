//go:build darwin

package renderprocess

import (
	"errors"
	"os"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

func processEntries() ([]string, error) {
	processes, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, err
	}
	entries := make([]string, 0, len(processes))
	for _, process := range processes {
		if process.Proc.P_pid <= 0 {
			continue
		}
		entries = append(entries, strconv.Itoa(int(process.Proc.P_pid)))
	}
	return entries, nil
}

func readIdentity(pid int) (identity, error) {
	process, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return identity{}, os.ErrNotExist
		}
		return identity{}, err
	}
	// Darwin's SZOMB state is terminal even before the parent reaps it.
	if process.Proc.P_stat == 5 {
		return identity{}, os.ErrNotExist
	}
	started := process.Proc.P_starttime
	if int(process.Proc.P_pid) != pid || started.Sec <= 0 || started.Usec < 0 {
		return identity{}, errors.New("malformed process observation")
	}
	return identity{
		pid:       pid,
		pgrp:      int(process.Eproc.Pgid),
		startTime: uint64(started.Sec)*1_000_000 + uint64(started.Usec),
	}, nil
}
