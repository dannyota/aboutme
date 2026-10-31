//go:build !linux

package realtimebench_test

import "testing"

// The stress measurement skips non-Linux hosts before collecting kernel evidence.
func kernelRelease() string { return "unknown" }

func TestKernelReleaseUnavailable(t *testing.T) {
	if got := kernelRelease(); got != "unknown" {
		t.Fatalf("kernel release = %q, want unknown", got)
	}
}
