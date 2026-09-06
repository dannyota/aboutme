package testutil

import (
	"errors"
	"sync"
	"testing"
)

type testSetupError struct{}

func (*testSetupError) Error() string { return "setup failed" }

func TestTestDatabaseSetupCacheConcurrentCallersShareOnePreparation(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var calls int
	cache := newTestDatabaseSetupCache(func(_ string) error {
		calls++
		once.Do(func() { close(started) })
		<-release
		return nil
	})

	const callers = 8
	errorsByCaller := make(chan error, callers)
	for range callers {
		go func() { errorsByCaller <- cache.prepare("postgres://shared") }()
	}
	<-started
	select {
	case err := <-errorsByCaller:
		t.Fatalf("prepare returned before shared setup completed: %v", err)
	default:
	}
	close(release)
	for range callers {
		if err := <-errorsByCaller; err != nil {
			t.Fatalf("prepare error: %v", err)
		}
	}
	if calls != 1 {
		t.Errorf("setup calls = %d, want 1", calls)
	}
}

func TestTestDatabaseSetupCacheCachesFailureForEveryCaller(t *testing.T) {
	t.Parallel()

	want := &testSetupError{}
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var calls int
	cache := newTestDatabaseSetupCache(func(_ string) error {
		calls++
		once.Do(func() { close(started) })
		<-release
		return want
	})

	const callers = 8
	errorsByCaller := make(chan error, callers)
	for range callers {
		go func() { errorsByCaller <- cache.prepare("postgres://shared") }()
	}
	<-started
	close(release)
	for range callers {
		var got *testSetupError
		if err := <-errorsByCaller; !errors.As(err, &got) || got != want {
			t.Errorf("prepare error = %v, want same error %v", got, want)
		}
	}
	var got *testSetupError
	if err := cache.prepare("postgres://shared"); !errors.As(err, &got) || got != want {
		t.Errorf("cached prepare error = %v, want same error %v", got, want)
	}
	if calls != 1 {
		t.Errorf("setup calls = %d, want 1", calls)
	}
}

func TestTestDatabaseSetupCacheKeepsDSNsIndependent(t *testing.T) {
	t.Parallel()

	started := make(chan string, 2)
	release := make(chan struct{})
	cache := newTestDatabaseSetupCache(func(dsn string) error {
		started <- dsn
		<-release
		return nil
	})
	errorsByCaller := make(chan error, 2)
	go func() { errorsByCaller <- cache.prepare("postgres://first") }()
	go func() { errorsByCaller <- cache.prepare("postgres://second") }()

	seen := map[string]bool{<-started: true, <-started: true}
	if !seen["postgres://first"] || !seen["postgres://second"] {
		t.Fatalf("independent setup keys started = %v", seen)
	}
	close(release)
	for range 2 {
		if err := <-errorsByCaller; err != nil {
			t.Fatalf("prepare error: %v", err)
		}
	}
}

func TestTestDatabaseSetupCacheDoesNotRepeatSuccessfulPreparation(t *testing.T) {
	t.Parallel()

	var calls int
	cache := newTestDatabaseSetupCache(func(_ string) error {
		calls++
		return nil
	})
	if err := cache.prepare("postgres://shared"); err != nil {
		t.Fatalf("first prepare error: %v", err)
	}
	if err := cache.prepare("postgres://shared"); err != nil {
		t.Fatalf("second prepare error: %v", err)
	}
	if calls != 1 {
		t.Errorf("setup calls = %d, want 1", calls)
	}
}
