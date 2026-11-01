package printrender

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestVersionCancellationJoinsStartedBrowser(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	startedPath := filepath.Join(t.TempDir(), "browser-started")
	quotedPath := strings.ReplaceAll(startedPath, "'", "'\\''")
	executable := writeExecutable(t, fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$$\" > '%s.tmp' && /bin/mv '%s.tmp' '%s' || exit 1\nexec /bin/sleep 30\n", quotedPath, quotedPath, quotedPath))
	version := defaultHooksWithSupervisor(testSupervisorCommand(t)).version
	probeCtx, probeCancel := context.WithCancel(ctx)
	defer probeCancel()
	result := make(chan error, 1)
	go func() {
		_, probeErr := version(probeCtx, executable)
		result <- probeErr
	}()
	// The browser starts after the supervisor installs its cancellation handler.
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	var line string
	for {
		data, readErr := os.ReadFile(startedPath)
		if readErr == nil {
			line = string(data)
			break
		}
		if !os.IsNotExist(readErr) {
			t.Fatalf("read browser startup: %v", readErr)
		}
		select {
		case probeErr := <-result:
			t.Fatalf("version probe returned before browser startup: %v", probeErr)
		case <-ctx.Done():
			t.Fatal("browser startup did not complete before deadline")
		case <-ticker.C:
		}
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || pid <= 0 {
		t.Fatalf("invalid browser PID %q: %v", line, err)
	}
	if stateErr := stoppedBrowserError(pid); stateErr == nil || !strings.Contains(stateErr.Error(), "remains executable in state") {
		t.Fatalf("live browser PID %d: expected executable process error, got %v", pid, stateErr)
	}
	probeCancel()
	select {
	case probeErr := <-result:
		assertInitializationError(t, probeErr, ErrInvalidConfig, "version-probe", "operation canceled")
	case <-ctx.Done():
		t.Fatal("version cancellation did not return after browser teardown")
	}
	if stateErr := stoppedBrowserError(pid); stateErr != nil {
		t.Fatal(stateErr)
	}
}

func stoppedBrowserError(pid int) error {
	state, err := ownedProcessState(pid)
	if err != nil {
		return err
	}
	if state != 0 && state != 'Z' {
		return fmt.Errorf("browser PID %d remains executable in state %c", pid, state)
	}
	return nil
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
