package printrender

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
)

func TestVersionFailureReportsExitStatusWithoutPrivateOutput(t *testing.T) {
	executable := writeExecutable(t, "#!/bin/sh\nprintf 'TOKEN=private-value /private/path document-text\\n' >&2\nexit 7\n")
	hooks := safeProcessHooks(t)
	_, err := New(Config{BrowserExecutable: executable, RenderOrigin: testRenderOrigin(t), testHooks: hooks})
	assertInitializationError(t, err, ErrInvalidConfig, "version-probe", "exit_status=7")
}

func TestVersionTimeoutReportsItsOperation(t *testing.T) {
	testCtx, testCancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer testCancel()
	executable := writeExecutable(t, "#!/bin/sh\nexit 0\n")
	hooks := safeProcessHooks(t)
	hooks.version = func(ctx context.Context, _ string) (string, error) {
		probeCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()
		<-probeCtx.Done()
		return "", probeCtx.Err()
	}
	origin := testRenderOrigin(t)
	result := make(chan error, 1)
	go func() {
		_, err := New(Config{BrowserExecutable: executable, RenderOrigin: origin, testHooks: hooks})
		result <- err
	}()
	select {
	case err := <-result:
		assertInitializationError(t, err, ErrInvalidConfig, "timeout", "operation=version-probe")
	case <-testCtx.Done():
		t.Fatal("version timeout did not return")
	}
}

func TestWrongBrowserVersionFailsClosedBeforeReadinessOrRender(t *testing.T) {
	executable := writeExecutable(t, "#!/bin/sh\nprintf 'Google Chrome 1.2.3.4\\n'\n")
	hooks := safeProcessHooks(t)
	hooks.ready = func(context.Context, *Renderer) error {
		t.Fatal("readiness launched with a wrong browser version")
		return nil
	}
	hooks.attempt = func(context.Context, *Renderer, renderjob.Navigation) ([]byte, error) {
		t.Fatal("render launched with a wrong browser version")
		return nil, nil
	}
	renderer, err := New(Config{BrowserExecutable: executable, RenderOrigin: testRenderOrigin(t), testHooks: hooks})
	assertInitializationError(t, err, ErrInvalidConfig, "version-probe", "browser version mismatch")
	if renderer != nil {
		t.Fatal("wrong browser version returned a renderer")
	}
}

func TestVersionOutputIsBoundedAndCannotHideAWrongVersion(t *testing.T) {
	executable := writeExecutable(t, fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' '%s %s'\n", strings.Repeat("x", 8192), expectedBrowserVersion))
	hooks := safeProcessHooks(t)
	_, err := New(Config{BrowserExecutable: executable, RenderOrigin: testRenderOrigin(t), testHooks: hooks})
	assertInitializationError(t, err, ErrInvalidConfig, "version-probe", "version output exceeds limit")
}

func TestMissingBinaryReportsLookupWithoutPath(t *testing.T) {
	_, err := New(Config{BrowserExecutable: filepath.Join(t.TempDir(), "private-value"), RenderOrigin: testRenderOrigin(t)})
	assertInitializationError(t, err, ErrInvalidConfig, "binary-lookup", "no such file or directory")
}

func TestReadinessRechecksAFakeBinaryThatFailsOnce(t *testing.T) {
	directory := t.TempDir()
	executable := writeExecutable(t, fmt.Sprintf(`#!/bin/sh
if [ ! -f '%s/attempted' ]; then
	: > '%s/attempted'
	printf 'TOKEN=private-value /private/path document-text\n' >&2
	exit 7
fi
printf 'Google Chrome for Testing %s\n'
`, directory, directory, expectedBrowserVersion))
	hooks := safeProcessHooks(t)
	version := hooks.version
	// Construction isolates the readiness failure from version verification.
	hooks.version = func(context.Context, string) (string, error) { return expectedBrowserVersion, nil }
	now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	hooks.now = func() time.Time { return now }
	hooks.ready = func(ctx context.Context, renderer *Renderer) error {
		_, probeErr := version(ctx, renderer.executable)
		return probeErr
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	renderer, err := New(Config{BrowserExecutable: executable, RenderOrigin: testRenderOrigin(t), Logger: logger, testHooks: hooks})
	if err != nil {
		t.Fatal(err)
	}
	if readyErr := renderer.Ready(); !errors.Is(readyErr, ErrUnavailable) {
		t.Fatalf("Ready() = %v, want opaque ErrUnavailable", readyErr)
	}
	firstLog := logs.String()
	if !strings.Contains(firstLog, "exit_status=7") || !strings.Contains(firstLog, "stage=version-probe") {
		t.Fatalf("missing initialization diagnostic: %s", firstLog)
	}
	assertNoPrivateDiagnostic(t, firstLog)
	if readyErr := renderer.Ready(); !errors.Is(readyErr, ErrUnavailable) {
		t.Fatalf("Ready() within cooldown = %v", readyErr)
	}
	if logs.String() != firstLog {
		t.Fatal("cached readiness failure repeated its log")
	}
	now = now.Add(5 * time.Second)
	if readyErr := renderer.Ready(); readyErr != nil {
		t.Fatalf("Ready() after cooldown = %v", readyErr)
	}
	if !strings.Contains(logs.String(), "initialization recovered") {
		t.Fatal("readiness recovery was not logged")
	}
}

func TestReadinessLaunchReportsBrowserExitAndRedactsOutput(t *testing.T) {
	executable := writeExecutable(t, "#!/bin/sh\nprintf 'No usable sandbox! TOKEN=private-value /private/path document-text\\n' >&2\nexit 7\n")
	hooks := safeProcessHooks(t)
	hooks.version = func(context.Context, string) (string, error) { return expectedBrowserVersion, nil }
	var logs bytes.Buffer
	renderer, err := New(Config{BrowserExecutable: executable, RenderOrigin: testRenderOrigin(t), Logger: slog.New(slog.NewTextHandler(&logs, nil)), testHooks: hooks})
	if err != nil {
		t.Fatal(err)
	}
	if readyErr := renderer.Ready(); !errors.Is(readyErr, ErrUnavailable) {
		t.Fatalf("Ready() = %v", readyErr)
	}
	assertNoPrivateDiagnostic(t, logs.String())
	for _, value := range []string{"stage=launch", "exit_status=7", "no usable sandbox"} {
		if !strings.Contains(logs.String(), value) {
			t.Fatalf("diagnostic lacks %q: %s", value, logs.String())
		}
	}
}

func TestConcurrentReadinessRequestsShareFailureAndRecovery(t *testing.T) {
	executable := writeExecutable(t, "#!/bin/sh\nexit 0\n")
	hooks := safeProcessHooks(t)
	hooks.version = func(context.Context, string) (string, error) { return expectedBrowserVersion, nil }
	var seconds atomic.Int64
	hooks.now = func() time.Time { return time.Unix(seconds.Load(), 0) }
	var calls atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	hooks.ready = func(context.Context, *Renderer) error {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
			return errors.New("private-value")
		}
		return nil
	}
	renderer, err := New(Config{BrowserExecutable: executable, RenderOrigin: testRenderOrigin(t), testHooks: hooks})
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 8)
	var requests sync.WaitGroup
	for range cap(results) {
		requests.Go(func() { results <- renderer.Ready() })
	}
	<-entered
	close(release)
	requests.Wait()
	for range cap(results) {
		if readyErr := <-results; !errors.Is(readyErr, ErrUnavailable) {
			t.Fatalf("concurrent Ready() = %v", readyErr)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("failed probes = %d, want 1", calls.Load())
	}
	seconds.Store(5)
	for range cap(results) {
		requests.Go(func() { results <- renderer.Ready() })
	}
	requests.Wait()
	for range cap(results) {
		if readyErr := <-results; readyErr != nil {
			t.Fatalf("concurrent recovery = %v", readyErr)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("total probes = %d, want 2", calls.Load())
	}
}

func TestReadinessDoesNotLaunchWhileARenderOwnsTheBrowser(t *testing.T) {
	executable := writeExecutable(t, "#!/bin/sh\nexit 0\n")
	hooks := safeProcessHooks(t)
	hooks.version = func(context.Context, string) (string, error) { return expectedBrowserVersion, nil }
	var probes atomic.Int32
	hooks.ready = func(context.Context, *Renderer) error {
		probes.Add(1)
		return nil
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	hooks.attempt = func(context.Context, *Renderer, renderjob.Navigation) ([]byte, error) {
		close(entered)
		<-release
		return []byte("output"), nil
	}
	renderer, err := New(Config{BrowserExecutable: executable, RenderOrigin: testRenderOrigin(t), testHooks: hooks})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, renderErr := renderer.Render(context.Background(), validTestNavigation(renderjob.PDF))
		done <- renderErr
	}()
	<-entered
	readyErr := renderer.Ready()
	close(release)
	if renderErr := <-done; renderErr != nil {
		t.Fatal(renderErr)
	}
	if !errors.Is(readyErr, ErrUnavailable) || probes.Load() != 0 {
		t.Fatalf("Ready() with an active render = %v, probes = %d", readyErr, probes.Load())
	}
	if readyErr = renderer.Ready(); readyErr != nil || probes.Load() != 1 {
		t.Fatalf("Ready() after render teardown = %v, probes = %d", readyErr, probes.Load())
	}
	// Cached success does not launch another browser during a later render.
	gate := renderer.browserGate()
	gate <- struct{}{}
	readyErr = renderer.Ready()
	<-gate
	if readyErr != nil || probes.Load() != 1 {
		t.Fatalf("cached readiness = %v, probes = %d", readyErr, probes.Load())
	}
}

func TestRenderCancellationStopsWaitingForABrowserPermit(t *testing.T) {
	renderer := &Renderer{render: func(context.Context, renderjob.Navigation) ([]byte, error) {
		t.Error("canceled render launched a browser")
		return nil, nil
	}}
	gate := renderer.browserGate()
	gate <- struct{}{}
	defer func() { <-gate }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waiting := make(chan struct{})
	observed := &permitWaitContext{Context: ctx, waiting: waiting}
	done := make(chan error, 1)
	go func() {
		_, err := renderer.Render(observed, validTestNavigation(renderjob.PDF))
		done <- err
	}()
	<-waiting
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiting render error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled render kept waiting for the browser permit")
	}
}

type permitWaitContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *permitWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestInitializationDiagnosticsUseOnlyClosedReasons(t *testing.T) {
	for _, test := range []struct{ input, reason string }{
		{"private-value /private/path document-text", "operation failed; detail redacted"},
		{"TOKEN=private-value\nFailed to move to new namespace: Operation not permitted", "sandbox namespace creation failed"},
		{"private-value: No space left on device", "no space left on device"},
		{"private-value: Resource temporarily unavailable", "resource temporarily unavailable"},
		{"private-value: error while loading shared libraries: /private/path", "shared library load failed"},
		{"private-value: ProcessSingleton lock: document-text", "browser profile lock failed"},
	} {
		failure := diagnoseInitialization(context.Background(), ErrUnavailable, "launch", errors.New(test.input), nil, time.Second)
		if failure.reason != test.reason {
			t.Fatalf("reason = %q, want %q", failure.reason, test.reason)
		}
		assertNoPrivateDiagnostic(t, failure.Error())
	}
}

func TestBrowserCleanupKeepsRenderOutputLimitError(t *testing.T) {
	err := withBrowser(context.Background(), "/bin/true", "http://127.0.0.1:20030", testSupervisorCommand(t), false, func(context.Context) error {
		return ErrOutputTooLarge
	})
	if !errors.Is(err, ErrOutputTooLarge) {
		t.Fatalf("render error after browser cleanup = %v", err)
	}
}

func TestBrowserJoinProofWithoutAPipeAllowsNoChildCompletion(t *testing.T) {
	proof := &browserJoinProof{done: make(chan struct{}), readerDone: make(chan struct{})}
	proof.noChildStarted()
	select {
	case <-proof.done:
	default:
		t.Fatal("failed pipe creation retained a nonexistent child")
	}
}

func safeProcessHooks(t *testing.T) *runtimeHooks {
	t.Helper()
	hooks := defaultHooksWithSupervisor(testSupervisorCommand(t))
	hooks.euid = func() int { return 1000 }
	hooks.sandboxSupported = func() bool { return true }
	return hooks
}

func assertInitializationError(t *testing.T, err, sentinel error, stage, detail string) {
	t.Helper()
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
	message := err.Error()
	if !strings.Contains(message, "stage="+stage) || !strings.Contains(message, detail) {
		t.Fatalf("error lacks stage %q or detail %q: %s", stage, detail, message)
	}
	if len(message) > 512 {
		t.Fatal("initialization diagnostic exceeds its bound")
	}
	assertNoPrivateDiagnostic(t, message)
}

func assertNoPrivateDiagnostic(t *testing.T, message string) {
	t.Helper()
	for _, value := range []string{"TOKEN", "private-value", "/private/path", "document-text"} {
		if strings.Contains(message, value) {
			t.Fatalf("initialization diagnostic leaked %q", value)
		}
	}
}
