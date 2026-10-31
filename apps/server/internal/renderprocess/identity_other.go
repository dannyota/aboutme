//go:build !linux && !darwin

package renderprocess

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func processEntries() ([]string, error) {
	data, err := exec.Command("ps", "-axo", "pid=").Output()
	return strings.Fields(string(data)), err
}

func readIdentity(pid int) (identity, error) {
	cmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "pgid=,stat=,lstart=")
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	data, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && len(data) == 0 {
			return identity{}, os.ErrNotExist
		}
		return identity{}, err
	}
	fields := strings.Fields(string(data))
	if len(fields) != 7 {
		return identity{}, errors.New("malformed process observation")
	}
	if strings.HasPrefix(fields[1], "Z") {
		return identity{}, os.ErrNotExist
	}
	group, err := strconv.Atoi(fields[0])
	if err != nil {
		return identity{}, err
	}
	started, err := time.Parse("Mon Jan 2 15:04:05 2006", strings.Join(fields[2:], " "))
	if err != nil || started.Unix() <= 0 {
		return identity{}, errors.New("malformed process observation")
	}
	return identity{pid: pid, pgrp: group, startTime: uint64(started.Unix())}, nil
}
