package main

import (
	"strings"
	"testing"
)

func withOwner(t *testing.T, value string) {
	t.Helper()
	old := owner
	t.Cleanup(func() { owner = old })
	owner = value
}

func TestDispatchVersion(t *testing.T) {
	for _, value := range []string{"dannyota", "dntmakers"} {
		withOwner(t, value)
		if err := dispatch([]string{"version"}); err != nil {
			t.Fatalf("dispatch([version]) built by %s: %v", value, err)
		}
	}
}

func TestDispatchRefusesWithoutOwner(t *testing.T) {
	for _, value := range []string{"", "someone", "Dannyota"} {
		withOwner(t, value)
		for _, args := range [][]string{{"version"}, {"run", "--interval", "0s"}} {
			err := dispatch(args)
			if err == nil || !strings.Contains(err.Error(), "owner") {
				t.Errorf("owner %q: dispatch(%v) = %v, want an owner refusal", value, args, err)
			}
		}
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

func TestDispatchRefusesLambda(t *testing.T) {
	clearObserverEnv(t)
	withOwner(t, "dannyota")
	err := dispatch([]string{"lambda"})
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("dispatch([lambda]) = %v, want an unknown command", err)
	}
}

func TestVersionString(t *testing.T) {
	oldVersion, oldCommit := version, commit
	t.Cleanup(func() { version, commit = oldVersion, oldCommit })

	version, commit = "", ""
	withOwner(t, "")
	if got := versionString(); got != "observer   " {
		t.Errorf("versionString() with unset build info = %q, want %q", got, "observer   ")
	}

	version, commit = "v0.6.5", "deadbeef"
	for _, value := range []string{"dannyota", "dntmakers"} {
		withOwner(t, value)
		want := "observer v0.6.5 deadbeef " + value
		if got := versionString(); got != want {
			t.Errorf("versionString() = %q, want %q", got, want)
		}
	}
}
