package renderprocess

import (
	"context"
	"errors"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestParseArgsRejectsUntrustedProofDescriptors(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"--extra-files", "0", "--proof-fd", "4", "--", "/bin/true"},
		{"--extra-files", "bad", "--proof-fd", "3", "--", "/bin/true"},
		{"--extra-files", "0", "--proof-fd", "3", "--"},
	} {
		if _, _, _, err := parseArgs(args); err == nil {
			t.Fatalf("accepted invalid arguments %#v", args)
		}
	}
}

func TestBindMembersPIDReuseClosesFDWithoutSignal(t *testing.T) {
	identities := []identity{{pid: 20, pgrp: 10, startTime: 1}, {pid: 20, pgrp: 10, startTime: 2}}
	reads, signals, closes := 0, 0, 0
	system := fakeProcessOS()
	system.readDir = func() ([]string, error) { return []string{"20"}, nil }
	system.readIdentity = func(int) (identity, error) { result := identities[reads]; reads++; return result, nil }
	system.pidfdSignal = func(int, syscall.Signal) error { signals++; return nil }
	system.close = func(int) error { closes++; return nil }
	if _, err := bindMembers(10, system); err == nil {
		t.Fatal("PID reuse was accepted")
	}
	if signals != 0 || closes != 1 {
		t.Fatalf("signals=%d closes=%d", signals, closes)
	}
}

func TestBindMembersObservationFaultsNeverComplete(t *testing.T) {
	for name, readErr := range map[string]error{"malformed": errors.New("malformed"), "unreadable": syscall.EACCES} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(context.Background())
			system := fakeProcessOS()
			system.readDir = func() ([]string, error) { return []string{"20"}, nil }
			system.readIdentity = func(int) (identity, error) { return identity{}, readErr }
			system.waitCadence = func(context.Context) error { cancel(readErr); return context.Cause(ctx) }
			if err := joinOwnedGroup(ctx, 10, system); !errors.Is(err, readErr) {
				t.Fatalf("join error=%v", err)
			}
		})
	}
}

func TestJoinSendFailureClosesEveryFDOnce(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	sentinel := errors.New("stop unresolved observation")
	closed := map[int]int{}
	polls := 0
	system := fakeProcessOS()
	system.readDir = func() ([]string, error) { return []string{"20", "21"}, nil }
	system.readIdentity = func(pid int) (identity, error) { return identity{pid: pid, pgrp: 10, startTime: 1}, nil }
	system.pidfdOpen = func(pid int) (int, error) { return pid + 100, nil }
	system.pidfdSignal = func(int, syscall.Signal) error { return syscall.EPERM }
	system.poll = func(int, time.Duration) (int16, error) { polls++; return 0, nil }
	system.close = func(fd int) error { closed[fd]++; return nil }
	system.waitCadence = func(context.Context) error { cancel(sentinel); return context.Cause(ctx) }
	if err := joinOwnedGroup(ctx, 10, system); !errors.Is(err, sentinel) {
		t.Fatalf("join error=%v", err)
	}
	if polls != 2 || closed[120] != 1 || closed[121] != 1 {
		t.Fatalf("polls=%d closes=%v", polls, closed)
	}
}

func TestBindMembersPollFaultsCloseFDAndReject(t *testing.T) {
	for name, poll := range map[string]func(int, time.Duration) (int16, error){
		"poll-error": func(int, time.Duration) (int16, error) { return 0, syscall.EIO },
		"poll-nval":  func(int, time.Duration) (int16, error) { return unix.POLLNVAL, nil },
	} {
		t.Run(name, func(t *testing.T) {
			closes := 0
			system := oneMemberProcessOS()
			system.poll = poll
			system.close = func(int) error { closes++; return nil }
			if _, err := bindMembers(10, system); err == nil || closes != 1 {
				t.Fatalf("error=%v closes=%d", err, closes)
			}
		})
	}
}

func TestBindMembersDisappearedCurrentCloseFailureClosesPriorMembers(t *testing.T) {
	reads := map[int]int{}
	closes := map[int]int{}
	system := fakeProcessOS()
	system.readDir = func() ([]string, error) { return []string{"20", "21"}, nil }
	system.readIdentity = func(pid int) (identity, error) {
		reads[pid]++
		if pid == 21 && reads[pid] == 2 {
			return identity{}, syscall.ENOENT
		}
		return identity{pid: pid, pgrp: 10, startTime: 1}, nil
	}
	system.pidfdOpen = func(pid int) (int, error) { return pid + 100, nil }
	system.close = func(fd int) error {
		closes[fd]++
		if fd == 121 {
			return syscall.EIO
		}
		return nil
	}
	if _, err := bindMembers(10, system); !errors.Is(err, syscall.EIO) {
		t.Fatalf("bind error=%v", err)
	}
	if closes[120] != 1 || closes[121] != 1 {
		t.Fatalf("closes=%v", closes)
	}
}

func TestBindMembersTerminalCurrentCloseFailureClosesPriorMembers(t *testing.T) {
	closes := map[int]int{}
	system := fakeProcessOS()
	system.readDir = func() ([]string, error) { return []string{"20", "21"}, nil }
	system.readIdentity = func(pid int) (identity, error) { return identity{pid: pid, pgrp: 10, startTime: 1}, nil }
	system.pidfdOpen = func(pid int) (int, error) { return pid + 100, nil }
	system.poll = func(fd int, _ time.Duration) (int16, error) {
		if fd == 121 {
			return unix.POLLIN, nil
		}
		return 0, nil
	}
	system.close = func(fd int) error {
		closes[fd]++
		if fd == 121 {
			return syscall.EIO
		}
		return nil
	}
	if _, err := bindMembers(10, system); !errors.Is(err, syscall.EIO) {
		t.Fatalf("bind error=%v", err)
	}
	if closes[120] != 1 || closes[121] != 1 {
		t.Fatalf("closes=%v", closes)
	}
}

func TestTerminalPidfdAllowsCompletionWithoutReap(t *testing.T) {
	system := oneMemberProcessOS()
	closed := 0
	system.poll = func(int, time.Duration) (int16, error) { return unix.POLLIN | unix.POLLHUP, nil }
	system.close = func(int) error { closed++; return nil }
	if err := joinOwnedGroup(context.Background(), 10, system); err != nil || closed != 1 {
		t.Fatalf("join error=%v closes=%d", err, closed)
	}
}

func TestPreflightFailureClosesProbeAndCannotLaunch(t *testing.T) {
	for name, configure := range map[string]func(*processOS){
		"signal-zero": func(system *processOS) {
			system.pidfdSignal = func(int, syscall.Signal) error { return syscall.ENOSYS }
		},
		"poll": func(system *processOS) {
			system.poll = func(int, time.Duration) (int16, error) { return unix.POLLNVAL, nil }
		},
	} {
		t.Run(name, func(t *testing.T) {
			closed := 0
			system := fakeProcessOS()
			system.close = func(int) error { closed++; return nil }
			configure(&system)
			if err := preflightProcessOS(10, system); err == nil || closed != 1 {
				t.Fatalf("error=%v closes=%d", err, closed)
			}
		})
	}
}

func fakeProcessOS() processOS {
	return processOS{
		readDir:      func() ([]string, error) { return nil, nil },
		readIdentity: func(pid int) (identity, error) { return identity{pid: pid}, nil },
		pidfdOpen:    func(pid int) (int, error) { return pid, nil },
		pidfdSignal:  func(int, syscall.Signal) error { return nil },
		poll:         func(int, time.Duration) (int16, error) { return 0, nil },
		close:        func(int) error { return nil },
		waitCadence:  func(ctx context.Context) error { return context.Cause(ctx) },
	}
}

func oneMemberProcessOS() processOS {
	system := fakeProcessOS()
	system.readDir = func() ([]string, error) { return []string{"20"}, nil }
	system.readIdentity = func(int) (identity, error) { return identity{pid: 20, pgrp: 10, startTime: 1}, nil }
	system.pidfdOpen = func(int) (int, error) { return 120, nil }
	return system
}

func TestParseArgsPreservesCommandAndExtraFileCount(t *testing.T) {
	n, proofFD, command, err := parseArgs([]string{"--extra-files", "2", "--proof-fd", "5", "--", "/usr/bin/env", "-i", "/bin/true"})
	if err != nil || n != 2 || proofFD != 5 || len(command) != 3 || command[0] != "/usr/bin/env" {
		t.Fatalf("parsed n=%d proof=%d command=%q err=%v", n, proofFD, command, err)
	}
}
