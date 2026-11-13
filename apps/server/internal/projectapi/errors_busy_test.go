package projectapi

import (
	"testing"
	"time"

	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/resumeapi"
)

func TestMapError_PublicStateBusy(t *testing.T) {
	for name, err := range map[string]error{
		"replanned": resumeapi.ErrPublicStateBusy,
		"drain":     &publicstate.DrainTimeoutError{Deadline: time.Unix(0, 0)},
	} {
		got := mapError(err)
		if got.status != 503 || got.code != "public_state_busy" || got.retryAfter != 1 {
			t.Fatalf("%s = %d %s retry %d, want 503 public_state_busy retry 1", name, got.status, got.code, got.retryAfter)
		}
	}
}
