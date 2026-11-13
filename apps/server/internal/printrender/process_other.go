//go:build !linux

package printrender

import "syscall"

// This build sets no parent-death signal outside Linux (syscall.SysProcAttr.Pdeathsig), so the supervisor or watchdog cleans up.
func browserProcessAttributes() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
