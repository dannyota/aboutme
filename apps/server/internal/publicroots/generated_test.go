package publicroots

import "testing"

func TestReservedAPI(t *testing.T) {
	t.Parallel()

	if !Reserved("api") {
		t.Fatal("api must be reserved")
	}
}
