package printrender

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/chromedp/cdproto/target"
)

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
	configureBrowserCommand(executable)(cmd)
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
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, browserEnvironmentExecutable, "-c", "/bin/sleep 30 & wait")
	configureBrowserCommand("/bin/sh")(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid || cmd.SysProcAttr.Pdeathsig != syscall.SIGKILL {
		t.Fatalf("SysProcAttr = %#v", cmd.SysProcAttr)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	cancel()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-time.After(2 * time.Second):
		t.Fatal("process group was not joined")
	case <-done:
	}
	if err := syscall.Kill(-pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("process group still exists: %v", err)
	}
}
