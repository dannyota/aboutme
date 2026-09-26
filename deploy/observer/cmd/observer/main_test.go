package main

import "testing"

func TestDispatchVersion(t *testing.T) {
	if err := dispatch([]string{"version"}); err != nil {
		t.Fatalf("dispatch([version]): %v", err)
	}
}

func TestDispatchNoArgs(t *testing.T) {
	if err := dispatch(nil); err == nil {
		t.Fatal("dispatch(nil): got nil error")
	}
}

func TestDispatchUnknownCommand(t *testing.T) {
	if err := dispatch([]string{"launch-the-missiles"}); err == nil {
		t.Fatal("dispatch([launch-the-missiles]): got nil error")
	}
}

func TestVersionString(t *testing.T) {
	oldVersion, oldCommit := version, commit
	t.Cleanup(func() { version, commit = oldVersion, oldCommit })

	version, commit = "", ""
	if got := versionString(); got != "observer  " {
		t.Errorf("versionString() with unset build info = %q, want %q", got, "observer  ")
	}

	version, commit = "v0.6.5", "deadbeef"
	if got := versionString(); got != "observer v0.6.5 deadbeef" {
		t.Errorf("versionString() = %q, want %q", got, "observer v0.6.5 deadbeef")
	}
}
