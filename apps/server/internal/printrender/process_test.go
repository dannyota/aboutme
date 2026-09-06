package printrender

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/chromedp/cdproto/target"

	"github.com/dannyota/aboutme/apps/server/internal/renderprocess"
)

func TestRenderBrowserSupervisorHelper(t *testing.T) {
	for index, arg := range os.Args {
		if arg == "render-supervisor-helper" {
			os.Exit(renderprocess.Run(os.Args[index+1:]))
		}
	}
}

func configureTestBrowserCommand(t *testing.T, executable string) func(*exec.Cmd) {
	t.Helper()
	testExecutable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	configure, proof := newBrowserCommand(executable, testExecutable, []string{"-test.run=^TestRenderBrowserSupervisorHelper$", "render-supervisor-helper"})
	t.Cleanup(proof.wait)
	return configure
}

func newTestBrowserCommand(t *testing.T, executable string) (func(*exec.Cmd), *browserJoinProof) {
	t.Helper()
	testExecutable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return newBrowserCommand(executable, testExecutable, []string{"-test.run=^TestRenderBrowserSupervisorHelper$", "render-supervisor-helper"})
}

func testSupervisorCommand(t *testing.T) supervisorCommand {
	t.Helper()
	testExecutable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return supervisorCommand{path: testExecutable, prefix: []string{"-test.run=^TestRenderBrowserSupervisorHelper$", "render-supervisor-helper"}}
}

func TestBrowserJoinProofRejectsMalformedOrRepeatedStart(t *testing.T) {
	for name, protocol := range map[string]string{
		"malformed":      "XDSD",
		"repeated-start": "SSD",
	} {
		t.Run(name, func(t *testing.T) {
			_, proof := newBrowserCommand("/bin/true", "/bin/true", nil)
			if _, err := proof.writer.Write([]byte(protocol)); err != nil {
				t.Fatal(err)
			}
			proof.closeWriters()
			<-proof.readerDone
			select {
			case <-proof.done:
				t.Fatalf("protocol %q completed join proof", protocol)
			default:
			}
		})
	}
}

func TestBrowserJoinProofAcceptsNoChildOrStartedDone(t *testing.T) {
	for _, protocol := range []string{"D", "SD"} {
		_, proof := newBrowserCommand("/bin/true", "/bin/true", nil)
		if _, err := proof.writer.Write([]byte(protocol)); err != nil {
			t.Fatal(err)
		}
		proof.closeWriters()
		<-proof.readerDone
		select {
		case <-proof.done:
		default:
			t.Fatalf("protocol %q did not complete", protocol)
		}
	}
}

func TestBrowserChildEnvironmentContainsOnlyFixedValues(t *testing.T) {
	if slices.Contains(os.Args, "capture-browser-environment") {
		if err := os.WriteFile("browser-environment", []byte(strings.Join(os.Environ(), "\n")), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Setenv("ABOUTME_PARENT_SECRET_SENTINEL", "must-not-reach-browser")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	cmd := exec.CommandContext(context.Background(), browserEnvironmentExecutable, "-test.run=^TestBrowserChildEnvironmentContainsOnlyFixedValues$", "capture-browser-environment")
	cmd.Dir = directory
	configureTestBrowserCommand(t, executable)(cmd)
	if cmd.Env == nil || len(cmd.Env) != 0 {
		t.Fatal("launcher environment is not explicitly empty")
	}
	if runErr := cmd.Run(); runErr != nil {
		t.Fatal(runErr)
	}
	got, err := os.ReadFile(filepath.Join(directory, "browser-environment"))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join(browserEnvironment(), "\n")
	if string(got) != want {
		t.Fatal("browser environment differs from the fixed allowlist")
	}
}

func TestBrowserFlagsKeepSandboxAndDeterminismPins(t *testing.T) {
	flags := browserFlags("http://127.0.0.1:23456")
	for name, want := range map[string]any{
		"headless":                      true,
		"no-sandbox":                    false,
		"disable-background-networking": true,
		"disable-component-update":      true,
		"disable-default-apps":          true,
		"disable-extensions":            true,
		"disable-sync":                  true,
		"dns-prefetch-disable":          true,
		"no-first-run":                  true,
		"force-color-profile":           "srgb",
		"font-render-hinting":           "none",
		"disable-lcd-text":              true,
		"disable-gpu":                   true,
		"hide-scrollbars":               true,
		"force-device-scale-factor":     "1",
		"proxy-server":                  "http://127.0.0.1:23456",
		"proxy-bypass-list":             "<-loopback>",
	} {
		if got := flags[name]; got != want {
			t.Fatalf("flag %q = %#v, want %#v", name, got, want)
		}
	}
	features, ok := flags["disable-features"].(string)
	if !ok {
		t.Fatal("disabled feature flag is not a string")
	}
	for _, feature := range []string{"PreconnectToSearch", "Prerender2", "SpeculationRulesPrefetchProxy"} {
		if !strings.Contains(features, feature) {
			t.Fatalf("disabled features omit %q: %q", feature, features)
		}
	}
	for _, forbidden := range []string{"enable-automation", "enable-unsafe-swiftshader", "disable-site-isolation-trials"} {
		if _, ok := flags[forbidden]; ok {
			t.Fatalf("forbidden flag %q present", forbidden)
		}
	}
}

func TestUnexpectedTargetRejectsPageOwnedExecutionTargets(t *testing.T) {
	main := target.ID("main")
	for _, test := range []struct {
		name string
		info *target.Info
		want bool
	}{
		{"main page", &target.Info{TargetID: main, Type: "page"}, false},
		{"browser ui", &target.Info{TargetID: "internal", Type: "browser_ui"}, false},
		{"popup", &target.Info{TargetID: "popup", Type: "page"}, true},
		{"worker", &target.Info{TargetID: "worker", Type: "worker"}, true},
		{"service worker", &target.Info{TargetID: "service", Type: "service_worker"}, true},
		{"frame", &target.Info{TargetID: "frame", Type: "iframe"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := unexpectedTarget(test.info, main); got != test.want {
				t.Fatalf("unexpectedTarget() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestStoppedCallbackAdmissionFailsAttempt(t *testing.T) {
	var group joinGroup
	group.stop()
	var failure attemptFailure
	canceled := false
	if scheduleCallback(&group, &failure, func() { canceled = true }, func() {}) {
		t.Fatal("callback started after join began")
	}
	if !failure.failed() || !canceled {
		t.Fatalf("failure = %v, canceled = %v", failure.failed(), canceled)
	}
}

func TestCommandCancellationKillsAndJoinsProcessGroup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	childPIDReader, childPIDWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer childPIDReader.Close() //nolint:errcheck
	defer childPIDWriter.Close() //nolint:errcheck
	cmd := exec.CommandContext(ctx, browserEnvironmentExecutable, "-c", `/bin/sleep 30 & printf '%s\n' "$!" >&3; wait`)
	cmd.ExtraFiles = []*os.File{childPIDWriter}
	configureTestBrowserCommand(t, "/bin/sh")(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid || cmd.SysProcAttr.Pdeathsig != syscall.SIGKILL {
		t.Fatalf("SysProcAttr = %#v", cmd.SysProcAttr)
	}
	if startErr := cmd.Start(); startErr != nil {
		t.Fatal(startErr)
	}
	done := make(chan error, 1)
	joined := false
	go func() {
		done <- cmd.Wait()
	}()
	defer func() {
		cancel()
		if joined {
			return
		}
		select {
		case waitErr := <-done:
			if waitErr == nil {
				t.Error("canceled process group exited successfully")
			}
		case <-time.After(2 * time.Second):
			t.Error("process group cleanup did not finish")
		}
	}()
	pid := cmd.Process.Pid
	if closeErr := childPIDWriter.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if deadlineErr := childPIDReader.SetReadDeadline(time.Now().Add(2 * time.Second)); deadlineErr != nil {
		t.Fatal(deadlineErr)
	}
	childLine, err := bufio.NewReader(childPIDReader).ReadString('\n')
	if err != nil {
		t.Fatalf("read child PID: %v", err)
	}
	childPID, err := strconv.Atoi(strings.TrimSpace(childLine))
	if err != nil || childPID <= 0 {
		t.Fatalf("child PID %q: %v", childLine, err)
	}
	cancel()
	select {
	case <-time.After(2 * time.Second):
		t.Fatal("process group was not joined")
	case waitErr := <-done:
		joined = true
		if waitErr == nil {
			t.Fatal("canceled process group exited successfully")
		}
	}
	if groupErr := syscall.Kill(-pid, 0); errors.Is(groupErr, syscall.ESRCH) {
		return
	} else if groupErr != nil {
		t.Fatalf("inspect owned process group: %v", groupErr)
	}
	state, err := ownedProcessState(childPID)
	if err != nil {
		t.Fatal(err)
	}
	if state != 0 && state != 'Z' {
		t.Fatalf("child PID %d remains executable in state %c", childPID, state)
	}
}

func TestCommandNaturalLeaderExitJoinsLiveDescendant(t *testing.T) {
	childPIDReader, childPIDWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer childPIDReader.Close() //nolint:errcheck
	defer childPIDWriter.Close() //nolint:errcheck
	exitGateReader, exitGateWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer exitGateReader.Close() //nolint:errcheck
	defer exitGateWriter.Close() //nolint:errcheck
	cmd := exec.CommandContext(context.Background(), browserEnvironmentExecutable, "-c", `/bin/sleep 30 & printf '%s\n' "$!" >&3; read -r release <&4; exit 0`)
	cmd.ExtraFiles = []*os.File{childPIDWriter, exitGateReader}
	configure, _ := newTestBrowserCommand(t, "/bin/sh")
	configure(cmd)
	if startErr := cmd.Start(); startErr != nil {
		t.Fatal(startErr)
	}
	done := make(chan error, 1)
	joined := false
	childPID := 0
	go func() { done <- cmd.Wait() }()
	defer func() {
		if joined {
			return
		}
		if childPID > 0 {
			ignoreProcessError(syscall.Kill(childPID, syscall.SIGKILL))
		}
		ignoreProcessError(exitGateWriter.Close())
		ignoreProcessError(cmd.Process.Signal(syscall.SIGTERM))
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("supervisor cleanup did not finish")
		}
	}()
	if closeErr := childPIDWriter.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if deadlineErr := childPIDReader.SetReadDeadline(time.Now().Add(2 * time.Second)); deadlineErr != nil {
		t.Fatal(deadlineErr)
	}
	line, err := bufio.NewReader(childPIDReader).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	childPID, err = strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatal(err)
	}
	state, err := ownedProcessState(childPID)
	if err != nil {
		t.Fatal(err)
	}
	if state == 0 || state == 'Z' {
		t.Fatalf("child PID %d was not live before natural leader exit", childPID)
	}
	if _, writeErr := exitGateWriter.Write([]byte("release\n")); writeErr != nil {
		t.Fatal(writeErr)
	}
	if closeErr := exitGateWriter.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	select {
	case waitErr := <-done:
		joined = true
		if waitErr != nil {
			t.Fatal(waitErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor did not join descendant after natural leader exit")
	}
	state, err = ownedProcessState(childPID)
	if err != nil {
		t.Fatal(err)
	}
	if state != 0 && state != 'Z' {
		t.Fatalf("child PID %d remains executable in state %c", childPID, state)
	}
}

func TestMissingSupervisorProofNeverCompletesJoin(t *testing.T) {
	configure, proof := newTestBrowserCommand(t, "/bin/sh")
	cmd := exec.CommandContext(context.Background(), browserEnvironmentExecutable, "-c", "kill -KILL $PPID")
	configure(cmd)
	if err := cmd.Run(); err == nil {
		t.Fatal("unexpected supervisor death succeeded")
	}
	select {
	case <-proof.done:
		t.Fatal("missing supervisor proof was treated as joined")
	default:
	}
}

func ownedProcessState(pid int) (byte, error) {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	closingParen := strings.LastIndexByte(string(stat), ')')
	if closingParen < 0 || closingParen+2 >= len(stat) || stat[closingParen+1] != ' ' {
		return 0, errors.New("malformed process stat for owned child")
	}
	return stat[closingParen+2], nil
}
