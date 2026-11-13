//go:build !linux

package main

import "errors"

func readCgroupEvidence() (cgroupEvidence, error) {
	return cgroupEvidence{}, errors.New("cgroup_unavailable")
}

func finishCgroupEvidence(*cgroupEvidence) error {
	return errors.New("cgroup_read_failed")
}
