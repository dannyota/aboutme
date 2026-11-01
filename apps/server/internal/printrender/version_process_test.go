package printrender

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestVersionCancellationJoinsStartedBrowser(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("print rendering runs only on Linux; CI covers browser cancellation")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	startedPath := filepath.Join(t.TempDir(), "browser-started")
	if err := syscall.Mkfifo(startedPath, 0o600); err != nil {
		t.Fatal(err)
	}
	started, err := os.OpenFile(startedPath, os.O_RDWR|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer started.Close() //nolint:errcheck
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("version cancellation test has no deadline")
	}
	if deadlineErr := started.SetReadDeadline(deadline); deadlineErr != nil {
		t.Fatal(deadlineErr)
	}
	executable := writeExecutable(t, fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$$\" > '%s'\nexec /bin/sleep 30\n", startedPath))
	version := defaultHooksWithSupervisor(testSupervisorCommand(t)).version
	probeCtx, probeCancel := context.WithCancel(ctx)
	defer probeCancel()
	result := make(chan error, 1)
	go func() {
		_, probeErr := version(probeCtx, executable)
		result <- probeErr
	}()
	// The browser starts after the supervisor installs its cancellation handler.
	line, err := bufio.NewReader(started).ReadString('\n')
	if err != nil {
		t.Fatalf("read browser startup: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || pid <= 0 {
		t.Fatalf("invalid browser PID %q: %v", line, err)
	}
	probeCancel()
	select {
	case probeErr := <-result:
		assertInitializationError(t, probeErr, ErrInvalidConfig, "version-probe", "operation canceled")
	case <-ctx.Done():
		t.Fatal("version cancellation did not return after browser teardown")
	}
	state, err := ownedProcessState(pid)
	if err != nil {
		t.Fatal(err)
	}
	if state != 0 && state != 'Z' {
		t.Fatalf("browser PID %d remains executable in state %c", pid, state)
	}
}

func TestDefaultVersionProbeUsesIsolatedBrowserProcess(t *testing.T) {
	t.Setenv("ABOUTME_PARENT_SECRET_SENTINEL", "must-not-reach-version-probe")
	executable := writeExecutable(t, `#!/bin/sh
if [ -n "${ABOUTME_PARENT_SECRET_SENTINEL+x}" ]; then
	exit 9
fi
printf '%s %s %s %s\n' "$(/bin/ps -o pgid= -p $$)" "$TZ" "$LANG" "$LC_ALL"
`)

	output, err := defaultHooksWithSupervisor(testSupervisorCommand(t)).version(context.Background(), executable)
	if err != nil {
		t.Fatal(err)
	}
	var probeGroup int
	var timezone, language, locale string
	if _, parseErr := fmt.Sscan(output, &probeGroup, &timezone, &language, &locale); parseErr != nil {
		t.Fatalf("parse version probe output: %v", parseErr)
	}
	serverGroup, err := syscall.Getpgid(syscall.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if probeGroup == serverGroup {
		t.Fatalf("version probe process group = server process group %d", serverGroup)
	}
	if timezone != "UTC" || language != "C.UTF-8" || locale != "C.UTF-8" {
		t.Fatalf("version probe environment = %q %q %q", timezone, language, locale)
	}
}
