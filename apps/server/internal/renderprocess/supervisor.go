// Package renderprocess supervises one isolated browser process group.
package renderprocess

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const observationCadence = 10 * time.Millisecond

type identity struct {
	pid       int
	pgrp      int
	startTime uint64
}

type processOS struct {
	readDir      func() ([]string, error)
	readIdentity func(int) (identity, error)
	pidfdOpen    func(int) (int, error)
	pidfdSignal  func(int, syscall.Signal) error
	poll         func(int, time.Duration) (int16, error)
	close        func(int) error
	waitCadence  func(context.Context) error
}

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

// Run supervises one browser process and does not return until its process group is terminal.
func Run(args []string) int {
	extraFiles, proofFD, command, err := parseArgs(args)
	if err != nil {
		return fail("invalid arguments")
	}
	proof := os.NewFile(uintptr(proofFD), "join-proof")
	if proof == nil {
		return fail("invalid proof pipe")
	}
	defer proof.Close() //nolint:errcheck
	launched := false
	defer func() {
		if !launched {
			ignoreWrite(proof.Write([]byte{'D'}))
		}
	}()
	unix.CloseOnExec(proofFD)
	anchor := os.Getpid()
	group := unix.Getpgrp()
	if group != anchor {
		return fail("invalid process group")
	}
	if err := preflightProcessOS(anchor, realProcessOS); err != nil {
		return fail("pidfd unavailable")
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer signal.Stop(signals)
	select {
	case <-signals:
		return fail("canceled before launch")
	default:
	}

	// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command -- argv comes from the server process that starts this supervisor and parseArgs checks its shape
	cmd := exec.CommandContext(context.Background(), command[0], command[1:]...) //nolint:gosec // validated argv is the supervisor's input contract
	cmd.Env = make([]string, 0)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	for fd := 3; fd < 3+extraFiles; fd++ {
		cmd.ExtraFiles = append(cmd.ExtraFiles, os.NewFile(uintptr(fd), "inherited-extra-file"))
	}
	if err := cmd.Start(); err != nil {
		return fail("browser launch failed")
	}
	launched = true
	_, proofErr := proof.Write([]byte{'S'})
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	canceled := false
	var waitErr error
	select {
	case waitErr = <-done:
	case <-signals:
		canceled = true
	}
	if err := joinOwnedGroup(context.Background(), anchor, realProcessOS); err != nil {
		return fail("browser teardown unresolved")
	}
	if proofErr == nil {
		_, proofErr = proof.Write([]byte{'D'})
	}
	if proofErr != nil {
		return fail("proof unavailable")
	}
	if !canceled {
		return exitCode(waitErr)
	}
	<-done
	return fail("browser canceled")
}

func parseArgs(args []string) (int, int, []string, error) {
	if len(args) < 6 || args[0] != "--extra-files" || args[2] != "--proof-fd" || args[4] != "--" {
		return 0, 0, nil, errors.New("invalid supervisor arguments")
	}
	n, err := strconv.Atoi(args[1])
	proofFD, proofErr := strconv.Atoi(args[3])
	if err != nil || proofErr != nil || n < 0 || n > 64 || proofFD != 3+n || len(args[5:]) == 0 {
		return 0, 0, nil, errors.New("invalid supervisor arguments")
	}
	return n, proofFD, args[5:], nil
}

func preflightProcessOS(anchor int, system processOS) (resultErr error) {
	probe, err := system.pidfdOpen(anchor)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, system.close(probe)) }()
	if signalErr := system.pidfdSignal(probe, 0); signalErr != nil {
		return signalErr
	}
	revents, err := system.poll(probe, 0)
	if err != nil || revents&unix.POLLNVAL != 0 {
		return errors.Join(errors.New("pidfd observation failed"), err)
	}
	return nil
}

func joinOwnedGroup(ctx context.Context, group int, system processOS) error {
	for {
		members, err := bindMembers(group, system)
		if err != nil {
			if err := system.waitCadence(ctx); err != nil {
				return err
			}
			continue
		}
		if len(members) == 0 {
			return nil
		}
		resolved := true
		for _, fd := range members {
			if err := system.pidfdSignal(fd, unix.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
				resolved = false
				break
			}
		}
		for _, fd := range members {
			for resolved {
				if err := context.Cause(ctx); err != nil {
					resolved = false
					break
				}
				revents, err := system.poll(fd, observationCadence)
				if err != nil {
					resolved = false
					break
				}
				if revents&unix.POLLNVAL != 0 {
					resolved = false
					break
				}
				if revents&(unix.POLLIN|unix.POLLHUP) != 0 {
					break
				}
			}
		}
		if err := closePidfds(members, system); err != nil {
			resolved = false
		}
		if !resolved {
			if err := system.waitCadence(ctx); err != nil {
				return err
			}
		}
	}
}

func bindMembers(group int, system processOS) ([]int, error) {
	entries, err := system.readDir()
	if err != nil {
		return nil, err
	}
	var result []int
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry)
		if err != nil || pid == group {
			continue
		}
		before, err := system.readIdentity(pid)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			ignoreError(closePidfds(result, system))
			return nil, err
		}
		if before.pgrp != group {
			continue
		}
		fd, err := system.pidfdOpen(pid)
		if errors.Is(err, syscall.ESRCH) {
			continue
		}
		if err != nil {
			ignoreError(closePidfds(result, system))
			return nil, err
		}
		after, err := system.readIdentity(pid)
		if errors.Is(err, os.ErrNotExist) {
			if closeErr := system.close(fd); closeErr != nil {
				return nil, errors.Join(closeErr, closePidfds(result, system))
			}
			continue
		}
		if err != nil || after != before {
			closeErr := system.close(fd)
			priorCloseErr := closePidfds(result, system)
			if err != nil {
				return nil, errors.Join(err, closeErr, priorCloseErr)
			}
			return nil, errors.Join(errors.New("process identity changed during binding"), closeErr, priorCloseErr)
		}
		revents, err := system.poll(fd, 0)
		if err != nil || revents&unix.POLLNVAL != 0 {
			return nil, errors.Join(errors.New("pidfd observation failed"), err, system.close(fd), closePidfds(result, system))
		}
		if revents&(unix.POLLIN|unix.POLLHUP) != 0 {
			if closeErr := system.close(fd); closeErr != nil {
				return nil, errors.Join(closeErr, closePidfds(result, system))
			}
			continue
		}
		result = append(result, fd)
	}
	return result, nil
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

func closePidfds(fds []int, system processOS) error {
	var result error
	for _, fd := range fds {
		result = errors.Join(result, system.close(fd))
	}
	return result
}

func ignoreError(error)      {}
func ignoreWrite(int, error) {}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}

func fail(message string) int {
	_, _ = fmt.Fprintln(os.Stderr, "render-browser-supervisor:", message)
	return 1
}
