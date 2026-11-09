package printrender

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNew_RetriesVersionTimeoutBeforeInitializing(t *testing.T) {
	executable := writeExecutable(t, "#!/bin/sh\nexit 0\n")
	hooks := safeProcessHooks(t)
	attempts := 0
	hooks.version = func(ctx context.Context, _ string) (string, error) {
		attempts++
		if attempts == 1 {
			<-ctx.Done()
			return "", ctx.Err()
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return "Google Chrome for Testing 156.0.8078.4", nil
	}
	hooks.ready = func(context.Context, *Renderer) error { return errors.New("browser is not ready") }
	renderer, err := New(Config{BrowserExecutable: executable, RenderOrigin: testRenderOrigin(t), testHooks: hooks})
	if err != nil || renderer == nil {
		t.Fatalf("New() after a version timeout = %v, %v", renderer, err)
	}
	if attempts != 2 {
		t.Fatalf("version attempts = %d, want 2", attempts)
	}
	if readyErr := renderer.Ready(); !errors.Is(readyErr, ErrUnavailable) {
		t.Fatalf("Ready() before browser readiness = %v, want ErrUnavailable", readyErr)
	}
}

func TestNew_FailsClosedAfterVersionTimeoutRetry(t *testing.T) {
	executable := writeExecutable(t, "#!/bin/sh\nexit 0\n")
	hooks := safeProcessHooks(t)
	attempts := 0
	hooks.version = func(ctx context.Context, _ string) (string, error) {
		attempts++
		<-ctx.Done()
		return "", ctx.Err()
	}
	origin := testRenderOrigin(t)
	type outcome struct {
		renderer *Renderer
		err      error
	}
	done := make(chan outcome, 1)
	go func() {
		renderer, err := New(Config{BrowserExecutable: executable, RenderOrigin: origin, testHooks: hooks})
		done <- outcome{renderer: renderer, err: err}
	}()
	select {
	case result := <-done:
		assertInitializationError(t, result.err, ErrInvalidConfig, "timeout", "operation=version-probe")
		if result.renderer != nil || attempts != 2 {
			t.Fatalf("exhausted version probe returned renderer = %v, attempts = %d", result.renderer, attempts)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("version probes exceeded their startup budget")
	}
}

func TestNew_DoesNotRetryOtherVersionFailures(t *testing.T) {
	for _, test := range []struct {
		name    string
		version string
		err     error
		detail  string
	}{
		{"canceled", "", context.Canceled, "operation canceled"},
		{"process failure", "", errors.New("browser launch failed"), "browser launch failed"},
		{"wrong version", "Google Chrome 1.2.3.4", nil, "browser version mismatch"},
	} {
		t.Run(test.name, func(t *testing.T) {
			executable := writeExecutable(t, "#!/bin/sh\nexit 0\n")
			hooks := safeProcessHooks(t)
			attempts := 0
			hooks.version = func(context.Context, string) (string, error) {
				attempts++
				return test.version, test.err
			}
			renderer, err := New(Config{BrowserExecutable: executable, RenderOrigin: testRenderOrigin(t), testHooks: hooks})
			assertInitializationError(t, err, ErrInvalidConfig, "version-probe", test.detail)
			if renderer != nil || attempts != 1 {
				t.Fatalf("invalid version probe returned renderer = %v, attempts = %d", renderer, attempts)
			}
		})
	}
}

func TestNew_PreservesFinalTimeoutDiagnosticsAfterOneRetry(t *testing.T) {
	executable := writeExecutable(t, "#!/bin/sh\nexit 0\n")
	hooks := safeProcessHooks(t)
	attempts := 0
	hooks.version = func(context.Context, string) (string, error) {
		attempts++
		return "", &initializationError{
			kind: ErrInvalidConfig, stage: "timeout", operation: "version-probe", reason: "deadline exceeded",
			exitStatus: attempts, signal: 9, elapsed: time.Second,
		}
	}
	_, err := New(Config{BrowserExecutable: executable, RenderOrigin: testRenderOrigin(t), testHooks: hooks})
	assertInitializationError(t, err, ErrInvalidConfig, "timeout", "operation=version-probe")
	var failure *initializationError
	if !errors.As(err, &failure) || attempts != 2 || failure.exitStatus != 2 || failure.signal != 9 || failure.elapsed != time.Second {
		t.Fatalf("final version failure = %v, attempts = %d", err, attempts)
	}
}

func TestCheckBrowserVersion_ParentCancellationPreventsRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	attempts := 0
	err := checkBrowserVersion(ctx, "/unused-browser", func(context.Context, string) (string, error) {
		attempts++
		cancel()
		return "", context.DeadlineExceeded
	})
	assertInitializationError(t, err, ErrInvalidConfig, "version-probe", "operation canceled")
	if attempts != 1 {
		t.Fatalf("version attempts after parent cancellation = %d, want 1", attempts)
	}
}

func TestCheckBrowserVersion_ParentDeadlinePreventsRetry(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	attempts := 0
	err := checkBrowserVersion(ctx, "/unused-browser", func(probeCtx context.Context, _ string) (string, error) {
		attempts++
		<-probeCtx.Done()
		return "", probeCtx.Err()
	})
	assertInitializationError(t, err, ErrInvalidConfig, "timeout", "operation=version-probe")
	if attempts != 1 {
		t.Fatalf("version attempts after parent deadline = %d, want 1", attempts)
	}
}
