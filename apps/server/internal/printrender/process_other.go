//go:build !linux

package printrender

import "syscall"

// Local non-Linux development has no parent-death signal, so the supervisor or watchdog must clean up.
func browserProcessAttributes() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
