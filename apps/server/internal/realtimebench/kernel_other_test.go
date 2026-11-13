//go:build !linux

package realtimebench_test

import "testing"

func kernelRelease() string { return "unknown" }

func TestKernelReleaseUnavailable(t *testing.T) {
	if got := kernelRelease(); got != "unknown" {
		t.Fatalf("kernel release = %q, want unknown", got)
	}
}
