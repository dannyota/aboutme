package publicstate

import (
	"context"
	"errors"
	"testing"
)

func TestReadinessRejectsEachRequiredDependency(t *testing.T) {
	t.Parallel()

	t.Run("missing coordinator", func(t *testing.T) {
		databaseCalled, rendererCalled := false, false
		readiness := NewReadiness(nil, ReadinessDependencies{
			PingDatabase:  func(context.Context) error { databaseCalled = true; return nil },
			ProbeRenderer: func(context.Context) error { rendererCalled = true; return nil },
		})
		if err := readiness.Ping(context.Background()); err == nil {
			t.Fatal("Ping() error = nil, want unavailable")
		}
		if databaseCalled || rendererCalled {
			t.Fatalf("missing coordinator ran dependencies: database=%t renderer=%t", databaseCalled, rendererCalled)
		}
	})

	newCoordinator := func(t *testing.T) *Coordinator {
		t.Helper()
		coordinator, err := NewCoordinator(CoordinatorConfig{DiscoveryGeneration: 41})
		if err != nil {
			t.Fatal(err)
		}
		return coordinator
	}
	t.Run("database failure", func(t *testing.T) {
		rendererCalled := false
		readiness := NewReadiness(newCoordinator(t), ReadinessDependencies{
			PingDatabase:  func(context.Context) error { return errors.New("db down") },
			ProbeRenderer: func(context.Context) error { rendererCalled = true; return nil },
		})
		if err := readiness.Ping(context.Background()); err == nil {
			t.Fatal("Ping() error = nil, want unavailable")
		}
		if rendererCalled {
			t.Fatal("renderer probe ran after database failure")
		}
	})
	t.Run("renderer failure", func(t *testing.T) {
		databaseCalled, rendererCalled := false, false
		readiness := NewReadiness(newCoordinator(t), ReadinessDependencies{
			PingDatabase:  func(context.Context) error { databaseCalled = true; return nil },
			ProbeRenderer: func(context.Context) error { rendererCalled = true; return errors.New("renderer down") },
		})
		if err := readiness.Ping(context.Background()); err == nil {
			t.Fatal("Ping() error = nil, want unavailable")
		}
		if !databaseCalled || !rendererCalled {
			t.Fatalf("renderer failure checks = database:%t renderer:%t, want both", databaseCalled, rendererCalled)
		}
	})
}

func TestReadinessAcceptsCoordinatorDatabaseAndRenderer(t *testing.T) {
	t.Parallel()

	coordinator, err := NewCoordinator(CoordinatorConfig{DiscoveryGeneration: 41})
	if err != nil {
		t.Fatal(err)
	}
	readiness := NewReadiness(coordinator, ReadinessDependencies{
		PingDatabase:  func(context.Context) error { return nil },
		ProbeRenderer: func(context.Context) error { return nil },
	})
	if err := readiness.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
}
