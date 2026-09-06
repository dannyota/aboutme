package accountapi

import (
	"strings"
	"testing"
)

func TestNewRejectsMissingDependencies(t *testing.T) {
	_, err := New(Dependencies{})
	if err == nil || !strings.Contains(err.Error(), "pool") {
		t.Fatalf("New(Dependencies{}) error = %v, want missing pool", err)
	}
}
