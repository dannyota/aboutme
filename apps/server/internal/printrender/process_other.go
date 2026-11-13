//go:build !linux

package printrender

import "syscall"

// Non-Linux hosts have no parent-death signal (Linux prctl PR_SET_PDEATHSIG), so the supervisor or watchdog cleans up.
func browserProcessAttributes() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
