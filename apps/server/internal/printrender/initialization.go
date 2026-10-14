package printrender

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const initializationOutputLimit = 4096

// initializationError retains only closed diagnostics, never dependency text.
type initializationError struct {
	kind       error
	stage      string
	operation  string
	reason     string
	exitStatus int
	signal     int
	elapsed    time.Duration
}

func (e *initializationError) Error() string {
	return fmt.Sprintf("%s: stage=%s operation=%s exit_status=%d signal=%d elapsed_ms=%d error=%s",
		e.kind, e.stage, e.operation, e.exitStatus, e.signal, e.elapsed.Milliseconds(), e.reason)
}

func (e *initializationError) Unwrap() error { return e.kind }

// LogValue returns only closed diagnostic fields for structured logging.
func (e *initializationError) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("stage", e.stage), slog.String("operation", e.operation),
		slog.Int("exit_status", e.exitStatus), slog.Int("signal", e.signal),
		slog.Int64("elapsed_ms", e.elapsed.Milliseconds()), slog.String("reason", e.reason),
	)
}

func initializationFailure(kind error, stage, reason string) *initializationError {
	return &initializationError{kind: kind, stage: stage, operation: stage, reason: reason, exitStatus: -1}
}

func diagnoseInitialization(ctx context.Context, kind error, stage string, err error, state *os.ProcessState, elapsed time.Duration) *initializationError {
	var previous *initializationError
	if errors.As(err, &previous) {
		copy := *previous
		copy.kind = kind
		return &copy
	}
	failure := initializationFailure(kind, stage, "operation failed; detail redacted")
	failure.elapsed = elapsed
	var exitErr *exec.ExitError
	if state == nil && errors.As(err, &exitErr) {
		state = exitErr.ProcessState
	}
	if state != nil {
		failure.exitStatus = state.ExitCode()
		if status, ok := state.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			failure.signal = int(status.Signal())
		}
	}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded), errors.Is(err, context.DeadlineExceeded), err != nil && err.Error() == "websocket url timeout reached":
		failure.stage, failure.reason = "timeout", "deadline exceeded"
	case errors.Is(ctx.Err(), context.Canceled), errors.Is(err, context.Canceled):
		failure.reason = "operation canceled"
	default:
		failure.reason = initializationReason(err)
	}
	return failure
}

func initializationReason(err error) string {
	if err == nil {
		return "operation failed; detail redacted"
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		// Errno text comes from the OS table and contains no path or input.
		return errno.Error()
	}
	text := err.Error()
	if len(text) > initializationOutputLimit {
		text = text[:initializationOutputLimit]
	}
	text = strings.ToLower(text)
	// Never log subprocess text. Match known causes and emit fixed words only.
	for _, diagnostic := range []struct{ match, reason string }{
		{"no usable sandbox", "no usable sandbox"},
		{"failed to move to new namespace", "sandbox namespace creation failed"},
		{"operation not permitted", "operation not permitted"},
		{"permission denied", "permission denied"},
		{"no space left on device", "no space left on device"},
		{"cannot allocate memory", "cannot allocate memory"},
		{"resource temporarily unavailable", "resource temporarily unavailable"},
		{"error while loading shared libraries", "shared library load failed"},
		{"processsingleton", "browser profile lock failed"},
		{"pidfd unavailable", "pidfd unavailable"},
		{"canceled before launch", "canceled before launch"},
		{"browser launch failed", "browser launch failed"},
	} {
		if strings.Contains(text, diagnostic.match) {
			return diagnostic.reason
		}
	}
	return "operation failed; detail redacted"
}

type boundedInitializationOutput struct {
	bytes    []byte
	overflow bool
}

func (b *boundedInitializationOutput) Write(value []byte) (int, error) {
	remaining := initializationOutputLimit - len(b.bytes)
	if len(value) > remaining {
		b.overflow = true
	}
	b.bytes = append(b.bytes, value[:min(len(value), remaining)]...)
	return len(value), nil
}

func probeBrowserVersion(ctx context.Context, executable string, supervisor supervisorCommand) (string, error) {
	started := time.Now()
	// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command -- executable is the configured absolute browser path that New checked and never request data
	cmd := exec.CommandContext(ctx, executable, "--version")
	configure, proof := newBrowserCommand(executable, supervisor.path, supervisor.prefix)
	configure(cmd)
	var stdout, stderr boundedInitializationOutput
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if cmd.Process == nil {
		proof.noChildStarted()
	}
	proof.wait()
	if err != nil || ctx.Err() != nil {
		failure := diagnoseInitialization(ctx, ErrInvalidConfig, "version-probe", err, cmd.ProcessState, time.Since(started))
		if failure.stage != "timeout" && ctx.Err() == nil && len(stderr.bytes) > 0 {
			failure.reason = initializationReason(errors.New(string(stderr.bytes)))
		}
		return "", failure
	}
	if stdout.overflow {
		failure := initializationFailure(ErrInvalidConfig, "version-probe", "version output exceeds limit")
		failure.exitStatus, failure.elapsed = 0, time.Since(started)
		return "", failure
	}
	return strings.TrimSpace(string(stdout.bytes)), nil
}
