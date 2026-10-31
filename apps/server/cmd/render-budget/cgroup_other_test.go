//go:build !linux

package main

import "testing"

func TestCgroupEvidenceUnavailable(t *testing.T) {
	evidence, err := readCgroupEvidence()
	if err == nil || err.Error() != "cgroup_unavailable" || evidence.Version != 0 {
		t.Fatalf("cgroup evidence = %#v, error = %v", evidence, err)
	}
	if err := finishCgroupEvidence(&evidence); err == nil || err.Error() != "cgroup_read_failed" {
		t.Fatalf("finish cgroup evidence error = %v", err)
	}
}
