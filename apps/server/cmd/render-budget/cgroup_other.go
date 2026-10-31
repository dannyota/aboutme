//go:build !linux

package main

import "errors"

// Budget measurements require Linux cgroup evidence and fail closed on local non-Linux hosts.
func readCgroupEvidence() (cgroupEvidence, error) {
	return cgroupEvidence{}, errors.New("cgroup_unavailable")
}

func finishCgroupEvidence(*cgroupEvidence) error {
	return errors.New("cgroup_read_failed")
}
